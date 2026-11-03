package maintenance

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RealDtx/maxwell-irc/config"
	"github.com/RealDtx/maxwell-irc/db"
)

func newTestStore(t *testing.T) (db.Store, func()) {
	t.Helper()
	dir, err := ioutil.TempDir("", "xirc-maintenance-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	store, err := db.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("failed to create store: %v", err)
	}
	if err := store.Migrate(); err != nil {
		store.Close()
		os.RemoveAll(dir)
		t.Fatalf("failed to migrate: %v", err)
	}
	cleanup := func() {
		store.Close()
		os.RemoveAll(dir)
	}
	return store, cleanup
}

// TestMaintenance_StartRunsImmediatePruneAndCap verifies that Start() runs an
// immediate pass that enforces the configured index cap, evicting the
// least-recently-seen entries first.
func TestMaintenance_StartRunsImmediatePruneAndCap(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	srv := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	names := []string{"a.mkv", "b.mkv", "c.mkv", "d.mkv", "e.mkv"}
	for _, name := range names {
		if err := store.UpsertIndexedFile(&db.IndexedFile{ServerID: srv.ID, Channel: "#c", BotNick: "b", Filename: name, RawLine: "r"}); err != nil {
			t.Fatalf("UpsertIndexedFile(%s) failed: %v", name, err)
		}
		// Ensure distinct last_seen_at ordering across entries.
		time.Sleep(5 * time.Millisecond)
	}

	m := New(store, config.MaintenanceConfig{SearchResultRetentionDays: 0, IndexMaxFiles: 3, IntervalHours: 0})
	m.Start()
	defer m.Stop()

	stats, err := store.GetIndexStats(0)
	if err != nil {
		t.Fatalf("GetIndexStats failed: %v", err)
	}
	if stats.TotalFiles != 3 {
		t.Fatalf("expected index capped to 3 files after Start(), got %d", stats.TotalFiles)
	}

	// The two oldest (a.mkv, b.mkv) should have been evicted first.
	remaining, err := store.SearchIndexedFiles("mkv", srv.ID, "", 10)
	if err != nil {
		t.Fatalf("SearchIndexedFiles failed: %v", err)
	}
	for _, r := range remaining {
		if r.Filename == "a.mkv" || r.Filename == "b.mkv" {
			t.Errorf("expected %s to be evicted as least-recently-seen, but it remains", r.Filename)
		}
	}
}

// TestMaintenance_DisabledWhenConfigZero verifies that a zero-value config
// (both retention and cap disabled) leaves existing data untouched.
func TestMaintenance_DisabledWhenConfigZero(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	srv := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}
	if err := store.UpsertIndexedFile(&db.IndexedFile{ServerID: srv.ID, Channel: "#c", BotNick: "b", Filename: "a.mkv", RawLine: "r"}); err != nil {
		t.Fatalf("UpsertIndexedFile failed: %v", err)
	}
	if err := store.CreateSearchResult(&db.SearchResult{ServerID: srv.ID, Channel: "#c", BotNick: "b", RawLine: "raw", SearchQuery: "q", Parsed: false}); err != nil {
		t.Fatalf("CreateSearchResult failed: %v", err)
	}

	m := New(store, config.MaintenanceConfig{SearchResultRetentionDays: 0, IndexMaxFiles: 0, IntervalHours: 0})
	m.Start()
	defer m.Stop()

	stats, err := store.GetIndexStats(0)
	if err != nil {
		t.Fatalf("GetIndexStats failed: %v", err)
	}
	if stats.TotalFiles != 1 {
		t.Fatalf("expected no eviction when IndexMaxFiles=0, got %d files", stats.TotalFiles)
	}

	unparsed, err := store.GetAllUnparsedSince(time.Now().Add(-24 * time.Hour))
	if err != nil {
		t.Fatalf("GetAllUnparsedSince failed: %v", err)
	}
	if len(unparsed) != 1 {
		t.Fatalf("expected no pruning when SearchResultRetentionDays=0, got %d remaining", len(unparsed))
	}
}

func TestRunOnce_PrunesExpiredSessions(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	u := &db.User{Username: "a", PasswordHash: "x", Role: "user", CreatedAt: time.Now()}
	if err := store.CreateUser(u); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if err := store.CreateSession(&db.Session{TokenHash: "old", UserID: u.ID, ExpiresAt: time.Now().Add(-time.Minute)}); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	New(store, config.MaintenanceConfig{}).runOnce()
	if s, _ := store.GetSession("old"); s != nil {
		t.Error("expired session not pruned")
	}
}

func TestSetConfigRestartsTicker(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	m := New(store, config.MaintenanceConfig{IntervalHours: 0})
	m.unit = time.Millisecond // test hook: IntervalHours counts milliseconds
	var runs int32
	m.onRun = func() { atomic.AddInt32(&runs, 1) }
	m.Start()
	defer m.Stop()

	// interval 0: only the immediate pass from Start() should run.
	time.Sleep(20 * time.Millisecond)
	if n := atomic.LoadInt32(&runs); n != 1 {
		t.Fatalf("interval 0: runs = %d, want only the immediate pass", n)
	}

	m.SetConfig(config.MaintenanceConfig{IntervalHours: 5})
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&runs) < 3 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if n := atomic.LoadInt32(&runs); n < 3 {
		t.Fatalf("after SetConfig(5ms): runs = %d, want periodic runs", n)
	}

	m.SetConfig(config.MaintenanceConfig{IntervalHours: 0})
	// Give a pass already in flight room to finish before snapshotting, so
	// it can't land after "before" and produce a false "still ticking".
	time.Sleep(50 * time.Millisecond)
	before := atomic.LoadInt32(&runs)
	time.Sleep(30 * time.Millisecond)
	if atomic.LoadInt32(&runs) != before {
		t.Fatal("interval 0 again: periodic runs continued")
	}
}

// TestSetConfigSameIntervalDoesNotResetTicker verifies that saving settings
// which don't touch IntervalHours doesn't send a ticker-reset signal — a
// live ticker keeps counting toward its next tick instead of being
// postponed by every unrelated settings save.
func TestSetConfigSameIntervalDoesNotResetTicker(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	m := New(store, config.MaintenanceConfig{IntervalHours: 5, SearchResultRetentionDays: 1})

	m.SetConfig(config.MaintenanceConfig{IntervalHours: 5, SearchResultRetentionDays: 2})

	select {
	case <-m.resetCh:
		t.Fatal("SetConfig with unchanged IntervalHours sent a ticker-reset signal")
	default:
	}
	if got := m.config().SearchResultRetentionDays; got != 2 {
		t.Fatalf("SearchResultRetentionDays = %d, want 2 (other fields must still update)", got)
	}

	m.SetConfig(config.MaintenanceConfig{IntervalHours: 7, SearchResultRetentionDays: 2})
	select {
	case <-m.resetCh:
	default:
		t.Fatal("SetConfig with a changed IntervalHours must send a ticker-reset signal")
	}
}

func TestMaintenance_PrunesIndexByRealmTTL(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	srv := &db.Server{Name: "srv", Host: "irc.example.net", Port: 6667, Nickname: "me", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatal(err)
	}
	for _, r := range []db.Realm{
		{ServerID: srv.ID, Name: "#Example-Chat", DownloadChannel: "#example-dl", IndexExpiryDays: 14},
		{ServerID: srv.ID, Name: "#keep", IndexExpiryDays: 0},
	} {
		r := r
		if err := store.CreateRealm(&r); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []db.IndexedFile{
		{ServerID: srv.ID, Channel: "#example-dl", BotNick: "B", Filename: "old.mkv", RawLine: "r"},
		{ServerID: srv.ID, Channel: "#keep", BotNick: "B", Filename: "kept.mkv", RawLine: "r"},
		{ServerID: srv.ID, Channel: "#other", BotNick: "B", Filename: "norealm.mkv", RawLine: "r"},
	} {
		f := f
		if err := store.UpsertIndexedFile(&f); err != nil {
			t.Fatal(err)
		}
	}
	count := func(q string) int {
		r, err := store.SearchIndexedFiles(q, 0, "", 10)
		if err != nil {
			t.Fatal(err)
		}
		return len(r)
	}

	m := New(store, config.MaintenanceConfig{})
	m.pruneIndexByRealm() // everything is fresh
	if count("old") != 1 {
		t.Fatal("fresh entry pruned")
	}
	m.now = func() time.Time { return time.Now().Add(15 * 24 * time.Hour) }
	m.pruneIndexByRealm()
	if count("old") != 0 {
		t.Error("entry older than the realm's TTL kept")
	}
	if count("kept") != 1 || count("norealm") != 1 {
		t.Error("TTL 0 or channel without realm must be kept")
	}
}

func TestNextRun(t *testing.T) {
	loc := time.FixedZone("x", 3600)
	at := func(d, h, m int) time.Time { return time.Date(2026, 11, d, h, m, 0, 0, loc) }
	cases := []struct {
		name  string
		now   time.Time
		start string
		every time.Duration
		want  time.Time
	}{
		{"before start time", at(3, 3, 59), "04:00", 24 * time.Hour, at(3, 4, 0)},
		{"at start time → next day", at(3, 4, 0), "04:00", 24 * time.Hour, at(4, 4, 0)},
		{"every 6h from 04:00", at(3, 11, 0), "04:00", 6 * time.Hour, at(3, 16, 0)},
		{"after last slot → next day's start", at(3, 23, 0), "04:00", 6 * time.Hour, at(4, 4, 0)},
		{"5h does not divide 24: restart at start", at(4, 1, 0), "04:00", 5 * time.Hour, at(4, 4, 0)},
		{"after midnight, before start", at(4, 2, 0), "04:00", 6 * time.Hour, at(4, 4, 0)},
		{"no start time: from anchor", at(3, 7, 30), "", 6 * time.Hour, at(3, 12, 0)}, // anchor 06:00
	}
	for _, c := range cases {
		got, ok := nextRun(c.now, at(3, 6, 0), c.start, c.every)
		if !ok || !got.Equal(c.want) {
			t.Errorf("%s: got %v ok=%v, want %v", c.name, got, ok, c.want)
		}
	}
	if _, ok := nextRun(at(3, 1, 0), at(3, 0, 0), "04:00", 0); ok {
		t.Error("every 0 must mean no scheduled runs")
	}
	for in, ok := range map[string]bool{"": true, "04:00": true, "23:59": true, "24:00": false, "4am": false} {
		if _, _, err := ParseClock(in); (err == nil) != ok {
			t.Errorf("ParseClock(%q) err=%v", in, err)
		}
	}
}
