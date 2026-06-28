package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
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
		`TRUNCATE reading_positions, devices, books RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
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

func TestLibraryListing(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	withCover := seedBook(t, s, "hash-cover", "The Republic", "Plato", pngBytes)
	noCover := seedBook(t, s, "hash-nocover", "Meditations", "Marcus Aurelius", nil)

	// Give one book a reading position so progress surfaces.
	if _, err := s.UpsertPosition(ctx, store.ReadingPosition{
		BookID: withCover.ID, DeviceID: "kobo-1", CFI: "/6/4!/2", Progress: 0.37,
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
