package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

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

const (
	// maxConcurrentWants caps in-flight background acquisition dispatches so a
	// big batch confirm doesn't hit the acquisition backend with N searches at
	// once (LYCM-79).
	maxConcurrentWants = 3
	// wantTimeout bounds one background dispatch. A live Bindery Want does a
	// lookup + add (a metadata pull each, ~15s cap apiece) — generous headroom
	// without letting a wedged dispatch pin a semaphore slot forever.
	wantTimeout = 2 * time.Minute
)

// dispatchWant hands an ISBN to the acquirer in the background, so the confirm
// request that recorded the inventory entry as `wanted` returns immediately
// instead of blocking on the acquisition backend's search (LYCM-79: a live
// batch confirm took ~80s synchronously). It deliberately uses a fresh
// background context, not the request's: the grab must survive the client
// disconnecting right after confirm. Failures are logged only — the entry
// already rests in `wanted`, which is exactly the state a failed grab should
// leave behind.
func (a *API) dispatchWant(code string) {
	a.wantWG.Add(1)
	go func() {
		defer a.wantWG.Done()
		a.wantSem <- struct{}{}
		defer func() { <-a.wantSem }()
		ctx, cancel := context.WithTimeout(context.Background(), wantTimeout)
		defer cancel()
		if err := a.acquirer.Want(ctx, code); err != nil {
			log.Printf("api: acquire want isbn=%s: %v (inventory stays wanted)", code, err)
		}
	}()
}

// waitWants blocks until every dispatched background acquisition has finished.
// Tests use it to assert on the acquirer after an async confirm returns.
func (a *API) waitWants() { a.wantWG.Wait() }

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
	// is already ingested stays ingested. The state is recorded first, then the
	// grab dispatches in the background (LYCM-79) — the response never waits on
	// the acquisition backend.
	if req.FindDigital && inv.State != store.StateIngested {
		inv, err = a.store.SetInventoryState(ctx, code, store.StateWanted)
		if err != nil {
			serverError(w, "set inventory state", err)
			return
		}
		a.dispatchWant(code)
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
