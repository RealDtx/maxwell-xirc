# xirc Startup Wizard — Design Spec

**Date:** 2026-04-12
**Status:** Approved

---

## Overview

When xirc starts and a configured destination directory does not exist, it currently exits with code 78 before the HTTP server starts. This blocks usage and provides no self-service recovery path.

This feature replaces that hard exit with a guided wizard — interactive CLI prompt when running in a terminal, web UI overlay when running headless (systemd/Docker). The wizard redirects the bad routing rules to valid directories and writes the result back to `config.yaml` so the setup is portable and reproducible.

---

## Goals

- Let xirc start (or guide the operator to fix config) when destination dirs are missing
- Make the corrected setup copyable: `config.yaml` is the portable artifact; the DB holds runtime data
- Support both interactive terminal use and headless/systemd deployment
- No directory creation — the wizard only redirects routing rules to paths that already exist

---

## Non-Goals

- Automatic directory creation
- Wizard for permission errors (those remain fatal exit 78 — operator must fix)
- First-run server/channel setup (out of scope)

---

## Error Classification (unchanged)

| Condition | Behaviour |
|-----------|-----------|
| Dir does not exist | Non-fatal — triggers wizard |
| Dir exists but not writable (`EACCES`, `EROFS`) | Fatal — `os.Exit(78)` |
| Other I/O error (NFS, `EIO`) | Fatal transient — `os.Exit(1)` |

---

## Core: `checkDirectories()` Refactor

`checkDirectories()` is changed to return a `[]string` of destination dirs that do not exist, instead of calling `os.Exit` for them. Permission and I/O errors continue to call `os.Exit` immediately.

```go
func checkDirectories(store db.Store) []string  // returns bad dirs
```

A `SetupState` struct is created in `main.go` after migrations and passed to the HTTP server:

```go
type SetupState struct {
    mu       sync.Mutex
    Required bool
    BadDirs  []string
}

func (s *SetupState) Complete() {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.Required = false
    s.BadDirs = nil
}
```

Startup sequence in `main.go`:

```
parse config → init DB → migrate → checkDirectories()
  → bad dirs?
      → TTY: CLI wizard → update config.yaml + DB → continue startup
      → no TTY: set SetupState.Required = true → start server (wizard served via web)
  → no bad dirs: continue startup normally
```

---

## Defaults

`os.UserHomeDir()` (available since Go 1.12) is called once at startup to derive suggested replacements:

| Rule category | Suggested replacement |
|---------------|----------------------|
| Video, audio, subtitle patterns | `$HOME/Videos` |
| Catch-all (`*`) | `$HOME/Downloads` |

The suggestion for each bad `destination_dir` is chosen by looking at what file patterns route to it:
- If any video/audio/subtitle pattern routes there → suggest `$HOME/Videos`
- If the catch-all (`*`) routes there → suggest `$HOME/Downloads`
- If both → two separate fields (one per dir)

---

## Persistence

When the wizard completes (either path), two writes happen:

### 1. `config.yaml` update

`storage.media_dir` and `storage.downloads_dir` are rewritten in-place using a YAML round-trip that preserves all other fields and comments where possible. This is the portable setup artifact.

### 2. Routing rules update (DB)

All routing rules with `destination_dir == old_dir` are updated to `destination_dir = new_dir`. This keeps the DB in sync so routing works immediately without waiting for the seed to re-run.

Both writes happen in the same logical operation. If either fails, neither is persisted (write config to a temp file first, then rename atomically; DB update wrapped in a transaction).

---

## Path A: CLI Wizard (TTY detected)

Runs before the HTTP server starts. TTY detection: `isatty(os.Stdin.Fd())` via a small helper (no external dependency — use `syscall.IoctlGetTermios` or equivalent for Linux).

**Prompt format:**

```
xirc: the following destination directories do not exist:

  /srv/dlna/media   → used by: *.mkv *.avi *.mp4 *.mov *.wmv *.flv *.webm *.mp3 *.flac *.ogg *.wav *.aac *.m4a *.srt *.sub *.ass *.ssa
  /srv/downloads    → used by: * (catch-all)

Enter replacement paths (press Enter to accept suggestion):

  /srv/dlna/media  [/home/pi/Videos]: _
  /srv/downloads   [/home/pi/Downloads]: _

Apply? [Y/n]: _
```

- Empty input → accept bracketed suggestion
- `n` at confirm → apply suggestions as defaults (same result, no data loss)
- Non-TTY stdin (pipe, systemd) → apply defaults silently, log each substitution:
  `startup: applying default path /srv/dlna/media → /home/pi/Videos`

After the wizard completes:
- `config.yaml` and DB are updated
- `SetupState.Required` is never set (startup continues normally)
- No restart required

---

## Path B: Web Wizard (no TTY)

The HTTP server starts with `SetupState.Required = true`.

### New endpoints

**`GET /api/setup/status`**
```json
{ "required": true, "bad_dirs": ["/srv/dlna/media", "/srv/downloads"] }
```

**`GET /api/setup/defaults`**
```json
{ "videos_dir": "/home/pi/Videos", "downloads_dir": "/home/pi/Downloads" }
```

**`POST /api/setup/complete`**

Request:
```json
{
  "mappings": [
    { "old_dir": "/srv/dlna/media",  "new_dir": "/home/pi/Videos" },
    { "old_dir": "/srv/downloads",   "new_dir": "/home/pi/Downloads" }
  ]
}
```

Response:
```json
{ "restart_required": true }
```

On success: updates `config.yaml` + DB, calls `SetupState.Complete()`.

### Frontend

`init()` calls `GET /api/setup/status`. If `required: true`:
- A fullscreen overlay (`position: fixed; inset: 0; z-index: 9999`) is rendered over the normal UI
- The overlay fetches `GET /api/setup/defaults` and pre-fills one input per bad dir
- Each input row shows: bad path, file patterns affected, suggested replacement (editable)
- **Cancel** → submits with defaults (no empty state; always results in a valid config)
- **Apply** → submits with user-entered values; on success overlay closes, a persistent banner appears:
  `"Setup complete — restart xirc for changes to take effect."`
- Input validation: on each input change, check that the entered path is non-empty; the Apply button is disabled while any field is empty

---

## Wizard Skip Condition

`checkDirectories()` returns an empty slice → `SetupState.Required = false` → no wizard shown, server starts normally. This covers:
- Fresh install where `config.yaml` already has valid dirs
- Subsequent start after the wizard previously wrote valid dirs to `config.yaml`
- Any environment where the dirs actually exist

---

## File Changes

```
main.go                         MODIFY — checkDirectories returns []string, TTY branch, SetupState wiring
config/config.go                MODIFY — add WriteStorageDirs(path, mediaDir, downloadsDir string) error
server/server.go                MODIFY — accept *SetupState, register setup routes
server/setup_handler.go         NEW    — GET /api/setup/status, GET /api/setup/defaults, POST /api/setup/complete
web/js/app.js                   MODIFY — init() setup check, setupRequired state, wizard handlers
web/index.html                  MODIFY — wizard overlay template
```

No new packages. `SetupState` is defined in `server/setup_handler.go` (same package as its consumers) and instantiated in `main.go` before being passed to `server.New()`.

---

## Testing

- Unit test `checkDirectories()` with a mock store returning rules pointing to a temp dir that is then deleted — verify bad dirs returned, no exit
- Unit test `config.WriteStorageDirs()` — write to temp file, verify YAML round-trip preserves other fields
- Unit test `POST /api/setup/complete` — mock store, verify routing rules updated and `SetupState.Required` cleared
- CLI path: tested manually (TTY detection can't be unit-tested easily in CI)
