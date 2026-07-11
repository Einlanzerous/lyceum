package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/magos/lyceum/internal/isbn"
	"github.com/magos/lyceum/internal/store"
)

// Acquirer requests a DRM-free digital copy of an owned-but-not-yet-digital
// title, keyed by ISBN. It is the seam to the acquisition component (Bindery in
// the argosy-acquisition stack, LYCM-601): the default implementation only
// logs, so an entry is recorded as `wanted` without a live grab. A real
// Bindery-backed Acquirer is wired in via WithAcquirer once that stack is
// configured.
type Acquirer interface {
	Want(ctx context.Context, isbn string) error
}

// logAcquirer is the no-op default: it records intent in the log but performs
// no external grab. Want never errors, so requesting a digital copy always
// succeeds in moving the entry to `wanted`.
type logAcquirer struct{}

func (logAcquirer) Want(_ context.Context, code string) error {
	log.Printf("acquire: want ISBN %s (no acquirer configured; state only)", code)
	return nil
}

// WithAcquirer installs a real acquisition backend (e.g. a Bindery client) in
// place of the logging default.
func WithAcquirer(acq Acquirer) Option {
	return func(a *API) { a.acquirer = acq }
}

// inventoryJSON is the wire shape for an inventory entry.
type inventoryJSON struct {
	ID     int64  `json:"id"`
	ISBN   string `json:"isbn"`
	Title  string `json:"title,omitempty"`
	Author string `json:"author,omitempty"`
	State  string `json:"state"`
	BookID *int64 `json:"book_id,omitempty"`
}

func toInventoryJSON(inv store.Inventory) inventoryJSON {
	return inventoryJSON{
		ID:     inv.ID,
		ISBN:   inv.ISBN,
		Title:  inv.Title,
		Author: inv.Author,
		State:  inv.State,
		BookID: inv.BookID,
	}
}

// inventoryRequest is the POST /inventory body. ISBN is required (any form an
// ISBN-10/13 takes — hyphenated, urn:isbn:, etc.); FindDigital asks the
// acquisition pipeline for a DRM-free copy, moving the entry to `wanted`.
type inventoryRequest struct {
	ISBN        string `json:"isbn"`
	Title       string `json:"title"`
	Author      string `json:"author"`
	FindDigital bool   `json:"find_digital"`
}

// handleInventoryCreate is the capture endpoint a barcode scan (LYCM-602) calls:
// it normalizes the ISBN, records/finds the owned title, and — when
// find_digital is set and no EPUB is linked yet — hands the ISBN to the
// acquisition pipeline and marks it `wanted`. It returns the resulting entry.
func (a *API) handleInventoryCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req inventoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	code, err := isbn.Normalize(req.ISBN)
	if err != nil {
		http.Error(w, "invalid ISBN", http.StatusBadRequest)
		return
	}

	inv, err := a.store.UpsertInventory(ctx, store.Inventory{
		ISBN:   code,
		Title:  req.Title,
		Author: req.Author,
		WorkID: a.resolveWorkID(ctx, code), // group print/ebook editions (LYCM-35)
	})
	if err != nil {
		serverError(w, "upsert inventory", err)
		return
	}

	// Only request a digital copy when one isn't already in hand. A title that
	// is already ingested stays ingested.
	if req.FindDigital && inv.State != store.StateIngested {
		if err := a.acquirer.Want(ctx, code); err != nil {
			serverError(w, "acquire want", err)
			return
		}
		inv, err = a.store.SetInventoryState(ctx, code, store.StateWanted)
		if err != nil {
			serverError(w, "set inventory state", err)
			return
		}
	}

	writeJSON(w, http.StatusOK, toInventoryJSON(inv))
}

// handleInventoryList returns all inventory entries, most recently updated
// first.
func (a *API) handleInventoryList(w http.ResponseWriter, r *http.Request) {
	entries, err := a.store.ListInventory(r.Context())
	if err != nil {
		serverError(w, "list inventory", err)
		return
	}
	out := make([]inventoryJSON, 0, len(entries))
	for _, inv := range entries {
		out = append(out, toInventoryJSON(inv))
	}
	writeJSON(w, http.StatusOK, out)
}
