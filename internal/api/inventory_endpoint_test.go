package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// recordingAcquirer captures the ISBNs handed to the acquisition pipeline so
// tests can assert whether a digital copy was requested.
type recordingAcquirer struct {
	mu    sync.Mutex
	wants []string
}

func (r *recordingAcquirer) Want(_ context.Context, code string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.wants = append(r.wants, code)
	return nil
}

func postInventory(t *testing.T, baseURL string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(baseURL+"/inventory", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST /inventory: %v", err)
	}
	return resp
}

// The scan/capture flow: a hyphenated ISBN with find_digital records an owned
// title, requests a digital copy, and lands in the wanted state.
func TestInventoryScanFindDigital(t *testing.T) {
	s := testStore(t)
	acq := &recordingAcquirer{}
	srv := httptest.NewServer(New(s, "", WithAcquirer(acq)).Handler())
	t.Cleanup(srv.Close)

	resp := postInventory(t, srv.URL, map[string]any{
		"isbn":         "978-0-14-044919-8",
		"title":        "The Iliad",
		"find_digital": true,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got inventoryJSON
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ISBN != "9780140449198" {
		t.Fatalf("isbn = %q, want normalized 9780140449198", got.ISBN)
	}
	if got.State != "wanted" {
		t.Fatalf("state = %q, want wanted", got.State)
	}
	if len(acq.wants) != 1 || acq.wants[0] != "9780140449198" {
		t.Fatalf("acquirer wants = %v, want [9780140449198]", acq.wants)
	}
}

// Without find_digital, a scan just records ownership and never calls the
// acquirer.
func TestInventoryScanOwnedOnly(t *testing.T) {
	s := testStore(t)
	acq := &recordingAcquirer{}
	srv := httptest.NewServer(New(s, "", WithAcquirer(acq)).Handler())
	t.Cleanup(srv.Close)

	resp := postInventory(t, srv.URL, map[string]any{"isbn": "9780140449198"})
	defer resp.Body.Close()
	var got inventoryJSON
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.State != "owned" {
		t.Fatalf("state = %q, want owned", got.State)
	}
	if len(acq.wants) != 0 {
		t.Fatalf("acquirer called %v, want none", acq.wants)
	}
}

func TestInventoryInvalidISBN(t *testing.T) {
	s := testStore(t)
	srv := newServer(t, s)

	resp := postInventory(t, srv.URL, map[string]any{"isbn": "not-an-isbn"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// An already-ingested title is not downgraded to wanted, and no grab is
// requested, even when find_digital is set.
func TestInventoryFindDigitalSkipsIngested(t *testing.T) {
	s := testStore(t)
	acq := &recordingAcquirer{}
	a := New(s, "", WithAcquirer(acq))
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)

	// Ingest an EPUB so the ISBN is in the ingested state.
	data := epubWithIdentifier(t, "Owned Digital", "urn:isbn:9780140449198")
	if _, _, err := a.ingestEPUB(context.Background(), data, "x.epub", ""); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	resp := postInventory(t, srv.URL, map[string]any{"isbn": "9780140449198", "find_digital": true})
	defer resp.Body.Close()
	var got inventoryJSON
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.State != "ingested" {
		t.Fatalf("state = %q, want ingested (not downgraded)", got.State)
	}
	if len(acq.wants) != 0 {
		t.Fatalf("acquirer called %v for an ingested title, want none", acq.wants)
	}
}
