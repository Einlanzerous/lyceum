package coverart

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// jpegBytes is a payload large enough to clear minCoverBytes whose magic bytes
// make http content sniffing report image/jpeg.
func jpegBytes(n int) []byte {
	return append([]byte("\xff\xd8\xff\xe0"), bytes.Repeat([]byte{0x42}, n)...)
}

// olServer stands in for Open Library: a covers endpoint (/b/isbn, /b/id) and a
// search endpoint (/search.json). It records what it was asked.
type olServer struct {
	cover        []byte
	isbnHits     map[string]bool // ISBNs that have a direct cover
	searchCover  int             // cover_i returned by search (0 = no result)
	idCovers     map[int]bool    // cover ids that resolve
	searchedFor  string          // last title queried
	isbnRequests []string
}

func newOL(t *testing.T, s *olServer) *OpenLibrary {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/b/isbn/"):
			if r.URL.Query().Get("default") != "false" {
				t.Errorf("cover request missing default=false: %s", r.URL)
			}
			code := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/b/isbn/"), "-L.jpg")
			s.isbnRequests = append(s.isbnRequests, code)
			if s.isbnHits[code] {
				w.Header().Set("Content-Type", "image/jpeg")
				_, _ = w.Write(s.cover)
				return
			}
			http.NotFound(w, r)
		case strings.HasPrefix(r.URL.Path, "/b/id/"):
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(s.cover)
		case r.URL.Path == "/search.json":
			s.searchedFor = r.URL.Query().Get("title")
			if s.searchCover > 0 {
				_, _ = w.Write([]byte(`{"docs":[{"cover_i":` + itoa(s.searchCover) + `}]}`))
			} else {
				_, _ = w.Write([]byte(`{"docs":[]}`))
			}
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(srv.Close)
	return &OpenLibrary{CoversBaseURL: srv.URL, SearchBaseURL: srv.URL, Client: srv.Client()}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestFetch_ISBNDirectHit(t *testing.T) {
	cover := jpegBytes(4096)
	s := &olServer{cover: cover, isbnHits: map[string]bool{"9781101543290": true}}
	f := newOL(t, s)

	got, mt, err := f.Fetch(context.Background(), Query{ISBNs: []string{"9781101543290"}, Title: "Prince of Thorns"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !bytes.Equal(got, cover) {
		t.Errorf("cover mismatch: got %d bytes", len(got))
	}
	if !strings.HasPrefix(mt, "image/") {
		t.Errorf("media type = %q", mt)
	}
	if s.searchedFor != "" {
		t.Errorf("search was consulted despite an ISBN hit (searched %q)", s.searchedFor)
	}
}

func TestFetch_ISBNMissThenSearch(t *testing.T) {
	// The Prince of Thorns case: the EPUB's ISBN isn't in Open Library, but a
	// title+author search resolves the work's cover.
	cover := jpegBytes(4096)
	s := &olServer{
		cover:       cover,
		isbnHits:    map[string]bool{}, // no direct ISBN hit
		searchCover: 7603832,
	}
	f := newOL(t, s)

	got, _, err := f.Fetch(context.Background(), Query{
		ISBNs:  []string{"9781101543290"},
		Title:  "Prince of Thorns",
		Author: "Mark Lawrence",
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !bytes.Equal(got, cover) {
		t.Errorf("cover mismatch: got %d bytes", len(got))
	}
	if s.searchedFor != "Prince of Thorns" {
		t.Errorf("searched for %q, want the title", s.searchedFor)
	}
	if len(s.isbnRequests) != 1 {
		t.Errorf("ISBN attempts = %v, want exactly one before the search fallback", s.isbnRequests)
	}
}

func TestFetch_MultipleISBNsTriedInOrder(t *testing.T) {
	cover := jpegBytes(4096)
	// Only the second ISBN has a cover.
	s := &olServer{cover: cover, isbnHits: map[string]bool{"9780000000002": true}}
	f := newOL(t, s)

	if _, _, err := f.Fetch(context.Background(), Query{ISBNs: []string{"9780000000001", "9780000000002"}}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	want := []string{"9780000000001", "9780000000002"}
	if strings.Join(s.isbnRequests, ",") != strings.Join(want, ",") {
		t.Errorf("ISBN attempts = %v, want %v", s.isbnRequests, want)
	}
}

func TestFetch_NoISBNNoSearchResult(t *testing.T) {
	s := &olServer{cover: jpegBytes(4096), searchCover: 0} // search finds nothing
	f := newOL(t, s)

	_, _, err := f.Fetch(context.Background(), Query{Title: "Utterly Obscure Title", Author: "Nobody"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestFetch_EmptyQueryIsNotFound(t *testing.T) {
	s := &olServer{cover: jpegBytes(4096)}
	f := newOL(t, s)
	if _, _, err := f.Fetch(context.Background(), Query{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound for an empty query", err)
	}
}

func TestFetch_TinyPayloadRejected(t *testing.T) {
	s := &olServer{cover: []byte("GIF89a"), isbnHits: map[string]bool{"9781101543290": true}}
	f := newOL(t, s)
	// Direct ISBN "hit" returns a sub-minCoverBytes body → treated as absent →
	// falls through to search, which has no result → ErrNotFound.
	if _, _, err := f.Fetch(context.Background(), Query{ISBNs: []string{"9781101543290"}, Title: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestFetch_OversizeRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBytes(maxCoverBytes + 1024))
	}))
	defer srv.Close()
	f := &OpenLibrary{CoversBaseURL: srv.URL, SearchBaseURL: srv.URL, Client: srv.Client()}
	_, _, err := f.Fetch(context.Background(), Query{ISBNs: []string{"9781101543290"}})
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want an oversize error", err)
	}
}
