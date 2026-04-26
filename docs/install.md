# maXwell IRC Installation Guide

This guide covers deploying maXwell IRC to a Raspberry Pi (or any Linux server) for the first time.

---

## Overview

The workflow has three phases:

1. **Preconfig** (dev machine) — interactive script that generates a named profile directory (e.g. `.maxwell/`) containing `config.yaml`, the systemd unit, and optionally the nginx config.
2. **First-time Pi setup** — create the system user, directories, and permissions on the target server (one-time, done over SSH).
3. **Deploy** (`make deploy`) — cross-compiles the binary, pushes all generated files, restarts the service.

---

## Prerequisites

**Dev machine:** `make`, `rsync`, `ssh`, Go (the Makefile uses `~/go-install/go/bin/go`).

**Target server:** Raspberry Pi OS 64-bit (Bookworm+), SSH access as a user with passwordless `sudo`.

```bash
# Install nginx on the Pi if you want a reverse proxy (recommended)
sudo apt update && sudo apt install -y nginx
```

---

## Step 1 — Run preconfig

```bash
make preconfig              # uses PROFILE=maxwell by default
make preconfig PROFILE=pi2  # for a second server
```

The script prompts for the following (press Enter to accept the default):

| Setting | Default | Notes |
|---------|---------|-------|
| Target server name (profile) | `maxwell` | Names the profile dir `.maxwell/` |
| SSH host | `192.168.20.2` | IP or hostname of the Pi |
| SSH user | `pi` | Must have passwordless sudo |
| Install directory | `/opt/maxwell-irc` | Where the binary and data live on target |
| Service OS username | `maxwell-irc` | Dedicated unprivileged user |
| HTTP port | `8085` | maxwell-irc binds to 127.0.0.1:PORT |
| Downloads directory | `/srv/downloads` | Where DCC files are saved |
| DLNA / media directory | `/srv/dlna/media` | Destination for routed media files |
| Temp directory | `/srv/downloads/.tmp` | In-progress DCC transfers |
| Database driver | `sqlite` | Or `mysql` / `mariadb` |
| SQLite path | `<install_dir>/data/maxwell-irc.db` | SQLite only |
| MySQL DSN | `mxirc:…@tcp(…)/maxwell_irc` | MySQL/MariaDB only |
| Passive DCC | `n` | Enable if the Pi is behind NAT |
| Use nginx | `y` | Generates an nginx proxy config |
| Nginx mode | `i` (subpath) | `s` = standalone vhost, `i` = location blocks for existing server |
| URL prefix *(subpath mode)* | `/mxirc` | Path prefix, e.g. `http://local/mxirc` |
| Nginx server_name *(standalone)* | `maxwell-irc.local` | Hostname for standalone vhost |
| Nginx listen port *(standalone)* | `80` | Port for standalone vhost |

**Database tables:** maXwell IRC uses `CREATE TABLE IF NOT EXISTS` and runs all migrations automatically on startup. No manual schema setup is needed for either SQLite or MySQL — just provide a reachable database.

**No nginx:** If you opt out, maxwell-irc binds to `127.0.0.1` (localhost only) — it is unreachable from the network until you add a reverse proxy. Never expose the raw port directly.

**Subpath mode (recommended when you already have nginx running):** The script generates `nginx-maxwell-irc-location.conf`, a snippet with `location /mxirc/` and `location /mxirc/ws` blocks. After deploy copies it to `/etc/nginx/snippets/maxwell-irc.conf`, add one line to your existing nginx `server` block:
```nginx
include /etc/nginx/snippets/maxwell-irc.conf;
```
Then reload nginx: `sudo nginx -t && sudo systemctl reload nginx`.

**Standalone mode:** A full `nginx-maxwell-irc.conf` server block is generated and deployed to `sites-available/`. The `sites-enabled/` symlink is created automatically by `make deploy`.

Generated files are written to `.<profile>/` (e.g. `.maxwell/`) and that directory is added to `.gitignore` automatically.

---

## Step 2 — First-time Pi setup

SSH to the Pi and run these once. Adjust paths if you chose non-default settings.

### 2a — Create system user

```bash
sudo groupadd --system maxwell-irc
sudo useradd --system --gid maxwell-irc --home-dir /opt/maxwell-irc --shell /usr/sbin/nologin maxwell-irc
```

### 2b — Create directory structure

```bash
sudo mkdir -p /opt/maxwell-irc/data
sudo mkdir -p /srv/downloads/.tmp
sudo mkdir -p /srv/dlna/media

sudo chown -R maxwell-irc:maxwell-irc /opt/maxwell-irc
sudo chown -R maxwell-irc:maxwell-irc /srv/downloads
sudo chown -R maxwell-irc:maxwell-irc /srv/dlna/media

sudo chmod 750 /opt/maxwell-irc/data
```

### 2c — Allow the deploy user to write to the install dir

`make deploy` runs rsync as your SSH user, so it needs write access:

```bash
sudo usermod -aG maxwell-irc pi       # replace 'pi' with your SSH user if different
sudo chmod g+w /opt/maxwell-irc
sudo chmod g+w /opt/maxwell-irc/data
```

Log out and back in (or run `newgrp maxwell-irc`) for the group change to take effect.

### 2d — Set up nginx

**Standalone mode:** `make deploy` copies the config to `sites-available/` and creates the `sites-enabled/` symlink automatically. Nothing to do manually.

**Subpath mode:** `make deploy` copies the snippet to `/etc/nginx/snippets/maxwell-irc.conf`. Add one line inside your existing nginx `server` block and reload:

```nginx
include /etc/nginx/snippets/maxwell-irc.conf;
```

```bash
sudo nginx -t && sudo systemctl reload nginx
```

---

## Step 3 — First deploy

```bash
make deploy PROFILE=maxwell
```

This will:
1. Cross-compile for ARM64
2. Push `maxwell-irc-arm64` to `<install_dir>/maxwell-irc`
3. Install `.maxwell/maxwell-irc.service` to `/etc/systemd/system/` and reload systemd
4. For standalone nginx: install config to `sites-available/`, create `sites-enabled/` symlink, reload nginx
   For subpath nginx: copy snippet to `/etc/nginx/snippets/maxwell-irc.conf`, reload nginx
5. Push `.maxwell/config.yaml` to `<install_dir>/config.yaml`
6. `sudo systemctl restart maxwell-irc`

### Enable start on boot

```bash
ssh pi@192.168.20.2 "sudo systemctl enable maxwell-irc"
```

### Verify

```bash
# On the Pi:
sudo systemctl status maxwell-irc
journalctl -u maxwell-irc -f

# From another machine on the network (nginx):
curl http://192.168.20.2/

# Direct (if port is reachable without nginx):
curl http://192.168.20.2:8085/
```

---

## Subsequent deploys

```bash
make deploy PROFILE=maxwell
```

The profile's `config.yaml` is always pushed. To push a different config file instead:

```bash
make deploy PROFILE=maxwell PI_CONFIG=/path/to/other.yaml
```

To target a different host without changing the profile:

```bash
make deploy PROFILE=maxwell PI_HOST=10.0.0.5 PI_USER=dtx
```

---

## Re-running preconfig

Run `make preconfig PROFILE=maxwell` at any time to update settings and regenerate the profile. The script overwrites existing files in the profile directory. Re-deploy afterwards to apply the changes.

---

## Multiple targets

```bash
make preconfig PROFILE=maxwell   # Pi at 192.168.20.2
make preconfig PROFILE=rpi2      # second Pi

make deploy PROFILE=maxwell
make deploy PROFILE=rpi2
```

Each profile directory is added to `.gitignore` by the preconfig script.
