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
}

// API bundles the dependencies the handlers need.
type API struct {
	store   Store
	dataDir string
}

// New builds an API over the given store. dataDir is retained for symmetry with
// the store's blob layout; the handlers serve whatever absolute or relative
// paths the book rows carry, so it is informational only.
func New(s Store, dataDir string) *API {
	return &API{store: s, dataDir: dataDir}
}

// Handler returns a ServeMux wired with the library and blob routes. Callers
// mount it (it does not register /healthz, which main.go owns).
func (a *API) Handler() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /upload", a.handleUpload)
	mux.HandleFunc("GET /library", a.handleLibrary)
	mux.HandleFunc("PUT /sync", a.handleSyncPut)
	mux.HandleFunc("GET /sync", a.handleSyncGet)
	mux.HandleFunc("GET /books/{id}/cover", a.handleCover)
	mux.HandleFunc("GET /books/{id}/file", a.handleFile)
	return mux
}

// bookJSON is the wire shape for a single library entry.
type bookJSON struct {
	ID       int64    `json:"id"`
	Title    string   `json:"title"`
	Author   string   `json:"author"`
	CoverURL string   `json:"cover_url"`
	Progress *float64 `json:"progress,omitempty"`
}

func coverURL(id int64) string { return fmt.Sprintf("/books/%d/cover", id) }

func (a *API) handleLibrary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	books, err := a.store.ListBooks(ctx)
	if err != nil {
		serverError(w, "list books", err)
		return
	}

	out := make([]bookJSON, 0, len(books))
	for _, b := range books {
		entry := bookJSON{
			ID:     b.ID,
			Title:  b.Title,
			Author: b.Author,
		}
		if b.CoverPath != "" {
			entry.CoverURL = coverURL(b.ID)
		}
		if pos, err := a.store.GetLatestPosition(ctx, b.ID); err == nil {
			p := pos.Progress
			entry.Progress = &p
		} else if !errors.Is(err, store.ErrNotFound) {
			serverError(w, "get latest position", err)
			return
		}
		out = append(out, entry)
	}

	writeJSON(w, http.StatusOK, out)
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
