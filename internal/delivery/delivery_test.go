package delivery

import (
	"bufio"
	"context"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"strings"
	"sync"
	"testing"
	"time"
)

// captureSMTP is a throwaway in-process SMTP server that speaks just enough of
// the protocol to accept one message and record the raw DATA payload. It lets
// the delivery tests assert a well-formed message without any live send.
type captureSMTP struct {
	ln   net.Listener
	mu   sync.Mutex
	data string // raw message bytes captured from the DATA phase
	from string
	to   string
}

func startCaptureSMTP(t *testing.T) *captureSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	c := &captureSMTP{ln: ln}
	go c.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return c
}

func (c *captureSMTP) addr() string { return c.ln.Addr().String() }

func (c *captureSMTP) serve() {
	conn, err := c.ln.Accept()
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	writeLine := func(s string) {
		_, _ = w.WriteString(s + "\r\n")
		_ = w.Flush()
	}

	writeLine("220 capture ESMTP ready")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.TrimSpace(line)
		upper := strings.ToUpper(cmd)
		switch {
		case strings.HasPrefix(upper, "EHLO"):
			writeLine("250-capture greets you")
			writeLine("250 OK")
		case strings.HasPrefix(upper, "HELO"):
			writeLine("250 OK")
		case strings.HasPrefix(upper, "MAIL FROM"):
			c.mu.Lock()
			c.from = cmd
			c.mu.Unlock()
			writeLine("250 OK")
		case strings.HasPrefix(upper, "RCPT TO"):
			c.mu.Lock()
			c.to = cmd
			c.mu.Unlock()
			writeLine("250 OK")
		case upper == "DATA":
			writeLine("354 End data with <CR><LF>.<CR><LF>")
			var b strings.Builder
			for {
				dl, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if dl == ".\r\n" || dl == ".\n" {
					break
				}
				// Undo dot-stuffing of any line beginning with '.'.
				if strings.HasPrefix(dl, "..") {
					dl = dl[1:]
				}
				b.WriteString(dl)
			}
			c.mu.Lock()
			c.data = b.String()
			c.mu.Unlock()
			writeLine("250 OK queued")
		case upper == "QUIT":
			writeLine("221 Bye")
			return
		case upper == "RSET", upper == "NOOP":
			writeLine("250 OK")
		default:
			writeLine("250 OK")
		}
	}
}

func TestSendBook(t *testing.T) {
	srv := startCaptureSMTP(t)

	sender, err := New(Config{
		Host: "127.0.0.1", Port: 2525,
		From: "Lyceum <library@lyceum.test>",
		TLS:  TLSNone,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Redirect the dialer at the capture server (the configured port is moot).
	sender.dial = func(network, _ string) (net.Conn, error) {
		return net.Dial(network, srv.addr())
	}

	epub := []byte("PK\x03\x04 this stands in for an EPUB payload \x00\x01\x02\xff")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sender.SendBook(ctx, "reader@kindle.com", Book{
		Title:    "Méditations",
		Filename: "book-7.epub",
		Content:  epub,
	}); err != nil {
		t.Fatalf("SendBook: %v", err)
	}

	srv.mu.Lock()
	raw, envFrom, envTo := srv.data, srv.from, srv.to
	srv.mu.Unlock()

	if !strings.Contains(envFrom, "library@lyceum.test") {
		t.Errorf("envelope MAIL FROM = %q, want library@lyceum.test", envFrom)
	}
	if !strings.Contains(envTo, "reader@kindle.com") {
		t.Errorf("envelope RCPT TO = %q, want reader@kindle.com", envTo)
	}
	if raw == "" {
		t.Fatal("no message captured")
	}

	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("parse message: %v", err)
	}
	if got := msg.Header.Get("To"); !strings.Contains(got, "reader@kindle.com") {
		t.Errorf("To header = %q", got)
	}
	// Subject is RFC 2047 encoded for the non-ASCII title; decode and check.
	subj, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil {
		t.Fatalf("decode subject: %v", err)
	}
	if subj != "Méditations" {
		t.Errorf("Subject = %q, want Méditations", subj)
	}

	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		t.Fatalf("Content-Type = %q (err %v), want multipart", mediaType, err)
	}

	mr := multipart.NewReader(msg.Body, params["boundary"])
	var foundEPUB bool
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("next part: %v", err)
		}
		if part.Header.Get("Content-Type") != "application/epub+zip" {
			continue
		}
		foundEPUB = true
		if cte := part.Header.Get("Content-Transfer-Encoding"); cte != "base64" {
			t.Errorf("attachment CTE = %q, want base64", cte)
		}
		if cd := part.Header.Get("Content-Disposition"); !strings.Contains(cd, "book-7.epub") {
			t.Errorf("Content-Disposition = %q, want filename book-7.epub", cd)
		}
		decoded, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, part))
		if err != nil {
			t.Fatalf("decode attachment: %v", err)
		}
		if string(decoded) != string(epub) {
			t.Errorf("attachment bytes mismatch: got %d bytes, want %d", len(decoded), len(epub))
		}
	}
	if !foundEPUB {
		t.Error("no application/epub+zip attachment found")
	}
}

func TestNewValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
	}{
		{"no host", Config{Port: 587, From: "a@b.c"}},
		{"no port", Config{Host: "x", From: "a@b.c"}},
		{"no from", Config{Host: "x", Port: 587}},
	} {
		if _, err := New(tc.cfg); err == nil {
			t.Errorf("%s: New = nil error, want error", tc.name)
		}
	}
}
