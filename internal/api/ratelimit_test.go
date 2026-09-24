package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestAnonymousReadRateLimited probes the per-IP guard on a public-read
// endpoint: requests under budget pass the requirePublicRead gate with 200,
// the first request over budget gets the structured rate_limited envelope
// (429, kind=app, empty message — no error text), other client IPs keep
// their own budget, and exempt paths (health, image proxy, non-API) plus
// non-read methods are never charged against it.
func TestAnonymousReadRateLimited(t *testing.T) {
	svc := unauthedService(t)
	svc.SetPublicRead(true)
	svc.ReloadPool([]string{"tok"})
	h := &APIHandler{svc: svc}

	lim := newRateLimiter(3, time.Minute)
	hnd := lim.middleware("/api/v1")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := h.requirePublicRead(r); err != nil {
			WriteError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, HealthStatus{Status: "ok"})
	}))

	const ip = "203.0.113.10"
	serve := func(method, path, remoteAddr string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		req.RemoteAddr = remoteAddr
		rec := httptest.NewRecorder()
		hnd.ServeHTTP(rec, req)
		return rec
	}

	// Under budget: the public-read gate answers 200.
	for i := range 3 {
		if rec := serve(http.MethodGet, "/api/v1/illusts/1", ip+":50000"); rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200 (body %s)", i+1, rec.Code, rec.Body.String())
		}
	}

	// Over budget: 429 with the structured envelope.
	rec := serve(http.MethodGet, "/api/v1/illusts/1", ip+":50000")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over-budget status = %d, want 429 (body %s)", rec.Code, rec.Body.String())
	}
	if ra := rec.Header().Get("Retry-After"); ra == "" {
		t.Error("Retry-After header missing on 429")
	}
	var body Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode 429 body: %v", err)
	}
	if body.Code != ErrorCodeRateLimited {
		t.Errorf("code = %q, want %q", body.Code, ErrorCodeRateLimited)
	}
	if body.Kind != ErrorKindApp {
		t.Errorf("kind = %q, want %q", body.Kind, ErrorKindApp)
	}
	if body.Message != "" {
		t.Errorf("message = %q, want empty (no raw error text)", body.Message)
	}

	// A different client IP has its own budget.
	if rec := serve(http.MethodGet, "/api/v1/illusts/1", "198.51.100.7:50000"); rec.Code != http.StatusOK {
		t.Errorf("other-IP status = %d, want 200", rec.Code)
	}

	// Exempt paths under the API base: health probes and the image proxy
	// (which carries its own bounded-cache protections) never spend the
	// read budget, even from the exhausted IP.
	for i := range 3 {
		if rec := serve(http.MethodGet, "/api/v1/health", ip+":50000"); rec.Code != http.StatusOK {
			t.Fatalf("health request %d: status = %d, want 200 (exempt)", i+1, rec.Code)
		}
	}
	if rec := serve(http.MethodGet, "/api/v1/proxy/img?src=x", ip+":50000"); rec.Code != http.StatusOK {
		t.Errorf("image-proxy status = %d, want 200 (exempt)", rec.Code)
	}
	// Paths outside the API base (SPA, docs) are not budgeted either.
	if rec := serve(http.MethodGet, "/login", ip+":50000"); rec.Code != http.StatusOK {
		t.Errorf("non-API status = %d, want 200 (exempt)", rec.Code)
	}
	// Mutations are not read-budgeted.
	if rec := serve(http.MethodPost, "/api/v1/illusts/1/bookmark", ip+":50000"); rec.Code != http.StatusOK {
		t.Errorf("POST status = %d, want 200 (read budget only)", rec.Code)
	}
}

// TestRateLimiter_CapHoldsUnderSpoofedIPFlood pins the M1 fix: the
// maxRateLimitEntries table cap must be enforced on every NEW key insert,
// not only on the over-budget rejection path. A flood rotating one in-budget
// request per fake IP (chi RealIP trusts X-Forwarded-For) never trips any
// single IP's budget, so insert-time enforcement is the only check that
// fires under exactly the attack the cap exists to absorb — without it the
// entries map grows without bound.
func TestRateLimiter_CapHoldsUnderSpoofedIPFlood(t *testing.T) {
	lim := newRateLimiter(300, time.Minute)
	now := time.Now()

	for i := range maxRateLimitEntries * 3 {
		ip := fmt.Sprintf("198.18.%d.%d", (i/256)%256, i%256)
		if ok, _ := lim.allow(ip, now); !ok {
			t.Fatalf("request %d from a fresh IP rejected, want accepted (in budget)", i)
		}
		if len(lim.entries) > maxRateLimitEntries {
			t.Fatalf("entries = %d after request %d, want <= %d (cap must run on new-key insert)",
				len(lim.entries), i, maxRateLimitEntries)
		}
	}
}
