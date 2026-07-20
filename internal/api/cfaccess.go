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

	if err := v.refresh(ctx); err != nil {
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

// refresh refetches the JWKS, subject to a cooldown once a key set is already
// held (so repeated unknown-kid tokens can't spin the certs endpoint).
func (v *CFAccessVerifier) refresh(ctx context.Context) error {
	v.mu.RLock()
	cooling := v.keys != nil && time.Since(v.lastFetch) < cfJWKSRefreshCooldown
	v.mu.RUnlock()
	if cooling {
		return nil
	}

	keys, err := fetchJWKS(ctx, v.httpClient, v.certsURL)

	v.mu.Lock()
	v.lastFetch = time.Now()
	if err == nil {
		v.keys = keys
		v.fetchedAt = time.Now()
	}
	v.mu.Unlock()
	return err
}

// jwksResponse is the JSON shape of Cloudflare's certs endpoint: a set of RSA
// public keys in JWK form.
type jwksResponse struct {
	Keys []struct {
		Kty string `json:"kty"`
		Kid string `json:"kid"`
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

	var jwks jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("cf access: decode jwks: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
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
	if len(nBytes) == 0 || len(eBytes) == 0 {
		return nil, errors.New("cf access: empty RSA modulus or exponent")
	}
	// The exponent is a big-endian integer of up to a few bytes; left-pad to 8
	// so it fits a uint64.
	var eBuf [8]byte
	copy(eBuf[8-len(eBytes):], eBytes)
	e := binary.BigEndian.Uint64(eBuf[:])
	if e == 0 {
		return nil, errors.New("cf access: zero RSA exponent")
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(e),
	}, nil
}

// decodeSegment base64url-decodes a JWS segment and unmarshals its JSON.
func decodeSegment(seg string, v any) error {
	raw, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}
