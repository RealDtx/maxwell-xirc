package db

import (
	"os"
	"testing"
	"time"
)

// MySQL tests require a running MariaDB instance.
// Set XIRC_TEST_MYSQL_DSN to enable, e.g.:
//   XIRC_TEST_MYSQL_DSN="xirc:password@tcp(localhost:3306)/xirc_test" go test -v -run TestMySQLStore ./...
//
// Without the env var, these tests are skipped.

func mysqlTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("XIRC_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("XIRC_TEST_MYSQL_DSN not set, skipping MySQL tests")
	}
	return dsn
}

func newTestMySQLStore(t *testing.T) *MySQLStore {
	t.Helper()
	dsn := mysqlTestDSN(t)
	store, err := NewMySQLStore(dsn)
	if err != nil {
		t.Fatalf("failed to create mysql store: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}
	// Clean tables for test isolation
	for _, table := range []string{"file_routing_rules", "post_hooks", "parse_patterns", "saved_searches", "indexed_files", "search_results", "downloads", "realms", "servers"} {
		store.db.Exec("DELETE FROM " + table)
	}
	return store
}

func TestMySQLStore_CreateAndGetServer(t *testing.T) {
	store := newTestMySQLStore(t)
	defer store.Close()

	srv := &Server{
		Name:         "test-server",
		Host:         "irc.example.com",
		Port:         6667,
		SSL:          true,
		Nickname:     "testbot",
		AltNicknames: []string{"testbot_", "testbot__"},
		AuthMethod:   "nickserv",
		AuthPassword: "secret",
		AutoConnect:  true,
		Enabled:      true,
	}

	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}
	if srv.ID == 0 {
		t.Error("expected ID to be set after create")
	}

	got, err := store.GetServer(srv.ID)
	if err != nil {
		t.Fatalf("GetServer failed: %v", err)
	}
	if got.Name != "test-server" {
		t.Errorf("expected name test-server, got %s", got.Name)
	}
	if got.Host != "irc.example.com" {
		t.Errorf("expected host irc.example.com, got %s", got.Host)
	}
	if !got.SSL {
		t.Error("expected SSL to be true")
	}
	if len(got.AltNicknames) != 2 {
		t.Errorf("unexpected alt_nicknames: %v", got.AltNicknames)
	}
}

func TestMySQLStore_ListServers(t *testing.T) {
	store := newTestMySQLStore(t)
	defer store.Close()

	store.CreateServer(&Server{Name: "srv1", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true})
	store.CreateServer(&Server{Name: "srv2", Host: "b.com", Port: 6697, Nickname: "bot", Enabled: true})

	servers, err := store.GetServers()
	if err != nil {
		t.Fatalf("GetServers failed: %v", err)
	}
	if len(servers) != 2 {
		t.Errorf("expected 2 servers, got %d", len(servers))
	}
}

func TestMySQLStore_CreateAndGetRealm(t *testing.T) {
	store := newTestMySQLStore(t)
	defer store.Close()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	r := &Realm{
		ServerID:        srv.ID,
		Name:            "#test",
		SearchCommand:   "!search",
		DownloadChannel: "#test-dl",
		AutoJoin:        true,
		Enabled:         true,
	}
	if err := store.CreateRealm(r); err != nil {
		t.Fatalf("CreateRealm failed: %v", err)
	}

	realms, err := store.GetRealms(srv.ID)
	if err != nil {
		t.Fatalf("GetRealms failed: %v", err)
	}
	if len(realms) != 1 {
		t.Fatalf("expected 1 realm, got %d", len(realms))
	}
	if realms[0].DownloadChannel != "#test-dl" {
		t.Errorf("expected download_channel #test-dl, got %s", realms[0].DownloadChannel)
	}
}

func TestMySQLStore_Downloads(t *testing.T) {
	store := newTestMySQLStore(t)
	defer store.Close()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	store.CreateDownload(&Download{ServerID: srv.ID, Channel: "#t", BotNick: "b1", PackNumber: 1, Status: "queued"})
	store.CreateDownload(&Download{ServerID: srv.ID, Channel: "#t", BotNick: "b2", PackNumber: 2, Status: "downloading"})

	all, _ := store.GetDownloads("")
	if len(all) != 2 {
		t.Errorf("expected 2 downloads, got %d", len(all))
	}

	queued, _ := store.GetDownloads("queued")
	if len(queued) != 1 {
		t.Errorf("expected 1 queued, got %d", len(queued))
	}
}

func TestMySQLStore_FileRoutingRules(t *testing.T) {
	store := newTestMySQLStore(t)
	defer store.Close()

	store.CreateFileRoutingRule(&FileRoutingRule{Pattern: "*.mkv", DestinationDir: "/media", Priority: 100, Enabled: true})
	store.CreateFileRoutingRule(&FileRoutingRule{Pattern: "*", DestinationDir: "/dl", Priority: 0, Builtin: true, Enabled: true})

	rules, _ := store.GetFileRoutingRules()
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].Priority != 100 {
		t.Errorf("expected first rule priority 100, got %d", rules[0].Priority)
	}
}

func TestMySQLStore_IndexedFiles_UpsertAndSearch(t *testing.T) {
	store := newTestMySQLStore(t)
	defer store.Close()

	srv1 := &Server{Name: "srv1", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	srv2 := &Server{Name: "srv2", Host: "b.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv1); err != nil {
		t.Fatalf("CreateServer srv1 failed: %v", err)
	}
	if err := store.CreateServer(srv2); err != nil {
		t.Fatalf("CreateServer srv2 failed: %v", err)
	}

	pack := 1
	size := "1G"
	if err := store.UpsertIndexedFile(&IndexedFile{ServerID: srv1.ID, Channel: "#c", BotNick: "b", PackNumber: &pack, Filename: "The.Matrix.1999.mkv", Filesize: &size, RawLine: "r"}); err != nil {
		t.Fatalf("UpsertIndexedFile failed: %v", err)
	}
	if err := store.UpsertIndexedFile(&IndexedFile{ServerID: srv2.ID, Channel: "#c2", BotNick: "b2", Filename: "Some.Other.Show.mkv", RawLine: "r2"}); err != nil {
		t.Fatalf("UpsertIndexedFile failed: %v", err)
	}

	// Re-observe the first file with a new pack number — should update in
	// place (hit_count increments) rather than duplicate.
	pack2 := 2
	if err := store.UpsertIndexedFile(&IndexedFile{ServerID: srv1.ID, Channel: "#c", BotNick: "b", PackNumber: &pack2, Filename: "The.Matrix.1999.mkv", RawLine: "r"}); err != nil {
		t.Fatalf("second UpsertIndexedFile failed: %v", err)
	}

	stats, err := store.GetIndexStats(0)
	if err != nil {
		t.Fatalf("GetIndexStats failed: %v", err)
	}
	if stats.TotalFiles != 2 {
		t.Fatalf("expected 2 total indexed files, got %d", stats.TotalFiles)
	}

	results, err := store.SearchIndexedFiles("matrix", 0, "", 10)
	if err != nil {
		t.Fatalf("SearchIndexedFiles failed: %v", err)
	}
	if len(results) != 1 || results[0].Filename != "The.Matrix.1999.mkv" {
		t.Fatalf("expected 1 match for 'matrix', got %+v", results)
	}
	if results[0].HitCount != 2 {
		t.Errorf("expected hit_count 2 after re-observing, got %d", results[0].HitCount)
	}
	if results[0].PackNumber == nil || *results[0].PackNumber != 2 {
		t.Errorf("expected pack_number to refresh to 2, got %v", results[0].PackNumber)
	}

	// Prefix matching: "matr" should still find "Matrix".
	prefixResults, err := store.SearchIndexedFiles("matr", 0, "", 10)
	if err != nil {
		t.Fatalf("SearchIndexedFiles (prefix) failed: %v", err)
	}
	if len(prefixResults) != 1 {
		t.Fatalf("expected prefix query 'matr' to match, got %d results", len(prefixResults))
	}

	// Scoped to srv2 only.
	scoped, err := store.SearchIndexedFiles("show", srv2.ID, "", 10)
	if err != nil {
		t.Fatalf("SearchIndexedFiles (scoped) failed: %v", err)
	}
	if len(scoped) != 1 || scoped[0].Filename != "Some.Other.Show.mkv" {
		t.Fatalf("expected 1 scoped match from srv2, got %+v", scoped)
	}

	// A query with boolean-mode special characters must not error.
	if _, err := store.SearchIndexedFiles(`matr"ix+`, 0, "", 10); err != nil {
		t.Fatalf("SearchIndexedFiles with special characters errored: %v", err)
	}
}

func TestMySQLStore_EnforceIndexCap(t *testing.T) {
	store := newTestMySQLStore(t)
	defer store.Close()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	for _, name := range []string{"a.mkv", "b.mkv", "c.mkv", "d.mkv", "e.mkv"} {
		if err := store.UpsertIndexedFile(&IndexedFile{ServerID: srv.ID, Channel: "#c", BotNick: "b", Filename: name, RawLine: "r"}); err != nil {
			t.Fatalf("UpsertIndexedFile(%s) failed: %v", name, err)
		}
		time.Sleep(1100 * time.Millisecond) // MySQL DATETIME has 1-second resolution by default
	}

	evicted, err := store.EnforceIndexCap(3)
	if err != nil {
		t.Fatalf("EnforceIndexCap failed: %v", err)
	}
	if evicted != 2 {
		t.Fatalf("expected 2 rows evicted, got %d", evicted)
	}

	stats, err := store.GetIndexStats(0)
	if err != nil {
		t.Fatalf("GetIndexStats failed: %v", err)
	}
	if stats.TotalFiles != 3 {
		t.Fatalf("expected 3 remaining files, got %d", stats.TotalFiles)
	}
}

func TestMySQLStore_PruneSearchResults(t *testing.T) {
	store := newTestMySQLStore(t)
	defer store.Close()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}
	if err := store.CreateSearchResult(&SearchResult{ServerID: srv.ID, Channel: "#c", BotNick: "b", RawLine: "raw", SearchQuery: "q", Parsed: false}); err != nil {
		t.Fatalf("CreateSearchResult failed: %v", err)
	}

	// Cutoff in the future so the freshly-created row counts as older-than-cutoff.
	deleted, err := store.PruneSearchResults(time.Now().Add(1 * time.Hour))
	if err != nil {
		t.Fatalf("PruneSearchResults failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 row pruned, got %d", deleted)
	}
}

func TestMySQLStore_DeleteServer_CascadesToRelatedData(t *testing.T) {
	store := newTestMySQLStore(t)
	defer store.Close()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}
	if err := store.CreateSearchResult(&SearchResult{ServerID: srv.ID, Channel: "#c", BotNick: "b", RawLine: "raw", SearchQuery: "q", Parsed: false}); err != nil {
		t.Fatalf("CreateSearchResult failed: %v", err)
	}
	if err := store.UpsertIndexedFile(&IndexedFile{ServerID: srv.ID, Channel: "#c", BotNick: "b", Filename: "a.mkv", RawLine: "r"}); err != nil {
		t.Fatalf("UpsertIndexedFile failed: %v", err)
	}
	if err := store.CreateSavedSearch(&SavedSearch{Name: "n", ServerID: srv.ID, Channel: "#c", Query: "q"}); err != nil {
		t.Fatalf("CreateSavedSearch failed: %v", err)
	}
	if err := store.CreateDownload(&Download{ServerID: srv.ID, Channel: "#c", BotNick: "b", PackNumber: 1, Filename: "f"}); err != nil {
		t.Fatalf("CreateDownload failed: %v", err)
	}

	if err := store.DeleteServer(srv.ID); err != nil {
		t.Fatalf("DeleteServer failed: %v", err)
	}

	if _, err := store.GetServer(srv.ID); err == nil {
		t.Error("expected error getting deleted server")
	}
	stats, err := store.GetIndexStats(0)
	if err != nil {
		t.Fatalf("GetIndexStats failed: %v", err)
	}
	if stats.TotalFiles != 0 {
		t.Errorf("expected indexed_files to be cleaned up, got %d remaining", stats.TotalFiles)
	}
	results, err := store.GetAllSearchResults("q", nil)
	if err != nil {
		t.Fatalf("GetAllSearchResults failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected search_results to be cleaned up, got %d remaining", len(results))
	}
}

func TestMySQLStore_UpsertIndexedFile_EvictsRotatedPack(t *testing.T) {
	store := newTestMySQLStore(t)
	defer store.Close()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	pack := 7
	old := &IndexedFile{ServerID: srv.ID, Channel: "#chan", BotNick: "xdcc", PackNumber: &pack, Filename: "Old.Content.mkv", RawLine: "raw"}
	if err := store.UpsertIndexedFile(old); err != nil {
		t.Fatalf("first UpsertIndexedFile failed: %v", err)
	}
	fresh := &IndexedFile{ServerID: srv.ID, Channel: "#other", BotNick: "xdcc", PackNumber: &pack, Filename: "New.Content.mkv", RawLine: "raw2"}
	if err := store.UpsertIndexedFile(fresh); err != nil {
		t.Fatalf("second UpsertIndexedFile failed: %v", err)
	}

	stats, _ := store.GetIndexStats(srv.ID)
	if stats.TotalFiles != 1 {
		t.Fatalf("expected rotated pack to evict old entry (1 row), got %d", stats.TotalFiles)
	}

	if err := store.EvictStaleIndexedFiles(srv.ID, "xdcc", pack, ""); err != nil {
		t.Fatalf("EvictStaleIndexedFiles failed: %v", err)
	}
	stats, _ = store.GetIndexStats(srv.ID)
	if stats.TotalFiles != 0 {
		t.Fatalf("expected bot+pack fully evicted, got %d rows", stats.TotalFiles)
	}
}
