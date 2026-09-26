# Index Stats — Design

**Date:** 2026-07-08
**Status:** Approved

## Goal

Surface statistics about the passively collected search index: bots per channel,
files per channel, files per bot, advertised catalog volume, freshness, and
per-bot bandwidth observed during actual downloads.

Two surfaces:

1. **Index search view** — extend the existing one-line stats row to
   `N files · M bots · K channels · [Clear index]`.
2. **Stats page** — new "Search Index" section with two tables (channels, bots)
   below the existing download statistics.

## Data sources

- `indexed_files` (server_id, channel, bot_nick, filename, filesize TEXT,
  last_seen_at). Filesize is the raw ad string (`"1.4G"`, `"700M"`, sometimes
  empty). Low cardinality per value (~hundreds of distinct strings across 45K
  rows), which makes GROUP BY on it cheap.
- `downloads` (server_id, channel, bot_nick, status, average_speed,
  peak_speed). Bandwidth facts exist **only** for bots we actually downloaded
  from; the tables render "—" otherwise.

## Backend

### 1. `dcc.ParseSize` extension

Add single-letter suffixes `T`, `G`, `M`, `K` (checked after the existing
two-letter `TB/GB/MB/KB` so those still match first). Backward compatible;
fixes parsing of the ad format for every caller.

### 2. Extend existing `GET /api/index/stats`

SQL gains `COUNT(DISTINCT bot_nick) AS total_bots` and
`COUNT(DISTINCT channel) AS total_channels`. Endpoint, server filter, and
callers unchanged otherwise. `db.IndexStats` gains the two fields.

### 3. New `GET /api/index/stats/detail`

No parameters (global across servers). Response:

```json
{
  "channels": [
    { "server_id": 1, "channel": "#example-dl", "bots": 42, "files": 30000,
      "advertised_bytes": 41099511627776, "last_seen_at": "…" }
  ],
  "bots": [
    { "server_id": 1, "channel": "#example-dl", "bot_nick": "ExampleBot|01",
      "files": 900, "advertised_bytes": 109951162777, "last_seen_at": "…",
      "transfers": 3, "avg_speed": 1048576, "peak_speed": 2097152 }
  ]
}
```

- Both arrays sorted by `files` descending.
- `transfers` = count of `status='completed'` downloads for that
  (server, channel, bot); `avg_speed` = unweighted `AVG(average_speed)` over
  those; `peak_speed` = `MAX(peak_speed)`. All zero when never downloaded from.
- `advertised_bytes` = sum of parsed filesize strings; empty/unparseable
  strings contribute 0.

### 4. Store layer

New `db.Store` interface method: `GetIndexStatsDetail() (*IndexStatsDetail, error)`.

Each store (SQLite, MySQL) runs two identical ANSI-SQL queries:

```sql
SELECT server_id, channel, bot_nick, filesize, COUNT(*), MAX(last_seen_at)
FROM indexed_files GROUP BY server_id, channel, bot_nick, filesize;

SELECT server_id, channel, bot_nick, COUNT(*), AVG(average_speed), MAX(peak_speed)
FROM downloads WHERE status = 'completed' GROUP BY server_id, channel, bot_nick;
```

and feeds the scanned rows to **one shared rollup helper** in the `db` package
(`db` may import `dcc` — no cycle, `dcc` is a leaf). The helper builds the
per-bot rows, merges download aggregates by (server, channel, bot), then folds
bots into per-channel aggregates (bots = distinct count, files/bytes = sums,
last_seen = max).

New models: `IndexChannelStats`, `IndexBotStats`, `IndexStatsDetail`.

### 5. Handler & route

`handleIndexStatsDetail` in `server/search_handlers.go` (GET only, 500 JSON on
store error, same shape as sibling handlers). Route
`/api/index/stats/detail` registered in `server/server.go` **before** the
existing `/api/index/stats` registration comment block for clarity (mux
handles exact paths, order irrelevant).

## Frontend

- `api.getIndexStatsDetail()` in the API helper.
- **Search view row** (`index.html`): text becomes
  `N file(s) · M bot(s) · K channel(s)` from the extended stats response.
- **Stats page**: "Search Index" `h3` section after the download history table:
  - **Channels table**: Server, Channel, Bots, Files, Advertised, Last activity.
  - **Bots table**: Server, Channel, Bot, Files, Advertised, Transfers,
    Avg speed, Peak speed, Last seen. Speed/transfers render "—" when zero.
  - Reuses existing table classes, `formatSize`, `formatDate`; speeds render
    as `formatSize(x) + '/s'`.
  - Loaded by `loadIndexStatsDetail()` wherever the stats view's existing
    loaders are triggered; empty index shows a muted "Index is empty" line.

## Error handling

- Endpoint: 500 with JSON error body (sibling-handler pattern).
- UI: `console.error` + empty tables (sibling-loader pattern).

## Testing

One Go test file covering:

- rollup helper: fabricated file/download agg rows → expected channel/bot
  aggregates (distinct bot count, byte sums, download merge, "—"/zero case);
- `ParseSize`: `"1.4G"`, `"700M"`, `"2T"`, `"512K"`, existing `"1GB"` still
  works, empty string still errors.

## Out of scope

- Server filter on the detail endpoint (stats page is global; tables carry a
  server column).
- Persisting parsed sizes in the DB (parse-at-read is cheap at this
  cardinality).
- Charts/visualizations.
