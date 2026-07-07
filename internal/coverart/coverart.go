// Package coverart fetches canonical book cover images from an external source.
// Many ingested EPUBs ship a poor, low-resolution, or outright wrong "cover" (a
// publisher title page, a generic logo, or nothing), so Lyceum can pull
// authoritative art keyed on the book's metadata.
//
// Two [Fetcher] implementations ship here. [AppleBooks] (the iTunes Search API)
// is the default: it is free, keyless, matches by title+author (so it covers
// books whose EPUB carries no ISBN), and returns clean, high-resolution,
// correct-edition covers. [OpenLibrary] is an alternative that matches by ISBN
// first and falls back to a title search, but its coverage and quality are
// spottier (foreign editions and low-res thumbnails leak through).
package coverart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrNotFound reports that no cover art could be found for the query. It is an
// expected, non-exceptional outcome — callers fall back to the embedded cover.
var ErrNotFound = errors.New("coverart: no cover available")

// Query describes the book to find cover art for. ISBNs are canonical ISBN-13s
// in preference order; Title/Author drive the search fallback when no ISBN
// resolves. Language (an EPUB dc:language code such as "en" or "eng") constrains
// the search fallback to that language so it does not return a foreign-edition
// cover. All fields are optional, but a Query with neither ISBNs nor a Title can
// never match.
type Query struct {
	Title    string
	Author   string
	Language string
	ISBNs    []string
}

// Fetcher retrieves cover art for a book. Implementations return ErrNotFound
// when the source has no cover, and a non-nil error only for genuine failures
// (network, unexpected status).
type Fetcher interface {
	Fetch(ctx context.Context, q Query) (data []byte, mediaType string, err error)
}

const (
	defaultCoversBaseURL = "https://covers.openlibrary.org"
	defaultSearchBaseURL = "https://openlibrary.org"
	defaultUserAgent     = "Lyceum/1.0 (self-hosted ebook server; cover fetch)"

	// maxCoverBytes bounds a fetched cover to keep a hostile or misbehaving
	// source from streaming an unbounded body into memory.
	maxCoverBytes = 8 << 20 // 8 MiB
	// minCoverBytes rejects trivially small payloads (Open Library's blank
	// placeholder is ~43 bytes). We pass default=false so the API 404s for a
	// missing cover, but this is cheap insurance against a placeholder anyway.
	minCoverBytes = 1024
)

// OpenLibrary fetches covers from Open Library. The zero value works once its
// URL/Client/UserAgent fields are filled (NewOpenLibrary is the convenient
// constructor); the fields are exported so tests can point at an httptest server.
type OpenLibrary struct {
	CoversBaseURL string // e.g. https://covers.openlibrary.org
	SearchBaseURL string // e.g. https://openlibrary.org
	Client        *http.Client
	UserAgent     string
}

// NewOpenLibrary returns a fetcher targeting the public Open Library with a
// bounded per-request timeout.
func NewOpenLibrary() *OpenLibrary {
	return &OpenLibrary{
		CoversBaseURL: defaultCoversBaseURL,
		SearchBaseURL: defaultSearchBaseURL,
		Client:        &http.Client{Timeout: 8 * time.Second},
		UserAgent:     defaultUserAgent,
	}
}

// Fetch resolves cover art for q: it tries each ISBN directly (exact-edition
// match), then falls back to a title+author search that resolves the work's
// cover id. ErrNotFound means every avenue cleanly reported "no cover"; a
// different error means a genuine failure occurred and nothing was found.
func (o *OpenLibrary) Fetch(ctx context.Context, q Query) ([]byte, string, error) {
	var lastErr error

	coversBase := strings.TrimRight(orDefault(o.CoversBaseURL, defaultCoversBaseURL), "/")

	// 1. Exact-edition match by ISBN.
	for _, code := range q.ISBNs {
		data, mt, err := o.fetchCover(ctx, fmt.Sprintf("%s/b/isbn/%s-L.jpg?default=false", coversBase, code))
		switch {
		case err == nil:
			return data, mt, nil
		case !errors.Is(err, ErrNotFound):
			lastErr = err
		}
	}

	// 2. Work-level match by title+author search.
	if id, ok, err := o.searchCoverID(ctx, q.Title, q.Author, q.Language); err != nil {
		lastErr = err
	} else if ok {
		data, mt, err := o.fetchCover(ctx, fmt.Sprintf("%s/b/id/%d-L.jpg?default=false", coversBase, id))
		switch {
		case err == nil:
			return data, mt, nil
		case !errors.Is(err, ErrNotFound):
			lastErr = err
		}
	}

	if lastErr != nil {
		return nil, "", lastErr
	}
	return nil, "", ErrNotFound
}

// fetchCover GETs a cover image URL through the OpenLibrary client.
func (o *OpenLibrary) fetchCover(ctx context.Context, coverURL string) ([]byte, string, error) {
	return getImage(ctx, o.Client, o.UserAgent, coverURL)
}

// getImage GETs an image URL, returning its bytes and media type. A 404 maps to
// ErrNotFound; a non-image, too-small (placeholder), or too-large body is
// treated as no cover / an error respectively. Shared by the fetchers.
func getImage(ctx context.Context, client *http.Client, userAgent, rawURL string) ([]byte, string, error) {
	resp, err := doGet(ctx, client, userAgent, rawURL)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		// proceed
	case http.StatusNotFound:
		return nil, "", ErrNotFound
	default:
		return nil, "", fmt.Errorf("coverart: status %d for %s", resp.StatusCode, rawURL)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCoverBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("coverart: read body: %w", err)
	}
	if len(data) > maxCoverBytes {
		return nil, "", fmt.Errorf("coverart: cover exceeds %d bytes", maxCoverBytes)
	}
	if len(data) < minCoverBytes {
		// Too small to be a real cover (e.g. a 1x1 "no image" placeholder).
		return nil, "", ErrNotFound
	}

	mediaType := resp.Header.Get("Content-Type")
	if mediaType == "" {
		mediaType = http.DetectContentType(data)
	}
	if !strings.HasPrefix(mediaType, "image/") {
		return nil, "", ErrNotFound
	}
	return data, mediaType, nil
}

// searchResponse is the subset of Open Library's search.json we consume.
type searchResponse struct {
	Docs []struct {
		CoverI int `json:"cover_i"`
	} `json:"docs"`
}

// searchCoverID looks up a work by title (and author/language, when present) and
// returns the first result's cover id. ok is false when the search has no usable
// cover; a non-nil error is a genuine failure. A blank title short-circuits to no
// match. The language filter avoids returning a foreign-edition cover for a book
// whose canonical edition is in another language.
func (o *OpenLibrary) searchCoverID(ctx context.Context, title, author, language string) (id int, ok bool, err error) {
	if strings.TrimSpace(title) == "" {
		return 0, false, nil
	}
	q := url.Values{}
	q.Set("title", title)
	if strings.TrimSpace(author) != "" {
		q.Set("author", author)
	}
	if lang := olLanguage(language); lang != "" {
		q.Set("language", lang)
	}
	q.Set("limit", "1")
	q.Set("fields", "cover_i")

	searchBase := strings.TrimRight(orDefault(o.SearchBaseURL, defaultSearchBaseURL), "/")
	resp, err := doGet(ctx, o.Client, o.UserAgent, searchBase+"/search.json?"+q.Encode())
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, false, fmt.Errorf("coverart: search status %d", resp.StatusCode)
	}

	var sr searchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&sr); err != nil {
		return 0, false, fmt.Errorf("coverart: decode search: %w", err)
	}
	if len(sr.Docs) == 0 || sr.Docs[0].CoverI <= 0 {
		return 0, false, nil
	}
	return sr.Docs[0].CoverI, true, nil
}

// doGet issues a GET with the given User-Agent and client (falling back to the
// default client). Shared by the fetchers.
func doGet(ctx context.Context, client *http.Client, userAgent, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("coverart: build request: %w", err)
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("coverart: get %s: %w", rawURL, err)
	}
	return resp, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// iso6391to6392 maps the ISO 639-1 (2-letter) language codes EPUBs commonly use
// to the ISO 639-2/B (3-letter) codes Open Library's search expects.
var iso6391to6392 = map[string]string{
	"en": "eng", "fr": "fre", "es": "spa", "de": "ger", "it": "ita",
	"pt": "por", "nl": "dut", "ru": "rus", "ja": "jpn", "zh": "chi",
	"la": "lat", "el": "gre", "pl": "pol", "sv": "swe", "cs": "cze",
	"tr": "tur", "da": "dan", "fi": "fin", "no": "nor",
}

// olLanguage normalizes an EPUB dc:language value to the 3-letter code Open
// Library expects, or "" when it can't (in which case the search runs
// unfiltered). It handles bare 2-letter codes, already-3-letter codes, and
// region-tagged forms like "en-US".
func olLanguage(lang string) string {
	l := strings.ToLower(strings.TrimSpace(lang))
	if i := strings.IndexAny(l, "-_"); i >= 0 {
		l = l[:i]
	}
	switch {
	case l == "":
		return ""
	case len(l) == 3:
		return l
	default:
		return iso6391to6392[l]
	}
}
