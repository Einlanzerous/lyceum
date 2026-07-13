package api

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/magos/lyceum/internal/epub"
	"github.com/magos/lyceum/internal/store"
)

// syncRequest is the wire shape of a PUT /sync body: a single device's reading
// position within a book, keyed on an EPUB CFI. updated_at is the client's own
// timestamp for the position and drives conflict resolution (see handleSyncPut).
type syncRequest struct {
	BookID    int64     `json:"book_id"`
	DeviceID  string    `json:"device_id"`
	CFI       string    `json:"cfi"`
	Progress  float64   `json:"progress"`
	UpdatedAt time.Time `json:"updated_at"`
}

// positionJSON is the wire shape of a reading position returned by /sync.
type positionJSON struct {
	BookID    int64     `json:"book_id"`
	DeviceID  string    `json:"device_id"`
	CFI       string    `json:"cfi"`
	Progress  float64   `json:"progress"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toPositionJSON(p store.ReadingPosition) positionJSON {
	return positionJSON{
		BookID:    p.BookID,
		DeviceID:  p.DeviceID,
		CFI:       p.CFI,
		Progress:  p.Progress,
		UpdatedAt: p.UpdatedAt,
	}
}

// handleSyncPut upserts a device's reading position for a book.
//
// The CFI is validated structurally via the internal/epub CFI parser; an
// invalid CFI is rejected with 400 before any write.
//
// Conflict rule: last-write-wins by updated_at. Each device owns one row per
// book (the (book_id, device_id) pair is unique), so the only conflict is a
// device overwriting its own row. A write only takes effect when its
// updated_at is at least as new as the stored row's; a stale write (older
// updated_at) is a no-op and the existing row is returned unchanged. When the
// body omits updated_at it defaults to the server's current time, which always
// wins. Across devices, GET resolves "latest" by the same updated_at ordering.
func (a *API) handleSyncPut(w http.ResponseWriter, r *http.Request) {
	var req syncRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.BookID <= 0 {
		http.Error(w, "book_id is required", http.StatusBadRequest)
		return
	}
	if req.DeviceID == "" {
		http.Error(w, "device_id is required", http.StatusBadRequest)
		return
	}
	if err := epub.ValidateCFI(req.CFI); err != nil {
		http.Error(w, "invalid cfi: "+err.Error(), http.StatusBadRequest)
		return
	}
	if math.IsNaN(req.Progress) || math.IsInf(req.Progress, 0) || req.Progress < 0 || req.Progress > 1 {
		http.Error(w, "progress must be between 0 and 1", http.StatusBadRequest)
		return
	}
	if req.UpdatedAt.IsZero() {
		req.UpdatedAt = time.Now()
	}

	saved, err := a.store.UpsertPositionLWW(r.Context(), store.ReadingPosition{
		BookID:    req.BookID,
		UserID:    userFrom(r.Context()).ID,
		DeviceID:  req.DeviceID,
		CFI:       req.CFI,
		Progress:  req.Progress,
		UpdatedAt: req.UpdatedAt,
	})
	if err != nil {
		serverError(w, "upsert position", err)
		return
	}

	writeJSON(w, http.StatusOK, toPositionJSON(saved))
}

// handleSyncGet returns the reading position to resume from for a book: the
// furthest position across all devices. Reading further on any device advances
// the resume point everywhere, and a later write at an earlier spot (a stale or
// pre-pagination progress=0 flush) can't drag it backward. device_id is accepted
// for symmetry with PUT but does not scope the resume. A book with no stored
// position at all yields 404.
func (a *API) handleSyncGet(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	bookID, err := strconv.ParseInt(q.Get("book_id"), 10, 64)
	if err != nil || bookID <= 0 {
		http.Error(w, "book_id is required", http.StatusBadRequest)
		return
	}

	// Resume from the furthest position across the signed-in user's devices, not
	// this device's own bookmark: reading further on any of their devices advances
	// the resume point everywhere, and a stale/zero write can't drag it backward
	// (LYCM sync fix). device_id is still accepted (clients send it) but no longer
	// scopes resume. Housemates' positions are never consulted (LYCM-801).
	pos, err := a.store.GetFurthestPosition(r.Context(), bookID, userFrom(r.Context()).ID)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "no reading position", http.StatusNotFound)
		return
	}
	if err != nil {
		serverError(w, "get position", err)
		return
	}

	writeJSON(w, http.StatusOK, toPositionJSON(pos))
}
