# maXwell IRC

A self-hosted XDCC web client for downloading files via IRC bots, designed to run on Raspberry Pi or any Linux server.

**User guide:** [docs/guide.md](docs/guide.md) — also available in the app under *Help*.

## Features

- Web-based UI — no client installation required
- XDCC bot search — send search commands to IRC bots and parse pack listings
- Download queue — concurrent DCC file transfers with progress tracking
- File routing rules — direct downloads to different directories by filename pattern
- Auto-extract — optionally unpack tar archives after download with automatic routing
- DCC passive mode — works behind NAT without port forwarding
- Multi-server support — connect to multiple IRC servers and channels
- Multi-user login — session-based auth with admin/user roles; trusted networks can skip login
- Preconfig + deploy workflow — repeatable deployments via rsync and systemd
- Nginx and Apache integration — an install wizard configures either automatically, or generate vhost/subpath configs yourself
- Docker support — docker compose for local development and testing
- Post-download hooks — browser notifications for downloads and events
- Auto-detect download channel — reads IRC topic on join to find XDCC bot

## Install

**On a Linux server (Debian/Ubuntu/Raspberry Pi OS)**
```bash
git clone https://github.com/RealDtx/maxwell-xirc.git && cd maxwell-xirc
sudo scripts/install.sh
```
The wizard asks for download/media/database paths, checks permissions,
creates the admin account, installs a systemd service and optionally
configures nginx or Apache. Open the printed URL and log in.

**Docker**
```bash
cd deploy && cp .env.example .env   # set DOWNLOADS_DIR, MEDIA_DIR, PUID/PGID
mkdir -p data && sudo chown 1000:1000 data   # your PUID:PGID
docker compose up -d
docker exec -it xirc xirc --create-admin <name>   # also resets a password
```
The port is published on `127.0.0.1` only; see [docs/install.md](docs/install.md#option-b--docker) to expose it.

**From a dev machine to a Pi** — `make preconfig`, then `make deploy`; see
[docs/install.md](docs/install.md).

Only need a reverse-proxy config? `scripts/install.sh --print-proxy apache --server-name xirc.example.com`

## Requirements

**Development machine:**
- Go (see `go.mod` for the minimum version)
- make
- rsync
- ssh (for remote deployment)

**Target device:**
- Raspberry Pi OS 64-bit (Bookworm or later) or any Linux server
- nginx or Apache (recommended, optional — `scripts/install.sh` can configure either)

## Configuration

Copy `config.example.yaml` to `config.yaml` and customize:

```bash
cp config.example.yaml config.yaml
```

Configuration can also be overridden via environment variables: `XIRC_<SECTION>_<KEY>` using the
yaml key, e.g. `XIRC_SERVER_PORT=9000` or `XIRC_STORAGE_DOWNLOADS_DIR=/downloads`. List values are
comma-separated, e.g. `XIRC_AUTH_TRUSTED_NETWORKS=192.168.0.0/16,10.0.0.0/8`.

The preconfig wizard (`make preconfig`) generates a complete configuration automatically.

## Building

```bash
make build          # Build for current platform
make test           # Run all tests
make build-pi       # Cross-compile for ARM64 (Raspberry Pi 4)
make docker         # Build Docker image
make docker-up      # Start docker compose (SQLite)
```

## Development

The project uses:
- **Backend:** Go (`github.com/RealDtx/maxwell-irc`)
- **Frontend:** Alpine.js + vanilla JavaScript (no build step required)
- **Database:** SQLite (default) or MySQL/MariaDB
- All database migrations run automatically

## License

MIT — see LICENSE
