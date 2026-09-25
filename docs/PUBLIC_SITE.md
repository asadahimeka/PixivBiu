# Public-Site Deployment

Runbook for the public-site conversion: obtaining and presetting Pixiv refresh tokens, running the development service, and deploying to a server. Settings facts live in [Configuration](CONFIGURATION.md), container details in [Docker](DOCKER.md), local setup in [Development](DEVELOPMENT.md), and design context in [Architecture](ARCHITECTURE.md).

## Deployment modes

One binary, two modes, switched by `pixiv.public_read_enabled`:

| | Local mode (default) | Public-site mode |
| --- | --- | --- |
| Who reads | the shared operator session | anonymous visitors, authenticated upstream by the preset token pool |
| Mutations (bookmark, follow, downloads, settings) | operator | closed — the write gate answers on the mode alone, and the login surface is closed, so a public instance is anonymous and effectively read-only |
| Tokens on disk | session in `<dataRoot>/usr/state.json` | session (if any) in `usr/state.json`; preset pool in `usr/settings.json` or the environment |

## 1. Obtain a Pixiv refresh token

Each refresh token belongs to one Pixiv account. Get one by signing in once through a local-mode instance:

1. Start the app somewhere with a browser and a working route to Pixiv: a release build (`./bin/pixivbiu`), or the development service ([section 3](#3-run-the-development-service)). Set `PIXIVBIU_PIXIV_PROXY` first if Pixiv is not directly reachable.
2. Open the login page and sign in with the OAuth popup, or paste an already-obtained refresh token.
3. On success the session is persisted to `<dataRoot>/usr/state.json`; copy its `refresh_token` value. Do this **before** logging out — logout clears the file.
4. Repeat per account (a fresh browser profile each time) to collect one token per pool member.

Treat every refresh token as a credential: never commit it, paste it into an issue, or copy another person's auth state. Pool entries are stored cleartext in `settings.json` and masked as `***` in API views.

### Presetting without the login page (headless server)

For a headless deployment you can skip the interactive login entirely:

- **Public mode**: preset the pool through `settings.json` or the environment as below; nothing else is needed.
- **Local mode**: sign in once on a machine with a browser, then copy its `usr/state.json` to the server's data root while the process is stopped. A file containing just `{"refresh_token": "..."}` is enough — the access token is refreshed on demand. The store creates the directory 0700 and the file 0600; preserve those modes.

## 2. Preset the refresh-token pool

Two sources, both applied at process start (manual file edits and environment changes need a restart; see [manual edits](CONFIGURATION.md#configuration-layers)):

**Settings file** — `<dataRoot>/usr/settings.json` (Docker: `/data/usr/settings.json`; development checkout: `./usr/settings.json`, which is gitignored). The file may contain only your overrides:

```json
{
  "pixiv": {
    "service_refresh_tokens": ["<token-account-a>", "<token-account-b>"],
    "public_read_enabled": true
  }
}
```

**Environment** — the list is comma-separated here; the file value stays a JSON array:

```sh
export PIXIVBIU_PIXIV_SERVICE_REFRESH_TOKENS="token-a,token-b"
export PIXIVBIU_PIXIV_PUBLIC_READ_ENABLED=true
```

Rules the core enforces ([validator](../cmd/server/app.go), [pool](../internal/pixiv/pool.go)):

- Entries are whitespace-trimmed; empty or whitespace-only entries are rejected at load and on PATCH.
- Enabling `public_read_enabled` with an empty pool fails validation and blocks startup.
- Both keys are hot when changed through the config API (a logged-in operator in local mode). In public mode there is no operator session, so toggling off means changing file/env and restarting.
- The Settings form cannot rewrite the list — the masked read-back PATCHes as a no-op. Use the file, the environment, or a PATCH carrying the JSON array itself.
- Upstream reads draw tokens round-robin from the pool. A token Pixiv permanently rejects (`invalid_grant`) is evicted immediately; once drained, anonymous reads fail closed (401) with no fallback to any operator session.
- While public mode is on, *all* reads authenticate upstream as pool identities, and per-browser user tokens are deferred — features that need the operator's own identity upstream (private bookmarks) are unavailable.
- **A public deployment should not carry an operator session.** The write gates and the open `GET /auth/status` ignore a session left in `<dataRoot>/usr/state.json` (migrated from a desktop install, or kept from a local-mode run) — the server enforces closure and logs a warning at boot — but removing or moving the file away is still cleaner: it stops the background refresh loop from keeping that dead session alive, and nothing can re-admit it later.

## 3. Run the development service

First checkout, from the repository root:

```sh
go mod download
cd frontend && bun install --frozen-lockfile
```

Then two terminals:

```sh
make dev                 # backend on http://127.0.0.1:4001 (port pinned, no auto-open)
cd frontend && bun run dev   # Vite on http://localhost:5173, proxying /api to 4001
```

Health (login not required): `curl -fsS http://127.0.0.1:4001/api/v1/health`. The API viewer is at `http://127.0.0.1:4001/docs`. Under `make dev`, state and downloads anchor to the repository root. To exercise public-site mode locally, put the pool and switch into `./usr/settings.json` and restart `make dev`. Full workflow, builds, and troubleshooting: [Development](DEVELOPMENT.md).

## 4. Deploy on a server

### Docker Compose (recommended)

Clone the repository, prepare the downloads bind mount for the container's uid, add the preset environment, and start:

```sh
git clone https://github.com/txperl/PixivBiu.git && cd PixivBiu
mkdir -p downloads && sudo chown 65532:65532 downloads
```

In [docker-compose.yml](../docker-compose.yml), extend the `environment` block:

```yaml
    environment:
      PIXIVBIU_PIXIV_SERVICE_REFRESH_TOKENS: "token-a,token-b"
      PIXIVBIU_PIXIV_PUBLIC_READ_ENABLED: "true"
      # PIXIVBIU_PIXIV_PROXY: ""                # only if Pixiv is not directly reachable
    # extra_hosts:                               # Linux only, with the proxy above
    #   - "host.docker.internal:host-gateway"
```

```sh
docker compose up -d
```

Fronting it with a reverse proxy? Publish only on loopback by changing the mapping to `127.0.0.1:4001:4001`. Volumes, the proxy-to-Pixiv options, updating, and backup/restore are documented in [Docker](DOCKER.md).

### docker run

```sh
docker run -d --name pixivbiu --restart unless-stopped \
  -p 127.0.0.1:4001:4001 \
  -e PIXIVBIU_PIXIV_SERVICE_REFRESH_TOKENS="token-a,token-b" \
  -e PIXIVBIU_PIXIV_PUBLIC_READ_ENABLED=true \
  -v pixivbiu-data:/data \
  -v "$PWD/downloads:/downloads" \
  ghcr.io/txperl/pixivbiu:latest
```

### Release binary or source build

Download a platform archive from [Releases](https://github.com/txperl/PixivBiu/releases), or build from source (`make dist` → `./bin/pixivbiu`, frontend embedded). The default data root is the executable's directory; override with `-data-dir <path>` or `PIXIVBIU_DATA_DIR`. `server.host`/`server.port` are internal, restart-only keys — set them through the environment or file, never the settings API.

Run it as a dedicated non-root user, e.g. under systemd:

```ini
[Unit]
Description=PixivBiu
After=network-online.target

[Service]
User=pixivbiu
ExecStart=/opt/pixivbiu/pixivbiu -data-dir /var/lib/pixivbiu
Environment=PIXIVBIU_SERVER_HOST=127.0.0.1
Environment=PIXIVBIU_PIXIV_SERVICE_REFRESH_TOKENS=token-a,token-b
Environment=PIXIVBIU_PIXIV_PUBLIC_READ_ENABLED=true
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

### Reverse proxy

Terminate TLS at the proxy and forward client addresses — chi's `RealIP` consumes `X-Forwarded-For`, and the built-in per-IP read budget (300 GET/HEAD per minute under `/api/v1`, health and image proxy exempt, not configurable) keys on that address. A proxy that does not set XFF collapses every visitor into one budget bucket. The SSE stream at `/api/v1/events` needs response buffering disabled and long-lived connections allowed:

```nginx
location / {
    proxy_pass http://127.0.0.1:4001;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_http_version 1.1;
    proxy_set_header Connection "";
    proxy_buffering off;
    proxy_read_timeout 1h;
}
```

### Access control

- **Local mode** shares one operator session with everyone who can reach the service — anyone on the network browses, downloads, and changes settings as the operator. Bind to loopback and gate access at the proxy (TLS, allowlist, or authentication) for anything beyond localhost.
- **Public mode** is anonymous read-only, but the data volume holds live credentials: keep `/data` private and back it up like a secret ([backup guide](DOCKER.md#backup-and-restore)).

### Verify

```sh
curl -fsS http://127.0.0.1:4001/api/v1/health        # {"status":"ok"} — process is up
curl -fsS http://127.0.0.1:4001/api/v1/auth/status   # {"authenticated":false,"public_read":true} in public mode
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:4001/openapi.json   # 404 — dev docs closed in public mode
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:4001/api/v1/system/version # 404 — system info closed
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:4001/api/v1/config  # 401 — operator surface closed
```

Then open the site in a private browser window: browsing works without signing in, sign-in and mutation controls are hidden, and the log shows no startup validation errors (a `public-site mode ignores the persisted operator session` warning means a leftover `state.json` should be removed). Docker health status: `docker inspect --format '{{.State.Health.Status}}' pixivbiu`.
