package api

import (
	"fmt"
	"net/http"
	"strings"
)

// Scopes guarding the integration surface. Core reader routes (/library,
// /upload, /sync, blob serving) are unauthenticated per existing policy; only
// the ecosystem hooks added in LYCM-400 require a token.
const (
	// ScopeEidolonRead authorizes the read-only Project Eidolon hooks
	// (/eidolon/*): current reading location and raw chapter text.
	ScopeEidolonRead = "eidolon:read"
	// ScopeDeliverySend authorizes triggering a "Send to Kindle" delivery.
	ScopeDeliverySend = "delivery:send"
)

// TokenAuth is a static, config-driven bearer-token table: it maps each token
// to the set of scopes it carries. It is built once at boot from
// LYCEUM_API_TOKENS (see ParseTokens) and is read-only thereafter, so it needs
// no locking.
type TokenAuth struct {
	tokens map[string]map[string]bool
}

// ParseTokens builds a TokenAuth from a config string of the form
//
//	token1=eidolon:read,token2=eidolon:read|delivery:send
//
// Entries are comma-separated; within an entry the token and its
// pipe-separated scope list are joined by '='. Whitespace around any field is
// trimmed. An empty spec yields an empty table (every protected route then
// 401s — the integration surface is closed until tokens are issued). It errors
// on a malformed entry (missing '=', empty token, or an unknown scope) so a
// typo in config fails loudly at boot rather than silently locking everyone
// out.
func ParseTokens(spec string) (*TokenAuth, error) {
	a := &TokenAuth{tokens: map[string]map[string]bool{}}
	for _, entry := range strings.Split(spec, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		token, scopeList, ok := strings.Cut(entry, "=")
		token = strings.TrimSpace(token)
		if !ok || token == "" {
			return nil, fmt.Errorf("api: malformed token entry %q (want token=scope|scope)", entry)
		}
		scopes := map[string]bool{}
		for _, s := range strings.Split(scopeList, "|") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if s != ScopeEidolonRead && s != ScopeDeliverySend {
				return nil, fmt.Errorf("api: unknown scope %q for token (valid: %s, %s)", s, ScopeEidolonRead, ScopeDeliverySend)
			}
			scopes[s] = true
		}
		if len(scopes) == 0 {
			return nil, fmt.Errorf("api: token %q has no scopes", token)
		}
		a.tokens[token] = scopes
	}
	return a, nil
}

// allows reports whether the given bearer token carries scope.
func (a *TokenAuth) allows(token, scope string) bool {
	if a == nil || token == "" {
		return false
	}
	return a.tokens[token][scope]
}

// bearerToken extracts the token from an "Authorization: Bearer <token>"
// header, returning "" when the header is absent or not a Bearer credential.
// The scheme match is case-insensitive per RFC 7235.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const scheme = "bearer "
	if len(h) < len(scheme) || !strings.EqualFold(h[:len(scheme)], scheme) {
		return ""
	}
	return strings.TrimSpace(h[len(scheme):])
}

// requireScope wraps next so it only runs for a request bearing a token with
// the required scope. A missing or unrecognized token is 401; a recognized
// token that lacks the scope is 403. When no token table is configured the
// route is closed (401) — the ecosystem hooks are opt-in via config.
func (a *API) requireScope(scope string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" || a.auth == nil || a.auth.tokens[token] == nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="lyceum"`)
			http.Error(w, "missing or invalid token", http.StatusUnauthorized)
			return
		}
		if !a.auth.allows(token, scope) {
			http.Error(w, "token lacks required scope "+scope, http.StatusForbidden)
			return
		}
		next(w, r)
	}
}
