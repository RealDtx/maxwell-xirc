# maXwell IRC

A self-hosted XDCC web client for downloading files via IRC bots, designed to run on Raspberry Pi or any Linux server.

## Features

- Web-based UI — no client installation required
- XDCC bot search — send search commands to IRC bots and parse pack listings
- Download queue — concurrent DCC file transfers with progress tracking
- File routing rules — direct downloads to different directories by filename pattern
- Auto-extract — optionally unpack tar archives after download with automatic routing
- DCC passive mode — works behind NAT without port forwarding
- Multi-server support — connect to multiple IRC servers and channels
- Preconfig + deploy workflow — repeatable deployments via rsync and systemd
- Nginx integration — generates vhost or subpath location block configs
- Docker support — docker compose for local development and testing
- Post-download hooks — notifications with quiet hours support
- Auto-detect download channel — reads IRC topic on join to find XDCC bot

## Quick Start

1. Clone the repository:
   ```bash
   git clone https://github.com/RealDtx/maxwell-irc.git
   cd maxwell-irc
   ```

2. Generate configuration:
   ```bash
   make preconfig PROFILE=pi
   ```

3. Perform first-time Pi setup (create user, directories, nginx):
   ```bash
   # See docs/install.md for detailed instructions
   ```

4. Deploy to Raspberry Pi:
   ```bash
   make deploy PROFILE=pi
   ```

For full installation instructions, see [docs/install.md](docs/install.md).

## Requirements

**Development machine:**
- Go 1.21+
- make
- rsync
- ssh (for remote deployment)

**Target device:**
- Raspberry Pi OS 64-bit (Bookworm or later) or any Linux server
- nginx (recommended)

## Configuration

Copy `config.example.yaml` to `config.yaml` and customize:

```bash
cp config.example.yaml config.yaml
```

Configuration can also be overridden via environment variables (e.g., `XIRC_SERVER_PORT=9000`).

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
