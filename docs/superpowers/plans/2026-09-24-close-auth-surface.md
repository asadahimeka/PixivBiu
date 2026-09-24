# Close Auth Surface in Public Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** In public mode the site is purely anonymous: auth endpoints closed at runtime, login UI removed, no admin concept; local mode keeps login untouched.

**Architecture:** Runtime gating, not deletion. Same binary stays dual-use: `public_read_enabled=true` closes the auth surface (backend 404 + frontend hides login), `false` keeps the original single-operator login. One small spec addition (read-only `public_read` on AuthStatus, Task 3) with dual-client regen by the orchestrator; the closed-auth envelope itself reuses `not_found`.

**Tech Stack:** Go (chi handlers, `WriteError`/`classify`), React SPA (`useMessages`, `cn`, existing guest-notice patterns from Tasks 4–5).

**Spec:** N/A — user decision 2026-09-24 (close all login, purely anonymous, no admin). Prior plan: `docs/superpowers/plans/2026-09-23-public-site-conversion.md`.

## Global Constraints

- No hand-edits to `internal/api/server.gen.go` / `frontend/src/lib/api/schema.gen.ts`; only Task 3 changes the spec (one AuthStatus field) and the orchestrator regenerates both clients itself.
- Backend errors only via `WriteError`/`classify`; never expose `err.Error()`; single httplog event per request; slog English + `slog.Any("error", err)` for background.
- Preserve RequestID → RealIP → httplog → rate limiter → routes order; single Recoverer.
- Config: no `SchemaVersion` bump (no shape change), no `Manager.Config()` mutation; the gate reads the same live `public_read_enabled` reader Task 5 installed.
- No `git commit` / `git push`; leave diff uncommitted; no file deletions.
- Frontend: text via `useMessages()` static maps (4 locales if new keys); icon-only controls keep labels; no visual redesign beyond hiding login affordances.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/api/handler_auth.go` (modify) | Public-mode guard on login/logout/oauth/connectivity/detect; `GetAuthStatus` stays (read-only mode signal) |
| `internal/api/handler_auth_test.go` (create, if no auth test file exists — else append) | Disabled-probe + local-mode-unchanged tests |
| `frontend/src/app/.../root-layout.tsx` or router (modify) | `/login` route hidden in public mode (from `useAuth().public_read`, Task 3) |
| `frontend/src/features/auth/components/account-button.tsx` (modify) | Guest static display in public mode (revert fix-wave Sign-in entry) |
| `frontend/src/pages/me/me-redirect.tsx`, settings page, mutation buttons (modify) | Guest notice / hidden (no login target exists anymore) |
| bookmark renderers `illust-bookmark-button.tsx`, `illust-action-bar.tsx` (modify) | Hidden for guests in public mode (hook alone cannot hide DOM) |
| `api/openapi.yaml` + AuthStatus schema (modify, Task 3) | Read-only `public_read: bool` on the already-open `GET /auth/status` |
| `internal/api/handler_auth.go` `GetAuthStatus` (modify, Task 3) | Populate `public_read` from the live flag reader |
| `docs/CONFIGURATION.md`, `docs/ARCHITECTURE.md` (modify) | No-admin public model + closed auth surface |

---

### Task 1: Backend closes auth surface in public mode

**Files:**
- Modify: `internal/api/handler_auth.go`
- Test: `internal/api/handler_auth_public_test.go` (create; check existing test files first, append if one fits)

**Interfaces:**
- Consumes: live `public_read_enabled` reader from Task 5 (same accessor the `requirePublicRead` gate uses)
- Produces: closed auth ops return the existing `not_found` envelope (404, `kind=app`, empty message) when public mode is on

- [ ] **Step 1: Write the failing test**

```go
func TestAuthSurfaceClosedInPublicMode(t *testing.T) {
    h := newPublicModeHandler(t) // public_read_enabled=true, empty session
    req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"refresh_token":"x"}`))
    rec := httptest.NewRecorder()
    h.Login(rec, req)
    if rec.Code != http.StatusNotFound {
        t.Fatalf("got %d want 404", rec.Code)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestAuthSurfaceClosedInPublicMode -v`
Expected: FAIL (no guard yet — login attempts upstream exchange)

- [ ] **Step 3: Write minimal implementation**

Guard at the top of `Login`, `Logout`, `StartOAuth`, `ExchangeOAuth`, `CheckConnectivity`, `DetectProxies` (NOT `GetAuthStatus` — it stays as the read-only mode signal):

```go
// requireAuthSurface rejects user-login operations while the public-site
// mode is on: the public deployment has no operator session by design.
// Local mode (flag off) is unaffected.
func (h *APIHandler) requireAuthSurface(w http.ResponseWriter, r *http.Request) bool {
    if h.publicReadEnabled() {
        WriteError(w, r, ErrAuthSurfaceClosed) // sentinel → 404 not_found envelope
        return false
    }
    return true
}
```

`ErrAuthSurfaceClosed` maps in `classify`'s sentinel table to the existing `ErrorCodeNotFound` (404, `kind=app`, empty message) — no new wire code, no spec change. Document why reuse is correct (endpoint unavailable in this mode, not a missing resource — envelope is generic by design).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -v && go vet ./internal/api/`
Expected: PASS (new probe + all pre-existing cases, incl. local-mode login behavior)

- [ ] **Step 5: Verify diff**

Run: `git diff --check`
Expected: clean. Leave uncommitted.

---

### Task 2: Frontend removes login affordances in public mode

**Files:**
- Modify: login route registration (`/login` hidden in public mode), `/me` route (hidden in public mode), nav (`/downloads` entry + account area), home page (`RecentDownloads` block hidden), `account-button.tsx` (guest static display), settings page (guest notice), mutation buttons (`follow-button.tsx`, `use-illust-bookmark.ts`, `filter-panel.tsx` onSubmit, `illust-download-button.tsx`), bookmark renderers (`illust-bookmark-button.tsx`, `illust-action-bar.tsx`, hidden for guests), locale JSONs only if a new key is unavoidable
- Test: `bunx @biomejs/biome ci . && bun run build` (in `frontend/`)

**Interfaces:**
- Consumes: `useAuth().public_read` (Task 3; the open status endpoint already feeds `useAuth().status`)
- Produces: zero login entry points in public mode; local mode pixel-identical

- [ ] **Step 1: Write down the failing check**

```text
Public mode (`useAuth().public_read === true`, served by the open status endpoint):
- /login and /me routes are hidden (NotFound or home — never the form/redirect)
- AccountButton shows guest static display (no Sign in — fix-wave entry reverted)
- Home hides the RecentDownloads block; nav hides the downloads entry
  (/downloads keeps its reviewed guest notice for direct URLs only)
- settings shows the guest notice pattern (no /login target)
- bookmark/follow/batch-download/card-download controls are hidden for guests
  (not redirected — the redirect target no longer exists)
- Artist pages (/user/:id), ranking, search, artwork detail: unchanged
Local mode (flag off): every screen pixel-identical to today.
```

- [ ] **Step 2: Confirm baseline**

Run: `bunx @biomejs/biome ci .` (in `frontend/`)
Expected: PASS

- [ ] **Step 3: Implement (minimal, per file)**

Read the mode from the existing `useAuth()` status (`public_read`, Task 3; already fetched once per session by the auth provider — no new store, no extra request); thread a boolean where the provider does not reach. Prefer hiding over disabling; reuse `downloads_guest_notice`-style `Sheet` + `SheetEmpty` blocks and existing locale keys; add keys only for text that has no equivalent (4 locales, `en` authoritative, flat snake_case). Keep the viewer anonymous browser download (Task 4 path); only remove login-targeted redirects.

- [ ] **Step 4: Verify**

Run: `bunx @biomejs/biome ci . && bun run build` (in `frontend/`)
Expected: PASS. Manual acceptance (browser, by user): public mode shows no login affordance anywhere; local mode unchanged.

- [ ] **Step 5: Verify diff + docs**

Update `docs/CONFIGURATION.md` (auth-surface-closed row/notes) and `docs/ARCHITECTURE.md` (no-admin public model) in the same task. Run: `git diff --check`. Expected: clean. Leave uncommitted.

---

### Task 3: Public-mode signal in AuthStatus (backend, then orchestrator regen)

**Files:**
- Modify: `api/openapi.yaml` (AuthStatus schema + descriptions), `internal/api/handler_auth.go` (`GetAuthStatus` populates the field)
- Test: `internal/api/handler_auth_public_test.go` (append: flag on/off cases)
- Regen (orchestrator, not implementer): `make gen-backend` + `gen:api` via one-shot local server

**Interfaces:**
- Consumes: live `public_read_enabled` reader (same accessor as the gates)
- Produces: `GET /auth/status` → `{authenticated, public_read, ...}`; regenerated `server.gen.go` + `schema.gen.ts`

- [ ] **Step 1: Write the failing test**

```go
func TestAuthStatusReportsPublicRead(t *testing.T) {
    // public-mode handler → public_read=true; local-mode handler → false.
    // Unauthenticated in both (the endpoint stays open).
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestAuthStatusReportsPublicRead -v`
Expected: FAIL (field does not exist)

- [ ] **Step 3: Write minimal implementation**

Spec: `AuthStatus` gains read-only `public_read: boolean` ("true while the server runs in public-site mode; login endpoints are closed then"). Handler: populate from the live flag reader (same accessor as `requirePublicRead`/`requireAuthSurface` — the three can never disagree). No other endpoint changes.

- [ ] **Step 4: Run tests + hand regen to orchestrator**

Run: `go test ./internal/api/ -v && go vet ./internal/api/ && git diff --check`
Expected: PASS. Then STOP — do not run codegen; report "ready for regen" (orchestrator regenerates both clients itself per repo rule).

---

## Self-Review

1. **Spec coverage:** user decision mapped — backend closed (Task 1 ✅), signal (Task 3), UI removed (Task 2), docs (Task 2 step 5). Dual-use preserved via runtime flag (ledger ruling).
2. **Placeholder scan:** no TBD/TODO; every step has file, command, expected output.
3. **Type consistency:** live flag reader shared with Task 5 gate; `not_found` envelope already exists in `classify`; frontend reuses Task 4–5 guest patterns.

---

Plan complete and saved to `docs/superpowers/plans/2026-09-24-close-auth-surface.md`. Two execution options:

**1. Subagent-Driven (recommended)** - fresh subagent per task + review between tasks

**2. Single fixer** - one @fixer for both tasks (small, cohesive), single review at the end

**Which approach?**
