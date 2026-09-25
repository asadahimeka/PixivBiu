package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/txperl/PixivBiu/internal/auth"
)

// newPublicModeHandler builds an APIHandler in public mode: the live
// public_read_enabled switch is on and the session is empty — the anonymous
// public-site configuration. A real PKCE store is wired so that, before the
// guard exists, StartOAuth fails on its404 assertion instead of a
// nil-pointer panic; once the gate lands it short-circuits ahead of every
// dependency either way.
func newPublicModeHandler(t *testing.T) *APIHandler {
	t.Helper()
	return NewHandler(publicService(t), nil, nil, auth.NewStore(), nil, nil, nil, nil, nil, nil, nil, "")
}

// TestAuthSurfaceClosedInPublicMode is the TDD probe: with public mode on,
// POST /auth/login must answer the existing not_found envelope (404,
// kind=app, empty message) before any token exchange is attempted.
func TestAuthSurfaceClosedInPublicMode(t *testing.T) {
	h := newPublicModeHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"refresh_token":"x"}`))
	rec := httptest.NewRecorder()
	h.Login(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got %d want 404", rec.Code)
	}
	var env Error
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Code != ErrorCodeNotFound || env.Kind != ErrorKindApp || env.Message != "" {
		t.Errorf("envelope = {code:%s kind:%s message:%q}, want {not_found app \"\"}",
			env.Code, env.Kind, env.Message)
	}
}

// TestAuthSurfaceClosedInPublicMode_AllOps puts every user-login op behind
// the same gate. Nil deps (hub/dl/cfgMgr) are deliberate: reaching any of
// them would panic, so a404 here proves the gate fires first — the same
// nil-dependency trick TestSubmitDownload_AnonymousRejectedBeforeManager
// uses. GetAuthStatus is absent on purpose; it stays open as the read-only
// mode signal (asserted separately below).
func TestAuthSurfaceClosedInPublicMode_AllOps(t *testing.T) {
	h := newPublicModeHandler(t)
	ops := []struct {
		name string
		call func(http.ResponseWriter, *http.Request)
		body string
	}{
		{"Login", h.Login, `{"refresh_token":"x"}`},
		{"Logout", h.Logout, ""},
		{"StartOAuth", h.StartOAuth, ""},
		{"ExchangeOAuth", h.ExchangeOAuth, `{"code":"c","state":"s"}`},
		{"CheckConnectivity", h.CheckConnectivity, `{"proxy":"http://127.0.0.1:1"}`},
		{"DetectProxies", h.DetectProxies, ""},
	}
	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			op.call(rec, httptest.NewRequest(http.MethodPost, "/auth/op", strings.NewReader(op.body)))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("got %d want 404", rec.Code)
			}
		})
	}
}

// TestAuthSurfaceOpenInLocalMode pins the other half of the contract: with
// the flag off, the gate must not fire. Login falls through to its normal
// empty-refresh-token400 — no network round-trip involved.
func TestAuthSurfaceOpenInLocalMode(t *testing.T) {
	h := NewHandler(unauthedService(t), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "")
	rec := httptest.NewRecorder()
	h.Login(rec, httptest.NewRequest(http.MethodPost, "/auth/login", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d want 400", rec.Code)
	}
}

// TestGetAuthStatusOpenInPublicMode keeps GetAuthStatus out of the closed
// surface: it is the read-only mode signal the frontend polls, and it leaks
// nothing (an empty session serializes as authenticated=false).
func TestGetAuthStatusOpenInPublicMode(t *testing.T) {
	h := newPublicModeHandler(t)
	rec := httptest.NewRecorder()
	h.GetAuthStatus(rec, httptest.NewRequest(http.MethodGet, "/auth/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d want 200", rec.Code)
	}
}

// TestGetAuthStatusAnonymizesOperatorSession pins the identity-leak fix: in
// public mode the open status endpoint must not reflect a session loaded
// from state.json — neither as `authenticated` (it would flip the frontend's
// login-affordance gates for guests) nor as identity fields. The session on
// disk is untouched: flipping back to local mode restores it.
func TestGetAuthStatusAnonymizesOperatorSession(t *testing.T) {
	h := NewHandler(publicAuthedService(t), nil, nil, auth.NewStore(), nil, nil, nil, nil, nil, nil, nil, "")

	rec := httptest.NewRecorder()
	h.GetAuthStatus(rec, httptest.NewRequest(http.MethodGet, "/auth/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var st AuthStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if st.Authenticated {
		t.Error("Authenticated = true, want false in public mode despite an operator session")
	}
	if st.PublicRead == nil || !*st.PublicRead {
		t.Errorf("PublicRead = %v, want non-nil true", st.PublicRead)
	}
	if st.UserId != nil || st.UserName != nil || st.ExpiresAt != nil || st.SessionExpired != nil {
		t.Errorf("identity fields must be absent, got user_id=%v user_name=%v expires_at=%v session_expired=%v",
			st.UserId, st.UserName, st.ExpiresAt, st.SessionExpired)
	}
}

// TestAuthStatusReportsPublicRead pins the Task 3 signal: the open status
// endpoint reports the live public-site flag, so the frontend can hide login
// affordances without holding an authenticated session.
func TestAuthStatusReportsPublicRead(t *testing.T) {
	h := newPublicModeHandler(t)
	rec := httptest.NewRecorder()
	h.GetAuthStatus(rec, httptest.NewRequest(http.MethodGet, "/auth/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("public: got %d want 200", rec.Code)
	}
	var st AuthStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("public: decode status: %v", err)
	}
	if st.PublicRead == nil || !*st.PublicRead {
		t.Errorf("public: PublicRead = %v, want non-nil true", st.PublicRead)
	}

	local := NewHandler(unauthedService(t), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "")
	rec2 := httptest.NewRecorder()
	local.GetAuthStatus(rec2, httptest.NewRequest(http.MethodGet, "/auth/status", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("local: got %d want 200", rec2.Code)
	}
	var st2 AuthStatus
	if err := json.Unmarshal(rec2.Body.Bytes(), &st2); err != nil {
		t.Fatalf("local: decode status: %v", err)
	}
	if st2.PublicRead == nil || *st2.PublicRead {
		t.Errorf("local: PublicRead = %v, want non-nil false", st2.PublicRead)
	}
}
