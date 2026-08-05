package api

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

// The origin a phone can reach, deliberately unlike the httptest server's own —
// the whole point of LYCM-102 is that invites advertise an origin the minting
// client does not necessarily know.
const testMobileBase = "https://lyceum-direct.example.test"

type invitePayload struct {
	InviteToken string `json:"invite_token"`
	PairingCode string `json:"pairing_code"`
	SignInURL   string `json:"sign_in_url"`
}

// assertSignInURL checks the link is the advertised origin's /sign-in carrying
// this exact invite. The token is read back through URL parsing rather than
// string comparison, so the escaping the server applies is asserted by
// round-trip rather than by rebuilding the same string the server built.
func assertSignInURL(t *testing.T, got invitePayload) {
	t.Helper()
	if got.InviteToken == "" {
		t.Fatal("no invite_token returned")
	}
	u, err := url.Parse(got.SignInURL)
	if err != nil {
		t.Fatalf("sign_in_url %q does not parse: %v", got.SignInURL, err)
	}
	if origin := u.Scheme + "://" + u.Host; origin != testMobileBase {
		t.Errorf("sign_in_url origin = %q, want %q", origin, testMobileBase)
	}
	if u.Path != "/sign-in" {
		t.Errorf("sign_in_url path = %q, want /sign-in", u.Path)
	}
	// The QR is only useful if it redeems the invite it was shown beside.
	if tok := u.Query().Get("token"); tok != got.InviteToken {
		t.Errorf("sign_in_url carries token %q, want %q", tok, got.InviteToken)
	}
}

// Every route that mints an invite has to carry the sign-in URL. Missing it on
// one of them is invisible on the server and shows up as a scan that dead-ends,
// on a phone, in someone else's hands.
func TestInviteRoutesCarrySignInURL(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	srv := authServer(t, s, WithMobileBaseURL(testMobileBase))
	ownerToken := signIn(t, s, srv, ownerID(ctx, t, s))

	created := decode[invitePayload](t, do(t, http.MethodPost, srv.URL+"/admin/users", ownerToken,
		map[string]string{"email": "mara@example.com", "display_name": "Mara"}))

	// A second account, redeemed into a real session, so the "add a device" case
	// runs as a member rather than as the owner — that route exists precisely to
	// serve someone who is not the owner, and calling it with the owner's token
	// would leave the path it was written for untested.
	member := decode[struct {
		User        userJSON `json:"user"`
		InviteToken string   `json:"invite_token"`
	}](t, do(t, http.MethodPost, srv.URL+"/admin/users", ownerToken,
		map[string]string{"email": "kit@example.com", "display_name": "Kit"}))
	memberSession := decode[struct {
		SessionToken string `json:"session_token"`
	}](t, do(t, http.MethodPost, srv.URL+"/auth/session", "",
		map[string]string{"token": member.InviteToken, "device_label": "Kit's Pixel"}))
	if memberSession.SessionToken == "" {
		t.Fatal("member could not redeem their invite; the self-invite case cannot run")
	}
	if member.User.IsOwner {
		t.Fatal("the second account is the owner; the member path would not be exercised")
	}

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
			decode[invitePayload](t, do(t, http.MethodPost, srv.URL+"/auth/invite",
				memberSession.SessionToken, nil)),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { assertSignInURL(t, tc.got) })
	}
}

// A scanned sign_in_url has to redeem, not merely look right — this is the join
// between the server-built link and the token semantics it wraps.
func TestSignInURLTokenRedeems(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	srv := authServer(t, s, WithMobileBaseURL(testMobileBase))
	ownerToken := signIn(t, s, srv, ownerID(ctx, t, s))

	inv := decode[invitePayload](t, do(t, http.MethodPost, srv.URL+"/admin/users", ownerToken,
		map[string]string{"email": "wren@example.com", "display_name": "Wren"}))

	// Pull the token back out of the URL exactly as a scanning client does,
	// rather than reusing the one beside it in the payload.
	u, err := url.Parse(inv.SignInURL)
	if err != nil {
		t.Fatalf("sign_in_url %q does not parse: %v", inv.SignInURL, err)
	}
	scanned := u.Query().Get("token")
	if scanned == "" {
		t.Fatalf("sign_in_url %q carries no token to redeem", inv.SignInURL)
	}

	resp := do(t, http.MethodPost, srv.URL+"/auth/session", "",
		map[string]string{"token": scanned, "device_label": "Pixel 8"})
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

	raw := decode[map[string]any](t, do(t, http.MethodPost, srv.URL+"/admin/users", ownerToken,
		map[string]string{"email": "ori@example.com", "display_name": "Ori"}))

	if _, present := raw["sign_in_url"]; present {
		t.Errorf("sign_in_url present with no mobile base URL configured: %v", raw["sign_in_url"])
	}
	// Compared as a string: an absent key decodes to a nil `any`, which is not
	// equal to "" and would let a payload that minted nothing pass unnoticed.
	if token, ok := raw["invite_token"].(string); !ok || token == "" {
		t.Errorf("invite_token = %v; the unconfigured path must still mint", raw["invite_token"])
	}
}

// A malformed base is refused rather than stored, so the field stays absent and
// clients keep the local fallback that would still have worked. Storing it would
// cost both: a QR that scans nowhere, and no fallback behind it.
func TestInviteOmitsSignInURLWhenBaseIsUnusable(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	srv := authServer(t, s, WithMobileBaseURL("lyceum-direct.example.test")) // no scheme
	ownerToken := signIn(t, s, srv, ownerID(ctx, t, s))

	raw := decode[map[string]any](t, do(t, http.MethodPost, srv.URL+"/admin/users", ownerToken,
		map[string]string{"email": "sol@example.com", "display_name": "Sol"}))

	if v, present := raw["sign_in_url"]; present {
		t.Errorf("sign_in_url = %v for an unusable base; it must be dropped so the client fallback survives", v)
	}
	if token, ok := raw["invite_token"].(string); !ok || token == "" {
		t.Errorf("invite_token = %v; a bad base must not stop invites minting", raw["invite_token"])
	}
}

// A trailing slash on the configured base must not produce a doubled path: the
// normalization happens once at construction, not per mint.
func TestInviteBaseIsNormalizedOnce(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	srv := authServer(t, s, WithMobileBaseURL(testMobileBase+"/"))
	ownerToken := signIn(t, s, srv, ownerID(ctx, t, s))

	inv := decode[invitePayload](t, do(t, http.MethodPost, srv.URL+"/admin/users", ownerToken,
		map[string]string{"email": "juno@example.com", "display_name": "Juno"}))

	assertSignInURL(t, inv)
}
