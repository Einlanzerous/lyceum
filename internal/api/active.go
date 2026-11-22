package api

import (
	"log"
	"net/http"
	"time"
)

// activeWindow is the definition of "active" for GET /admin/users/active. It is
// a constant and not a parameter on purpose: the route is a count for a
// dashboard, and a caller-chosen window would let it be narrowed into a probe of
// who was online at a given moment.
const activeWindow = 15 * time.Minute

// activeUsersJSON is the whole response. It is an allow-list struct and must not
// embed userJSON or memberJSON: those carry emails, names and per-user
// timestamps, and a field added to either would otherwise leak here silently.
type activeUsersJSON struct {
	Active        int       `json:"active"`
	WindowSeconds int       `json:"window_seconds"`
	AsOf          time.Time `json:"asOf"`
}

// handleActiveUsers reports how many accounts used a session in the last 900 s,
// by the database clock (LYCM-140, for Satlink).
//
// While user auth is off nothing writes last_used_at, so the count is unknown
// rather than zero: it answers 503 with a reason, never {active: 0}. That is
// 503 and not requireOwner's 403 because the caller's credential is fine; it is
// the answer that does not exist.
func (a *API) handleActiveUsers(w http.ResponseWriter, r *http.Request) {
	if !a.userAuth {
		http.Error(w, "active-user count unavailable: LYCEUM_AUTH is off, so sessions are not tracked",
			http.StatusServiceUnavailable)
		return
	}
	n, asOf, err := a.store.CountActiveUsers(r.Context(), activeWindow)
	if err != nil {
		// 503, not serverError's 500: the count is unavailable, which is what the
		// contract tells Satlink to expect, and the same answer requireScope gives
		// when the database cannot be reached. The body names no error detail.
		log.Printf("api: count active users: %v", err)
		http.Error(w, "active-user count unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, activeUsersJSON{
		Active:        n,
		WindowSeconds: int(activeWindow / time.Second),
		AsOf:          asOf.UTC(),
	})
}
