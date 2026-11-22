package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/magos/lyceum/internal/store"
)

// Scopes guarding the integration surface. Core reader routes (/library,
// /upload, /sync, blob serving) are unauthenticated per existing policy; only
// the ecosystem hooks added in LYCM-400 (and the admin:read count) require a token.
const (
	// ScopeEidolonRead authorizes the read-only Project Eidolon hooks
	// (/eidolon/*): current reading location and raw chapter text.
	ScopeEidolonRead = store.ScopeEidolonRead
	// ScopeDeliverySend authorizes triggering a "Send to Kindle" delivery.
	ScopeDeliverySend = store.ScopeDeliverySend
	// ScopeAdminRead authorizes GET /admin/users/active (a count, nothing else).
	ScopeAdminRead = store.ScopeAdminRead
)

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

// requireScope wraps next so it only runs for a request bearing a database
// service token (LYCM-140) that carries the required scope.
//
//   - No token, an unknown one and a revoked one are all 401 with one body, so
//     the response never says which. A presented unknown or revoked token is
//     audited, once per hash prefix per hour.
//   - A live token without the scope is 403.
//   - A database error is 503, never an allow.
//
// There is no cache: a cache would delay revocation, and revocation is the
// point. The table is read on every call; these routes carry little traffic.
func (a *API) requireScope(scope string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			refuseToken(w)
			return
		}
		check, err := a.store.CheckServiceToken(r.Context(), token, scope)
		switch {
		case errors.Is(err, store.ErrNotFound):
			a.auditRefused(r.Context(), token, "", "unknown")
			refuseToken(w)
			return
		case err != nil:
			log.Printf("auth: check service token: %v", err)
			http.Error(w, "authorization unavailable", http.StatusServiceUnavailable)
			return
		case check.Revoked:
			a.auditRefused(r.Context(), token, check.Label, "revoked")
			refuseToken(w)
			return
		case check.Scope != scope:
			http.Error(w, "token lacks required scope "+scope, http.StatusForbidden)
			return
		}
		// The label, never the token, is what identifies a consumer in logs.
		log.Printf("auth: service token %q (%s) %s %s", check.Label, scope, r.Method, r.URL.Path)
		next(w, r)
	}
}

func refuseToken(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="lyceum"`)
	http.Error(w, "missing or invalid token", http.StatusUnauthorized)
}

// auditRefused records a refused presentation. A failure to audit is logged and
// does not change the answer: the caller is refused either way.
func (a *API) auditRefused(ctx context.Context, token, label, reason string) {
	if err := a.store.AuditRefusedServiceToken(ctx, token, label, reason); err != nil {
		log.Printf("auth: audit refused service token: %v", err)
	}
}
