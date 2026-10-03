# maXwell IRC — User Guide

## Overview {#overview}
maXwell IRC is a self-hosted web client for finding and downloading files that bots offer over IRC XDCC. It connects to IRC servers in the background, and you search, request and manage downloads from the browser — no IRC client needed.

The sidebar is your map: **Search**, **Downloads**, **Files**, **Stats**, **Settings** (admins only) and **Help**. A Simple/Advanced switch at the bottom controls how much detail you see (see [Modes](#modes)).

You log in with an account, or your network may be configured to skip login entirely (see [Access](#access)). Everything you do — searching, requesting downloads, browsing files — works the same either way; only Settings and file-management actions require the admin role.

Good to know: the server binary takes `--config` (path to `config.yaml`), `--debug` (verbose logging) and `--create-admin <name>` (create or reset an admin account, then exit) — these are for the person running the server, not something you'll see in the app. First-time setup and installation are covered in [Installation](#install).

## Concepts: servers, channels, realms {#concepts}
Three things make up how maXwell IRC talks to IRC:

- **Server** — one IRC network connection: host, port, nickname, TLS, auto-connect. Configured under [Settings → Servers](#settings-servers).
- **Realm** — a pairing of two channels on that server: a **chat channel**, where you type a search command and a search bot replies (e.g. `#example-chat`), and a **download channel**, where bots continuously advertise the packs they carry (e.g. `#example-downloads`). Configured under [Settings → Realms](#settings-realms). If you leave the download channel blank, xirc can fill it in automatically the first time it reads that channel's topic on join.
- **Passive index** — bots in a download channel announce their catalog on their own, all the time, whether or not you're searching. maXwell IRC parses every one of those announcements it sees and stores it, building a searchable catalog without ever sending a search itself. That's what **index search** queries: instant, local, and available even if IRC is briefly down.

**Live search** is the other mode: it sends your realm's search command (e.g. `!s some.movie`) to the configured search bot in the chat channel and parses whatever comes back over the next few seconds. It reaches bots or catalogs the passive index hasn't seen yet, but needs a live connection and takes longer.

Both feed the same [Search](#search) view, and both rely on parse patterns — regexes that turn a raw bot line into pack number / filename / size — configured under [Settings → Patterns](#settings-patterns) and, per realm, under [Settings → Realms](#settings-realms).

## Simple and advanced mode {#modes}
The Mode switch at the bottom of the sidebar is a personal display preference, remembered in your browser — it doesn't change any data or permissions.

**Simple** shows only what you need day to day: Search, Downloads, Files and Help. It hides the server/channel tree, the Stats and Settings nav entries, the per-channel side-by-side layout toggle, and the raw IRC user-list panes.

**Advanced** adds everything back: the server tree in the sidebar for jumping straight to a channel, Stats, Settings (if you're an admin), the raw "Bot IRC" console pane per channel, a layout toggle (single pane vs. side-by-side chat/search), and the "All servers" filter on global search.

Switch any time with the Simple/Advanced buttons — nothing is lost either way.

## Search {#search}
Use Search to find files bots are offering, either from the collected [passive index](#concepts) or with a live query.

**How to use it:** pick **Index** or **Live**, type a query, and press Enter or Search. Results show as a table: Pack #, Filename, Size, Bot, Downloads (times requested), and an Actions column with a Download button. Click a column header to sort. Save a query with **Bookmark** and reuse it from the "Saved searches…" dropdown.

Live search targets the realm's configured search bot; if none is set, it auto-detects from the first reply. A search stops waiting after the realm's search timeout.

Good to know: a row is only downloadable once xirc knows the bot nick and pack number — some raw or unparsed lines aren't. Admins see a "teach parser" link on unparsed rows to add a matching pattern on the spot (see [Settings → Patterns](#settings-patterns)). Requesting a pack queues it under [Downloads](#downloads); whether the file is actually kept or only counted is controlled by the "Download mode" toggle on the [Stats](#stats) page.

## Downloads {#downloads}
The Downloads view lists every pack you've requested and its progress.

Status moves through: **queued** → **downloading** → **processing** (file received, being moved/extracted) → **completed**, or **failed** if something went wrong. **needs_action** means a bot sent a file xirc wasn't expecting (no matching pending request) — retry it once it's fixed, or ignore it.

**How to use it:** Cancel a queued or downloading item; Retry a failed or needs_action one. Admins can also reorder the queue with "Move to front", pick a specific destination folder before it starts (instead of the automatic one — see [Routing](#routing)), and toggle per-download auto-extract for archives. Click the folder icon to jump to a download's location in [Files](#files).

Good to know: once a pack is requested, xirc waits up to one hour for the bot to actually send it before giving up and marking it failed.

## Routing, library and auto-extract {#routing}
After a download finishes, maXwell IRC can sort it automatically instead of leaving it flat in the downloads folder. This is the **library**: a set of categories (series, show, movie, music, magazine, ebook, game, software), each matching by file extension and filename pattern, in priority order. A matched file is moved under the category's folder, with the destination path built from a template like `{title}/{season_dir}`. A file that matches nothing stays where it landed.

Archives (tar/zip/rar/7z) inside a matched category can be auto-extracted: the archive is unpacked and its contents flattened into the resolved folder, and — if the category and the individual download both allow it — the archive is deleted afterward, only once extraction succeeded. Series, movie and music categories extract (and delete the archive) by default; others don't.

Configure categories, the media root and auto-organize under [Settings → Library](#settings-library); the same mechanics drive the manual **Extract…** action in [Files](#files).

## Files {#files}
Files is a browser for everything under your configured destination folders (downloads, media, and any subfolders).

**How to use it:** pick a Destination on the left, browse or filter, click a file to open it. Playback is native-only, so video (mp4, m4v, webm, mkv, mov), audio (mp3, m4a, aac, flac, ogg, opus, wav), PDF, and images (jpg, jpeg, png, gif, webp, avif) open in a built-in viewer; text files (subtitles, .nfo, .txt and anything else that turns out to be text) open in a text window; other files download instead. Even within a playable extension, your browser may not decode the codec inside it — an MKV using HEVC, AC3 or DTS, for example, usually needs downloading and playing in a desktop player like VLC; the viewer shows a "can't play" message when that happens. Every file also has a ⬇ download button regardless of whether it can preview.

**Admin-only:** drag-and-drop or "Move to…" between destinations, rename, delete (folders go with their contents), create folders, and create empty files (**+ New file** — the new file opens straight in the text editor). **Extract…** appears next to the first volume of a detected archive; it runs in the background and unpacks into a new folder named after the archive. "Delete archive afterwards" removes all volumes of the archive, but only if extraction succeeded — failures are left in place and show up in the error list next to the file.

**Text files:** admins can edit files up to 2 MB and save with the Save button or Ctrl+S; everyone else sees them read-only. Larger files (logs) open read-only in 256 KB pieces with "Load more" and "Jump to end". The encoding is detected (UTF-8, otherwise CP437 for .nfo/.diz and Windows-1252 for the rest) and can be switched in the window; saving keeps the file's encoding (or the one you switched to in the window) and normalises line endings to the file's dominant style (lone CR and `\r\r\n` become that style), and refuses characters the encoding can't store instead of mangling them. If the file changed on disk since you opened it, you're asked whether to overwrite it or keep editing. Binary files are recognized by content and simply download.

## Statistics {#stats}
Stats summarizes your download history and how full the passive index is.

Summary cards: total transfers, success rate, total bytes transferred, and bytes actually kept on disk. Below that, a paged history of individual downloads, and two index tables — one per download channel (bots seen, files, advertised bytes, last activity) and one per bot (adds transfer count and average/peak speed, from completed downloads).

The **Download mode** toggle here — "Save file" vs. "Stats only (don't save)" — is what search's Download button uses when you request a pack: in stats-only mode, xirc still transfers the file to confirm the bot has it and records size/speed, but deletes it immediately instead of keeping it. Useful for building up the index or checking a bot's health without spending disk space.

## Settings: Servers {#settings-servers}
The IRC networks maXwell IRC connects to (admin-only).

**How to use it:** Add Server with a name, host, port, nickname, SSL, auto-connect and authentication (none, NickServ or SASL, with a password); Edit or Delete existing ones from the table. The password is never shown again — when editing, leave it empty to keep the current one; switching authentication to None removes it.

A server needs at least one realm before it's useful for searching or downloading — see [Settings → Realms](#settings-realms).

## Settings: Realms {#settings-realms}
The chat/download channel pairs for each server (see [Concepts](#concepts) for what a realm is).

**How to use it:** pick a server, then Add Realm: the chat channel name (e.g. `#example-chat`), a display name, a channel key if the channel is `+k`, the search command (e.g. `!s`), the download channel (e.g. `#example-downloads`), the search bot (leave blank to auto-detect on the first search), a search timeout, and whether to auto-join it on connect. From the same form you can scope existing parse patterns to just this realm.

Good to know: leaving the download channel blank isn't final — xirc fills it in the first time it reads that channel's topic after joining, if the topic names one.

## Settings: Library {#settings-library}
Where finished downloads are organized (see [Routing](#routing) for the mechanics).

**How to use it:** toggle auto-organize, set how many folder levels deep to search, and the media root, all globally. Per category (Series, Show, Movies, Music, …): enable/disable it, its folder, path template (with the fields it accepts shown below the input), season-folder style for series, create-folders / auto-extract / delete-archive-after-extract, and — under Advanced — its file extensions, regex patterns, and priority (higher wins on a tie). "Re-detect from folders" reseeds categories from what's already on disk; the preview box below tests a filename against the current rules before you save.

## Settings: Patterns {#settings-patterns}
The regexes that turn a raw bot line into a pack number, filename and size (see [Concepts](#concepts)).

**How to use it:** the table lists every pattern with its scope (global or one realm/channel), regex, priority and tags — click a row's scope or tags to edit them inline. Export the whole set as YAML or JSON, or Import a file to add more. Paste a raw IRC line into the tester at the bottom to see which pattern matches and what fields it extracts.

Good to know: an unparsed row in [Search](#search) has a "teach parser" link for admins that opens a trainer to build a new pattern from that exact line.

## Settings: General {#settings-general}
Personal display preferences — stored only in your browser, not shared with other users.

**How to use it:** pick a date format from the presets (DD-MM-YYYY, YYYY-MM-DD, MM/DD/YYYY, each with time) or type your own; the preview below updates live.

## Settings: Users {#settings-users}
Accounts and roles (admin-only tab). See [Access](#access) for what each role can actually do.

**How to use it:** the table lists every user with their role; change a role from the dropdown, Reset password, or Delete a user (you can't delete yourself). Add a new one at the bottom with a username, a password (minimum 8 characters), and a role.

## Settings: System {#settings-system}
Server-wide configuration (admin-only), applied live when you Save.

**How to use it:** Storage (downloads/temp folder, minimum free space), Downloads (max parallel transfers), Maintenance (how long to keep search results, index size cap, how often cleanup runs), Login (trusted networks/role/proxies — see [Access](#access)), and Interface — **Help blocks start**, which controls the collapsible help panel (the same text you're reading now) under each view: always open, open once per browser until you close it, or closed until you click the ⓘ icon. A read-only Server card shows host, database and config-file location for reference.

Good to know: any field can be locked by an environment variable on the server — locked fields show disabled with a tooltip naming the variable. If the config file itself can't be written, Save is disabled and a warning explains why.

## Settings: Backup {#settings-backup}
Export servers and realms (with all their channels) and system settings to a JSON file, and import such a file here or on another xirc (admin-only).

**How to use it:** under Export, tick whole servers, single realms (their server comes along, holding only those realms) or "All", optionally System settings, then Export. Under Import, pick a file: a preview lists every item as *new*, *exists* (with the fields that would change) or *error*. Choose whether existing items are skipped or overwritten, override that per row (or exclude a row), then Apply — the result column shows what happened to each item.

Good to know: passwords and channel keys are never exported, and an import never changes the ones already set — re-enter them after importing onto a new machine. Login/access settings (trusted networks, role, proxies) are never exported or imported. Import only adds or updates; nothing missing from the file is deleted. Settings fixed by an environment variable keep their current value.

## Access: login, roles, trusted networks {#access}
Every account has a role: **admin** (Settings, all file-management actions, pattern edits, user management) or **user** (search, request downloads, browse and view files, read-only elsewhere). Anonymous visitors, before logging in, can still reach the login page and the app's static assets — every API action requires a session or a trusted network.

You can also skip login by IP: networks listed under [Settings → System](#settings-system) (or set at install time) are granted a configured role — admin or user — without a password, useful for a trusted home LAN. Behind a reverse proxy, only proxies you've explicitly trusted are allowed to report a client's real IP or HTTPS state via forwarding headers; anyone else's headers are ignored.

Login attempts are rate-limited per IP: after 5 failed attempts within a minute, further attempts are refused for the rest of that minute.

## Installation {#install}
Run `sudo scripts/install.sh` on the target machine and choose **native** (installs a systemd service directly) or **docker** (writes a `docker-compose.yaml` + `.env` and runs it via Docker Compose). Either way it asks for your downloads/media paths and the port to listen on.

For the database, native mode offers **sqlite** or an **existing MariaDB/MySQL** server you already run. Docker mode offers the same two plus a third: **bundled** — a MariaDB container the wizard sets up and manages alongside xirc, with generated passwords.

Full walkthrough, including remote deploy and reverse-proxy options: [install.md](https://github.com/RealDtx/maxwell-xirc/blob/master/docs/install.md).
