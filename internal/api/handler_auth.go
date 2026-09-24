package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/txperl/PixivBiu/internal/pixiv"
	"github.com/txperl/PixivBiu/internal/state"
	"github.com/txperl/PixivBiu/internal/sysproxy"
)

// ErrAuthSurfaceClosed signals that a user-login operation was attempted
// while the public-site mode is on: the public deployment is purely
// anonymous by design, so there is no operator session to create or manage.
// It maps through classify's sentinel table to the existing
// ErrorCodeNotFound (404, kind=app, empty message) — reusing that envelope is
// correct because this is "the endpoint is unavailable in this mode", and
// the not_found envelope is generic by design: the client localizes by code
// and never needs a bespoke wire code (no spec change).
var ErrAuthSurfaceClosed = errors.New("auth surface closed in public mode")

// Register ErrAuthSurfaceClosed in classify's sentinel table. This lives in
// an init here (rather than beside the other entries in handler.go) because
// this change's file scope is limited to handler_auth.go; init runs after
// all package-level vars, so sentinelErrors is already populated.
func init() {
	sentinelErrors = append(sentinelErrors, struct {
		err    error
		code   ErrorCode
		status int
	}{ErrAuthSurfaceClosed, ErrorCodeNotFound, http.StatusNotFound})
}

// publicReadEnabled reads the live public_read_enabled flag off the Service —
// the same accessor requirePublicRead uses, so this gate and the read gate can
// never disagree about the mode. Nil-safe: a bare handler reads as local mode.
func (h *APIHandler) publicReadEnabled() bool {
	return h.svc != nil && h.svc.PublicReadEnabled()
}

// requireAuthSurface rejects user-login operations while the public-site
// mode is on: the public deployment has no operator session by design, so
// login/logout/OAuth/connectivity onboarding are all closed. Local mode
// (flag off) is unaffected — the same binary keeps the original single-user
// login. GetAuthStatus deliberately skips this gate: it stays open as the
// read-only mode signal. Errors flow only through WriteError/classify.
func (h *APIHandler) requireAuthSurface(w http.ResponseWriter, r *http.Request) bool {
	if h.publicReadEnabled() {
		WriteError(w, r, ErrAuthSurfaceClosed) // sentinel → 404 not_found envelope
		return false
	}
	return true
}

func (h *APIHandler) Login(w http.ResponseWriter, r *http.Request) {
	if !h.requireAuthSurface(w, r) {
		return
	}
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		WriteError(w, r, err)
		return
	}
	if req.RefreshToken == "" {
		WriteError(w, r, pixiv.ErrNoRefreshToken)
		return
	}
	tok, err := h.svc.Login(r.Context(), req.RefreshToken)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, makeAuthStatus(tok, false, h.publicReadEnabled()))
}

func (h *APIHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if !h.requireAuthSurface(w, r) {
		return
	}
	if err := h.svc.Logout(); err != nil {
		WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *APIHandler) GetAuthStatus(w http.ResponseWriter, r *http.Request) {
	tok, sessionExpired := h.svc.AuthSnapshot()
	writeJSON(w, http.StatusOK, makeAuthStatus(tok, sessionExpired, h.publicReadEnabled()))
}

// StartOAuth issues a fresh PKCE state + verifier and hands the client back
// the hosted Pixiv login URL. The verifier stays server-side; the client only
// needs `state` to call ExchangeOAuth.
func (h *APIHandler) StartOAuth(w http.ResponseWriter, r *http.Request) {
	if !h.requireAuthSurface(w, r) {
		return
	}
	state, _, challenge, err := h.pkce.Issue()
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, OAuthStartResponse{
		State:    state,
		LoginUrl: pixiv.BuildLoginURL(challenge),
	})
}

// ExchangeOAuth completes the OAuth flow: it pulls the verifier the server
// stored under `state`, asks Pixiv to trade the authorisation code for tokens,
// and persists the result through the same path as plain refresh-token login.
// `code` may be either the bare code or the full callback URL the user
// pasted from the popup's address bar.
func (h *APIHandler) ExchangeOAuth(w http.ResponseWriter, r *http.Request) {
	if !h.requireAuthSurface(w, r) {
		return
	}
	var req OAuthExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		WriteError(w, r, err)
		return
	}
	code := extractAuthCode(req.Code)
	if code == "" {
		WriteError(w, r, pixiv.ErrNoAuthCode)
		return
	}
	verifier, err := h.pkce.Consume(req.State)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	tok, err := h.svc.LoginWithAuthCode(r.Context(), code, verifier)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, makeAuthStatus(tok, false, h.publicReadEnabled()))
}

// CheckConnectivity probes whether Pixiv is reachable over the backend's
// current network path, optionally testing a candidate proxy. It is
// deliberately unauthenticated: the login onboarding calls it before sign-in so
// a user on a restricted network can discover — and fix, via a proxy — a dead
// connection before wrestling with the OAuth popup.
//
// reachable=false is a normal 200 result (the answer to the question), not an
// error. When a supplied proxy works AND there's no Pixiv session yet, the
// proxy is persisted so the imminent OAuth login uses it. That write is bounded
// to the pre-login window: once authenticated, proxy changes belong on the
// (auth-gated) Settings page, so we skip persisting here.
func (h *APIHandler) CheckConnectivity(w http.ResponseWriter, r *http.Request) {
	if !h.requireAuthSurface(w, r) {
		return
	}
	var req CheckConnectivityJSONRequestBody
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, r, err)
		return
	}

	var override *string
	if req.Proxy != nil {
		p := strings.TrimSpace(*req.Proxy)
		override = &p
	}

	reachable, latency, err := h.svc.ProbeReachable(r.Context(), override)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	if reachable && override != nil {
		if err := h.persistOnboardingProxy(*override); err != nil {
			WriteError(w, r, err)
			return
		}
	}

	ms := int(latency.Milliseconds())
	writeJSON(w, http.StatusOK, ConnectivityStatus{Reachable: reachable, LatencyMs: &ms})
}

// detectProxyTimeout bounds the OS proxy lookup so a wedged `scutil` subprocess
// (macOS) can't hang the request. Detection is local and normally returns in
// milliseconds; the rest of the budget is slack.
const detectProxyTimeout = 3 * time.Second

// DetectProxies reports the proxy candidates discovered from the operating
// system — the GUI system proxy (macOS scutil / Windows registry) plus the
// HTTP(S)_PROXY / ALL_PROXY env vars — ordered system-first. Login onboarding
// calls it after a direct connectivity probe fails, so it can offer and
// auto-test a system proxy the user enabled without TUN mode (which env vars
// never expose) instead of making them type it. This reads local config only:
// no network I/O and nothing persisted — testing/persisting a candidate is the
// job of CheckConnectivity. Unauthenticated, matching CheckConnectivity, since
// it's part of the pre-login flow.
func (h *APIHandler) DetectProxies(w http.ResponseWriter, r *http.Request) {
	if !h.requireAuthSurface(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), detectProxyTimeout)
	defer cancel()

	found := sysproxy.Detect(ctx)
	candidates := make([]ProxyCandidate, 0, len(found))
	for _, c := range found {
		candidates = append(candidates, ProxyCandidate{
			Url:    c.URL,
			Source: ProxyCandidateSource(c.Source),
		})
	}
	writeJSON(w, http.StatusOK, DetectedProxies{Candidates: candidates})
}

// persistOnboardingProxy records a proxy the connectivity probe just proved
// works, so the imminent OAuth login uses it. It is a deliberate no-op once a
// Pixiv session exists: after login, proxy changes go through the auth-gated
// Settings page (PATCH /config), not this open endpoint. The write hot-reloads
// the live client via the config manager's OnReload hooks (cmd/server/main.go)
// before the client proceeds to OAuth.
func (h *APIHandler) persistOnboardingProxy(proxy string) error {
	if h.svc.Authenticated() {
		return nil
	}
	_, err := h.cfgMgr.Patch(map[string]any{"pixiv": map[string]any{"proxy": proxy}})
	return err
}

// extractAuthCode tolerates the user pasting either the full Pixiv callback
// URL or just the bare code. Anything that looks like a URL is parsed first;
// otherwise we trust the trimmed input.
func extractAuthCode(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "pixiv://") {
		if u, err := url.Parse(s); err == nil {
			if c := u.Query().Get("code"); c != "" {
				return c
			}
		}
	}
	return s
}

// makeAuthStatus projects an internal state.Token (+ the session-expired flag)
// onto the generated AuthStatus model. The refresh token is the session — a
// blank refresh token means logged out, and sessionExpired then distinguishes a
// revoked session from a never-logged-in first run. An expired access token
// still reads as authenticated: the service renews it transparently (background
// loop + on-401 self-heal); only a permanent refresh rejection (invalid_grant)
// clears the refresh token.
func makeAuthStatus(tok state.Token, sessionExpired bool, publicRead bool) AuthStatus {
	if tok.RefreshToken == "" {
		s := AuthStatus{Authenticated: false, PublicRead: &publicRead}
		if sessionExpired {
			v := true
			s.SessionExpired = &v
		}
		return s
	}
	s := AuthStatus{Authenticated: true, PublicRead: &publicRead}
	if tok.UserID != 0 {
		v := tok.UserID
		s.UserId = &v
	}
	if tok.UserName != "" {
		v := tok.UserName
		s.UserName = &v
	}
	if !tok.AccessTokenExpiresAt.IsZero() {
		v := tok.AccessTokenExpiresAt
		s.ExpiresAt = &v
	}
	return s
}
