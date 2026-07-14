package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/magos/lyceum/internal/store"
)

// LYCM-801 user authentication.
//
// Two token namespaces live side by side and are never interchangeable:
//
//   - Service tokens (LYCEUM_API_TOKENS, see auth.go) are scoped credentials for
//     other *programs* — Project Eidolon and send-to-kindle. They are matched
//     against an in-memory table by requireScope.
//   - User tokens (this file) are credentials for *people*. They live in the
//     user_tokens table, are matched by SHA-256, and guard the reader core.
//
// A service token therefore cannot read someone's library, and a session token
// cannot drive a Kindle delivery. That separation is deliberate: the ecosystem
// hooks are a machine surface with a narrow blast radius, and folding them into
// the human account model would widen it.

// sessionCookie carries the session token for requests a client cannot put a
// header on.
//
// The bearer header alone is not enough: the shelf renders covers as plain
// <img src="/books/{id}/cover"> (web/src/views/LibraryView.vue), and a browser
// image request carries no Authorization header — gating the blob routes on the
// header alone would render every cover broken. The same applies to the Flutter
// WebView reader, which loads /reader/{id} from the server and then fetches
// same-origin from inside the page. A cookie rides both automatically.
//
// So a session may arrive either way: native clients (Flutter's http.Client, the
// Wails shell calling cross-origin) send the header; browsers get the cookie.
const sessionCookie = "lyceum_session"

// ctxKey is this package's private context-key type, so nothing outside can
// collide with (or forge) the authenticated user.
type ctxKey int

const userCtxKey ctxKey = iota

func withUser(ctx context.Context, u store.User) context.Context {
	return context.WithValue(ctx, userCtxKey, u)
}

// sessionToken pulls the caller's session credential from the Authorization
// header, falling back to the cookie. Header wins so that a native client can
// override a stale cookie.
func sessionToken(r *http.Request) string {
	if token := bearerToken(r); token != "" {
		return token
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value
	}
	return ""
}

// setSessionCookie issues the session cookie. HttpOnly keeps it away from page
// scripts (so an XSS in the reader can't exfiltrate it); Lax is enough because
// every mutating route is JSON-only and none is reachable by a cross-site form
// post. Secure is set only when the request already arrived over TLS, so a
// LAN/Tailscale install on plain HTTP still works.
func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

// userFrom returns the authenticated user for a request handled behind
// requireUser or requireOwner. Handlers reached any other way get the zero
// value, which has ID 0 and therefore matches no reading position.
func userFrom(ctx context.Context) store.User {
	u, _ := ctx.Value(userCtxKey).(store.User)
	return u
}

// owner returns the owner account, memoised for the lifetime of the process.
//
// With user auth off this is consulted on *every* request — including each of
// the dozens of cover blobs one shelf render pulls — so re-querying it each time
// would add a round-trip per request to the mode this ships in. The row is
// written only at boot (ReconcileOwner) and by PATCH /auth/me, which refreshes
// the cache with the row it gets back, so the memo cannot go stale.
func (a *API) owner(ctx context.Context) (store.User, error) {
	a.ownerMu.RLock()
	cached := a.ownerUser
	a.ownerMu.RUnlock()
	if cached.ID != 0 {
		return cached, nil
	}

	loaded, err := a.store.GetOwner(ctx)
	if err != nil {
		return store.User{}, err
	}
	a.cacheOwner(loaded)
	return loaded, nil
}

// cacheOwner memoises the owner row. It is called on load and again whenever the
// owner is renamed, so /auth/me never reports a stale display name.
func (a *API) cacheOwner(u store.User) {
	if !u.IsOwner {
		return
	}
	a.ownerMu.Lock()
	a.ownerUser = u
	a.ownerMu.Unlock()
}

// authenticate resolves the caller. With user auth off (the default while the
// clients still ship no credentials — see WithUserAuth), every request is
// treated as the owner, which reproduces the pre-accounts single-user behaviour
// exactly. With it on, a valid session token is required.
func (a *API) authenticate(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	if !a.userAuth {
		owner, err := a.owner(r.Context())
		if err != nil {
			serverError(w, "load owner", err)
			return store.User{}, false
		}
		return owner, true
	}

	u, err := a.store.UserByToken(r.Context(), sessionToken(r))
	if errors.Is(err, store.ErrNotFound) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="lyceum"`)
		http.Error(w, "missing or invalid session token", http.StatusUnauthorized)
		return store.User{}, false
	}
	if err != nil {
		serverError(w, "resolve session token", err)
		return store.User{}, false
	}
	return u, true
}

// inviteExpiry is the deadline stamped on a freshly minted invite.
func inviteExpiry() *time.Time {
	t := time.Now().Add(store.InviteTTL)
	return &t
}

// requireUser wraps next so it only runs for a request carrying a valid session
// token, with the resolved user available via userFrom.
func (a *API) requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := a.authenticate(w, r)
		if !ok {
			return
		}
		next(w, r.WithContext(withUser(r.Context(), u)))
	}
}

// requireOwner is requireUser plus an ownership check: a valid session belonging
// to a member gets 403. It guards the /admin routes — inviting and removing
// people is the owner's call alone.
//
// It also refuses outright while user auth is off. Administering a household on
// a server that has no notion of who is asking is not meaningful: with auth off
// every caller is served as the owner, so anyone who can reach the port could
// mint themselves an invite, redeem it for a durable session, and still hold it
// after the operator turns auth on — escalating straight through the step meant
// to close the door. Use `lyceum mint-token` on the host to bootstrap instead.
func (a *API) requireOwner(next http.HandlerFunc) http.HandlerFunc {
	return a.requireUser(func(w http.ResponseWriter, r *http.Request) {
		if !a.userAuth {
			http.Error(w, "household administration requires LYCEUM_AUTH; "+
				"use `lyceum mint-token` on the server to issue a sign-in invite",
				http.StatusForbidden)
			return
		}
		if !userFrom(r.Context()).IsOwner {
			http.Error(w, "owner only", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

// userJSON is the wire shape of an account. It deliberately carries no token
// material: a token is returned exactly once, by the call that mints it.
type userJSON struct {
	ID          int64  `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	IsOwner     bool   `json:"is_owner"`
}

func toUserJSON(u store.User) userJSON {
	return userJSON{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, IsOwner: u.IsOwner}
}

// handleAuthSession redeems a single-use invite for a long-lived session token
// bound to this device. This is how every client signs in.
//
// Body: {"token": "lyc_...", "device_label": "Pixel 8"}
// 200:  {"user": {...}, "session_token": "lyc_..."}
//
// The session token is shown here and never again; the client stores it and
// sends it as `Authorization: Bearer`. A spent, expired, or unknown invite is
// 401 — indistinguishable on purpose, so probing can't tell a used invite from a
// nonexistent one.
func (a *API) handleAuthSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token       string `json:"token"`
		DeviceLabel string `json:"device_label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	u, session, err := a.store.RedeemInvite(r.Context(), strings.TrimSpace(req.Token), req.DeviceLabel)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "invalid or already-used invite token", http.StatusUnauthorized)
		return
	}
	if err != nil {
		serverError(w, "redeem invite", err)
		return
	}

	// Also set the cookie: the SPA's covers (<img src>) and the Flutter WebView
	// reader cannot attach an Authorization header, so the token has to ride a
	// credential the browser sends on its own. Native clients ignore this and use
	// the returned token as a bearer.
	setSessionCookie(w, r, session)

	writeJSON(w, http.StatusOK, struct {
		User         userJSON `json:"user"`
		SessionToken string   `json:"session_token"`
	}{toUserJSON(u), session})
}

// handleAuthSignOut revokes the session token the request is carrying, so this
// device stops working while the user's other devices are untouched.
func (a *API) handleAuthSignOut(w http.ResponseWriter, r *http.Request) {
	if token := sessionToken(r); token != "" {
		if err := a.store.RevokeToken(r.Context(), token); err != nil {
			serverError(w, "revoke session", err)
			return
		}
	}
	clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// handleAuthMe returns the signed-in account.
func (a *API) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, toUserJSON(userFrom(r.Context())))
}

// handleAuthUpdateMe renames the signed-in account. This is where the LYCM-700
// local display name folds in: the name now lives on the server and follows the
// person across their devices, instead of being a per-browser localStorage label.
//
// Body: {"display_name": "Mara"}
func (a *API) handleAuthUpdateMe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(req.DisplayName)
	if name == "" {
		http.Error(w, "display_name is required", http.StatusBadRequest)
		return
	}

	u, err := a.store.UpdateDisplayName(r.Context(), userFrom(r.Context()).ID, name)
	if err != nil {
		serverError(w, "update display name", err)
		return
	}
	a.cacheOwner(u) // no-op unless u is the owner; keeps the memo fresh
	writeJSON(w, http.StatusOK, toUserJSON(u))
}

// handleAdminUserCreate adds a household member and returns their one-time
// invite token. Owner only.
//
// Body: {"email": "mara@example.com", "display_name": "Mara"}
// 201:  {"user": {...}, "invite_token": "lyc_..."}
//
// This is the hook Purser's `lyceum` connector calls (SERV-38), mirroring what
// its Switchyard connector already does: create the account with the email set,
// mint a starter credential, hand it back exactly once.
func (a *API) handleAdminUserCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Email) == "" {
		http.Error(w, "email is required", http.StatusBadRequest)
		return
	}

	u, err := a.store.CreateUser(r.Context(), req.Email, strings.TrimSpace(req.DisplayName))
	if errors.Is(err, store.ErrDuplicateEmail) {
		http.Error(w, "email is already registered", http.StatusConflict)
		return
	}
	if err != nil {
		serverError(w, "create user", err)
		return
	}

	invite, err := a.store.MintToken(r.Context(), u.ID, store.TokenInvite, "invite", inviteExpiry())
	if err != nil {
		serverError(w, "mint invite", err)
		return
	}

	writeJSON(w, http.StatusCreated, struct {
		User        userJSON `json:"user"`
		InviteToken string   `json:"invite_token"`
	}{toUserJSON(u), invite})
}

// memberJSON is a household-list row: the account plus enough to tell an active
// housemate from one who was invited and never showed up.
type memberJSON struct {
	userJSON
	// LastSeenAt is null when they have never signed in on any device — which is
	// what the list renders as "never signed in".
	LastSeenAt *time.Time `json:"last_seen_at"`
	// InviteExpiresAt is null unless an unredeemed invite is outstanding, which is
	// what makes the row show as "invite pending".
	InviteExpiresAt *time.Time `json:"invite_expires_at"`
	// SessionCount backs the row's "2 devices".
	SessionCount int `json:"session_count"`
}

// handleAdminUserList returns every account with its household metadata. Owner only.
func (a *API) handleAdminUserList(w http.ResponseWriter, r *http.Request) {
	members, err := a.store.ListMembers(r.Context())
	if err != nil {
		serverError(w, "list members", err)
		return
	}
	out := make([]memberJSON, 0, len(members))
	for _, m := range members {
		out = append(out, memberJSON{
			userJSON:        toUserJSON(m.User),
			LastSeenAt:      m.LastSeenAt,
			InviteExpiresAt: m.InviteExpiresAt,
			SessionCount:    m.SessionCount,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// sessionJSON is one signed-in device. It never carries token material — a
// session is revoked by row id, not by presenting the secret again.
type sessionJSON struct {
	ID         int64      `json:"id"`
	DeviceName string     `json:"device_label"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt *time.Time `json:"last_seen_at"`
	Current    bool       `json:"current"`
}

// handleSessionList returns the caller's own signed-in devices.
//
// This is the one real risk in a password-free model: a session does not expire,
// so a lost or lent device stays signed in forever unless its owner can see it
// and cut it off. Hence the list.
func (a *API) handleSessionList(w http.ResponseWriter, r *http.Request) {
	sessions, err := a.store.ListSessions(r.Context(), userFrom(r.Context()).ID, sessionToken(r))
	if err != nil {
		serverError(w, "list sessions", err)
		return
	}
	out := make([]sessionJSON, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, sessionJSON{
			ID: s.ID, DeviceName: s.Label, CreatedAt: s.CreatedAt,
			LastSeenAt: s.LastUsedAt, Current: s.Current,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSessionRevoke signs one of the caller's own devices out. The store scopes
// the delete to the caller, so a member cannot cut off someone else's device by
// guessing an id — it simply reports 404.
func (a *API) handleSessionRevoke(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return
	}
	switch err := a.store.RevokeSession(r.Context(), userFrom(r.Context()).ID, id); {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "session not found", http.StatusNotFound)
		return
	case err != nil:
		serverError(w, "revoke session", err)
		return
	}
	// Revoking the credential this very request rode in on is a sign-out; drop the
	// cookie too, or the browser keeps sending a token that no longer resolves.
	if sessionToken(r) != "" {
		if cur, err := a.store.ListSessions(r.Context(), userFrom(r.Context()).ID, sessionToken(r)); err == nil {
			var stillHere bool
			for _, s := range cur {
				stillHere = stillHere || s.Current
			}
			if !stillHere {
				clearSessionCookie(w, r)
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAdminUserInvite mints a fresh invite for an existing member — a second
// device, or a replacement for one they never redeemed. Owner only.
func (a *API) handleAdminUserInvite(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	u, err := a.store.GetUser(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if err != nil {
		serverError(w, "get user", err)
		return
	}

	invite, err := a.store.MintToken(r.Context(), u.ID, store.TokenInvite, "invite", inviteExpiry())
	if err != nil {
		serverError(w, "mint invite", err)
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		User        userJSON `json:"user"`
		InviteToken string   `json:"invite_token"`
	}{toUserJSON(u), invite})
}

// handleAdminUserDelete removes a member along with their tokens and reading
// positions (FK cascade). The owner cannot be removed. Owner only.
func (a *API) handleAdminUserDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	switch err := a.store.DeleteUser(r.Context(), id); {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "user not found", http.StatusNotFound)
		return
	case errors.Is(err, store.ErrOwnerImmutable):
		http.Error(w, "the owner account cannot be removed", http.StatusForbidden)
		return
	case err != nil:
		serverError(w, "delete user", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
