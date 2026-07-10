package store

import (
	"context"
	"errors"
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
		`TRUNCATE reading_positions, devices, inventory, books RESTART IDENTITY CASCADE`)
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

func TestBookSeriesRoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	// A book with series metadata round-trips through insert.
	withSeries := sampleBook("series-h1")
	withSeries.Series = "The Southern Reach"
	withSeries.SeriesIndex = 2
	saved, err := s.InsertBook(ctx, withSeries)
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}
	if saved.Series != "The Southern Reach" || saved.SeriesIndex != 2 {
		t.Fatalf("insert series = (%q, %v), want (The Southern Reach, 2)", saved.Series, saved.SeriesIndex)
	}

	// A standalone book stores NULLs and reads back as ("", 0).
	standalone, err := s.InsertBook(ctx, sampleBook("series-h2"))
	if err != nil {
		t.Fatalf("InsertBook standalone: %v", err)
	}
	if standalone.Series != "" || standalone.SeriesIndex != 0 {
		t.Fatalf("standalone series = (%q, %v), want empty", standalone.Series, standalone.SeriesIndex)
	}

	// A re-stamp (UpdateBookContent) refreshes series metadata.
	refreshed := sampleBook("series-h3-new")
	refreshed.Series = "Earthsea"
	refreshed.SeriesIndex = 1
	updated, err := s.UpdateBookContent(ctx, saved.ID, refreshed)
	if err != nil {
		t.Fatalf("UpdateBookContent: %v", err)
	}
	if updated.Series != "Earthsea" || updated.SeriesIndex != 1 {
		t.Fatalf("updated series = (%q, %v), want (Earthsea, 1)", updated.Series, updated.SeriesIndex)
	}
}

func TestUpdateBookSeries(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	book, err := s.InsertBook(ctx, sampleBook("set-series-h1"))
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}

	// Assign a series + index.
	got, err := s.UpdateBookSeries(ctx, book.ID, "The Broken Empire", 2)
	if err != nil {
		t.Fatalf("UpdateBookSeries: %v", err)
	}
	if got.Series != "The Broken Empire" || got.SeriesIndex != 2 {
		t.Fatalf("set series = (%q, %v), want (The Broken Empire, 2)", got.Series, got.SeriesIndex)
	}

	// Clearing (empty name, 0 index) stores NULLs and reads back empty.
	cleared, err := s.UpdateBookSeries(ctx, book.ID, "", 0)
	if err != nil {
		t.Fatalf("UpdateBookSeries clear: %v", err)
	}
	if cleared.Series != "" || cleared.SeriesIndex != 0 {
		t.Fatalf("cleared series = (%q, %v), want empty", cleared.Series, cleared.SeriesIndex)
	}

	// A missing id is ErrNotFound.
	if _, err := s.UpdateBookSeries(ctx, 999999, "X", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateBookSeries(missing) err = %v, want ErrNotFound", err)
	}
}

func TestSetBookFinished(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	book, err := s.InsertBook(ctx, sampleBook("finish-h1"))
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}
	if book.FinishedAt != nil {
		t.Fatalf("new book FinishedAt = %v, want nil", book.FinishedAt)
	}

	done, err := s.SetBookFinished(ctx, book.ID, true)
	if err != nil {
		t.Fatalf("SetBookFinished(true): %v", err)
	}
	if done.FinishedAt == nil {
		t.Fatalf("marked book FinishedAt = nil, want a timestamp")
	}

	cleared, err := s.SetBookFinished(ctx, book.ID, false)
	if err != nil {
		t.Fatalf("SetBookFinished(false): %v", err)
	}
	if cleared.FinishedAt != nil {
		t.Fatalf("unmarked book FinishedAt = %v, want nil", cleared.FinishedAt)
	}

	if _, err := s.SetBookFinished(ctx, 999999, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetBookFinished(missing) err = %v, want ErrNotFound", err)
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

func TestGetFurthestPosition(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	book, err := s.InsertBook(ctx, sampleBook("furthest-hash"))
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}

	if _, err := s.GetFurthestPosition(ctx, book.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound with no positions, got %v", err)
	}

	// device-a read furthest (page 90).
	if _, err := s.UpsertPosition(ctx, ReadingPosition{
		BookID: book.ID, DeviceID: "device-a", CFI: "/90", Progress: 0.9,
	}); err != nil {
		t.Fatalf("upsert a: %v", err)
	}
	// device-b wrote LATER but at the very start (e.g. a still-open reader that
	// flushed a pre-pagination progress=0 on navigation). It must NOT win.
	if _, err := s.UpsertPosition(ctx, ReadingPosition{
		BookID: book.ID, DeviceID: "device-b", CFI: "/2", Progress: 0,
	}); err != nil {
		t.Fatalf("upsert b: %v", err)
	}

	pos, err := s.GetFurthestPosition(ctx, book.ID)
	if err != nil {
		t.Fatalf("GetFurthestPosition: %v", err)
	}
	if pos.DeviceID != "device-a" || pos.Progress != 0.9 {
		t.Fatalf("furthest = %q @ %v, want device-a @ 0.9 (recency must not override progress)",
			pos.DeviceID, pos.Progress)
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
