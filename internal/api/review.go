package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/magos/lyceum/internal/coverart"
	"github.com/magos/lyceum/internal/store"
)

// maxCoverUploadBytes caps a replacement cover upload (LYCM-58). Covers are
// small; this bounds memory for the read-into-RAM normalize pass.
const maxCoverUploadBytes = 10 << 20

// handleReviewList returns the ingest-QC review queue (LYCM-58): books held
// pending because ingest flagged an issue, newest first, each carrying its
// detected flags.
func (a *API) handleReviewList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	books, err := a.store.ListPendingBooks(ctx)
	if err != nil {
		serverError(w, "list pending books", err)
		return
	}
	// The review queue is empty most of the time — it holds a book only between
	// a flagged ingest and someone clearing it — and an empty queue needs no
	// reader state. Without this the common case sweeps a reader's entire
	// history to render nothing.
	st := readerState{}
	if len(books) > 0 {
		st, err = a.readerStateAll(ctx)
		if err != nil {
			serverError(w, "read reader state", err)
			return
		}
	}
	out := make([]bookJSON, 0, len(books))
	for _, b := range books {
		out = append(out, a.bookJSONFor(b, st))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleApprove publishes a pending book to the shelf (LYCM-58), returning the
// updated entry.
func (a *API) handleApprove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid book id", http.StatusBadRequest)
		return
	}
	b, err := a.store.ApproveBook(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "book not found", http.StatusNotFound)
		return
	case err != nil:
		serverError(w, "approve book", err)
		return
	}
	a.writeBook(w, r, b)
}

// handleUpdateBook edits a book's title/author (LYCM-58 override for mangled
// MOBI metadata). Body: {"title": "...", "author": "..."}. Title is required; it
// does not change review state.
func (a *API) handleUpdateBook(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid book id", http.StatusBadRequest)
		return
	}
	// Series and SeriesIndex are pointers so "not sent" (leave alone) is
	// distinct from "" / 0 (clear). A book's series otherwise comes only from
	// its EPUB or the batch-confirm intent, with no way to set or fix it after
	// ingest (LYCM-129).
	var req struct {
		Title       string   `json:"title"`
		Author      string   `json:"author"`
		Series      *string  `json:"series"`
		SeriesIndex *float64 `json:"series_index"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}
	if req.SeriesIndex != nil && !validSeriesIndex(*req.SeriesIndex) {
		http.Error(w, "series_index must be a number >= 0", http.StatusBadRequest)
		return
	}
	b, err := a.store.UpdateBookMeta(r.Context(), id, req.Title, req.Author)
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "book not found", http.StatusNotFound)
		return
	case err != nil:
		serverError(w, "update book", err)
		return
	}
	if req.Series != nil || req.SeriesIndex != nil {
		series, index := b.Series, b.SeriesIndex
		if req.Series != nil {
			series = strings.TrimSpace(*req.Series)
		}
		if req.SeriesIndex != nil {
			index = *req.SeriesIndex
		}
		if series == "" {
			index = 0 // a position without a series is meaningless
		}
		if b, err = a.store.UpdateBookSeries(r.Context(), id, series, index); err != nil {
			serverError(w, "update book series", err)
			return
		}
	}
	a.writeBook(w, r, b)
}

type linkInventoryRequest struct {
	InventoryID int64 `json:"inventory_id"`
}

type linkInventoryResponse struct {
	Book      bookJSON      `json:"book"`
	Inventory inventoryJSON `json:"inventory"`
}

// handleLinkInventory is the reviewer's "this held book is that wanted title"
// (LYCM-128): an EPUB that carried no ISBN — a converted file, some retail
// EPUBs — or one whose ISBN the resolver did not know has nothing for ingest
// to join on, so its wanted print entry stays wanted beside a fresh ingested
// row. This fulfils the chosen entry with the book, folds any entry the book
// already owned into it (store.FulfilInventory), and applies any series
// intent the entry carried (LYCM-82) — what an ISBN join would have done. The
// book's review state is not touched: linking says which title it is,
// approving says it belongs on the shelf.
func (a *API) handleLinkInventory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	book, ok := a.lookupBook(w, r)
	if !ok {
		return
	}
	var req linkInventoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InventoryID == 0 {
		http.Error(w, "inventory_id is required", http.StatusBadRequest)
		return
	}
	inv, err := a.store.FulfilInventory(ctx, req.InventoryID, book.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "inventory entry not found", http.StatusNotFound)
		return
	case errors.Is(err, store.ErrInventoryFulfilled):
		http.Error(w, "that title already has a book linked", http.StatusConflict)
		return
	case err != nil:
		serverError(w, "link inventory", err)
		return
	}
	if book.Series == "" && inv.Series != "" {
		a.applySeriesIntent(ctx, book.ID, inv.Series, inv.SeriesIndex)
		if fresh, err := a.store.GetBook(ctx, book.ID); err == nil {
			book = fresh
		}
	}
	st, err := a.readerStateOne(ctx, book.ID)
	if err != nil {
		serverError(w, "assemble book", err)
		return
	}
	writeJSON(w, http.StatusOK, linkInventoryResponse{
		Book:      a.bookJSONFor(book, st),
		Inventory: toInventoryJSON(inv),
	})
}

// validSeriesIndex is the one rule for a series position on every write path:
// 0 means "no position", anything else must be a finite non-negative number.
func validSeriesIndex(f float64) bool {
	return f >= 0 && !math.IsNaN(f) && !math.IsInf(f, 0)
}

// handleReplaceCover replaces a book's cover from an uploaded image (LYCM-58,
// multipart field "file"). The image is normalized (LYCM-65) before storage.
func (a *API) handleReplaceCover(w http.ResponseWriter, r *http.Request) {
	b, ok := a.lookupBook(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCoverUploadBytes)
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing multipart file field \"file\"", http.StatusBadRequest)
		return
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "could not read uploaded cover", http.StatusBadRequest)
		return
	}
	if len(data) == 0 {
		http.Error(w, "uploaded cover is empty", http.StatusBadRequest)
		return
	}

	updated, err := a.storeCover(r.Context(), b, data)
	if err != nil {
		serverError(w, "replace cover", err)
		return
	}
	a.writeBook(w, r, updated)
}

// handleRefetchCover re-fetches a book's cover from the configured art source
// (Apple Books by default, LYCM-56) keyed on its title/author, and stores the
// normalized result (LYCM-58).
func (a *API) handleRefetchCover(w http.ResponseWriter, r *http.Request) {
	b, ok := a.lookupBook(w, r)
	if !ok {
		return
	}
	if a.covers == nil {
		http.Error(w, "cover fetch is not configured", http.StatusServiceUnavailable)
		return
	}
	art, _, err := a.covers.Fetch(r.Context(), coverart.Query{Title: b.Title, Author: b.Author})
	switch {
	case errors.Is(err, coverart.ErrNotFound):
		http.Error(w, "no cover found for this book", http.StatusNotFound)
		return
	case err != nil:
		serverError(w, "refetch cover", err)
		return
	}
	updated, err := a.storeCover(r.Context(), b, art)
	if err != nil {
		serverError(w, "store cover", err)
		return
	}
	a.writeBook(w, r, updated)
}

// storeCover normalizes cover bytes and writes them to a book's served cover
// location — its recorded cover_path, else cover.bin alongside the EPUB blob (the
// same resolution the backfill tool uses) — recording the path when the book had
// none. It returns the refreshed book.
func (a *API) storeCover(ctx context.Context, b store.Book, art []byte) (store.Book, error) {
	art = a.normalizeCover(b.Title, art)

	coverPath := b.CoverPath
	if coverPath == "" {
		coverPath = filepath.Join(filepath.Dir(b.FilePath), "cover.bin")
	}
	if err := a.store.SaveCoverAt(coverPath, art); err != nil {
		return store.Book{}, err
	}
	if b.CoverPath == "" {
		if err := a.store.SetCoverPath(ctx, b.ID, coverPath); err != nil {
			return store.Book{}, err
		}
	}
	return a.store.GetBook(ctx, b.ID)
}

// writeBook renders a book as JSON (200), folding in its cover URL and review
// state via bookJSONFor.
func (a *API) writeBook(w http.ResponseWriter, r *http.Request, b store.Book) {
	st, err := a.readerStateOne(r.Context(), b.ID)
	if err != nil {
		serverError(w, "assemble book", err)
		return
	}
	writeJSON(w, http.StatusOK, a.bookJSONFor(b, st))
}
