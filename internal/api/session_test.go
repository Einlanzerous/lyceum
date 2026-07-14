package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/magos/lyceum/internal/store"
)

// authServer starts an API with session enforcement ON — the state the clients
// will run against once they ship a sign-in screen. The default server used by
// every other test leaves it off (see WithUserAuth).
func authServer(t *testing.T, s *store.Store) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(New(s, "", WithUserAuth(true)).Handler())
	t.Cleanup(srv.Close)
	return srv
}

// do issues a request with an optional bearer token.
func do(t *testing.T, method, url, token string, body any) *http.Response {
	t.Helper()
	var rdr *bytes.Reader = bytes.NewReader(nil)
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return v
}

// signIn mints an invite for a user and redeems it through the HTTP surface,
// returning the session token a client would then carry.
func signIn(t *testing.T, s *store.Store, srv *httptest.Server, userID int64) string {
	t.Helper()
	invite, err := s.MintToken(context.Background(), userID, store.TokenInvite, "test", nil)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	resp := do(t, http.MethodPost, srv.URL+"/auth/session", "", map[string]string{
		"token": invite, "device_label": "test-device",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /auth/session status = %d, want 200", resp.StatusCode)
	}
	out := decode[struct {
		SessionToken string `json:"session_token"`
	}](t, resp)
	if out.SessionToken == "" {
		t.Fatal("no session_token returned")
	}
	return out.SessionToken
}

func TestReaderCoreRequiresSession(t *testing.T) {
	s := testStore(t)
	seedBook(t, s, "gated-hash", "Dune", "Herbert", nil)
	srv := authServer(t, s)

	// No credential at all.
	if resp := do(t, http.MethodGet, srv.URL+"/library", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET /library = %d, want 401", resp.StatusCode)
	}
	// A garbage credential.
	if resp := do(t, http.MethodGet, srv.URL+"/library", "lyc_bogus", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bogus-token GET /library = %d, want 401", resp.StatusCode)
	}

	// A real session gets in.
	token := signIn(t, s, srv, ownerID(context.Background(), t, s))
	resp := do(t, http.MethodGet, srv.URL+"/library", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated GET /library = %d, want 200", resp.StatusCode)
	}
	if books := decode[[]bookJSON](t, resp); len(books) != 1 {
		t.Fatalf("library returned %d books, want 1", len(books))
	}
}

// TestReaderCoreOpenWhenAuthDisabled is the compatibility path this PR ships on:
// every existing client sends no credential, so with LYCEUM_AUTH off the server
// must behave exactly as it did before accounts existed.
func TestReaderCoreOpenWhenAuthDisabled(t *testing.T) {
	s := testStore(t)
	seedBook(t, s, "open-hash", "Dune", "Herbert", nil)
	srv := newServer(t, s) // no WithUserAuth

	resp := do(t, http.MethodGet, srv.URL+"/library", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /library with auth disabled = %d, want 200", resp.StatusCode)
	}

	// And the caller is the owner, so their reading history is the one that
	// surfaces — not a phantom user 0 with no positions.
	me := do(t, http.MethodGet, srv.URL+"/auth/me", "", nil)
	if me.StatusCode != http.StatusOK {
		t.Fatalf("GET /auth/me with auth disabled = %d, want 200", me.StatusCode)
	}
	if u := decode[userJSON](t, me); !u.IsOwner {
		t.Fatalf("auth-disabled caller = %+v, want the owner", u)
	}
}

func TestSignOutRevokesOnlyThisDevice(t *testing.T) {
	s := testStore(t)
	srv := authServer(t, s)
	owner := ownerID(context.Background(), t, s)

	laptop := signIn(t, s, srv, owner)
	phone := signIn(t, s, srv, owner)

	if resp := do(t, http.MethodDelete, srv.URL+"/auth/session", laptop, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE /auth/session = %d, want 204", resp.StatusCode)
	}

	if resp := do(t, http.MethodGet, srv.URL+"/auth/me", laptop, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("signed-out token still works: %d, want 401", resp.StatusCode)
	}
	if resp := do(t, http.MethodGet, srv.URL+"/auth/me", phone, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("signing out one device killed another: %d, want 200", resp.StatusCode)
	}
}

func TestAdminRoutesAreOwnerOnly(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	srv := authServer(t, s)

	member, err := s.CreateUser(ctx, "mara@example.com", "Mara")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	memberToken := signIn(t, s, srv, member.ID)
	ownerToken := signIn(t, s, srv, ownerID(ctx, t, s))

	// A member is authenticated but not authorized: 403, not 401.
	if resp := do(t, http.MethodGet, srv.URL+"/admin/users", memberToken, nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member GET /admin/users = %d, want 403", resp.StatusCode)
	}
	if resp := do(t, http.MethodPost, srv.URL+"/admin/users", memberToken,
		map[string]string{"email": "sneaky@example.com"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member POST /admin/users = %d, want 403", resp.StatusCode)
	}
	// And no account was created by the attempt.
	if _, err := s.GetUserByEmail(ctx, "sneaky@example.com"); err == nil {
		t.Fatal("a member managed to create an account")
	}

	if resp := do(t, http.MethodGet, srv.URL+"/admin/users", ownerToken, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("owner GET /admin/users = %d, want 200", resp.StatusCode)
	}

	// The owner cannot be deleted, even by themselves.
	ownerPath := srv.URL + "/admin/users/" + strconv.FormatInt(ownerID(ctx, t, s), 10)
	if resp := do(t, http.MethodDelete, ownerPath, ownerToken, nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("DELETE owner = %d, want 403", resp.StatusCode)
	}
}

// TestAdminInviteFlow walks the path Purser's connector will drive (SERV-38):
// create the account, get a one-time invite back, redeem it for a session.
func TestAdminInviteFlow(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	srv := authServer(t, s)
	ownerToken := signIn(t, s, srv, ownerID(ctx, t, s))

	resp := do(t, http.MethodPost, srv.URL+"/admin/users", ownerToken,
		map[string]string{"email": "mara@example.com", "display_name": "Mara"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /admin/users = %d, want 201", resp.StatusCode)
	}
	created := decode[struct {
		User        userJSON `json:"user"`
		InviteToken string   `json:"invite_token"`
	}](t, resp)
	if created.InviteToken == "" {
		t.Fatal("no invite_token returned; Purser has nothing to hand the new user")
	}
	if created.User.IsOwner {
		t.Fatal("an invited member must not be created as the owner")
	}

	// Redeeming it yields a working session for the new member.
	redeem := do(t, http.MethodPost, srv.URL+"/auth/session", "",
		map[string]string{"token": created.InviteToken, "device_label": "Pixel 8"})
	if redeem.StatusCode != http.StatusOK {
		t.Fatalf("POST /auth/session = %d, want 200", redeem.StatusCode)
	}
	session := decode[struct {
		User         userJSON `json:"user"`
		SessionToken string   `json:"session_token"`
	}](t, redeem)
	if session.User.Email != "mara@example.com" {
		t.Fatalf("redeemed as %q, want mara@example.com", session.User.Email)
	}

	me := do(t, http.MethodGet, srv.URL+"/auth/me", session.SessionToken, nil)
	if me.StatusCode != http.StatusOK {
		t.Fatalf("GET /auth/me = %d, want 200", me.StatusCode)
	}

	// The invite is spent — a second device can't ride it in.
	again := do(t, http.MethodPost, srv.URL+"/auth/session", "",
		map[string]string{"token": created.InviteToken, "device_label": "Another Phone"})
	if again.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reusing a spent invite = %d, want 401", again.StatusCode)
	}

	// A duplicate email is a conflict, not a second account.
	dup := do(t, http.MethodPost, srv.URL+"/admin/users", ownerToken,
		map[string]string{"email": "mara@example.com", "display_name": "Impostor"})
	if dup.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate POST /admin/users = %d, want 409", dup.StatusCode)
	}
}

// TestSyncIsPrivatePerUser is the user-visible payoff: two people, one shelf,
// separate bookmarks. If this regresses, one housemate's progress bar shows on
// everyone's shelf.
func TestSyncIsPrivatePerUser(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	srv := authServer(t, s)

	book := seedBook(t, s, "shared-hash", "Dune", "Herbert", nil)

	mara, err := s.CreateUser(ctx, "mara@example.com", "Mara")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	ownerToken := signIn(t, s, srv, ownerID(ctx, t, s))
	maraToken := signIn(t, s, srv, mara.ID)

	// The owner reads most of the way through.
	put := do(t, http.MethodPut, srv.URL+"/sync", ownerToken, map[string]any{
		"book_id": book.ID, "device_id": "kobo", "cfi": "epubcfi(/6/4!/2)", "progress": 0.8,
	})
	if put.StatusCode != http.StatusOK {
		t.Fatalf("owner PUT /sync = %d, want 200", put.StatusCode)
	}

	// Mara has never opened it: no resume point, and no progress on her shelf.
	if resp := do(t, http.MethodGet, srv.URL+"/sync?book_id="+strconv.FormatInt(book.ID, 10), maraToken, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("mara GET /sync = %d, want 404 — she is seeing the owner's bookmark", resp.StatusCode)
	}
	lib := decode[[]bookJSON](t, do(t, http.MethodGet, srv.URL+"/library", maraToken, nil))
	if len(lib) != 1 {
		t.Fatalf("mara sees %d books, want 1 (the shelf is shared)", len(lib))
	}
	if lib[0].Progress != nil {
		t.Fatalf("mara's shelf shows progress %v, want none — that is the owner's", *lib[0].Progress)
	}

	// She reads a little; the owner's position is untouched.
	if resp := do(t, http.MethodPut, srv.URL+"/sync", maraToken, map[string]any{
		"book_id": book.ID, "device_id": "kobo", "cfi": "epubcfi(/6/2!/2)", "progress": 0.1,
	}); resp.StatusCode != http.StatusOK {
		t.Fatalf("mara PUT /sync = %d, want 200", resp.StatusCode)
	}

	ownerLib := decode[[]bookJSON](t, do(t, http.MethodGet, srv.URL+"/library", ownerToken, nil))
	if ownerLib[0].Progress == nil || *ownerLib[0].Progress != 0.8 {
		t.Fatalf("owner progress = %v, want 0.8 — mara's read dragged it backward", ownerLib[0].Progress)
	}
	maraLib := decode[[]bookJSON](t, do(t, http.MethodGet, srv.URL+"/library", maraToken, nil))
	if maraLib[0].Progress == nil || *maraLib[0].Progress != 0.1 {
		t.Fatalf("mara progress = %v, want 0.1", maraLib[0].Progress)
	}
}

// TestDisplayNameRoundTrip covers the LYCM-700 local label folding into the
// account: the name now lives on the server and follows the person.
func TestDisplayNameRoundTrip(t *testing.T) {
	s := testStore(t)
	srv := authServer(t, s)
	token := signIn(t, s, srv, ownerID(context.Background(), t, s))

	resp := do(t, http.MethodPatch, srv.URL+"/auth/me", token,
		map[string]string{"display_name": "Magos"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH /auth/me = %d, want 200", resp.StatusCode)
	}
	if u := decode[userJSON](t, resp); u.DisplayName != "Magos" {
		t.Fatalf("display_name = %q, want Magos", u.DisplayName)
	}

	me := decode[userJSON](t, do(t, http.MethodGet, srv.URL+"/auth/me", token, nil))
	if me.DisplayName != "Magos" {
		t.Fatalf("reloaded display_name = %q, want Magos", me.DisplayName)
	}

	if resp := do(t, http.MethodPatch, srv.URL+"/auth/me", token,
		map[string]string{"display_name": "  "}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("blank display_name = %d, want 400", resp.StatusCode)
	}
}

// TestServiceTokensCannotReachTheReaderCore guards the namespace split: an
// eidolon:read token is a credential for a *program*, and must not double as a
// household member's session.
func TestServiceTokensCannotReachTheReaderCore(t *testing.T) {
	s := testStore(t)
	auth, err := ParseTokens("svc-token=eidolon:read")
	if err != nil {
		t.Fatalf("ParseTokens: %v", err)
	}
	srv := httptest.NewServer(New(s, "", WithAuth(auth), WithUserAuth(true)).Handler())
	t.Cleanup(srv.Close)

	if resp := do(t, http.MethodGet, srv.URL+"/library", "svc-token", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("service token reached /library: %d, want 401", resp.StatusCode)
	}

	// And the converse: a user session must not authorize a scoped service route.
	session := signIn(t, s, srv, ownerID(context.Background(), t, s))
	book := seedBook(t, s, "svc-hash", "Dune", "Herbert", nil)
	path := srv.URL + "/books/" + strconv.FormatInt(book.ID, 10) + "/deliveries"
	if resp := do(t, http.MethodGet, path, session, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("user session reached a delivery:send route: %d, want 401", resp.StatusCode)
	}
}

// --- Regressions caught in review ---

// TestCookieAuthenticatesBlobRoutes covers the reason the session may arrive as a
// cookie at all: the shelf renders covers as <img src="/books/{id}/cover">, and a
// browser image request carries no Authorization header. If only the bearer
// header were accepted, every cover would 404/401 the moment auth was turned on.
func TestCookieAuthenticatesBlobRoutes(t *testing.T) {
	s := testStore(t)
	book := seedBook(t, s, "cookie-hash", "Dune", "Herbert", pngBytes)
	srv := authServer(t, s)

	invite, err := s.MintToken(context.Background(), ownerID(context.Background(), t, s),
		store.TokenInvite, "test", nil)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	// A cookie jar is exactly what a browser brings.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}

	body, _ := json.Marshal(map[string]string{"token": invite, "device_label": "browser"})
	resp, err := client.Post(srv.URL+"/auth/session", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /auth/session: %v", err)
	}
	resp.Body.Close()

	var gotCookie bool
	for _, c := range jar.Cookies(resp.Request.URL) {
		if c.Name == sessionCookie {
			gotCookie = true
		}
	}
	if !gotCookie {
		t.Fatal("sign-in did not set the session cookie; browser <img> covers cannot authenticate")
	}

	// The cover request an <img> tag makes: no Authorization header, cookie only.
	cover, err := client.Get(srv.URL + "/books/" + strconv.FormatInt(book.ID, 10) + "/cover")
	if err != nil {
		t.Fatalf("GET cover: %v", err)
	}
	defer cover.Body.Close()
	if cover.StatusCode != http.StatusOK {
		t.Fatalf("cookie-authenticated cover = %d, want 200", cover.StatusCode)
	}
	if cc := cover.Header.Get("Cache-Control"); !strings.HasPrefix(cc, "private") {
		t.Fatalf("Cache-Control = %q on an authenticated blob; a shared cache could "+
			"store it and serve it to an unauthenticated caller", cc)
	}

	// Signing out drops the cookie, and the cover stops resolving.
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/auth/session", nil)
	out, err := client.Do(req)
	if err != nil {
		t.Fatalf("DELETE /auth/session: %v", err)
	}
	out.Body.Close()
	after, err := client.Get(srv.URL + "/books/" + strconv.FormatInt(book.ID, 10) + "/cover")
	if err != nil {
		t.Fatalf("GET cover after sign-out: %v", err)
	}
	defer after.Body.Close()
	if after.StatusCode != http.StatusUnauthorized {
		t.Fatalf("cover after sign-out = %d, want 401", after.StatusCode)
	}
}

// TestAdminClosedWhenAuthDisabled: with auth off every caller is served as the
// owner, so leaving the mint routes open would let anyone on the network issue
// themselves an invite, redeem it for a durable session, and keep it after the
// operator turns auth on — walking straight through the step meant to shut the
// door.
func TestAdminClosedWhenAuthDisabled(t *testing.T) {
	s := testStore(t)
	srv := newServer(t, s) // auth off

	resp := do(t, http.MethodPost, srv.URL+"/admin/users", "",
		map[string]string{"email": "attacker@example.com", "display_name": "Mallory"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST /admin/users with auth off = %d, want 403", resp.StatusCode)
	}
	if _, err := s.GetUserByEmail(context.Background(), "attacker@example.com"); err == nil {
		t.Fatal("an unauthenticated caller minted an account on an auth-off server")
	}
	if resp := do(t, http.MethodGet, srv.URL+"/admin/users", "", nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("GET /admin/users with auth off = %d, want 403", resp.StatusCode)
	}
}

// TestInvitesExpire: redeeming an invite yields a durable session, so a stale one
// left in an inbox or a scrollback must not stay redeemable forever.
func TestInvitesExpire(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	srv := authServer(t, s)
	ownerToken := signIn(t, s, srv, ownerID(ctx, t, s))

	resp := do(t, http.MethodPost, srv.URL+"/admin/users", ownerToken,
		map[string]string{"email": "mara@example.com", "display_name": "Mara"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /admin/users = %d, want 201", resp.StatusCode)
	}
	created := decode[struct {
		User        userJSON `json:"user"`
		InviteToken string   `json:"invite_token"`
	}](t, resp)

	var expires *time.Time
	if err := s.Pool().QueryRow(ctx,
		`SELECT expires_at FROM user_tokens WHERE user_id = $1 AND kind = 'invite'`,
		created.User.ID).Scan(&expires); err != nil {
		t.Fatalf("read expires_at: %v", err)
	}
	if expires == nil {
		t.Fatal("invite has no expiry; a leaked token would be a permanent way in")
	}
	if d := time.Until(*expires); d <= 0 || d > store.InviteTTL+time.Minute {
		t.Fatalf("invite expires in %s, want ~%s", d, store.InviteTTL)
	}
}

// TestOwnerRenameIsNotStale guards the memoised owner: PATCH /auth/me must
// refresh the cache, or /auth/me keeps reporting the old display name.
func TestOwnerRenameIsNotStale(t *testing.T) {
	s := testStore(t)
	srv := newServer(t, s) // auth off — the mode that uses the memo

	// Prime the cache.
	if u := decode[userJSON](t, do(t, http.MethodGet, srv.URL+"/auth/me", "", nil)); !u.IsOwner {
		t.Fatalf("expected the owner, got %+v", u)
	}
	do(t, http.MethodPatch, srv.URL+"/auth/me", "", map[string]string{"display_name": "Renamed"})

	if u := decode[userJSON](t, do(t, http.MethodGet, srv.URL+"/auth/me", "", nil)); u.DisplayName != "Renamed" {
		t.Fatalf("display_name = %q after rename, want Renamed (the owner memo went stale)", u.DisplayName)
	}
}

// --- Devices & household metadata (the surfaces the design handoff assumes) ---

// TestSessionListAndRevoke: a session never expires, so the only real risk in a
// password-free model is a lost device staying signed in forever. The devices
// list is how its owner sees it and cuts it off.
func TestSessionListAndRevoke(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	srv := authServer(t, s)
	owner := ownerID(ctx, t, s)

	laptop := signIn(t, s, srv, owner)
	phone := signIn(t, s, srv, owner)

	// Listed from the laptop, exactly one row is "this device".
	sessions := decode[[]sessionJSON](t, do(t, http.MethodGet, srv.URL+"/auth/sessions", laptop, nil))
	if len(sessions) != 2 {
		t.Fatalf("got %d devices, want 2", len(sessions))
	}
	var current, other sessionJSON
	for _, sess := range sessions {
		if sess.Current {
			current = sess
		} else {
			other = sess
		}
	}
	if current.ID == 0 || other.ID == 0 {
		t.Fatalf("expected exactly one current device, got %+v", sessions)
	}

	// Revoking the *other* device kills it and leaves this one alone.
	path := srv.URL + "/auth/sessions/" + strconv.FormatInt(other.ID, 10)
	if resp := do(t, http.MethodDelete, path, laptop, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke other device = %d, want 204", resp.StatusCode)
	}
	if resp := do(t, http.MethodGet, srv.URL+"/auth/me", phone, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked device still works: %d, want 401", resp.StatusCode)
	}
	if resp := do(t, http.MethodGet, srv.URL+"/auth/me", laptop, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("revoking one device killed the other: %d, want 200", resp.StatusCode)
	}
}

// TestSessionRevokeIsScopedToOwner: a member must not be able to sign someone
// else's device out by guessing a row id.
func TestSessionRevokeIsScopedToOwner(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	srv := authServer(t, s)

	mara, err := s.CreateUser(ctx, "mara@example.com", "Mara")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	ownerToken := signIn(t, s, srv, ownerID(ctx, t, s))
	maraToken := signIn(t, s, srv, mara.ID)

	ownerSessions := decode[[]sessionJSON](t, do(t, http.MethodGet, srv.URL+"/auth/sessions", ownerToken, nil))
	victim := ownerSessions[0].ID

	path := srv.URL + "/auth/sessions/" + strconv.FormatInt(victim, 10)
	if resp := do(t, http.MethodDelete, path, maraToken, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("mara revoking the owner's device = %d, want 404", resp.StatusCode)
	}
	if resp := do(t, http.MethodGet, srv.URL+"/auth/me", ownerToken, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("the owner's device was revoked by a member: %d", resp.StatusCode)
	}
}

// TestHouseholdListMetadata backs the household rows: an invited-but-absent
// housemate ("invite pending · expires in 6 days · never signed in") must be
// distinguishable from an active one ("Active · 1 device · last seen today").
func TestHouseholdListMetadata(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	srv := authServer(t, s)
	ownerToken := signIn(t, s, srv, ownerID(ctx, t, s))

	// Theo is invited and never shows up.
	do(t, http.MethodPost, srv.URL+"/admin/users", ownerToken,
		map[string]string{"email": "theo@example.com", "display_name": "Theo"})
	// Mara is invited and signs in.
	resp := do(t, http.MethodPost, srv.URL+"/admin/users", ownerToken,
		map[string]string{"email": "mara@example.com", "display_name": "Mara"})
	mara := decode[struct {
		User        userJSON `json:"user"`
		InviteToken string   `json:"invite_token"`
	}](t, resp)
	do(t, http.MethodPost, srv.URL+"/auth/session", "",
		map[string]string{"token": mara.InviteToken, "device_label": "Pixel 8"})

	members := decode[[]memberJSON](t, do(t, http.MethodGet, srv.URL+"/admin/users", ownerToken, nil))
	if len(members) != 3 {
		t.Fatalf("got %d members, want 3", len(members))
	}
	if !members[0].IsOwner {
		t.Fatalf("owner must sort first, got %+v", members[0])
	}

	byEmail := map[string]memberJSON{}
	for _, m := range members {
		byEmail[m.Email] = m
	}

	theo := byEmail["theo@example.com"]
	if theo.InviteExpiresAt == nil {
		t.Fatal("Theo has no invite_expires_at; the row can't show 'invite pending · expires in N days'")
	}
	if theo.LastSeenAt != nil || theo.SessionCount != 0 {
		t.Fatalf("Theo never signed in but reads as seen=%v devices=%d", theo.LastSeenAt, theo.SessionCount)
	}

	m := byEmail["mara@example.com"]
	if m.LastSeenAt == nil || m.SessionCount != 1 {
		t.Fatalf("Mara signed in on one device but reads as seen=%v devices=%d", m.LastSeenAt, m.SessionCount)
	}
	if m.InviteExpiresAt != nil {
		t.Fatal("Mara's invite was redeemed; it must no longer read as pending")
	}
}
