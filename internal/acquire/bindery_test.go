package acquire

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// capture records what a fake Bindery received so a test can assert on the
// requests Want made.
type capture struct {
	lookupISBN string
	lookupHits int
	addHits    int
	addBody    addBookRequest
	apiKeySeen string
	searchIDs  []int64 // ids handed to POST /book/bulk action=search
}

// stubBindery stands up a fake Bindery whose /book/lookup returns lookupBody
// (or 404 when empty) and whose /author/book echoes a created book with id 77.
// It returns a client pointed at it and the capture of what it saw.
func stubBindery(t *testing.T, lookupStatus int, lookupBody string) (*Bindery, *capture) {
	t.Helper()
	cap := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.apiKeySeen = r.Header.Get("X-Api-Key")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/book/lookup":
			cap.lookupHits++
			cap.lookupISBN = r.URL.Query().Get("isbn")
			if lookupStatus != http.StatusOK {
				w.WriteHeader(lookupStatus)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, lookupBody)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/author/book":
			cap.addHits++
			_ = json.NewDecoder(r.Body).Decode(&cap.addBody)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":77,"title":"The Dinosaur Lords","foreignBookId":"OL1B"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/book/bulk":
			var req struct {
				IDs    []int64 `json:"ids"`
				Action string  `json:"action"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Action == "search" {
				cap.searchIDs = append(cap.searchIDs, req.IDs...)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"results":{"77":{"ok":true}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	b := NewBindery(srv.URL, "secret-key")
	return b, cap
}

// a lookup body for a fresh (not-yet-in-library) book with a resolvable author.
const freshLookup = `{
	"id":0,"foreignBookId":"OL1B","title":"The Dinosaur Lords","mediaType":"ebook",
	"author":{"foreignAuthorId":"OL2A","authorName":"Victor Milán"}
}`

func TestWantAddsFreshBookThenSearches(t *testing.T) {
	b, cap := stubBindery(t, http.StatusOK, freshLookup)

	if err := b.Want(context.Background(), "9780765382115"); err != nil {
		t.Fatalf("Want: %v", err)
	}
	if cap.apiKeySeen != "secret-key" {
		t.Fatalf("X-Api-Key = %q, want secret-key", cap.apiKeySeen)
	}
	if cap.lookupISBN != "9780765382115" {
		t.Fatalf("lookup isbn = %q", cap.lookupISBN)
	}
	if cap.addHits != 1 {
		t.Fatalf("add hits = %d, want 1", cap.addHits)
	}
	// The search is Want's own explicit trigger, after the profile step, not
	// searchOnAdd racing it (LYCM-81).
	if cap.addBody.SearchOnAdd {
		t.Fatalf("add set searchOnAdd; the search must follow the profile assignment instead")
	}
	if len(cap.searchIDs) != 1 || cap.searchIDs[0] != 77 {
		t.Fatalf("search ids = %v, want [77] (the created book)", cap.searchIDs)
	}
	if cap.addBody.ForeignBookID != "OL1B" || cap.addBody.ForeignAuthorID != "OL2A" {
		t.Fatalf("add foreign ids = %q/%q", cap.addBody.ForeignBookID, cap.addBody.ForeignAuthorID)
	}
	if cap.addBody.AuthorName != "Victor Milán" {
		t.Fatalf("add authorName = %q", cap.addBody.AuthorName)
	}
	if cap.addBody.MediaType != "ebook" {
		t.Fatalf("add mediaType = %q, want ebook", cap.addBody.MediaType)
	}
}

func TestWantIdempotentWhenAlreadyInLibrary(t *testing.T) {
	// A non-zero id means Bindery already tracks the book: Want must not add it
	// again (which would 409).
	const inLibrary = `{
		"id":42,"foreignBookId":"OL1B","title":"The Dinosaur Lords",
		"author":{"foreignAuthorId":"OL2A","authorName":"Victor Milán"}
	}`
	b, cap := stubBindery(t, http.StatusOK, inLibrary)

	if err := b.Want(context.Background(), "9780765382115"); err != nil {
		t.Fatalf("Want: %v", err)
	}
	if cap.addHits != 0 {
		t.Fatalf("add hits = %d, want 0 for an already-tracked book", cap.addHits)
	}
}

func TestWantNotFoundIsNonFatal(t *testing.T) {
	// A 404 lookup must not error (the caller records `wanted` regardless) and
	// must not attempt an add.
	b, cap := stubBindery(t, http.StatusNotFound, "")

	if err := b.Want(context.Background(), "9780000000002"); err != nil {
		t.Fatalf("Want on 404 lookup = %v, want nil (best-effort)", err)
	}
	if cap.lookupHits != 1 {
		t.Fatalf("lookup hits = %d, want 1", cap.lookupHits)
	}
	if cap.addHits != 0 {
		t.Fatalf("add hits = %d, want 0 when nothing was found", cap.addHits)
	}
}

func TestWantEmptyMatchIsNonFatal(t *testing.T) {
	// A 200 with no foreignBookId is treated as no match: no add, no error.
	b, cap := stubBindery(t, http.StatusOK, `{"id":0}`)

	if err := b.Want(context.Background(), "9780000000002"); err != nil {
		t.Fatalf("Want: %v", err)
	}
	if cap.addHits != 0 {
		t.Fatalf("add hits = %d, want 0 for an empty match", cap.addHits)
	}
}

func TestWantBackendErrorIsNonFatal(t *testing.T) {
	// A 5xx from lookup is logged and swallowed so a flaky Bindery never breaks
	// the confirm flow.
	b, _ := stubBindery(t, http.StatusInternalServerError, "")

	if err := b.Want(context.Background(), "9780765382115"); err != nil {
		t.Fatalf("Want on lookup 500 = %v, want nil (best-effort)", err)
	}
}

func TestWantRetriesTransientTransportError(t *testing.T) {
	// A lookup whose first attempt drops the connection (a stand-in for the
	// client timeout seen under burst, LYCM-99) must be retried and then
	// succeed, driving the add exactly once.
	old := retryBackoff
	retryBackoff = time.Millisecond
	t.Cleanup(func() { retryBackoff = old })

	// Counters are touched from the server goroutine and read after Want
	// returns; a hijacked+closed connection gives no happens-before, so use
	// atomics to stay clean under -race.
	var lookupHits, addHits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/book/lookup":
			if lookupHits.Add(1) == 1 {
				// Hijack and close without a response → transport error.
				hj, ok := w.(http.Hijacker)
				if !ok {
					t.Errorf("test server does not support hijack")
					return
				}
				conn, _, err := hj.Hijack()
				if err != nil {
					t.Errorf("hijack: %v", err)
					return
				}
				_ = conn.Close()
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, freshLookup)
		case "/api/v1/author/book":
			addHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":77,"title":"The Dinosaur Lords","foreignBookId":"OL1B"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	b := NewBindery(srv.URL, "k")

	if err := b.Want(context.Background(), "9780765382115"); err != nil {
		t.Fatalf("Want: %v", err)
	}
	if got := lookupHits.Load(); got < 2 {
		t.Fatalf("lookup hits = %d, want >= 2 (a retry after the dropped connection)", got)
	}
	if got := addHits.Load(); got != 1 {
		t.Fatalf("add hits = %d, want 1", got)
	}
}

func TestWantExhaustedRetriesIsNonFatal(t *testing.T) {
	// When every attempt fails transport-side, Want gives up after maxAttempts
	// and still returns nil (best-effort: the entry rests in `wanted`).
	old := retryBackoff
	retryBackoff = time.Millisecond
	t.Cleanup(func() { retryBackoff = old })

	// Atomic: written by the server goroutine, read after Want returns, with no
	// happens-before from the hijacked+closed connection (stays clean -race).
	var lookupHits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/book/lookup" {
			http.NotFound(w, r)
			return
		}
		lookupHits.Add(1)
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Errorf("test server does not support hijack")
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		_ = conn.Close()
	}))
	t.Cleanup(srv.Close)
	b := NewBindery(srv.URL, "k")

	if err := b.Want(context.Background(), "9780765382115"); err != nil {
		t.Fatalf("Want on exhausted retries = %v, want nil (best-effort)", err)
	}
	if got := lookupHits.Load(); got != maxAttempts {
		t.Fatalf("lookup hits = %d, want %d (one per attempt)", got, maxAttempts)
	}
}

func TestWantAddConflictIsNonFatal(t *testing.T) {
	// A 409 on add (a concurrent confirm added it first) is benign.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/book/lookup" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, freshLookup)
			return
		}
		w.WriteHeader(http.StatusConflict)
	}))
	t.Cleanup(srv.Close)
	b := NewBindery(srv.URL, "k")

	if err := b.Want(context.Background(), "9780765382115"); err != nil {
		t.Fatalf("Want on add 409 = %v, want nil", err)
	}
}

// An add that came back with no authorId is the one way this feature can
// silently do nothing, so it must say so — distinguishably from a 409.
func TestWantLogsWhenAddCarriesNoAuthorID(t *testing.T) {
	logs := captureLog(t)
	b, _ := stubBindery(t, http.StatusOK, freshLookup) // its add response has no authorId
	if err := b.Want(context.Background(), "9780765382115"); err != nil {
		t.Fatalf("Want: %v", err)
	}
	if out := logs.String(); !strings.Contains(out, "returned no authorId") {
		t.Fatalf("expected a log line naming the missing authorId; got:\n%s", out)
	}
}

// profileBindery is a fake whose add attaches the book to author 9, and which
// serves the author record, the profile list and the author update that
// ensureAuthorProfile (LYCM-81) drives. authorProfile is what GET /author/9
// reports (nil = none); puts collects every PUT /author/9 body.
type profileCapture struct {
	mu           sync.Mutex
	puts         []map[string]int64
	profileLists int
	// order is the sequence of writes Bindery saw — "put" for the author
	// profile update, "search" for the bulk search — so a test can assert
	// the profile landed before the search was queued.
	order []string
}

func profileBindery(t *testing.T, authorProfile *int64, profiles string) (*Bindery, *profileCapture) {
	t.Helper()
	cap := &profileCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/book/lookup":
			_, _ = io.WriteString(w, freshLookup)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/author/book":
			_, _ = io.WriteString(w, `{"id":77,"authorId":9,"title":"The Dinosaur Lords","foreignBookId":"OL1B"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/author/9":
			if authorProfile == nil {
				_, _ = io.WriteString(w, `{"id":9,"authorName":"Victor Milán","qualityProfileId":null}`)
			} else {
				_, _ = fmt.Fprintf(w, `{"id":9,"authorName":"Victor Milán","qualityProfileId":%d}`, *authorProfile)
			}
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/author/9":
			var body map[string]int64
			_ = json.NewDecoder(r.Body).Decode(&body)
			cap.mu.Lock()
			cap.puts = append(cap.puts, body)
			cap.order = append(cap.order, "put")
			cap.mu.Unlock()
			_, _ = io.WriteString(w, `{"id":9}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/book/bulk":
			cap.mu.Lock()
			cap.order = append(cap.order, "search")
			cap.mu.Unlock()
			_, _ = io.WriteString(w, `{"results":{"77":{"ok":true}}}`)
		case r.URL.Path == "/api/v1/qualityprofile":
			cap.mu.Lock()
			cap.profileLists++
			cap.mu.Unlock()
			_, _ = io.WriteString(w, profiles)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return NewBindery(srv.URL, "k"), cap
}

// Bindery's stock profiles: "Any" (id 1, cutoff mobi) and "E-Book" (id 2,
// cutoff epub).
const stockProfiles = `[{"id":1,"name":"Any","cutoff":"mobi","items":[]},{"id":2,"name":"E-Book","cutoff":"epub","items":[]}]`

func TestWantGivesNewAuthorTheEpubProfile(t *testing.T) {
	// POST /author/book creates the author with no quality profile, and a
	// profile-less author takes the first format that turns up (LYCM-81). The
	// add must be followed by an author update assigning the epub-cutoff one.
	b, cap := profileBindery(t, nil, stockProfiles)

	if err := b.Want(context.Background(), "9780765382115"); err != nil {
		t.Fatalf("Want: %v", err)
	}
	if len(cap.puts) != 1 || cap.puts[0]["qualityProfileId"] != 2 {
		t.Fatalf("author updates = %v, want one setting qualityProfileId 2 (cutoff epub)", cap.puts)
	}
	// The whole point: the profile is on the author before the search is
	// queued, so the release decision sees it.
	if strings.Join(cap.order, ",") != "put,search" {
		t.Fatalf("write order = %v, want the profile update before the search", cap.order)
	}
}

func TestWantLeavesAnAssignedProfileAlone(t *testing.T) {
	// A profile the user chose by hand (or an earlier add set) is never
	// overwritten.
	five := int64(5)
	b, cap := profileBindery(t, &five, stockProfiles)

	if err := b.Want(context.Background(), "9780765382115"); err != nil {
		t.Fatalf("Want: %v", err)
	}
	if len(cap.puts) != 0 {
		t.Fatalf("author updates = %v, want none for an author that has a profile", cap.puts)
	}
	if cap.profileLists != 0 {
		t.Fatalf("profile list fetched %d times, want 0 when nothing needs choosing", cap.profileLists)
	}
	if strings.Join(cap.order, ",") != "search" {
		t.Fatalf("write order = %v, want just the search", cap.order)
	}
}

func TestWantProfileConfiguredByNameOrID(t *testing.T) {
	for _, tc := range []struct {
		want string
		id   int64
	}{{"any", 1}, {"E-BOOK", 2}, {"1", 1}, {"2", 2}} {
		b, cap := profileBindery(t, nil, stockProfiles)
		b.QualityProfile = tc.want
		if err := b.Want(context.Background(), "9780765382115"); err != nil {
			t.Fatalf("Want: %v", err)
		}
		if len(cap.puts) != 1 || cap.puts[0]["qualityProfileId"] != tc.id {
			t.Fatalf("QualityProfile %q: author updates = %v, want profile %d", tc.want, cap.puts, tc.id)
		}
	}
}

func TestWantNoUsableProfileIsNonFatal(t *testing.T) {
	// No epub-cutoff profile (or a configured name that matches nothing): the
	// grab was already requested, so Want still succeeds and only logs.
	for _, tc := range []struct{ want, profiles string }{
		{"", `[{"id":1,"name":"Any","cutoff":"mobi","items":[]}]`},
		{"Audiobook", stockProfiles},
	} {
		b, cap := profileBindery(t, nil, tc.profiles)
		b.QualityProfile = tc.want
		if err := b.Want(context.Background(), "9780765382115"); err != nil {
			t.Fatalf("Want with no usable profile = %v, want nil (best-effort)", err)
		}
		if len(cap.puts) != 0 {
			t.Fatalf("author updates = %v, want none", cap.puts)
		}
	}
}

func TestWantCachesTheProfileLookup(t *testing.T) {
	b, cap := profileBindery(t, nil, stockProfiles)
	for i := 0; i < 3; i++ {
		if err := b.Want(context.Background(), "9780765382115"); err != nil {
			t.Fatalf("Want: %v", err)
		}
	}
	if cap.profileLists != 1 {
		t.Fatalf("profile list fetched %d times over 3 adds, want 1", cap.profileLists)
	}
	if len(cap.puts) != 3 {
		t.Fatalf("author updates = %d, want 3", len(cap.puts))
	}
}

func TestPickQualityProfile(t *testing.T) {
	profiles := []binderyQualityProfile{{ID: 1, Name: "Any", Cutoff: "mobi"}, {ID: 2, Name: "E-Book", Cutoff: "epub"}}
	if id, err := pickQualityProfile(profiles, ""); err != nil || id != 2 {
		t.Fatalf("default pick = %d, %v; want 2 (cutoff epub)", id, err)
	}
	if id, err := pickQualityProfile(profiles, " e-book "); err != nil || id != 2 {
		t.Fatalf("by name = %d, %v; want 2", id, err)
	}
	if id, err := pickQualityProfile(profiles, "1"); err != nil || id != 1 {
		t.Fatalf("by id = %d, %v; want 1", id, err)
	}
	if _, err := pickQualityProfile(profiles, "7"); err == nil {
		t.Fatalf("unknown id should fail rather than fall back")
	}
	if _, err := pickQualityProfile(nil, ""); err == nil {
		t.Fatalf("no profiles should fail")
	}
}

// captureLog routes the package logger into a buffer for the test's duration.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(old) })
	return &buf
}
