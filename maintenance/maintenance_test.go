package maintenance

import (
	"io/ioutil"
	"os"
	"path/filepath"
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
