package pixiv

import (
	"context"
	"net/http"
	"time"

	"github.com/txperl/pixivgo"
)

// poolSession is one cached upstream identity for a pooled refresh token:
// the latest rotated refresh plus its access token and expiry. Keyed in
// Service.poolSess by the pool-issued refresh token so Evict/ReloadPool can
// find it even after rotation.
type poolSession struct {
	refresh   string
	access    string
	expiresAt time.Time
}

// CallRequest runs fn against the pixiv client selected for this request:
// it resolves the per-request refresh token through ReadRefreshToken (user
// token → operator session; public mode → pool; local mode → operator
// session) and dispatches via CallRefresh. No resolved token →
// ErrNotAuthenticated (401) — never a silent operator fallback.
func CallRequest[T any](ctx context.Context, s *Service, r *http.Request, fn func(*pixivgo.Client) (T, error)) (T, error) {
	refresh, ok := s.ReadRefreshToken(r)
	if !ok {
		var zero T
		return zero, ErrNotAuthenticated
	}
	return CallRefresh(ctx, s, refresh, fn)
}

// CallRefresh is CallRequest for callers that already resolved the token
// (the ranked-search fan-out resolves once so the whole window shares one
// identity instead of round-robin-skipping pool slots per page).
//
// The operator session's token routes through Call (self-healing refresh +
// pinned retry); anything else is a pooled identity and goes through the
// pool session cache: reuse while the access token has comfortable life
// left, single-flighted Auth on a private Clone otherwise (Auth mutates its
// receiver — never the shared client), and exactly one forced re-exchange
// retry when Pixiv still rejects the access token — the forced retry runs
// under the same singleflight with a cache re-check keyed on the rejected
// access token, so concurrent 401s on one identity collapse into a single
// Auth instead of double-consuming one rotated refresh token. A permanent
// rejection (invalid_grant) evicts the token from the pool and fails as
// ErrNotAuthenticated; transient failures surface as-is for classify.
func CallRefresh[T any](ctx context.Context, s *Service, refresh string, fn func(*pixivgo.Client) (T, error)) (T, error) {
	var zero T
	if refresh == "" {
		return zero, ErrNotAuthenticated
	}
	if s.Authenticated() && refresh == s.refreshToken() {
		return Call(ctx, s, fn)
	}

	client, access, err := s.poolSessionClient(ctx, refresh, "")
	if err != nil {
		return zero, s.poolSessionFailure(refresh, err)
	}
	v, err := fn(client)
	if err == nil || !isAuthError(err) {
		return v, err
	}
	if isInvalidGrant(err) {
		s.evictPoolSession(refresh)
		return zero, ErrNotAuthenticated
	}
	// The cached/re-exchanged access token was rejected (expired early or
	// revoked server-side) — force ONE fresh exchange and replay. Bounded:
	// the replay's result is returned as-is, no second forced round-trip.
	// The rejected access token travels into poolSessionClient so the
	// singleflight's cache re-check can adopt an exchange another concurrent
	// 401 already completed instead of Authing the same rotated refresh
	// twice — rotation's loser would see invalid_grant and wrongly evict a
	// healthy pool token.
	client, _, err = s.poolSessionClient(ctx, refresh, access)
	if err != nil {
		return zero, s.poolSessionFailure(refresh, err)
	}
	return fn(client)
}

// poolSessionFailure maps an exchange failure: a permanent refresh-token
// rejection evicts the pooled token (and any rotated successor) and surfaces
// as ErrNotAuthenticated; everything else (network, 5xx, transient 4xx)
// passes through untouched so classify can assign the right envelope.
func (s *Service) poolSessionFailure(refresh string, err error) error {
	if isInvalidGrant(err) {
		s.evictPoolSession(refresh)
		return ErrNotAuthenticated
	}
	return err
}

// poolSessionClient returns a client authenticated as the pooled identity
// behind refresh together with the access token it carries. rejectAccess ""
// is the normal path: reuse the warm cache, otherwise exchange under
// singleflight so a burst of anonymous reads costs one Auth. A non-empty
// rejectAccess is the bounded post-401 retry: the cache is trusted only
// while it holds a DIFFERENT access token — one another flight already
// re-exchanged — and a re-exchange, when still needed, runs under the SAME
// singleflight with that re-check inside the flight. Concurrent 401s on one
// identity therefore perform exactly one Auth of the rotated refresh; the
// previous force path bypassed both checks and could double-Auth, whose
// loser gets invalid_grant and evicts a healthy pool token (review M2).
func (s *Service) poolSessionClient(ctx context.Context, refresh, rejectAccess string) (*pixivgo.Client, string, error) {
	if c, acc, ok := s.cachedPoolClient(refresh); ok && acc != rejectAccess {
		return c, acc, nil
	}

	v, err, _ := s.poolSessG.Do(refresh, func() (any, error) {
		// Another flight may have populated or re-exchanged the cache while
		// this one queued — adopt its result instead of Authing again.
		if c, acc, ok := s.cachedPoolClient(refresh); ok && acc != rejectAccess {
			return poolClientResult{client: c, access: acc}, nil
		}
		c, acc, err := s.exchangePoolToken(ctx, refresh, refresh)
		if err != nil {
			return nil, err
		}
		return poolClientResult{client: c, access: acc}, nil
	})
	if err != nil {
		return nil, "", err
	}
	res := v.(poolClientResult)
	return res.client, res.access, nil
}

// poolClientResult carries a pooled client and the access token it was built
// with through singleflight's any-typed result channel.
type poolClientResult struct {
	client *pixivgo.Client
	access string
}

// cachedPoolClient builds a client from the cached session when its access
// token still has comfortable life left (mirrors the operator loop's lead
// time, so a session is re-exchanged before Pixiv starts rejecting it) and
// reports that access token, so the post-401 force path can tell whether the
// cache still holds the very token Pixiv just rejected.
func (s *Service) cachedPoolClient(refresh string) (*pixivgo.Client, string, bool) {
	s.poolSessMu.Lock()
	sess, ok := s.poolSess[refresh]
	s.poolSessMu.Unlock()
	if !ok || time.Until(sess.expiresAt) <= refreshLeadTime {
		return nil, "", false
	}
	c := s.Client().Clone()
	c.SetAuth(sess.access, sess.refresh)
	return c, sess.access, true
}

// exchangePoolToken authenticates a throwaway Clone (Auth mutates its
// receiver; the shared client must never see a pool token) and stores the
// resulting session under the pool-issued key, returning the client with the
// access token it carries. The stored refresh prefers an already-rotated
// successor so rotation isn't reset back to a possibly stale token; expiry
// carries the same .Round(0) wall-clock semantics as the operator path.
func (s *Service) exchangePoolToken(ctx context.Context, poolRefresh, sessRefresh string) (*pixivgo.Client, string, error) {
	rt := sessRefresh
	s.poolSessMu.Lock()
	if sess, ok := s.poolSess[poolRefresh]; ok && sess.refresh != "" {
		rt = sess.refresh
	}
	s.poolSessMu.Unlock()

	xctx, cancel := context.WithTimeout(ctx, authTimeout)
	defer cancel()

	c := s.Client().Clone()
	resp, err := c.Auth(xctx, rt)
	if err != nil {
		return nil, "", err
	}

	now := time.Now()
	s.poolSessMu.Lock()
	if s.poolSess == nil {
		s.poolSess = make(map[string]poolSession)
	}
	s.poolSess[poolRefresh] = poolSession{
		refresh:   resp.RefreshToken,
		access:    resp.AccessToken,
		expiresAt: now.Add(time.Duration(resp.ExpiresIn) * time.Second).Round(0),
	}
	s.prunePoolSessionsLocked(now)
	s.poolSessMu.Unlock()
	return c, resp.AccessToken, nil
}

// maxPoolSessions bounds the session cache under IP-churn-free conditions —
// one entry per pool token ever seen. When a pathological key set exceeds
// it, expired entries are dropped first; a still-oversized cache is cleared
// wholesale (fail-open: the next read simply re-exchanges).
const maxPoolSessions = 64

func (s *Service) prunePoolSessionsLocked(now time.Time) {
	if len(s.poolSess) <= maxPoolSessions {
		return
	}
	for k, v := range s.poolSess {
		if now.After(v.expiresAt) {
			delete(s.poolSess, k)
		}
	}
	if len(s.poolSess) > maxPoolSessions {
		s.poolSess = make(map[string]poolSession)
	}
}

// evictPoolSession drops the cached session for a rejected refresh token and
// removes both the pool-issued token and any rotated successor from the
// issuing pool, so the rotation stops handing out an identity Pixiv has
// permanently rejected. Idempotent; safe when the pool already swapped.
func (s *Service) evictPoolSession(refresh string) {
	s.poolSessMu.Lock()
	rotated := ""
	if sess, ok := s.poolSess[refresh]; ok {
		rotated = sess.refresh
		delete(s.poolSess, refresh)
	}
	s.poolSessMu.Unlock()

	s.mu.RLock()
	p := s.pool
	s.mu.RUnlock()
	if p == nil {
		return
	}
	p.Evict(refresh)
	if rotated != "" && rotated != refresh {
		p.Evict(rotated)
	}
}
