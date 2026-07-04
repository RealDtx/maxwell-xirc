# Downloads view: date column, sort, and text filter

## Problem

The Downloads view (`web/index.html`, `activeView === 'downloads'`) only supports
filtering by status (All/Queued/Downloading/Completed/Failed tabs). There is no
visible "date added" column and no way to sort or text-filter the list. The
search-results table already has column sorting (`sortBy`/`sortIndicator` +
`searchSort` state in `web/js/app.js`); downloads has no equivalent.

The backend (`db.SQLiteStore.GetDownloads` / `MySQLStore.GetDownloads`) already
returns rows `ORDER BY created_at DESC`, so "newest first" is already the
natural order delivered by the API — the default sort just needs to preserve
that behavior client-side.

## Scope

Frontend-only change (`web/index.html`, `web/js/app.js`). No API/DB changes:
`created_at`, `filename`, `bot_nick`, `filesize`, `speed`, `average_speed`,
progress inputs (`bytes_received`/`total_size`) are already present in the
`Download` payload returned by `getDownloads()`.

## Changes

### 1. Date column

Add a new "Date" column to the downloads table in `web/index.html` (single
render site, `x-for="dl in filteredDownloads()"` around line 865). Cell
renders `this.fmtDate(dl.created_at)` (existing helper, already used in
`dlTimeTooltip`). The existing "Time" column (ETA while downloading / duration
once completed / relative time otherwise, via `dlTimeInfo`) is unchanged and
keeps its tooltip.

### 2. Sortable column headers

Add `dlSort: { col: 'created_at', dir: 'desc' }` to the Alpine data alongside
the existing `downloadFilter: 'all'`.

Add two methods mirroring the existing `sortBy`/`sortIndicator` pair used for
search results, but operating on `dlSort`:

- `dlSortBy(col)` — toggle `asc`/`desc` if already sorted on `col`, else set
  `{ col, dir: 'asc' }`... except the initial default must stay
  `{ col: 'created_at', dir: 'desc' }` so the page loads newest-first without
  requiring a click.
- `dlSortIndicator(col)` — returns `' ▲'`/`' ▼'`/`''` same as the search-results
  version.

Sortable columns and their comparison values (a small `dlSortValue(dl, col)`
helper avoids duplicating per-column logic in the comparator):

| Header    | col key      | value extractor                                                          |
|-----------|--------------|---------------------------------------------------------------------------|
| Date      | `created_at` | `dl.created_at`                                                           |
| Filename  | `filename`   | `dl.filename \|\| ''`                                                     |
| Bot       | `bot_nick`   | `dl.bot_nick \|\| ''`                                                     |
| Size      | `filesize`   | `dl.filesize \|\| dl.total_size \|\| 0`                                   |
| Progress  | `progress`   | `this.downloadProgress(dl)` (existing method, 0–100)                      |
| Speed     | `speed`      | downloading → `dl.speed\|\|0`; completed → `dl.average_speed\|\|0`; else `0` |

Comparator: numeric subtraction if both values are numbers, else
locale-compare on strings — same pattern as the existing search-results sort
in `displaySearchRows()`.

The "Time" and "Status" columns are not independently sortable (Time is a
derived display of Date/Progress/Speed already covered above; Status sorting
is redundant with the status filter tabs already on this view).

### 3. Text filter

Add `downloadSearch: ''` to Alpine data. Add a small text input next to the
status tabs (reuse `.search-bar input` styling already used for the global
search bar). Matches case-insensitively against `filename` or `bot_nick`
substrings.

### 4. `filteredDownloads()`

Currently: status filter only. New pipeline: status filter → text filter →
sort via `dlSortValue`/`dlSort`. Order matters: filter narrows the array before
sorting (cheaper, and keeps behavior identical when the search box is empty).

## Out of scope

- No backend/API changes.
- No date-range filtering (only text search on filename/bot, per user
  decision during brainstorming).
- No persistence of sort/filter state across page reloads.
