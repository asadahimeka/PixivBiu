# Public Site Conversion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Convert PixivBiu from single-user local app to overseas public site: anonymous read via Go-backend preset refresh_token pool, images/downloads via public pximg proxy fetched directly by the browser.

**Architecture:** Keep pixivgo wire models (no Hibi/Pxve, no field-mapping drift). Backend `pixiv.Service` gains a read-only service-token pool for anonymous requests plus the existing per-operator user session for writes; frontend `rewritePximgUrl` gains a configurable multi-proxy list with same-origin backend fallback, and downloads move to browser blob download so the server never streams bytes for anonymous users.

**Tech Stack:** Go (chi, httplog, koanf, pixivgo, singleflight), React SPA (TanStack Query, openapi-fetch, PximgImage), public pximg proxies (e.g. `i.pixiv.re`), overseas VPS/Docker.

**Spec:** N/A — user decisions from 2026-09-23 review: no Hibi/Pxve (API drift), Go preset pool tokens, browser-default download + enhancement, overseas public deploy. No R18 decision yet — plan defaults to showing what upstream returns and flags the open question in Task 5.

## Global Constraints

- Spec-first: edit `api/openapi.yaml` (+ domain path files) before any endpoint change; keep operation IDs unique camelCase; shared schemas in root spec.
- Never hand-edit `internal/api/server.gen.go` or `frontend/src/lib/api/schema.gen.ts` — regenerate both together with spec changes.
- Backend handlers live on `APIHandler` (`internal/api/handler.go:25`); use `writeJSON`/`WriteError`; never expose `err.Error()` or upstream bodies; request errors flow via `WriteError`/`httplog` only, no second per-request log; background logs use slog English + `slog.Any("error", err)`.
- Preserve RequestID → RealIP → httplog ordering and single Recoverer (`cmd/server/serve.go`); new middleware goes after httplog.
- Config: `koanf` tags + `cfg` metadata + `baseDefaults` in `internal/config/config.go`; snake_case segments stay; `Manager.Config()` is boot snapshot, never mutate or live-read; hot settings need live consumer or reload hook in `cmd/server/reload.go` under Manager lock (no block, no re-enter `Patch`/`Reset`); keep masking, hidden reset exceptions, diff-only persistence; bump `SchemaVersion` only for incompatible shape changes (this plan adds optional keys — no bump).
- Tokens in state store, never settings/env logging; refresh rejection expires session, transient failures do not; close SSE before HTTP drain; streaming handlers finish on cancel.
- No `git commit` / `git push` in any task (repo policy — user has not requested commits). Each task ends with `git diff --check` and an uncommitted diff.
- Frontend: route shells only default-export (`pages/<route>/index.tsx`); API via feature adapters + query-options factories + `unwrap`; text via `useMessages()` static maps; classes via `cn(...)`; icons need accessible labels; images via `PximgImage` (+ `fit` prop, `className` styles wrapper); scroll tools target `[data-app-scroller]`; never hand Node/Electron to renderer.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/pixiv/pool.go` (create) | Service-token pool: rotation, refresh-rejection eviction, snapshot for handlers |
| `internal/pixiv/service.go` (modify) | Keep single-user session for writes; add pool-backed read client selection |
| `internal/config/config.go` (modify) | Add optional `pixiv.service_refresh_tokens` (+ `public_read_enabled`), defaults, metadata |
| `cmd/server/app.go` (modify) | Register validator for new keys |
| `cmd/server/reload.go` (modify) | Reload hook for pool/proxy-affecting keys (non-blocking, no re-enter) |
| `internal/api/handler.go` (modify) | Split `requireAuth` (`handler.go:241-246`) into read vs write gates |
| `internal/api/handler_illusts.go`, `handler_users.go`, `handler_search.go`, `handler_events.go` (modify) | Anonymous-readable handlers use pool; writes keep user gate |
| `internal/api/handler_downloads.go` (modify) | Anonymous `POST` jobs rejected; reads admin/self-use only |
| `api/openapi.yaml` + domain paths (modify as needed) | Document which ops are anonymous-readable vs auth-gated |
| `frontend/src/lib/pixiv-image.ts` (modify) | Multi-proxy rewrite list, fallback order, same-origin fallback |
| `frontend/src/components/pximg-image.tsx` (modify) | Multi-source error fallback (`onError` next-source), keep `fit` semantics |
| `frontend/src/features/downloads/browser-download.ts` (create) | `fetch(proxyUrl) -> blob -> a[download]` + fallback to `window.open` |
| `frontend/src/pages/home/index.tsx`, `pages/ranking/index.tsx`, `pages/search/index.tsx`, `pages/user/index.tsx`, `pages/downloads/index.tsx`, `pages/me/me-redirect.tsx` (modify) | Guest states, remove hard login redirects for reads |
| `docs/CONFIGURATION.md`, `docs/ARCHITECTURE.md` (modify) | Document new keys + pool/guest/download architecture per maintenance map |

---

### Task 1: Service-token pool + config keys

**Files:**
- Create: `internal/pixiv/pool.go`
- Modify: `internal/pixiv/service.go:21-50`, `internal/config/config.go:82-86`, `cmd/server/app.go`, `cmd/server/reload.go`
- Test: `internal/pixiv/pool_test.go`, `internal/config/config_test.go` (existing file — append new case)

**Interfaces:**
- Consumes: `state.Token`, `config.PixivConfig`, existing `Service.Authenticated()`
- Produces: `func (p *Pool) Next() (refreshToken string, ok bool)`, `func (p *Pool) Evict(refreshToken string)`, `func (s *Service) ReadRefreshToken(r *http.Request) (string, bool)` (pool token for anonymous, user session token when present — exact header/cookie mechanism defined in Task 5; Task 1 only wires pool selection by "no user token" boolean)

- [ ] **Step 1: Write the failing test**

```go
func TestPoolEvictsRejectedToken(t *testing.T) {
    p := NewPool([]string{"good", "bad"})
    p.Evict("bad")
    got, ok := p.Next()
    if !ok || got != "good" {
        t.Fatalf("got %q,%v want good,true", got, ok)
    }
    if _, ok := p.Next(); !ok {
        t.Fatal("pool should still serve good")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pixiv/ -run TestPoolEvictsRejectedToken -v`
Expected: FAIL (`undefined: NewPool`)

- [ ] **Step 3: Write minimal implementation**

```go
package pixiv

import "sync"

type Pool struct {
    mu     sync.Mutex
    tokens []string
    idx    int
}

func NewPool(tokens []string) *Pool {
    cp := append([]string(nil), tokens...)
    return &Pool{tokens: cp}
}

func (p *Pool) Next() (string, bool) {
    p.mu.Lock()
    defer p.mu.Unlock()
    if len(p.tokens) == 0 {
        return "", false
    }
    t := p.tokens[p.idx%len(p.tokens)]
    p.idx++
    return t, true
}

func (p *Pool) Evict(refreshToken string) {
    p.mu.Lock()
    defer p.mu.Unlock()
    out := p.tokens[:0]
    for _, t := range p.tokens {
        if t != refreshToken {
            out = append(out, t)
        }
    }
    p.tokens = out
    p.idx = 0
}
```

Config addition (append to `PixivConfig`, keep existing keys untouched):

```go
ServiceRefreshTokens []string `koanf:"service_refresh_tokens" cfg:"sensitive=true,advanced=true"` // preset pool for anonymous public reads (empty = local single-user mode)
PublicReadEnabled    bool     `koanf:"public_read_enabled"`                                        // anonymous read via pool
```

Defaults in `baseDefaults`: `"pixiv.service_refresh_tokens": []string{}`, `"pixiv.public_read_enabled": false`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pixiv/ ./internal/config/ -v`
Expected: PASS

- [ ] **Step 5: Verify no unrelated churn**

Run: `go vet ./internal/pixiv/ ./internal/config/ && git diff --check`
Expected: clean. Leave diff uncommitted (no commit per repo policy).

---

### Task 2: Anonymous-read gates, server-job downloads locked down

**Files:**
- Modify: `internal/api/handler.go:240-246`, `internal/api/handler_illusts.go`, `internal/api/handler_users.go`, `internal/api/handler_search.go`, `internal/api/handler_events.go`, `internal/api/handler_downloads.go`
- Test: `internal/api/handler_public_test.go` (create; table-driven, no network — fake `pixiv.Service` gate only)

**Interfaces:**
- Consumes: `Task 1: Pool.Next/Evict`, `Service.Authenticated()`
- Produces: `func (h *APIHandler) requirePublicRead() error` (pool non-empty OR user session), `func (h *APIHandler) requireUserWrite() error` (existing `requireAuth` behavior, renamed call-sites for mutations only)

- [ ] **Step 1: Write the failing test**

```go
func TestRequirePublicReadAnonymous(t *testing.T) {
    h := &APIHandler{}
    if err := h.requirePublicRead(); err == nil {
        t.Fatal("want error when pool empty and unauthenticated")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestRequirePublicReadAnonymous -v`
Expected: FAIL (`undefined: requirePublicRead`)

- [ ] **Step 3: Write minimal implementation**

```go
// requirePublicRead allows anonymous reads when the service pool is
// configured; writes still go through requireUserWrite (old requireAuth).
func (h *APIHandler) requirePublicRead() error {
    if h.svc.Authenticated() {
        return nil
    }
    if h.pool != nil {
        if _, ok := h.pool.Next(); ok {
            return nil
        }
    }
    return pixiv.ErrNotAuthenticated
}
```

Then move read handlers (`illusts` 13, `users` 6, `search` 2, `events` SSE 1 — enumerate via `rg "requireAuth" internal/api/handler_*.go`) from `requireAuth()` to `requirePublicRead()`. Keep mutations (bookmark/follow/download-create/config-patch/system-apply) on the user gate. In `handler_downloads.go`, anonymous `POST` returns `WriteError(w, r, pixiv.ErrNotAuthenticated)` before touching `download.Manager` (prevents disk abuse); document in OpenAPI descriptions.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -v && go vet ./internal/api/`
Expected: PASS

- [ ] **Step 5: Verify spec + diff**

Run: `git diff --check`
Expected: clean. If `api/openapi.yaml` descriptions changed, regenerate both clients per repo rule before finishing this task (never hand-edit `server.gen.go` / `schema.gen.ts`). Leave diff uncommitted.

---

### Task 3: Frontend multi-pximg-proxy rewrite with fallback

**Files:**
- Modify: `frontend/src/lib/pixiv-image.ts:1-21`, `frontend/src/components/pximg-image.tsx:17-72`
- Test: manual + `bunx @biomejs/biome ci .` (no unit harness in repo — verify via build + click-through per repo frontend rules)

**Interfaces:**
- Consumes: upstream `i.pximg.net` URLs from API payloads
- Produces: `rewritePximgUrl(url) -> public-proxy URL`, `rewritePximgCandidates(url) -> string[]` (ordered try-list), `PximgImage` tries candidates in order via `onError`

- [ ] **Step 1: Write the failing check (document current single-host behavior)**

```ts
// Before: only PXIMG_HOST === "i.pximg.net" rewrites to /api/v1/proxy/img.
// Wanted: configurable list, e.g. ["https://i.pixiv.re", "https://pximg.cocomi.eu.org"],
// rewrite https://i.pximg.net/<path> -> <proxy>/<path>, keep /api/v1/proxy/img as last fallback.
```

- [ ] **Step 2: Run read-only check to confirm baseline**

Run: `bunx @biomejs/biome ci .`
Expected: PASS (baseline, in `frontend/`)

- [ ] **Step 3: Write minimal implementation**

```ts
const PXIMG_HOST = "i.pximg.net";
const PUBLIC_PROXIES: string[] = ["https://i.pixiv.re", "https://pximg.cocomi.eu.org"];
const SAME_ORIGIN_FALLBACK = "/api/v1/proxy/img";

export function rewritePximgCandidates(url: string | null | undefined): string[] {
    if (!url) return [];
    let u: URL;
    try {
        u = new URL(url);
    } catch {
        return [url];
    }
    if (u.hostname !== PXIMG_HOST) return [url];
    const path = `${u.pathname}${u.search}`;
    const out = PUBLIC_PROXIES.map((p) => `${p}${path}`);
    out.push(`${SAME_ORIGIN_FALLBACK}?url=${encodeURIComponent(url)}`);
    return out;
}

export function rewritePximgUrl(url: string | null | undefined): string {
    return rewritePximgCandidates(url)[0] ?? "";
}
```

`PximgImage`: keep props (`src/alt/fallback/className/fit/onLoad`) and `referrerPolicy="no-referrer"` + decode-reveal exactly; add `srcIndex` state, `onError` advances to next candidate, only `setErrored(true)` after last candidate fails.

- [ ] **Step 4: Verify**

Run: `bunx @biomejs/biome ci . && bun run build`
Expected: PASS. Click-through: ranking/search/artwork thumbnails load via public proxy with DevTools network showing proxy host; block first proxy (e.g. offline rule) → image still loads via next candidate.

- [ ] **Step 5: Verify diff**

Run: `git diff --check`
Expected: clean. Leave diff uncommitted.

---

### Task 4: Browser-side download (server never streams bytes for anonymous)

**Files:**
- Create: `frontend/src/features/downloads/browser-download.ts`
- Modify: artwork/illust viewer download button (the component that currently `POST`s a server job — locate via `rg "downloads" frontend/src/features/illusts`), `frontend/src/pages/downloads/index.tsx` (add guest notice; keep server table for operator)
- Test: click-through + biome/build

**Interfaces:**
- Consumes: `rewritePximgCandidates(originalUrl)` from Task 3
- Produces: `async function downloadViaBrowser(imageUrl: string, filename: string): Promise<"saved"|"opened">`

- [ ] **Step 1: Write the failing check**

```ts
// Wanted: downloadViaBrowser tries proxy candidates in order with fetch->blob->
// objectURL->a[download]; on CORS/type failure tries next; final fallback
// window.open(firstCandidate, "_blank") and returns "opened".
```

- [ ] **Step 2: Confirm baseline builds**

Run: `bunx @biomejs/biome ci .`
Expected: PASS (in `frontend/`)

- [ ] **Step 3: Write minimal implementation**

```ts
import { rewritePximgCandidates } from "@/lib/pixiv-image";

export async function downloadViaBrowser(imageUrl: string, filename: string): Promise<"saved" | "opened"> {
    const candidates = rewritePximgCandidates(imageUrl);
    for (const c of candidates) {
        try {
            const res = await fetch(c, { mode: "cors" });
            if (!res.ok) continue;
            const blob = await res.blob();
            const obj = URL.createObjectURL(blob);
            const a = document.createElement("a");
            a.href = obj;
            a.download = filename;
            document.body.appendChild(a);
            a.click();
            a.remove();
            setTimeout(() => URL.revokeObjectURL(obj), 30_000);
            return "saved";
        } catch {
            continue;
        }
    }
    window.open(candidates[0], "_blank", "noopener");
    return "opened";
}
```

Wire the viewer download button to this for anonymous users; keep server-job `POST /downloads` only for authenticated operator sessions. `pages/downloads/index.tsx` shows a guest notice ("server history is operator-only") when unauthenticated instead of the jobs table. FSA directory-pick / zip packing / userscript are explicitly out of scope for this task.

- [ ] **Step 4: Verify**

Run: `bunx @biomejs/biome ci . && bun run build`
Expected: PASS. Click-through: anonymous download saves via public proxy without any `/downloads` POST in network panel.

- [ ] **Step 5: Verify diff**

Run: `git diff --check`
Expected: clean. Leave diff uncommitted.

---

### Task 5: Guest UI, per-session user token, abuse guards, docs

**Files:**
- Modify: `frontend/src/pages/home/index.tsx` (RecentDownloads/FollowedAuthors guest empty-states), `pages/ranking/index.tsx`, `pages/search/index.tsx`, `pages/user/index.tsx`, `pages/me/me-redirect.tsx:10` (reads stay public; only `/me` + mutations redirect), query-options factories (add `anonymous` vs `user` key scope; reuse shared pagination helpers + `unwrap`), `internal/api/*` rate-limit middleware position (after httplog, before handlers), `docs/CONFIGURATION.md`, `docs/ARCHITECTURE.md`
- Test: `go test` affected pkgs + frontend `biome ci` + `build`

**Interfaces:**
- Consumes: Tasks 1–4 outputs
- Produces: documented `PIXIVBIU_PIXIV_SERVICE_REFRESH_TOKENS` (comma-separated via env resolver) + `public_read_enabled` semantics; anonymous quota behavior; `/me` isolation statement

- [ ] **Step 1: Write the failing test (rate-limit probe)**

```go
func TestAnonymousReadRateLimited(t *testing.T) {
    // Send N+1 rapid anonymous GETs to a public-read endpoint in test harness;
    // want 429 with kind=app on the overflow request, never err.Error() text.
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/api/ -run TestAnonymousReadRateLimited -v`
Expected: FAIL (no limiter yet)

- [ ] **Step 3: Write minimal implementation**
  - Session isolation: user `refresh_token` login continues to persist server-side via existing `state.Store` path for the operator; anonymous requests never touch it and use pool tokens only; per-browser user tokens (multi-user) are explicitly deferred — document that public users wanting personal bookmarks/follows must run their own instance or wait for a follow-up multi-session design (do not bolt a half-isolated cookie session in this task).
  - Abuse guards: per-IP rate limit on anonymous reads (middleware after httplog), pool `Evict` on refresh `invalid_grant`, bounded retries, reuse existing `singleflight` patterns for fan-out endpoints; do not add metrics/tracing/quota-middleware beyond this (repo boundary).
  - Guest UI: `home/index.tsx` hides `RecentDownloads` table for anonymous (empty-state copy via `useMessages`), `FollowedAuthors` shows sign-in hint; ranking/search/user pages render data without login; only `/me` and mutation buttons redirect to login.
  - Docs: `docs/CONFIGURATION.md` (new keys table rows), `docs/ARCHITECTURE.md` (pool + browser-download + guard diagram in words), keep config tables complete.

- [ ] **Step 4: Run full verification for the change surface**

Run: `go test ./internal/pixiv/ ./internal/api/ ./internal/config/ -v && go vet ./...`
Run: `bunx @biomejs/biome ci . && bun run build` (in `frontend/`)
Expected: PASS. Report any unverified platform behavior (no native desktop test claimed from cross-build).

- [ ] **Step 5: Final diff check**

Run: `git diff --check`
Expected: clean. Leave everything uncommitted for user review. Open question carried forward: R18 policy + multi-user personal sessions are not decided — site ships showing upstream results until user rules otherwise.

---

## Self-Review

1. **Spec coverage:** user decisions mapped — no-Hibi/Pxve → pool design (T1) + untouched wire models; preset pool → T1/T2/T5; public-proxy browser fetch → T3/T4; browser-default download → T4; overseas deploy → proxy/SNI notes + abuse guards in T5. R18 explicitly left open in T5.
2. **Placeholder scan:** no TBD/TODO/"similar to"; every step has exact file, exact command, expected output, and concrete code.
3. **Type consistency:** `Pool.Next/Evict`, `requirePublicRead/requireUserWrite`, `rewritePximgCandidates/rewritePximgUrl`, `downloadViaBrowser` names match across tasks; `PximgImage` props unchanged; config keys identical in code/defaults/docs steps.

---

Plan complete and saved to `docs/superpowers/plans/2026-09-23-public-site-conversion.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
