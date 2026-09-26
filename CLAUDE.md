# Agent Instructions for maXwell IRC

## Versioning

- **Always use the latest stable version** of any runtime, language, tool, or library dependency.
- When installing anything (Go, Node, Python, npm packages, Go modules, etc.), check for the current latest release and install that version.
- If an outdated version is encountered during work, upgrade it proactively before continuing.

## Go

- Go runtime is installed at `~/go-install/go` (currently 1.27.1). The system Go at `/usr/bin/go` is 1.13 and must **not** be used.
- **Always build via `make`** — the Makefile points to the correct Go binary automatically:
  - Build: `make build`
  - Test: `make test`
  - Direct invocation: `~/go-install/go/bin/go build -o maxwell-irc .`
- After dependency changes: `~/go-install/go/bin/go mod tidy`

## Server

- The maxwell-irc server runs on port **8085**.
- Do **not** start the server automatically — the user starts it manually (it connects to external IRC servers).
- DB: `./data/xirc.db` (SQLite, per `config.yaml`). Running the binary without `config.yaml` falls back to the default `./data/maxwell-irc.db` — if a stray file of that name appears, something ran without the config.
- Channel message logs: `./data/logs/<serverID>/#channel.log` — useful for validating parse patterns against real traffic.
