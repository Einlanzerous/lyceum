package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

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
	// inventory_isbns and the ingest_* tables are listed explicitly rather than
	// left to CASCADE: this persistent test schema predates some of their FKs, so
	// relying on cascade let stale rows survive between tests.
	_, err := pool.Exec(ctx,
		`TRUNCATE reading_positions, book_reads, devices, inventory_isbns, inventory,
		         ingest_candidates, ingest_batches, deleted_sources, books RESTART IDENTITY CASCADE`)
	if err != nil {
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

// ownerID is the account that migration 0011 seeds and that adopts all
// pre-accounts reading history. Reading positions are per-user, so tests that
// write them directly through the store hang them off the owner — which is also
// who the API serves when user auth is off.
func ownerID(ctx context.Context, t *testing.T, s *Store) int64 {
	t.Helper()
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	return owner.ID
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
	if !reflect.DeepEqual(fetched, got) {
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

	owner := ownerID(ctx, t, s)

	book, err := s.InsertBook(ctx, sampleBook("finish-h1"))
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}
	isFinished := func(when string) bool {
		t.Helper()
		finished, err := s.IsBookFinished(ctx, book.ID, owner)
		if err != nil {
			t.Fatalf("IsBookFinished (%s): %v", when, err)
		}
		return finished
	}
	if isFinished("before marking") {
		t.Fatal("a freshly inserted book reports finished")
	}

	if err := s.SetBookFinished(ctx, book.ID, owner, true); err != nil {
		t.Fatalf("SetBookFinished(true): %v", err)
	}
	if !isFinished("after marking") {
		t.Fatal("book not finished after marking")
	}

	// Marking twice is not a conflict, and keeps the original finish date: for a
	// book the 0014 back-fill carried over, re-stamping would erase the only
	// record of when it was actually read.
	var firstMark time.Time
	if err := s.pool.QueryRow(ctx,
		`SELECT finished_at FROM book_reads WHERE book_id = $1 AND user_id = $2`,
		book.ID, owner).Scan(&firstMark); err != nil {
		t.Fatalf("read finished_at: %v", err)
	}
	if err := s.SetBookFinished(ctx, book.ID, owner, true); err != nil {
		t.Fatalf("SetBookFinished(true) again: %v", err)
	}
	if !isFinished("after re-marking") {
		t.Fatal("book not finished after re-marking")
	}
	var secondMark time.Time
	if err := s.pool.QueryRow(ctx,
		`SELECT finished_at FROM book_reads WHERE book_id = $1 AND user_id = $2`,
		book.ID, owner).Scan(&secondMark); err != nil {
		t.Fatalf("read finished_at after re-marking: %v", err)
	}
	if !secondMark.Equal(firstMark) {
		t.Fatalf("re-marking moved the finish date from %v to %v", firstMark, secondMark)
	}

	if err := s.SetBookFinished(ctx, book.ID, owner, false); err != nil {
		t.Fatalf("SetBookFinished(false): %v", err)
	}
	if isFinished("after unmarking") {
		t.Fatal("book still finished after unmarking")
	}

	// Unmarking a book that was never marked is a no-op, not an error.
	if err := s.SetBookFinished(ctx, book.ID, owner, false); err != nil {
		t.Fatalf("SetBookFinished(false) on an unmarked book: %v", err)
	}

	// A missing book id is ErrNotFound in both directions. The clear path is the
	// interesting one: nothing was deleted either way, so only the books lookup
	// tells "no such book" apart from "was not marked".
	if err := s.SetBookFinished(ctx, 999999, owner, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetBookFinished(missing, true) err = %v, want ErrNotFound", err)
	}
	if err := s.SetBookFinished(ctx, 999999, owner, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetBookFinished(missing, false) err = %v, want ErrNotFound", err)
	}
	if finished, err := s.IsBookFinished(ctx, 999999, owner); err != nil || finished {
		t.Fatalf("IsBookFinished(missing) = %v, %v; want false, nil", finished, err)
	}
}

func TestPositionUpsertAndGet(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	owner := ownerID(ctx, t, s)

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
			pos:      ReadingPosition{BookID: book.ID, UserID: owner, DeviceID: "kobo-1", CFI: "/6/4!/2", Progress: 0.1},
			wantCFI:  "/6/4!/2",
			wantProg: 0.1,
		},
		{
			name:     "update same device",
			pos:      ReadingPosition{BookID: book.ID, UserID: owner, DeviceID: "kobo-1", CFI: "/6/8!/4", Progress: 0.42},
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

			got, err := s.GetPosition(ctx, book.ID, owner, tc.pos.DeviceID)
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
	if _, err := s.GetPosition(context.Background(), 1, 1, "ghost"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetFurthestPosition(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	owner := ownerID(ctx, t, s)

	book, err := s.InsertBook(ctx, sampleBook("furthest-hash"))
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}

	if _, err := s.GetFurthestPosition(ctx, book.ID, owner); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound with no positions, got %v", err)
	}

	// device-a read furthest (page 90).
	if _, err := s.UpsertPosition(ctx, ReadingPosition{
		BookID: book.ID, UserID: owner, DeviceID: "device-a", CFI: "/90", Progress: 0.9,
	}); err != nil {
		t.Fatalf("upsert a: %v", err)
	}
	// device-b wrote LATER but at the very start (e.g. a still-open reader that
	// flushed a pre-pagination progress=0 on navigation). It must NOT win.
	if _, err := s.UpsertPosition(ctx, ReadingPosition{
		BookID: book.ID, UserID: owner, DeviceID: "device-b", CFI: "/2", Progress: 0,
	}); err != nil {
		t.Fatalf("upsert b: %v", err)
	}

	pos, err := s.GetFurthestPosition(ctx, book.ID, owner)
	if err != nil {
		t.Fatalf("GetFurthestPosition: %v", err)
	}
	if pos.DeviceID != "device-a" || pos.Progress != 0.9 {
		t.Fatalf("furthest = %q @ %v, want device-a @ 0.9 (recency must not override progress)",
			pos.DeviceID, pos.Progress)
	}
}

// TestFurthestPositionsMatchSingle is the anti-drift test for LYCM-115:
// ListFurthestPositions answers the same question as GetFurthestPosition in
// bulk, and the two ORDER BYs must not diverge. The seeded rows are the case
// where a naive batch query goes wrong — the most recent write is the least far
// along, so ordering by recency picks a different row than ordering by progress,
// and only one of those is the resume anchor.
func TestFurthestPositionsMatchSingle(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	owner := ownerID(ctx, t, s)

	// Several books, so DISTINCT ON has to partition rather than just sort.
	var books []Book
	for _, h := range []string{"batch-pos-a", "batch-pos-b", "batch-pos-c"} {
		b, err := s.InsertBook(ctx, sampleBook(h))
		if err != nil {
			t.Fatalf("InsertBook %s: %v", h, err)
		}
		books = append(books, b)
	}
	// An untouched book must be absent from the map, not present-and-zero: that
	// is the bulk spelling of ErrNotFound, and the shelf renders "not started"
	// from its absence.
	untouched, err := s.InsertBook(ctx, sampleBook("batch-pos-untouched"))
	if err != nil {
		t.Fatalf("InsertBook untouched: %v", err)
	}

	for _, b := range books {
		if _, err := s.UpsertPosition(ctx, ReadingPosition{
			BookID: b.ID, UserID: owner, DeviceID: "device-a", CFI: "/90", Progress: 0.9,
		}); err != nil {
			t.Fatalf("upsert a: %v", err)
		}
		// Later write, earlier spot — the pre-pagination progress=0 flush.
		if _, err := s.UpsertPosition(ctx, ReadingPosition{
			BookID: b.ID, UserID: owner, DeviceID: "device-b", CFI: "/2", Progress: 0,
		}); err != nil {
			t.Fatalf("upsert b: %v", err)
		}
	}

	got, err := s.ListFurthestPositions(ctx, owner)
	if err != nil {
		t.Fatalf("ListFurthestPositions: %v", err)
	}
	if len(got) != len(books) {
		t.Fatalf("got %d positions, want %d", len(got), len(books))
	}
	if _, ok := got[untouched.ID]; ok {
		t.Error("a book with no positions is present in the map; absence is how the shelf reads 'not started'")
	}
	for _, b := range books {
		want, err := s.GetFurthestPosition(ctx, b.ID, owner)
		if err != nil {
			t.Fatalf("GetFurthestPosition(%d): %v", b.ID, err)
		}
		switch g := got[b.ID]; {
		case g.ID != want.ID:
			t.Errorf("book %d: batch picked position %d (%s @ %v), single picked %d (%s @ %v)",
				b.ID, g.ID, g.DeviceID, g.Progress, want.ID, want.DeviceID, want.Progress)
		case g.DeviceID != "device-a" || g.Progress != 0.9:
			t.Errorf("book %d: batch = %q @ %v, want device-a @ 0.9", b.ID, g.DeviceID, g.Progress)
		case g.CFI != want.CFI || !g.UpdatedAt.Equal(want.UpdatedAt):
			t.Errorf("book %d: batch row differs from single beyond identity", b.ID)
		}
	}
}

// TestListFurthestPositionsPerUser: the batch sweep is scoped to one reader, the
// property LYCM-801 established for the single-book form. Reading someone else's
// map would put a housemate's bookmarks on your shelf.
func TestListFurthestPositionsPerUser(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	owner := ownerID(ctx, t, s)

	book, err := s.InsertBook(ctx, sampleBook("batch-pos-scope"))
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}
	mara, err := s.CreateUser(ctx, "mara@example.com", "Mara")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := s.UpsertPosition(ctx, ReadingPosition{
		BookID: book.ID, UserID: owner, DeviceID: "d", CFI: "/50", Progress: 0.5,
	}); err != nil {
		t.Fatalf("upsert owner: %v", err)
	}

	maraPositions, err := s.ListFurthestPositions(ctx, mara.ID)
	if err != nil {
		t.Fatalf("ListFurthestPositions(mara): %v", err)
	}
	if len(maraPositions) != 0 {
		t.Errorf("mara sees %d positions; the owner's reading is not hers", len(maraPositions))
	}
}

// TestListFinishedBooks covers the batch form of IsBookFinished, including the
// per-user scoping that is the whole point of LYCM-112.
func TestListFinishedBooks(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	owner := ownerID(ctx, t, s)

	read, err := s.InsertBook(ctx, sampleBook("batch-fin-read"))
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}
	unread, err := s.InsertBook(ctx, sampleBook("batch-fin-unread"))
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}
	mara, err := s.CreateUser(ctx, "mara@example.com", "Mara")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	empty, err := s.ListFinishedBooks(ctx, owner)
	if err != nil {
		t.Fatalf("ListFinishedBooks (none marked): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("got %d finished books before marking any, want 0", len(empty))
	}

	if err := s.SetBookFinished(ctx, read.ID, owner, true); err != nil {
		t.Fatalf("SetBookFinished: %v", err)
	}

	got, err := s.ListFinishedBooks(ctx, owner)
	if err != nil {
		t.Fatalf("ListFinishedBooks: %v", err)
	}
	if !got[read.ID] {
		t.Error("marked book missing from the owner's finished set")
	}
	if got[unread.ID] {
		t.Error("unmarked book reported finished")
	}
	if len(got) != 1 {
		t.Errorf("owner has %d finished books, want 1", len(got))
	}

	maraFinished, err := s.ListFinishedBooks(ctx, mara.ID)
	if err != nil {
		t.Fatalf("ListFinishedBooks(mara): %v", err)
	}
	if len(maraFinished) != 0 {
		t.Errorf("mara has %d finished books; the owner's read is not hers", len(maraFinished))
	}

	// Un-marking deletes the row, so the id leaves the set entirely.
	if err := s.SetBookFinished(ctx, read.ID, owner, false); err != nil {
		t.Fatalf("SetBookFinished(false): %v", err)
	}
	after, err := s.ListFinishedBooks(ctx, owner)
	if err != nil {
		t.Fatalf("ListFinishedBooks after un-marking: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("un-marked book still in the finished set: %v", after)
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
