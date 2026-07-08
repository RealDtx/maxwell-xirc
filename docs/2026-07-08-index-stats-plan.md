# Index Stats Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface per-channel and per-bot statistics about the passive search index (counts, advertised volume, freshness, observed download bandwidth) in the index search view and on the stats page.

**Architecture:** Extend `dcc.ParseSize` for single-letter suffixes; add `total_bots`/`total_channels` to the existing `/api/index/stats`; add a new global `GET /api/index/stats/detail` backed by one `db.Store` method per store (two identical ANSI GROUP BY queries) feeding one shared Go rollup helper in the `db` package. Frontend: extend the index-search stats row and add a "Search Index" section with two tables to the stats page.

**Tech Stack:** Go (build via `make build`, test via `make test` — never `/usr/bin/go`; direct invocation `~/go-install/go/bin/go`), SQLite (modernc.org/sqlite) + MySQL (go-sql-driver, `parseTime=true`), Alpine.js SPA in `web/`.

**Spec:** `docs/2026-07-08-index-stats-design.md`

## Global Constraints

- Build/test only via `make build` / `make test` (Makefile points at `~/go-install/go/bin/go`; system Go 1.13 must not be used).
- Do **not** start the maxwell-irc server; the user runs it manually on port 8085.
- Every `db.Store` interface addition must be implemented in **both** `db/sqlite.go` and `db/mysql.go` with identical SQL strings.
- Timestamps in the new detail payload are **strings** (SQLite returns `"2006-01-02 15:04:05"`, MySQL RFC3339 via database/sql's time.Time→string conversion); both formats are lexicographically sortable and parse in JS `new Date(...)`.
- JSON arrays must serialize as `[]`, never `null` (initialize slices).

---

### Task 1: ParseSize single-letter suffixes

**Files:**
- Modify: `dcc/disk.go:32-40` (multipliers table)
- Test: `dcc/disk_test.go` (append new test func; `TestParseSize` already exists at line 32 — do not rename it)

**Interfaces:**
- Consumes: existing `dcc.ParseSize(s string) (int64, error)`.
- Produces: `ParseSize` additionally accepts `"1.4G"`, `"700M"`, `"2T"`, `"512K"` (case-insensitive). Later tasks call it from the `db` package.

- [ ] **Step 1: Write the failing test**

Append to `dcc/disk_test.go`:

```go
func TestParseSize_SingleLetterSuffixes(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"1.4G", int64(1.4 * float64(1<<30))},
		{"700M", 700 * (1 << 20)},
		{"2T", 2 * (1 << 40)},
		{"512K", 512 * (1 << 10)},
		{"1.4g", int64(1.4 * float64(1<<30))},
		{"1GB", 1 << 30}, // two-letter form still works
	}
	for _, c := range cases {
		got, err := ParseSize(c.in)
		if err != nil {
			t.Errorf("ParseSize(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
	if _, err := ParseSize(""); err == nil {
		t.Error("ParseSize(\"\") should still error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `~/go-install/go/bin/go test ./dcc/ -run TestParseSize_SingleLetterSuffixes -v`
Expected: FAIL with `ParseSize("1.4G") error: invalid size: 1.4G`

- [ ] **Step 3: Extend the multipliers table**

In `dcc/disk.go`, extend the `multipliers` slice (order matters: two-letter suffixes first so `"1GB"` matches `GB`, not `B`; there is no plain-`B` suffix so appending single letters is safe):

```go
	multipliers := []struct {
		suffix string
		mult   int64
	}{
		{"TB", 1 << 40},
		{"GB", 1 << 30},
		{"MB", 1 << 20},
		{"KB", 1 << 10},
		// Single-letter forms as used in XDCC ads ("1.4G", "700M").
		{"T", 1 << 40},
		{"G", 1 << 30},
		{"M", 1 << 20},
		{"K", 1 << 10},
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `~/go-install/go/bin/go test ./dcc/ -v`
Expected: all PASS (including the pre-existing `TestParseSize`)

- [ ] **Step 5: Commit**

```bash
git add dcc/disk.go dcc/disk_test.go
git commit -m "feat(dcc): ParseSize accepts single-letter size suffixes (1.4G)"
```

---

### Task 2: total_bots / total_channels in IndexStats

**Files:**
- Modify: `db/models.go:150-152` (`IndexStats` struct)
- Modify: `db/sqlite.go:641-653` (`GetIndexStats`)
- Modify: `db/mysql.go:794-806` (`GetIndexStats`)
- Test: `db/sqlite_test.go` (append new test func)

**Interfaces:**
- Consumes: existing `Store.GetIndexStats(serverID int64) (*IndexStats, error)` (signature unchanged).
- Produces: `db.IndexStats{TotalFiles, TotalBots, TotalChannels int64}` with JSON fields `total_files`, `total_bots`, `total_channels`. Task 6 reads the two new JSON fields.

- [ ] **Step 1: Write the failing test**

Append to `db/sqlite_test.go` (uses the existing `newTestSQLiteStore` helper from the top of that file):

```go
func TestSQLiteStore_GetIndexStats_BotAndChannelCounts(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	size := "1.4G"
	seed := []IndexedFile{
		{ServerID: srv.ID, Channel: "#a", BotNick: "bot1", Filename: "f1.mkv", Filesize: &size, RawLine: "r"},
		{ServerID: srv.ID, Channel: "#a", BotNick: "bot2", Filename: "f2.mkv", Filesize: &size, RawLine: "r"},
		{ServerID: srv.ID, Channel: "#b", BotNick: "bot1", Filename: "f3.mkv", Filesize: &size, RawLine: "r"},
	}
	for i := range seed {
		if err := store.UpsertIndexedFile(&seed[i]); err != nil {
			t.Fatalf("UpsertIndexedFile failed: %v", err)
		}
	}

	stats, err := store.GetIndexStats(0)
	if err != nil {
		t.Fatalf("GetIndexStats failed: %v", err)
	}
	if stats.TotalFiles != 3 {
		t.Errorf("TotalFiles = %d, want 3", stats.TotalFiles)
	}
	if stats.TotalBots != 2 {
		t.Errorf("TotalBots = %d, want 2", stats.TotalBots)
	}
	if stats.TotalChannels != 2 {
		t.Errorf("TotalChannels = %d, want 2", stats.TotalChannels)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `~/go-install/go/bin/go test ./db/ -run TestSQLiteStore_GetIndexStats_BotAndChannelCounts -v`
Expected: FAIL to compile — `stats.TotalBots undefined`

- [ ] **Step 3: Extend model and both stores**

`db/models.go` — replace the `IndexStats` struct:

```go
type IndexStats struct {
	TotalFiles    int64 `json:"total_files"`
	TotalBots     int64 `json:"total_bots"`
	TotalChannels int64 `json:"total_channels"`
}
```

`db/sqlite.go` — replace the body of `GetIndexStats`:

```go
func (s *SQLiteStore) GetIndexStats(serverID int64) (*IndexStats, error) {
	var stats IndexStats
	var err error
	const q = "SELECT COUNT(*), COUNT(DISTINCT bot_nick), COUNT(DISTINCT channel) FROM indexed_files"
	if serverID != 0 {
		err = s.db.QueryRow(q+" WHERE server_id=?", serverID).Scan(&stats.TotalFiles, &stats.TotalBots, &stats.TotalChannels)
	} else {
		err = s.db.QueryRow(q).Scan(&stats.TotalFiles, &stats.TotalBots, &stats.TotalChannels)
	}
	if err != nil {
		return nil, err
	}
	return &stats, nil
}
```

`db/mysql.go` — replace the body of `GetIndexStats` with **exactly the same code** (only the receiver differs: `func (s *MySQLStore) GetIndexStats(...)`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add db/models.go db/sqlite.go db/mysql.go db/sqlite_test.go
git commit -m "feat(index): add bot and channel counts to index stats"
```

---

### Task 3: Detail models + shared rollup helper

**Files:**
- Create: `db/indexstats.go`
- Test: `db/indexstats_test.go` (new file; pure in-memory test, no DB)

**Interfaces:**
- Consumes: `dcc.ParseSize` from Task 1 (`db` may import `dcc`; `dcc` is a leaf package, no cycle).
- Produces (used by Tasks 4–5):
  - `db.IndexChannelStats`, `db.IndexBotStats`, `db.IndexStatsDetail` (JSON shapes below)
  - `rollupIndexStats(files []indexFileAggRow, transfers []botTransferAggRow) *IndexStatsDetail` (package-private)
  - row types `indexFileAggRow{ServerID int64; Channel, BotNick, Filesize string; Count int64; LastSeen string}` and `botTransferAggRow{ServerID int64; Channel, BotNick string; Transfers int64; AvgSpeed float64; PeakSpeed int64}`

- [ ] **Step 1: Write the failing test**

Create `db/indexstats_test.go`:

```go
package db

import "testing"

func TestRollupIndexStats(t *testing.T) {
	files := []indexFileAggRow{
		{ServerID: 1, Channel: "#a", BotNick: "bot1", Filesize: "1G", Count: 2, LastSeen: "2026-07-01 10:00:00"},
		{ServerID: 1, Channel: "#a", BotNick: "bot1", Filesize: "512M", Count: 1, LastSeen: "2026-07-02 10:00:00"},
		{ServerID: 1, Channel: "#a", BotNick: "bot2", Filesize: "", Count: 5, LastSeen: "2026-07-03 10:00:00"},
		{ServerID: 2, Channel: "#b", BotNick: "bot3", Filesize: "junk", Count: 1, LastSeen: "2026-07-04 10:00:00"},
	}
	transfers := []botTransferAggRow{
		{ServerID: 1, Channel: "#a", BotNick: "bot1", Transfers: 3, AvgSpeed: 1048576.5, PeakSpeed: 2097152},
		// transfer for a bot no longer in the index: must be ignored
		{ServerID: 9, Channel: "#gone", BotNick: "ghost", Transfers: 1, AvgSpeed: 1, PeakSpeed: 1},
	}

	d := rollupIndexStats(files, transfers)

	if len(d.Bots) != 3 {
		t.Fatalf("len(Bots) = %d, want 3", len(d.Bots))
	}
	// Sorted by files desc: bot2 (5), bot1 (3), bot3 (1).
	b := d.Bots[1]
	if b.BotNick != "bot1" || b.Files != 3 {
		t.Fatalf("Bots[1] = %+v, want bot1 with 3 files", b)
	}
	wantBytes := int64(2*(1<<30)) + 512*(1<<20)
	if b.AdvertisedBytes != wantBytes {
		t.Errorf("bot1 AdvertisedBytes = %d, want %d", b.AdvertisedBytes, wantBytes)
	}
	if b.LastSeenAt != "2026-07-02 10:00:00" {
		t.Errorf("bot1 LastSeenAt = %q", b.LastSeenAt)
	}
	if b.Transfers != 3 || b.AvgSpeed != 1048576 || b.PeakSpeed != 2097152 {
		t.Errorf("bot1 transfer stats = %+v", b)
	}
	if d.Bots[0].Transfers != 0 || d.Bots[0].AvgSpeed != 0 {
		t.Errorf("bot2 should have zero transfer stats, got %+v", d.Bots[0])
	}

	if len(d.Channels) != 2 {
		t.Fatalf("len(Channels) = %d, want 2", len(d.Channels))
	}
	// Sorted by files desc: #a (8), #b (1).
	c := d.Channels[0]
	if c.Channel != "#a" || c.Bots != 2 || c.Files != 8 {
		t.Fatalf("Channels[0] = %+v, want #a with 2 bots / 8 files", c)
	}
	if c.AdvertisedBytes != wantBytes { // bot2 ("") and unparseable contribute 0
		t.Errorf("#a AdvertisedBytes = %d, want %d", c.AdvertisedBytes, wantBytes)
	}
	if c.LastSeenAt != "2026-07-03 10:00:00" {
		t.Errorf("#a LastSeenAt = %q", c.LastSeenAt)
	}
}

func TestRollupIndexStats_Empty(t *testing.T) {
	d := rollupIndexStats(nil, nil)
	if d.Channels == nil || d.Bots == nil {
		t.Fatal("empty rollup must return empty slices, not nil (JSON [] not null)")
	}
	if len(d.Channels) != 0 || len(d.Bots) != 0 {
		t.Fatalf("expected empty result, got %+v", d)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `~/go-install/go/bin/go test ./db/ -run TestRollupIndexStats -v`
Expected: FAIL to compile — `undefined: indexFileAggRow`

- [ ] **Step 3: Create `db/indexstats.go`**

```go
package db

import (
	"sort"

	"github.com/RealDtx/maxwell-irc/dcc"
)

// IndexChannelStats aggregates the persistent index per download channel.
type IndexChannelStats struct {
	ServerID        int64  `json:"server_id"`
	Channel         string `json:"channel"`
	Bots            int    `json:"bots"`
	Files           int64  `json:"files"`
	AdvertisedBytes int64  `json:"advertised_bytes"`
	LastSeenAt      string `json:"last_seen_at"`
}

// IndexBotStats aggregates the persistent index per bot, enriched with
// bandwidth observed during completed downloads (zero = never downloaded).
type IndexBotStats struct {
	ServerID        int64  `json:"server_id"`
	Channel         string `json:"channel"`
	BotNick         string `json:"bot_nick"`
	Files           int64  `json:"files"`
	AdvertisedBytes int64  `json:"advertised_bytes"`
	LastSeenAt      string `json:"last_seen_at"`
	Transfers       int64  `json:"transfers"`
	AvgSpeed        int64  `json:"avg_speed"`
	PeakSpeed       int64  `json:"peak_speed"`
}

type IndexStatsDetail struct {
	Channels []IndexChannelStats `json:"channels"`
	Bots     []IndexBotStats     `json:"bots"`
}

// indexFileAggRow is one scanned row of:
//
//	SELECT server_id, channel, bot_nick, COALESCE(filesize,''), COUNT(*), MAX(last_seen_at)
//	FROM indexed_files GROUP BY server_id, channel, bot_nick, filesize
//
// LastSeen stays a string: modernc/sqlite returns TEXT for expression columns,
// go-sql-driver's time.Time converts to RFC3339 via database/sql. Both formats
// are lexicographically ordered, so string max is correct per store.
type indexFileAggRow struct {
	ServerID int64
	Channel  string
	BotNick  string
	Filesize string
	Count    int64
	LastSeen string
}

// botTransferAggRow is one scanned row of:
//
//	SELECT server_id, channel, bot_nick, COUNT(*), COALESCE(AVG(average_speed),0), COALESCE(MAX(peak_speed),0)
//	FROM downloads WHERE status='completed' GROUP BY server_id, channel, bot_nick
type botTransferAggRow struct {
	ServerID  int64
	Channel   string
	BotNick   string
	Transfers int64
	AvgSpeed  float64
	PeakSpeed int64
}

type botKey struct {
	serverID int64
	channel  string
	botNick  string
}

// rollupIndexStats folds the pre-grouped rows into per-bot and per-channel
// aggregates. Advertised sizes are parsed from the raw ad strings ("1.4G");
// empty or unparseable sizes contribute 0 bytes.
func rollupIndexStats(files []indexFileAggRow, transfers []botTransferAggRow) *IndexStatsDetail {
	bots := map[botKey]*IndexBotStats{}
	for _, r := range files {
		k := botKey{r.ServerID, r.Channel, r.BotNick}
		b := bots[k]
		if b == nil {
			b = &IndexBotStats{ServerID: r.ServerID, Channel: r.Channel, BotNick: r.BotNick}
			bots[k] = b
		}
		b.Files += r.Count
		if bytes, err := dcc.ParseSize(r.Filesize); err == nil {
			b.AdvertisedBytes += bytes * r.Count
		}
		if r.LastSeen > b.LastSeenAt {
			b.LastSeenAt = r.LastSeen
		}
	}

	for _, tr := range transfers {
		if b := bots[botKey{tr.ServerID, tr.Channel, tr.BotNick}]; b != nil {
			b.Transfers = tr.Transfers
			b.AvgSpeed = int64(tr.AvgSpeed)
			b.PeakSpeed = tr.PeakSpeed
		}
	}

	channels := map[botKey]*IndexChannelStats{} // botNick left empty in key
	for _, b := range bots {
		k := botKey{serverID: b.ServerID, channel: b.Channel}
		c := channels[k]
		if c == nil {
			c = &IndexChannelStats{ServerID: b.ServerID, Channel: b.Channel}
			channels[k] = c
		}
		c.Bots++
		c.Files += b.Files
		c.AdvertisedBytes += b.AdvertisedBytes
		if b.LastSeenAt > c.LastSeenAt {
			c.LastSeenAt = b.LastSeenAt
		}
	}

	d := &IndexStatsDetail{Channels: []IndexChannelStats{}, Bots: []IndexBotStats{}}
	for _, b := range bots {
		d.Bots = append(d.Bots, *b)
	}
	for _, c := range channels {
		d.Channels = append(d.Channels, *c)
	}
	sort.Slice(d.Bots, func(i, j int) bool {
		if d.Bots[i].Files != d.Bots[j].Files {
			return d.Bots[i].Files > d.Bots[j].Files
		}
		return d.Bots[i].BotNick < d.Bots[j].BotNick
	})
	sort.Slice(d.Channels, func(i, j int) bool {
		if d.Channels[i].Files != d.Channels[j].Files {
			return d.Channels[i].Files > d.Channels[j].Files
		}
		return d.Channels[i].Channel < d.Channels[j].Channel
	})
	return d
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `~/go-install/go/bin/go test ./db/ -run TestRollupIndexStats -v`
Expected: PASS (both tests)

- [ ] **Step 5: Commit**

```bash
git add db/indexstats.go db/indexstats_test.go
git commit -m "feat(index): stats-detail models and shared rollup helper"
```

---

### Task 4: GetIndexStatsDetail store method (SQLite + MySQL + interface)

**Files:**
- Modify: `db/store.go` (interface, after the `ClearIndex` entry around line 84)
- Modify: `db/sqlite.go` (new method after `ClearIndex`, around line 663)
- Modify: `db/mysql.go` (new method after `ClearIndex`, around line 816)
- Test: `db/sqlite_test.go` (append integration test)

**Interfaces:**
- Consumes: `rollupIndexStats`, `indexFileAggRow`, `botTransferAggRow` from Task 3.
- Produces: `Store.GetIndexStatsDetail() (*IndexStatsDetail, error)` — used by the handler in Task 5.

- [ ] **Step 1: Write the failing test**

Append to `db/sqlite_test.go` (tests live in package `db`, so direct `store.db.Exec` is available to flip download rows to completed):

```go
func TestSQLiteStore_GetIndexStatsDetail(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	size := "1G"
	seed := []IndexedFile{
		{ServerID: srv.ID, Channel: "#a", BotNick: "bot1", Filename: "f1.mkv", Filesize: &size, RawLine: "r"},
		{ServerID: srv.ID, Channel: "#a", BotNick: "bot1", Filename: "f2.mkv", Filesize: &size, RawLine: "r"},
		{ServerID: srv.ID, Channel: "#a", BotNick: "bot2", Filename: "f3.mkv", RawLine: "r"}, // no size
	}
	for i := range seed {
		if err := store.UpsertIndexedFile(&seed[i]); err != nil {
			t.Fatalf("UpsertIndexedFile failed: %v", err)
		}
	}

	dl := &Download{ServerID: srv.ID, Channel: "#a", BotNick: "bot1", PackNumber: 1, Filename: "f1.mkv", Status: "queued"}
	if err := store.CreateDownload(dl); err != nil {
		t.Fatalf("CreateDownload failed: %v", err)
	}
	if _, err := store.db.Exec(
		"UPDATE downloads SET status='completed', average_speed=1000, peak_speed=2000 WHERE id=?", dl.ID); err != nil {
		t.Fatalf("failed to mark download completed: %v", err)
	}

	d, err := store.GetIndexStatsDetail()
	if err != nil {
		t.Fatalf("GetIndexStatsDetail failed: %v", err)
	}

	if len(d.Channels) != 1 || d.Channels[0].Bots != 2 || d.Channels[0].Files != 3 {
		t.Fatalf("Channels = %+v, want one #a row with 2 bots / 3 files", d.Channels)
	}
	if d.Channels[0].AdvertisedBytes != 2*(1<<30) {
		t.Errorf("AdvertisedBytes = %d, want %d", d.Channels[0].AdvertisedBytes, 2*(1<<30))
	}
	if d.Channels[0].LastSeenAt == "" {
		t.Error("channel LastSeenAt is empty")
	}

	if len(d.Bots) != 2 {
		t.Fatalf("len(Bots) = %d, want 2", len(d.Bots))
	}
	// Sorted by files desc: bot1 (2 files) first.
	if d.Bots[0].BotNick != "bot1" || d.Bots[0].Transfers != 1 ||
		d.Bots[0].AvgSpeed != 1000 || d.Bots[0].PeakSpeed != 2000 {
		t.Errorf("Bots[0] = %+v, want bot1 with 1 transfer @ 1000/2000", d.Bots[0])
	}
	if d.Bots[1].BotNick != "bot2" || d.Bots[1].Transfers != 0 {
		t.Errorf("Bots[1] = %+v, want bot2 with no transfers", d.Bots[1])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `~/go-install/go/bin/go test ./db/ -run TestSQLiteStore_GetIndexStatsDetail -v`
Expected: FAIL to compile — `store.GetIndexStatsDetail undefined`

- [ ] **Step 3: Add interface entry and both implementations**

`db/store.go`, directly after the `ClearIndex(serverID int64) error` line:

```go
	// GetIndexStatsDetail returns per-channel and per-bot aggregates over
	// the persistent index, enriched with bandwidth observed during
	// completed downloads. Global (all servers); rows carry server_id.
	GetIndexStatsDetail() (*IndexStatsDetail, error)
```

`db/sqlite.go`, after `ClearIndex`:

```go
func (s *SQLiteStore) GetIndexStatsDetail() (*IndexStatsDetail, error) {
	return queryIndexStatsDetail(s.db)
}
```

`db/mysql.go`, after `ClearIndex`:

```go
func (s *MySQLStore) GetIndexStatsDetail() (*IndexStatsDetail, error) {
	return queryIndexStatsDetail(s.db)
}
```

Append the shared query function to `db/indexstats.go` (both stores hold a `*sql.DB` field named `db`; the SQL is ANSI and identical for both engines — add `"database/sql"` to that file's imports):

```go
// queryIndexStatsDetail runs the two aggregate queries and rolls them up.
// The SQL is identical for SQLite and MySQL, so both stores share it.
func queryIndexStatsDetail(dbc *sql.DB) (*IndexStatsDetail, error) {
	rows, err := dbc.Query(`SELECT server_id, channel, bot_nick, COALESCE(filesize,''), COUNT(*), MAX(last_seen_at)
		FROM indexed_files GROUP BY server_id, channel, bot_nick, filesize`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []indexFileAggRow
	for rows.Next() {
		var r indexFileAggRow
		if err := rows.Scan(&r.ServerID, &r.Channel, &r.BotNick, &r.Filesize, &r.Count, &r.LastSeen); err != nil {
			return nil, err
		}
		files = append(files, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	dlRows, err := dbc.Query(`SELECT server_id, channel, bot_nick, COUNT(*), COALESCE(AVG(average_speed),0), COALESCE(MAX(peak_speed),0)
		FROM downloads WHERE status='completed' GROUP BY server_id, channel, bot_nick`)
	if err != nil {
		return nil, err
	}
	defer dlRows.Close()
	var transfers []botTransferAggRow
	for dlRows.Next() {
		var r botTransferAggRow
		if err := dlRows.Scan(&r.ServerID, &r.Channel, &r.BotNick, &r.Transfers, &r.AvgSpeed, &r.PeakSpeed); err != nil {
			return nil, err
		}
		transfers = append(transfers, r)
	}
	if err := dlRows.Err(); err != nil {
		return nil, err
	}

	return rollupIndexStats(files, transfers), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test`
Expected: all PASS (this also proves both stores still satisfy the `Store` interface — a missing method is a compile error)

- [ ] **Step 5: Commit**

```bash
git add db/store.go db/sqlite.go db/mysql.go db/indexstats.go db/sqlite_test.go
git commit -m "feat(index): GetIndexStatsDetail store method (SQLite + MySQL)"
```

---

### Task 5: HTTP handler and route

**Files:**
- Modify: `server/search_handlers.go` (new handler after `handleIndexStats`, around line 685)
- Modify: `server/server.go:77` (route block "Index search endpoints")

**Interfaces:**
- Consumes: `Store.GetIndexStatsDetail()` from Task 4; existing `writeJSON`/`writeError` helpers in the server package.
- Produces: `GET /api/index/stats/detail` → 200 with `db.IndexStatsDetail` JSON (`{"channels":[…],"bots":[…]}`), 405 on non-GET, 500 JSON on store error. Task 7's `api.getIndexStatsDetail()` calls it.

There is no HTTP-handler test suite in this repo (checked: `ls server/*_test.go` finds none) — the handler is 10 lines of glue over the store method tested in Task 4, so verification is `make build` plus the user exercising the UI.

- [ ] **Step 1: Add the handler**

In `server/search_handlers.go`, after `handleIndexStats` (line 684):

```go
// handleIndexStatsDetail handles GET /api/index/stats/detail — per-channel
// and per-bot aggregates over the persistent index, plus bandwidth observed
// during completed downloads. Global across servers.
func (s *Server) handleIndexStatsDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	detail, err := s.store.GetIndexStatsDetail()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail)
}
```

- [ ] **Step 2: Register the route**

In `server/server.go`, inside the "Index search endpoints" block, after the `/api/index/stats` line (exact-path patterns don't overlap, so both registrations coexist):

```go
	s.mux.HandleFunc("/api/index/stats/detail", s.handleIndexStatsDetail)
```

- [ ] **Step 3: Build**

Run: `make build`
Expected: builds cleanly, binary `maxwell-irc` produced

- [ ] **Step 4: Commit**

```bash
git add server/search_handlers.go server/server.go
git commit -m "feat(api): GET /api/index/stats/detail endpoint"
```

---

### Task 6: Search-view stats row shows bots and channels

**Files:**
- Modify: `web/index.html` (the index-mode stats row, currently around line 279)

**Interfaces:**
- Consumes: `indexStats.total_bots` / `indexStats.total_channels` from Task 2 (already loaded via the existing `loadIndexStats()`; no JS change needed).
- Produces: nothing downstream.

- [ ] **Step 1: Extend the row text**

In `web/index.html`, replace:

```html
<span x-text="(indexStats.total_files || 0) + ' file(s) indexed'"></span>
```

with:

```html
<span x-text="(indexStats.total_files || 0) + ' file(s) · ' + (indexStats.total_bots || 0) + ' bot(s) · ' + (indexStats.total_channels || 0) + ' channel(s)'"></span>
```

- [ ] **Step 2: Verify**

Run: `node --check web/js/app.js` (unchanged, but cheap) and visually confirm the HTML edit landed: `grep -n "total_bots" web/index.html`
Expected: one match in the index-mode stats row.

- [ ] **Step 3: Commit**

```bash
git add web/index.html
git commit -m "feat(ui): show bot and channel counts in index search stats row"
```

---

### Task 7: Stats page "Search Index" section

**Files:**
- Modify: `web/js/api.js` (after `clearIndex`, around line 105)
- Modify: `web/js/app.js` (state around line 141, `loadStats()` around line 1630)
- Modify: `web/index.html` (stats view, after the history "Load more" button around line 1561, inside the `x-show="activeView === 'stats'"` div)

**Interfaces:**
- Consumes: `GET /api/index/stats/detail` from Task 5; existing helpers `formatSize`, `formatSpeed`, `formatDate` (web/js/utils.js), `getServerName(serverId)` (app.js).
- Produces: nothing downstream.

- [ ] **Step 1: API helper**

In `web/js/api.js`, after the `clearIndex` entry:

```js
    getIndexStatsDetail() {
        return this.get('/index/stats/detail');
    },
```

- [ ] **Step 2: State + loader**

In `web/js/app.js`, after `statsHistoryOffset: 0,` (line 141):

```js
        indexDetail: null,
```

In `loadStats()`, extend the `Promise.all` to three loads:

```js
        async loadStats() {
            this.statsLoading = true;
            try {
                const [summary, history, indexDetail] = await Promise.all([
                    api.getDownloadStats(),
                    api.getDownloadHistory(0, 50),
                    api.getIndexStatsDetail(),
                ]);
                this.statsSummary = summary;
                this.statsHistory = Array.isArray(history) ? history : [];
                this.statsHistoryOffset = 50;
                this.indexDetail = indexDetail;
            } catch(e) {
                console.error('loadStats', e);
            } finally {
                this.statsLoading = false;
            }
        },
```

- [ ] **Step 3: Tables**

In `web/index.html`, after the history "Load more" button (`loadMoreHistory()`, line ~1561) but **inside** the stats view div, add:

```html
            <!-- Search index stats -->
            <h3 class="mt-3 mb-2">Search Index</h3>
            <template x-if="indexDetail && indexDetail.channels.length === 0">
                <div class="text-muted text-sm">Index is empty.</div>
            </template>
            <template x-if="indexDetail && indexDetail.channels.length > 0">
                <div>
                    <div style="overflow-x:auto;">
                        <table class="w-full text-sm mb-4">
                            <thead>
                                <tr class="text-muted border-b">
                                    <th class="text-left pb-2 pr-3">Server</th>
                                    <th class="text-left pb-2 pr-3">Channel</th>
                                    <th class="text-left pb-2 pr-3">Bots</th>
                                    <th class="text-left pb-2 pr-3">Files</th>
                                    <th class="text-left pb-2 pr-3">Advertised</th>
                                    <th class="text-left pb-2">Last activity</th>
                                </tr>
                            </thead>
                            <tbody>
                                <template x-for="c in indexDetail.channels" :key="c.server_id + c.channel">
                                    <tr class="border-b hover-row">
                                        <td class="py-1 pr-3 text-xs" x-text="getServerName(c.server_id)"></td>
                                        <td class="py-1 pr-3 text-xs" x-text="c.channel"></td>
                                        <td class="py-1 pr-3 text-xs" x-text="c.bots"></td>
                                        <td class="py-1 pr-3 text-xs" x-text="c.files"></td>
                                        <td class="py-1 pr-3 text-xs" x-text="formatSize(c.advertised_bytes)"></td>
                                        <td class="py-1 text-muted text-xs" x-text="formatDate(c.last_seen_at)"></td>
                                    </tr>
                                </template>
                            </tbody>
                        </table>
                    </div>
                    <div style="overflow-x:auto;">
                        <table class="w-full text-sm">
                            <thead>
                                <tr class="text-muted border-b">
                                    <th class="text-left pb-2 pr-3">Server</th>
                                    <th class="text-left pb-2 pr-3">Channel</th>
                                    <th class="text-left pb-2 pr-3">Bot</th>
                                    <th class="text-left pb-2 pr-3">Files</th>
                                    <th class="text-left pb-2 pr-3">Advertised</th>
                                    <th class="text-left pb-2 pr-3">Transfers</th>
                                    <th class="text-left pb-2 pr-3">Avg speed</th>
                                    <th class="text-left pb-2 pr-3">Peak speed</th>
                                    <th class="text-left pb-2">Last seen</th>
                                </tr>
                            </thead>
                            <tbody>
                                <template x-for="b in indexDetail.bots" :key="b.server_id + b.channel + b.bot_nick">
                                    <tr class="border-b hover-row">
                                        <td class="py-1 pr-3 text-xs" x-text="getServerName(b.server_id)"></td>
                                        <td class="py-1 pr-3 text-xs" x-text="b.channel"></td>
                                        <td class="py-1 pr-3 text-xs font-mono" x-text="b.bot_nick"></td>
                                        <td class="py-1 pr-3 text-xs" x-text="b.files"></td>
                                        <td class="py-1 pr-3 text-xs" x-text="formatSize(b.advertised_bytes)"></td>
                                        <td class="py-1 pr-3 text-xs" x-text="b.transfers || '—'"></td>
                                        <td class="py-1 pr-3 text-xs" x-text="formatSpeed(b.avg_speed)"></td>
                                        <td class="py-1 pr-3 text-xs" x-text="formatSpeed(b.peak_speed)"></td>
                                        <td class="py-1 text-muted text-xs" x-text="formatDate(b.last_seen_at)"></td>
                                    </tr>
                                </template>
                            </tbody>
                        </table>
                    </div>
                </div>
            </template>
```

Notes: `formatSpeed(0)` already renders `'-'`, covering never-downloaded bots. `formatSize(0)` renders `'0 B'`.

- [ ] **Step 4: Verify**

Run: `node --check web/js/app.js && node --check web/js/api.js`
Expected: both exit 0.

Then ask the user to start the server and check: stats page shows the two tables with real data from `data/xirc.db` (45K indexed files exist); index search view row shows files/bots/channels. Do **not** start the server yourself.

- [ ] **Step 5: Commit**

```bash
git add web/js/api.js web/js/app.js web/index.html
git commit -m "feat(ui): Search Index section with channel and bot tables on stats page"
```
