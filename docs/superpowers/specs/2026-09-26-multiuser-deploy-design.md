# Multi-user login, easy install, graceful FS permissions — Design

Date: 2026-09-26
Status: approved in brainstorming, pending spec review

## Goal

Make xirc safe to expose beyond a LAN and easy for a non-author to install
(bare metal with nginx **or** Apache, or Docker), without anything failing
opaquely when the service user lacks filesystem permissions. Also remove
author-specific/personal data from the tracked tree.

Success criteria:

- A fresh host goes from `git clone` to a working, logged-in UI via one
  wizard (`sudo scripts/install.sh`) or one `docker compose up`.
- Requests from outside configured trusted networks cannot reach any API
  without a valid session; normal users cannot reach admin APIs (server-side
  enforcement, not just hidden UI).
- A non-writable media/download directory never crashes the app; the
  affected feature is disabled with a human-readable reason and fix hint.

Non-goals: per-user download ownership (downloads stay shared), SSO/OIDC,
2FA, rewriting git history (author email stays).

## Sub-projects and order

1. Personal-info cleanup
2. Auth: users, sessions, roles
3. FS capability checks / graceful degradation
4. Docker image + release pipeline
5. Reverse-proxy templates + `install.sh` wizard

2 → 3 → 4/5 because the capability banner and the Docker/installer docs
depend on auth (admin-only banner, first-admin flow). 1 is independent.

---

## 1. Personal-info cleanup

Audit result: **no real secrets** in the tree or in git history (only
`changeme` placeholders; IRC passwords are `json:"-"`). Personal/site
specifics to neutralise:

| Where | Change |
|---|---|
| `Makefile`, `scripts/preconfig.sh`, `docs/install.md` | `192.168.20.2` → `maxwell.local`; `PI_USER=dtx`/`pi` examples → `maxwell` |
| `deploy/docker-compose.yaml` | `TZ=Europe/Berlin` → commented example |
| `deploy/docker-compose.yaml` | MariaDB passwords → `${MYSQL_PASSWORD:?set in .env}` / `${MYSQL_ROOT_PASSWORD:?…}`; add `deploy/.env.example` |
| `docs/2026-07-08-*`, code comments (`parser/parser.go`, `irc/connection.go`), tests (`parser/*_test.go`) | real channel/bot names (`#moviegods`, `#mg-chat`, `BotReign`, `[EWG]…`) → `#example-dl`, `#example-chat`, `ExampleBot` |

**Kept deliberately:** builtin pattern *names* (`botreign-pipe-xdcc`,
`ewg-command-xdcc`) — they are keys for the one-time seed upgrade in
`parser/seed.go`; renaming would orphan rows on existing installs. They name
public bot-script formats, not people. Their comments become generic.
`RealDtx` (public GitHub handle / module path) and the commit author email
stay.

---

## 2. Auth

### Data

New tables (migrations for SQLite and MySQL, same shape):

- `users(id, username UNIQUE, password_hash, role, created_at)` —
  `password_hash` is bcrypt (`golang.org/x/crypto/bcrypt`, `DefaultCost`);
  bcrypt embeds a per-hash random salt and the cost. `role ∈ {admin, user}`.
- `sessions(token_hash PRIMARY KEY, user_id, expires_at)` — token is 32
  random bytes (base64url) in the cookie; only its SHA-256 is stored. 30-day
  lifetime, sliding (extended on use, at most once per hour to limit writes).
  Deleting a user cascades to sessions. Expired sessions are pruned by the
  existing `maintenance` job.

Store interface gains: `CreateUser, GetUserByName, GetUser, ListUsers,
UpdateUser, DeleteUser, CountAdmins, CreateSession, GetSession,
TouchSession, DeleteSession, DeleteExpiredSessions`.

### Config

```yaml
auth:
  trusted_networks: []          # CIDRs that skip login; empty = login always required
  trusted_role: admin           # role granted to trusted-network visitors: admin|user
  trusted_proxies: [127.0.0.1/32, ::1/128]  # peers whose X-Forwarded-For/-Proto are believed
```

Env overrides (`XIRC_AUTH_TRUSTED_NETWORKS=…`) require the env-override code
in `config/config.go` to support `[]string` as comma-separated values.

### Request pipeline (`server/auth.go`)

Middleware wraps `s.mux`:

1. **Client IP**: `r.RemoteAddr`; if that peer is in `trusted_proxies`, use
   the right-most `X-Forwarded-For` entry that is not itself a trusted proxy.
   **Scheme**: `X-Forwarded-Proto` only from trusted proxies, else `r.TLS`.
2. **Identity**: valid `xirc_session` cookie → that user. Else client IP in
   `trusted_networks` → synthetic principal `{username: "lan", role:
   trusted_role, via: "network"}`. Else anonymous.
3. **Anonymous** may reach: static files, `/api/health`,
   `/api/auth/login`, `/api/auth/me`, and `/api/setup/*` **only while the
   users table is empty**. Everything else under `/api/` and `/ws` → 401.
4. **Role check**: one table `adminOnly` (path prefix + methods) in
   `auth.go`. Non-admin hitting it → 403. The table:

| Admin-only | Methods |
|---|---|
| `/api/irc/raw`, `/api/irc/connect`, `/api/irc/disconnect`, `/api/irc/join` | all |
| `/api/servers`, `/api/realms`, `/api/library` (+ subpaths) | non-GET |
| `/api/search/patterns` (+ subpaths), `/api/search/unmatched` | all |
| `/api/index/clear`, `/api/errors`, `/api/users` (+ subpaths), `/api/setup/*` (after first admin), `/api/capabilities/recheck` | all |
| `/api/files` | non-GET |
| `/api/downloads/delete`, `/api/downloads/clear`, `/api/downloads/set-target`, `/api/downloads/move` | all |

Users keep: search (live + index), request/cancel/retry downloads, downloads
list, dashboard/stats/storage, read-only browse/files, sending channel
messages (`/api/irc/message`), WebSocket.

Principal is placed in the request context; handlers needing it use
`principalFrom(r.Context())`.

### Cookie

`xirc_session`, `HttpOnly`, `SameSite=Lax`, `Path=<server.prefix or "/">`,
`Secure` when the effective scheme is https. CSRF: SameSite=Lax + all
mutating endpoints require `Content-Type: application/json` (middleware
rejects other content types on non-GET `/api/*` with 415) — no token.

### Endpoints

- `POST /api/auth/login {username,password}` → sets cookie, returns me.
  Rate limit: 5 failures per client IP per minute (in-memory map, pruned on
  access) → 429. Constant-time-ish: on unknown user, still run a bcrypt
  compare against a fixed dummy hash.
- `POST /api/auth/logout` → deletes session, clears cookie.
- `GET /api/auth/me` → `{username, role, via: "session"|"network"}`, or
  `{setup_required: true}` when no users exist, or 401.
- `POST /api/auth/setup {username,password}` → creates first admin; 409 once
  any user exists. Logs in.
- `GET/POST /api/users`, `PUT/DELETE /api/users/{id}` (admin) — create,
  change role, reset password, delete. Guard: cannot delete or demote the
  last admin (409). Password min length 8.
- CLI: `xirc --create-admin <name>` reads the password from the TTY (or
  `XIRC_ADMIN_PASSWORD` for non-interactive use), creates/overwrites that
  admin, exits. Works via `docker exec`.

### Frontend

- `api.js`: single internal `request()` used by get/post/put/del; on 401
  dispatches `xirc:unauthorized` → app shows login overlay.
- `app.js` startup: `GET /api/auth/me` → `me`, `isAdmin`. `setup_required` →
  create-admin form (before the existing directory wizard).
- Admin-only sections (settings: servers/realms/patterns/library/users,
  raw IRC input, file-manager mutating actions, clear index, errors) wrapped
  in `x-show="isAdmin"`. Existing simple/advanced toggle unchanged (display
  preference only).
- Settings → new "Users" tab (list, add, role toggle, reset password,
  delete). Header shows username + logout (hidden when `via === "network"`
  with a "log in" link instead).

### Tests

- Client-IP resolution: direct peer; trusted proxy with XFF; **untrusted peer
  sending spoofed XFF** (must be ignored); IPv6.
- Permission table: for each admin-only entry, user → 403, admin → pass;
  anonymous → 401; trusted network with `trusted_role: user` → 403 on admin
  routes.
- Login ok/bad password/rate limit; session expiry; logout invalidates.
- Last-admin guard; setup endpoint 409 after first user.
- Store tests for both SQLite and MySQL (MySQL tests follow existing skip
  convention).

---

## 3. Filesystem capabilities

### Current behaviour replaced

`main.go:checkDirectories` exits 78 on a non-writable media/downloads dir.
New behaviour: only the **database directory** (SQLite) stays fatal; all
else degrades.

### `fscheck` package

```go
type DirStatus struct {
    Path   string `json:"path"`
    Exists bool   `json:"exists"`
    Read   bool   `json:"read"`
    Write  bool   `json:"write"`
    Reason string `json:"reason,omitempty"` // human text incl. owner/mode/uid
}
func Probe(path string) DirStatus
func Describe(err error, path string) error // EACCES/EPERM/EROFS → friendly error
```

`Probe`: stat → list (read) → create+remove `.xirc_write_check` (write).
`Reason` example: `"not writable: /srv/media (owner root:root 0755; xirc runs
as uid 1000 gid 1000)"`.

### Capability model

A `Capabilities` struct held by the server (mutex-guarded), recomputed at
startup, every 5 min, and on `POST /api/capabilities/recheck` (admin).
Broadcast over WS as `capabilities` event when it changes.

| Capability | Depends on | Effect when false |
|---|---|---|
| `downloads` | `downloads_dir` + `temp_dir` writable | `/api/downloads/request` → 503 with reason; queue does not start new transfers |
| `library` | `media_dir` + each category dir writable | engine leaves finished files in `downloads_dir` (existing fallback) and records the reason on the download; library moves → 503 |
| `library_read` | `media_dir` readable | library browse hidden |
| `roots[]` | each file-manager root read/write | root hidden (no read) or read-only: move/rename/delete → 403 with reason |
| `logging` | log dir writable | channel logging off, one warning logged |
| `library_config` | `categories.yaml` dir writable | library settings read-only in UI |
| `auto_extract` | extraction target writable | extraction skipped with reason |

`GET /api/capabilities` (any authenticated principal) returns the struct.
Missing directories keep the existing setup-wizard flow (but no exit).

### Point-of-use errors

All FS mutations in `server/files_handler.go`, `server/download_handlers.go`,
`queue/engine.go`, `library/*`, `dcc/transfer.go` wrap errors with
`fscheck.Describe`. Handlers map permission errors to 403 (not 500) with
the described message. The engine marks the download failed/“left in
downloads” with the message; never leaves a half-moved file (copy fallback
removes the partial destination on error).

### UI

- Disabled controls show `reason` as tooltip (everyone).
- Admin banner listing failing capabilities + fix hint:
  systemd install → `sudo chown -R xirc:xirc <dir>` (or add `xirc` to the
  dir's group); Docker (detected via `/.dockerenv`) → "set PUID/PGID to the
  owner of the host directory". "Recheck" button.

### Tests

Temp dirs with 0555/0000 (skipped when `os.Geteuid()==0`):
`Probe` read/write matrix; files handler move on read-only root → 403 with
reason; engine with non-writable media dir → file stays in downloads, marked
done with reason; startup no longer exits.

---

## 4. Docker + release

### Dockerfile (`deploy/Dockerfile`)

- Builder: `golang:<latest stable>-alpine`, `CGO_ENABLED=0`, version via
  `-X main.version`.
- Runtime: `alpine:<latest>` + `ca-certificates tzdata`; user created from
  `ARG PUID=1000 PGID=1000`; binary at `/usr/local/bin/xirc`.
- **No config baked in.** Image `ENV`:
  `XIRC_SERVER_HOST=0.0.0.0`, `XIRC_DATABASE_PATH=/data/xirc.db`,
  `XIRC_STORAGE_DOWNLOADS_DIR=/downloads`,
  `XIRC_STORAGE_TEMP_DIR=/downloads/.tmp`, `XIRC_STORAGE_MEDIA_DIR=/media`,
  `XIRC_STORAGE_CATEGORIES_FILE=/data/categories.yaml`.
- `VOLUME /data /downloads /media`, `EXPOSE 8085`,
  `HEALTHCHECK` via `wget -qO- http://127.0.0.1:8085/api/health`.
- `ENTRYPOINT ["xirc", "--config", "/data/config.yaml"]`.
- Channel logs land in `/data/logs` automatically: `main.go` derives the
  log dir from `filepath.Dir(database.path)`. With MySQL the log dir falls
  becomes `./logs` (`filepath.Dir("")` is `.`) — the image sets `WORKDIR /data`
  so that also resolves under the volume.

### Config loading

`config.Load`: missing file → log `"config: %s not found, using defaults +
environment"` and continue with defaults + env. Parse errors stay fatal.

### docker-compose.yaml

Service `xirc` with `build:` and `image: ghcr.io/realdtx/xirc:latest`;
ports `8085:8085` (comment: bind to 127.0.0.1 when behind a host proxy);
volumes `./data:/data`, `${DOWNLOADS_DIR}:/downloads`, `${MEDIA_DIR}:/media`
from `.env` (with same-filesystem note for fast renames); commented `TZ`,
`XIRC_AUTH_TRUSTED_NETWORKS`. MariaDB profile unchanged except passwords from
`.env`.

### Release workflow

`.github/workflows/release.yml` (on `v*` tag): binaries named
`xirc-linux-amd64`, `xirc-linux-arm64`, `xirc-linux-armv7`; Go version from
`go.mod` (`go-version-file`). New job: `docker/setup-qemu` +
`docker/setup-buildx` + `docker/login-action` (GHCR, `GITHUB_TOKEN`) +
`docker/build-push-action`, platforms `linux/amd64,linux/arm64`, tags
`latest` + version; image name `ghcr.io/<lowercased repository_owner>/xirc`.
Tags containing `-` (e.g. `-beta`) create a GitHub **pre-release**
(`prerelease: ${{ contains(github.ref_name, '-') }}`).

**First release:** once all sub-projects are merged and `make test` passes,
tag `v0.4.0-beta` on master and push the tag (with the user's go-ahead at
that moment) — this produces the first downloadable binaries and image that
`install.sh` step 1 relies on. `install.sh` queries
`/repos/RealDtx/maxwell-xirc/releases` (not `/latest`, which skips
pre-releases) and takes the newest entry.

---

## 5. Reverse proxy + install wizard

### Shared templates: `scripts/proxy-templates.sh`

Sourced bash functions, parameters via env (`PORT`, `PREFIX`, `SERVER_NAME`):

- `render_nginx_site`, `render_nginx_subpath`
- `render_apache_site`, `render_apache_subpath`

All variants: WebSocket upgrade on `<prefix>/ws`, `X-Forwarded-For`,
`X-Forwarded-Proto`, `Host`, long read timeout. Nginx LAN `allow/deny` block
becomes a commented optional section. Apache uses `mod_proxy`,
`mod_proxy_http`, `mod_proxy_wstunnel`, `mod_rewrite` (`RewriteCond
%{HTTP:Upgrade} websocket [NC]` → `ws://`), `mod_headers`
(`RequestHeader set X-Forwarded-Proto expr=%{REQUEST_SCHEME}`),
`ProxyPreserveHost On`, `ProxyTimeout 86400`; header comment lists the
`a2enmod` line. Subpath variants strip the prefix and require
`server.prefix` set to the same value.

`preconfig.sh` is refactored to use these functions and gains the
nginx/apache/none choice; `make deploy` installs whichever file exists
(Apache: copy to `sites-available`, `a2enmod …`, `a2ensite`, `apachectl
configtest`, reload). `deploy/nginx-maxwell-irc.conf` and new
`deploy/apache-xirc.conf` are regenerated from the templates (checked-in
examples).

### `scripts/install.sh` (run on the target, as root)

Flags: `--print-proxy nginx|apache [--prefix /xirc] [--server-name host]
[--port 8085]` → print config + enable commands, no root needed, no changes.

Wizard steps:

1. **Binary**: `./xirc` next to the script → else `go build` from checkout if
   a Go ≥ `go.mod` version is on PATH → else latest GitHub release asset for
   `uname -m` (x86_64→amd64, aarch64→arm64, armv7l→armv7) → else stop with
   instructions. Installs to `<install_dir>/xirc`.
2. **Paths** (defaults): install dir `/opt/xirc`; downloads `/srv/downloads`;
   temp `<downloads>/.tmp`; media (finished) `/srv/media`; DB SQLite
   `/opt/xirc/data/xirc.db` or MariaDB DSN; port `8085`.
3. **User**: create system user `xirc` (no login shell) if missing.
4. **Per-path check**: missing → offer create (owned by `xirc`). Then
   `sudo -u xirc test -w`; if not writable offer: (a) `chown -R xirc:`,
   (b) add `xirc` to the dir's group + `chmod g+rwX`, (c) continue — feature
   will be disabled at runtime (section 3).
5. **Auth**: trusted networks (suggest private ranges, default none),
   trusted role (admin/user).
6. **Write** `config.yaml` (keep existing unless user confirms overwrite) and
   `/etc/systemd/system/xirc.service` (existing unit contents, paths
   substituted, `User=xirc`); `systemctl daemon-reload && enable --now xirc`.
7. **Proxy**: detect nginx (`command -v nginx`) / Apache (`apache2ctl` or
   `httpd`). Ask: nginx / apache / both / none; mode: own site
   (server name) or subpath (prefix; also written to `server.prefix`); then
   **auto** or **manual**.
   - Auto: write site file, enable (nginx symlink / `a2enmod` + `a2ensite`),
     config test; on failure remove the file, show error, fall back to
     manual output; on success reload.
   - Manual: print rendered config + exact commands.
8. **Summary**: URL; "first visit creates the admin account"; HTTPS hint
   (`certbot --nginx` / `--apache`); how to re-run.

Idempotent: re-running detects existing user/unit/config and asks before
changing each.

### Naming

Installer-created artefacts use `xirc` (binary, user, unit, install dir),
matching the existing Pi deployment. `make build` output name is unchanged.

### Docs

`README.md`: three install paths (installer, Docker, remote `make deploy`).
`docs/install.md`: updated defaults, Apache section, Docker section, TLS
note (Secure cookie needs HTTPS at the proxy), first-admin + `--create-admin`.

### Tests

`bash -n` on scripts; a small `scripts/test-proxy-templates.sh` that renders
each variant and, when available, runs `nginx -t -c` / `apachectl -t` on the
output (skips otherwise). `--print-proxy` exercised in CI via `bash`.
