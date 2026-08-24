package api

// Cloudflare Access SSO sign-in (LYCM-803, mirrors Switchyard's SWY-161).
//
// Lyceum sits behind the Zero Gravity Cloudflare edge (construct-server SERV-24/25).
// Every browser request that reaches us through the tunnel already carries a
// Cloudflare-verified identity in the `Cf-Access-Jwt-Assertion` header — a JWT
// signed by the team domain and stamped with the Access application's audience
// (AUD) tag. This verifier validates that JWT so the browser SPA can be signed
// in with no second login; handleAuthCFAccess (session.go) turns a verified
// email into a Lyceum session.
//
// Why hand-rolled rather than a JWT library: the check is narrow and fixed —
// RS256 against a published JWKS, one issuer, one audience — and Lyceum keeps a
// deliberately small dependency set. The signature verification itself uses only
// stdlib audited primitives (crypto/rsa, crypto/sha256). The dangerous JWT
// footguns are closed explicitly: the algorithm is pinned to RS256 (so a token
// can't downgrade to `none`, and an RSA public key can never be misused as an
// HMAC secret), and issuer/audience/expiry are all checked, never trusted.
//
// Spoofing: a forged `Cf-Access-Jwt-Assertion` on the direct/Tailscale path
// (Lyceum still publishes :4005 to the host) fails signature verification — only
// Cloudflare holds the signing key — so the header authenticates nobody off the
// tunnel. Traefik additionally strips the header on the public entrypoint; it is
// preserved only on the internal (tunnel) entrypoint.

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// cfJWKSCacheMaxAge bounds how long a fetched key set is served before a
	// refresh, and cfJWKSRefreshCooldown throttles refetches triggered by an
	// unknown key id so a burst of tokens signed by a rotated-away key can't
	// hammer the certs endpoint. Both mirror the jose defaults SWY-161 runs on.
	cfJWKSCacheMaxAge     = 10 * time.Minute
	cfJWKSRefreshCooldown = 30 * time.Second
	cfJWKSFetchTimeout    = 10 * time.Second

	// maxJWKSBytes bounds the remote key document, and minRSAModulusBits
	// refuses a downgraded signing key. Both come from construct-server's
	// cf-access-guard, which this file is now level with (LYCM-122).
	maxJWKSBytes      = 1 << 20
	minRSAModulusBits = 2048
)

// CFAccessVerifier verifies Cloudflare Access JWTs for one Access application,
// caching the team domain's JWKS in memory. The zero value is not usable; build
// it with NewCFAccessVerifier.
type CFAccessVerifier struct {
	teamDomain string // bare host, e.g. zero-gravity-industries.cloudflareaccess.com
	issuer     string // https://<teamDomain>
	aud        string // the Access application's audience tag
	certsURL   string // https://<teamDomain>/cdn-cgi/access/certs
	httpClient *http.Client

	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey // kid -> key
	fetchedAt time.Time                 // when keys was last populated
	lastFetch time.Time                 // when a fetch was last attempted (cooldown)
	inflight  chan struct{}             // non-nil while a fetch runs; closed when it settles
	lastErr   error                     // result of the last settled fetch
}

// NewCFAccessVerifier builds a verifier for the given team domain and audience.
// Keys are fetched lazily on the first verification. It is exported so main.go
// can construct one to hand to WithCFAccess; the type itself stays unexported.
func NewCFAccessVerifier(teamDomain, aud string) *CFAccessVerifier {
	teamDomain = strings.TrimSpace(teamDomain)
	return &CFAccessVerifier{
		teamDomain: teamDomain,
		issuer:     "https://" + teamDomain,
		aud:        strings.TrimSpace(aud),
		certsURL:   "https://" + teamDomain + "/cdn-cgi/access/certs",
		httpClient: &http.Client{Timeout: cfJWKSFetchTimeout},
	}
}

// errCFAccessInvalid is the single opaque error every verification failure maps
// to. The handler turns it into one generic 401, so a probe can't distinguish a
// bad signature from a wrong audience from an expired token.
var errCFAccessInvalid = errors.New("invalid Cloudflare Access token")

// cfAccessAudience decodes the JWT `aud` claim, which Cloudflare emits as either
// a single string or an array of strings.
type cfAccessAudience []string

func (a *cfAccessAudience) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*a = cfAccessAudience{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*a = many
	return nil
}

func (a cfAccessAudience) contains(want string) bool {
	for _, v := range a {
		if v == want {
			return true
		}
	}
	return false
}

// cfAccessClaims is the subset of the Access JWT payload Lyceum reads.
type cfAccessClaims struct {
	Iss   string           `json:"iss"`
	Aud   cfAccessAudience `json:"aud"`
	Exp   int64            `json:"exp"`
	Nbf   int64            `json:"nbf"`
	Email string           `json:"email"`
}

// jwtHeader is the decoded JWS header.
type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

// Verify checks a `Cf-Access-Jwt-Assertion` value and returns the verified
// email. It enforces, in order: three JWS segments, RS256, a known signing key,
// a valid RSA signature, the exact issuer and audience, expiry (and not-before
// when present), and a non-empty email claim. Every failure returns
// errCFAccessInvalid.
func (v *CFAccessVerifier) Verify(ctx context.Context, token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errCFAccessInvalid
	}

	var hdr jwtHeader
	if err := decodeSegment(parts[0], &hdr); err != nil {
		return "", errCFAccessInvalid
	}
	// Pin the algorithm: this closes the classic JWT downgrade holes — `none`,
	// and treating the RSA public key as an HMAC secret — by only ever taking
	// the RSA verification path below.
	if hdr.Alg != "RS256" || hdr.Kid == "" {
		return "", errCFAccessInvalid
	}

	key, err := v.key(ctx, hdr.Kid)
	if err != nil {
		return "", errCFAccessInvalid
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", errCFAccessInvalid
	}
	signed := parts[0] + "." + parts[1]
	digest := sha256.Sum256([]byte(signed))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig); err != nil {
		return "", errCFAccessInvalid
	}

	var claims cfAccessClaims
	if err := decodeSegment(parts[1], &claims); err != nil {
		return "", errCFAccessInvalid
	}
	if claims.Iss != v.issuer || !claims.Aud.contains(v.aud) {
		return "", errCFAccessInvalid
	}
	now := time.Now()
	if claims.Exp == 0 || now.After(time.Unix(claims.Exp, 0)) {
		return "", errCFAccessInvalid
	}
	if claims.Nbf != 0 && now.Before(time.Unix(claims.Nbf, 0)) {
		return "", errCFAccessInvalid
	}
	if claims.Email == "" {
		return "", errCFAccessInvalid
	}
	return claims.Email, nil
}

// key returns the RSA public key for kid, refreshing the JWKS when the cache is
// stale or the key is unknown. A stale-but-present key is preferred over a
// failed refresh, so a transient certs-endpoint outage doesn't reject a token
// signed by a key we already hold.
func (v *CFAccessVerifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.RLock()
	k, ok := v.keys[kid]
	fresh := time.Since(v.fetchedAt) < cfJWKSCacheMaxAge
	v.mu.RUnlock()
	if ok && fresh {
		return k, nil
	}

	// Holding no key for this kid means there is nothing to answer with if the
	// refresh is skipped, so it is worth waiting on a fetch another request has
	// already started. Holding a stale one means the opposite: don't queue
	// behind somebody else's fetch for an answer already in memory.
	//
	// Read that precisely — it bounds the wait on ANOTHER caller's fetch, not
	// the wait on the network. A stale-key holder arriving with nothing in
	// flight and the cooldown expired still becomes the fetcher itself and
	// blocks inline before falling back to the key it was holding, exactly as
	// it did before any of this.
	if err := v.refresh(ctx, !ok); err != nil {
		if ok {
			return k, nil // serve the stale key rather than fail on a fetch blip
		}
		return nil, err
	}

	v.mu.RLock()
	k, ok = v.keys[kid]
	v.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("cf access: no signing key for kid %q", kid)
	}
	return k, nil
}

// refresh refetches the JWKS, subject to a cooldown so repeated unknown-kid
// tokens can't spin the certs endpoint. waitForInflight says whether the caller
// has nothing to fall back on, and so would rather wait for a fetch already in
// progress than be refused.
//
// The cooldown is deliberately NOT conditioned on already holding keys. Gating
// it on `v.keys != nil` made it inert in precisely the two situations it exists
// for — a cold start, and a certs endpoint that is failing. In both, keys stays
// nil, so every request refetched immediately and POST /auth/sso/cloudflare
// became a request amplifier pointed at Cloudflare: one unauthenticated request
// in, one JWKS fetch out, no malformed key or hostile input required. The fix
// is construct-server's: stamp the attempt unconditionally, before the fetch.
//
// Stamping it up front is only honest if the attempt actually happens, which is
// why the fetch below is detached from the caller's context and why a second
// request arriving mid-fetch waits rather than being turned away. Without
// those, one browser giving up mid-fetch would consume the whole window on
// everybody's behalf and complete nothing.
func (v *CFAccessVerifier) refresh(ctx context.Context, waitForInflight bool) error {
	v.mu.Lock()
	if done := v.inflight; done != nil {
		v.mu.Unlock()
		if !waitForInflight {
			return errCFAccessCooling
		}
		// Wait for the fetch already running instead of refusing: at a cold
		// start there is no key to fall back on, and a burst of concurrent
		// sign-ins right after a deploy is the ordinary case, not an attack.
		//
		// With both cases ready the select picks at random, so an already
		// cancelled waiter can return ctx.Err() with usable keys sitting in the
		// cache. That costs nothing: its connection is gone either way, and the
		// keys are there for whoever comes next.
		select {
		case <-done:
			v.mu.RLock()
			defer v.mu.RUnlock()
			return v.lastErr
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if !v.lastFetch.IsZero() && time.Since(v.lastFetch) < cfJWKSRefreshCooldown {
		v.mu.Unlock()
		return errCFAccessCooling
	}
	v.lastFetch = time.Now() // stamped for the ATTEMPT, not for its success
	done := make(chan struct{})
	v.inflight = done
	v.mu.Unlock()

	// The fetch fills a cache every caller shares, so it must not run on one
	// caller's request context: handleAuthCFAccess passes r.Context(), so a
	// browser that gives up mid-fetch would cancel it and leave behind the
	// cooldown window it had already consumed — refusing every sign-in for the
	// next 30 seconds against a perfectly healthy certs endpoint, with no key
	// set to fall back on. Detach it and bound it on its own.
	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfJWKSFetchTimeout)
	defer cancel()
	keys, err := fetchJWKS(fetchCtx, v.httpClient, v.certsURL)

	v.mu.Lock()
	defer v.mu.Unlock()
	v.inflight = nil
	v.lastErr = err
	close(done)
	if err != nil {
		// A failed fetch leaves the previous key set in place: replacing a
		// working set with nothing would turn a Cloudflare blip into a total
		// sign-in outage.
		return err
	}
	v.keys = keys
	v.fetchedAt = time.Now()
	return nil
}

// errCFAccessCooling means the refresh was skipped by the cooldown, not that it
// failed. The caller treats the two the same way — it has no key either way —
// but keeping them distinct stops a skipped refresh reading as an outage.
var errCFAccessCooling = errors.New("cf access: jwks refresh is cooling down")

// jwksResponse is the JSON shape of Cloudflare's certs endpoint: a set of RSA
// public keys in JWK form.
type jwksResponse struct {
	Keys []struct {
		Kty string `json:"kty"`
		Kid string `json:"kid"`
		Use string `json:"use"`
		N   string `json:"n"` // base64url big-endian modulus
		E   string `json:"e"` // base64url big-endian exponent
	} `json:"keys"`
}

// fetchJWKS retrieves and parses the RSA keys at certsURL, keyed by kid.
func fetchJWKS(ctx context.Context, client *http.Client, certsURL string) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, certsURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cf access: fetch jwks: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cf access: fetch jwks: status %d", resp.StatusCode)
	}

	// Bounded read: a remote document parsed on a request path that must not be
	// able to exhaust memory, however unlikely the source is to misbehave.
	var jwks jwksResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJWKSBytes)).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("cf access: decode jwks: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		// Non-RSA and non-signing keys are skipped rather than rejected: the
		// endpoint is allowed to publish key types this verifier doesn't use.
		if k.Kty != "RSA" || k.Kid == "" || (k.Use != "" && k.Use != "sig") {
			continue
		}
		pub, err := rsaPublicKey(k.N, k.E)
		if err != nil {
			continue // skip a malformed key rather than fail the whole set
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		return nil, errors.New("cf access: jwks had no usable RSA keys")
	}
	return keys, nil
}

// rsaPublicKey rebuilds an RSA public key from the base64url modulus/exponent of
// a JWK.
func rsaPublicKey(nB64, eB64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, err
	}
	// The upper bound is not paranoia about RSA: without it, a >8-byte exponent
	// from a hostile or malformed key set slices eBuf with a negative index and
	// panics the process before copy is ever called. A real exponent is three
	// bytes.
	if len(nBytes) == 0 || len(eBytes) == 0 || len(eBytes) > 8 {
		return nil, errors.New("cf access: unusable RSA modulus or exponent")
	}
	// A short modulus is a broken key, not a small one. Cloudflare publishes
	// 2048-bit keys; refusing anything under that means a downgraded key set
	// can't quietly weaken verification.
	//
	// Measured on the integer, not on len(nBytes): the encoded length counts
	// leading zero bytes, so a 1024-bit modulus left-padded to 256 bytes would
	// clear a byte-length floor and still yield a 1024-bit key. (construct-
	// server's guard tests the encoding — SERV-131 carries the same fix.)
	n := new(big.Int).SetBytes(nBytes)
	if n.BitLen() < minRSAModulusBits {
		return nil, errors.New("cf access: RSA modulus is below the 2048-bit minimum")
	}
	// The exponent is a big-endian integer of up to a few bytes; left-pad to 8
	// so it fits a uint64.
	var eBuf [8]byte
	copy(eBuf[8-len(eBytes):], eBytes)
	e := binary.BigEndian.Uint64(eBuf[:])
	// E is an int: an exponent above MaxInt32 is implementation-defined
	// truncation on a 32-bit build and a nonsense key everywhere.
	if e == 0 || e > 1<<31-1 {
		return nil, errors.New("cf access: RSA exponent out of range")
	}
	return &rsa.PublicKey{N: n, E: int(e)}, nil
}

// decodeSegment base64url-decodes a JWS segment and unmarshals its JSON.
func decodeSegment(seg string, v any) error {
	raw, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}
