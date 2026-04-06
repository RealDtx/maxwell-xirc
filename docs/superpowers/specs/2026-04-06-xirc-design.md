# xirc — XDCC IRC Web Client

## Overview

**xirc** is a web-controlled XDCC IRC client built as a single Go binary. It connects to multiple IRC servers, searches for files via configurable channel commands, manages a download queue with DCC transfers, and routes completed files to the appropriate directories based on file type. A browser-based UI provides structured search/download management alongside a full IRC console per channel.

**Target environment:** Raspberry Pi 4 (or similar), behind nginx reverse proxy, accessible only from the internal network or via VPN.

---

## Architecture

Single Go binary acting as both IRC client and HTTP/WebSocket server.

```
┌─────────────────────────────────────────────────┐
│                    nginx                         │
│            (reverse proxy, internal only)        │
└──────────────┬──────────────────────────────────┘
               │ HTTP + WebSocket
               ▼
┌─────────────────────────────────────────────────┐
│                 xirc (Go binary)                 │
│                                                  │
│  ┌──────────┐  ┌───────────┐  ┌──────────────┐  │
│  │ Web/API  │  │    IRC    │  │   DCC        │  │
│  │ Server   │◄─►  Manager  │◄─► Transfer     │  │
│  │ (HTTP+WS)│  │           │  │  Engine      │  │
│  └────┬─────┘  └─────┬─────┘  └──────┬───────┘  │
│       │              │               │           │
│       ▼              ▼               ▼           │
│  ┌──────────────────────────────────────────┐    │
│  │           Core Services                   │    │
│  │  • Search Parser    • Download Queue      │    │
│  │  • File Router      • Notification Bus    │    │
│  │  • Disk Monitor     • Post-Hook Runner    │    │
│  └──────────────┬───────────────────────────┘    │
│                 │                                 │
│                 ▼                                 │
│          ┌─────────────┐                         │
│          │  SQLite or   │                         │
│          │  MariaDB     │                         │
│          └─────────────┘                         │
└─────────────────────────────────────────────────┘
               │                    │
               ▼                    ▼
        ┌────────────┐     ┌──────────────┐
        │ DLNA Dir   │     │ Downloads Dir │
        │ (media)    │     │ (other files) │
        └────────────┘     └──────────────┘
```

**Key components:**

- **Web/API Server** — serves frontend static files, REST API for configuration/search/queue management, WebSocket for real-time updates (IRC messages, download progress, notifications).
- **IRC Manager** — manages connections to multiple servers, handles authentication (NickServ, SASL, channel keys), auto-joins channel pairs, routes messages.
- **DCC Transfer Engine** — handles DCC SEND negotiations, manages concurrent transfers (configurable max, one-per-bot), performs disk space checks before accepting.
- **Core Services** — search result parsing, download queue with persistence, file-type routing, post-download hooks, notification bus (browser push now, extensible later), disk monitoring.

**Data flow for a typical search-and-download:**

1. User types search term in web UI → API → IRC Manager sends `!s term` to search channel.
2. Bot responses arrive → Search Parser extracts pack info → WebSocket pushes results to UI.
3. User clicks download → Queue Manager checks disk space → IRC Manager ensures presence in download channel → sends `xdcc send #N` → DCC Engine accepts transfer.
4. Progress updates stream via WebSocket → on completion, File Router moves file to correct directory → Post-Hook runs → Notification fires.

---

## Technology Stack

- **Language:** Go 1.22+
- **IRC:** [girc](https://github.com/lrstanley/girc) library (or ergochat/irc-go)
- **HTTP/WebSocket:** Go standard library `net/http` + [gorilla/websocket](https://github.com/gorilla/websocket)
- **Database:** SQLite via `modernc.org/sqlite` (pure Go, no CGO) OR MariaDB via `go-sql-driver/mysql` — both first-class
- **Frontend:** Vanilla JS with Alpine.js for reactivity, no build toolchain
- **Deployment:** Docker (primary) or systemd, behind nginx reverse proxy

---

## Data Model

### servers

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER PK | Auto-increment |
| name | TEXT | Display name |
| host | TEXT | IRC server hostname |
| port | INTEGER | IRC server port |
| ssl | BOOLEAN | Use TLS |
| nickname | TEXT | Primary nickname |
| alt_nicknames | JSON | Array of fallback nicknames |
| auth_method | TEXT | `none`, `nickserv`, or `sasl` |
| auth_password | TEXT | Password for auth (nullable) |
| auto_connect | BOOLEAN | Connect on app start |
| enabled | BOOLEAN | Server is active |
| created_at | TIMESTAMP | |
| updated_at | TIMESTAMP | |

### channels

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER PK | |
| server_id | INTEGER FK | References servers.id |
| name | TEXT | Channel name (e.g. `#channel`) |
| key | TEXT | Channel password (nullable) |
| search_command | TEXT | Default `!s` |
| download_channel | TEXT | Paired channel for DCC (nullable = same channel) |
| auto_join | BOOLEAN | Join when connected |
| enabled | BOOLEAN | |

### downloads

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER PK | |
| server_id | INTEGER FK | |
| channel | TEXT | Channel where DCC happens |
| bot_nick | TEXT | Bot nickname |
| pack_number | INTEGER | Pack number requested |
| filename | TEXT | |
| filesize | INTEGER | Advertised size in bytes |
| downloaded_bytes | INTEGER | Bytes received so far |
| status | TEXT | `queued`, `downloading`, `completed`, `failed`, `cancelled`, `needs_action` |
| destination_path | TEXT | Final file path after routing |
| error_message | TEXT | Error details (nullable) |
| peak_speed | INTEGER | Peak transfer speed in bytes/s |
| average_speed | INTEGER | Average transfer speed in bytes/s |
| started_at | TIMESTAMP | |
| completed_at | TIMESTAMP | |
| created_at | TIMESTAMP | |

### search_results

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER PK | |
| server_id | INTEGER FK | |
| channel | TEXT | Search channel |
| bot_nick | TEXT | Bot that sent the result |
| pack_number | INTEGER | Nullable if unparsed |
| filename | TEXT | Nullable if unparsed |
| filesize | TEXT | Size as advertised (e.g. "1.4G") |
| downloads_count | INTEGER | Download count if advertised |
| raw_line | TEXT | Original IRC message |
| search_query | TEXT | The search term used |
| parsed | BOOLEAN | Whether parsing succeeded |
| created_at | TIMESTAMP | |

### saved_searches

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER PK | |
| name | TEXT | Bookmark name |
| server_id | INTEGER FK | |
| channel | TEXT | |
| query | TEXT | Search term |
| created_at | TIMESTAMP | |

### parse_patterns

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER PK | |
| name | TEXT | Pattern name |
| regex | TEXT | Regular expression |
| field_mapping | JSON | Capture group → field mapping |
| priority | INTEGER | Tried in order, higher first |
| builtin | BOOLEAN | Shipped with app (cannot be deleted) |
| enabled | BOOLEAN | |
| match_count | INTEGER | Successful matches |
| fail_count | INTEGER | Consecutive failures |
| last_matched_at | TIMESTAMP | |
| auto_disabled | BOOLEAN | System-disabled due to degradation |

### post_hooks

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER PK | |
| name | TEXT | Hook name |
| scope | TEXT | `global`, `server`, or `channel` |
| scope_id | INTEGER | FK to server or channel (nullable for global) |
| hook_type | TEXT | `script` or `rename` |
| config | JSON | Script path or regex pattern + replacement |
| enabled | BOOLEAN | |

### file_routing_rules

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER PK | |
| pattern | TEXT | Glob pattern (e.g. `*.mkv`) |
| destination_dir | TEXT | Target directory |
| priority | INTEGER | Higher = checked first |
| builtin | BOOLEAN | The catch-all `*` rule is builtin and undeletable |
| enabled | BOOLEAN | |

### Database Abstraction

A Go repository interface that both SQLite and MariaDB implementations satisfy. The app selects the driver based on configuration. Schema is compatible with both — no engine-specific SQL.

```go
type Store interface {
    // Servers
    GetServers() ([]Server, error)
    CreateServer(s *Server) error
    UpdateServer(s *Server) error
    DeleteServer(id int64) error
    // Channels
    GetChannels(serverID int64) ([]Channel, error)
    // ... same pattern for all entities
}
```

---

## IRC Manager

### Connection Lifecycle

1. **Connect** — TCP or TLS based on server config.
2. **Authenticate** — based on `auth_method`:
   - `none`: skip.
   - `nickserv`: after RPL_WELCOME (001), send `PRIVMSG NickServ :IDENTIFY <password>`.
   - `sasl`: SASL PLAIN during capability negotiation, before registration.
3. **Auto-join** — join all enabled channels with `auto_join = true` for this server (both search and download channels), using channel keys where configured.
4. **Maintain** — handle PINGs, reconnect on disconnect with exponential backoff, re-join channels after reconnect.

### Channel Pairs

Channels have an optional `download_channel` field. When set:
- Search commands (`!s term`) are sent to this channel.
- The bots responding are located in the `download_channel`.
- The app auto-joins both channels.
- When a download is requested, the IRC Manager verifies it is present in the download channel before sending `xdcc send`.

When `download_channel` is null, search and DCC happen in the same channel.

### Message Routing

- All incoming messages pass through the Search Parser to check for pack listings.
- All messages are forwarded via WebSocket to connected UI clients viewing that channel's console.
- CTCP/DCC messages are routed to the DCC Transfer Engine.
- NickServ/server notices go to a system log viewable in the UI.

### Reconnection

- Exponential backoff: 5s → 10s → 30s → 60s → 120s, then retry every 120s.
- Queue state preserved across reconnects — queued downloads resume once reconnected.
- Connection status per server visible in the UI (connected, connecting, disconnected).

### Auto-Join Opt-Out

- `auto_connect` on servers: controls whether the server connects on app startup.
- `auto_join` on channels: controls whether the channel is joined when the server connects.
- Both can be toggled from the UI. Manual connect/join is always available via UI buttons or the IRC console.

---

## DCC Transfer Engine

### DCC SEND Flow

1. User requests download → IRC Manager sends `xdcc send #N` to bot in the download channel.
2. Bot responds with CTCP: `DCC SEND <filename> <ip> <port> <filesize>`.
3. Pre-transfer checks:
   - **Disk space:** Is `filesize` < available space on target volume minus safety margin (configurable, default 1GB free)?
   - **Concurrent limit:** Is active transfer count < configured max (default 3)?
   - **Per-bot limit:** Is there already an active transfer from this bot?
4. If checks pass → open TCP connection to `ip:port`, begin receiving data.
5. If concurrent limit hit → status stays `queued`, starts automatically when a slot opens.
6. Stream to a temp file (`.part` suffix) in temp directory, update `downloaded_bytes` in DB periodically.
7. On completion → rename to final filename, run file routing, execute post-hooks, fire notification.

### Passive DCC

Passive DCC (port 0 in DCC SEND) requires the client to open a listening port.

- **Detection:** When a bot sends DCC SEND with port 0, or sends NOTICEs/PRIVMSGs containing keywords like "passive", "firewall", "can't connect", "DCC rejected" — correlate with recent transfer requests.
- **If passive DCC is not configured:** Mark transfer as `needs_action`, notify user with the bot's original message, explain what's needed (port range configuration).
- **If passive DCC is configured:** (`passive_enabled: true`, `passive_ports` range set, `external_ip` set) — handle automatically: listen on a port from the range, send CTCP response, accept bot connection.
- **Never open ports without explicit user configuration.**

### DCC RESUME

If a `.part` file exists from a previous incomplete transfer, attempt `DCC RESUME` to continue. Fall back to full restart if the bot rejects the resume.

### Download Queue

- Global max concurrent transfers (configurable, default 3).
- One active transfer per bot at a time.
- FIFO queue, persisted in database.
- User can reorder, cancel, or retry failed downloads from the UI.
- On app restart: re-queue any downloads that were in `downloading` status.

### Disk Monitoring

- **Pre-download:** Check advertised `filesize` against available space minus safety margin. Reject if insufficient, notify user.
- **During transfer:** If available space drops below critical threshold (configurable, default 500MB), pause all active transfers and send critical notification.
- **Dashboard widget:** Disk usage of target directories visible in sidebar.

### Speed & Statistics

- **Per download:** Current speed, peak speed, average speed — tracked during transfer, stored in `downloads` table.
- **Per bot:** Total downloaded (bytes), download count, average speed, peak speed, failure rate — aggregated from downloads table, shown in tooltip on bot name.
- **Per channel:** Total downloaded, download count, average speed — shown in channel view header.
- **Overall:** Total downloaded all-time/today/this week, current aggregate speed, top bots by speed, top channels by volume.

---

## Search Parser

### Search Flow

1. User enters search term → app sends configured command (default `!s term`) to the search channel.
2. Multiple bots respond with PRIVMSGs or NOTICEs containing pack listings.
3. Responses arrive over several seconds, often multiple lines per bot.

### Parsing Strategy — Layered Approach

**1. Pattern library:** `parse_patterns` table ships with 10-15 common XDCC result regexes, ordered by priority. Each pattern defines capture groups mapped to fields (pack_number, filename, filesize, downloads_count).

**2. Try patterns in order:** For each incoming line from a bot, run through enabled patterns until one matches.

**3. Bot-pattern cache:** Once a pattern matches a bot, remember the association in memory. Next time that bot responds, try its known pattern first.

**4. Fallback — raw capture:** If no pattern matches, store the raw line in `search_results` with `parsed = false`. Show it in the UI with a visual indicator.

**5. Unparsed result handling in UI:**
- Show the raw line as-is so the user can read it.
- Offer a "teach parser" action: user marks which parts are pack number, filename, size → app generates a regex and adds it to `parse_patterns`.
- Or: user can manually enter a pack number and request download directly from the raw line.

### Pattern Degradation

Patterns track `match_count` and `fail_count`:
- Consecutive failures without a match increment `fail_count`.
- A successful match resets `fail_count` and increments `match_count`.
- After a configurable threshold of consecutive failures (default 50), the pattern is auto-disabled and the user is notified.
- Built-in patterns can be auto-disabled but not deleted. User-created patterns are never auto-deleted, only disabled.

### Parser Feedback Loop

The full cycle must work end-to-end:
1. Bot sends a format we don't recognize → shows as unparsed.
2. User teaches the pattern via UI → stored in DB.
3. Same bot sends another result → now parses correctly.
4. App restart → pattern persists, still works.

This cycle is a key integration test target.

### Search Session Management

- Each search gets a unique ID — all results grouped.
- Results stream into the UI in real-time via WebSocket as bot responses arrive.
- Configurable timeout (default 15s) after the last received result marks the search as complete.
- Previous search results kept in DB for history.
- Saved searches (bookmarks) can be re-executed with one click.

### Result Deduplication

- Same bot + same pack number in one search = keep the latest.
- Same filename from different bots = show all (different sources for the user to choose from).

---

## File Routing

### Rules

The `file_routing_rules` table is evaluated top-to-bottom by priority (higher first). First match wins.

**Default shipped rules:**

| Priority | Pattern | Destination |
|----------|---------|-------------|
| 100 | `*.mkv, *.avi, *.mp4, *.mov, *.wmv, *.flv, *.webm` | Media dir (DLNA) |
| 90 | `*.mp3, *.flac, *.ogg, *.wav, *.aac, *.m4a` | Media dir (DLNA) |
| 80 | `*.srt, *.sub, *.ass, *.ssa` | Media dir (subtitles with video) |
| 0 | `*` | Downloads dir (catch-all, undeletable) |

Users can add, edit, reorder, and delete rules via the UI. The catch-all rule at priority 0 is built-in and cannot be deleted or disabled — every file always has a destination.

### Routing Flow

1. DCC transfer completes → temp `.part` file renamed to final filename.
2. Walk rules by priority descending, match filename against pattern (glob).
3. First match determines destination directory.
4. Move file (atomic if same filesystem, copy+delete if cross-filesystem).
5. If move fails (permissions, path doesn't exist) → keep file in temp dir, mark as `routing_failed`, notify user.

### Pattern Suggestion in UI

When adding a new routing rule, the UI shows recent download filenames and suggests a glob pattern based on file extension. Live preview shows which recent files would match. For rename hooks, a live preview shows input → output as the user builds the regex.

---

## Post-Download Hooks

Hooks are configured per scope: global, per-server, or per-channel. Multiple hooks can apply to one download — they run in order.

### Hook Types

**Shell script/command:**

```
hook_type: script
config: { "command": "/path/to/script.sh" }
```

Environment variables passed to the script:
- `XIRC_FILE` — full path to the downloaded file
- `XIRC_FILENAME` — just the filename
- `XIRC_BOT` — bot nickname
- `XIRC_SERVER` — server name
- `XIRC_CHANNEL` — channel name
- `XIRC_FILESIZE` — size in bytes
- `XIRC_PACK` — pack number

**Regex rename:**

```
hook_type: rename
config: { "match": "(.+)\\.S(\\d+)E(\\d+)\\.(.+)", "replace": "Season $2/S${2}E${3} - $1.$4" }
```

The rename type can create subdirectories if the replacement contains `/`.

### Execution

- Timeout per hook (configurable, default 60s) — kills runaway scripts.
- Exit code 0 = success, non-zero = failure → logged with download record, user notified.
- Hook stdout/stderr captured and stored for debugging.
- Hooks run sequentially per download but don't block other downloads.

---

## Notification System

### Event Bus Architecture

```
Download complete ──┐
DCC needs action ───┤
Disk space warning ─┼──▶ Notification Bus ──▶ [ Browser Push ]
Hook failure ───────┤                        [ (future backends) ]
Connection lost ────┘
```

The bus receives typed events. Each backend registers for event types it cares about.

### Browser Notifications (Current Backend)

- Web Push API — frontend requests permission on first visit.
- Fires even when the tab is in the background.
- Clicking a notification navigates to the relevant view.

### Notification Events

| Event | Severity | Example |
|-------|----------|---------|
| Download completed | info | "movie.mkv completed (1.4G, avg 2.1MB/s)" |
| Download failed | warning | "file.rar failed: connection reset" |
| Passive DCC required | action | "bot5 requires passive DCC — configure port range" |
| Bot DCC hint message | action | "bot5 says: enable passive mode" |
| Disk space low | warning | "DLNA volume: 800MB remaining" |
| Disk space critical | critical | "Downloads paused — only 200MB free" |
| Hook failed | warning | "Post-hook 'rename.sh' failed on movie.mkv" |
| Server disconnected | warning | "Lost connection to irc.server.net, reconnecting..." |
| Server reconnected | info | "Reconnected to irc.server.net" |

### User Preferences

- Toggle notifications on/off per event type.
- Quiet hours (optional) — suppress non-critical notifications during configured window.

### Extensible Backend Interface

```go
type Notifier interface {
    Name() string
    Send(event NotificationEvent) error
    SupportedEvents() []EventType
}
```

Adding webhook, Home Assistant, or Telegram backends later is implementing this interface and registering with the bus. Backend config stored in a `settings` table as JSON.

---

## Web UI

### Technology

Vanilla JS with Alpine.js for reactivity. No build step — static files served by the Go binary. Dark theme default.

### Layout

```
┌──────────┬─────────────────────────────────┐
│          │                                 │
│  Servers │   Main Content Area             │
│  ├ irc1  │                                 │
│  │ ├ #ch │                                 │
│  │ └ #ch │                                 │
│  └ irc2  │                                 │
│    ├ #ch │                                 │
│    └ #ch │                                 │
│          │                                 │
│──────────│                                 │
│ Downloads│                                 │
│ Settings │                                 │
│ Disk ██░ │                                 │
└──────────┴─────────────────────────────────┘
```

**Sidebar:**
- Server/channel tree — expandable, connection status indicator (green/yellow/red dot).
- Downloads link with badge showing active count.
- Settings link.
- Disk usage bar at the bottom — color-coded by thresholds.

### Views

**Channel View** — two-panel, togglable:

- **Structured panel (default):** Search bar at top, results table below (sortable by filename, size, bot, download count). Each row has a download button. Unparsed results highlighted with "teach parser" action. Saved search bookmarks in dropdown. Channel stats (total downloaded, average speed) in header.
- **IRC Console panel:** Full message log, input field for raw commands, scrollable history. Can toggle between structured and console, or split-view side by side.

**Downloads View:**

Table with columns: Status, Filename, Bot, Size, Progress, Speed (current/peak), ETA, Actions.

- Status icons: downloading, queued, completed, failed, needs_action.
- Drag or arrow buttons to reorder queue.
- Filter by status.
- Failed/needs_action entries show error or bot message inline.
- Bot name shows speed/reliability tooltip on hover.

**Settings View** — tabbed:

- **Servers:** Add/edit/delete. Form: name, host, port, SSL, nickname, alt nicks, auth method, password, auto-connect. Test connection button.
- **Channels:** Per server. Form: channel name, key, search command, download channel (dropdown/text), auto-join.
- **File Routing:** Rule list, drag to reorder. Add rule with pattern suggestion from recent downloads, live preview.
- **Post-Hooks:** Per scope. Add script hook (path + test button) or rename hook (regex builder with live preview).
- **General:** Media directory, downloads directory, max concurrent transfers, disk space thresholds, DCC passive config, notification preferences.

### Real-Time Updates

Single WebSocket connection carrying typed events:

- `irc_message` — channel messages for console view.
- `search_result` — parsed result for structured view.
- `download_progress` — speed, bytes, percentage updates.
- `download_status` — status transitions.
- `notification` — alerts.
- `connection_status` — server connect/disconnect.

Frontend subscribes based on active view; connection stays open.

---

## Deployment

All four combinations are first-class:

| Deployment | Database | How |
|------------|----------|-----|
| Docker + SQLite | `docker compose up -d` | Default, zero external dependencies |
| Docker + MariaDB | `docker compose --profile mariadb up -d` | MariaDB container included |
| Systemd + SQLite | Copy binary, enable service | No DB server needed |
| Systemd + MariaDB | Copy binary, enable service | User provides MariaDB |

### Configuration

`config.yaml` — all settings have sensible defaults:

```yaml
server:
  host: 127.0.0.1
  port: 8085

database:
  driver: sqlite    # or "mysql"
  path: ./data/xirc.db
  # dsn: xirc:password@tcp(localhost:3306)/xirc  # for mysql

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

Environment variables override config file values (prefix `XIRC_`), convenient for Docker.

### Docker

**Dockerfile:**

```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o xirc .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
RUN adduser -D -h /app xirc
USER xirc
WORKDIR /app
COPY --from=builder /build/xirc .
EXPOSE 8085
VOLUME ["/app/data", "/srv/dlna/media", "/srv/downloads"]
ENTRYPOINT ["./xirc", "--config", "/app/data/config.yaml"]
```

**docker-compose.yaml:**

```yaml
services:
  xirc:
    build: .
    container_name: xirc
    restart: unless-stopped
    ports:
      - "127.0.0.1:8085:8085"
    volumes:
      - ./data:/app/data
      - /srv/dlna/media:/srv/dlna/media
      - /srv/downloads:/srv/downloads
    environment:
      - TZ=Europe/Berlin
    networks:
      - xirc

  db:
    image: mariadb:11
    container_name: xirc-db
    restart: unless-stopped
    profiles:
      - mariadb
    environment:
      - MYSQL_ROOT_PASSWORD=rootpass
      - MYSQL_DATABASE=xirc
      - MYSQL_USER=xirc
      - MYSQL_PASSWORD=password
    volumes:
      - db_data:/var/lib/mysql
    networks:
      - xirc

networks:
  xirc:
    driver: bridge

volumes:
  db_data:
```

For passive DCC, expose additional ports: `"30000-30010:30000-30010"`.

### Systemd

```ini
[Unit]
Description=xirc - XDCC IRC Web Client
After=network.target

[Service]
Type=simple
User=xirc
WorkingDirectory=/opt/xirc
ExecStart=/opt/xirc/xirc --config /opt/xirc/config.yaml
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

### Nginx

```nginx
server {
    listen 80;
    server_name xirc.local;

    allow 192.168.0.0/16;
    allow 10.0.0.0/8;
    allow 172.16.0.0/12;
    deny all;

    location / {
        proxy_pass http://127.0.0.1:8085;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    location /ws {
        proxy_pass http://127.0.0.1:8085;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_read_timeout 86400;
    }
}
```

### Cross-Compilation

```bash
GOOS=linux GOARCH=arm64 go build -o xirc   # Raspberry Pi 4
GOOS=linux GOARCH=arm go build -o xirc      # Raspberry Pi 3 or older
```

---

## State Persistence

On restart (app or container), xirc:

1. **Reconnects** to all servers with `auto_connect = true`.
2. **Re-joins** channels with `auto_join = true`.
3. **Re-queues** downloads that were in `downloading` status (retries once connected).
4. **Preserves** search history, bookmarks, parse patterns, and all configuration.

All state lives in the database. The only external state is the config file and downloaded files on disk.
