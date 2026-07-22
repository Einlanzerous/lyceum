package acquire

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// capture records what a fake Bindery received so a test can assert on the
// requests Want made.
type capture struct {
	lookupISBN string
	lookupHits int
	addHits    int
	addBody    addBookRequest
	apiKeySeen string
}

// stubBindery stands up a fake Bindery whose /book/lookup returns lookupBody
// (or 404 when empty) and whose /author/book echoes a created book with id 77.
// It returns a client pointed at it and the capture of what it saw.
func stubBindery(t *testing.T, lookupStatus int, lookupBody string) (*Bindery, *capture) {
	t.Helper()
	cap := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.apiKeySeen = r.Header.Get("X-Api-Key")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/book/lookup":
			cap.lookupHits++
			cap.lookupISBN = r.URL.Query().Get("isbn")
			if lookupStatus != http.StatusOK {
				w.WriteHeader(lookupStatus)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, lookupBody)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/author/book":
			cap.addHits++
			_ = json.NewDecoder(r.Body).Decode(&cap.addBody)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":77,"title":"The Dinosaur Lords","foreignBookId":"OL1B"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	b := NewBindery(srv.URL, "secret-key")
	return b, cap
}

// a lookup body for a fresh (not-yet-in-library) book with a resolvable author.
const freshLookup = `{
	"id":0,"foreignBookId":"OL1B","title":"The Dinosaur Lords","mediaType":"ebook",
	"author":{"foreignAuthorId":"OL2A","authorName":"Victor Milán"}
}`

func TestWantAddsFreshBookWithSearchOnAdd(t *testing.T) {
	b, cap := stubBindery(t, http.StatusOK, freshLookup)

	if err := b.Want(context.Background(), "9780765382115"); err != nil {
		t.Fatalf("Want: %v", err)
	}
	if cap.apiKeySeen != "secret-key" {
		t.Fatalf("X-Api-Key = %q, want secret-key", cap.apiKeySeen)
	}
	if cap.lookupISBN != "9780765382115" {
		t.Fatalf("lookup isbn = %q", cap.lookupISBN)
	}
	if cap.addHits != 1 {
		t.Fatalf("add hits = %d, want 1", cap.addHits)
	}
	if !cap.addBody.SearchOnAdd {
		t.Fatalf("add did not set searchOnAdd")
	}
	if cap.addBody.ForeignBookID != "OL1B" || cap.addBody.ForeignAuthorID != "OL2A" {
		t.Fatalf("add foreign ids = %q/%q", cap.addBody.ForeignBookID, cap.addBody.ForeignAuthorID)
	}
	if cap.addBody.AuthorName != "Victor Milán" {
		t.Fatalf("add authorName = %q", cap.addBody.AuthorName)
	}
	if cap.addBody.MediaType != "ebook" {
		t.Fatalf("add mediaType = %q, want ebook", cap.addBody.MediaType)
	}
}

func TestWantIdempotentWhenAlreadyInLibrary(t *testing.T) {
	// A non-zero id means Bindery already tracks the book: Want must not add it
	// again (which would 409).
	const inLibrary = `{
		"id":42,"foreignBookId":"OL1B","title":"The Dinosaur Lords",
		"author":{"foreignAuthorId":"OL2A","authorName":"Victor Milán"}
	}`
	b, cap := stubBindery(t, http.StatusOK, inLibrary)

	if err := b.Want(context.Background(), "9780765382115"); err != nil {
		t.Fatalf("Want: %v", err)
	}
	if cap.addHits != 0 {
		t.Fatalf("add hits = %d, want 0 for an already-tracked book", cap.addHits)
	}
}

func TestWantNotFoundIsNonFatal(t *testing.T) {
	// A 404 lookup must not error (the caller records `wanted` regardless) and
	// must not attempt an add.
	b, cap := stubBindery(t, http.StatusNotFound, "")

	if err := b.Want(context.Background(), "9780000000002"); err != nil {
		t.Fatalf("Want on 404 lookup = %v, want nil (best-effort)", err)
	}
	if cap.lookupHits != 1 {
		t.Fatalf("lookup hits = %d, want 1", cap.lookupHits)
	}
	if cap.addHits != 0 {
		t.Fatalf("add hits = %d, want 0 when nothing was found", cap.addHits)
	}
}

func TestWantEmptyMatchIsNonFatal(t *testing.T) {
	// A 200 with no foreignBookId is treated as no match: no add, no error.
	b, cap := stubBindery(t, http.StatusOK, `{"id":0}`)

	if err := b.Want(context.Background(), "9780000000002"); err != nil {
		t.Fatalf("Want: %v", err)
	}
	if cap.addHits != 0 {
		t.Fatalf("add hits = %d, want 0 for an empty match", cap.addHits)
	}
}

func TestWantBackendErrorIsNonFatal(t *testing.T) {
	// A 5xx from lookup is logged and swallowed so a flaky Bindery never breaks
	// the confirm flow.
	b, _ := stubBindery(t, http.StatusInternalServerError, "")

	if err := b.Want(context.Background(), "9780765382115"); err != nil {
		t.Fatalf("Want on lookup 500 = %v, want nil (best-effort)", err)
	}
}

func TestWantRetriesTransientTransportError(t *testing.T) {
	// A lookup whose first attempt drops the connection (a stand-in for the
	// client timeout seen under burst, LYCM-99) must be retried and then
	// succeed, driving the add exactly once.
	old := retryBackoff
	retryBackoff = time.Millisecond
	t.Cleanup(func() { retryBackoff = old })

	// Counters are touched from the server goroutine and read after Want
	// returns; a hijacked+closed connection gives no happens-before, so use
	// atomics to stay clean under -race.
	var lookupHits, addHits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/book/lookup":
			if lookupHits.Add(1) == 1 {
				// Hijack and close without a response → transport error.
				hj, ok := w.(http.Hijacker)
				if !ok {
					t.Errorf("test server does not support hijack")
					return
				}
				conn, _, err := hj.Hijack()
				if err != nil {
					t.Errorf("hijack: %v", err)
					return
				}
				_ = conn.Close()
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, freshLookup)
		case "/api/v1/author/book":
			addHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":77,"title":"The Dinosaur Lords","foreignBookId":"OL1B"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	b := NewBindery(srv.URL, "k")

	if err := b.Want(context.Background(), "9780765382115"); err != nil {
		t.Fatalf("Want: %v", err)
	}
	if got := lookupHits.Load(); got < 2 {
		t.Fatalf("lookup hits = %d, want >= 2 (a retry after the dropped connection)", got)
	}
	if got := addHits.Load(); got != 1 {
		t.Fatalf("add hits = %d, want 1", got)
	}
}

func TestWantExhaustedRetriesIsNonFatal(t *testing.T) {
	// When every attempt fails transport-side, Want gives up after maxAttempts
	// and still returns nil (best-effort: the entry rests in `wanted`).
	old := retryBackoff
	retryBackoff = time.Millisecond
	t.Cleanup(func() { retryBackoff = old })

	// Atomic: written by the server goroutine, read after Want returns, with no
	// happens-before from the hijacked+closed connection (stays clean -race).
	var lookupHits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/book/lookup" {
			http.NotFound(w, r)
			return
		}
		lookupHits.Add(1)
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Errorf("test server does not support hijack")
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		_ = conn.Close()
	}))
	t.Cleanup(srv.Close)
	b := NewBindery(srv.URL, "k")

	if err := b.Want(context.Background(), "9780765382115"); err != nil {
		t.Fatalf("Want on exhausted retries = %v, want nil (best-effort)", err)
	}
	if got := lookupHits.Load(); got != maxAttempts {
		t.Fatalf("lookup hits = %d, want %d (one per attempt)", got, maxAttempts)
	}
}

func TestWantAddConflictIsNonFatal(t *testing.T) {
	// A 409 on add (a concurrent confirm added it first) is benign.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/book/lookup" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, freshLookup)
			return
		}
		w.WriteHeader(http.StatusConflict)
	}))
	t.Cleanup(srv.Close)
	b := NewBindery(srv.URL, "k")

	if err := b.Want(context.Background(), "9780765382115"); err != nil {
		t.Fatalf("Want on add 409 = %v, want nil", err)
	}
}
