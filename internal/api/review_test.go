package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/magos/lyceum/internal/coverart"
	"github.com/magos/lyceum/internal/store"
)

// solidPNG builds a clean solid w×h PNG cover.
func solidPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	fill := color.NRGBA{R: 120, G: 90, B: 60, A: 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, fill)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func getJSON(t *testing.T, url string, out any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}

func postExpect(t *testing.T, url string, want int) {
	t.Helper()
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != want {
		t.Fatalf("POST %s: status %d, want %d", url, resp.StatusCode, want)
	}
}

// TestIngestQCHoldsFlaggedThenApprove exercises the LYCM-58 loop: a mangled,
// coverless, ISBN-less ingest is held off the shelf, surfaces in the review
// queue with its flags, can be edited, and is published on approve.
func TestIngestQCHoldsFlaggedThenApprove(t *testing.T) {
	s := testStore(t)
	// No cover available, so the coverless EPUB stays coverless → flagged.
	a := New(s, "", WithCoverFetcher(&fakeFetcher{err: coverart.ErrNotFound}), WithIngestQC(true))
	ctx := context.Background()

	data := epubWithIdentifier(t, "Novel - Dragonlance - Chronicles 03", "urn:uuid:xyz")
	book, result, err := a.ingestEPUB(ctx, data, "chron.epub", "")
	if err != nil || result != ingestCreated {
		t.Fatalf("ingest: result=%v err=%v", result, err)
	}
	if book.ReviewState != store.ReviewPending {
		t.Fatalf("review_state = %q, want pending", book.ReviewState)
	}

	srv := httptest.NewServer(a.Handler())
	defer srv.Close()

	var shelf []bookJSON
	getJSON(t, srv.URL+"/library", &shelf)
	if len(shelf) != 0 {
		t.Fatalf("shelf = %+v, want the flagged book hidden", shelf)
	}

	var queue []bookJSON
	getJSON(t, srv.URL+"/ingest/review", &queue)
	if len(queue) != 1 || queue[0].ID != book.ID {
		t.Fatalf("review queue = %+v, want the flagged book", queue)
	}
	if queue[0].ReviewState != store.ReviewPending {
		t.Fatalf("queue entry review_state = %q, want pending", queue[0].ReviewState)
	}
	for _, want := range []string{"no_isbn", "no_cover", "suspicious_title"} {
		if !slices.Contains(queue[0].ReviewFlags, want) {
			t.Fatalf("flags = %v, missing %s", queue[0].ReviewFlags, want)
		}
	}

	// Edit the mangled metadata.
	id := strconv.FormatInt(book.ID, 10)
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/books/"+id,
		bytes.NewReader([]byte(`{"title":"Dragons of Spring Dawning","author":"Weis & Hickman"}`)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	var edited bookJSON
	if err := json.NewDecoder(resp.Body).Decode(&edited); err != nil {
		t.Fatalf("decode PATCH: %v", err)
	}
	_ = resp.Body.Close()
	if edited.Title != "Dragons of Spring Dawning" || edited.Author != "Weis & Hickman" {
		t.Fatalf("edited = %q / %q, want the corrected metadata", edited.Title, edited.Author)
	}

	// Approve → published, on the shelf, out of the queue.
	postExpect(t, srv.URL+"/books/"+id+"/approve", http.StatusOK)

	getJSON(t, srv.URL+"/library", &shelf)
	if len(shelf) != 1 || shelf[0].ID != book.ID {
		t.Fatalf("shelf after approve = %+v, want the approved book", shelf)
	}
	getJSON(t, srv.URL+"/ingest/review", &queue)
	if len(queue) != 0 {
		t.Fatalf("review queue after approve = %+v, want empty", queue)
	}
}

// TestIngestQCCleanPublishesStraightThrough: an ISBN'd book with a good fetched
// cover and a sane title trips no detector and never enters the queue.
func TestIngestQCCleanPublishesStraightThrough(t *testing.T) {
	s := testStore(t)
	a := New(s, "", WithCoverFetcher(&fakeFetcher{data: solidPNG(t, 366, 600)}), WithIngestQC(true))
	ctx := context.Background()

	data := epubWithIdentifier(t, "The Iliad", "urn:isbn:9780140447941")
	book, result, err := a.ingestEPUB(ctx, data, "iliad.epub", "")
	if err != nil || result != ingestCreated {
		t.Fatalf("ingest: result=%v err=%v", result, err)
	}
	if book.ReviewState != store.ReviewPublished {
		t.Fatalf("clean book review_state = %q (flags via ingest), want published", book.ReviewState)
	}

	srv := httptest.NewServer(a.Handler())
	defer srv.Close()

	var shelf, queue []bookJSON
	getJSON(t, srv.URL+"/library", &shelf)
	getJSON(t, srv.URL+"/ingest/review", &queue)
	if len(shelf) != 1 || len(queue) != 0 {
		t.Fatalf("clean book: shelf=%d queue=%d, want 1/0", len(shelf), len(queue))
	}
}

// TestReplaceCoverUpload replaces a book's cover via multipart upload and
// confirms the stored bytes are the normalized JPEG.
func TestReplaceCoverUpload(t *testing.T) {
	s := testStore(t)
	a := New(s, "", WithIngestQC(true))
	ctx := context.Background()

	// Coverless ingest → pending, no cover yet.
	data := epubWithIdentifier(t, "A Coverless Book", "urn:uuid:no-cover")
	book, _, err := a.ingestEPUB(ctx, data, "x.epub", "")
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	srv := httptest.NewServer(a.Handler())
	defer srv.Close()
	id := strconv.FormatInt(book.ID, 10)

	// POST a replacement cover as multipart/form-data field "file".
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "cover.png")
	_, _ = fw.Write(solidPNG(t, 400, 640))
	_ = mw.Close()

	resp, err := http.Post(srv.URL+"/books/"+id+"/cover", mw.FormDataContentType(), &body)
	if err != nil {
		t.Fatalf("POST cover: %v", err)
	}
	var updated bookJSON
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	_ = resp.Body.Close()
	if updated.CoverURL == "" {
		t.Fatalf("cover url empty after upload: %+v", updated)
	}

	// The served cover must be the normalized JPEG.
	cresp, err := http.Get(srv.URL + updated.CoverURL)
	if err != nil {
		t.Fatalf("GET cover: %v", err)
	}
	defer func() { _ = cresp.Body.Close() }()
	raw, _ := io.ReadAll(cresp.Body)
	if _, err := jpeg.Decode(bytes.NewReader(raw)); err != nil {
		t.Fatalf("stored cover is not normalized JPEG: %v", err)
	}
}

// TestRefetchCover covers the re-fetch endpoint's outcomes.
func TestRefetchCover(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mkBook := func(a *API) string {
		data := epubWithIdentifier(t, "Refetch Me", "urn:uuid:refetch")
		b, _, err := a.ingestEPUB(ctx, data, "r.epub", "")
		if err != nil {
			t.Fatalf("ingest: %v", err)
		}
		return strconv.FormatInt(b.ID, 10)
	}

	t.Run("no fetcher configured → 503", func(t *testing.T) {
		a := New(s, "", WithIngestQC(true))
		id := mkBook(a)
		srv := httptest.NewServer(a.Handler())
		defer srv.Close()
		postExpect(t, srv.URL+"/books/"+id+"/cover/refetch", http.StatusServiceUnavailable)
	})

	t.Run("source has nothing → 404", func(t *testing.T) {
		a := New(s, "", WithCoverFetcher(&fakeFetcher{err: coverart.ErrNotFound}))
		id := mkBook(a)
		srv := httptest.NewServer(a.Handler())
		defer srv.Close()
		postExpect(t, srv.URL+"/books/"+id+"/cover/refetch", http.StatusNotFound)
	})

	t.Run("found → 200 and cover stored", func(t *testing.T) {
		a := New(s, "", WithCoverFetcher(&fakeFetcher{data: solidPNG(t, 366, 600)}))
		id := mkBook(a)
		srv := httptest.NewServer(a.Handler())
		defer srv.Close()
		resp, err := http.Post(srv.URL+"/books/"+id+"/cover/refetch", "application/json", nil)
		if err != nil {
			t.Fatalf("POST refetch: %v", err)
		}
		var got bookJSON
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || got.CoverURL == "" {
			t.Fatalf("refetch = status %d cover %q, want 200 with a cover", resp.StatusCode, got.CoverURL)
		}
	})
}

// TestReviewQueueReflectsAMarkedPendingBook documents why the review queue still
// reads the caller's finished set in bulk (LYCM-115) rather than skipping the
// lookup on the grounds that a pending book cannot be marked read.
//
// Nothing on the server enforces that. PUT /books/{id}/finished resolves the
// book by id alone, and SetBookFinished sources its row from `books WHERE id =
// $1` with no review_state filter, so a pending id is markable by any client
// that has one. Only the UI makes it unreachable, by never listing pending books
// on the shelf. Assuming the answer instead of asking would make the queue
// disagree with GET /books/{id} about the same book.
func TestReviewQueueReflectsAMarkedPendingBook(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	filePath, _, err := s.SaveBlobs("pending-finish-hash", epubBytes, nil)
	if err != nil {
		t.Fatalf("SaveBlobs: %v", err)
	}
	pending, err := s.InsertBook(ctx, store.Book{
		Title:       "Held For Review",
		FilePath:    filePath,
		FileHash:    "pending-finish-hash",
		SizeBytes:   int64(len(epubBytes)),
		ReviewState: store.ReviewPending,
		ReviewFlags: []string{"no_isbn"},
	})
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}
	srv := newServer(t, s)

	req, _ := http.NewRequest(http.MethodPut,
		fmt.Sprintf("%s/books/%d/finished", srv.URL, pending.ID),
		strings.NewReader(`{"finished":true}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT finished: %v", err)
	}
	resp.Body.Close()
	// Not a Skip: this is the only test justifying the review queue's finished
	// lookup, and skipping here would turn it green precisely when the premise it
	// guards against changed.
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT finished on a pending book = %d, want 204; if the server now "+
			"refuses this, the review queue can stop asking (see handleReviewList)", resp.StatusCode)
	}

	var queue []bookJSON
	getJSON(t, srv.URL+"/ingest/review", &queue)
	if len(queue) != 1 || queue[0].ID != pending.ID {
		t.Fatalf("review queue = %+v, want the pending book", queue)
	}
	if !queue[0].Finished {
		t.Error("review queue reports the book unread, but it was just marked read")
	}

	var single bookJSON
	getJSON(t, fmt.Sprintf("%s/books/%d", srv.URL, pending.ID), &single)
	if single.Finished != queue[0].Finished {
		t.Errorf("GET /books/%d says finished=%v, review queue says %v; the two disagree",
			pending.ID, single.Finished, queue[0].Finished)
	}
}

// PATCH /books/{id} sets, keeps and clears a book's series (LYCM-129): the
// only other sources are the EPUB's own metadata and the batch-confirm intent,
// so a grabbed EPUB with none stayed series-less with no way to fix it.
func TestUpdateBookSeries(t *testing.T) {
	s := testStore(t)
	srv := httptest.NewServer(New(s, "").Handler())
	t.Cleanup(srv.Close)
	book := seedBook(t, s, "series-edit", "The Final Empire", "Brandon Sanderson", nil)
	url := srv.URL + "/books/" + strconv.FormatInt(book.ID, 10)

	patch := func(body string) (bookJSON, int) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPatch, url, bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("PATCH: %v", err)
		}
		defer resp.Body.Close()
		var got bookJSON
		if resp.StatusCode == http.StatusOK {
			if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
				t.Fatalf("decode PATCH: %v", err)
			}
		}
		return got, resp.StatusCode
	}

	got, code := patch(`{"title":"The Final Empire","author":"Brandon Sanderson","series":" Mistborn ","series_index":1}`)
	if code != http.StatusOK || got.Series != "Mistborn" || got.SeriesIndex == nil || *got.SeriesIndex != 1 {
		t.Fatalf("set series = %d %+v, want 200 Mistborn #1", code, got)
	}

	// Title/author-only edits (the review queue's form before this change)
	// leave the series alone.
	got, _ = patch(`{"title":"Mistborn: The Final Empire","author":"Brandon Sanderson"}`)
	if got.Title != "Mistborn: The Final Empire" || got.Series != "Mistborn" || got.SeriesIndex == nil {
		t.Fatalf("title-only patch = %+v, want the series kept", got)
	}

	// The index alone moves the book within its series.
	got, _ = patch(`{"title":"Mistborn: The Final Empire","author":"Brandon Sanderson","series_index":1.5}`)
	if got.Series != "Mistborn" || got.SeriesIndex == nil || *got.SeriesIndex != 1.5 {
		t.Fatalf("index-only patch = %+v, want Mistborn #1.5", got)
	}

	if _, code := patch(`{"title":"X","author":"","series_index":-1}`); code != http.StatusBadRequest {
		t.Fatalf("negative series_index = %d, want 400", code)
	}

	// An empty series clears both.
	got, _ = patch(`{"title":"The Final Empire","author":"Brandon Sanderson","series":""}`)
	if got.Series != "" || got.SeriesIndex != nil {
		t.Fatalf("cleared = %+v, want no series and no index", got)
	}
	reloaded, err := s.GetBook(context.Background(), book.ID)
	if err != nil {
		t.Fatalf("GetBook: %v", err)
	}
	if reloaded.Series != "" || reloaded.SeriesIndex != 0 {
		t.Fatalf("stored after clear = %q #%v, want none", reloaded.Series, reloaded.SeriesIndex)
	}
}
