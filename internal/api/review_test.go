package api

import (
	"bytes"
	"context"
	"encoding/json"
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
