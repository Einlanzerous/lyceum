package main

import (
	"bytes"
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/magos/lyceum/internal/store"
)

// testServiceTokenStore returns a migrated store in a schema of its own, or skips
// without TEST_DATABASE_URL.
func testServiceTokenStore(t *testing.T) (*store.Store, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping DB-backed test")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	const schema = "lyceum_test_cmd"
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+schema); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM service_tokens; DELETE FROM audit_log`); err != nil {
		t.Fatal(err)
	}
	return store.New(pool, t.TempDir()), pool
}

func runCmd(t *testing.T, st *store.Store, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, info bytes.Buffer
	err = serviceTokenCmd(context.Background(), st, args, &out, &info, "cli:test")
	return out.String(), info.String(), err
}

func TestServiceTokenCLI(t *testing.T) {
	st, pool := testServiceTokenStore(t)
	ctx := context.Background()

	// mint: the token alone on stdout, lys_-prefixed, hash-only in the database.
	out, info, err := runCmd(t, st, "mint", "--label", "satlink", "--scope", "admin:read")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	tok := strings.TrimSpace(out)
	if !strings.HasPrefix(tok, "lys_") || strings.ContainsAny(tok, " \n") {
		t.Fatalf("stdout is not a bare lys_ token: %q", out)
	}
	if strings.Contains(info, tok) {
		t.Fatal("the token was also written to the human-readable stream")
	}
	var stored int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM service_tokens WHERE token_hash = encode(sha256($1::bytea), 'hex')`, tok).Scan(&stored); err != nil || stored != 1 {
		t.Fatalf("hex SHA-256 row count = %d (%v), want 1", stored, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM service_tokens WHERE token_hash = $1`, tok).Scan(&stored); err != nil || stored != 0 {
		t.Fatalf("plaintext stored: %d rows (%v)", stored, err)
	}

	// An unknown scope is refused, and a missing label.
	if _, _, err := runCmd(t, st, "mint", "--label", "x", "--scope", "root"); err == nil {
		t.Fatal("unknown scope accepted")
	}
	if _, _, err := runCmd(t, st, "mint", "--scope", "admin:read"); err == nil {
		t.Fatal("missing label accepted")
	}

	// A second mint for a live label succeeds (rotation).
	out2, _, err := runCmd(t, st, "mint", "--label", "satlink", "--scope", "admin:read")
	if err != nil {
		t.Fatalf("rotation mint: %v", err)
	}
	tok2 := strings.TrimSpace(out2)

	// list: columns, never a hash or plaintext.
	listed, _, err := runCmd(t, st, "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ID", "LABEL", "SCOPE", "CREATED", "LAST USED", "REVOKED", "satlink", "admin:read"} {
		if !strings.Contains(listed, want) {
			t.Errorf("list output lacks %q:\n%s", want, listed)
		}
	}
	var hash string
	if err := pool.QueryRow(ctx, `SELECT token_hash FROM service_tokens LIMIT 1`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(listed, hash) || strings.Contains(listed, tok) || strings.Contains(listed, tok2) {
		t.Fatalf("list leaks a hash or plaintext:\n%s", listed)
	}

	// revoke --id keeps the row; revoke needs exactly one selector.
	if _, _, err := runCmd(t, st, "revoke"); err == nil {
		t.Fatal("revoke with no selector accepted")
	}
	if _, _, err := runCmd(t, st, "revoke", "--id", "1", "--label", "satlink"); err == nil {
		t.Fatal("revoke with both selectors accepted")
	}
	var firstID int64
	if err := pool.QueryRow(ctx, `SELECT min(id) FROM service_tokens`).Scan(&firstID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runCmd(t, st, "revoke", "--id", strconv.FormatInt(firstID, 10)); err != nil {
		t.Fatal(err)
	}
	var total, live int
	_ = pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE revoked_at IS NULL) FROM service_tokens`).Scan(&total, &live)
	if total != 2 || live != 1 {
		t.Fatalf("after revoke --id: %d rows, %d live; want 2 and 1", total, live)
	}

	// revoke --label revokes every live token with that label.
	if _, _, err := runCmd(t, st, "revoke", "--label", "satlink"); err != nil {
		t.Fatal(err)
	}
	_ = pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE revoked_at IS NULL) FROM service_tokens`).Scan(&live)
	if live != 0 {
		t.Fatalf("%d tokens still live after revoke --label", live)
	}
	if _, _, err := runCmd(t, st, "revoke", "--label", "satlink"); err == nil {
		t.Fatal("revoking a label with nothing live reported success")
	}

	// Each mint and revoke left an audit row naming the OS-user actor.
	var mints, revokes int
	_ = pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE action='service_token.mint'), count(*) FILTER (WHERE action='service_token.revoke') FROM audit_log WHERE actor = 'cli:test'`).Scan(&mints, &revokes)
	if mints != 2 || revokes != 2 {
		t.Fatalf("audit rows: %d mint, %d revoke; want 2 and 2", mints, revokes)
	}

	if _, _, err := runCmd(t, st); err == nil {
		t.Fatal("no subcommand accepted")
	}
}
