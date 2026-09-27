# maXwell IRC Installation Guide

There are three ways to install xirc:

1. **`scripts/install.sh`** — an interactive wizard you run as root on the
   target server. Installs the binary, creates the `xirc` system user, checks
   directory permissions, installs the systemd service, and optionally
   configures nginx or Apache. This is the quickest path for a single server.
2. **Docker** — `docker compose up -d` from the `deploy/` directory.
3. **`make preconfig` + `make deploy`** — from a dev machine, generates a
   profile directory and pushes the binary/config/service/proxy files to a
   target over SSH. Useful for repeatable or scripted deploys, or updating a
   server you already set up.

All three end at the same place: open the printed URL and log in.
`install.sh` creates the admin account before it starts the service; with
Docker or `make deploy`, create it with `--create-admin` (below) — otherwise
the first visitor gets to create it. **Don't make xirc reachable from other
machines until an admin exists.** See [Users & login](#users--login).

---

## Option A — `scripts/install.sh`

```bash
git clone https://github.com/RealDtx/maxwell-xirc.git && cd maxwell-xirc
sudo scripts/install.sh
```

The wizard:

1. **Asks for paths** — install directory (`/opt/xirc`), downloads directory
   (`/srv/downloads`), temp directory, media directory (`/srv/media`),
   database (SQLite path or MySQL/MariaDB DSN), and the port xirc listens on
   (`8085`, bound to `127.0.0.1`).
2. **Finds a binary** — offers a prebuilt `scripts/xirc` next to the script
   (showing its date) if there is one, else builds from source into a
   temporary directory if a Go toolchain matching `go.mod`'s version is on
   `PATH`, else downloads the newest release asset for your architecture
   (`amd64`/`arm64`/`armv7`) from GitHub, else stops with instructions.
   Nothing is written into the checkout, so a re-run after `git pull`
   always builds fresh.
3. **Creates the `xirc` system user** (no login shell) if it doesn't exist.
4. **Checks each directory**: offers to create it (owned by `xirc`) if
   missing; if it exists but isn't writable by `xirc`, offers to (a) `chown
   -R xirc:xirc` it, (b) add `xirc` to the directory's group and make it
   group-writable (only offered for a non-system group), or (c) leave it —
   the related feature is disabled at runtime with a reason shown in the UI
   (see [Permissions](#permissions)). An existing directory's ownership is
   never changed without asking.
5. **Asks about login** — networks that skip login, and the role granted to
   them (see [Users & login](#users--login)).
6. **Writes `config.yaml` and `/etc/systemd/system/xirc.service`**. If
   either file already exists and differs, it asks before overwriting; a
   replaced proxy file is instead backed up as `<file>.bak.<timestamp>`.
7. **Creates the admin account** — asks for a username and password (typed
   hidden, passed via `XIRC_ADMIN_PASSWORD`, never on the command line) and
   runs `xirc --create-admin` as the `xirc` user. Defaults to yes on a first
   install; on a re-run it defaults to no (yes resets that admin's password).
   Only then does it run `systemctl enable --now xirc`.
8. **Reverse proxy** — detects installed nginx/Apache and offers to configure
   one or both, as either a standalone site (own hostname) or a subpath of an
   existing site. See [Reverse proxy](#reverse-proxy).
9. **Prints a summary** — the URL to open, the admin to log in as (or, if
   you skipped that step, a reminder that the first visit creates the admin),
   and (if a proxy was configured) the `certbot` command for HTTPS.

Re-running the script is safe: it detects the existing user, unit, config and
proxy files and asks before changing each.

**Flags** (no root needed, no changes made):

```bash
scripts/install.sh --help
scripts/install.sh --print-proxy nginx|apache [--prefix /xirc] [--server-name host] [--port 8085]
```

`--print-proxy` prints the rendered config and the exact commands to enable
it — useful if you manage nginx/Apache yourself and just want the snippet.

---

## Option B — Docker

```bash
cd deploy
cp .env.example .env   # edit DOWNLOADS_DIR, MEDIA_DIR, PUID/PGID
mkdir -p data && sudo chown 1000:1000 data   # your PUID:PGID from .env
docker compose up -d
docker exec -it xirc xirc --create-admin <name>   # before exposing the port
```

The container runs as `PUID:PGID` (compose `user:`), so `./data`,
`DOWNLOADS_DIR` and `MEDIA_DIR` must all be writable by that uid/gid. Create
`./data` yourself as above — if it's missing, Docker creates it owned by
root and xirc can't open its database (it exits and restarts in a loop).

The port is published on `127.0.0.1:8085` only. Create the admin first (last
line above); then either put a reverse proxy on the host in front of it, or
change the `ports:` line in `docker-compose.yaml` to `"8085:8085"` to reach
xirc directly from other machines. Until an admin exists, whoever opens xirc
first can create one.

`.env` (see `deploy/.env.example`):

| Variable | Purpose |
|---|---|
| `DOWNLOADS_DIR`, `MEDIA_DIR` | Host directories mounted into the container; must already exist and be writable by `PUID:PGID` |
| `PUID`, `PGID` | uid/gid the container runs as (default `1000`) — set to `id -u` / `id -g` of the owner of `./data` and the host directories |
| `TZ` | Container timezone (default `UTC`) |
| `TRUSTED_NETWORKS`, `TRUSTED_ROLE` | See [Users & login](#users--login) |
| `MYSQL_ROOT_PASSWORD`, `MYSQL_PASSWORD` | Only used with the `mariadb` profile below |

Volumes: `./data:/data` (SQLite DB, `config.yaml` override, channel logs,
`categories.yaml`), `${DOWNLOADS_DIR}:/downloads`, `${MEDIA_DIR}:/media`. Keep
downloads and media on the same host filesystem so finished downloads are
renamed into place instantly instead of copied.

No config is baked into the image — defaults come from `ENV` in the
Dockerfile (`XIRC_SERVER_HOST`, `XIRC_DATABASE_PATH=/data/xirc.db`,
`XIRC_STORAGE_DOWNLOADS_DIR=/downloads`, `XIRC_STORAGE_TEMP_DIR`,
`XIRC_STORAGE_MEDIA_DIR=/media`, `XIRC_STORAGE_CATEGORIES_FILE`); mount a
`/data/config.yaml` or add more `XIRC_*` environment variables to override
anything else. Mount the `/data` directory (as above), not a single
`config.yaml` file: saving System settings replaces the file atomically,
which a single-file bind mount doesn't allow (xirc then falls back to a
non-atomic in-place write).

**MariaDB** (optional, instead of the default SQLite):

```bash
docker compose --profile mariadb up -d
```

Starts a `mariadb:11` container (`xirc-db`) alongside `xirc`. Set
`database.driver: mysql` / `database.dsn` in `config.yaml` (or via
`XIRC_DATABASE_DRIVER` / `XIRC_DATABASE_DSN`) pointing at it.

**Client IPs behind Docker's bridge network:** connections reach the
container from the bridge gateway (e.g. `172.18.0.1`), not from the real
client — whether they come straight through the published port or from a
reverse proxy on the host, whose address isn't in the default
`auth.trusted_proxies` (`127.0.0.1/32`, `::1/128`). As long as that's the
case:

- **the login rate limit is shared by everyone** — it counts failures per
  client IP, and every client has the same IP, so a few wrong passwords from
  anyone lock everybody out for a minute;
- **never put the bridge range in `trusted_networks`** (e.g.
  `172.16.0.0/12`): every client, including anyone on the internet reaching
  your proxy, would match it and skip login with that role.

To see real client IPs, either run the container with `network_mode: host`,
or add the host proxy's address as seen by the container (the bridge
gateway) to `auth.trusted_proxies` via `XIRC_AUTH_TRUSTED_PROXIES`, so its
`X-Forwarded-For` is believed.

**Reset a forgotten password:**

```bash
docker exec -it xirc xirc --create-admin <name>
```

---

## Option C — dev machine → target (`make preconfig` + `make deploy`)

Useful when you want a repeatable, scripted deploy from your workstation, or
you're updating a server that's already set up. Requires `make`, `rsync`,
`ssh` on the dev machine, and passwordless `sudo` for the SSH user on the
target.

### Step 1 — Run preconfig

```bash
make preconfig              # uses PROFILE=maxwell by default
make preconfig PROFILE=pi2  # for a second server
```

The script prompts for the following (press Enter to accept the default):

| Setting | Default | Notes |
|---------|---------|-------|
| Target server name (profile) | `maxwell` | Names the profile dir `.maxwell/` |
| SSH host | `maxwell.local` | IP or hostname of the target |
| SSH user | `maxwell` | Must have passwordless sudo |
| Install directory | `/opt/xirc` | Where the binary and data live on target |
| Service OS username | `xirc` | Dedicated unprivileged user |
| HTTP port | `8085` | xirc binds to 127.0.0.1:PORT |
| Downloads directory | `/srv/downloads` | Where DCC files are saved |
| DLNA / media directory | `/srv/dlna/media` | Destination for routed media files |
| Temp directory | `/srv/downloads/.tmp` | In-progress DCC transfers |
| Database driver | `sqlite` | Or `mysql` / `mariadb` |
| SQLite path | `<install_dir>/data/xirc.db` | SQLite only |
| MySQL DSN | `mxirc:changeme@tcp(…)/maxwell_irc` | MySQL/MariaDB only |
| Reverse proxy | `nginx` | `nginx` / `apache` / `none` |
| Mode | `i` (subpath) | `s` = own site (hostname), `i` = subpath snippet for an existing site |
| Hostname *(own site)* | `xirc.local` | Only asked in site mode |
| URL prefix *(subpath)* | `/xirc` | Only asked in subpath mode; also written to `server.prefix` |
| Networks that skip login | *(empty)* | Comma-separated CIDRs; empty = always log in |
| Role for those networks | `admin` | `admin` / `user` |

**Database tables:** xirc uses `CREATE TABLE IF NOT EXISTS` and runs all
migrations automatically on startup. No manual schema setup is needed for
either SQLite or MySQL — just provide a reachable database.

Generated files land in `.<profile>/` (e.g. `.maxwell/`): `config.yaml`,
`xirc.service`, `settings.mk` (read by the Makefile), and — if a proxy was
chosen — `nginx-xirc.conf` / `nginx-xirc-location.conf` /
`apache-xirc.conf` / `apache-xirc-location.conf` (the `-location` suffix is
used for subpath mode). That directory is added to `.gitignore`
automatically.

### Step 2 — First-time target setup

The target needs the `xirc` system user, the install/data directories, and
(if a proxy config was generated) nginx or Apache installed, before the
first `make deploy` can push files to it. The simplest way to get there is
to run the installer once on the target:

```bash
scp scripts/install.sh scripts/proxy-templates.sh maxwell@target:/tmp/
ssh -t maxwell@target "cd /tmp && sudo ./install.sh"

# The installer leaves /opt/xirc owned by xirc:xirc (mode 755), but
# `make deploy` rsyncs as your SSH user — let that user write it:
ssh maxwell@target "sudo usermod -aG xirc maxwell && sudo chmod -R g+w /opt/xirc"
```

(Replace `maxwell` with your SSH user and `/opt/xirc` with the install
directory you chose; the group change applies from the next SSH login.)
Match the paths and port you answer with to what you gave `preconfig` (or
just re-run `preconfig` afterwards to match what you chose in the wizard).
`make deploy` will then overwrite `xirc.service` and any proxy files it
manages with the profile's own copies; the wizard's `config.yaml` is kept
(see [Subsequent deploys](#subsequent-deploys)).

If you'd rather not run the wizard, create the pieces by hand:

```bash
# On the target:
sudo useradd --system --home-dir /opt/xirc --shell /usr/sbin/nologin xirc
sudo mkdir -p /opt/xirc/data /srv/downloads/.tmp /srv/dlna/media
sudo chown -R xirc:xirc /opt/xirc /srv/downloads /srv/dlna/media

# Let the deploy (SSH) user write the install dir and DB, since `make deploy`
# rsyncs as that user, not as xirc:
sudo usermod -aG xirc maxwell   # replace 'maxwell' with your SSH user
sudo chmod g+w /opt/xirc /opt/xirc/data
```

Log out and back in (or `newgrp xirc`) for the group change to take effect.
Install nginx or Apache yourself if you generated a proxy config — `make
deploy` copies the file into place and enables it but doesn't install the
proxy package.

### Step 3 — First deploy

```bash
make deploy PROFILE=maxwell
```

This will:

1. Cross-compile for ARM64 (`make build-pi`).
2. Push the binary to `<install_dir>/xirc` and sync `web/` to
   `<install_dir>/web/`.
3. Install `.maxwell/xirc.service` to `/etc/systemd/system/` and reload
   systemd.
4. Push whichever of `nginx-xirc.conf`, `nginx-xirc-location.conf`,
   `apache-xirc.conf`, `apache-xirc-location.conf` exist in the profile:
   nginx site → `/etc/nginx/sites-available/xirc` (symlinked into
   `sites-enabled/`); nginx subpath → `/etc/nginx/snippets/xirc.conf`
   (include it yourself in your existing `server {}` block, see [Reverse
   proxy](#reverse-proxy)); Apache site → `/etc/apache2/sites-available/
   xirc.conf` (enabled with `a2ensite`); Apache subpath →
   `/etc/apache2/conf-available/xirc.conf` (include it yourself in your
   `<VirtualHost>`). Each case runs `nginx -t` / `apachectl configtest`
   before reloading.
5. Push `.maxwell/config.yaml` to `<install_dir>/config.yaml` — only if the
   target has none yet (`FORCE_CONFIG=1` pushes it anyway).
6. `sudo systemctl restart xirc`.

### Enable start on boot

```bash
ssh maxwell@maxwell.local "sudo systemctl enable xirc"
```

### Verify

```bash
# On the target:
sudo systemctl status xirc
journalctl -u xirc -f

# From another machine on the network (proxy):
curl http://maxwell.local/

# Direct (if the port is reachable without a proxy):
curl http://maxwell.local:8085/
```

### Subsequent deploys

```bash
make deploy PROFILE=maxwell
```

The profile's `config.yaml` is only pushed when the target has none, so
settings saved in the web UI survive deploys. To overwrite the target's file
with the profile's anyway:

```bash
make deploy PROFILE=maxwell FORCE_CONFIG=1
```

To push a different config file instead (always pushed):

```bash
make deploy PROFILE=maxwell PI_CONFIG=/path/to/other.yaml
```

To target a different host without changing the profile:

```bash
make deploy PROFILE=maxwell PI_HOST=10.0.0.5 PI_USER=maxwell
```

### Re-running preconfig

Run `make preconfig PROFILE=maxwell` at any time to update settings and
regenerate the profile. The script overwrites existing files in the profile
directory. Re-deploy afterwards to apply the changes.

### Multiple targets

```bash
make preconfig PROFILE=maxwell   # target at maxwell.local
make preconfig PROFILE=rpi2      # second target

make deploy PROFILE=maxwell
make deploy PROFILE=rpi2
```

Each profile directory is added to `.gitignore` by the preconfig script.

---

## Users & login

`install.sh` creates the first `admin` for you. If no admin exists yet (you
skipped that step, or installed via Docker / `make deploy` without running
`--create-admin`), the first visit to xirc shows an admin-creation form
instead of the login screen and whoever fills it in becomes `admin` — so
create the admin before xirc is reachable from other machines. After that, everyone else logs in at the same URL. Admins manage
further accounts under **Settings → Users** — add a user, change their role,
reset their password, or delete them (the last remaining admin can't be
demoted or deleted).

Two roles exist:

| Role | Can do |
|---|---|
| `user` | Search (live + index), request/cancel/retry downloads, view the download queue, dashboard/stats/storage, read-only file browsing, send channel messages, use the WebSocket live feed |
| `admin` | Everything a `user` can, plus: raw IRC input and connect/disconnect/join; manage servers, realms, and the media library configuration; manage search patterns and unmatched-listing tools; clear the search index; view error logs; manage users; the directory-setup wizard and admin's directory browser; move/rename/delete files and clear/retarget downloads |

All of the admin-only endpoints are enforced server-side (not just hidden in
the UI) — a `user` account hitting one gets a 403.

**Trusted networks** let clients on specific IP ranges skip the login screen
entirely, useful for a home LAN:

```yaml
auth:
  trusted_networks: [192.168.1.0/24]   # empty = login always required
  trusted_role: admin                   # role granted to those clients: admin|user
  trusted_proxies: [127.0.0.1/32, "::1/128"]   # peers whose X-Forwarded-For/-Proto are trusted
```

`trusted_proxies` matters when xirc sits behind a reverse proxy: only
requests whose immediate peer is in this list have their
`X-Forwarded-For`/`X-Forwarded-Proto` headers believed (otherwise a client
could spoof its own IP to gain trusted-network access). `install.sh` and
`preconfig` only ask for `trusted_networks`/`trusted_role`; the default
`trusted_proxies` (loopback only) is right for nginx/Apache running on the
same host — leave it unless xirc is reached through another hop.

**Locked out, or need to reset the admin password?**

```bash
# systemd install:
sudo -u xirc /opt/xirc/xirc --config /opt/xirc/config.yaml --create-admin admin

# Docker:
docker exec -it xirc xirc --create-admin admin
```

Run interactively it prompts for a password twice on the terminal;
non-interactively (e.g. scripted) set `XIRC_ADMIN_PASSWORD` instead. This
creates the user if it doesn't exist, or resets its password and role to
`admin` if it does.

---

## Settings in the web UI

Admins can edit most of `config.yaml` live under **Settings → System**, without
restarting or SSHing in: storage (downloads/temp folders, minimum free
space), max parallel downloads, maintenance (search-result retention, index
cap, how often it runs), and login (trusted networks, their role, trusted
proxies — the same fields as [Users & login](#users--login) above). Changes
are validated, written to `config.yaml`, and applied to the running server
immediately. The first save writes every editable key into `config.yaml`
(defaults become explicit values) and leaves the file owned by the user xirc
runs as, with its mode kept.

`server.*` and `database.*` (including the DSN, never exposed by any API)
and the media root aren't editable here — the media root lives under
**Settings → Library**; the rest only change by editing `config.yaml` and
restarting.

A field set by an `XIRC_*` environment variable (see
[Option B](#option-b--docker) and
[Upgrading](#upgrading-from-03-or-earlier) for the naming) shows disabled in the UI — the env
var always wins on the next restart, so the page won't let you override it
there. If `config.yaml` isn't writable by the user xirc runs as, the System
tab shows why and disables Save; see [Permissions](#permissions) below to
fix ownership.

After the directory-setup wizard runs on first start, restart xirc before
editing System settings, so the running server and the System tab see the
wizard's directories.

With `make deploy`, the target's `config.yaml` belongs to the UI after the
first deploy; `make deploy FORCE_CONFIG=1` pushes the profile's file again
(overwriting what was saved in the UI).

---

## Reverse proxy

The systemd install (`install.sh`, `preconfig`) binds xirc to `127.0.0.1` —
put a reverse proxy in front to reach it from other devices. In Docker xirc
listens on `0.0.0.0` inside the container, and compose publishes the port
on `127.0.0.1` by default (see [Option B](#option-b--docker)). Both nginx and
Apache configs come from the same templates (`scripts/proxy-templates.sh`),
so an install.sh, preconfig-generated, or hand-rendered config all behave
the same way.

Render either without installing anything:

```bash
scripts/install.sh --print-proxy nginx  --server-name xirc.example.com
scripts/install.sh --print-proxy apache --prefix /xirc --port 8085
```

**Own site vs. subpath:** an own-site config is a full server block/vhost
for a dedicated hostname. A subpath config is a snippet (`location`
block for nginx, plain directives for Apache) to `include` inside a
`server {}` / `<VirtualHost>` block you already have — set
`server.prefix` in xirc's `config.yaml` to the same prefix you rendered
with, or nothing works.

**Apache** needs these modules enabled (`install.sh` and `make deploy` run
this for you): `a2enmod proxy proxy_http proxy_wstunnel rewrite headers`.

**WebSocket:** both templates upgrade `<prefix>/ws` and forward `Host`
(as the full `host:port`, not just the hostname — xirc's WebSocket origin
check needs the port), `X-Forwarded-For`, and `X-Forwarded-Proto`.

**HTTPS:** the login cookie is marked `Secure` once the effective scheme is
https, so browsers will only send it back over HTTPS at that point — put TLS
at the proxy (`sudo certbot --nginx` or `sudo certbot --apache` after the
plain-HTTP config works) rather than in xirc itself.

---

## Permissions

xirc keeps running even when a directory it wants isn't writable (or
doesn't exist) — only a missing SQLite database directory is fatal at
startup. Everything else degrades feature-by-feature with a human-readable
reason instead of crashing or silently failing:

| If this isn't writable | This happens |
|---|---|
| Downloads or temp dir | New download requests are refused (503) with the reason; downloads already in the queue still start and then fail with the write error |
| Media dir (or a category dir under it) | Finished downloads stay in the downloads directory instead of being sorted, with the reason recorded on the download |
| Media dir (not readable) | Library browsing is hidden |
| A file-manager root (not writable) | Move/rename/delete/create-folder are disabled for it, with the reason as a tooltip — the root itself still shows in the destination list |
| A file-manager root (not readable) | Not hidden from the list; opening it fails with a 403 and the reason |
| Log directory | Channel logging turns off (one warning logged) |
| `categories.yaml`'s directory | Library settings become read-only in the UI |
| `config.yaml`'s directory | System settings can't be saved (Save disabled, with the reason) — xirc writes a temp file there and renames it over `config.yaml` |

**Where to see it:** any disabled control shows the reason as a tooltip.
Admins additionally see a banner listing every failing capability with a fix
hint and a "Recheck" button (capabilities are also rechecked automatically
every 5 minutes, or on demand via `POST /api/capabilities/recheck`).

**Fix commands** (also what the banner suggests):

```bash
# systemd install — re-own the directory for the xirc user:
sudo chown -R xirc:xirc <dir>

# Docker — the container runs as PUID:PGID from .env; either make <dir> on
# the host writable by that uid/gid:
sudo chown -R 1000:1000 <host dir>   # your PUID:PGID
# …or set PUID/PGID in .env to its current owner (`stat -c '%u:%g' <host dir>`)
# and recreate the container:
docker compose up -d
```

In the container `<dir>` is `/data`, `/downloads` or `/media`: the host
directory behind it is `deploy/data`, `DOWNLOADS_DIR` or `MEDIA_DIR`.

---

## Upgrading from 0.3 or earlier

- **Login is now required.** After the upgrade nobody is logged in: the
  first visit creates the admin, unless you create it beforehand with
  `xirc --create-admin <name>` (see [Users & login](#users--login)). Do that
  before the new version is reachable from other machines.
- **Update the reverse-proxy config first**, then add `auth.trusted_networks`.
  The old templates sent `Host $host` and no `X-Forwarded-For` /
  `X-Forwarded-Proto` on the `/ws` location, so xirc sees every WebSocket
  coming from the proxy itself: trusted-network clients then get a 401 on
  `/ws` and the live feed dies. Regenerate the config (`make preconfig` +
  `make deploy`, re-run `install.sh`, or `install.sh --print-proxy …`), or
  patch the `/ws` block by hand to send `Host $http_host`,
  `X-Forwarded-For $proxy_add_x_forwarded_for` and
  `X-Forwarded-Proto $scheme` (Apache: `ProxyPreserveHost On` plus
  `RequestHeader set X-Forwarded-Proto expr=%{REQUEST_SCHEME}`).
- **Environment overrides were renamed.** Names now come from the YAML keys:
  `XIRC_<SECTION>_<YAML_KEY_UPPER>`. Keys with an underscore changed, e.g.
  `XIRC_STORAGE_DOWNLOADSDIR` → `XIRC_STORAGE_DOWNLOADS_DIR`,
  `XIRC_STORAGE_MEDIADIR` → `XIRC_STORAGE_MEDIA_DIR`,
  `XIRC_STORAGE_TEMPDIR` → `XIRC_STORAGE_TEMP_DIR`. Single-word keys such as
  `XIRC_SERVER_PORT` are unchanged. Old names are silently ignored.
- **Docker layout changed.** The old compose file mounted the repo's
  `../data` at `/app/data` and the whole of `/srv` at `/srv`, and baked a
  `config.yaml` into the image. The new one (service and container `xirc`,
  image `ghcr.io/realdtx/xirc`) has no baked config and mounts
  `deploy/data` → `/data` (database `xirc.db`, optional `config.yaml`,
  `categories.yaml`, logs), `DOWNLOADS_DIR` → `/downloads` and `MEDIA_DIR` →
  `/media`, and runs as `PUID:PGID`. To carry over your data, copy the old
  SQLite file to `deploy/data/xirc.db` (and your `categories.yaml`, if any,
  to `deploy/data/`), `chown` everything to `PUID:PGID`, and set `media_root` in
  `categories.yaml` to `/media`. Remove the old container first
  (`docker rm -f maxwell-irc`) so both don't run at once.
