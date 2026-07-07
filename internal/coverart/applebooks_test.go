package coverart

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// appleStub stands in for the iTunes Search API + artwork CDN.
type appleStub struct {
	results     []appleResult // returned by /search
	cover       []byte        // served for artwork requests
	searchTerm  string        // last term queried
	artworkReqs []string      // artwork paths requested (post-upscale)
}

func newApple(t *testing.T, s *appleStub) *AppleBooks {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/search":
			s.searchTerm = r.URL.Query().Get("term")
			if r.URL.Query().Get("entity") != "ebook" {
				t.Errorf("entity = %q, want ebook", r.URL.Query().Get("entity"))
			}
			_ = json.NewEncoder(w).Encode(appleSearchResponse{
				ResultCount: len(s.results),
				Results:     s.results,
			})
		case strings.HasPrefix(r.URL.Path, "/art/"):
			s.artworkReqs = append(s.artworkReqs, r.URL.Path)
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(s.cover)
		default:
			http.Error(w, "unexpected "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(srv.Close)
	// Rewrite result artwork URLs to point at this test server.
	for i := range s.results {
		if s.results[i].ArtworkURL != "" {
			s.results[i].ArtworkURL = srv.URL + s.results[i].ArtworkURL
		}
	}
	return &AppleBooks{SearchBaseURL: srv.URL, Country: "US", Client: srv.Client()}
}

func TestAppleFetch_ISBNMatchPreferred(t *testing.T) {
	cover := jpegBytes(4096)
	s := &appleStub{
		cover: cover,
		results: []appleResult{
			{TrackName: "War of the Twins (audiobook?)", ArtworkURL: "/art/9999999999999/100x100bb.jpg"},
			{TrackName: "War of the Twins", ArtworkURL: "/art/9780786954421/100x100bb.jpg"},
		},
	}
	f := newApple(t, s)

	got, mt, err := f.Fetch(context.Background(), Query{
		Title:  "War of the Twins",
		Author: "Margaret Weis",
		ISBNs:  []string{"9780786954421"},
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !bytes.Equal(got, cover) {
		t.Errorf("cover mismatch: got %d bytes", len(got))
	}
	if !strings.HasPrefix(mt, "image/") {
		t.Errorf("media type = %q", mt)
	}
	if s.searchTerm != "War of the Twins Margaret Weis" {
		t.Errorf("search term = %q, want title+author", s.searchTerm)
	}
	// The ISBN-carrying result should have been chosen and upscaled to 600.
	if len(s.artworkReqs) != 1 || !strings.Contains(s.artworkReqs[0], "9780786954421") {
		t.Fatalf("artwork requests = %v, want the ISBN edition", s.artworkReqs)
	}
	if !strings.Contains(s.artworkReqs[0], "600x600bb.jpg") {
		t.Errorf("artwork not upscaled: %s", s.artworkReqs[0])
	}
}

func TestAppleFetch_FirstResultWhenNoISBN(t *testing.T) {
	cover := jpegBytes(4096)
	s := &appleStub{
		cover: cover,
		results: []appleResult{
			{TrackName: "Dragons of Spring Dawning", ArtworkURL: "/art/aaa/100x100bb.jpg"},
			{TrackName: "Other", ArtworkURL: "/art/bbb/100x100bb.jpg"},
		},
	}
	f := newApple(t, s)

	if _, _, err := f.Fetch(context.Background(), Query{Title: "Dragons of Spring Dawning", Author: "Weis"}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(s.artworkReqs) != 1 || !strings.Contains(s.artworkReqs[0], "/art/aaa/") {
		t.Fatalf("artwork requests = %v, want the first result", s.artworkReqs)
	}
}

func TestAppleFetch_NoResults(t *testing.T) {
	f := newApple(t, &appleStub{cover: jpegBytes(4096)})
	if _, _, err := f.Fetch(context.Background(), Query{Title: "Nothing Here", Author: "Nobody"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestAppleFetch_EmptyQuery(t *testing.T) {
	f := newApple(t, &appleStub{cover: jpegBytes(4096)})
	if _, _, err := f.Fetch(context.Background(), Query{ISBNs: []string{"9780786954421"}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound for a titleless query", err)
	}
}

func TestUpscaleAppleArtwork(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://x/img/100x100bb.jpg", "https://x/img/600x600bb.jpg"},
		{"https://x/img/170x170bb.png", "https://x/img/600x600bb.png"},
		{"https://x/img/weird.jpg", "https://x/img/weird.jpg"}, // unchanged
	}
	for _, c := range cases {
		if got := upscaleAppleArtwork(c.in, 600); got != c.want {
			t.Errorf("upscale(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPickArtworkURL(t *testing.T) {
	rs := []appleResult{
		{ArtworkURL: "https://x/9990/100x100bb.jpg"},
		{ArtworkURL: "https://x/9780786954421/100x100bb.jpg"},
	}
	if got := pickArtworkURL(rs, []string{"9780786954421"}); !strings.Contains(got, "9780786954421") {
		t.Errorf("ISBN match not preferred: %q", got)
	}
	if got := pickArtworkURL(rs, nil); got != rs[0].ArtworkURL {
		t.Errorf("no-ISBN pick = %q, want first", got)
	}
	if got := pickArtworkURL(nil, nil); got != "" {
		t.Errorf("empty results = %q, want \"\"", got)
	}
}
