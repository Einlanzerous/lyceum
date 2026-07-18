package api

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// Pairing-code sign-in is rate-limited per client IP (LYCM-88). A pairing code
// is low-entropy next to a 256-bit token, so this limit — together with the
// code's short TTL and single-use redemption — is what makes brute force
// infeasible. The token sign-in path is not limited; a 256-bit secret needs no
// help.
const (
	pairingRateWindow = time.Minute
	pairingRateBurst  = 10
)

type rateWindow struct {
	count   int
	resetAt time.Time
}

// ipRateLimiter is a small fixed-window per-key limiter: a lock, a map, and lazy
// pruning. Deliberately minimal — for Lyceum's scale the map holds a handful of
// entries, and a fixed window is plenty for a brute-force backstop.
type ipRateLimiter struct {
	mu     sync.Mutex
	window time.Duration
	burst  int
	hits   map[string]*rateWindow
	now    func() time.Time // injectable so tests don't sleep
}

func newIPRateLimiter(window time.Duration, burst int) *ipRateLimiter {
	return &ipRateLimiter{
		window: window,
		burst:  burst,
		hits:   make(map[string]*rateWindow),
		now:    time.Now,
	}
}

// allow records one attempt for key and reports whether it is within the limit.
func (l *ipRateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	w := l.hits[key]
	if w == nil || now.After(w.resetAt) {
		// Prune stale windows opportunistically so a spray of distinct source IPs
		// can't grow the map without bound.
		if len(l.hits) > 1024 {
			for k, v := range l.hits {
				if now.After(v.resetAt) {
					delete(l.hits, k)
				}
			}
		}
		l.hits[key] = &rateWindow{count: 1, resetAt: now.Add(l.window)}
		return true
	}
	if w.count >= l.burst {
		return false
	}
	w.count++
	return true
}

// clientIP is the address a request actually came from, for rate-limit keying.
// It reads RemoteAddr (the real transport peer), never X-Forwarded-For: that
// header is client-settable and would let an attacker rotate the key at will.
// Behind a reverse proxy every request shares the proxy's IP, degrading this to
// a global limit on the pairing path — a safe, fail-closed coarsening, not a gap.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
