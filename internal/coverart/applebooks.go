package coverart

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	defaultAppleSearchURL = "https://itunes.apple.com"
	// appleArtworkSize is the square bounding box requested from Apple's image
	// CDN. Book covers keep their aspect ratio within it, so a 600 box yields a
	// ~400x600 cover — ample for the library grid, upscaled from the 100px URL
	// the search returns.
	appleArtworkSize = 600
)

// appleSizeRE matches the trailing "…/{w}x{h}bb.jpg" size segment of an Apple
// artwork URL so it can be swapped for a larger one.
var appleSizeRE = regexp.MustCompile(`/\d+x\d+bb\.(jpg|png)$`)

// AppleBooks fetches covers from the Apple Books catalog via the public iTunes
// Search API. It matches by title+author (so it covers EPUBs with no ISBN) and,
// when the book's ISBN is known, prefers the result whose artwork URL carries
// that ISBN — an exact-edition match. Free and keyless.
type AppleBooks struct {
	SearchBaseURL string // e.g. https://itunes.apple.com
	Country       string // iTunes storefront, e.g. "US" (default US)
	Client        *http.Client
	UserAgent     string
}

// NewAppleBooks returns a fetcher targeting the public iTunes Search API with a
// bounded per-request timeout and the US storefront.
func NewAppleBooks() *AppleBooks {
	return &AppleBooks{
		SearchBaseURL: defaultAppleSearchURL,
		Country:       "US",
		Client:        &http.Client{Timeout: 8 * time.Second},
		UserAgent:     defaultUserAgent,
	}
}

// appleResult is the subset of an iTunes Search result we consume.
type appleResult struct {
	TrackName   string `json:"trackName"`
	ArtistName  string `json:"artistName"`
	ArtworkURL  string `json:"artworkUrl100"`
	Kind        string `json:"kind"`
	WrapperType string `json:"wrapperType"`
}

type appleSearchResponse struct {
	ResultCount int           `json:"resultCount"`
	Results     []appleResult `json:"results"`
}

// Fetch searches Apple Books for the book and downloads the best match's cover
// at a larger size. It prefers a result whose artwork URL contains one of the
// book's ISBNs (confirming the edition); otherwise it takes the first (most
// relevant) result. ErrNotFound means the search returned nothing usable.
func (a *AppleBooks) Fetch(ctx context.Context, q Query) ([]byte, string, error) {
	term := strings.TrimSpace(strings.TrimSpace(q.Title) + " " + strings.TrimSpace(q.Author))
	if term == "" {
		return nil, "", ErrNotFound
	}

	res, err := a.search(ctx, term)
	if err != nil {
		return nil, "", err
	}
	art := pickArtworkURL(res, q.ISBNs)
	if art == "" {
		return nil, "", ErrNotFound
	}
	return getImage(ctx, a.Client, a.UserAgent, upscaleAppleArtwork(art, appleArtworkSize))
}

func (a *AppleBooks) search(ctx context.Context, term string) ([]appleResult, error) {
	v := url.Values{}
	v.Set("term", term)
	v.Set("entity", "ebook")
	v.Set("limit", "5")
	if c := strings.TrimSpace(a.Country); c != "" {
		v.Set("country", c)
	}

	base := strings.TrimRight(orDefault(a.SearchBaseURL, defaultAppleSearchURL), "/")
	resp, err := doGet(ctx, a.Client, a.UserAgent, base+"/search?"+v.Encode())
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("coverart: apple search status %d", resp.StatusCode)
	}

	var sr appleSearchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&sr); err != nil {
		return nil, fmt.Errorf("coverart: decode apple search: %w", err)
	}
	return sr.Results, nil
}

// pickArtworkURL chooses the artwork URL to use: the first result whose URL
// carries one of the wanted ISBNs (exact edition), else the first result with
// any artwork. Returns "" when there is nothing usable.
func pickArtworkURL(results []appleResult, isbns []string) string {
	for _, code := range isbns {
		for _, r := range results {
			if r.ArtworkURL != "" && strings.Contains(r.ArtworkURL, code) {
				return r.ArtworkURL
			}
		}
	}
	for _, r := range results {
		if r.ArtworkURL != "" {
			return r.ArtworkURL
		}
	}
	return ""
}

// upscaleAppleArtwork rewrites an iTunes artworkUrl100 (…/100x100bb.jpg) to a
// larger square box. If the URL doesn't match the expected shape it is returned
// unchanged.
func upscaleAppleArtwork(artURL string, size int) string {
	repl := fmt.Sprintf("/%dx%dbb.$1", size, size)
	return appleSizeRE.ReplaceAllString(artURL, repl)
}
