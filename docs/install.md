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

All three end at the same place: open the printed URL and the first visit
creates the admin account (see [Users & login](#users--login)).

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
2. **Finds a binary** — `./xirc` next to the script or in the repo root, else
   builds from source if a Go toolchain matching `go.mod`'s version is on
   `PATH`, else downloads the newest release asset for your architecture
   (`amd64`/`arm64`/`armv7`) from GitHub, else stops with instructions.
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
6. **Writes `config.yaml` and `/etc/systemd/system/xirc.service`**, then
   `systemctl enable --now xirc`. If either file already exists and differs,
   it asks before overwriting; a replaced proxy file is instead backed up as
   `<file>.bak.<timestamp>`.
7. **Reverse proxy** — detects installed nginx/Apache and offers to configure
   one or both, as either a standalone site (own hostname) or a subpath of an
   existing site. See [Reverse proxy](#reverse-proxy).
8. **Prints a summary** — the URL to open, a reminder that the first visit
   creates the admin account, and (if a proxy was configured) the `certbot`
   command for HTTPS.

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
docker compose up -d
```

`.env` (see `deploy/.env.example`):

| Variable | Purpose |
|---|---|
| `DOWNLOADS_DIR`, `MEDIA_DIR` | Host directories mounted into the container; must already exist and be owned by `PUID:PGID` |
| `PUID`, `PGID` | Container user's uid/gid — set to `id -u` / `id -g` of the host directories' owner |
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
anything else.

**MariaDB** (optional, instead of the default SQLite):

```bash
docker compose --profile mariadb up -d
```

Starts a `mariadb:11` container (`xirc-db`) alongside `xirc`. Set
`database.driver: mysql` / `database.dsn` in `config.yaml` (or via
`XIRC_DATABASE_DRIVER` / `XIRC_DATABASE_DSN`) pointing at it.

**Trusted networks caveat:** by default Docker's bridge network NATs client
connections, so xirc only ever sees the container gateway's IP — CIDR
matching against `auth.trusted_networks` won't identify real clients, and a
reverse proxy on the host won't be trusted either since its address isn't in
the default `auth.trusted_proxies` (`127.0.0.1/32`, `::1/128`). To make
`trusted_networks` or a proxy's `X-Forwarded-For` work, either run the
container with `network_mode: host`, or add the proxy's address (as seen by
the container) to `auth.trusted_proxies` via `XIRC_AUTH_TRUSTED_PROXIES`.

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
| Auto-extract tar archives | `n` | Unpacks downloaded archives and routes the contents |
| Delete archive after extraction | `y` | Only asked if auto-extract is enabled |
| Database driver | `sqlite` | Or `mysql` / `mariadb` |
| SQLite path | `<install_dir>/data/xirc.db` | SQLite only |
| MySQL DSN | `mxirc:changeme@tcp(…)/maxwell_irc` | MySQL/MariaDB only |
| Passive DCC | `n` | Enable if the target is behind NAT |
| External IP | *(empty)* | Only asked if passive DCC is enabled |
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
scp scripts/install.sh scripts/proxy-templates.sh xirc-user@target:/tmp/
ssh xirc-user@target "cd /tmp && sudo ./install.sh"
```

Match the paths and port you answer with to what you gave `preconfig` (or
just re-run `preconfig` afterwards to match what you chose in the wizard).
`make deploy` will then overwrite `config.yaml`, `xirc.service` and any
proxy files it manages with the profile's own copies.

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
5. Push `.maxwell/config.yaml` to `<install_dir>/config.yaml`.
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

The profile's `config.yaml` is always pushed. To push a different config file
instead:

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

The first visit to xirc (no proxy needed) shows an admin-creation form
instead of the login screen; the account you create there becomes the first
`admin`. After that, everyone else logs in at the same URL. Admins manage
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
  trusted_proxies: [127.0.0.1/32, ::1/128]   # peers whose X-Forwarded-For/-Proto are trusted
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

## Reverse proxy

xirc always binds to `127.0.0.1` — put a reverse proxy in front to reach it
from other devices, and never expose the raw port directly. Both nginx and
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
| Downloads or temp dir | New downloads are refused (503) with the reason; the queue stops starting new transfers |
| Media dir (or a category dir under it) | Finished downloads stay in the downloads directory instead of being sorted, with the reason recorded on the download |
| Media dir (not readable) | Library browsing is hidden |
| A file-manager root (not writable) | Move/rename/delete/create-folder are disabled for it, with the reason as a tooltip — the root itself still shows in the destination list |
| A file-manager root (not readable) | Not hidden from the list; opening it fails with a 403 and the reason |
| Log directory | Channel logging turns off (one warning logged) |
| `categories.yaml`'s directory | Library settings become read-only in the UI |

**Where to see it:** any disabled control shows the reason as a tooltip.
Admins additionally see a banner listing every failing capability with a fix
hint and a "Recheck" button (capabilities are also rechecked automatically
every 5 minutes, or on demand via `POST /api/capabilities/recheck`).

**Fix commands** (also what the banner suggests):

```bash
# systemd install — re-own the directory for the xirc user:
sudo chown -R xirc:xirc <dir>

# Docker — set PUID/PGID (in .env) to the uid/gid that owns <dir> on the host,
# then recreate the container:
docker compose up -d
```
