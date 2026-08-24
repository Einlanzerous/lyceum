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
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// jwk renders one JWK entry from a real public key.
func jwk(kid string, pub *rsa.PublicKey) map[string]string {
	return rawJWK(kid, "sig",
		base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()))
}

// rawJWK renders one JWK entry from pre-encoded parts, so a case can publish a
// deliberately malformed key.
func rawJWK(kid, use, n, e string) map[string]string {
	return map[string]string{"kty": "RSA", "kid": kid, "alg": "RS256", "use": use, "n": n, "e": e}
}

// jwksBody is a Cloudflare-style JWKS document carrying one RSA public key.
func jwksBody(kid string, pub *rsa.PublicKey) map[string]any {
	return map[string]any{"keys": []map[string]string{jwk(kid, pub)}}
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

// jwksServerServing serves an arbitrary set of JWK entries, so a case can
// publish malformed or non-signing keys alongside a usable one.
func jwksServerServing(t *testing.T, hits *int32, keys ...map[string]string) *httptest.Server {
	t.Helper()
	body := map[string]any{"keys": keys}
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

// --- LYCM-122: malformed keys are contained, not fatal ----------------------

// rsaPublicKey refuses every unusable modulus/exponent instead of panicking.
// The 9-byte exponent is the regression: eBuf[8-9:] is eBuf[-1:], which Go
// panics on before copy is ever called, and a panic on the request goroutine of
// POST /auth/sso/cloudflare takes the process down rather than the request.
func TestRSAPublicKeyBounds(t *testing.T) {
	good := testRSAKey(t)
	goodN := base64.RawURLEncoding.EncodeToString(good.PublicKey.N.Bytes())

	short, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate 1024-bit key: %v", err)
	}
	shortN := base64.RawURLEncoding.EncodeToString(short.PublicKey.N.Bytes())

	b64 := func(b ...byte) string { return base64.RawURLEncoding.EncodeToString(b) }

	cases := []struct {
		name   string
		n, e   string
		wantE  int // expected exponent when accepted
		accept bool
	}{
		{name: "3-byte exponent (65537, the normal case)", n: goodN, e: "AQAB", wantE: 65537, accept: true},
		{name: "1-byte exponent", n: goodN, e: b64(0x03), wantE: 3, accept: true},
		{name: "8-byte exponent in range", n: goodN, e: b64(0, 0, 0, 0, 0, 1, 0, 1), wantE: 65537, accept: true},
		{name: "9-byte exponent", n: goodN, e: "AQIDBAUGBwgJ"},
		{name: "16-byte exponent", n: goodN, e: b64(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16)},
		{name: "empty exponent", n: goodN, e: ""},
		{name: "zero exponent", n: goodN, e: b64(0, 0)},
		{name: "exponent above MaxInt32", n: goodN, e: b64(1, 0, 0, 0, 0)},
		{name: "exponent at MaxUint32", n: goodN, e: b64(0xff, 0xff, 0xff, 0xff)},
		{name: "empty modulus", n: "", e: "AQAB"},
		{name: "1024-bit modulus", n: shortN, e: "AQAB"},
		// A byte-length floor would pass this: 128 zero bytes of padding make a
		// 1024-bit modulus encode as 256 bytes.
		{name: "1024-bit modulus padded to 2048 bits", n: base64.RawURLEncoding.EncodeToString(
			append(make([]byte, 128), short.PublicKey.N.Bytes()...)), e: "AQAB"},
		{name: "modulus not base64url", n: "!!!not base64!!!", e: "AQAB"},
		{name: "exponent not base64url", n: goodN, e: "!!!not base64!!!"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pub, err := rsaPublicKey(tc.n, tc.e) // must not panic
			if !tc.accept {
				if err == nil {
					t.Fatalf("rsaPublicKey accepted %s, want refusal", tc.name)
				}
				return
			}
			if err != nil {
				t.Fatalf("rsaPublicKey: %v", err)
			}
			if pub.E != tc.wantE {
				t.Fatalf("E = %d, want %d", pub.E, tc.wantE)
			}
			if pub.N.Cmp(good.PublicKey.N) != 0 {
				t.Fatal("modulus did not round-trip")
			}
		})
	}
}

// A key set carrying one unusable key still yields the others — the `continue`
// in fetchJWKS does what its comment says. Before LYCM-122 the over-long
// exponent panicked out of the loop instead of being skipped by it, taking the
// server with it.
func TestCFAccessVerifySurvivesPoisonedKey(t *testing.T) {
	key := testRSAKey(t)
	short, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate 1024-bit key: %v", err)
	}
	goodN := base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())

	poison := []map[string]string{
		rawJWK("over-long-exponent", "sig", goodN, "AQIDBAUGBwgJ"),
		rawJWK("empty-exponent", "sig", goodN, ""),
		rawJWK("huge-exponent", "sig", goodN, base64.RawURLEncoding.EncodeToString([]byte{1, 0, 0, 0, 0})),
		rawJWK("downgraded-modulus", "sig", base64.RawURLEncoding.EncodeToString(short.PublicKey.N.Bytes()), "AQAB"),
		rawJWK("not-base64", "sig", "!!!", "!!!"),
	}
	srv := jwksServerServing(t, nil, append(poison, jwk(testKID, &key.PublicKey))...)

	v := verifierFor(srv.URL, testAUD)
	email, err := v.Verify(context.Background(), signToken(t, key, "RS256", testKID, validClaims()))
	if err != nil {
		t.Fatalf("Verify with a poisoned key set: %v", err)
	}
	if email != "reader@home.lan" {
		t.Fatalf("email = %q, want reader@home.lan", email)
	}

	// The unusable keys were skipped, not admitted.
	v.mu.RLock()
	defer v.mu.RUnlock()
	if len(v.keys) != 1 {
		t.Fatalf("cached %d keys, want 1 (the usable one)", len(v.keys))
	}
	if _, ok := v.keys[testKID]; !ok {
		t.Fatalf("cached keys = %v, want only %q", v.keys, testKID)
	}
}

// A JWK marked for encryption is not a signing key. Cloudflare is allowed to
// publish key types this verifier doesn't use.
func TestCFAccessVerifySkipsNonSigningKeys(t *testing.T) {
	key := testRSAKey(t)
	enc := jwk("enc-key", &key.PublicKey)
	enc["use"] = "enc"
	srv := jwksServerServing(t, nil, enc, jwk(testKID, &key.PublicKey))

	v := verifierFor(srv.URL, testAUD)
	if _, err := v.Verify(context.Background(), signToken(t, key, "RS256", testKID, validClaims())); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if _, err := v.Verify(context.Background(), signToken(t, key, "RS256", "enc-key", validClaims())); err == nil {
		t.Fatal("Verify against an enc-use key succeeded, want rejection")
	}
}

// The remote key document is read through a limit, so a certs endpoint that
// serves without end can't exhaust memory on the request path. The document
// here is otherwise perfectly good — an unbounded read would parse it and
// return the key, so only the limit can make this fail.
func TestFetchJWKSBoundsTheRemoteDocument(t *testing.T) {
	key := testRSAKey(t)
	good, err := json.Marshal(map[string]any{"keys": []map[string]string{jwk(testKID, &key.PublicKey)}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// A usable key set, preceded by padding that runs past the limit.
	body := `{"pad":"` + strings.Repeat("x", 2*maxJWKSBytes) + `",` + string(good[1:])
	if len(body) <= maxJWKSBytes {
		t.Fatalf("test document is %d bytes, want more than the %d-byte limit", len(body), maxJWKSBytes)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	if _, err := fetchJWKS(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatal("fetchJWKS read an oversized document to completion, want it bounded")
	}
}

// --- LYCM-122: the refresh cooldown applies before the first key set --------

// Cold start with a failing certs endpoint: every sign-in attempt used to
// refetch, because the cooldown was gated on `v.keys != nil` and keys stays nil
// exactly when the endpoint is down. That made POST /auth/sso/cloudflare a
// request amplifier pointed at Cloudflare — one unauthenticated request in, one
// JWKS fetch out.
func TestCFAccessCooldownAppliesOnColdStart(t *testing.T) {
	key := testRSAKey(t)
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	v := verifierFor(srv.URL, testAUD)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		if _, err := v.Verify(ctx, signToken(t, key, "RS256", testKID, validClaims())); err == nil {
			t.Fatalf("Verify #%d succeeded against a failing certs endpoint", i)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("certs endpoint fetched %d times for 10 requests, want 1 (cooldown)", got)
	}
}

// The cooldown throttles the retry, it doesn't abandon it: once it elapses, a
// certs endpoint that has come back is picked up.
func TestCFAccessRecoversAfterColdStartFailure(t *testing.T) {
	key := testRSAKey(t)
	var fail atomic.Bool
	fail.Store(true)
	var hits int32
	body := jwksBody(testKID, &key.PublicKey)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		if fail.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)

	v := verifierFor(srv.URL, testAUD)
	ctx := context.Background()
	if _, err := v.Verify(ctx, signToken(t, key, "RS256", testKID, validClaims())); err == nil {
		t.Fatal("Verify succeeded while the certs endpoint was failing")
	}

	fail.Store(false)
	v.mu.Lock()
	v.lastFetch = time.Now().Add(-cfJWKSRefreshCooldown - time.Second)
	v.mu.Unlock()

	if _, err := v.Verify(ctx, signToken(t, key, "RS256", testKID, validClaims())); err != nil {
		t.Fatalf("Verify after the endpoint recovered: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("certs endpoint fetched %d times, want 2", got)
	}
}

// A fetch that fails leaves the working key set in place rather than clearing
// it: a Cloudflare blip must not become a total sign-in outage.
func TestCFAccessKeepsKeysWhenRefreshFails(t *testing.T) {
	key := testRSAKey(t)
	var fail atomic.Bool
	body := jwksBody(testKID, &key.PublicKey)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)

	v := verifierFor(srv.URL, testAUD)
	ctx := context.Background()
	if _, err := v.Verify(ctx, signToken(t, key, "RS256", testKID, validClaims())); err != nil {
		t.Fatalf("prime: %v", err)
	}

	// The endpoint starts failing and the cache goes stale: the held key is
	// still served rather than the request being rejected.
	fail.Store(true)
	v.mu.Lock()
	v.fetchedAt = time.Now().Add(-cfJWKSCacheMaxAge - time.Second)
	v.lastFetch = time.Now().Add(-cfJWKSRefreshCooldown - time.Second)
	v.mu.Unlock()

	if _, err := v.Verify(ctx, signToken(t, key, "RS256", testKID, validClaims())); err != nil {
		t.Fatalf("Verify with a stale-but-held key during an outage: %v", err)
	}
}

// A caller that gives up mid-fetch must not consume the cooldown on everybody
// else's behalf. handleAuthCFAccess passes r.Context(), so before the fetch was
// detached, one abandoned request at a cold start refused every sign-in for the
// next 30 seconds against a perfectly healthy certs endpoint — and with no key
// set held, there was nothing to fall back on.
func TestCFAccessColdStartSurvivesAbandonedRequest(t *testing.T) {
	key := testRSAKey(t)
	var hits int32
	body := jwksBody(testKID, &key.PublicKey)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		time.Sleep(300 * time.Millisecond) // the fetch is in flight
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)

	v := verifierFor(srv.URL, testAUD)
	token := signToken(t, key, "RS256", testKID, validClaims())

	// The first sign-in after a restart, abandoned while the fetch is running.
	// Its own outcome is moot — the response goes to a closed connection — so
	// what is asserted is that it finished the fetch on everyone's behalf
	// rather than cancelling it and keeping the window it had claimed.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, _ = v.Verify(ctx, token)

	v.mu.RLock()
	populated := len(v.keys) > 0
	v.mu.RUnlock()
	if !populated {
		t.Fatal("the abandoned request cancelled the shared fetch, leaving no keys")
	}

	// The next honest sign-in is answered, not refused for the cooldown window.
	if _, err := v.Verify(context.Background(), token); err != nil {
		t.Fatalf("sign-in after an abandoned cold-start request: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("certs endpoint fetched %d times, want 1 (the detached fetch completed)", got)
	}
}

// Sign-ins arriving together at a cold start — the ordinary case right after a
// deploy — share the one fetch rather than all but the first being refused by
// the cooldown it stamped.
func TestCFAccessColdStartCoalescesConcurrentSignIns(t *testing.T) {
	key := testRSAKey(t)
	var hits int32
	body := jwksBody(testKID, &key.PublicKey)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		time.Sleep(200 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)

	v := verifierFor(srv.URL, testAUD)
	token := signToken(t, key, "RS256", testKID, validClaims())

	const callers = 8
	errs := make(chan error, callers)
	var start sync.WaitGroup
	start.Add(1)
	for i := 0; i < callers; i++ {
		go func() {
			start.Wait()
			_, err := v.Verify(context.Background(), token)
			errs <- err
		}()
	}
	start.Done()

	for i := 0; i < callers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent sign-in %d refused: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("certs endpoint fetched %d times, want 1 (one fetch, shared)", got)
	}
}

// Waiting is for callers with nothing to answer with. A request holding a key
// whose cache has merely gone stale is answered from memory immediately, even
// while somebody else's refresh is in flight — it must never be parked on the
// network for an answer it already has.
func TestCFAccessStaleKeyDoesNotWaitOnAnInflightFetch(t *testing.T) {
	key := testRSAKey(t)
	body := jwksBody(testKID, &key.PublicKey)
	release := make(chan struct{})
	var slow atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if slow.Load() {
			<-release // hold the fetch open
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	v := verifierFor(srv.URL, testAUD)
	token := signToken(t, key, "RS256", testKID, validClaims())
	if _, err := v.Verify(context.Background(), token); err != nil {
		t.Fatalf("prime: %v", err)
	}

	// Age the cache past its max, and park a refresh (triggered by an unknown
	// kid, which has nothing to fall back on) inside the certs endpoint.
	slow.Store(true)
	v.mu.Lock()
	v.fetchedAt = time.Now().Add(-cfJWKSCacheMaxAge - time.Second)
	v.lastFetch = time.Now().Add(-cfJWKSRefreshCooldown - time.Second)
	v.mu.Unlock()

	parked := make(chan struct{})
	go func() {
		defer close(parked)
		_, _ = v.Verify(context.Background(), signToken(t, key, "RS256", "unknown-kid", validClaims()))
	}()
	// Give the parked request time to become the in-flight fetch.
	for i := 0; i < 100; i++ {
		v.mu.RLock()
		running := v.inflight != nil
		v.mu.RUnlock()
		if running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	done := make(chan error, 1)
	go func() {
		_, err := v.Verify(context.Background(), token)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("stale-key sign-in during an in-flight fetch: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a sign-in holding a stale key waited on somebody else's fetch")
	}

	release <- struct{}{}
	<-parked
}
