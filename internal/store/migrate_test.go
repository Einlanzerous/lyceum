package store

import (
	"context"
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/magos/lyceum/migrations"
)

// testSchema isolates this package's test binary in its own Postgres schema so
// that `go test ./...`, which runs package test binaries in parallel against the
// single TEST_DATABASE_URL database, does not race other packages on shared
// tables (migrations, TRUNCATE, row counts).
const testSchema = "lyceum_test_store"

// testPool connects to TEST_DATABASE_URL or skips the test when it is unset. The
// returned pool is pinned to testSchema via search_path.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping DB-backed test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := connectSchema(ctx, dsn, testSchema)
	if err != nil {
		t.Fatalf("connectSchema: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// connectSchema opens a pool whose every connection uses the given schema as its
// search_path, creating the schema first. Setting search_path to a not-yet-
// existing schema is permitted by Postgres, so CREATE SCHEMA on the first
// connection succeeds before any object resolution happens.
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

// cleanState drops every Lyceum-managed table so Migrate starts from scratch.
func cleanState(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		DROP TABLE IF EXISTS reading_positions, book_reads, devices, inventory, deliveries, books, schema_migrations CASCADE`)
	if err != nil {
		t.Fatalf("clean state: %v", err)
	}
}

func tableExists(ctx context.Context, t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var exists bool
	// to_regclass resolves the name through the connection's search_path, so it
	// reflects the per-binary test schema rather than a hardcoded "public".
	err := pool.QueryRow(ctx,
		`SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists)
	if err != nil {
		t.Fatalf("check table %q: %v", name, err)
	}
	return exists
}

// columnExists reports whether a table in the test schema has a given column.
func columnExists(ctx context.Context, t *testing.T, pool *pgxpool.Pool, table, column string) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			 WHERE table_schema = current_schema()
			   AND table_name = $1 AND column_name = $2)`, table, column).Scan(&exists)
	if err != nil {
		t.Fatalf("check column %s.%s: %v", table, column, err)
	}
	return exists
}

func TestMigrate(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cleanState(ctx, t, pool)

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	for _, tbl := range []string{"books", "devices", "reading_positions", "inventory", "schema_migrations"} {
		if !tableExists(ctx, t, pool, tbl) {
			t.Errorf("expected table %q to exist after Migrate", tbl)
		}
	}

	// books.title is the positive control: every other columnExists assertion in
	// this file passes when the column is absent, so a helper that always said
	// "no" would satisfy them all without testing anything.
	if !columnExists(ctx, t, pool, "books", "title") {
		t.Error("columnExists reports books.title missing; the helper is broken")
	}
	// 0015 retires books.finished_at; book_reads carries mark-as-read now, and a
	// column nothing reads is one a later query can quietly start trusting.
	if columnExists(ctx, t, pool, "books", "finished_at") {
		t.Error("books.finished_at still exists after Migrate; 0015 should have dropped it")
	}

	// schema_migrations should record version 0001.
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM schema_migrations WHERE version = '0001'`).Scan(&n); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if n != 1 {
		t.Errorf("expected exactly 1 row for version 0001, got %d", n)
	}
}

func TestMigrateIdempotent(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cleanState(ctx, t, pool)

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	// Running again must be a no-op, not an error.
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}

	// Two runs must apply each embedded migration exactly once. Derive the
	// expected count from the embedded set so adding a migration doesn't make
	// this assertion stale.
	want, err := countUpMigrations()
	if err != nil {
		t.Fatalf("countUpMigrations: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if n != want {
		t.Errorf("expected %d applied migrations after two runs, got %d", want, n)
	}
}

// countUpMigrations counts the embedded *.up.sql files, the canonical number of
// migrations Migrate should apply.
func countUpMigrations() (int, error) {
	migs, err := loadMigrations()
	if err != nil {
		return 0, err
	}
	return len(migs), nil
}

// TestBookReadsBackfill runs migration 0014 over the state every existing
// install upgrades from: books marked read the old, library-wide way (LYCM-112).
// Those marks belong to the owner, the only person the pre-accounts flag could
// have meant — and to nobody else, or the fix would hand every housemate a shelf
// full of books they never read.
func TestBookReadsBackfill(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cleanState(ctx, t, pool)
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// cleanState leaves users standing (0011 seeds the owner and the schema
	// insists on exactly one), so clear the housemates a previous case in this
	// persistent schema left behind.
	truncateAll(ctx, t, pool)

	s := New(pool, t.TempDir())
	read, err := s.InsertBook(ctx, sampleBook("backfill-read-hash"))
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}
	unread, err := s.InsertBook(ctx, sampleBook("backfill-unread-hash"))
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}
	mara, err := s.CreateUser(ctx, "mara@example.com", "Mara")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Rewind to just before 0014: one book marked on books.finished_at, and no
	// book_reads table at all. 0015 has since dropped that column, so putting it
	// back is part of the rewind, and both versions have to be unrecorded for
	// Migrate to replay the pair.
	if _, err := pool.Exec(ctx,
		`ALTER TABLE books ADD COLUMN IF NOT EXISTS finished_at TIMESTAMPTZ`); err != nil {
		t.Fatalf("restore legacy finished_at column: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE books SET finished_at = now() WHERE id = $1`, read.ID); err != nil {
		t.Fatalf("seed legacy finished_at: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP TABLE book_reads`); err != nil {
		t.Fatalf("drop book_reads: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM schema_migrations WHERE version IN ('0014', '0015')`); err != nil {
		t.Fatalf("unrecord 0014/0015: %v", err)
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("re-Migrate: %v", err)
	}

	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	if finished, err := s.IsBookFinished(ctx, read.ID, owner.ID); err != nil || !finished {
		t.Fatalf("owner finished = %v (err %v) for a book marked read before 0014; want true", finished, err)
	}
	if finished, err := s.IsBookFinished(ctx, unread.ID, owner.ID); err != nil || finished {
		t.Fatalf("owner finished = %v (err %v) for a never-marked book; want false", finished, err)
	}
	if finished, err := s.IsBookFinished(ctx, read.ID, mara.ID); err != nil || finished {
		t.Fatalf("mara finished = %v (err %v); the back-fill handed her the owner's read", finished, err)
	}

	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM book_reads`).Scan(&rows); err != nil {
		t.Fatalf("count book_reads: %v", err)
	}
	if rows != 1 {
		t.Fatalf("back-fill wrote %d rows, want 1", rows)
	}

	// 0015 follows 0014 in the same replay, so the column it drops must be gone
	// again — and 0014 must have read it before that happened, which the
	// back-fill assertions above just showed.
	if columnExists(ctx, t, pool, "books", "finished_at") {
		t.Error("books.finished_at survived the replay; 0015 should follow 0014")
	}
}

// runDown applies one embedded *.down.sql. Migrate loads *.up.sql only, so this
// is the sole path by which a down migration is ever executed in this repo.
func runDown(ctx context.Context, t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()
	body, err := fs.ReadFile(migrations.FS, name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if _, err := pool.Exec(ctx, string(body)); err != nil {
		t.Fatalf("run %s: %v", name, err)
	}
}

// TestDropFinishedAtDown exercises 0015's down, the one down migration in the
// repo that moves data rather than dropping what its up created. Nothing runs
// the downs — so untested, its first execution would be by hand, against a live
// database, mid-rollback. It has to carry the owner's reads back into the column
// and leave everyone else's behind, and 0014's down after it has to find them
// there.
func TestDropFinishedAtDown(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cleanState(ctx, t, pool)
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	truncateAll(ctx, t, pool)

	// The downs leave the schema behind the versions schema_migrations records,
	// which Migrate would then skip straight past. Rebuild for whatever runs
	// next in this persistent schema.
	t.Cleanup(func() {
		cleanState(ctx, t, pool)
		if err := Migrate(ctx, pool); err != nil {
			t.Fatalf("restore schema after down migrations: %v", err)
		}
	})

	s := New(pool, t.TempDir())
	ownerRead, err := s.InsertBook(ctx, sampleBook("down-owner-hash"))
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}
	maraRead, err := s.InsertBook(ctx, sampleBook("down-mara-hash"))
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}
	unread, err := s.InsertBook(ctx, sampleBook("down-unread-hash"))
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	mara, err := s.CreateUser(ctx, "mara@example.com", "Mara")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.SetBookFinished(ctx, ownerRead.ID, owner.ID, true); err != nil {
		t.Fatalf("SetBookFinished owner: %v", err)
	}
	if err := s.SetBookFinished(ctx, maraRead.ID, mara.ID, true); err != nil {
		t.Fatalf("SetBookFinished mara: %v", err)
	}

	var want time.Time
	if err := pool.QueryRow(ctx,
		`SELECT finished_at FROM book_reads WHERE book_id = $1 AND user_id = $2`,
		ownerRead.ID, owner.ID).Scan(&want); err != nil {
		t.Fatalf("read owner's finish date: %v", err)
	}

	runDown(ctx, t, pool, "0015_drop_book_finished.down.sql")

	if !columnExists(ctx, t, pool, "books", "finished_at") {
		t.Fatal("0015's down did not restore books.finished_at")
	}
	// The owner's own finish date, not the moment of the rollback: that date is
	// the answer to "when did you finish this", and re-stamping it loses the
	// only record of one.
	if got := finishedAt(ctx, t, pool, ownerRead.ID); got == nil || !got.Equal(want) {
		t.Errorf("owner's book finished_at = %v, want %v", got, want)
	}
	// Mara's read must not become the library-wide flag — that is LYCM-112's bug
	// reintroduced by the rollback that was supposed to be safe.
	if got := finishedAt(ctx, t, pool, maraRead.ID); got != nil {
		t.Errorf("a non-owner's read leaked into the shared flag: finished_at = %v, want NULL", got)
	}
	if got := finishedAt(ctx, t, pool, unread.ID); got != nil {
		t.Errorf("never-read book finished_at = %v, want NULL", got)
	}

	// 0014's down completes the unwind to the pre-LYCM-112 shape: book_reads
	// gone, the marks still standing in the column 0015's down refilled.
	runDown(ctx, t, pool, "0014_book_reads.down.sql")

	if tableExists(ctx, t, pool, "book_reads") {
		t.Error("0014's down left book_reads standing")
	}
	if got := finishedAt(ctx, t, pool, ownerRead.ID); got == nil || !got.Equal(want) {
		t.Errorf("after both downs, owner's book finished_at = %v, want %v", got, want)
	}
}

// finishedAt reads books.finished_at, which exists only between 0015's down and
// 0014's down. A nil return is SQL NULL: not finished.
func finishedAt(ctx context.Context, t *testing.T, pool *pgxpool.Pool, bookID int64) *time.Time {
	t.Helper()
	var at *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT finished_at FROM books WHERE id = $1`, bookID).Scan(&at); err != nil {
		t.Fatalf("read books.finished_at for %d: %v", bookID, err)
	}
	return at
}

func TestConnectBadDSN(t *testing.T) {
	ctx := context.Background()
	if _, err := Connect(ctx, "://not-a-dsn"); err == nil {
		t.Fatal("expected error for malformed DSN, got nil")
	}
}
