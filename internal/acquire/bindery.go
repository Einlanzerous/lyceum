// Package acquire turns an owned-but-not-yet-digital ISBN into a real grab
// request against a live acquisition backend — the concrete implementation of
// the api.Acquirer seam (LYCM-35). Its counterpart, the metadata edition
// resolver, only identifies which book a barcode is; this package actually asks
// the backend to fetch a DRM-free EPUB.
//
// The backend is Bindery (https://github.com/vavallee/bindery), the deployed
// Readarr replacement in the argosy-acquisition stack: it searches the shared
// Prowlarr indexers and downloads via SABnzbd into /data/media/books, which the
// Lyceum folder-ingest watcher then picks up. This client drives Bindery's
// REST API (/api/v1/*, X-Api-Key auth) to add the scanned title to Bindery's
// library and kick off its own search+grab pipeline.
package acquire

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultUserAgent = "Lyceum/1.0 (self-hosted ebook server; acquire)"

	// requestTimeout bounds a single Bindery HTTP attempt. Bindery adds a book
	// synchronously — a metadata pull from external providers (Hardcover/
	// OpenLibrary) — then runs the searchOnAdd search+grab as a background
	// command. That synchronous pull routinely runs past ~15s under concurrent
	// load, so a short cap silently stalls the Want (LYCM-99); 60s gives it
	// comfortable headroom. The outer per-dispatch deadline (api.wantTimeout)
	// still caps total time across retries, and Want is best-effort regardless.
	requestTimeout = 60 * time.Second

	// maxAttempts is how many times do() issues a request before giving up. A
	// lookup/add that times out under burst often succeeds on a calmer retry
	// (LYCM-99), so transport errors get a bounded backoff-and-retry. This is
	// safe by construction: GET lookup is side-effect-free, and a duplicate add
	// returns 409, which Want treats as success.
	maxAttempts = 3

	// maxBody bounds a decoded Bindery JSON response.
	maxBody = 4 << 20
)

// retryBackoff is the base delay between do() attempts; the wait grows linearly
// per attempt (1×, 2×, …) and is skipped once the caller's context is done. A
// var so tests can shrink it.
var retryBackoff = 2 * time.Second

// errNotFound signals that Bindery could not resolve the ISBN to an addable
// book (no metadata match). It is handled internally by Want as a best-effort
// miss — the inventory entry still records intent as `wanted` — and is not
// surfaced to callers.
var errNotFound = errors.New("acquire: bindery has no book for ISBN")

// Bindery is an api.Acquirer backed by a Bindery server. The zero value needs
// BaseURL + APIKey set; NewBindery is the convenient constructor. Exported
// fields let tests point Client/BaseURL at an httptest server.
type Bindery struct {
	BaseURL   string // e.g. http://localhost:8787 (no trailing /api/v1)
	APIKey    string
	Client    *http.Client
	UserAgent string

	// QualityProfile names the Bindery quality profile a newly created author
	// should carry (LYCEUM_BINDERY_QUALITY_PROFILE): a numeric id or a profile
	// name, matched case-insensitively. "" picks the first profile whose cutoff
	// is "epub" (Bindery's stock "E-Book"). See ensureAuthorProfile.
	QualityProfile string

	profileMu sync.Mutex
	profileID int64 // resolved QualityProfile, once known; 0 = not yet
}

// NewBindery returns a client targeting baseURL with the given API key (found in
// Bindery's Settings → General → Security) and a bounded per-request timeout.
func NewBindery(baseURL, apiKey string) *Bindery {
	return &Bindery{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		APIKey:    apiKey,
		Client:    &http.Client{Timeout: requestTimeout},
		UserAgent: defaultUserAgent,
	}
}

// binderyBook is the subset of Bindery's Book we read from a lookup / add
// response. A lookup returns metadata (ID == 0 until it is in the library); the
// nested author carries the foreign IDs the add endpoint needs.
type binderyBook struct {
	ID            int64         `json:"id"`
	ForeignBookID string        `json:"foreignBookId"`
	AuthorID      int64         `json:"authorId"` // the library author row an add attached the book to
	Title         string        `json:"title"`
	MediaType     string        `json:"mediaType"`
	Monitored     bool          `json:"monitored"`
	Author        binderyAuthor `json:"author"`
}

// binderyAuthorRecord is the subset of Bindery's Author (GET /author/{id}) that
// ensureAuthorProfile reads.
type binderyAuthorRecord struct {
	ID               int64  `json:"id"`
	Name             string `json:"authorName"`
	QualityProfileID *int64 `json:"qualityProfileId"`
}

// binderyQualityProfile is the subset of Bindery's QualityProfile
// (GET /qualityprofile) that profile selection reads.
type binderyQualityProfile struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Cutoff string `json:"cutoff"`
}

type binderyAuthor struct {
	ForeignAuthorID string `json:"foreignAuthorId"`
	AuthorName      string `json:"authorName"`
	Name            string `json:"name"` // fallback field name on some responses
}

func (a binderyAuthor) name() string {
	if a.AuthorName != "" {
		return a.AuthorName
	}
	return a.Name
}

// addBookRequest is the POST /author/book body. searchOnAdd would make Bindery
// run its own search+grab straight from the add; Want sends it false and
// triggers that same pipeline itself once the author's quality profile is in
// place (LYCM-81). Either way the pipeline augments the release with the
// bookId (the field a raw-release grab omits, which is what previously
// blocked auto-import).
type addBookRequest struct {
	ForeignBookID   string `json:"foreignBookId"`
	ForeignAuthorID string `json:"foreignAuthorId"`
	AuthorName      string `json:"authorName"`
	SearchOnAdd     bool   `json:"searchOnAdd"`
	MediaType       string `json:"mediaType,omitempty"`
}

// Want asks Bindery to acquire a DRM-free EPUB for the given canonical ISBN-13.
// It is best-effort: a missing title, an unreachable backend, or a non-2xx
// response is logged and swallowed (returns nil) so it never fails the confirm
// request that records the inventory entry as `wanted`. Bindery does the actual
// download asynchronously; the file later lands in /data/media/books and the
// folder-ingest watcher imports it.
//
// The grab itself is Bindery's search+grab pipeline, triggered explicitly after
// the add (see searchBook). If that ever proves insufficient in live runs, the
// next escalation is: POST /api/v1/book/{id}/search, pick the best approved
// release, then POST /api/v1/queue/grab with {guid,title,nzbUrl,size,protocol,
// mediaType,bookId} — setting bookId is the essential part.
func (b *Bindery) Want(ctx context.Context, code string) error {
	book, err := b.lookup(ctx, code)
	if err != nil {
		if errors.Is(err, errNotFound) {
			log.Printf("acquire: bindery has no match for ISBN %s; recorded wanted only", code)
			return nil
		}
		log.Printf("acquire: bindery lookup ISBN %s failed: %v; recorded wanted only", code, err)
		return nil
	}
	if book.ForeignBookID == "" || book.Author.name() == "" {
		log.Printf("acquire: bindery lookup ISBN %s returned no addable book; recorded wanted only", code)
		return nil
	}

	// Already in Bindery's library: its monitor/search pipeline already owns the
	// grab. Adding again would 409 — treat as success (idempotent re-confirm).
	if book.ID != 0 {
		log.Printf("acquire: bindery already tracks ISBN %s (bookId=%d, %q)", code, book.ID, book.Title)
		return nil
	}

	// The search is triggered separately below rather than with searchOnAdd:
	// an add for a new author creates that author with no quality profile, and
	// a searchOnAdd search would be queued against it before the profile is in
	// place (LYCM-81). Add → profile → search keeps the order deterministic.
	created, err := b.addBook(ctx, addBookRequest{
		ForeignBookID:   book.ForeignBookID,
		ForeignAuthorID: book.Author.ForeignAuthorID, // may be empty; Bindery resolves by ISBN
		AuthorName:      book.Author.name(),
		SearchOnAdd:     false,
		MediaType:       "ebook",
	})
	if err != nil {
		log.Printf("acquire: bindery add ISBN %s failed: %v; recorded wanted only", code, err)
		return nil
	}
	if created.ID == 0 {
		// A 409: a concurrent confirm added it first, and that add's own
		// pipeline owns the grab.
		log.Printf("acquire: bindery already added ISBN %s concurrently; its own search owns the grab", code)
		return nil
	}
	b.ensureAuthorProfile(ctx, created)
	if err := b.searchBook(ctx, created.ID); err != nil {
		// The book is monitored and wanted in Bindery, so its scheduled
		// wanted-search still picks it up — later than an explicit search.
		log.Printf("acquire: bindery search for ISBN %s (bookId=%d) failed: %v; Bindery's scheduled search will retry", code, created.ID, err)
		return nil
	}
	log.Printf("acquire: bindery grabbing ISBN %s (bookId=%d, %q)", code, created.ID, created.Title)
	return nil
}

// ensureAuthorProfile gives the author an add just attached a book to a quality
// profile when it has none (LYCM-81). Bindery's POST /author/book creates a
// new author with no quality profile — its request body has no such field —
// and a profile-less author has no format filter, so a .mobi/.azw3 omnibus
// release wins over waiting for an EPUB; the EPUB-only watcher then refuses
// the file (8 of 13 grabs on 2026-08-27). Setting the profile after the add
// (and before Want triggers the search) is the only seam Bindery offers. An
// author that already carries a profile — set by hand, or by an earlier add —
// is left exactly as it is.
//
// Best effort: a failure here only logs, and is not retried for this book —
// a re-triggered wanted row finds the book already tracked and returns before
// reaching here — so a dropped update is only repaired by the author's next
// add. Every exit is logged, so a Bindery whose add response stopped carrying
// authorId (the one field this feature hangs on) reads as a broken feature
// in the log rather than a working one.
func (b *Bindery) ensureAuthorProfile(ctx context.Context, created binderyBook) {
	authorID := created.AuthorID
	if authorID == 0 {
		log.Printf("acquire: bindery add of bookId=%d returned no authorId; quality profile not checked (does this Bindery's book response carry authorId?)", created.ID)
		return
	}
	author, err := b.getAuthor(ctx, authorID)
	if err != nil {
		log.Printf("acquire: bindery author %d: %v; quality profile not checked", authorID, err)
		return
	}
	if author.QualityProfileID != nil {
		return
	}
	profileID, err := b.qualityProfileID(ctx)
	if err != nil {
		log.Printf("acquire: bindery author %d (%q) has no quality profile and none could be chosen: %v", authorID, author.Name, err)
		return
	}
	if err := b.setAuthorProfile(ctx, authorID, profileID); err != nil {
		log.Printf("acquire: bindery author %d (%q): set quality profile %d: %v", authorID, author.Name, profileID, err)
		return
	}
	log.Printf("acquire: bindery author %d (%q) had no quality profile; set profile %d", authorID, author.Name, profileID)
}

// qualityProfileID resolves QualityProfile against Bindery's profile list and
// caches the answer: profiles are configuration, not data, so one lookup per
// process is plenty. A failed lookup is not cached, so a Bindery that was
// briefly unreachable is asked again on the next add. The lock covers only the
// cache, never the HTTP call: concurrent dispatches may both fetch the list
// once on a cold cache, which is cheap, whereas holding the lock through
// do()'s retries against an unreachable Bindery would stall the other
// dispatches past their own deadlines.
func (b *Bindery) qualityProfileID(ctx context.Context) (int64, error) {
	b.profileMu.Lock()
	cached := b.profileID
	b.profileMu.Unlock()
	if cached != 0 {
		return cached, nil
	}
	profiles, err := b.listQualityProfiles(ctx)
	if err != nil {
		return 0, err
	}
	id, err := pickQualityProfile(profiles, b.QualityProfile)
	if err != nil {
		return 0, err
	}
	b.profileMu.Lock()
	b.profileID = id
	b.profileMu.Unlock()
	return id, nil
}

// searchBook asks Bindery to search its indexers for a book and grab the best
// release — the same SearchAndGrabBook that searchOnAdd would have queued,
// via the bulk endpoint's "search" action (POST /book/bulk), which is the API
// surface Bindery exposes for it (POST /book/{id}/search is the interactive
// release list). Fire-and-forget on Bindery's side: results show in its
// History.
func (b *Bindery) searchBook(ctx context.Context, bookID int64) error {
	body, err := json.Marshal(map[string]any{"ids": []int64{bookID}, "action": "search"})
	if err != nil {
		return fmt.Errorf("acquire: encode search: %w", err)
	}
	resp, err := b.do(ctx, http.MethodPost, "/api/v1/book/bulk", body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("acquire: bindery book search status %d", resp.StatusCode)
	}
	return nil
}

// pickQualityProfile chooses the profile `want` names — a numeric id, or a
// name matched case-insensitively — or, when want is "", the first profile
// whose cutoff is "epub": the profile that makes Bindery hold out for an EPUB
// rather than take the first format that turns up.
func pickQualityProfile(profiles []binderyQualityProfile, want string) (int64, error) {
	want = strings.TrimSpace(want)
	if want == "" {
		for _, p := range profiles {
			if strings.EqualFold(p.Cutoff, "epub") {
				return p.ID, nil
			}
		}
		return 0, fmt.Errorf("acquire: bindery has no quality profile with cutoff epub (set LYCEUM_BINDERY_QUALITY_PROFILE)")
	}
	if id, err := strconv.ParseInt(want, 10, 64); err == nil {
		for _, p := range profiles {
			if p.ID == id {
				return p.ID, nil
			}
		}
	}
	for _, p := range profiles {
		if strings.EqualFold(p.Name, want) {
			return p.ID, nil
		}
	}
	return 0, fmt.Errorf("acquire: bindery has no quality profile %q", want)
}

// getAuthor reads one library author (GET /author/{id}).
func (b *Bindery) getAuthor(ctx context.Context, id int64) (binderyAuthorRecord, error) {
	resp, err := b.do(ctx, http.MethodGet, "/api/v1/author/"+strconv.FormatInt(id, 10), nil)
	if err != nil {
		return binderyAuthorRecord{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return binderyAuthorRecord{}, fmt.Errorf("acquire: bindery author status %d", resp.StatusCode)
	}
	var a binderyAuthorRecord
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&a); err != nil {
		return binderyAuthorRecord{}, fmt.Errorf("acquire: decode author: %w", err)
	}
	return a, nil
}

// setAuthorProfile assigns a quality profile to an author (PUT /author/{id});
// Bindery's update handler leaves every field the body omits alone.
func (b *Bindery) setAuthorProfile(ctx context.Context, id, profileID int64) error {
	body, err := json.Marshal(map[string]int64{"qualityProfileId": profileID})
	if err != nil {
		return fmt.Errorf("acquire: encode author update: %w", err)
	}
	resp, err := b.do(ctx, http.MethodPut, "/api/v1/author/"+strconv.FormatInt(id, 10), body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("acquire: bindery author update status %d", resp.StatusCode)
	}
	return nil
}

// listQualityProfiles reads Bindery's quality profiles (GET /qualityprofile).
func (b *Bindery) listQualityProfiles(ctx context.Context) ([]binderyQualityProfile, error) {
	resp, err := b.do(ctx, http.MethodGet, "/api/v1/qualityprofile", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("acquire: bindery quality profiles status %d", resp.StatusCode)
	}
	var profiles []binderyQualityProfile
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&profiles); err != nil {
		return nil, fmt.Errorf("acquire: decode quality profiles: %w", err)
	}
	return profiles, nil
}

// lookup resolves an ISBN to Bindery's book metadata. A 404 (or an empty body)
// is reported as errNotFound.
func (b *Bindery) lookup(ctx context.Context, code string) (binderyBook, error) {
	q := url.Values{}
	q.Set("isbn", code)
	resp, err := b.do(ctx, http.MethodGet, "/api/v1/book/lookup?"+q.Encode(), nil)
	if err != nil {
		return binderyBook{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return binderyBook{}, errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return binderyBook{}, fmt.Errorf("acquire: bindery lookup status %d", resp.StatusCode)
	}

	var book binderyBook
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&book); err != nil {
		return binderyBook{}, fmt.Errorf("acquire: decode lookup: %w", err)
	}
	if book.ForeignBookID == "" {
		return binderyBook{}, errNotFound
	}
	return book, nil
}

// addBook adds a book to Bindery's library and returns the created record (with
// its assigned id). A 409 Conflict means the book already exists — a benign
// race with a concurrent confirm — and is reported as success with an empty
// book so Want treats it idempotently.
func (b *Bindery) addBook(ctx context.Context, req addBookRequest) (binderyBook, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return binderyBook{}, fmt.Errorf("acquire: encode add: %w", err)
	}
	resp, err := b.do(ctx, http.MethodPost, "/api/v1/author/book", body)
	if err != nil {
		return binderyBook{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusConflict {
		return binderyBook{}, nil
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return binderyBook{}, fmt.Errorf("acquire: bindery add status %d", resp.StatusCode)
	}

	var book binderyBook
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&book); err != nil {
		return binderyBook{}, fmt.Errorf("acquire: decode add: %w", err)
	}
	return book, nil
}

// do issues an authenticated request to Bindery, retrying transport failures
// (a timed-out or dropped connection) up to maxAttempts with a linear backoff.
// path is everything after BaseURL (including /api/v1 and any query string);
// body is nil for GETs. A response — even a non-2xx one — is returned as-is
// without retrying; only transport-level errors are retried, and never past the
// caller's context deadline (the outer api.wantTimeout bounds total time).
func (b *Bindery) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("acquire: %s %s: %w", method, path, ctx.Err())
			case <-time.After(time.Duration(attempt-1) * retryBackoff):
			}
		}
		resp, err := b.doOnce(ctx, method, path, body)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		// A cancelled/expired outer context is terminal — retrying against a
		// dead deadline just burns attempts.
		if ctx.Err() != nil {
			break
		}
		if attempt < maxAttempts {
			log.Printf("acquire: %s %s attempt %d/%d failed: %v; retrying", method, path, attempt, maxAttempts, err)
		}
	}
	return nil, lastErr
}

// doOnce issues a single authenticated request to Bindery without retry.
func (b *Bindery) doOnce(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.BaseURL+path, r)
	if err != nil {
		return nil, fmt.Errorf("acquire: build request: %w", err)
	}
	req.Header.Set("X-Api-Key", b.APIKey)
	if b.UserAgent != "" {
		req.Header.Set("User-Agent", b.UserAgent)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := b.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("acquire: %s %s: %w", method, path, err)
	}
	return resp, nil
}
