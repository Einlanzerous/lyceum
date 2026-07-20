package api

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// --- test helpers -----------------------------------------------------------

const (
	testTeamDomain = "test-team.cloudflareaccess.com"
	testIssuer     = "https://test-team.cloudflareaccess.com"
	testAUD        = "test-audience-tag-abcdef0123456789"
	testKID        = "key-1"
)

func testRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

// jwksBody is a Cloudflare-style JWKS document carrying one RSA public key.
func jwksBody(kid string, pub *rsa.PublicKey) map[string]any {
	return map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA",
			"kid": kid,
			"alg": "RS256",
			"use": "sig",
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}},
	}
}

// jwksServer serves a static JWKS for one RSA public key, counting the number of
// times it is fetched so cache behaviour can be asserted.
func jwksServer(t *testing.T, kid string, pub *rsa.PublicKey, hits *int32) *httptest.Server {
	t.Helper()
	body := jwksBody(kid, pub)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits != nil {
			atomic.AddInt32(hits, 1)
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// verifierFor builds a verifier whose JWKS fetch is redirected at the test
// server rather than the real Cloudflare certs endpoint.
func verifierFor(jwksURL, aud string) *CFAccessVerifier {
	v := NewCFAccessVerifier(testTeamDomain, aud)
	v.certsURL = jwksURL
	return v
}

// signToken builds and RS256-signs a JWT with the given header alg/kid and claims.
func signToken(t *testing.T, key *rsa.PrivateKey, alg, kid string, claims map[string]any) string {
	t.Helper()
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	header := enc(map[string]string{"alg": alg, "kid": kid, "typ": "JWT"})
	payload := enc(claims)
	signing := header + "." + payload

	if alg == "none" {
		return signing + "."
	}
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// validClaims is a well-formed claim set; cases mutate a copy to break one thing.
func validClaims() map[string]any {
	now := time.Now()
	return map[string]any{
		"iss":   testIssuer,
		"aud":   []string{testAUD},
		"email": "reader@home.lan",
		"sub":   "abc123",
		"iat":   now.Add(-time.Minute).Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}
}

// --- tests ------------------------------------------------------------------

func TestCFAccessVerifyHappyPath(t *testing.T) {
	key := testRSAKey(t)
	srv := jwksServer(t, testKID, &key.PublicKey, nil)
	v := verifierFor(srv.URL, testAUD)

	token := signToken(t, key, "RS256", testKID, validClaims())
	email, err := v.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if email != "reader@home.lan" {
		t.Fatalf("email = %q, want reader@home.lan", email)
	}
}

func TestCFAccessVerifyAudAsString(t *testing.T) {
	// Cloudflare may emit aud as a bare string rather than an array.
	key := testRSAKey(t)
	srv := jwksServer(t, testKID, &key.PublicKey, nil)
	v := verifierFor(srv.URL, testAUD)

	claims := validClaims()
	claims["aud"] = testAUD // string, not []string
	token := signToken(t, key, "RS256", testKID, claims)
	if _, err := v.Verify(context.Background(), token); err != nil {
		t.Fatalf("Verify with string aud: %v", err)
	}
}

func TestCFAccessVerifyRejections(t *testing.T) {
	key := testRSAKey(t)
	otherKey := testRSAKey(t)
	srv := jwksServer(t, testKID, &key.PublicKey, nil)
	v := verifierFor(srv.URL, testAUD)
	ctx := context.Background()

	cases := []struct {
		name  string
		token string
	}{
		{"bad signature (unknown key)", signToken(t, otherKey, "RS256", testKID, validClaims())},
		{"alg none", signToken(t, key, "none", testKID, validClaims())},
		{"unknown kid", signToken(t, key, "RS256", "no-such-kid", validClaims())},
		{"not a jwt", "not-a-jwt"},
		{"wrong audience", func() string {
			c := validClaims()
			c["aud"] = []string{"some-other-aud"}
			return signToken(t, key, "RS256", testKID, c)
		}()},
		{"wrong issuer", func() string {
			c := validClaims()
			c["iss"] = "https://evil.cloudflareaccess.com"
			return signToken(t, key, "RS256", testKID, c)
		}()},
		{"expired", func() string {
			c := validClaims()
			c["exp"] = time.Now().Add(-time.Hour).Unix()
			return signToken(t, key, "RS256", testKID, c)
		}()},
		{"not yet valid (nbf)", func() string {
			c := validClaims()
			c["nbf"] = time.Now().Add(time.Hour).Unix()
			return signToken(t, key, "RS256", testKID, c)
		}()},
		{"no exp", func() string {
			c := validClaims()
			delete(c, "exp")
			return signToken(t, key, "RS256", testKID, c)
		}()},
		{"missing email", func() string {
			c := validClaims()
			delete(c, "email")
			return signToken(t, key, "RS256", testKID, c)
		}()},
		{"empty email", func() string {
			c := validClaims()
			c["email"] = ""
			return signToken(t, key, "RS256", testKID, c)
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := v.Verify(ctx, tc.token); err == nil {
				t.Fatalf("Verify(%s) succeeded, want rejection", tc.name)
			}
		})
	}
}

// The JWKS is fetched once and served from cache on subsequent verifications
// within the cache window.
func TestCFAccessVerifyCachesJWKS(t *testing.T) {
	key := testRSAKey(t)
	var hits int32
	srv := jwksServer(t, testKID, &key.PublicKey, &hits)
	v := verifierFor(srv.URL, testAUD)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		token := signToken(t, key, "RS256", testKID, validClaims())
		if _, err := v.Verify(ctx, token); err != nil {
			t.Fatalf("Verify #%d: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("JWKS fetched %d times, want 1 (cached)", got)
	}
}

// Within the refresh cooldown, an unknown kid does NOT trigger another fetch —
// this throttles a burst of tokens signed by a rotated-away key (mirrors jose's
// 30s cooldown). The token is still rejected; we just don't hammer the endpoint.
func TestCFAccessVerifyCooldownSuppressesRefetch(t *testing.T) {
	key := testRSAKey(t)
	var hits int32
	srv := jwksServer(t, testKID, &key.PublicKey, &hits)
	v := verifierFor(srv.URL, testAUD)
	ctx := context.Background()

	if _, err := v.Verify(ctx, signToken(t, key, "RS256", testKID, validClaims())); err != nil {
		t.Fatalf("prime: %v", err)
	}
	// Unknown kid immediately after the prime: inside the cooldown, so no refetch.
	if _, err := v.Verify(ctx, signToken(t, key, "RS256", "key-2", validClaims())); err == nil {
		t.Fatal("Verify with unknown kid succeeded, want rejection")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("JWKS fetched %d times, want 1 (cooldown suppresses refetch)", got)
	}
}

// Once the cooldown has elapsed, a key that rotated in after the first fetch is
// picked up: the unknown kid forces a refresh and the new token then verifies.
func TestCFAccessVerifyRefetchesAfterCooldown(t *testing.T) {
	key1 := testRSAKey(t)
	key2 := testRSAKey(t)
	var hits int32

	// A JWKS server whose served key set can be swapped (a rotation).
	served := &atomic.Value{}
	served.Store(jwksBody(testKID, &key1.PublicKey))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_ = json.NewEncoder(w).Encode(served.Load())
	}))
	t.Cleanup(srv.Close)

	v := verifierFor(srv.URL, testAUD)
	ctx := context.Background()

	if _, err := v.Verify(ctx, signToken(t, key1, "RS256", testKID, validClaims())); err != nil {
		t.Fatalf("prime: %v", err)
	}

	// Rotate: the server now serves key-2, and push our last fetch past the
	// cooldown so the next unknown kid is allowed to refetch.
	served.Store(jwksBody("key-2", &key2.PublicKey))
	v.mu.Lock()
	v.lastFetch = time.Now().Add(-cfJWKSRefreshCooldown - time.Second)
	v.mu.Unlock()

	if _, err := v.Verify(ctx, signToken(t, key2, "RS256", "key-2", validClaims())); err != nil {
		t.Fatalf("Verify after rotation: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("JWKS fetched %d times, want 2 (refetch after cooldown)", got)
	}
}

// rsaPublicKey round-trips a JWK-encoded modulus/exponent back to a usable key.
func TestRSAPublicKeyRoundTrip(t *testing.T) {
	key := testRSAKey(t)
	n := base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes())
	pub, err := rsaPublicKey(n, e)
	if err != nil {
		t.Fatalf("rsaPublicKey: %v", err)
	}
	if pub.N.Cmp(key.PublicKey.N) != 0 || pub.E != key.PublicKey.E {
		t.Fatalf("round-trip mismatch: got E=%d, want E=%d", pub.E, key.PublicKey.E)
	}
	_ = fmt.Sprint(pub) // keep fmt import if trimmed later
}
