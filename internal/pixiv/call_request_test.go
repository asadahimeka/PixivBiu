package pixiv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/txperl/pixivgo"

	"github.com/txperl/PixivBiu/internal/config"
)

// tokenResponse builds the {"response":{...}} envelope pixivgo.Auth expects.
func tokenResponse(access, refresh string) string {
	return `{"response":{"refresh_token":"` + refresh + `","access_token":"` + access + `","expires_in":3600}}`
}

// newPoolTestService points a fresh unauthenticated service at srv — API
// calls and /auth/token share the base URL — and installs the pooled refresh
// token the tests exercise.
func newPoolTestService(t *testing.T, srv *httptest.Server, poolTokens ...string) *Service {
	t.Helper()
	svc := newTestService(t, config.PixivConfig{})
	svc.client = pixivgo.NewClient(
		pixivgo.WithBaseURL(srv.URL),
		pixivgo.WithHTTPClient(srv.Client()),
	)
	svc.ReloadPool(poolTokens)
	return svc
}

// TestPoolSessionClient_ForceRetryAdoptsRefreshedCache pins M2's cache
// re-check on the forced post-401 path: once one force retry has
// re-exchanged the identity, a later retry carrying the SAME rejected access
// token must adopt the refreshed session from the cache instead of running
// another Auth. Parallel Auths against one rotated refresh are how Pixiv's
// rotation semantics hand the loser invalid_grant and wrongly evict a
// healthy pool token — the force path previously bypassed both the cache
// re-check and the singleflight (it went straight to exchangePoolToken).
func TestPoolSessionClient_ForceRetryAdoptsRefreshedCache(t *testing.T) {
	var authCount atomic.Int32
	srv := routeServer(t, map[string]http.HandlerFunc{
		"/auth/token": func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			authCount.Add(1)
			switch r.FormValue("refresh_token") {
			case "pool-rt":
				_, _ = w.Write([]byte(tokenResponse("at0", "rt2")))
			case "rt2":
				_, _ = w.Write([]byte(tokenResponse("at1", "rt3")))
			default:
				// A third Auth means the force path skipped the cache
				// re-check; answer the way rotation semantics would.
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			}
		},
	})
	svc := newPoolTestService(t, srv, "pool-rt")
	ctx := context.Background()

	// Normal path: cache miss → one exchange.
	_, acc, err := svc.poolSessionClient(ctx, "pool-rt", "")
	if err != nil || acc != "at0" {
		t.Fatalf("initial exchange: access = %q, err = %v (want at0, nil)", acc, err)
	}
	if got := authCount.Load(); got != 1 {
		t.Fatalf("authCount after initial exchange = %d, want 1", got)
	}

	// First forced retry: the cache still holds the rejected at0, so exactly
	// one re-exchange is required.
	_, acc, err = svc.poolSessionClient(ctx, "pool-rt", "at0")
	if err != nil || acc != "at1" {
		t.Fatalf("first force retry: access = %q, err = %v (want at1, nil)", acc, err)
	}
	if got := authCount.Load(); got != 2 {
		t.Fatalf("authCount after first force retry = %d, want 2", got)
	}

	// A second request that also 401'd on at0: the cache now serves at1 —
	// adopt it without touching /auth/token again.
	_, acc, err = svc.poolSessionClient(ctx, "pool-rt", "at0")
	if err != nil {
		t.Fatalf("second force retry: %v", err)
	}
	if acc != "at1" {
		t.Errorf("second force retry access = %q, want at1 (adopt the refreshed cache)", acc)
	}
	if got := authCount.Load(); got != 2 {
		t.Errorf("authCount = %d, want 2 (force path must re-check the cache, not Auth again)", got)
	}
}

// TestCallRefresh_Concurrent401SingleExchanges pins M2's singleflight on the
// forced-retry path through CallRefresh: two concurrent 401s on one pooled
// identity must collapse into exactly ONE re-exchange of the rotated refresh.
// Under Pixiv's rotation semantics a refresh token authorizes a single Auth —
// a second concurrent Auth of the same rotated token answers invalid_grant
// and would evict a healthy pool token from rotation.
//
// The API handler barriers both initial requests so both goroutines hold the
// rejected at0 client and enter the force path together; the auth handler
// enforces one-Auth-per-rotation and widens the success window so a missing
// singleflight deterministically races (mirrors TestCall_SingleFlightUnderConcurrent401).
func TestCallRefresh_Concurrent401SingleExchanges(t *testing.T) {
	var authCount, apiCount, arrived atomic.Int32
	var mu sync.Mutex
	used := map[string]bool{}
	release := make(chan struct{})

	srv := routeServer(t, map[string]http.HandlerFunc{
		"/auth/token": func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			rt := r.FormValue("refresh_token")
			authCount.Add(1)
			mu.Lock()
			alreadyUsed := used[rt]
			used[rt] = true
			mu.Unlock()
			if alreadyUsed {
				// Rotation semantics: each refresh token authorizes one Auth.
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			if rt == "pool-rt" {
				_, _ = w.Write([]byte(tokenResponse("at0", "rt2")))
				return
			}
			time.Sleep(50 * time.Millisecond) // widen the window a missing singleflight would race through
			_, _ = w.Write([]byte(tokenResponse("at1", "rt3")))
		},
		"/v1/illust/detail": func(w http.ResponseWriter, r *http.Request) {
			apiCount.Add(1)
			if r.Header.Get("Authorization") == "Bearer at1" {
				_, _ = w.Write([]byte(`{}`))
				return
			}
			if r.Header.Get("Authorization") == "Bearer at0" && arrived.Add(1) <= 2 {
				// Hold both initial 401s until each goroutine has one, then
				// release them into the force path together.
				if arrived.Load() == 2 {
					close(release)
				}
				<-release
			}
			w.WriteHeader(http.StatusUnauthorized)
		},
	})
	svc := newPoolTestService(t, srv, "pool-rt")

	const n = 2
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = CallRefresh(context.Background(), svc, "pool-rt", func(c *pixivgo.Client) (*pixivgo.IllustDetailResponse, error) {
				return c.IllustDetail(context.Background(), pixivgo.IllustDetailParams{IllustID: 1})
			})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
	if got := authCount.Load(); got != 2 {
		t.Errorf("authCount = %d, want 2 (initial exchange + exactly one forced re-exchange)", got)
	}
	if !svc.PoolHasTokens() {
		t.Error("pool drained — a healthy token was evicted by a racing re-exchange")
	}
	if got := apiCount.Load(); got != 2*n {
		t.Errorf("apiCount = %d, want %d (401 + replay per goroutine)", got, 2*n)
	}
}
