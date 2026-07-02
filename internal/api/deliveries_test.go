package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/magos/lyceum/internal/delivery"
	"github.com/magos/lyceum/internal/store"
)

// fakeSender captures SendBook calls and can be told to fail.
type fakeSender struct {
	mu   sync.Mutex
	sent []sentBook
	fail error
}

type sentBook struct {
	toAddr string
	book   delivery.Book
}

func (f *fakeSender) SendBook(_ context.Context, toAddr string, book delivery.Book) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.sent = append(f.sent, sentBook{toAddr: toAddr, book: book})
	return nil
}

func (f *fakeSender) calls() []sentBook {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentBook(nil), f.sent...)
}

const (
	tokenRead = "eid-token"
	tokenSend = "snd-token"
)

func newDeliveryServer(t *testing.T, s *store.Store, sender BookSender, kindleAddr string, autoSend bool) *httptest.Server {
	t.Helper()
	auth, err := ParseTokens(tokenRead + "=eidolon:read," + tokenSend + "=delivery:send")
	if err != nil {
		t.Fatalf("ParseTokens: %v", err)
	}
	disp := NewDispatcher(s, sender, 2, 5*time.Second)
	t.Cleanup(disp.Close)
	srv := httptest.NewServer(New(s, "", WithAuth(auth), WithDeliveries(disp, kindleAddr, autoSend)).Handler())
	t.Cleanup(srv.Close)
	return srv
}

// pollDelivery waits for the most recent delivery on a book to reach a terminal
// status, returning it. It fails the test on timeout.
func pollDelivery(t *testing.T, srv *httptest.Server, bookID int64) deliveryJSON {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/books/"+strconv.FormatInt(bookID, 10)+"/deliveries", nil)
		req.Header.Set("Authorization", "Bearer "+tokenSend)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET deliveries: %v", err)
		}
		var ds []deliveryJSON
		_ = json.NewDecoder(resp.Body).Decode(&ds)
		resp.Body.Close()
		if len(ds) > 0 && ds[0].Status != store.DeliveryQueued {
			return ds[0]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("delivery for book %d did not reach a terminal status in time", bookID)
	return deliveryJSON{}
}

func postSend(t *testing.T, srv *httptest.Server, bookID int64, token, body string) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = bytes.NewBufferString(body)
	}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/books/"+strconv.FormatInt(bookID, 10)+"/send-to-kindle", rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST send-to-kindle: %v", err)
	}
	return resp
}

func TestManualSendToKindle(t *testing.T) {
	s := testStore(t)
	b := seedBook(t, s, "hash-send", "Moby-Dick", "Melville", nil)
	sender := &fakeSender{}
	srv := newDeliveryServer(t, s, sender, "", false)

	resp := postSend(t, srv, b.ID, tokenSend, `{"to_addr":"reader@kindle.com"}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	var queued deliveryJSON
	_ = json.NewDecoder(resp.Body).Decode(&queued)
	resp.Body.Close()
	if queued.Status != store.DeliveryQueued || queued.ToAddr != "reader@kindle.com" {
		t.Fatalf("queued delivery = %+v", queued)
	}

	final := pollDelivery(t, srv, b.ID)
	if final.Status != store.DeliverySent {
		t.Fatalf("final status = %q, want sent (error=%q)", final.Status, final.Error)
	}
	calls := sender.calls()
	if len(calls) != 1 {
		t.Fatalf("sender calls = %d, want 1", len(calls))
	}
	if calls[0].toAddr != "reader@kindle.com" {
		t.Errorf("toAddr = %q", calls[0].toAddr)
	}
	if string(calls[0].book.Content) != string(epubBytes) {
		t.Errorf("delivered content mismatch")
	}
	if calls[0].book.Filename != fmt.Sprintf("book-%d.epub", b.ID) {
		t.Errorf("filename = %q", calls[0].book.Filename)
	}
}

func TestSendToKindleAuth(t *testing.T) {
	s := testStore(t)
	b := seedBook(t, s, "hash-auth", "Book", "Author", nil)
	srv := newDeliveryServer(t, s, &fakeSender{}, "k@kindle.com", false)

	// No token → 401.
	resp := postSend(t, srv, b.ID, "", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-token status = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()

	// Valid token but wrong scope (eidolon:read) → 403.
	resp = postSend(t, srv, b.ID, tokenRead, "")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong-scope status = %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestSendToKindleDefaultRecipient(t *testing.T) {
	s := testStore(t)
	b := seedBook(t, s, "hash-default", "Book", "Author", nil)
	sender := &fakeSender{}
	srv := newDeliveryServer(t, s, sender, "configured@kindle.com", false)

	resp := postSend(t, srv, b.ID, tokenSend, "") // empty body → use configured addr
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	resp.Body.Close()

	final := pollDelivery(t, srv, b.ID)
	if final.Status != store.DeliverySent || final.ToAddr != "configured@kindle.com" {
		t.Fatalf("final = %+v", final)
	}
}

func TestSendToKindleNoRecipient(t *testing.T) {
	s := testStore(t)
	b := seedBook(t, s, "hash-norcpt", "Book", "Author", nil)
	srv := newDeliveryServer(t, s, &fakeSender{}, "", false) // no configured addr

	resp := postSend(t, srv, b.ID, tokenSend, "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestSendFailureRecorded(t *testing.T) {
	s := testStore(t)
	b := seedBook(t, s, "hash-fail", "Book", "Author", nil)
	sender := &fakeSender{fail: errors.New("smtp exploded")}
	srv := newDeliveryServer(t, s, sender, "", false)

	resp := postSend(t, srv, b.ID, tokenSend, `{"to_addr":"x@kindle.com"}`)
	resp.Body.Close()

	final := pollDelivery(t, srv, b.ID)
	if final.Status != store.DeliveryFailed {
		t.Fatalf("status = %q, want failed", final.Status)
	}
	if final.Error == "" {
		t.Error("expected an error message on the failed delivery")
	}
}

// TestEnqueueAfterClose verifies the shutdown contract: once the dispatcher is
// closed, a new Enqueue records the delivery as failed rather than panicking on
// a send to the closed channel.
func TestEnqueueAfterClose(t *testing.T) {
	s := testStore(t)
	b := seedBook(t, s, "hash-closed", "Book", "Author", nil)
	disp := NewDispatcher(s, &fakeSender{}, 2, time.Second)
	disp.Close()
	disp.Close() // idempotent

	rec, err := disp.Enqueue(context.Background(), b, "x@kindle.com")
	if err != nil {
		t.Fatalf("Enqueue after close: %v", err)
	}
	if rec.Status != store.DeliveryFailed {
		t.Fatalf("status = %q, want failed", rec.Status)
	}
}

func TestAutoSendOnUpload(t *testing.T) {
	s := testStore(t)
	sender := &fakeSender{}
	srv := newDeliveryServer(t, s, sender, "auto@kindle.com", true)

	// Upload a real EPUB through the live endpoint; the hook should enqueue.
	book := uploadEPUB(t, srv, syntheticEPUB(t))

	final := pollDelivery(t, srv, book.ID)
	if final.Status != store.DeliverySent || final.ToAddr != "auto@kindle.com" {
		t.Fatalf("auto-send final = %+v", final)
	}
	if calls := sender.calls(); len(calls) != 1 {
		t.Fatalf("sender calls = %d, want 1", len(calls))
	}
}

// uploadEPUB POSTs data to /upload as multipart and returns the created book.
func uploadEPUB(t *testing.T, srv *httptest.Server, data []byte) bookJSON {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "book.epub")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	_ = mw.Close()

	resp, err := http.Post(srv.URL+"/upload", mw.FormDataContentType(), &body)
	if err != nil {
		t.Fatalf("POST /upload: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload status = %d, want 201: %s", resp.StatusCode, b)
	}
	var book bookJSON
	if err := json.NewDecoder(resp.Body).Decode(&book); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	return book
}
