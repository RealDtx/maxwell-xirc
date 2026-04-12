# xirc Plan 9: UI Redesign & Backend Enhancements — Design Document

**Date:** 2026-04-11
**Status:** Draft — awaiting user approval before plan writing

---

## Overview

This document captures the full design for Plan 9. It covers eleven distinct areas, some UI-only and some requiring new backend endpoints or data model additions.

---

## 1. Transient vs Non-Transient Error Classification

### Problem
Currently errors surface late (e.g., during a download attempt) and the systemd unit restarts unconditionally.

### Design
Introduce two exit code conventions across the application:

| Exit Code | Meaning | systemd behavior |
|-----------|---------|-----------------|
| `1` | Transient error (mount not ready, DB locked, network timeout) | Restart allowed |
| `78` (`EX_CONFIG`) | Non-transient / configuration error (wrong permissions, missing required config field, invalid routing dir) | Do NOT restart |

Add `RestartPreventExitStatus=78` to `deploy/xirc.service`.

In code: `internal/exitcodes/exitcodes.go` exports `ExitTransient = 1`, `ExitConfig = 78`.

### Startup permission check sequence
Runs in `main.go` after DB init, before IRC connections and HTTP server start:

```
parse config → init DB → run migrations → checkDirectories() → start IRC → start HTTP
```

`checkDirectories()`:
1. Query all unique `destination_dir` values from `file_routing_rules`
2. For each dir:
   - If dir does not exist → `os.Exit(ExitConfig)` — non-transient, operator must fix
   - Attempt `os.Create(dir + "/.xirc_write_check")` + immediate `os.Remove`:
     - `os.ErrPermission` → `os.Exit(ExitConfig)` — wrong ownership/mode, operator must fix
     - `syscall.ENOENT` (parent vanished mid-check, race) → treat as transient → `os.Exit(ExitTransient)`
     - `syscall.EIO` or other I/O error (NFS hiccup) → transient → `os.Exit(ExitTransient)`
     - Success → continue
3. Log `startup: all N destination directories verified (writable)` on success

### Deployment note
Docs / README should explain how to grant the `xirc` user access:
```bash
# Option A: ownership
chown xirc:xirc /srv/downloads

# Option B: group access
usermod -aG media xirc
chmod g+rwx /srv/downloads

# Option C: ACL
setfacl -m u:xirc:rwx /srv/downloads
```
`ReadWritePaths=` in the systemd unit must list all configured destination dirs (currently static; Plan 9 does not make this dynamic — operator updates the unit when adding a new routing rule).

---

## 2. Simple / Advanced Mode Toggle

### Design
- A toggle stored in `localStorage` key `xirc_mode` (`"simple"` | `"advanced"`), default `"simple"`
- Toggle button in the sidebar footer (small label: "Simple / Advanced")
- **Simple mode** shows: Search (global), Downloads, File Manager
- **Advanced mode** shows: everything — servers, channels, IRC consoles, settings, debug views
- The Alpine store reads `localStorage` on `init()` and sets `appMode`; writing the toggle updates `localStorage` and `appMode` reactively

### Simple mode Search
In simple mode, the search bar is always visible at the top. It searches across all configured servers/channels that have a `search_command` set. The user never sees server or channel pickers — the app picks the best available channel automatically (first channel with `search_command`, or round-robins if multiple).

---

## 3. File Routing UX Inversion

### Design (UI-only — no DB migration)
The routing rules settings tab is restructured to show rules **grouped by `destination_dir`** instead of a flat list.

**Rendering:**
- Each destination dir is a collapsible card header
- Inside: extension patterns shown as tag chips (`mkv`, `mp4`, `avi`, …)
- Each chip has an × to delete that specific rule
- "+ Add extension" opens an inline picker: predefined chips (mkv, mp4, avi, mp3, flac, epub, pdf, zip, cbz) + free-text input
- Selecting a predefined or entering a custom extension calls `api.createRoutingRule({pattern: "*.ext", destination_dir: dir, priority: 0})`

**Add new destination:**
- "Add destination" button opens a modal with:
  1. Directory path (text field + directory picker button — see §6)
  2. Priority (number, default 0)
  3. Initial extension (optional)
- Saves as one or more `FileRoutingRule` records

---

## 4. Server-Side Directory Picker

### Backend
New endpoint: `GET /api/browse?path=/srv/downloads`

Response:
```json
{
  "path": "/srv/downloads",
  "parent": "/srv",
  "entries": [
    {"name": "media", "is_dir": true},
    {"name": "ebooks", "is_dir": true},
    {"name": "README.txt", "is_dir": false}
  ]
}
```

Security:
- `filepath.Clean` + `filepath.EvalSymlinks` on the requested path — prevents `../` traversal
- Return only directories where the process has read permission (`os.ReadDir`); on `os.ErrPermission` return 403
- No allowlist needed — filesystem permissions are the security boundary (xirc runs as dedicated user)

### Frontend
A reusable `DirPickerModal` component (Alpine `x-data` component):
- Breadcrumb header showing current path segments (clickable to navigate up)
- Scrollable list of subdirectories (files shown greyed out / not selectable)
- "Select this directory" button confirms
- "Cancel" button closes without selection
- Replaces / sits alongside every path text field: a small folder icon button opens the modal

**Used in:**
- Routing rule `destination_dir`
- Channel form `download_channel` (if it takes a path — currently it's a channel name; check §7)

---

## 5. Home / Dashboard View

### Navigation
- Clicking the **"xirc"** header text in the sidebar sets `activeView = 'home'`
- Sidebar nav row gains a **"Channels"** tab (returns to last active channel, or prompts to select one)

### Home view content
- Per-server status cards:
  - Server name + status dot (connected / connecting / disconnected)
  - Connected since / uptime (if connected)
  - Reconnect count
  - Lag (ms, from PING/PONG)
  - Channel list with member counts (if available)
- **Error overview panel** — see §10
- Quick-access: recent downloads (last 5), recent errors

---

## 6. Backend IRC Message Buffer

### Design
An in-memory ring buffer per `(server_id, channel)` key, capacity 1 000 messages. `channel = ""` holds server-level messages (not directed to any channel).

**New struct in `irc` package:**
```go
type MessageBuffer struct {
    mu   sync.RWMutex
    bufs map[string][]*BufferedMessage  // key: "serverID:channel"
    cap  int
}
```

**New REST endpoint:**
`GET /api/irc/{server_id}/messages?channel=&before=RFC3339&limit=200`

- Returns up to `limit` messages (max 200) with `timestamp < before` (omit `before` for latest)
- Response: `[]BufferedMessage{timestamp, nick, text}`
- Used for initial load and infinite-scroll pagination

**WS still delivers live messages** — frontend appends them; the buffer endpoint is only for history.

### Frontend scroll behaviour
On view open: fetch last 200 messages, populate log.
On scroll-to-top: detect scroll position ≤ 20px from top → fetch next page with `before=<oldest loaded timestamp>`, prepend to log.

---

## 7. Server View (click server in sidebar)

Replaces the current "no server selected" empty state.

### Layout
Two-panel:
1. **Status card** (top)
2. **Raw IRC log** (bottom, scrollable, live + historical via §6)

### Status card fields
| Field | Source |
|-------|--------|
| Status | `ircStatus[id].status` |
| Connected since | new `connected_at` field on IRC status WS event |
| Uptime | derived client-side from `connected_at` |
| Reconnect count | new `reconnect_count` field |
| Lag | new `lag_ms` field, updated via PING/PONG ticker (30s interval) |

### Backend additions
- `IRCStatus` struct gains `ConnectedAt *time.Time`, `ReconnectCount int`, `LagMs int64`
- IRC client manager tracks reconnect count (increments on each reconnect)
- IRC client sends periodic PING, records round-trip time; updates `LagMs`
- WS `connection_status` event extended with these fields
- Server-level messages (NOTICE, ERROR, MODE, etc. not in a channel) already flow through the event bus — wire them into the message buffer with `channel: ""`

---

## 8. Channel View Rework

### Layout toggle
Two modes, stored in `localStorage` per channel key:
- **Single pane**: tabs at top switch between Search pane and IRC pane
- **Side-by-side**: Search pane left, IRC pane right, resizable (CSS flex, draggable divider optional)

### Tab row
- **Search Channel** tab — always visible
- **Download Channel** tab — only rendered if `channel.download_channel != ""`

Each tab/pane is an independent IRC view (log + input) for its respective channel.

### IRC pane
- Message log (buffered, scrollable — see §6)
- Input bar (send message or `/raw` command)
- Collapsible **user list** sidebar (right edge of pane)

### User list
- Snapshot on demand: `GET /api/irc/{server_id}/names?channel=name` → sends NAMES command, waits for 353/366 replies (timeout 5s), returns `[]string`
- "Refresh" button in the user list header triggers a new snapshot
- Clicking a user:
  - Highlights that user's messages in the IRC log (yellow background or bold nick)
  - If a bot nick, filters search results to rows where `bot_nick === selectedUser`

### Backend: names endpoint
`GET /api/irc/{server_id}/names?channel=name`
- IRC client sends `NAMES #channel`
- Collects `353` (RPL_NAMREPLY) lines until `366` (RPL_ENDOFNAMES) or timeout
- Returns parsed nick list (strip mode prefixes: `@`, `+`, `%`, etc.)
- Response: `{"channel": "#search", "nicks": ["@BotFather", "someuser", ...]}`

---

## 9. File Manager View

Available in both simple and advanced modes.

### Design
A view showing all configured `destination_dir` directories.

**Layout:**
- Left: directory list (from routing rules, grouped by dir)
- Right: file list for selected directory
  - Columns: filename, size, modified date
  - Sort by any column
  - No deletion from UI (read-only — the xirc user can write files but the web UI only browses)

**Backend endpoint:**
`GET /api/files?dir=/srv/downloads`
- Returns file listing for a directory that is in the set of configured `destination_dir` values (security: only whitelisted dirs, not arbitrary filesystem)
- Unlike `GET /api/browse` (which allows navigation), this only serves configured routing dirs
- Response: `{"dir": "/srv/downloads", "files": [{"name": "movie.mkv", "size": 1234567890, "modified": "RFC3339"}]}`

---

## 10. Download Stats & History

### Data model
New table `download_stats`:
```sql
CREATE TABLE download_stats (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    filename    TEXT NOT NULL,
    size_bytes  INTEGER,
    server_id   INTEGER,
    channel     TEXT,
    bot_nick    TEXT,
    pack_number INTEGER,
    started_at  DATETIME,
    completed_at DATETIME,
    status      TEXT,   -- 'completed', 'failed', 'cancelled', 'stats_only'
    stats_only  BOOLEAN DEFAULT 0
);
```

### "Stats only" mode
- Global toggle in settings: **Download mode**: "Save file" | "Stats only"
- Per-download override: checkbox in the download request modal (advanced mode only)
- When `stats_only = true`: transfer completes normally, then `os.Remove(filePath)` is called immediately after; the `download_stats` record is kept
- The `downloads` table (active queue) is unaffected; `stats_only` only influences post-transfer cleanup

### Stats view
Available in both simple and advanced modes.

Panels:
- **Summary**: total transfers, success rate (%), total bytes transferred, total bytes saved (excludes stats-only)
- **Per-bot table**: bot nick, total packs, success rate, average speed
- **Timeline**: simple bar chart (no charting library — rendered with CSS flex bars) showing transfers per day (last 30 days)
- **History table**: paginated, sortable, filterable by status/server/bot

---

## 11. Error Overview

### Design
A panel on the home dashboard (and a dedicated tab in advanced mode).

**Error types tracked:**
- IRC disconnection events (server_id, timestamp, reason)
- Download failures (download_id, filename, reason)
- Hook failures (hook_id, hook_name, stderr snippet)
- Startup permission check failures (dir, reason) — logged but don't re-trigger after startup
- Search timeouts

**Backend:**
- In-memory ring buffer of last 200 errors (same pattern as message buffer)
- New WS event type `error_event` with fields: `{error_type, server_id?, message, timestamp}`
- New REST endpoint: `GET /api/errors?limit=50` for initial load

**Frontend:**
- Badge on the home nav item (red dot) when unread errors exist
- Error list: icon by type, timestamp, message, "dismiss" per item or "clear all"
- Errors acknowledged in `localStorage` (list of acknowledged timestamps); badge clears when all are acknowledged

---

## 12. Mobile Responsive Layout

### Design
- Sidebar collapses to an off-canvas drawer on viewports < 768px
- Hamburger button (top-left) toggles drawer; overlay closes it
- Channel view: Single pane only on mobile (Side-by-side disabled)
- User list on mobile: full-screen overlay instead of inline sidebar
- Settings tabs become a dropdown `<select>` on mobile
- No new CSS framework — use existing Tailwind-style utility classes or plain CSS media queries matching whatever the current stylesheet uses

---

## Summary of New Backend Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/irc/{id}/messages` | Buffered IRC message history |
| GET | `/api/irc/{id}/names` | NAMES snapshot for a channel |
| GET | `/api/browse` | Server-side directory browser (for picker) |
| GET | `/api/files` | File listing for configured routing dirs |
| GET | `/api/errors` | Recent error log |
| GET | `/api/stats/downloads` | Download statistics summary |
| GET | `/api/stats/history` | Download history (paginated) |

## Summary of New/Modified Data Models

| What | Change |
|------|--------|
| `IRCStatus` | + `connected_at`, `reconnect_count`, `lag_ms` |
| `download_stats` | New table |
| `downloads` | + `stats_only bool` field |
| Message buffer | New in-memory struct (not persisted) |
| Error buffer | New in-memory struct (not persisted) |

## Summary of New Config Fields

| Field | Type | Default | Purpose |
|-------|------|---------|---------|
| `stats_only_default` | bool | false | Global stats-only download mode |

## Summary of New Deployment Changes

| What | Change |
|------|--------|
| `deploy/xirc.service` | + `RestartPreventExitStatus=78` |
| `internal/exitcodes/exitcodes.go` | New: `ExitTransient=1`, `ExitConfig=78` |

---

## Out of Scope for Plan 9

- Persistent message buffer (survives restart) — in-memory only for now
- Telegram / webhook notification backends (notify package already supports adding them)
- IRC channel join/part from UI (requires deeper IRC client changes)
- Keyboard shortcuts
- Dark mode (already implemented per existing code — just verify toggle is wired)
