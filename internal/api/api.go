// Package api exposes Lyceum's read-side HTTP surface: the library listing and
// the cover/EPUB blob endpoints. It is mounted by cmd/lyceum on top of the
// existing /healthz route.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/magos/lyceum/internal/coverart"
	"github.com/magos/lyceum/internal/store"
)

// Store is the slice of *store.Store behaviour the API depends on. Keeping it
// as an interface makes the handlers trivially testable with a fake.
type Store interface {
	ListBooks(ctx context.Context) ([]store.Book, error)
	GetBook(ctx context.Context, id int64) (store.Book, error)
	GetBookByHash(ctx context.Context, hash string) (store.Book, error)
	GetLatestPosition(ctx context.Context, bookID int64) (store.ReadingPosition, error)
	GetPosition(ctx context.Context, bookID int64, deviceID string) (store.ReadingPosition, error)
	UpsertPositionLWW(ctx context.Context, p store.ReadingPosition) (store.ReadingPosition, error)
	InsertBook(ctx context.Context, b store.Book) (store.Book, error)
	SaveBlobs(fileHash string, epub, cover []byte) (filePath, coverPath string, err error)
	ListDeliveriesByBook(ctx context.Context, bookID int64) ([]store.Delivery, error)

	// Book lifecycle (LYCM-66): stable-identity folder ingest + delete.
	GetBookBySourcePath(ctx context.Context, sourcePath string) (store.Book, error)
	SetBookSourcePath(ctx context.Context, id int64, sourcePath string) error
	UpdateBookContent(ctx context.Context, id int64, b store.Book) (store.Book, error)
	SetBookFinished(ctx context.Context, id int64, finished bool) (store.Book, error)
	DeleteBook(ctx context.Context, id int64) (store.Book, error)
	RemoveBlobs(filePath string) error

	// Inventory (LYCM-601): ownership/acquisition state keyed by ISBN.
	UpsertInventory(ctx context.Context, inv store.Inventory) (store.Inventory, error)
	SetInventoryState(ctx context.Context, isbn, state string) (store.Inventory, error)
	LinkBookToInventory(ctx context.Context, isbn string, bookID int64, title, author string) (store.Inventory, error)
	ListInventory(ctx context.Context) ([]store.Inventory, error)
}

// API bundles the dependencies the handlers need.
type API struct {
	store    Store
	dataDir  string
	auth     *TokenAuth       // bearer-token table for the /eidolon + delivery routes
	delivery *deliveryConfig  // "Send to Kindle" dispatcher + policy (nil when unconfigured)
	acquirer Acquirer         // ISBN -> DRM-free copy requester (logging no-op by default)
	covers   coverart.Fetcher // ISBN -> canonical cover art (nil = use embedded covers only)
}

// Option configures an API at construction time.
type Option func(*API)

// WithAuth installs the bearer-token table guarding the ecosystem hooks
// (/eidolon/*) and the send-to-kindle route. Without it those routes are
// closed (every request 401s); core reader routes are unaffected.
func WithAuth(auth *TokenAuth) Option {
	return func(a *API) { a.auth = auth }
}

// WithCoverFetcher installs a source of canonical cover art (e.g. Open Library)
// consulted at ingest. When set, a freshly-ingested book with a usable ISBN
// prefers the fetched cover over its embedded one, falling back to the embedded
// cover when no art is found. Without it, ingest uses embedded covers only.
func WithCoverFetcher(f coverart.Fetcher) Option {
	return func(a *API) { a.covers = f }
}

// New builds an API over the given store. dataDir is retained for symmetry with
// the store's blob layout; the handlers serve whatever absolute or relative
// paths the book rows carry, so it is informational only.
func New(s Store, dataDir string, opts ...Option) *API {
	a := &API{store: s, dataDir: dataDir, acquirer: logAcquirer{}}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Handler returns a ServeMux wired with the library and blob routes. Callers
// mount it (it does not register /healthz, which main.go owns).
func (a *API) Handler() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /upload", a.handleUpload)
	mux.HandleFunc("GET /library", a.handleLibrary)

	// Inventory (LYCM-601): the scan/capture surface LYCM-602 feeds. Open like
	// /upload and /library — the personal read/write core, not the scoped
	// ecosystem hooks.
	mux.HandleFunc("POST /inventory", a.handleInventoryCreate)
	mux.HandleFunc("GET /inventory", a.handleInventoryList)

	mux.HandleFunc("PUT /sync", a.handleSyncPut)
	mux.HandleFunc("GET /sync", a.handleSyncGet)
	mux.HandleFunc("GET /books/{id}", a.handleGetBook)
	mux.HandleFunc("GET /books/{id}/cover", a.handleCover)
	mux.HandleFunc("GET /books/{id}/file", a.handleFile)
	mux.HandleFunc("DELETE /books/{id}", a.handleDelete)
	mux.HandleFunc("PUT /books/{id}/finished", a.handleSetFinished)

	// "Send to Kindle" (LYCM-402). Both routes require the delivery:send scope.
	mux.HandleFunc("POST /books/{id}/send-to-kindle", a.requireScope(ScopeDeliverySend, a.handleSendToKindle))
	mux.HandleFunc("GET /books/{id}/deliveries", a.requireScope(ScopeDeliverySend, a.handleListDeliveries))

	// Project Eidolon hooks (LYCM-403/404). Read-only; require eidolon:read.
	mux.HandleFunc("GET /eidolon/books/{id}/location", a.requireScope(ScopeEidolonRead, a.handleEidolonLocation))
	mux.HandleFunc("GET /eidolon/books/{id}/chapter", a.requireScope(ScopeEidolonRead, a.handleEidolonChapter))
	return mux
}

// bookJSON is the wire shape for a single library entry.
type bookJSON struct {
	ID       int64    `json:"id"`
	Title    string   `json:"title"`
	Author   string   `json:"author"`
	CoverURL string   `json:"cover_url"`
	Progress *float64 `json:"progress,omitempty"`
	// AddedAt (RFC3339) backs the "recently added" library sort. Series and
	// SeriesIndex drive series roll-up in the library grid; both are omitted for
	// standalone books (LYCM-36 / LYCM-62).
	AddedAt     string   `json:"added_at"`
	Series      string   `json:"series,omitempty"`
	SeriesIndex *float64 `json:"series_index,omitempty"`
	// ReadAt (RFC3339) is when the book's latest reading position was recorded;
	// it lets the client pin the most-recently-read book to the top of the
	// shelf. Omitted when the book has never been opened.
	ReadAt string `json:"read_at,omitempty"`
	// Finished is true when the book has been explicitly marked read, regardless
	// of reading progress (LYCM mark-as-read).
	Finished bool `json:"finished,omitempty"`
}

func coverURL(id int64) string { return fmt.Sprintf("/books/%d/cover", id) }

// bookJSONFor assembles the wire shape for one book, folding in its cover URL,
// series fields, latest reading position, and finished state.
func (a *API) bookJSONFor(ctx context.Context, b store.Book) (bookJSON, error) {
	entry := bookJSON{
		ID:       b.ID,
		Title:    b.Title,
		Author:   b.Author,
		AddedAt:  b.AddedAt.UTC().Format(time.RFC3339),
		Series:   b.Series,
		Finished: b.FinishedAt != nil,
	}
	if b.CoverPath != "" {
		entry.CoverURL = coverURL(b.ID)
	}
	if b.SeriesIndex != 0 {
		idx := b.SeriesIndex
		entry.SeriesIndex = &idx
	}
	if pos, err := a.store.GetLatestPosition(ctx, b.ID); err == nil {
		p := pos.Progress
		entry.Progress = &p
		entry.ReadAt = pos.UpdatedAt.UTC().Format(time.RFC3339)
	} else if !errors.Is(err, store.ErrNotFound) {
		return bookJSON{}, err
	}
	return entry, nil
}

func (a *API) handleLibrary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	books, err := a.store.ListBooks(ctx)
	if err != nil {
		serverError(w, "list books", err)
		return
	}

	out := make([]bookJSON, 0, len(books))
	for _, b := range books {
		entry, err := a.bookJSONFor(ctx, b)
		if err != nil {
			serverError(w, "build book json", err)
			return
		}
		out = append(out, entry)
	}

	writeJSON(w, http.StatusOK, out)
}

// handleGetBook returns a single book's wire shape (used by the reader to read
// finished/progress state without loading the whole library).
func (a *API) handleGetBook(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid book id", http.StatusBadRequest)
		return
	}
	b, err := a.store.GetBook(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "book not found", http.StatusNotFound)
		return
	case err != nil:
		serverError(w, "get book", err)
		return
	}
	entry, err := a.bookJSONFor(r.Context(), b)
	if err != nil {
		serverError(w, "build book json", err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (a *API) handleCover(w http.ResponseWriter, r *http.Request) {
	b, ok := a.lookupBook(w, r)
	if !ok {
		return
	}
	if b.CoverPath == "" {
		http.Error(w, "no cover", http.StatusNotFound)
		return
	}
	// Covers are content-addressed and effectively immutable, so they can be
	// cached aggressively. Content-Type is sniffed since the blob is stored
	// extensionless (cover.bin).
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	serveBlob(w, r, b.CoverPath, "")
}

func (a *API) handleFile(w http.ResponseWriter, r *http.Request) {
	b, ok := a.lookupBook(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fmt.Sprintf("book-%d.epub", b.ID)))
	serveBlob(w, r, b.FilePath, "application/epub+zip")
}

// handleDelete removes a book and its on-disk blobs (LYCM-66). It responds 204
// on success, 404 if no book has the id. Dependent rows are handled by the
// schema FKs (reading_positions/deliveries cascade, inventory link nulled), so
// this is safe without an explicit cleanup pass.
// handleSetFinished marks a book read or unread. Body: {"finished": bool}.
func (a *API) handleSetFinished(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid book id", http.StatusBadRequest)
		return
	}
	var req struct {
		Finished bool `json:"finished"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	switch _, err := a.store.SetBookFinished(r.Context(), id, req.Finished); {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "book not found", http.StatusNotFound)
		return
	case err != nil:
		serverError(w, "set finished", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid book id", http.StatusBadRequest)
		return
	}
	deleted, err := a.store.DeleteBook(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "book not found", http.StatusNotFound)
		return
	case err != nil:
		serverError(w, "delete book", err)
		return
	}
	// The row is gone; a leftover blob dir is only wasted disk, so a cleanup
	// failure is logged, not surfaced as an error to the caller.
	if err := a.store.RemoveBlobs(deleted.FilePath); err != nil {
		log.Printf("api: delete book %d: remove blobs: %v", deleted.ID, err)
	}
	w.WriteHeader(http.StatusNoContent)
}

// lookupBook parses the {id} path value and loads the book, writing the
// appropriate 4xx/5xx response and returning ok=false on any failure.
func (a *API) lookupBook(w http.ResponseWriter, r *http.Request) (store.Book, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid book id", http.StatusBadRequest)
		return store.Book{}, false
	}
	b, err := a.store.GetBook(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "book not found", http.StatusNotFound)
		return store.Book{}, false
	}
	if err != nil {
		serverError(w, "get book", err)
		return store.Book{}, false
	}
	return b, true
}

// serveBlob streams the file at path. If contentType is non-empty it is used
// verbatim; otherwise http.ServeContent sniffs it from the bytes. ServeContent
// also gives us Range support, Last-Modified and conditional requests for free.
func serveBlob(w http.ResponseWriter, r *http.Request, path, contentType string) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "blob missing", http.StatusNotFound)
			return
		}
		serverError(w, "open blob", err)
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		serverError(w, "stat blob", err)
		return
	}
	if contentType == "" {
		// Blobs are stored extensionless (e.g. cover.bin), so let ServeContent's
		// extension-based guess fall through to content sniffing.
		var err error
		if contentType, err = sniffContentType(f); err != nil {
			serverError(w, "sniff content type", err)
			return
		}
	}
	w.Header().Set("Content-Type", contentType)
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

// sniffContentType reads the leading bytes of f to detect its media type, then
// rewinds f so the subsequent ServeContent streams from the start.
func sniffContentType(f *os.File) (string, error) {
	var buf [512]byte
	n, err := f.Read(buf[:])
	if err != nil && err != io.EOF {
		return "", err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	return http.DetectContentType(buf[:n]), nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("api: encode response: %v", err)
	}
}

func serverError(w http.ResponseWriter, what string, err error) {
	log.Printf("api: %s: %v", what, err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}
