# Changelog

Notable changes per release. Older releases: see the GitHub release pages.

## [v0.13.1-beta] - 2026-11-03

### Fixed
- "database is locked (5) (SQLITE_BUSY)" on login, when clearing downloads and on other writes while bots are advertising or downloads are running: SQLite writers now wait up to 5 s for each other instead of failing at once, and transactions take the write lock up front.

## [v0.13.0-beta] - 2026-11-03

### Added
- **SSL transfers**: packs are requested with `xdcc ssend`, so bots that support it (DCC SSEND) send over TLS — active and passive. Per realm (Settings → Realms → Transfer encryption): *SSL, fall back to plain* (default: plain offer, "unknown command" or no answer within ~8 s → normal `xdcc send`), *SSL only*, or *Plain DCC only*. Each bot's SSL support is remembered in the database (also learnt from "ssl supported" notices), so the fallback wait happens at most once per bot. SSL downloads show 🔒. Bot certificates are self-signed and not verified.
- **Index expiry**: each realm has *Forget index entries after N days* (default 30, 0 = never); packs its bots haven't re-advertised for that long are removed from the index (default daily at 04:00).
- **Maintenance schedules**: both jobs — search results & index size, and index expiry — take a start time and "run every N hours" (Settings → System; `maintenance.start_time` / `interval_hours`, `index_expiry_start_time` / `index_expiry_interval_hours`).

### Changed
- Replacing a pack's index entry when a bot sends a different file now matches the bot's nick case-insensitively.
- Updating a realm through the API keeps fields the request leaves out instead of resetting them.

## [v0.12.0-beta] - 2026-11-03

### Added
- **Channel discovery** (admin): finds XDCC channels on a network. *Normal* mode lists channels and keeps those whose name or topic matches `xdcc`, `dcc`, `download` or your own keywords, pairing chat and download channels. *Aggressive* mode joins the busiest channels and counts bot ads (asks for confirmation; operators may treat it as spam). Scan now, or on connect per server; results in the server view, with a badge and notice on the Overview.
- **Preview a discovered channel**: joins it (and its partner) and opens it, with a bar to mark it as chat or download channel (preset from detection) and save it as a realm, or leave again. The discovered-channels table sorts by any column, most users first.
- **Passive (reverse) DCC** for bots that connect to you instead: set a port range under Settings → System, forward it on your router (and publish it in Docker). The address sent to bots is your public IPv4 or a **DynDNS hostname**; left empty, it is detected via public IP services, then OpenDNS.
- **Quick add** (➕ above Overview): paste `irc://irc.example.net/#channel` (`ircs://` for TLS) to add the server and its channels as realms, or a `/msg SomeBot xdcc send #12` command to queue a pack (asks for the realm).
- **Add links** takes `/msg <bot> xdcc send #N` commands too; the button is now a primary **＋ Add links**.
- **Bot debug mode** (Settings → General → Show bot traffic): each realm's bots are listed under it in the sidebar; click one for a panel with every message to and from that bot.
- **Downloads → Status**: asks the bot for your queue position (`XDCC QUEUE`); the bot's latest reply shows under the filename.
- **Retry** for cancelled downloads.

### Changed
- **Cancel tells the bot**: `XDCC REMOVE` while queued, `XDCC CANCEL` once the bot has offered the file (before, a re-request was answered with "You already requested that pack" until the bot's 180 s timeout).
- When the server refuses to deliver a message to a bot (e.g. "You must be connected via TLS to message this user", "No such nick"), the download fails at once with that reason instead of after an hour, and the refusal shows in red in the bot's debug panel.
- Ticking TLS in the server form moves a default port 6667 to 6697 (and back); a "not a TLS handshake" connect error explains the port mix-up.

### Fixed
- A new or deleted server (or deleted realm) showed in the sidebar only after a page reload.
- A DCC transfer whose connection failed hung in "downloading" instead of failing.
- Retried downloads waited for the next one-minute sweep before starting.
- README claimed passive mode needs no port forwarding — it does.

## [v0.11.0-beta] - 2026-11-01

### Added
- Downloads whose offered filename differs from the pack list (outdated list, swapped pack) are flagged with ⚠ mismatch; `downloads.on_mismatch` decides whether such files are skipped (default) or downloaded unsorted.
