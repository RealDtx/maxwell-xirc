# xirc Installation Guide — Raspberry Pi (maxwell)

This guide covers a first-time bare-metal install of xirc on a Raspberry Pi 4 running
Raspberry Pi OS 64-bit, served through nginx at `http://192.168.20.2/` (hostname `maxwell`).

---

## Prerequisites

- Raspberry Pi OS Lite 64-bit (Bookworm or later)
- SSH access to the Pi as the `pi` user with passwordless `sudo`
- nginx installed on the Pi
- Your dev machine has `make`, `rsync`, and `ssh` available

Install nginx if not already present:

```bash
sudo apt update
sudo apt install -y nginx
```

---

## 1. Create system user and group

xirc runs as a dedicated unprivileged user. Create it before anything else:

```bash
sudo groupadd --system xirc
sudo useradd --system --gid xirc --home-dir /opt/xirc --shell /usr/sbin/nologin xirc
```

---

## 2. Create directories

```bash
# Binary and config
sudo mkdir -p /opt/xirc
sudo chown xirc:xirc /opt/xirc

# SQLite database (mode 750 — only xirc user/group can read)
sudo mkdir -p /opt/xirc/data
sudo chown xirc:xirc /opt/xirc/data
sudo chmod 750 /opt/xirc/data

# XDCC download landing zone
sudo mkdir -p /srv/downloads
sudo chown xirc:xirc /srv/downloads

# DLNA/media directory
sudo mkdir -p /srv/dlna/media
sudo chown xirc:xirc /srv/dlna/media
```

---

## 3. Grant deploy user (pi) write access to /opt/xirc

rsync needs to write the binary and config as the `pi` user, so add `pi` to the `xirc`
group and make `/opt/xirc` group-writable:

```bash
sudo usermod -aG xirc pi
sudo chmod g+w /opt/xirc
```

Log out and back in (or run `newgrp xirc`) for the group change to take effect in your
current session.

---

## 4. Install the systemd service

```bash
sudo cp /path/to/repo/deploy/xirc.service /etc/systemd/system/xirc.service
sudo systemctl daemon-reload
sudo systemctl enable xirc
```

The service file runs xirc as `User=xirc`, starts it from `/opt/xirc`, and grants
write access to `/opt/xirc/data`, `/srv/dlna/media`, and `/srv/downloads` via
`ReadWritePaths`.

---

## 5. Install the nginx site

```bash
sudo cp /path/to/repo/deploy/nginx-xirc.conf /etc/nginx/sites-available/xirc
sudo ln -s /etc/nginx/sites-available/xirc /etc/nginx/sites-enabled/xirc
sudo nginx -t
sudo systemctl reload nginx
```

The nginx config proxies HTTP on port 80 to xirc at `127.0.0.1:8085` and handles
WebSocket upgrades at `/ws`. Access is restricted to private networks:
`192.168.0.0/16`, `10.0.0.0/8`, and `172.16.0.0/12`.

---

## 6. Create /opt/xirc/config.yaml

Create the production config on the Pi at `/opt/xirc/config.yaml`:

```yaml
server:
  host: 127.0.0.1
  port: 8085

database:
  driver: sqlite
  path: /opt/xirc/data/xirc.db

storage:
  media_dir: /srv/dlna/media
  downloads_dir: /srv/downloads
  temp_dir: /srv/downloads/.tmp
  min_free_space: 1GB
  critical_free_space: 500MB

dcc:
  passive_enabled: false
  passive_ports: "30000-30010"
  external_ip: ""

downloads:
  max_concurrent: 3

notifications:
  quiet_hours_start: ""
  quiet_hours_end: ""
```

If you need DCC passive mode (Pi is behind NAT), set `passive_enabled: true` and
`external_ip` to your external IP or DDNS hostname, and forward the passive port range
in your router.

---

## 7. First deploy from the dev machine

From the repository root on your dev machine, run:

```bash
make deploy PI_CONFIG=/path/to/prod-config.yaml
```

This will:

1. Cross-compile an ARM64 binary (`xirc-arm64`)
2. rsync the binary to `pi@192.168.20.2:/opt/xirc/xirc`
3. Deploy and reload the systemd service unit
4. Deploy and reload the nginx site config
5. rsync `PI_CONFIG` to `/opt/xirc/config.yaml`
6. Restart the xirc service

For subsequent deploys without changing the config, omit `PI_CONFIG`:

```bash
make deploy
```

To target a different host or user:

```bash
make deploy PI_HOST=192.168.20.5 PI_USER=admin
```

---

## 8. Verify the deployment

**On the Pi** — check the service is running and listening:

```bash
sudo systemctl status xirc
curl http://localhost:8085/
```

The `systemctl status` output should show `active (running)`. The curl should return
the xirc HTML page.

**From another machine on the network** — verify nginx proxying:

```bash
curl http://192.168.20.2/
```

**Browser check** — open `http://maxwell/` or `http://192.168.20.2/` in your browser.
Open the browser developer tools (F12), go to the Network tab, filter by WS, and
confirm a WebSocket connection to `/ws` is established with status 101 Switching
Protocols.

---

## Troubleshooting

**Service won't start** — check logs:
```bash
sudo journalctl -u xirc -n 50 --no-pager
```

**nginx 502 Bad Gateway** — xirc is not running or not listening on port 8085. Check
`systemctl status xirc` and the journal.

**Permission denied on /opt/xirc/xirc (rsync)** — make sure `pi` is in the `xirc`
group and `/opt/xirc` is `g+w`. Verify with `groups pi` and `ls -ld /opt/xirc`.

**WebSocket connection fails** — confirm nginx has the `/ws` location block with
`proxy_http_version 1.1` and the `Upgrade`/`Connection` headers. The config in
`deploy/nginx-xirc.conf` already includes this.
