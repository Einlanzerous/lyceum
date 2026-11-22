package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/magos/lyceum/internal/store"
)

func scopedOK(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }

func callScoped(t *testing.T, h http.HandlerFunc, header string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/eidolon/x", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

// TestRequireScope covers the verdict for each kind of presentation against the
// real table: unknown and absent are 401, a wrong scope is 403, a live token
// with the scope runs the handler.
func TestRequireScope(t *testing.T) {
	s := testStore(t)
	read := mintServiceToken(t, s, "reader", store.ScopeEidolonRead)
	send := mintServiceToken(t, s, "sender", store.ScopeDeliverySend)
	guarded := New(s, "").requireScope(ScopeEidolonRead, scopedOK)

	cases := []struct {
		name, header string
		want         int
	}{
		{"no header", "", http.StatusUnauthorized},
		{"garbage scheme", "Basic abc", http.StatusUnauthorized},
		{"unknown token", "Bearer nope", http.StatusUnauthorized},
		{"wrong scope", "Bearer " + send, http.StatusForbidden},
		{"valid", "Bearer " + read, http.StatusNoContent},
		{"case-insensitive scheme", "bearer " + read, http.StatusNoContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := callScoped(t, guarded, tc.header).Code; got != tc.want {
				t.Fatalf("status = %d, want %d", got, tc.want)
			}
		})
	}
}

// With no rows at all, every scoped route is closed.
func TestRequireScopeEmptyTableIsClosed(t *testing.T) {
	s := testStore(t)
	guarded := New(s, "").requireScope(ScopeEidolonRead, func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler ran with an empty service_tokens table")
	})
	if got := callScoped(t, guarded, "Bearer anything").Code; got != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", got)
	}
}

// Revocation takes effect on the next request, with no restart and no cache, and
// the refusal is indistinguishable from an unknown token's.
func TestRequireScopeRevocation(t *testing.T) {
	s := testStore(t)
	id, tok, err := s.MintServiceToken(context.Background(), "rot", store.ScopeEidolonRead, "test")
	if err != nil {
		t.Fatal(err)
	}
	guarded := New(s, "").requireScope(ScopeEidolonRead, scopedOK)

	if got := callScoped(t, guarded, "Bearer "+tok).Code; got != http.StatusNoContent {
		t.Fatalf("live token = %d, want 204", got)
	}
	if err := s.RevokeServiceTokenByID(context.Background(), id, "test"); err != nil {
		t.Fatal(err)
	}
	revoked := callScoped(t, guarded, "Bearer "+tok)
	unknown := callScoped(t, guarded, "Bearer lys_never-minted")
	if revoked.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token = %d, want 401", revoked.Code)
	}
	if revoked.Code != unknown.Code || revoked.Body.String() != unknown.Body.String() ||
		revoked.Header().Get("WWW-Authenticate") != unknown.Header().Get("WWW-Authenticate") {
		t.Fatalf("revoked and unknown answers differ:\n%d %q\n%d %q",
			revoked.Code, revoked.Body.String(), unknown.Code, unknown.Body.String())
	}
}

// A database error is 503 and the handler never runs: the lookup failing must
// not be read as "allowed".
func TestRequireScopeDatabaseErrorIs503(t *testing.T) {
	s, pool := testStoreAndPool(t)
	tok := mintServiceToken(t, s, "reader", store.ScopeEidolonRead)
	guarded := New(s, "").requireScope(ScopeEidolonRead, func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler ran while the database was unreachable")
	})
	pool.Close()
	if got := callScoped(t, guarded, "Bearer "+tok).Code; got != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", got)
	}
}

func auditRows(t *testing.T, pool *pgxpool.Pool, action string) []struct{ Target, Detail string } {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT target_id, detail::text FROM audit_log WHERE action = $1 ORDER BY id`, action)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []struct{ Target, Detail string }
	for rows.Next() {
		var r struct{ Target, Detail string }
		if err := rows.Scan(&r.Target, &r.Detail); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// A refused unknown or revoked token is audited once per hash prefix per hour,
// carries the label when the token is known, and never carries the plaintext.
func TestRefusedTokensAreAuditedOncePerPrefixPerHour(t *testing.T) {
	s, pool := testStoreAndPool(t)
	id, revokedTok, err := s.MintServiceToken(context.Background(), "old-consumer", store.ScopeEidolonRead, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeServiceTokenByID(context.Background(), id, "test"); err != nil {
		t.Fatal(err)
	}
	guarded := New(s, "").requireScope(ScopeEidolonRead, scopedOK)

	unknownTok := "lys_not-a-real-token"
	for i := 0; i < 3; i++ {
		callScoped(t, guarded, "Bearer "+unknownTok)
		callScoped(t, guarded, "Bearer "+revokedTok)
	}
	callScoped(t, guarded, "") // nothing presented: nothing to audit

	rows := auditRows(t, pool, "service_token.refused")
	if len(rows) != 2 {
		t.Fatalf("refused audit rows = %d, want 2 (one per prefix): %+v", len(rows), rows)
	}
	var sawRevoked, sawUnknown bool
	for _, r := range rows {
		if strings.Contains(r.Detail, revokedTok) || strings.Contains(r.Target, revokedTok) ||
			strings.Contains(r.Detail, unknownTok) || strings.Contains(r.Target, unknownTok) {
			t.Fatalf("audit row carries plaintext: %+v", r)
		}
		sawRevoked = sawRevoked || strings.Contains(r.Detail, `"revoked"`) && strings.Contains(r.Detail, "old-consumer")
		sawUnknown = sawUnknown || strings.Contains(r.Detail, `"unknown"`)
	}
	if !sawRevoked || !sawUnknown {
		t.Fatalf("want a revoked row naming its label and an unknown row: %+v", rows)
	}

	// An hour later the same prefix is recorded again.
	if _, err := pool.Exec(context.Background(),
		`UPDATE audit_log SET at = now() - interval '61 minutes' WHERE action = 'service_token.refused'`); err != nil {
		t.Fatal(err)
	}
	callScoped(t, guarded, "Bearer "+unknownTok)
	if got := len(auditRows(t, pool, "service_token.refused")); got != 3 {
		t.Fatalf("rows after the hour = %d, want 3", got)
	}
}

// last_used_at is stamped on use, at most once a minute, and not by a refusal.
func TestLastUsedStampedAtMostOncePerMinute(t *testing.T) {
	s, pool := testStoreAndPool(t)
	read := mintServiceToken(t, s, "reader", store.ScopeEidolonRead)
	send := mintServiceToken(t, s, "sender", store.ScopeDeliverySend)
	guarded := New(s, "").requireScope(ScopeEidolonRead, scopedOK)
	ctx := context.Background()

	lastUsed := func(label string) *time.Time {
		var ts *time.Time
		if err := pool.QueryRow(ctx, `SELECT last_used_at FROM service_tokens WHERE consumer_label = $1`, label).Scan(&ts); err != nil {
			t.Fatal(err)
		}
		return ts
	}
	if lastUsed("reader") != nil {
		t.Fatal("last_used_at set before any use")
	}
	callScoped(t, guarded, "Bearer "+read)
	first := lastUsed("reader")
	if first == nil {
		t.Fatal("last_used_at not stamped on first use")
	}

	// 30 s ago: a second use inside the minute leaves it alone.
	if _, err := pool.Exec(ctx, `UPDATE service_tokens SET last_used_at = now() - interval '30 seconds' WHERE consumer_label = 'reader'`); err != nil {
		t.Fatal(err)
	}
	before := lastUsed("reader")
	callScoped(t, guarded, "Bearer "+read)
	if after := lastUsed("reader"); !after.Equal(*before) {
		t.Fatalf("last_used_at rewritten inside the minute: %v -> %v", before, after)
	}

	// 2 min ago: restamped.
	if _, err := pool.Exec(ctx, `UPDATE service_tokens SET last_used_at = now() - interval '2 minutes' WHERE consumer_label = 'reader'`); err != nil {
		t.Fatal(err)
	}
	before = lastUsed("reader")
	callScoped(t, guarded, "Bearer "+read)
	if after := lastUsed("reader"); !after.After(*before) {
		t.Fatalf("last_used_at not restamped after a minute: %v -> %v", before, after)
	}

	// A token refused for its scope is not "used".
	callScoped(t, guarded, "Bearer "+send)
	if lastUsed("sender") != nil {
		t.Fatal("a wrong-scope presentation stamped last_used_at")
	}
}

// The scope-isolation proof (criterion 11). Auth is on, because with it off
// requireUser routes are served as the owner and "cannot read anything else"
// would prove nothing. The route list is the one Handler() actually registered.
func TestAdminReadTokenReachesOnlyTheCountRoute(t *testing.T) {
	s := testStore(t)
	admin := mintServiceToken(t, s, "satlink", store.ScopeAdminRead)
	read, send := seedServiceTokens(t, s)

	a := New(s, "", WithUserAuth(true))
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	if len(a.routes) < 40 {
		t.Fatalf("recorded %d routes; the recorder looks broken", len(a.routes))
	}

	const count = "GET /admin/users/active"
	bare := map[string]bool{"POST /auth/session": true, "POST /auth/sso/cloudflare": true}
	sawCount := false
	for _, pattern := range a.routes {
		method, path, _ := strings.Cut(pattern, " ")
		path = strings.ReplaceAll(path, "{id}", "1")
		status := do(t, method, srv.URL+path, admin, map[string]any{"token": admin, "code": ""}).StatusCode

		switch {
		case pattern == count:
			sawCount = true
			if status != http.StatusOK {
				t.Errorf("%s with admin:read = %d, want 200", pattern, status)
			}
		case bare[pattern]:
			// No guard at all: the presented admin token must buy nothing from the
			// handler itself. Redeeming a service token as an invite is a 401.
			if status != http.StatusUnauthorized {
				t.Errorf("%s with admin:read = %d, want 401", pattern, status)
			}
		default:
			if status != http.StatusUnauthorized && status != http.StatusForbidden {
				t.Errorf("%s with admin:read = %d, want 401 or 403", pattern, status)
			}
		}
	}
	if !sawCount {
		t.Fatal("the count route is not in Handler()")
	}

	// The other two scopes still reach their routes, and not the count route.
	reach := map[string][]string{
		read: {"GET /eidolon/books/1/location", "GET /eidolon/books/1/chapter"},
		send: {"POST /books/1/send-to-kindle", "GET /books/1/deliveries"},
	}
	for tok, routes := range reach {
		for _, r := range routes {
			method, path, _ := strings.Cut(r, " ")
			status := do(t, method, srv.URL+path, tok, map[string]any{}).StatusCode
			if status == http.StatusUnauthorized || status == http.StatusForbidden {
				t.Errorf("%s with its own scope = %d, want it to reach the handler", r, status)
			}
		}
		if got := do(t, http.MethodGet, srv.URL+"/admin/users/active", tok, nil).StatusCode; got != http.StatusForbidden {
			t.Errorf("count route with another scope = %d, want 403", got)
		}
	}
}

// A user session is not a service token either: it cannot reach the count.
func TestSessionCannotReachTheCountRoute(t *testing.T) {
	s := testStore(t)
	srv := authServer(t, s)
	session := signIn(t, s, srv, ownerID(context.Background(), t, s))
	if got := do(t, http.MethodGet, srv.URL+"/admin/users/active", session, nil).StatusCode; got != http.StatusUnauthorized {
		t.Fatalf("session on count route = %d, want 401", got)
	}
}

func TestActiveUsersCount(t *testing.T) {
	s, pool := testStoreAndPool(t)
	ctx := context.Background()
	srv := authServer(t, s)
	admin := mintServiceToken(t, s, "satlink", store.ScopeAdminRead)

	// seed makes a member with one session last used `ago` before the database's now.
	seed := func(email string, ago ...string) {
		u, err := s.CreateUser(ctx, email, email)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range ago {
			if _, err := s.MintToken(ctx, u.ID, store.TokenSession, "dev", nil); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx,
				`UPDATE user_tokens SET last_used_at = now() - $2::interval
				  WHERE id = (SELECT max(id) FROM user_tokens WHERE user_id = $1)`, u.ID, d); err != nil {
				t.Fatal(err)
			}
		}
	}
	seed("inside@example.test", "60 seconds")
	seed("edge@example.test", "895 seconds")
	seed("outside@example.test", "905 seconds")
	seed("two-devices@example.test", "10 seconds", "20 seconds") // one person, counted once
	u, _ := s.CreateUser(ctx, "never@example.test", "never")     // a session that was never used
	if _, err := s.MintToken(ctx, u.ID, store.TokenSession, "dev", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MintToken(ctx, u.ID, store.TokenInvite, "dev", nil); err != nil {
		t.Fatal(err)
	}

	resp := do(t, http.MethodGet, srv.URL+"/admin/users/active", admin, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	body := string(raw)

	got := decodeBytes[map[string]any](t, raw)
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if want := []string{"active", "asOf", "window_seconds"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
	if got["active"] != float64(3) {
		t.Errorf("active = %v, want 3 (inside, edge, two-devices once)", got["active"])
	}
	if got["window_seconds"] != float64(900) {
		t.Errorf("window_seconds = %v, want 900", got["window_seconds"])
	}
	asOf, err := time.Parse(time.RFC3339Nano, got["asOf"].(string))
	if err != nil || time.Since(asOf) > time.Minute || time.Until(asOf) > time.Minute {
		t.Errorf("asOf = %v (%v), want about now", got["asOf"], err)
	}
	for _, leak := range []string{"example.test", "email", "display_name", "id\""} {
		if strings.Contains(body, leak) {
			t.Errorf("body contains %q: %s", leak, body)
		}
	}

	// The window is not a parameter.
	resp = do(t, http.MethodGet, srv.URL+"/admin/users/active?window=60&window_seconds=60", admin, nil)
	if wide := decodeBytes[map[string]any](t, mustRead(t, resp)); wide["active"] != float64(3) || wide["window_seconds"] != float64(900) {
		t.Errorf("a window parameter changed the answer: %v", wide)
	}
}

// The response type is an allow-list: its own fields only, none promoted from
// userJSON or memberJSON, which carry emails, names and per-user timestamps.
func TestActiveUsersJSONIsAnAllowList(t *testing.T) {
	typ := reflect.TypeOf(activeUsersJSON{})
	var names []string
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Anonymous {
			t.Errorf("activeUsersJSON embeds %s", f.Type)
		}
		names = append(names, f.Name)
	}
	if want := []string{"Active", "WindowSeconds", "AsOf"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("fields = %v, want %v", names, want)
	}
}

// With auth off nothing writes last_used_at, so the answer is "unknown", not 0.
func TestActiveUsersAuthOffIs503NotZero(t *testing.T) {
	s := testStore(t)
	admin := mintServiceToken(t, s, "satlink", store.ScopeAdminRead)
	srv := httptest.NewServer(New(s, "").Handler()) // user auth off
	t.Cleanup(srv.Close)

	resp := do(t, http.MethodGet, srv.URL+"/admin/users/active", admin, nil)
	body := string(mustRead(t, resp))
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	if strings.Contains(body, "active\"") || !strings.Contains(body, "LYCEUM_AUTH") {
		t.Fatalf("body = %q, want a reason naming LYCEUM_AUTH and no count", body)
	}
}

func mustRead(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func decodeBytes[T any](t *testing.T, b []byte) T {
	t.Helper()
	return decode[T](t, &http.Response{Body: io.NopCloser(strings.NewReader(string(b)))})
}

// failingCount is a store whose count query fails while token lookup still works,
// so the failure under test is the handler's, not requireScope's.
type failingCount struct{ *store.Store }

func (failingCount) CountActiveUsers(context.Context, time.Duration) (int, time.Time, error) {
	return 0, time.Time{}, errors.New("boom: secret detail")
}

// A failed count query is 503 with a short reason and no count, never a 500 and
// never {active: 0}.
func TestActiveUsersCountFailureIs503(t *testing.T) {
	s := testStore(t)
	admin := mintServiceToken(t, s, "satlink", store.ScopeAdminRead)
	srv := httptest.NewServer(New(failingCount{s}, "", WithUserAuth(true)).Handler())
	t.Cleanup(srv.Close)

	resp := do(t, http.MethodGet, srv.URL+"/admin/users/active", admin, nil)
	body := string(mustRead(t, resp))
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	if strings.Contains(body, "active\"") || strings.Contains(body, "{") || strings.Contains(body, "boom") {
		t.Fatalf("body = %q, want a short reason with no count and no error detail", body)
	}
}
