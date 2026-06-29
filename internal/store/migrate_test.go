package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
		DROP TABLE IF EXISTS reading_positions, devices, inventory, deliveries, books, schema_migrations CASCADE`)
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

func TestConnectBadDSN(t *testing.T) {
	ctx := context.Background()
	if _, err := Connect(ctx, "://not-a-dsn"); err == nil {
		t.Fatal("expected error for malformed DSN, got nil")
	}
}
