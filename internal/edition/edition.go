// Package edition resolves an ISBN (or a free-text title) to candidate book
// editions — the metadata a scanned physical book maps to during ISBN ingest
// review (LYCM-603). It is the read-only "match" stage of the pipeline: given a
// normalized ISBN it returns zero or more [store.Edition]s (zero => no match,
// one => a confident match, several => an ambiguous set the reviewer picks from).
//
// The shipped [OpenLibrary] resolver is keyless and best-effort. It is distinct
// from the acquisition backend (the Acquirer seam / Bindery): resolving an
// edition never obtains a readable EPUB, it only identifies which book a barcode
// is. The API layer scores the returned set into a confidence and a candidate
// status.
package edition

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/magos/lyceum/internal/isbn"
	"github.com/magos/lyceum/internal/store"
)

const (
	defaultSearchBaseURL = "https://openlibrary.org"
	defaultCoversBaseURL = "https://covers.openlibrary.org"
	defaultUserAgent     = "Lyceum/1.0 (self-hosted ebook server; edition resolve)"

	// maxEditions bounds how many candidates one lookup returns. A confident ISBN
	// match is usually a single doc; the cap keeps an ambiguous title search from
	// flooding the review queue.
	maxEditions = 5
)

// OpenLibrary resolves editions via Open Library's search API. The zero value
// works once its fields are set; NewOpenLibrary is the convenient constructor,
// and the exported fields let tests point at an httptest server.
type OpenLibrary struct {
	SearchBaseURL string // e.g. https://openlibrary.org
	CoversBaseURL string // e.g. https://covers.openlibrary.org
	Client        *http.Client
	UserAgent     string
}

// NewOpenLibrary returns a resolver targeting public Open Library with a bounded
// per-request timeout.
func NewOpenLibrary() *OpenLibrary {
	return &OpenLibrary{
		SearchBaseURL: defaultSearchBaseURL,
		CoversBaseURL: defaultCoversBaseURL,
		Client:        &http.Client{Timeout: 8 * time.Second},
		UserAgent:     defaultUserAgent,
	}
}

// ResolveISBN returns candidate editions for a canonical ISBN-13. An empty slice
// (nil error) means no edition matched — the caller flags the scan no_match.
func (o *OpenLibrary) ResolveISBN(ctx context.Context, code string) ([]store.Edition, error) {
	if strings.TrimSpace(code) == "" {
		return nil, nil
	}
	q := url.Values{}
	q.Set("q", code)
	return o.search(ctx, q, code)
}

// SearchTitle returns candidate editions for a free-text title query — the
// scanner-free add-by-title path on desktop. A blank query matches nothing.
func (o *OpenLibrary) SearchTitle(ctx context.Context, query string) ([]store.Edition, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	q := url.Values{}
	q.Set("title", query)
	return o.search(ctx, q, "")
}

// searchDoc is the subset of Open Library's search.json we consume.
type searchDoc struct {
	Key         string   `json:"key"`
	Title       string   `json:"title"`
	AuthorName  []string `json:"author_name"`
	FirstYear   int      `json:"first_publish_year"`
	Publisher   []string `json:"publisher"`
	PagesMedian int      `json:"number_of_pages_median"`
	Language    []string `json:"language"`
	CoverI      int      `json:"cover_i"`
	ISBN        []string `json:"isbn"`
}

type searchResponse struct {
	Docs []searchDoc `json:"docs"`
}

// search runs one search.json query and maps its docs to editions. wantISBN, when
// non-empty, is the scanned code: it is stamped as each edition's canonical ISBN
// and id so a Confirm keys off the code the user actually scanned rather than an
// arbitrary edition ISBN from the work.
func (o *OpenLibrary) search(ctx context.Context, params url.Values, wantISBN string) ([]store.Edition, error) {
	params.Set("fields", "key,title,author_name,first_publish_year,publisher,number_of_pages_median,language,cover_i,isbn")
	params.Set("limit", strconv.Itoa(maxEditions))

	base := strings.TrimRight(orDefault(o.SearchBaseURL, defaultSearchBaseURL), "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/search.json?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("edition: build request: %w", err)
	}
	if o.UserAgent != "" {
		req.Header.Set("User-Agent", o.UserAgent)
	}
	client := o.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("edition: search: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("edition: search status %d", resp.StatusCode)
	}

	var sr searchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&sr); err != nil {
		return nil, fmt.Errorf("edition: decode search: %w", err)
	}

	out := make([]store.Edition, 0, len(sr.Docs))
	for _, d := range sr.Docs {
		out = append(out, o.toEdition(d, wantISBN))
	}
	return out, nil
}

// toEdition maps one search doc to a store.Edition. It prefers wantISBN (the
// scanned code) for the canonical ISBN/id, falling back to the first valid ISBN
// on the doc, then the Open Library work key, so every edition has a stable id.
func (o *OpenLibrary) toEdition(d searchDoc, wantISBN string) store.Edition {
	code := wantISBN
	if code == "" {
		if c, ok := isbn.FirstFrom(d.ISBN); ok {
			code = c
		}
	}
	id := code
	if id == "" {
		id = d.Key
	}

	e := store.Edition{
		ID:     id,
		WorkID: d.Key, // the OpenLibrary work key groups editions of one title
		ISBN13: code,
		Title:  strings.TrimSpace(d.Title),
	}
	if len(d.AuthorName) > 0 {
		e.Author = d.AuthorName[0]
	}
	if len(d.Publisher) > 0 {
		e.Publisher = d.Publisher[0]
	}
	if d.FirstYear > 0 {
		e.Year = strconv.Itoa(d.FirstYear)
	}
	if d.PagesMedian > 0 {
		e.Pages = d.PagesMedian
	}
	if len(d.Language) > 0 {
		e.Language = d.Language[0]
	}
	if d.CoverI > 0 {
		coversBase := strings.TrimRight(orDefault(o.CoversBaseURL, defaultCoversBaseURL), "/")
		e.CoverURL = fmt.Sprintf("%s/b/id/%d-M.jpg", coversBase, d.CoverI)
	}
	return e
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
