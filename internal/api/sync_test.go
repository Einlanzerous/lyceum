package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/magos/lyceum/internal/store"
)

// putSync PUTs a sync body and returns the response.
func putSync(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, url+"/sync", bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /sync: %v", err)
	}
	return resp
}

// getSync GETs /sync for a book, optionally scoped to a device.
func getSync(t *testing.T, url string, bookID int64, deviceID string) *http.Response {
	t.Helper()
	q := url + "/sync?book_id=" + strconv.FormatInt(bookID, 10)
	if deviceID != "" {
		q += "&device_id=" + deviceID
	}
	resp, err := http.Get(q)
	if err != nil {
		t.Fatalf("GET /sync: %v", err)
	}
	return resp
}

func decodePosition(t *testing.T, resp *http.Response) positionJSON {
	t.Helper()
	defer resp.Body.Close()
	var p positionJSON
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatalf("decode position: %v", err)
	}
	return p
}

// TestSyncPutThenGet covers the primary path: device A PUTs a CFI, a device-
// scoped GET resolves it, and an unscoped GET (e.g. device B) sees A's latest
// position per the last-write-wins rule.
func TestSyncPutThenGet(t *testing.T) {
	s := testStore(t)
	book := seedBook(t, s, "sync-hash", "The Republic", "Plato", nil)
	srv := newServer(t, s)

	cfi := "epubcfi(/6/4[chap01]!/4/2/1:0)"
	t0 := time.Now().Add(-time.Minute).UTC().Truncate(time.Millisecond)

	resp := putSync(t, srv.URL, map[string]any{
		"book_id":    book.ID,
		"device_id":  "device-a",
		"cfi":        cfi,
		"progress":   0.25,
		"updated_at": t0,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", resp.StatusCode)
	}
	put := decodePosition(t, resp)
	if put.CFI != cfi || put.DeviceID != "device-a" || put.Progress != 0.25 {
		t.Fatalf("PUT echoed %+v", put)
	}

	// Device-scoped GET resolves device A's position.
	got := decodePosition(t, getSync(t, srv.URL, book.ID, "device-a"))
	if got.CFI != cfi || got.DeviceID != "device-a" {
		t.Fatalf("device GET = %+v, want cfi=%q device-a", got, cfi)
	}

	// Unscoped GET (what device B would issue) sees A's latest position.
	latest := decodePosition(t, getSync(t, srv.URL, book.ID, ""))
	if latest.DeviceID != "device-a" || latest.CFI != cfi {
		t.Fatalf("latest GET = %+v, want device-a cfi=%q", latest, cfi)
	}
}

// TestSyncScopedGetFallsBackToLatest is the literal LYCM-107 round-trip: device
// A PUTs a CFI, and device B (which has never synced this book) issues a GET
// scoped to its own device_id and still resolves A's position.
func TestSyncScopedGetFallsBackToLatest(t *testing.T) {
	s := testStore(t)
	book := seedBook(t, s, "xdev-hash", "Phaedrus", "Plato", nil)
	srv := newServer(t, s)

	cfi := "epubcfi(/6/4[chap01]!/4/2/1:0)"
	if resp := putSync(t, srv.URL, map[string]any{
		"book_id": book.ID, "device_id": "device-a",
		"cfi": cfi, "progress": 0.42,
		"updated_at": time.Now().UTC(),
	}); resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT a status = %d", resp.StatusCode)
	}

	resp := getSync(t, srv.URL, book.ID, "device-b")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("device-b scoped GET status = %d, want 200 (fallback to latest)", resp.StatusCode)
	}
	got := decodePosition(t, resp)
	if got.CFI != cfi || got.DeviceID != "device-a" {
		t.Fatalf("device-b GET = %+v, want A's cfi=%q device-a", got, cfi)
	}
}

// TestSyncFurthestWinsAcrossDevices verifies resume tracks the furthest read
// across devices, not the most recent write: a newer write at an earlier spot
// (e.g. a still-open reader flushing progress=0 on navigation) must not drag the
// resume point backward, and any device resumes at the furthest point.
func TestSyncFurthestWinsAcrossDevices(t *testing.T) {
	s := testStore(t)
	book := seedBook(t, s, "furthest-hash", "Meditations", "Marcus Aurelius", nil)
	srv := newServer(t, s)

	base := time.Now().UTC().Truncate(time.Millisecond)

	// Device A read furthest (0.8), earlier.
	if resp := putSync(t, srv.URL, map[string]any{
		"book_id": book.ID, "device_id": "device-a",
		"cfi": "epubcfi(/6/8!/12)", "progress": 0.8,
		"updated_at": base.Add(-time.Hour),
	}); resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT a status = %d", resp.StatusCode)
	}
	// Device B wrote LATER but at the start (0.1) — must not win.
	if resp := putSync(t, srv.URL, map[string]any{
		"book_id": book.ID, "device_id": "device-b",
		"cfi": "epubcfi(/6/2!/4)", "progress": 0.1,
		"updated_at": base,
	}); resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT b status = %d", resp.StatusCode)
	}

	// Unscoped GET resumes at the furthest read, not the latest write.
	latest := decodePosition(t, getSync(t, srv.URL, book.ID, ""))
	if latest.DeviceID != "device-a" || latest.Progress != 0.8 {
		t.Fatalf("unscoped GET = %+v, want device-a progress 0.8 (furthest)", latest)
	}

	// Device B, asking for its own resume, still gets the furthest read from A.
	b := decodePosition(t, getSync(t, srv.URL, book.ID, "device-b"))
	if b.DeviceID != "device-a" || b.Progress != 0.8 {
		t.Fatalf("device-b GET = %+v, want device-a progress 0.8 (furthest wins)", b)
	}
}

// TestSyncStaleWriteIsNoOp verifies last-write-wins within a single device: a
// PUT carrying an older updated_at must not clobber a newer stored position.
func TestSyncStaleWriteIsNoOp(t *testing.T) {
	s := testStore(t)
	book := seedBook(t, s, "stale-hash", "The Republic", "Plato", nil)
	srv := newServer(t, s)

	newCFI := "epubcfi(/6/10!/2)"
	newTime := time.Now().UTC().Truncate(time.Millisecond)

	if resp := putSync(t, srv.URL, map[string]any{
		"book_id": book.ID, "device_id": "device-a",
		"cfi": newCFI, "progress": 0.5, "updated_at": newTime,
	}); resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT new status = %d", resp.StatusCode)
	}

	// A stale write (older updated_at) must be ignored.
	stale := putSync(t, srv.URL, map[string]any{
		"book_id": book.ID, "device_id": "device-a",
		"cfi": "epubcfi(/6/2!/2)", "progress": 0.05,
		"updated_at": newTime.Add(-time.Hour),
	})
	if stale.StatusCode != http.StatusOK {
		t.Fatalf("stale PUT status = %d, want 200", stale.StatusCode)
	}
	echoed := decodePosition(t, stale)
	if echoed.CFI != newCFI || echoed.Progress != 0.5 {
		t.Fatalf("stale PUT should return the winning row, got %+v", echoed)
	}

	got := decodePosition(t, getSync(t, srv.URL, book.ID, "device-a"))
	if got.CFI != newCFI || got.Progress != 0.5 {
		t.Fatalf("stored position = %+v, want unchanged newer write", got)
	}
}

func TestSyncPutInvalidCFI(t *testing.T) {
	s := testStore(t)
	book := seedBook(t, s, "badcfi-hash", "The Republic", "Plato", nil)
	srv := newServer(t, s)

	resp := putSync(t, srv.URL, map[string]any{
		"book_id": book.ID, "device_id": "device-a",
		"cfi": "not-a-cfi", "progress": 0.3,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for invalid CFI", resp.StatusCode)
	}

	// Nothing should have been stored.
	if _, err := s.GetPosition(context.Background(), book.ID, ownerID(context.Background(), t, s), "device-a"); err != store.ErrNotFound {
		t.Fatalf("expected no stored position, got %v", err)
	}
}

func TestSyncPutValidationErrors(t *testing.T) {
	s := testStore(t)
	book := seedBook(t, s, "valerr-hash", "The Republic", "Plato", nil)
	srv := newServer(t, s)

	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing book_id", map[string]any{"device_id": "d", "cfi": "epubcfi(/6/2)", "progress": 0.1}},
		{"missing device_id", map[string]any{"book_id": book.ID, "cfi": "epubcfi(/6/2)", "progress": 0.1}},
		{"progress out of range", map[string]any{"book_id": book.ID, "device_id": "d", "cfi": "epubcfi(/6/2)", "progress": 1.5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := putSync(t, srv.URL, tc.body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}

func TestSyncPutDefaultsUpdatedAt(t *testing.T) {
	s := testStore(t)
	book := seedBook(t, s, "defaultts-hash", "The Republic", "Plato", nil)
	srv := newServer(t, s)

	before := time.Now().Add(-time.Second)
	resp := putSync(t, srv.URL, map[string]any{
		"book_id": book.ID, "device_id": "device-a",
		"cfi": "epubcfi(/6/4!/2)", "progress": 0.4,
	})
	pos := decodePosition(t, resp)
	if pos.UpdatedAt.Before(before) {
		t.Fatalf("updated_at = %v, expected server-stamped time after %v", pos.UpdatedAt, before)
	}
}

func TestSyncGetNotFound(t *testing.T) {
	s := testStore(t)
	book := seedBook(t, s, "missing-hash", "The Republic", "Plato", nil)
	srv := newServer(t, s)

	resp := getSync(t, srv.URL, book.ID, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unscoped GET status = %d, want 404", resp.StatusCode)
	}

	resp = getSync(t, srv.URL, book.ID, "ghost-device")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("device GET status = %d, want 404", resp.StatusCode)
	}
}

func TestSyncGetBadBookID(t *testing.T) {
	s := testStore(t)
	srv := newServer(t, s)

	resp, err := http.Get(srv.URL + "/sync?book_id=not-a-number")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
