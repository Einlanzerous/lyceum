package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/magos/lyceum/migrations"
)

// 0018 applies, its down drops both tables, and the up replays on top of that.
func TestServiceTokensMigrationUpAndDown(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cleanState(ctx, t, pool)
	t.Cleanup(func() {
		cleanState(ctx, t, pool)
		if err := Migrate(ctx, pool); err != nil {
			t.Fatalf("restore schema after down migration: %v", err)
		}
	})
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	for _, tbl := range []string{"service_tokens", "audit_log"} {
		if !tableExists(ctx, t, pool, tbl) {
			t.Fatalf("%s missing after Migrate", tbl)
		}
	}

	runDown(ctx, t, pool, "0018_service_tokens.down.sql")
	for _, tbl := range []string{"service_tokens", "audit_log"} {
		if tableExists(ctx, t, pool, tbl) {
			t.Fatalf("%s survived 0018's down", tbl)
		}
	}

	up, err := fs.ReadFile(migrations.FS, "0018_service_tokens.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(up)); err != nil {
		t.Fatalf("0018 up after its down: %v", err)
	}
	if !tableExists(ctx, t, pool, "service_tokens") || !tableExists(ctx, t, pool, "audit_log") {
		t.Fatal("tables missing after replaying the up")
	}
}

func TestMintServiceTokenStoresOnlyTheHash(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	id, plaintext, err := s.MintServiceToken(ctx, "satlink", ScopeAdminRead, "cli:test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plaintext, "lys_") {
		t.Fatalf("token %q lacks the lys_ prefix", plaintext[:4])
	}
	var hash, label, scope string
	if err := s.pool.QueryRow(ctx,
		`SELECT token_hash, consumer_label, scope FROM service_tokens WHERE id = $1`, id).Scan(&hash, &label, &scope); err != nil {
		t.Fatal(err)
	}
	if hash != hashToken(plaintext) || hash == plaintext || label != "satlink" || scope != ScopeAdminRead {
		t.Fatalf("row = (%q, %q, %q)", hash, label, scope)
	}

	var n int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_log
		  WHERE action = 'service_token.mint' AND actor = 'cli:test' AND target_id = $1::text
		    AND detail::text NOT LIKE '%' || $2 || '%' AND detail::text NOT LIKE '%' || $3 || '%'`,
		fmt.Sprint(id), plaintext, hash).Scan(&n); err != nil || n != 1 {
		t.Fatalf("mint audit rows = %d (%v), want 1 carrying neither plaintext nor hash", n, err)
	}
}

func TestMintServiceTokenRejectsBadInput(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, _, err := s.MintServiceToken(ctx, "x", "root:all", "t"); !errors.Is(err, ErrUnknownScope) {
		t.Fatalf("unknown scope err = %v, want ErrUnknownScope", err)
	}
	if _, _, err := s.MintServiceToken(ctx, "  ", ScopeAdminRead, "t"); err == nil {
		t.Fatal("blank label accepted")
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM service_tokens) + (SELECT count(*) FROM audit_log)`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rejected mints left %d rows (%v)", n, err)
	}
}

func TestRevokeServiceTokens(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	// Two live tokens for one label is rotation, and must work.
	id1, _, _ := s.MintServiceToken(ctx, "rot", ScopeAdminRead, "t")
	id2, tok2, err := s.MintServiceToken(ctx, "rot", ScopeAdminRead, "t")
	if err != nil {
		t.Fatalf("second mint for a live label: %v", err)
	}
	other, otherTok, _ := s.MintServiceToken(ctx, "other", ScopeEidolonRead, "t")

	if err := s.RevokeServiceTokenByID(ctx, id1, "cli:test"); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeServiceTokenByID(ctx, id1, "cli:test"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoking twice = %v, want ErrNotFound", err)
	}
	if c, err := s.CheckServiceToken(ctx, tok2, ScopeAdminRead); err != nil || c.Revoked {
		t.Fatalf("the rotated-in token was affected: %+v, %v", c, err)
	}

	n, err := s.RevokeServiceTokensByLabel(ctx, "rot", "cli:test")
	if err != nil || n != 1 {
		t.Fatalf("revoke by label = %d, %v; want 1 (id1 was already revoked)", n, err)
	}
	if c, err := s.CheckServiceToken(ctx, tok2, ScopeAdminRead); err != nil || !c.Revoked || c.Label != "rot" {
		t.Fatalf("after revoke: %+v, %v", c, err)
	}
	if c, err := s.CheckServiceToken(ctx, otherTok, ScopeEidolonRead); err != nil || c.Revoked {
		t.Fatalf("an unrelated label was revoked: %+v, %v (id %d)", c, err, other)
	}
	if _, err := s.RevokeServiceTokensByLabel(ctx, "nobody", "cli:test"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown label = %v, want ErrNotFound", err)
	}

	// Rows are kept, and list shows both as revoked.
	toks, err := s.ListServiceTokens(ctx)
	if err != nil || len(toks) != 3 {
		t.Fatalf("list = %d rows, %v; want 3", len(toks), err)
	}
	for _, tk := range toks {
		if (tk.ID == id1 || tk.ID == id2) != (tk.RevokedAt != nil) {
			t.Errorf("token %d revoked_at = %v", tk.ID, tk.RevokedAt)
		}
	}

	var audits int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = 'service_token.revoke'`).Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("revoke audit rows = %d (%v), want 2 (one per token revoked)", audits, err)
	}
}

// Mint and revoke audit rows survive a request context that is already
// cancelled: the change happened, so the trail must say so.
func TestServiceTokenAuditSurvivesCancelledContext(t *testing.T) {
	s := newStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	id, _, err := s.MintServiceToken(ctx, "c", ScopeAdminRead, "t")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeServiceTokenByID(ctx, id, "t"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE action IN ('service_token.mint','service_token.revoke')`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("audit rows = %d (%v), want 2", n, err)
	}
}
