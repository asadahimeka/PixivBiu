package api

import (
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrRateLimited is the sentinel for a local per-IP read-budget rejection.
// classify maps it to 429 / rate_limited / kind=app with an empty message,
// so the frontend localizes from error_rate_limited — no raw text, no
// upstream involvement (this is OUR guard, not Pixiv's).
var ErrRateLimited = errors.New("api: rate limited")

const (
	// defaultReadLimitPerIP / defaultReadWindow size the fixed window for
	// anonymous read budgeting: generous for interactive paging (a grid page
	// costs a handful of API reads; the image proxy and health probes are
	// exempt), tight enough to stop a script from draining the token pool.
	// Not configurable — repo config boundary keeps static defaults here.
	defaultReadLimitPerIP = 300
	defaultReadWindow     = time.Minute
	// maxRateLimitEntries bounds the per-IP table under spoofed-XFF churn
	// (chi's RealIP trusts X-Forwarded-For, so a direct-exposure deployment
	// lets an attacker rotate fake client IPs). The cap is enforced on every
	// NEW key insert — not only on the over-budget rejection path, because a
	// flood rotating one in-budget request per fake IP never trips a per-IP
	// budget. Expired entries are swept first; a still-oversized table is
	// cleared wholesale — fail-open by design, because a forged-IP flood must
	// not become a lockout of real clients. Deployments should terminate TLS
	// behind a reverse proxy that sets XFF itself.
	maxRateLimitEntries = 4096
)

type rateLimitEntry struct {
	count   int
	resetAt time.Time
}

// rateLimiter is a fixed-window per-key (client IP) counter. Construction
// injects limit/window so tests can exhaust a budget in a few requests.
type rateLimiter struct {
	limit   int
	window  time.Duration
	mu      sync.Mutex
	entries map[string]rateLimitEntry
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		limit:   limit,
		window:  window,
		entries: make(map[string]rateLimitEntry),
	}
}

// allow consumes one unit for ip. Within budget it returns ok=true; over
// budget ok=false with the wait until the window resets (for Retry-After).
func (l *rateLimiter) allow(ip string, now time.Time) (ok bool, wait time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	_, existed := l.entries[ip]
	e := l.entries[ip]
	if e.resetAt.IsZero() || !now.Before(e.resetAt) {
		e = rateLimitEntry{count: 0, resetAt: now.Add(l.window)}
	}
	e.count++
	l.entries[ip] = e

	// Enforce the table cap whenever an insert grows it past the limit —
	// insert-time, not reject-time: a spoofed-XFF flood rotating one
	// in-budget request per fake IP never reaches the over-budget path, and
	// inserts are the only way the table grows.
	if !existed && len(l.entries) > maxRateLimitEntries {
		l.sweepLocked(now)
	}

	if e.count <= l.limit {
		return true, 0
	}
	return false, time.Until(e.resetAt)
}

// sweepLocked drops expired entries and, if the table is still over the cap,
// clears it wholesale (fail-open). Caller must hold l.mu.
func (l *rateLimiter) sweepLocked(now time.Time) {
	for k, v := range l.entries {
		if !now.Before(v.resetAt) {
			delete(l.entries, k)
		}
	}
	if len(l.entries) > maxRateLimitEntries {
		clear(l.entries)
	}
}

// ReadRateLimit returns the per-IP read-budget middleware for the API base.
// It charges GET/HEAD requests under base (minus health and the image proxy,
// which carry their own protections) against a fixed window per client IP
// and answers overflow with the structured 429 envelope via WriteError.
// Registered after httplog (so overflow still lands in the request log with
// error.type=rate_limited) and before the generated routes. The client IP
// is read from the already-rewritten RemoteAddr, i.e. chi RealIP's answer —
// no second XFF parse here.
func ReadRateLimit(base string) func(http.Handler) http.Handler {
	return newRateLimiter(defaultReadLimitPerIP, defaultReadWindow).middleware(base)
}

func (l *rateLimiter) middleware(base string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if (r.Method == http.MethodGet || r.Method == http.MethodHead) && budgetedPath(base, r.URL.Path) {
				if ok, wait := l.allow(clientIP(r), time.Now()); !ok {
					seconds := int(wait.Seconds() + 0.999)
					w.Header().Set("Retry-After", strconv.Itoa(max(1, seconds)))
					WriteError(w, r, ErrRateLimited)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// budgetedPath reports whether path counts against the read budget: API reads
// under base, excluding GET /health (probes fire continuously) and the image
// proxy (bounded downloads + its own cache guard upstream). Everything
// outside the API base — SPA assets, /docs — is never budgeted.
func budgetedPath(base, path string) bool {
	if path != base && !strings.HasPrefix(path, base+"/") {
		return false
	}
	if path == base+"/health" || strings.HasPrefix(path, base+"/proxy/") {
		return false
	}
	return true
}

// clientIP strips the port from RemoteAddr (already XFF-rewritten by chi's
// RealIP upstream); an address without a port is used verbatim.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
