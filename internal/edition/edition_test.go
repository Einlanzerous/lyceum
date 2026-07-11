package edition

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// stubSearch stands up an Open Library-shaped /search.json server returning body
// for any query, and returns a resolver pointed at it. It records the last query
// values so a test can assert what was asked.
func stubSearch(t *testing.T, body string) (*OpenLibrary, *url.Values) {
	t.Helper()
	var last url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search.json" {
			http.NotFound(w, r)
			return
		}
		last = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	r := NewOpenLibrary()
	r.SearchBaseURL = srv.URL
	r.CoversBaseURL = "https://covers.example"
	return r, &last
}

func TestResolveISBNSingleEdition(t *testing.T) {
	r, last := stubSearch(t, `{"docs":[{
		"key":"/works/OL1W","title":"Piranesi","author_name":["Susanna Clarke"],
		"first_publish_year":2020,"publisher":["Bloomsbury"],
		"number_of_pages_median":272,"language":["eng"],"cover_i":42,
		"isbn":["9781635575637","1635575630"]}]}`)

	eds, err := r.ResolveISBN(context.Background(), "9781635575637")
	if err != nil {
		t.Fatalf("ResolveISBN: %v", err)
	}
	if len(eds) != 1 {
		t.Fatalf("editions = %d, want 1", len(eds))
	}
	e := eds[0]
	// The scanned ISBN is stamped as the canonical id/isbn, not an arbitrary one
	// off the work.
	if e.ID != "9781635575637" || e.ISBN13 != "9781635575637" {
		t.Fatalf("id/isbn = %q/%q, want 9781635575637", e.ID, e.ISBN13)
	}
	if e.Title != "Piranesi" || e.Author != "Susanna Clarke" {
		t.Fatalf("title/author = %q/%q", e.Title, e.Author)
	}
	if e.Publisher != "Bloomsbury" || e.Year != "2020" || e.Pages != 272 {
		t.Fatalf("publisher/year/pages = %q/%q/%d", e.Publisher, e.Year, e.Pages)
	}
	if e.CoverURL != "https://covers.example/b/id/42-M.jpg" {
		t.Fatalf("cover = %q", e.CoverURL)
	}
	// The ISBN went out as the query.
	if got := last.Get("q"); got != "9781635575637" {
		t.Fatalf("query q = %q, want the isbn", got)
	}
}

func TestResolveISBNNoMatch(t *testing.T) {
	r, _ := stubSearch(t, `{"docs":[]}`)
	eds, err := r.ResolveISBN(context.Background(), "9780000000002")
	if err != nil {
		t.Fatalf("ResolveISBN: %v", err)
	}
	if len(eds) != 0 {
		t.Fatalf("editions = %d, want 0", len(eds))
	}
}

func TestResolveISBNBlankShortCircuits(t *testing.T) {
	r, last := stubSearch(t, `{"docs":[{"title":"nope"}]}`)
	eds, err := r.ResolveISBN(context.Background(), "   ")
	if err != nil {
		t.Fatalf("ResolveISBN: %v", err)
	}
	if len(eds) != 0 {
		t.Fatalf("editions = %d, want 0 for blank isbn", len(eds))
	}
	if last.Has("q") {
		t.Fatalf("blank isbn should not hit the server")
	}
}

func TestSearchTitleMultiple(t *testing.T) {
	r, last := stubSearch(t, `{"docs":[
		{"key":"/works/OL2W","title":"Dune","author_name":["Frank Herbert"],"isbn":["9780441172719"]},
		{"key":"/works/OL3W","title":"Dune","author_name":["Frank Herbert"],"isbn":["9781473233966"]}]}`)

	eds, err := r.SearchTitle(context.Background(), "Dune")
	if err != nil {
		t.Fatalf("SearchTitle: %v", err)
	}
	if len(eds) != 2 {
		t.Fatalf("editions = %d, want 2", len(eds))
	}
	// With no scanned ISBN, each edition takes the first valid ISBN off its doc,
	// giving distinct stable ids the reviewer can pick between.
	if eds[0].ID != "9780441172719" || eds[1].ID != "9781473233966" {
		t.Fatalf("ids = %q, %q", eds[0].ID, eds[1].ID)
	}
	if got := last.Get("title"); got != "Dune" {
		t.Fatalf("title query = %q", got)
	}
}
