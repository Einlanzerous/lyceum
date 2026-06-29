// Package delivery is Lyceum's "Send to Kindle" SMTP component (LYCM-401). It
// builds a MIME message with the EPUB attached and ships it to a recipient's
// Kindle address over SMTP. It is deliberately decoupled from internal/store:
// callers hand it the book's bytes and a destination address.
//
// Modern Kindle "Send to Kindle" firmware accepts EPUB directly, so no format
// conversion is performed; a future ticket could add EPUB→KFX/MOBI conversion
// behind the same SendBook entrypoint.
package delivery

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"
)

// TLS modes for the SMTP connection.
const (
	TLSStartTLS = "starttls" // plaintext connect, upgrade via STARTTLS (port 587)
	TLSImplicit = "implicit" // TLS from the first byte (port 465)
	TLSNone     = "none"     // plaintext, no encryption (local capture servers/tests)
)

// Config describes the upstream SMTP relay. From is the envelope sender and the
// message From header; it must be an address the relay is allowed to send as.
type Config struct {
	Host     string
	Port     int
	Username string // optional; when empty no AUTH is attempted
	Password string
	From     string
	TLS      string // one of the TLS* constants; defaults to STARTTLS when empty
}

func (c Config) addr() string { return net.JoinHostPort(c.Host, fmt.Sprintf("%d", c.Port)) }

// Book is the payload to deliver: the EPUB bytes plus the metadata used to name
// the attachment and title the message.
type Book struct {
	Title    string
	Filename string // attachment filename, e.g. "book-12.epub"
	Content  []byte // raw EPUB bytes
}

// Sender ships books over SMTP using a fixed Config.
type Sender struct {
	cfg Config
	// dial is overridable in tests to inject a capture server; production uses
	// the real net dialer via the default nil value.
	dial func(network, addr string) (net.Conn, error)
}

// New builds a Sender. It validates that the essential relay fields are set so
// a misconfiguration surfaces at construction rather than on first send.
func New(cfg Config) (*Sender, error) {
	if cfg.Host == "" || cfg.Port == 0 {
		return nil, errors.New("delivery: SMTP host and port are required")
	}
	if strings.TrimSpace(cfg.From) == "" {
		return nil, errors.New("delivery: From address is required")
	}
	if cfg.TLS == "" {
		cfg.TLS = TLSStartTLS
	}
	return &Sender{cfg: cfg}, nil
}

// SendBook delivers book to toAddr as an EPUB attachment. The context bounds
// the dial; the SMTP exchange itself is governed by the connection deadline
// derived from it.
func (s *Sender) SendBook(ctx context.Context, toAddr string, book Book) error {
	from, err := mail.ParseAddress(s.cfg.From)
	if err != nil {
		return fmt.Errorf("delivery: invalid From %q: %w", s.cfg.From, err)
	}
	to, err := mail.ParseAddress(toAddr)
	if err != nil {
		return fmt.Errorf("delivery: invalid recipient %q: %w", toAddr, err)
	}

	msg, err := buildMessage(s.cfg.From, to.Address, book)
	if err != nil {
		return fmt.Errorf("delivery: build message: %w", err)
	}

	client, err := s.connect(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if s.cfg.Username != "" {
		auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("delivery: auth: %w", err)
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return fmt.Errorf("delivery: MAIL FROM: %w", err)
	}
	if err := client.Rcpt(to.Address); err != nil {
		return fmt.Errorf("delivery: RCPT TO: %w", err)
	}
	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("delivery: DATA: %w", err)
	}
	if _, err := wc.Write(msg); err != nil {
		_ = wc.Close()
		return fmt.Errorf("delivery: write message: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("delivery: close DATA: %w", err)
	}
	return client.Quit()
}

// connect dials the relay, performs EHLO, and (for STARTTLS) upgrades the
// connection. It returns a ready smtp.Client positioned for AUTH/MAIL.
func (s *Sender) connect(ctx context.Context) (*smtp.Client, error) {
	dialer := s.dial
	if dialer == nil {
		nd := &net.Dialer{}
		dialer = func(network, addr string) (net.Conn, error) {
			return nd.DialContext(ctx, network, addr)
		}
	}

	addr := s.cfg.addr()
	conn, err := dialer("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("delivery: dial %s: %w", addr, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	if s.cfg.TLS == TLSImplicit {
		conn = tls.Client(conn, &tls.Config{ServerName: s.cfg.Host})
	}

	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("delivery: smtp client: %w", err)
	}

	if s.cfg.TLS == TLSStartTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: s.cfg.Host}); err != nil {
				_ = client.Close()
				return nil, fmt.Errorf("delivery: STARTTLS: %w", err)
			}
		} else {
			_ = client.Close()
			return nil, errors.New("delivery: server does not advertise STARTTLS")
		}
	}
	return client, nil
}

// buildMessage assembles a multipart/mixed message: a short text/plain body and
// the EPUB as a base64 application/epub+zip attachment. from is used verbatim
// in the header; envelope addressing is handled by the caller.
func buildMessage(from, to string, book Book) ([]byte, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	title := strings.TrimSpace(book.Title)
	if title == "" {
		title = "Your book"
	}
	filename := book.Filename
	if filename == "" {
		filename = "book.epub"
	}

	// Top-level headers. The Date/Message-ID give the message a well-formed
	// envelope; Subject is informational (Kindle ignores it for EPUB).
	headers := textproto.MIMEHeader{}
	headers.Set("From", from)
	headers.Set("To", to)
	headers.Set("Subject", mime.QEncoding.Encode("utf-8", title))
	headers.Set("Date", time.Now().Format(time.RFC1123Z))
	headers.Set("MIME-Version", "1.0")
	headers.Set("Content-Type", "multipart/mixed; boundary="+mw.Boundary())
	for k, vs := range headers {
		for _, v := range vs {
			fmt.Fprintf(&buf, "%s: %s\r\n", k, v)
		}
	}
	buf.WriteString("\r\n")

	// Plain-text body part.
	bodyHdr := textproto.MIMEHeader{}
	bodyHdr.Set("Content-Type", "text/plain; charset=utf-8")
	// 8bit, not 7bit: the title (and thus the body line) may carry non-ASCII
	// UTF-8, which a 7bit declaration would misrepresent to strict relays.
	bodyHdr.Set("Content-Transfer-Encoding", "8bit")
	bp, err := mw.CreatePart(bodyHdr)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(bp, "%s, delivered by Lyceum.\r\n", title)

	// EPUB attachment part, base64 with RFC 2045 line wrapping.
	attHdr := textproto.MIMEHeader{}
	attHdr.Set("Content-Type", "application/epub+zip")
	attHdr.Set("Content-Transfer-Encoding", "base64")
	attHdr.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	ap, err := mw.CreatePart(attHdr)
	if err != nil {
		return nil, err
	}
	if err := writeBase64Wrapped(ap, book.Content); err != nil {
		return nil, err
	}

	if err := mw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
