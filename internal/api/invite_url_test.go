package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/magos/lyceum/internal/store"
)

// The origin a phone can reach, deliberately unlike the httptest server's own —
// the whole point of LYCM-102 is that invites advertise an origin the minting
// client does not necessarily know.
const testMobileBase = "https://lyceum-direct.example.test"

// mobileInviteServer starts an auth-enforcing API that advertises a mobile
// origin, i.e. a server configured the way prod is behind Cloudflare Access.
func mobileInviteServer(t *testing.T, s *store.Store) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(
		New(s, "", WithUserAuth(true), WithMobileBaseURL(testMobileBase)).Handler(),
	)
	t.Cleanup(srv.Close)
	return srv
}

type invitePayload struct {
	InviteToken string `json:"invite_token"`
	PairingCode string `json:"pairing_code"`
	SignInURL   string `json:"sign_in_url"`
}

// Every route that mints an invite has to carry the sign-in URL. Missing it on
// one of them is invisible on the server and shows up as a scan that dead-ends,
// on a phone, in someone else's hands.
func TestInviteRoutesCarrySignInURL(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	srv := mobileInviteServer(t, s)
	ownerToken := signIn(t, s, srv, ownerID(ctx, t, s))

	created := decode[invitePayload](t, do(t, http.MethodPost, srv.URL+"/admin/users", ownerToken,
		map[string]string{"email": "mara@example.com", "display_name": "Mara"}))
	member := decode[struct {
		User userJSON `json:"user"`
	}](t, do(t, http.MethodPost, srv.URL+"/admin/users", ownerToken,
		map[string]string{"email": "kit@example.com", "display_name": "Kit"}))

	cases := []struct {
		name string
		got  invitePayload
	}{
		{"admin creates a member", created},
		{
			"admin re-invites a member",
			decode[invitePayload](t, do(t, http.MethodPost,
				srv.URL+"/admin/users/"+itoa(member.User.ID)+"/invite", ownerToken, nil)),
		},
		{
			"a member adds their own device",
			decode[invitePayload](t, do(t, http.MethodPost, srv.URL+"/auth/invite", ownerToken, nil)),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got.InviteToken == "" {
				t.Fatal("no invite_token returned")
			}
			want := testMobileBase + "/sign-in?token=" + tc.got.InviteToken
			if tc.got.SignInURL != want {
				t.Errorf("sign_in_url = %q, want %q", tc.got.SignInURL, want)
			}
			// The QR is only useful if it redeems the invite it was shown with.
			if !strings.Contains(tc.got.SignInURL, tc.got.InviteToken) {
				t.Errorf("sign_in_url %q does not carry the token it was minted with", tc.got.SignInURL)
			}
		})
	}
}

// A scanned sign_in_url has to redeem, not merely look right — this is the join
// between the server-built link and the token semantics it wraps.
func TestSignInURLTokenRedeems(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	srv := mobileInviteServer(t, s)
	ownerToken := signIn(t, s, srv, ownerID(ctx, t, s))

	inv := decode[invitePayload](t, do(t, http.MethodPost, srv.URL+"/admin/users", ownerToken,
		map[string]string{"email": "wren@example.com", "display_name": "Wren"}))

	// Pull the token back out of the URL exactly as a scanning client does,
	// rather than reusing the one beside it in the payload.
	_, query, found := strings.Cut(inv.SignInURL, "?token=")
	if !found {
		t.Fatalf("sign_in_url %q has no ?token= to parse", inv.SignInURL)
	}

	resp := do(t, http.MethodPost, srv.URL+"/auth/session", "",
		map[string]string{"token": query, "device_label": "Pixel 8"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("redeeming the token from sign_in_url = %d, want 200", resp.StatusCode)
	}
	session := decode[struct {
		User userJSON `json:"user"`
	}](t, resp)
	if session.User.Email != "wren@example.com" {
		t.Fatalf("redeemed as %q, want wren@example.com", session.User.Email)
	}
}

// Unset is the LAN and dev case: the field must be absent rather than empty or
// half-built, so clients fall through to the origin they already know.
func TestInviteOmitsSignInURLWhenUnconfigured(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	srv := authServer(t, s) // no WithMobileBaseURL
	ownerToken := signIn(t, s, srv, ownerID(ctx, t, s))

	resp := do(t, http.MethodPost, srv.URL+"/admin/users", ownerToken,
		map[string]string{"email": "ori@example.com", "display_name": "Ori"})
	raw := decode[map[string]any](t, resp)

	if _, present := raw["sign_in_url"]; present {
		t.Errorf("sign_in_url present with no mobile base URL configured: %v", raw["sign_in_url"])
	}
	if raw["invite_token"] == "" {
		t.Error("invite_token missing; the unconfigured path must still mint")
	}
}
