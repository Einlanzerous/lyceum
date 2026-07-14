package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/magos/lyceum/internal/store"
)

// testStore returns a migrated store.Store backed by TEST_DATABASE_URL with the
// data tables truncated, plus a temp data dir. It skips when the test database
// is unavailable.
func testStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping DB-backed test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := connectSchema(ctx, dsn, "lyceum_test_api")
	if err != nil {
		t.Fatalf("connectSchema: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	truncate(ctx, t, pool)
	return store.New(pool, t.TempDir())
}

// connectSchema opens a pool pinned to its own Postgres schema via search_path,
// creating the schema first. This isolates the api package's test binary from
// other packages that share the single TEST_DATABASE_URL database when
// `go test ./...` runs their test binaries in parallel.
func connectSchema(ctx context.Context, dsn, schema string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+schema); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func truncate(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`TRUNCATE ingest_candidates, ingest_batches, reading_positions, devices, inventory, books RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	// users is deliberately NOT truncated: migration 0011 seeds exactly one owner
	// and the schema insists on it. Clear the members and every credential
	// instead, so each case starts from a lone, token-less owner (LYCM-801).
	if _, err := pool.Exec(ctx,
		`DELETE FROM user_tokens; DELETE FROM users WHERE NOT is_owner`); err != nil {
		t.Fatalf("reset users: %v", err)
	}
}

// ownerID is the account migration 0011 seeds. With user auth off (the default
// in these tests, since New is called without WithUserAuth) every request is
// served as the owner, so tests that seed or assert reading positions directly
// through the store must use this id to line up with what the handlers see.
func ownerID(ctx context.Context, t *testing.T, s *store.Store) int64 {
	t.Helper()
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	return owner.ID
}

// pngBytes is a minimal payload whose magic bytes make http content sniffing
// report image/png.
var pngBytes = []byte("\x89PNG\r\n\x1a\nfake-cover-data")

var epubBytes = []byte("PK\x03\x04 fake epub payload")

// seedBook writes blobs and inserts a book row, returning it.
func seedBook(t *testing.T, s *store.Store, hash, title, author string, cover []byte) store.Book {
	t.Helper()
	filePath, coverPath, err := s.SaveBlobs(hash, epubBytes, cover)
	if err != nil {
		t.Fatalf("SaveBlobs: %v", err)
	}
	b, err := s.InsertBook(context.Background(), store.Book{
		Title:     title,
		Author:    author,
		CoverPath: coverPath,
		FilePath:  filePath,
		FileHash:  hash,
		SizeBytes: int64(len(epubBytes)),
	})
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}
	return b
}

func newServer(t *testing.T, s *store.Store) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(New(s, "").Handler())
	t.Cleanup(srv.Close)
	return srv
}

func TestSetFinishedEndpoint(t *testing.T) {
	s := testStore(t)
	book := seedBook(t, s, "finish-hash", "Dune", "Herbert", nil)
	srv := newServer(t, s)

	put := func(finished bool) int {
		body := strings.NewReader(fmt.Sprintf(`{"finished":%t}`, finished))
		req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/books/%d/finished", srv.URL, book.ID), body)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("PUT finished: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	// Mark finished, confirm it surfaces on /library.
	if code := put(true); code != http.StatusNoContent {
		t.Fatalf("PUT finished=true status = %d, want 204", code)
	}
	if !libraryBookByID(t, srv, book.ID).Finished {
		t.Fatalf("book not reported finished after marking")
	}

	// Unmark, confirm it clears.
	if code := put(false); code != http.StatusNoContent {
		t.Fatalf("PUT finished=false status = %d, want 204", code)
	}
	if libraryBookByID(t, srv, book.ID).Finished {
		t.Fatalf("book still reported finished after unmarking")
	}

	// Unknown id → 404.
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/books/999999/finished", strings.NewReader(`{"finished":true}`))
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("PUT finished on missing book status = %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()
}

// libraryBookByID fetches /library and returns the entry with the given id.
func libraryBookByID(t *testing.T, srv *httptest.Server, id int64) bookJSON {
	t.Helper()
	resp, err := http.Get(srv.URL + "/library")
	if err != nil {
		t.Fatalf("GET /library: %v", err)
	}
	defer resp.Body.Close()
	var books []bookJSON
	if err := json.NewDecoder(resp.Body).Decode(&books); err != nil {
		t.Fatalf("decode library: %v", err)
	}
	for _, b := range books {
		if b.ID == id {
			return b
		}
	}
	t.Fatalf("book %d not found in library", id)
	return bookJSON{}
}

func TestLibrarySeriesAndAddedAt(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	filePath, _, err := s.SaveBlobs("series-hash", epubBytes, nil)
	if err != nil {
		t.Fatalf("SaveBlobs: %v", err)
	}
	member, err := s.InsertBook(ctx, store.Book{
		Title:       "Authority",
		Author:      "Jeff VanderMeer",
		FilePath:    filePath,
		FileHash:    "series-hash",
		SizeBytes:   int64(len(epubBytes)),
		Series:      "The Southern Reach",
		SeriesIndex: 2,
	})
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}
	standalone := seedBook(t, s, "solo-hash", "Piranesi", "Susanna Clarke", nil)

	srv := newServer(t, s)
	resp, err := http.Get(srv.URL + "/library")
	if err != nil {
		t.Fatalf("GET /library: %v", err)
	}
	defer resp.Body.Close()

	var books []bookJSON
	if err := json.NewDecoder(resp.Body).Decode(&books); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byID := map[int64]bookJSON{}
	for _, b := range books {
		byID[b.ID] = b
	}

	m := byID[member.ID]
	if m.Series != "The Southern Reach" {
		t.Errorf("member series = %q, want The Southern Reach", m.Series)
	}
	if m.SeriesIndex == nil || *m.SeriesIndex != 2 {
		t.Errorf("member series_index = %v, want 2", m.SeriesIndex)
	}
	if m.AddedAt == "" {
		t.Errorf("member added_at is empty, want RFC3339 timestamp")
	}
	if _, err := time.Parse(time.RFC3339, m.AddedAt); err != nil {
		t.Errorf("member added_at = %q is not RFC3339: %v", m.AddedAt, err)
	}

	solo := byID[standalone.ID]
	if solo.Series != "" {
		t.Errorf("standalone series = %q, want empty", solo.Series)
	}
	if solo.SeriesIndex != nil {
		t.Errorf("standalone series_index = %v, want nil", solo.SeriesIndex)
	}
}

func TestLibraryListing(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	withCover := seedBook(t, s, "hash-cover", "The Republic", "Plato", pngBytes)
	noCover := seedBook(t, s, "hash-nocover", "Meditations", "Marcus Aurelius", nil)

	// Give one book a reading position so progress surfaces. It must belong to the
	// owner: with user auth off, that is who the handlers serve.
	if _, err := s.UpsertPosition(ctx, store.ReadingPosition{
		BookID: withCover.ID, UserID: ownerID(ctx, t, s),
		DeviceID: "kobo-1", CFI: "/6/4!/2", Progress: 0.37,
	}); err != nil {
		t.Fatalf("UpsertPosition: %v", err)
	}

	srv := newServer(t, s)
	resp, err := http.Get(srv.URL + "/library")
	if err != nil {
		t.Fatalf("GET /library: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	var books []bookJSON
	if err := json.NewDecoder(resp.Body).Decode(&books); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(books) != 2 {
		t.Fatalf("expected 2 books, got %d: %+v", len(books), books)
	}

	byID := map[int64]bookJSON{}
	for _, b := range books {
		byID[b.ID] = b
	}

	wc := byID[withCover.ID]
	if wc.Title != "The Republic" || wc.Author != "Plato" {
		t.Fatalf("withCover metadata wrong: %+v", wc)
	}
	if wc.CoverURL != coverURL(withCover.ID) {
		t.Fatalf("withCover cover URL = %q, want %q", wc.CoverURL, coverURL(withCover.ID))
	}
	if wc.Progress == nil || *wc.Progress != 0.37 {
		t.Fatalf("withCover progress = %v, want 0.37", wc.Progress)
	}

	nc := byID[noCover.ID]
	if nc.CoverURL != "" {
		t.Fatalf("noCover cover URL = %q, want empty", nc.CoverURL)
	}
	if nc.Progress != nil {
		t.Fatalf("noCover progress = %v, want nil", nc.Progress)
	}
}

func TestLibraryEmpty(t *testing.T) {
	s := testStore(t)
	srv := newServer(t, s)

	resp, err := http.Get(srv.URL + "/library")
	if err != nil {
		t.Fatalf("GET /library: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "[]\n" {
		t.Fatalf("empty library body = %q, want []", string(body))
	}
}

func TestCoverServing(t *testing.T) {
	s := testStore(t)
	b := seedBook(t, s, "hash-cover", "The Republic", "Plato", pngBytes)
	srv := newServer(t, s)

	resp, err := http.Get(srv.URL + coverURL(b.ID))
	if err != nil {
		t.Fatalf("GET cover: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc == "" {
		t.Fatal("expected a Cache-Control header on cover")
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != string(pngBytes) {
		t.Fatalf("cover body mismatch")
	}
}

func TestCoverMissing(t *testing.T) {
	s := testStore(t)
	b := seedBook(t, s, "hash-nocover", "Meditations", "Marcus Aurelius", nil)
	srv := newServer(t, s)

	resp, err := http.Get(srv.URL + coverURL(b.ID))
	if err != nil {
		t.Fatalf("GET cover: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestFileStreaming(t *testing.T) {
	s := testStore(t)
	b := seedBook(t, s, "hash-file", "The Republic", "Plato", pngBytes)
	srv := newServer(t, s)

	resp, err := http.Get(srv.URL + "/books/" + strconv.FormatInt(b.ID, 10) + "/file")
	if err != nil {
		t.Fatalf("GET file: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/epub+zip" {
		t.Fatalf("Content-Type = %q, want application/epub+zip", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != string(epubBytes) {
		t.Fatalf("epub body mismatch")
	}
}

func TestBookNotFound(t *testing.T) {
	s := testStore(t)
	srv := newServer(t, s)

	for _, path := range []string{"/books/999999/cover", "/books/999999/file"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, resp.StatusCode)
		}
	}
}

func TestBadID(t *testing.T) {
	s := testStore(t)
	srv := newServer(t, s)

	resp, err := http.Get(srv.URL + "/books/not-a-number/cover")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
