// Package acquire turns an owned-but-not-yet-digital ISBN into a real grab
// request against a live acquisition backend — the concrete implementation of
// the api.Acquirer seam (LYCM-35). Its counterpart, the metadata edition
// resolver, only identifies which book a barcode is; this package actually asks
// the backend to fetch a DRM-free EPUB.
//
// The backend is Bindery (https://github.com/vavallee/bindery), the deployed
// Readarr replacement in the argosy-acquisition stack: it searches the shared
// Prowlarr indexers and downloads via SABnzbd into /data/media/books, which the
// Lyceum folder-ingest watcher then picks up. This client drives Bindery's
// REST API (/api/v1/*, X-Api-Key auth) to add the scanned title to Bindery's
// library and kick off its own search+grab pipeline.
package acquire

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultUserAgent = "Lyceum/1.0 (self-hosted ebook server; acquire)"

	// requestTimeout bounds a single Bindery call. Bindery adds a book
	// synchronously (a metadata pull) but runs the searchOnAdd search+grab as a
	// background command, so its add endpoint returns promptly; the cap guards a
	// wedged backend without blocking the confirm request that calls Want.
	requestTimeout = 15 * time.Second

	// maxBody bounds a decoded Bindery JSON response.
	maxBody = 4 << 20
)

// errNotFound signals that Bindery could not resolve the ISBN to an addable
// book (no metadata match). It is handled internally by Want as a best-effort
// miss — the inventory entry still records intent as `wanted` — and is not
// surfaced to callers.
var errNotFound = errors.New("acquire: bindery has no book for ISBN")

// Bindery is an api.Acquirer backed by a Bindery server. The zero value needs
// BaseURL + APIKey set; NewBindery is the convenient constructor. Exported
// fields let tests point Client/BaseURL at an httptest server.
type Bindery struct {
	BaseURL   string // e.g. http://localhost:8787 (no trailing /api/v1)
	APIKey    string
	Client    *http.Client
	UserAgent string
}

// NewBindery returns a client targeting baseURL with the given API key (found in
// Bindery's Settings → General → Security) and a bounded per-request timeout.
func NewBindery(baseURL, apiKey string) *Bindery {
	return &Bindery{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		APIKey:    apiKey,
		Client:    &http.Client{Timeout: requestTimeout},
		UserAgent: defaultUserAgent,
	}
}

// binderyBook is the subset of Bindery's Book we read from a lookup / add
// response. A lookup returns metadata (ID == 0 until it is in the library); the
// nested author carries the foreign IDs the add endpoint needs.
type binderyBook struct {
	ID            int64         `json:"id"`
	ForeignBookID string        `json:"foreignBookId"`
	Title         string        `json:"title"`
	MediaType     string        `json:"mediaType"`
	Monitored     bool          `json:"monitored"`
	Author        binderyAuthor `json:"author"`
}

type binderyAuthor struct {
	ForeignAuthorID string `json:"foreignAuthorId"`
	AuthorName      string `json:"authorName"`
	Name            string `json:"name"` // fallback field name on some responses
}

func (a binderyAuthor) name() string {
	if a.AuthorName != "" {
		return a.AuthorName
	}
	return a.Name
}

// addBookRequest is the POST /author/book body. searchOnAdd makes Bindery run
// its own search+grab once the book is monitored/wanted — that pipeline
// augments the release with the bookId (the field a raw-release grab omits,
// which is what previously blocked auto-import).
type addBookRequest struct {
	ForeignBookID   string `json:"foreignBookId"`
	ForeignAuthorID string `json:"foreignAuthorId"`
	AuthorName      string `json:"authorName"`
	SearchOnAdd     bool   `json:"searchOnAdd"`
	MediaType       string `json:"mediaType,omitempty"`
}

// Want asks Bindery to acquire a DRM-free EPUB for the given canonical ISBN-13.
// It is best-effort: a missing title, an unreachable backend, or a non-2xx
// response is logged and swallowed (returns nil) so it never fails the confirm
// request that records the inventory entry as `wanted`. Bindery does the actual
// download asynchronously; the file later lands in /data/media/books and the
// folder-ingest watcher imports it.
//
// If searchOnAdd ever proves insufficient in live runs, the explicit escalation
// is: POST /api/v1/book/{id}/search, pick the best approved release, then POST
// /api/v1/queue/grab with {guid,title,nzbUrl,size,protocol,mediaType,bookId} —
// setting bookId is the essential part.
func (b *Bindery) Want(ctx context.Context, code string) error {
	book, err := b.lookup(ctx, code)
	if err != nil {
		if errors.Is(err, errNotFound) {
			log.Printf("acquire: bindery has no match for ISBN %s; recorded wanted only", code)
			return nil
		}
		log.Printf("acquire: bindery lookup ISBN %s failed: %v; recorded wanted only", code, err)
		return nil
	}
	if book.ForeignBookID == "" || book.Author.name() == "" {
		log.Printf("acquire: bindery lookup ISBN %s returned no addable book; recorded wanted only", code)
		return nil
	}

	// Already in Bindery's library: its monitor/search pipeline already owns the
	// grab. Adding again would 409 — treat as success (idempotent re-confirm).
	if book.ID != 0 {
		log.Printf("acquire: bindery already tracks ISBN %s (bookId=%d, %q)", code, book.ID, book.Title)
		return nil
	}

	created, err := b.addBook(ctx, addBookRequest{
		ForeignBookID:   book.ForeignBookID,
		ForeignAuthorID: book.Author.ForeignAuthorID, // may be empty; Bindery resolves by ISBN
		AuthorName:      book.Author.name(),
		SearchOnAdd:     true,
		MediaType:       "ebook",
	})
	if err != nil {
		log.Printf("acquire: bindery add ISBN %s failed: %v; recorded wanted only", code, err)
		return nil
	}
	log.Printf("acquire: bindery grabbing ISBN %s (bookId=%d, %q)", code, created.ID, created.Title)
	return nil
}

// lookup resolves an ISBN to Bindery's book metadata. A 404 (or an empty body)
// is reported as errNotFound.
func (b *Bindery) lookup(ctx context.Context, code string) (binderyBook, error) {
	q := url.Values{}
	q.Set("isbn", code)
	resp, err := b.do(ctx, http.MethodGet, "/api/v1/book/lookup?"+q.Encode(), nil)
	if err != nil {
		return binderyBook{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return binderyBook{}, errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return binderyBook{}, fmt.Errorf("acquire: bindery lookup status %d", resp.StatusCode)
	}

	var book binderyBook
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&book); err != nil {
		return binderyBook{}, fmt.Errorf("acquire: decode lookup: %w", err)
	}
	if book.ForeignBookID == "" {
		return binderyBook{}, errNotFound
	}
	return book, nil
}

// addBook adds a book to Bindery's library and returns the created record (with
// its assigned id). A 409 Conflict means the book already exists — a benign
// race with a concurrent confirm — and is reported as success with an empty
// book so Want treats it idempotently.
func (b *Bindery) addBook(ctx context.Context, req addBookRequest) (binderyBook, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return binderyBook{}, fmt.Errorf("acquire: encode add: %w", err)
	}
	resp, err := b.do(ctx, http.MethodPost, "/api/v1/author/book", body)
	if err != nil {
		return binderyBook{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusConflict {
		return binderyBook{}, nil
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return binderyBook{}, fmt.Errorf("acquire: bindery add status %d", resp.StatusCode)
	}

	var book binderyBook
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&book); err != nil {
		return binderyBook{}, fmt.Errorf("acquire: decode add: %w", err)
	}
	return book, nil
}

// do issues an authenticated request to Bindery. path is everything after
// BaseURL (including /api/v1 and any query string); body is nil for GETs.
func (b *Bindery) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.BaseURL+path, r)
	if err != nil {
		return nil, fmt.Errorf("acquire: build request: %w", err)
	}
	req.Header.Set("X-Api-Key", b.APIKey)
	if b.UserAgent != "" {
		req.Header.Set("User-Agent", b.UserAgent)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := b.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("acquire: %s %s: %w", method, path, err)
	}
	return resp, nil
}
