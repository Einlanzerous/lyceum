package api

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/magos/lyceum/internal/coverart"
	"github.com/magos/lyceum/internal/epub"
)

// fakeFetcher is a coverart.Fetcher stub returning canned results and recording
// the query it was asked for.
type fakeFetcher struct {
	data      []byte
	err       error
	asked     coverart.Query
	callCount int
}

func (f *fakeFetcher) Fetch(_ context.Context, q coverart.Query) ([]byte, string, error) {
	f.callCount++
	f.asked = q
	if f.err != nil {
		return nil, "", f.err
	}
	return f.data, "image/jpeg", nil
}

// coverForIngest only fetches when the EPUB has no embedded cover, so these
// cases split on whether CoverData is present.
func TestCoverForIngest(t *testing.T) {
	embedded := []byte("embedded-cover-bytes")
	fetched := []byte("fetched-cover-bytes")
	// A present embedded cover, with a UUID ahead of the ISBN (as real EPUBs do).
	withCover := &epub.Metadata{
		CoverData:   embedded,
		Identifiers: []string{"urn:uuid:x", "9781101543290"},
	}
	// No embedded cover — the gap the fetch is meant to fill.
	noCover := &epub.Metadata{
		Title:       "Prince of Thorns",
		Author:      "Mark Lawrence",
		Identifiers: []string{"urn:uuid:x", "9781101543290"},
	}
	ctx := context.Background()

	t.Run("no fetcher configured keeps embedded", func(t *testing.T) {
		a := &API{}
		if got := a.coverForIngest(ctx, withCover); !bytes.Equal(got, embedded) {
			t.Fatalf("got %q, want embedded", got)
		}
	})

	t.Run("present embedded cover is kept without fetching", func(t *testing.T) {
		f := &fakeFetcher{data: fetched}
		a := &API{covers: f}
		if got := a.coverForIngest(ctx, withCover); !bytes.Equal(got, embedded) {
			t.Fatalf("got %q, want embedded (external art must not replace a present cover)", got)
		}
		if f.callCount != 0 {
			t.Fatalf("fetcher called %d times for a book that already has a cover; want 0", f.callCount)
		}
	})

	t.Run("missing cover is filled from fetched art", func(t *testing.T) {
		f := &fakeFetcher{data: fetched}
		a := &API{covers: f}
		got := a.coverForIngest(ctx, noCover)
		if !bytes.Equal(got, fetched) {
			t.Fatalf("got %q, want fetched", got)
		}
		if len(f.asked.ISBNs) != 1 || f.asked.ISBNs[0] != "9781101543290" {
			t.Fatalf("fetched ISBNs = %v, want [9781101543290] (the ISBN, not the UUID)", f.asked.ISBNs)
		}
	})

	t.Run("missing cover with ErrNotFound stays coverless", func(t *testing.T) {
		a := &API{covers: &fakeFetcher{err: coverart.ErrNotFound}}
		if got := a.coverForIngest(ctx, noCover); len(got) != 0 {
			t.Fatalf("got %d bytes, want none on ErrNotFound", len(got))
		}
	})

	t.Run("missing cover with fetch error stays coverless", func(t *testing.T) {
		a := &API{covers: &fakeFetcher{err: errors.New("network down")}}
		if got := a.coverForIngest(ctx, noCover); len(got) != 0 {
			t.Fatalf("got %d bytes, want none on error", len(got))
		}
	})

	t.Run("no cover and nothing to key on skips fetch entirely", func(t *testing.T) {
		f := &fakeFetcher{data: fetched}
		a := &API{covers: f}
		bare := &epub.Metadata{Identifiers: []string{"urn:uuid:x"}}
		if got := a.coverForIngest(ctx, bare); len(got) != 0 {
			t.Fatalf("got %d bytes, want none", len(got))
		}
		if f.callCount != 0 {
			t.Fatalf("fetcher called %d times with nothing to key on; want 0", f.callCount)
		}
	})

	// A real square PNG with a white frame — the case cover normalization exists
	// for. Built once, reused by the two wiring cases below.
	framedPNG := whiteFramedPNG(t, 320, 320, 60)

	t.Run("normalization on rewrites the stored cover", func(t *testing.T) {
		a := &API{normalizeCovers: true}
		got := a.coverForIngest(ctx, &epub.Metadata{CoverData: framedPNG})
		if bytes.Equal(got, framedPNG) {
			t.Fatalf("cover was stored verbatim; normalization should have rewritten it")
		}
		img, err := jpeg.Decode(bytes.NewReader(got))
		if err != nil {
			t.Fatalf("normalized cover is not JPEG: %v", err)
		}
		if a := float64(img.Bounds().Dx()) / float64(img.Bounds().Dy()); a < 0.55 || a > 0.67 {
			t.Fatalf("normalized aspect = %.3f, want ~0.61", a)
		}
	})

	t.Run("normalization off stores the cover verbatim", func(t *testing.T) {
		a := &API{} // normalizeCovers zero value == false
		if got := a.coverForIngest(ctx, &epub.Metadata{CoverData: framedPNG}); !bytes.Equal(got, framedPNG) {
			t.Fatalf("cover was altered with normalization disabled")
		}
	})

	t.Run("missing cover with title but no ISBN still searches", func(t *testing.T) {
		f := &fakeFetcher{data: fetched}
		a := &API{covers: f}
		titled := &epub.Metadata{
			Title:       "Dragons of Spring Dawning",
			Author:      "Margaret Weis",
			Identifiers: []string{"d87e3b24-2a62-4783-b562-f5ef38d4f3a3"},
		}
		if got := a.coverForIngest(ctx, titled); !bytes.Equal(got, fetched) {
			t.Fatalf("got %q, want fetched via search", got)
		}
		if f.callCount != 1 || f.asked.Title != "Dragons of Spring Dawning" {
			t.Fatalf("fetch calls=%d title=%q; want one call keyed on the title", f.callCount, f.asked.Title)
		}
	})
}

// whiteFramedPNG builds a w×h PNG that is a mid-tone art block inside a white
// frame `border` px wide — a stand-in for the framed embedded covers that
// normalization is meant to clean.
func whiteFramedPNG(t *testing.T, w, h, border int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	art := color.NRGBA{R: 120, G: 90, B: 60, A: 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := white
			if x >= border && x < w-border && y >= border && y < h-border {
				c = art
			}
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}
