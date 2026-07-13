package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOwnerSeededByMigration(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	if !owner.IsOwner || owner.ID == 0 {
		t.Fatalf("owner = %+v, want a real row with IsOwner", owner)
	}

	// The single-owner index must actually hold: a second owner is impossible.
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO users (email, display_name, is_owner) VALUES ('usurper@example.com', 'Usurper', TRUE)`); err == nil {
		t.Fatal("inserted a second owner; the users_single_owner index is not enforcing")
	}
}

func TestCreateUser(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "  Mara@Example.COM ", "Mara")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.Email != "mara@example.com" {
		t.Fatalf("email = %q, want it normalized to mara@example.com", u.Email)
	}
	if u.IsOwner {
		t.Fatal("a new member must not be the owner")
	}

	// Lookup is case-insensitive — Cloudflare Access (LYCM-803) and Purser will
	// not agree with each other on casing, and neither may create a second
	// account for the same person.
	got, err := s.GetUserByEmail(ctx, "MARA@example.com")
	if err != nil || got.ID != u.ID {
		t.Fatalf("GetUserByEmail(differently cased) = (%+v, %v), want user %d", got, err, u.ID)
	}

	if _, err := s.CreateUser(ctx, "mara@example.com", "Impostor"); !errors.Is(err, ErrDuplicateEmail) {
		t.Fatalf("duplicate CreateUser err = %v, want ErrDuplicateEmail", err)
	}
}

func TestDeleteUserRefusesOwner(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	owner := ownerID(ctx, t, s)
	if err := s.DeleteUser(ctx, owner); !errors.Is(err, ErrOwnerImmutable) {
		t.Fatalf("DeleteUser(owner) err = %v, want ErrOwnerImmutable", err)
	}

	member, err := s.CreateUser(ctx, "member@example.com", "Member")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.DeleteUser(ctx, member.ID); err != nil {
		t.Fatalf("DeleteUser(member): %v", err)
	}
	if err := s.DeleteUser(ctx, member.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteUser(gone) err = %v, want ErrNotFound", err)
	}
}

func TestSessionTokenLifecycle(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	owner := ownerID(ctx, t, s)

	session, err := s.MintToken(ctx, owner, TokenSession, "laptop", nil)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	got, err := s.UserByToken(ctx, session)
	if err != nil {
		t.Fatalf("UserByToken: %v", err)
	}
	if got.ID != owner {
		t.Fatalf("UserByToken = user %d, want owner %d", got.ID, owner)
	}

	// The plaintext must not be recoverable from the database.
	var n int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM user_tokens WHERE token_hash = $1`, session).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatal("the token plaintext is stored in token_hash; it must be hashed")
	}

	if err := s.RevokeToken(ctx, session); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	if _, err := s.UserByToken(ctx, session); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked token still resolves: err = %v, want ErrNotFound", err)
	}
}

func TestUserByTokenRejectsBadCredentials(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	owner := ownerID(ctx, t, s)

	expired := time.Now().Add(-time.Hour)
	stale, err := s.MintToken(ctx, owner, TokenSession, "old-phone", &expired)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	// An invite is not a session: it must be redeemed before it authenticates
	// anything, otherwise handing someone an invite would hand them access
	// without the single-use claim ever happening.
	invite, err := s.MintToken(ctx, owner, TokenInvite, "invite", nil)
	if err != nil {
		t.Fatalf("MintToken(invite): %v", err)
	}

	for _, tc := range []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"unknown", "lyc_nonsense"},
		{"expired session", stale},
		{"unredeemed invite", invite},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.UserByToken(ctx, tc.token); !errors.Is(err, ErrNotFound) {
				t.Fatalf("UserByToken(%s) err = %v, want ErrNotFound", tc.name, err)
			}
		})
	}
}

func TestRedeemInviteIsSingleUse(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	member, err := s.CreateUser(ctx, "mara@example.com", "Mara")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	invite, err := s.MintToken(ctx, member.ID, TokenInvite, "invite", nil)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	u, session, err := s.RedeemInvite(ctx, invite, "Pixel 8")
	if err != nil {
		t.Fatalf("RedeemInvite: %v", err)
	}
	if u.ID != member.ID {
		t.Fatalf("redeemed as user %d, want %d", u.ID, member.ID)
	}
	if session == invite {
		t.Fatal("the session token must be freshly minted, not the invite echoed back")
	}

	// The new session works...
	if got, err := s.UserByToken(ctx, session); err != nil || got.ID != member.ID {
		t.Fatalf("session does not authenticate: (%+v, %v)", got, err)
	}
	// ...and the invite is spent. A second device cannot ride the same invite in.
	if _, _, err := s.RedeemInvite(ctx, invite, "Someone Else's Laptop"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second RedeemInvite err = %v, want ErrNotFound (invites are single-use)", err)
	}
}

func TestReconcileOwner(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	owner, err := s.ReconcileOwner(ctx, "magos@example.com", "Magos")
	if err != nil {
		t.Fatalf("ReconcileOwner: %v", err)
	}
	if owner.Email != "magos@example.com" || owner.DisplayName != "Magos" {
		t.Fatalf("owner = %+v, want the configured identity applied", owner)
	}
	if !owner.IsOwner {
		t.Fatal("reconcile must not clear ownership")
	}

	// Idempotent across boots.
	again, err := s.ReconcileOwner(ctx, "magos@example.com", "Magos")
	if err != nil || again.ID != owner.ID {
		t.Fatalf("second ReconcileOwner = (%+v, %v), want the same row", again, err)
	}

	// It must never create a second owner, and never steal a member's address.
	member, err := s.CreateUser(ctx, "mara@example.com", "Mara")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	got, err := s.ReconcileOwner(ctx, "mara@example.com", "Mara")
	if !errors.Is(err, ErrDuplicateEmail) {
		t.Fatalf("ReconcileOwner onto a member's email err = %v, want ErrDuplicateEmail", err)
	}
	if got.ID != owner.ID {
		t.Fatalf("collision returned user %d, want the unchanged owner %d", got.ID, owner.ID)
	}
	if reloaded, err := s.GetUser(ctx, member.ID); err != nil || reloaded.IsOwner {
		t.Fatalf("member %d was promoted to owner", member.ID)
	}
}

// TestPositionsAreScopedPerUser is the heart of LYCM-801: housemates share the
// shelf but not each other's bookmarks. Two people reading the same book on the
// *same* device must not collide, and neither may see the other's progress.
func TestPositionsAreScopedPerUser(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	owner := ownerID(ctx, t, s)
	mara, err := s.CreateUser(ctx, "mara@example.com", "Mara")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	book, err := s.InsertBook(ctx, sampleBook("shared-shelf-hash"))
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}

	// Same book, same physical device (the household tablet), two readers.
	const shared = "living-room-tablet"
	if _, err := s.UpsertPosition(ctx, ReadingPosition{
		BookID: book.ID, UserID: owner, DeviceID: shared, CFI: "/90", Progress: 0.9,
	}); err != nil {
		t.Fatalf("upsert owner position: %v", err)
	}
	if _, err := s.UpsertPosition(ctx, ReadingPosition{
		BookID: book.ID, UserID: mara.ID, DeviceID: shared, CFI: "/10", Progress: 0.1,
	}); err != nil {
		t.Fatalf("upsert mara position: %v", err)
	}

	// Two rows, not one overwriting the other.
	var n int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM reading_positions WHERE book_id = $1`, book.ID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("got %d position rows, want 2 — the shared device collapsed both readers onto one bookmark", n)
	}

	// Each sees only their own place. If this leaks, one person finishing a book
	// would show everyone else as finished.
	ownerPos, err := s.GetFurthestPosition(ctx, book.ID, owner)
	if err != nil {
		t.Fatalf("GetFurthestPosition(owner): %v", err)
	}
	if ownerPos.Progress != 0.9 {
		t.Fatalf("owner progress = %v, want 0.9", ownerPos.Progress)
	}
	maraPos, err := s.GetFurthestPosition(ctx, book.ID, mara.ID)
	if err != nil {
		t.Fatalf("GetFurthestPosition(mara): %v", err)
	}
	if maraPos.Progress != 0.1 {
		t.Fatalf("mara progress = %v, want 0.1 — she is seeing someone else's bookmark", maraPos.Progress)
	}

	// A user who has never opened the book has no position, even though others have.
	bystander, err := s.CreateUser(ctx, "bystander@example.com", "Bystander")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := s.GetFurthestPosition(ctx, book.ID, bystander.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bystander position err = %v, want ErrNotFound", err)
	}
}

// TestDeleteUserCascadesPositions guards the FK: removing someone must take
// their bookmarks and credentials with them.
func TestDeleteUserCascadesPositions(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	mara, err := s.CreateUser(ctx, "mara@example.com", "Mara")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	session, err := s.MintToken(ctx, mara.ID, TokenSession, "phone", nil)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	book, err := s.InsertBook(ctx, sampleBook("cascade-hash"))
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}
	if _, err := s.UpsertPosition(ctx, ReadingPosition{
		BookID: book.ID, UserID: mara.ID, DeviceID: "phone", CFI: "/4", Progress: 0.5,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if err := s.DeleteUser(ctx, mara.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	var positions int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM reading_positions WHERE user_id = $1`, mara.ID).Scan(&positions); err != nil {
		t.Fatalf("count positions: %v", err)
	}
	if positions != 0 {
		t.Fatalf("%d orphaned positions survived the user", positions)
	}
	if _, err := s.UserByToken(ctx, session); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a deleted user's session still authenticates: %v", err)
	}
}
