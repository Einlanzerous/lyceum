package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/magos/lyceum/internal/store"
)

// cfAccessServer starts an API with user auth ON and the given Cloudflare Access
// verifier installed (pass nil to leave SSO unconfigured).
func cfAccessServer(t *testing.T, s *store.Store, v *CFAccessVerifier) *httptest.Server {
	t.Helper()
	opts := []Option{WithUserAuth(true)}
	if v != nil {
		opts = append(opts, WithCFAccess(v))
	}
	srv := httptest.NewServer(New(s, "", opts...).Handler())
	t.Cleanup(srv.Close)
	return srv
}

// postCF issues POST /auth/sso/cloudflare with an optional Cf-Access-Jwt-Assertion.
func postCF(t *testing.T, url, jwt string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url+"/auth/sso/cloudflare", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if jwt != "" {
		req.Header.Set("Cf-Access-Jwt-Assertion", jwt)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /auth/sso/cloudflare: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// When the feature is unconfigured, the endpoint reports sso_disabled so the SPA
// falls back to invite/pairing sign-in.
func TestCFAccessSSODisabled(t *testing.T) {
	s := testStore(t)
	srv := cfAccessServer(t, s, nil)

	resp := postCF(t, srv.URL, "anything")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	body := decode[ssoErrorBody](t, resp)
	if body.Error != "sso_disabled" {
		t.Fatalf("error = %q, want sso_disabled", body.Error)
	}
}

// Configured, but no tunnel header on the request: also sso_disabled (nothing to
// verify — the request didn't come through Cloudflare).
func TestCFAccessSSOMissingHeader(t *testing.T) {
	s := testStore(t)
	key := testRSAKey(t)
	jwks := jwksServer(t, testKID, &key.PublicKey, nil)
	srv := cfAccessServer(t, s, verifierFor(jwks.URL, testAUD))

	resp := postCF(t, srv.URL, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if body := decode[ssoErrorBody](t, resp); body.Error != "sso_disabled" {
		t.Fatalf("error = %q, want sso_disabled", body.Error)
	}
}

// A header that fails verification is a generic unauthorized.
func TestCFAccessSSOInvalidToken(t *testing.T) {
	s := testStore(t)
	key := testRSAKey(t)
	other := testRSAKey(t)
	jwks := jwksServer(t, testKID, &key.PublicKey, nil)
	srv := cfAccessServer(t, s, verifierFor(jwks.URL, testAUD))

	// Signed by a key the JWKS doesn't contain.
	bad := signToken(t, other, "RS256", testKID, validClaims())
	resp := postCF(t, srv.URL, bad)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if body := decode[ssoErrorBody](t, resp); body.Error != "unauthorized" {
		t.Fatalf("error = %q, want unauthorized", body.Error)
	}
}

// A verified email with no Lyceum account is refused, never auto-provisioned,
// and the refusal names the email so the person knows what to ask for.
func TestCFAccessSSONoAccount(t *testing.T) {
	s := testStore(t)
	key := testRSAKey(t)
	jwks := jwksServer(t, testKID, &key.PublicKey, nil)
	srv := cfAccessServer(t, s, verifierFor(jwks.URL, testAUD))

	claims := validClaims()
	claims["email"] = "stranger@home.lan"
	token := signToken(t, key, "RS256", testKID, claims)

	resp := postCF(t, srv.URL, token)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	body := decode[ssoErrorBody](t, resp)
	if body.Error != "sso_no_account" {
		t.Fatalf("error = %q, want sso_no_account", body.Error)
	}
	if body.Email != "stranger@home.lan" {
		t.Fatalf("email = %q, want the refused address echoed back", body.Email)
	}

	// And no account was created as a side effect.
	if _, err := s.GetUserByEmail(context.Background(), "stranger@home.lan"); err == nil {
		t.Fatal("a user was auto-provisioned; the CF gate must never create accounts")
	}
}

// The happy path: a verified email matching an account (case-insensitively)
// mints a working session and sets the cookie.
func TestCFAccessSSOHappyPath(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.CreateUser(ctx, "mara@home.lan", "Mara"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	key := testRSAKey(t)
	jwks := jwksServer(t, testKID, &key.PublicKey, nil)
	srv := cfAccessServer(t, s, verifierFor(jwks.URL, testAUD))

	// Cloudflare hands back the address in whatever case the IdP holds; the match
	// must be case-insensitive.
	claims := validClaims()
	claims["email"] = "Mara@Home.LAN"
	token := signToken(t, key, "RS256", testKID, claims)

	resp := postCF(t, srv.URL, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// The session cookie is set on the response.
	var gotCookie bool
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie && c.Value != "" {
			gotCookie = true
		}
	}
	if !gotCookie {
		t.Fatal("no lyceum_session cookie set on SSO sign-in")
	}

	out := decode[struct {
		User         struct{ Email string } `json:"user"`
		SessionToken string                 `json:"session_token"`
	}](t, resp)
	if out.SessionToken == "" {
		t.Fatal("no session_token returned")
	}
	if out.User.Email != "mara@home.lan" {
		t.Fatalf("user email = %q, want normalized mara@home.lan", out.User.Email)
	}

	// The minted session actually authenticates the reader core.
	me := do(t, http.MethodGet, srv.URL+"/auth/me", out.SessionToken, nil)
	if me.StatusCode != http.StatusOK {
		t.Fatalf("GET /auth/me with SSO session = %d, want 200", me.StatusCode)
	}
	if who := decode[struct {
		Email string `json:"email"`
	}](t, me); who.Email != "mara@home.lan" {
		t.Fatalf("/auth/me = %q, want mara@home.lan", who.Email)
	}
}
