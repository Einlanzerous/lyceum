package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newStore returns a migrated Store backed by TEST_DATABASE_URL with all
// data tables truncated. It skips when the test database is unavailable.
func newStore(t *testing.T) *Store {
	t.Helper()
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	truncateAll(ctx, t, pool)
	return New(pool, t.TempDir())
}

// truncateAll empties the data tables between cases, leaving the schema (and
// schema_migrations) intact. RESTART IDENTITY resets the BIGINT id sequences.
func truncateAll(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx,
		`TRUNCATE reading_positions, devices, books RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func sampleBook(hash string) Book {
	return Book{
		Title:     "The Republic",
		Author:    "Plato",
		CoverPath: "",
		FilePath:  "/data/" + hash + "/book.epub",
		FileHash:  hash,
		SizeBytes: 1234,
	}
}

func TestInsertAndGetBook(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	in := sampleBook("hash-aaa")
	got, err := s.InsertBook(ctx, in)
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}
	if got.ID == 0 {
		t.Fatal("expected non-zero id")
	}
	if got.Title != in.Title || got.Author != in.Author || got.FileHash != in.FileHash {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.AddedAt.IsZero() {
		t.Error("expected AddedAt to be set")
	}

	fetched, err := s.GetBook(ctx, got.ID)
	if err != nil {
		t.Fatalf("GetBook: %v", err)
	}
	if fetched != got {
		t.Fatalf("GetBook mismatch: got %+v want %+v", fetched, got)
	}
}

func TestGetBookNotFound(t *testing.T) {
	s := newStore(t)
	if _, err := s.GetBook(context.Background(), 999999); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestInsertBookIdempotentOnHash(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	first, err := s.InsertBook(ctx, sampleBook("dup-hash"))
	if err != nil {
		t.Fatalf("first InsertBook: %v", err)
	}

	// Same hash, different metadata: must return the original row, not insert.
	dup := sampleBook("dup-hash")
	dup.Title = "A Different Title"
	dup.Author = "Someone Else"
	second, err := s.InsertBook(ctx, dup)
	if err != nil {
		t.Fatalf("second InsertBook: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected same id %d, got %d", first.ID, second.ID)
	}
	if second.Title != first.Title {
		t.Fatalf("expected original title %q, got %q", first.Title, second.Title)
	}

	books, err := s.ListBooks(ctx)
	if err != nil {
		t.Fatalf("ListBooks: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("expected exactly 1 book after duplicate insert, got %d", len(books))
	}
}

func TestListBooks(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	cases := []Book{
		sampleBook("h1"),
		sampleBook("h2"),
		sampleBook("h3"),
	}
	for _, b := range cases {
		if _, err := s.InsertBook(ctx, b); err != nil {
			t.Fatalf("InsertBook(%s): %v", b.FileHash, err)
		}
	}

	books, err := s.ListBooks(ctx)
	if err != nil {
		t.Fatalf("ListBooks: %v", err)
	}
	if len(books) != len(cases) {
		t.Fatalf("expected %d books, got %d", len(cases), len(books))
	}
}

func TestPositionUpsertAndGet(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	book, err := s.InsertBook(ctx, sampleBook("pos-hash"))
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}

	tests := []struct {
		name     string
		pos      ReadingPosition
		wantCFI  string
		wantProg float64
	}{
		{
			name:     "insert",
			pos:      ReadingPosition{BookID: book.ID, DeviceID: "kobo-1", CFI: "/6/4!/2", Progress: 0.1},
			wantCFI:  "/6/4!/2",
			wantProg: 0.1,
		},
		{
			name:     "update same device",
			pos:      ReadingPosition{BookID: book.ID, DeviceID: "kobo-1", CFI: "/6/8!/4", Progress: 0.42},
			wantCFI:  "/6/8!/4",
			wantProg: 0.42,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			saved, err := s.UpsertPosition(ctx, tc.pos)
			if err != nil {
				t.Fatalf("UpsertPosition: %v", err)
			}
			if saved.CFI != tc.wantCFI || saved.Progress != tc.wantProg {
				t.Fatalf("saved = %+v, want cfi=%q prog=%v", saved, tc.wantCFI, tc.wantProg)
			}

			got, err := s.GetPosition(ctx, book.ID, tc.pos.DeviceID)
			if err != nil {
				t.Fatalf("GetPosition: %v", err)
			}
			if got.CFI != tc.wantCFI || got.Progress != tc.wantProg {
				t.Fatalf("GetPosition = %+v, want cfi=%q prog=%v", got, tc.wantCFI, tc.wantProg)
			}
		})
	}

	// After both upserts on kobo-1 there must still be exactly one row.
	var n int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM reading_positions WHERE book_id = $1`, book.ID).Scan(&n); err != nil {
		t.Fatalf("count positions: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 position row after upserts, got %d", n)
	}
}

func TestGetPositionNotFound(t *testing.T) {
	s := newStore(t)
	if _, err := s.GetPosition(context.Background(), 1, "ghost"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetLatestPosition(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	book, err := s.InsertBook(ctx, sampleBook("latest-hash"))
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}

	if _, err := s.GetLatestPosition(ctx, book.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound with no positions, got %v", err)
	}

	if _, err := s.UpsertPosition(ctx, ReadingPosition{
		BookID: book.ID, DeviceID: "device-a", CFI: "/2", Progress: 0.2,
	}); err != nil {
		t.Fatalf("upsert a: %v", err)
	}
	// device-b updates later, so it should be the latest.
	if _, err := s.UpsertPosition(ctx, ReadingPosition{
		BookID: book.ID, DeviceID: "device-b", CFI: "/9", Progress: 0.9,
	}); err != nil {
		t.Fatalf("upsert b: %v", err)
	}

	latest, err := s.GetLatestPosition(ctx, book.ID)
	if err != nil {
		t.Fatalf("GetLatestPosition: %v", err)
	}
	if latest.DeviceID != "device-b" {
		t.Fatalf("expected latest device-b, got %q", latest.DeviceID)
	}
}

func TestSaveBlobs(t *testing.T) {
	s := newStore(t)

	epub := []byte("PK\x03\x04 fake epub bytes")
	cover := []byte("\xff\xd8\xff fake jpeg")

	filePath, coverPath, err := s.SaveBlobs("blob-hash", epub, cover)
	if err != nil {
		t.Fatalf("SaveBlobs: %v", err)
	}

	gotEpub, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read epub: %v", err)
	}
	if string(gotEpub) != string(epub) {
		t.Fatalf("epub bytes mismatch")
	}
	gotCover, err := os.ReadFile(coverPath)
	if err != nil {
		t.Fatalf("read cover: %v", err)
	}
	if string(gotCover) != string(cover) {
		t.Fatalf("cover bytes mismatch")
	}

	// Paths must be namespaced under dataDir/<hash>.
	if filepath.Base(filepath.Dir(filePath)) != "blob-hash" {
		t.Fatalf("expected hash-namespaced path, got %q", filePath)
	}

	// No cover supplied -> empty cover path.
	_, noCover, err := s.SaveBlobs("blob-hash-2", epub, nil)
	if err != nil {
		t.Fatalf("SaveBlobs no cover: %v", err)
	}
	if noCover != "" {
		t.Fatalf("expected empty cover path, got %q", noCover)
	}
}
