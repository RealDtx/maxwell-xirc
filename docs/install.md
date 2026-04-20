# xirc Installation Guide

This guide covers deploying xirc to a Raspberry Pi (or any Linux server) for the first time.

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
| Install directory | `/opt/xirc` | Where the binary and data live on target |
| Service OS username | `xirc` | Dedicated unprivileged user |
| HTTP port | `8085` | xirc binds to 127.0.0.1:PORT |
| Downloads directory | `/srv/downloads` | Where DCC files are saved |
| DLNA / media directory | `/srv/dlna/media` | Destination for routed media files |
| Temp directory | `/srv/downloads/.tmp` | In-progress DCC transfers |
| Database driver | `sqlite` | Or `mysql` / `mariadb` |
| SQLite path | `<install_dir>/data/xirc.db` | SQLite only |
| MySQL DSN | `xirc:…@tcp(…)/xirc` | MySQL/MariaDB only |
| Passive DCC | `n` | Enable if the Pi is behind NAT |
| Use nginx | `y` | Generates a ready-to-use nginx vhost config |
| Nginx server_name | `xirc.local` | Hostname for the nginx vhost |

**Database tables:** xirc uses `CREATE TABLE IF NOT EXISTS` and runs all migrations automatically on startup. No manual schema setup is needed for either SQLite or MySQL — just provide a reachable database.

**No nginx:** If you opt out, the script prints a security warning. Do not expose the raw HTTP port to the internet — you must place xirc behind a TLS-terminating reverse proxy before any public access.

Generated files are written to `.<profile>/` (e.g. `.maxwell/`) and that directory is added to `.gitignore` automatically.

---

## Step 2 — First-time Pi setup

SSH to the Pi and run these once. Adjust paths if you chose non-default settings.

### 2a — Create system user

```bash
sudo groupadd --system xirc
sudo useradd --system --gid xirc --home-dir /opt/xirc --shell /usr/sbin/nologin xirc
```

### 2b — Create directory structure

```bash
sudo mkdir -p /opt/xirc/data
sudo mkdir -p /srv/downloads/.tmp
sudo mkdir -p /srv/dlna/media

sudo chown -R xirc:xirc /opt/xirc
sudo chown -R xirc:xirc /srv/downloads
sudo chown -R xirc:xirc /srv/dlna/media

sudo chmod 750 /opt/xirc/data
```

### 2c — Allow the deploy user to write to the install dir

`make deploy` runs rsync as your SSH user, so it needs write access:

```bash
sudo usermod -aG xirc pi       # replace 'pi' with your SSH user if different
sudo chmod g+w /opt/xirc
sudo chmod g+w /opt/xirc/data
```

Log out and back in (or run `newgrp xirc`) for the group change to take effect.

### 2d — Enable the nginx site (if you chose nginx)

```bash
sudo ln -s /etc/nginx/sites-available/xirc /etc/nginx/sites-enabled/xirc
# Reload after deploy installs the config:
# sudo nginx -t && sudo systemctl reload nginx
```

`make deploy` installs the nginx config and reloads nginx automatically.

---

## Step 3 — First deploy

```bash
make deploy PROFILE=maxwell
```

This will:
1. Cross-compile for ARM64
2. Push `xirc-arm64` to `<install_dir>/xirc`
3. Install `.maxwell/xirc.service` to `/etc/systemd/system/` and reload systemd
4. Install `.maxwell/nginx-xirc.conf` to `/etc/nginx/sites-available/xirc` and reload nginx (if generated)
5. Push `.maxwell/config.yaml` to `<install_dir>/config.yaml`
6. `sudo systemctl restart xirc`

### Enable start on boot

```bash
ssh pi@192.168.20.2 "sudo systemctl enable xirc"
```

### Verify

```bash
# On the Pi:
sudo systemctl status xirc
journalctl -u xirc -f

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
