package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/magos/lyceum/internal/store"
)

// A freshly created member's invite comes with a short pairing code, and typing
// that code in signs the new device in — the whole point of LYCM-88.
func TestAdminCreateReturnsWorkingPairingCode(t *testing.T) {
	s := testStore(t)
	srv := authServer(t, s)
	ctx := context.Background()

	ownerToken := signIn(t, s, srv, ownerID(ctx, t, s))

	resp := do(t, http.MethodPost, srv.URL+"/admin/users", ownerToken,
		map[string]string{"email": "theo@home.lan", "display_name": "Theo"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /admin/users = %d, want 201", resp.StatusCode)
	}
	created := decode[struct {
		User        struct{ Email string } `json:"user"`
		InviteToken string                 `json:"invite_token"`
		PairingCode string                 `json:"pairing_code"`
	}](t, resp)
	if created.InviteToken == "" || created.PairingCode == "" {
		t.Fatalf("missing credential in reveal: token=%q code=%q", created.InviteToken, created.PairingCode)
	}

	// Redeem via the code (as a person would type it), not the token.
	redeem := do(t, http.MethodPost, srv.URL+"/auth/session", "",
		map[string]string{"code": created.PairingCode, "device_label": "Pixel 8"})
	if redeem.StatusCode != http.StatusOK {
		t.Fatalf("POST /auth/session {code} = %d, want 200", redeem.StatusCode)
	}
	session := decode[struct {
		User         struct{ Email string } `json:"user"`
		SessionToken string                 `json:"session_token"`
	}](t, redeem)
	if session.SessionToken == "" || session.User.Email != "theo@home.lan" {
		t.Fatalf("code redemption returned %+v, want a session for theo@home.lan", session)
	}

	// The session it produced actually authenticates.
	me := do(t, http.MethodGet, srv.URL+"/auth/me", session.SessionToken, nil)
	if me.StatusCode != http.StatusOK {
		t.Fatalf("GET /auth/me with code-minted session = %d, want 200", me.StatusCode)
	}
}

// A wrong pairing code is the same generic 401 as a wrong token — no leak that
// codes are a distinct, smaller-keyspace credential.
func TestSignInWrongPairingCodeIs401(t *testing.T) {
	s := testStore(t)
	srv := authServer(t, s)

	resp := do(t, http.MethodPost, srv.URL+"/auth/session", "",
		map[string]string{"code": "BK4T9Q2M", "device_label": "dev"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST /auth/session {wrong code} = %d, want 401", resp.StatusCode)
	}
}

// The code path is rate-limited per client so its small keyspace can't be
// hammered; the burst is pairingRateBurst, after which the limiter answers 429.
func TestPairingCodeSignInIsRateLimited(t *testing.T) {
	s := testStore(t)
	srv := authServer(t, s)

	// Exhaust the burst with wrong codes — each is a normal 401 attempt.
	for i := 0; i < pairingRateBurst; i++ {
		resp := do(t, http.MethodPost, srv.URL+"/auth/session", "",
			map[string]string{"code": "BK4T9Q2M", "device_label": "dev"})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401 within the burst", i+1, resp.StatusCode)
		}
	}
	// One more from the same client trips the limiter.
	resp := do(t, http.MethodPost, srv.URL+"/auth/session", "",
		map[string]string{"code": "BK4T9Q2M", "device_label": "dev"})
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("attempt past the burst = %d, want 429", resp.StatusCode)
	}
}

// The token path must not be rate-limited: a 256-bit token needs no throttle,
// and coupling it to the code limiter would let code-guessing lock out real
// token sign-ins.
func TestTokenSignInNotRateLimited(t *testing.T) {
	s := testStore(t)
	srv := authServer(t, s)
	ctx := context.Background()

	// Far more attempts than the pairing burst, all on the token path.
	for i := 0; i < pairingRateBurst+5; i++ {
		invite, err := s.MintToken(ctx, ownerID(ctx, t, s), store.TokenInvite, "test", nil)
		if err != nil {
			t.Fatalf("MintToken: %v", err)
		}
		resp := do(t, http.MethodPost, srv.URL+"/auth/session", "",
			map[string]string{"token": invite, "device_label": "dev"})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("token sign-in %d = %d, want 200 (token path is never throttled)", i+1, resp.StatusCode)
		}
	}
}
