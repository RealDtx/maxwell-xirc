package db

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestSQLiteStore(t *testing.T) (*SQLiteStore, func()) {
	t.Helper()
	dir, err := ioutil.TempDir("", "xirc-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	path := filepath.Join(dir, "test.db")
	store, err := NewSQLiteStore(path)
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	if err := store.Migrate(); err != nil {
		if !strings.Contains(err.Error(), "no such table: channels") {
			store.Close()
			os.RemoveAll(dir)
			t.Fatalf("failed to run migrations: %v", err)
		}
	}
	cleanup := func() {
		store.Close()
		os.RemoveAll(dir)
	}
	return store, cleanup
}

func TestSQLiteStore_CreateAndGetServer(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

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
	if len(got.AltNicknames) != 2 || got.AltNicknames[0] != "testbot_" {
		t.Errorf("unexpected alt_nicknames: %v", got.AltNicknames)
	}
	if got.AuthMethod != "nickserv" {
		t.Errorf("expected auth_method nickserv, got %s", got.AuthMethod)
	}
}

func TestSQLiteStore_ListServers(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

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

func TestSQLiteStore_UpdateServer(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "old", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	srv.Name = "new"
	srv.Port = 6697
	if err := store.UpdateServer(srv); err != nil {
		t.Fatalf("UpdateServer failed: %v", err)
	}

	got, _ := store.GetServer(srv.ID)
	if got.Name != "new" {
		t.Errorf("expected name new, got %s", got.Name)
	}
	if got.Port != 6697 {
		t.Errorf("expected port 6697, got %d", got.Port)
	}
}

func TestSQLiteStore_DeleteServer(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "deleteme", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	if err := store.DeleteServer(srv.ID); err != nil {
		t.Fatalf("DeleteServer failed: %v", err)
	}

	_, err := store.GetServer(srv.ID)
	if err == nil {
		t.Error("expected error after deleting server")
	}
}

func TestSQLiteStore_CreateAndGetRealm(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	r := &Realm{
		ServerID:        srv.ID,
		Name:            "#test",
		Key:             "secret",
		SearchCommand:   "!search",
		DownloadChannel: "#test-downloads",
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
	if realms[0].Name != "#test" {
		t.Errorf("expected realm #test, got %s", realms[0].Name)
	}
	if realms[0].DownloadChannel != "#test-downloads" {
		t.Errorf("expected download_channel #test-downloads, got %s", realms[0].DownloadChannel)
	}
	if realms[0].SearchCommand != "!search" {
		t.Errorf("expected search_command !search, got %s", realms[0].SearchCommand)
	}
}

func TestSQLiteStore_CreateAndGetDownload(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	dl := &Download{
		ServerID:   srv.ID,
		Channel:    "#test",
		BotNick:    "xdcc_bot",
		PackNumber: 42,
		Filename:   "movie.mkv",
		Filesize:   1500000000,
		Status:     "queued",
	}

	if err := store.CreateDownload(dl); err != nil {
		t.Fatalf("CreateDownload failed: %v", err)
	}

	downloads, err := store.GetDownloads("")
	if err != nil {
		t.Fatalf("GetDownloads failed: %v", err)
	}
	if len(downloads) != 1 {
		t.Fatalf("expected 1 download, got %d", len(downloads))
	}
	if downloads[0].Filename != "movie.mkv" {
		t.Errorf("expected filename movie.mkv, got %s", downloads[0].Filename)
	}
	if downloads[0].Status != "queued" {
		t.Errorf("expected status queued, got %s", downloads[0].Status)
	}
}

func TestSQLiteStore_GetDownloads_FilterByStatus(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	store.CreateDownload(&Download{ServerID: srv.ID, Channel: "#t", BotNick: "b1", PackNumber: 1, Status: "queued"})
	store.CreateDownload(&Download{ServerID: srv.ID, Channel: "#t", BotNick: "b2", PackNumber: 2, Status: "downloading"})
	store.CreateDownload(&Download{ServerID: srv.ID, Channel: "#t", BotNick: "b3", PackNumber: 3, Status: "queued"})

	queued, _ := store.GetDownloads("queued")
	if len(queued) != 2 {
		t.Errorf("expected 2 queued downloads, got %d", len(queued))
	}

	downloading, _ := store.GetDownloads("downloading")
	if len(downloading) != 1 {
		t.Errorf("expected 1 downloading, got %d", len(downloading))
	}

	all, _ := store.GetDownloads("")
	if len(all) != 3 {
		t.Errorf("expected 3 total downloads, got %d", len(all))
	}
}

func TestSQLiteStore_FileRoutingRules(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	store.CreateFileRoutingRule(&FileRoutingRule{
		Pattern: "*.mkv", DestinationDir: "/media", Priority: 100, Enabled: true,
	})
	store.CreateFileRoutingRule(&FileRoutingRule{
		Pattern: "*", DestinationDir: "/downloads", Priority: 0, Builtin: true, Enabled: true,
	})

	rules, err := store.GetFileRoutingRules()
	if err != nil {
		t.Fatalf("GetFileRoutingRules failed: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	// Should be ordered by priority descending
	if rules[0].Priority != 100 {
		t.Errorf("expected first rule priority 100, got %d", rules[0].Priority)
	}
	if rules[1].Priority != 0 {
		t.Errorf("expected second rule priority 0, got %d", rules[1].Priority)
	}
}

func TestSQLiteStore_PersistsToDisk(t *testing.T) {
	dir, err := ioutil.TempDir("", "xirc-persist-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "persist.db")

	store1, _ := NewSQLiteStore(path)
	store1.Migrate()
	store1.CreateServer(&Server{Name: "persist-test", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true})
	store1.Close()

	store2, _ := NewSQLiteStore(path)
	store2.Migrate()
	defer store2.Close()

	servers, _ := store2.GetServers()
	if len(servers) != 1 {
		t.Fatalf("expected 1 server after reopen, got %d", len(servers))
	}
	if servers[0].Name != "persist-test" {
		t.Errorf("expected name persist-test, got %s", servers[0].Name)
	}
	_ = os.Remove(path)
}

func TestSQLiteStore_GetRealm(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	r := &Realm{ServerID: srv.ID, Name: "#books", Key: "k", SearchCommand: "!s", DownloadChannel: "#books-dl", AutoJoin: true, Enabled: true}
	if err := store.CreateRealm(r); err != nil {
		t.Fatalf("CreateRealm failed: %v", err)
	}

	got, err := store.GetRealm(r.ID)
	if err != nil {
		t.Fatalf("GetRealm failed: %v", err)
	}
	if got.Name != "#books" || got.DownloadChannel != "#books-dl" || !got.AutoJoin {
		t.Errorf("unexpected realm: %+v", got)
	}
}

func TestSQLiteStore_UpdateRealm(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	r := &Realm{ServerID: srv.ID, Name: "#old", AutoJoin: false, Enabled: true}
	if err := store.CreateRealm(r); err != nil {
		t.Fatalf("CreateRealm failed: %v", err)
	}

	r.Name = "#new"
	r.AutoJoin = true
	if err := store.UpdateRealm(r); err != nil {
		t.Fatalf("UpdateRealm failed: %v", err)
	}

	got, err := store.GetRealm(r.ID)
	if err != nil {
		t.Fatalf("GetRealm failed: %v", err)
	}
	if got.Name != "#new" {
		t.Errorf("expected #new, got %s", got.Name)
	}
	if !got.AutoJoin {
		t.Error("expected auto_join true")
	}
}

func TestSQLiteStore_DeleteRealm(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	r := &Realm{ServerID: srv.ID, Name: "#gone", Enabled: true}
	if err := store.CreateRealm(r); err != nil {
		t.Fatalf("CreateRealm failed: %v", err)
	}
	if err := store.DeleteRealm(r.ID); err != nil {
		t.Fatalf("DeleteRealm failed: %v", err)
	}

	realms, err := store.GetRealms(srv.ID)
	if err != nil {
		t.Fatalf("GetRealms failed: %v", err)
	}
	if len(realms) != 0 {
		t.Errorf("expected 0 realms, got %d", len(realms))
	}
}

func TestSQLiteStore_GetAndUpdateDownload(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	dl := &Download{ServerID: srv.ID, Channel: "#c", BotNick: "b", PackNumber: 7, Filename: "f.bin", Filesize: 10, Status: "queued"}
	if err := store.CreateDownload(dl); err != nil {
		t.Fatalf("CreateDownload failed: %v", err)
	}

	got, err := store.GetDownload(dl.ID)
	if err != nil {
		t.Fatalf("GetDownload failed: %v", err)
	}
	if got.Status != "queued" {
		t.Errorf("expected queued, got %s", got.Status)
	}

	got.Status = "completed"
	if err := store.UpdateDownload(got); err != nil {
		t.Fatalf("UpdateDownload failed: %v", err)
	}

	updated, err := store.GetDownload(dl.ID)
	if err != nil {
		t.Fatalf("GetDownload after update failed: %v", err)
	}
	if updated.Status != "completed" {
		t.Errorf("expected completed, got %s", updated.Status)
	}
}

func TestSQLiteStore_GetSearchResults(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	pack := 12
	filename := "x.mkv"
	filesize := "1.2 GB"
	sr := &SearchResult{ServerID: srv.ID, Channel: "#chan", BotNick: "xdcc", PackNumber: &pack, Filename: &filename, Filesize: &filesize, RawLine: "raw", SearchQuery: "movie", Parsed: true}
	if err := store.CreateSearchResult(sr); err != nil {
		t.Fatalf("CreateSearchResult failed: %v", err)
	}

	results, err := store.GetSearchResults("movie", srv.ID, "#chan")
	if err != nil {
		t.Fatalf("GetSearchResults failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].BotNick != "xdcc" {
		t.Errorf("expected bot xdcc, got %s", results[0].BotNick)
	}
}

func TestSQLiteStore_DeleteDownloads(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	completed := &Download{ServerID: srv.ID, Channel: "#test", BotNick: "bot", PackNumber: 1, Status: "completed"}
	downloading := &Download{ServerID: srv.ID, Channel: "#test", BotNick: "bot", PackNumber: 2, Status: "downloading"}
	if err := store.CreateDownload(completed); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateDownload(downloading); err != nil {
		t.Fatal(err)
	}

	n, err := store.DeleteDownloads([]int64{completed.ID})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 deleted, got %d", n)
	}

	n, err = store.DeleteDownloads([]int64{downloading.ID})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0 deleted (active), got %d", n)
	}
}

func TestSQLiteStore_DeleteDownloadsByStatus(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	for i := 0; i < 2; i++ {
		dl := &Download{ServerID: srv.ID, Channel: "#test", BotNick: "bot", PackNumber: i + 1, Status: "failed", ErrorMessage: "err"}
		if err := store.CreateDownload(dl); err != nil {
			t.Fatal(err)
		}
	}
	completed := &Download{ServerID: srv.ID, Channel: "#test", BotNick: "bot", PackNumber: 99, Status: "completed"}
	if err := store.CreateDownload(completed); err != nil {
		t.Fatal(err)
	}

	n, err := store.DeleteDownloadsByStatus("failed")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 deleted, got %d", n)
	}

	remaining, err := store.GetDownloads("")
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].Status != "completed" {
		t.Fatalf("expected 1 completed remaining, got %d", len(remaining))
	}

	_, err = store.DeleteDownloadsByStatus("downloading")
	if err == nil {
		t.Fatal("expected error for active status, got nil")
	}
}

func TestSQLiteStore_GetAllSearchResults(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	sr1 := &SearchResult{ServerID: srv.ID, Channel: "#a", BotNick: "b1", RawLine: "r1", SearchQuery: "linux", Parsed: true}
	sr2 := &SearchResult{ServerID: srv.ID, Channel: "#b", BotNick: "b2", RawLine: "r2", SearchQuery: "linux", Parsed: true}
	sr3 := &SearchResult{ServerID: srv.ID, Channel: "#c", BotNick: "b3", RawLine: "r3", SearchQuery: "linux", Parsed: false}
	if err := store.CreateSearchResult(sr1); err != nil {
		t.Fatalf("CreateSearchResult sr1 failed: %v", err)
	}
	if err := store.CreateSearchResult(sr2); err != nil {
		t.Fatalf("CreateSearchResult sr2 failed: %v", err)
	}
	if err := store.CreateSearchResult(sr3); err != nil {
		t.Fatalf("CreateSearchResult sr3 failed: %v", err)
	}

	results, err := store.GetAllSearchResults("linux", nil)
	if err != nil {
		t.Fatalf("GetAllSearchResults failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 parsed results, got %d", len(results))
	}
}

func TestSQLiteStore_GetAllSearchResults_LimitedTo500(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	for i := 0; i < 510; i++ {
		raw := fmt.Sprintf("raw-%d", i)
		res := &SearchResult{
			ServerID:    srv.ID,
			Channel:     "#cap",
			BotNick:     "bot",
			RawLine:     raw,
			SearchQuery: "limit-test",
			Parsed:      true,
		}
		if err := store.CreateSearchResult(res); err != nil {
			t.Fatalf("CreateSearchResult %d failed: %v", i, err)
		}
	}

	results, err := store.GetAllSearchResults("limit-test", nil)
	if err != nil {
		t.Fatalf("GetAllSearchResults nil since failed: %v", err)
	}
	if len(results) != 500 {
		t.Fatalf("expected 500 results with nil since, got %d", len(results))
	}

	since := time.Now().Add(-time.Hour)
	results, err = store.GetAllSearchResults("limit-test", &since)
	if err != nil {
		t.Fatalf("GetAllSearchResults with since failed: %v", err)
	}
	if len(results) != 500 {
		t.Fatalf("expected 500 results with since, got %d", len(results))
	}
}

func TestSQLiteStore_GetAllSearchResults_SameSecondComparison(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	// Insert results with precise, known timestamps using raw SQL.
	// These simulate results stored at sub-second offsets within the same second.
	timestamps := []string{
		"2026-04-19T16:53:32.567011129Z", // 567ms
		"2026-04-19T16:53:32.100000000Z", // 100ms
		"2026-04-19T16:53:32.999999999Z", // 999ms
	}
	for i, ts := range timestamps {
		_, err := store.db.Exec(
			`INSERT INTO search_results (server_id, channel, bot_nick, pack_number, filename, filesize, downloads_count, raw_line, search_query, parsed, created_at)
			 VALUES (?, '#test', 'bot', ?, 'file.mkv', '1.5G', 0, 'raw', 'test-query', 1, ?)`,
			srv.ID, i+1, ts,
		)
		if err != nil {
			t.Fatalf("insert result %d: %v", i, err)
		}
	}

	// Query with since at the START of the same second (.000).
	// All 3 results are AFTER .000 and should be returned.
	since := time.Date(2026, 4, 19, 16, 53, 32, 0, time.UTC) // .000000000
	results, err := store.GetAllSearchResults("test-query", &since)
	if err != nil {
		t.Fatalf("GetAllSearchResults since .000: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("since .000: expected 3 results, got %d", len(results))
	}

	// Query with since at .5 (500ms) — should return 2 results (.567 and .999).
	since = time.Date(2026, 4, 19, 16, 53, 32, 500_000_000, time.UTC)
	results, err = store.GetAllSearchResults("test-query", &since)
	if err != nil {
		t.Fatalf("GetAllSearchResults since .5: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("since .5: expected 2 results (.567 and .999), got %d", len(results))
	}

	// Query with since at .567011129 (exact match) — should return 1 result (.999 only).
	since = time.Date(2026, 4, 19, 16, 53, 32, 567_011_129, time.UTC)
	results, err = store.GetAllSearchResults("test-query", &since)
	if err != nil {
		t.Fatalf("GetAllSearchResults since .567011129: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("since .567011129: expected 1 result (.999 only), got %d", len(results))
	}

	// Query with since 1 second BEFORE — should return all 3 results.
	since = time.Date(2026, 4, 19, 16, 53, 31, 0, time.UTC)
	results, err = store.GetAllSearchResults("test-query", &since)
	if err != nil {
		t.Fatalf("GetAllSearchResults since second-before: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("since second-before: expected 3 results, got %d", len(results))
	}
}

func TestSQLiteStore_SavedSearches(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	ss := &SavedSearch{Name: "daily", ServerID: srv.ID, Channel: "#chan", Query: "ubuntu"}
	if err := store.CreateSavedSearch(ss); err != nil {
		t.Fatalf("CreateSavedSearch failed: %v", err)
	}

	searches, err := store.GetSavedSearches()
	if err != nil {
		t.Fatalf("GetSavedSearches failed: %v", err)
	}
	if len(searches) != 1 {
		t.Fatalf("expected 1 saved search, got %d", len(searches))
	}

	if err := store.DeleteSavedSearch(ss.ID); err != nil {
		t.Fatalf("DeleteSavedSearch failed: %v", err)
	}

	searches, err = store.GetSavedSearches()
	if err != nil {
		t.Fatalf("GetSavedSearches after delete failed: %v", err)
	}
	if len(searches) != 0 {
		t.Errorf("expected 0 saved searches, got %d", len(searches))
	}
}

func TestSQLiteStore_ParsePatterns(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	p := &ParsePattern{Name: "p1", Regex: `#(\\d+) (.+)`, FieldMapping: "{}", Priority: 10, Enabled: true}
	if err := store.CreateParsePattern(p); err != nil {
		t.Fatalf("CreateParsePattern failed: %v", err)
	}

	patterns, err := store.GetParsePatterns()
	if err != nil {
		t.Fatalf("GetParsePatterns failed: %v", err)
	}
	if len(patterns) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(patterns))
	}

	p.Name = "p1-updated"
	p.Enabled = true
	if err := store.UpdateParsePattern(p); err != nil {
		t.Fatalf("UpdateParsePattern failed: %v", err)
	}

	patterns, err = store.GetParsePatterns()
	if err != nil {
		t.Fatalf("GetParsePatterns after update failed: %v", err)
	}
	if len(patterns) != 1 || patterns[0].Name != "p1-updated" {
		t.Errorf("unexpected patterns after update: %+v", patterns)
	}
}

func TestSQLiteStore_PostHooks(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	h := &PostHook{Name: "notify", Scope: "global", HookType: "webhook", Config: `{"url":"https://example.com"}`, Enabled: true}
	if err := store.CreatePostHook(h); err != nil {
		t.Fatalf("CreatePostHook failed: %v", err)
	}

	hooks, err := store.GetPostHooks("", nil)
	if err != nil {
		t.Fatalf("GetPostHooks failed: %v", err)
	}
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(hooks))
	}

	got, err := store.GetPostHookByID(h.ID)
	if err != nil {
		t.Fatalf("GetPostHookByID failed: %v", err)
	}
	if got == nil || got.Name != "notify" {
		t.Fatalf("unexpected hook: %+v", got)
	}

	got.Name = "notify-updated"
	if err := store.UpdatePostHook(got); err != nil {
		t.Fatalf("UpdatePostHook failed: %v", err)
	}

	updated, err := store.GetPostHookByID(h.ID)
	if err != nil {
		t.Fatalf("GetPostHookByID after update failed: %v", err)
	}
	if updated.Name != "notify-updated" {
		t.Errorf("expected updated name, got %s", updated.Name)
	}

	if err := store.DeletePostHook(h.ID); err != nil {
		t.Fatalf("DeletePostHook failed: %v", err)
	}
	deleted, err := store.GetPostHookByID(h.ID)
	if err != nil {
		t.Fatalf("GetPostHookByID after delete failed: %v", err)
	}
	if deleted != nil {
		t.Errorf("expected nil after delete, got %+v", deleted)
	}
}

func TestSQLiteStore_FileRoutingRuleFull(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	r := &FileRoutingRule{Pattern: "*.zip", DestinationDir: "/downloads", Priority: 5, Enabled: true}
	if err := store.CreateFileRoutingRule(r); err != nil {
		t.Fatalf("CreateFileRoutingRule failed: %v", err)
	}

	rules, err := store.GetAllFileRoutingRules()
	if err != nil {
		t.Fatalf("GetAllFileRoutingRules failed: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}

	got, err := store.GetFileRoutingRuleByID(r.ID)
	if err != nil {
		t.Fatalf("GetFileRoutingRuleByID failed: %v", err)
	}
	if got == nil || got.Pattern != "*.zip" {
		t.Fatalf("unexpected rule: %+v", got)
	}

	got.Pattern = "*.tar.gz"
	got.Priority = 50
	if err := store.UpdateFileRoutingRule(got); err != nil {
		t.Fatalf("UpdateFileRoutingRule failed: %v", err)
	}

	updated, err := store.GetFileRoutingRuleByID(r.ID)
	if err != nil {
		t.Fatalf("GetFileRoutingRuleByID after update failed: %v", err)
	}
	if updated.Pattern != "*.tar.gz" || updated.Priority != 50 {
		t.Errorf("unexpected updated rule: %+v", updated)
	}

	if err := store.DeleteFileRoutingRule(r.ID); err != nil {
		t.Fatalf("DeleteFileRoutingRule failed: %v", err)
	}
	deleted, err := store.GetFileRoutingRuleByID(r.ID)
	if err != nil {
		t.Fatalf("GetFileRoutingRuleByID after delete failed: %v", err)
	}
	if deleted != nil {
		t.Errorf("expected nil after delete, got %+v", deleted)
	}
}

func TestSQLiteStore_DownloadStats(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	stat := &DownloadStat{Filename: "a.bin", SizeBytes: 2048, ServerID: 1, Channel: "#c", BotNick: "bot", PackNumber: 3, Status: "completed", StatsOnly: false}
	if err := store.CreateDownloadStat(stat); err != nil {
		t.Fatalf("CreateDownloadStat failed: %v", err)
	}

	summary, err := store.GetDownloadStatsSummary()
	if err != nil {
		t.Fatalf("GetDownloadStatsSummary failed: %v", err)
	}
	if summary.TotalTransfers != 1 {
		t.Errorf("expected total transfers 1, got %d", summary.TotalTransfers)
	}
	if summary.TotalBytes != 2048 {
		t.Errorf("expected total bytes 2048, got %d", summary.TotalBytes)
	}
	if summary.TotalSaved != 2048 {
		t.Errorf("expected total saved 2048, got %d", summary.TotalSaved)
	}

	history, err := store.GetDownloadHistory(0, 10)
	if err != nil {
		t.Fatalf("GetDownloadHistory failed: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 history row, got %d", len(history))
	}
	if history[0].Filename != "a.bin" || history[0].Status != "completed" {
		t.Errorf("unexpected history row: %+v", history[0])
	}
}

func TestSQLiteStore_UpsertIndexedFile_CreatesEntry(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	pack := 12
	size := "1.2G"
	f := &IndexedFile{ServerID: srv.ID, Channel: "#chan", BotNick: "xdcc", PackNumber: &pack, Filename: "Some.Movie.2024.mkv", Filesize: &size, RawLine: "raw line"}
	if err := store.UpsertIndexedFile(f); err != nil {
		t.Fatalf("UpsertIndexedFile failed: %v", err)
	}

	stats, err := store.GetIndexStats(0)
	if err != nil {
		t.Fatalf("GetIndexStats failed: %v", err)
	}
	if stats.TotalFiles != 1 {
		t.Fatalf("expected 1 indexed file, got %d", stats.TotalFiles)
	}

	results, err := store.SearchIndexedFiles("movie", 0, "", 10)
	if err != nil {
		t.Fatalf("SearchIndexedFiles failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].HitCount != 1 {
		t.Errorf("expected hit_count 1, got %d", results[0].HitCount)
	}
}

func TestSQLiteStore_UpsertIndexedFile_UpdatesExistingIncrementsHitCount(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	pack := 1
	size := "1G"
	f := &IndexedFile{ServerID: srv.ID, Channel: "#chan", BotNick: "xdcc", PackNumber: &pack, Filename: "Same.File.mkv", Filesize: &size, RawLine: "raw"}
	if err := store.UpsertIndexedFile(f); err != nil {
		t.Fatalf("first UpsertIndexedFile failed: %v", err)
	}

	// Seen again later with a new pack number and size (bot re-listed the file).
	pack2 := 2
	size2 := "1.1G"
	f2 := &IndexedFile{ServerID: srv.ID, Channel: "#chan", BotNick: "xdcc", PackNumber: &pack2, Filename: "Same.File.mkv", Filesize: &size2, RawLine: "raw2"}
	if err := store.UpsertIndexedFile(f2); err != nil {
		t.Fatalf("second UpsertIndexedFile failed: %v", err)
	}

	stats, err := store.GetIndexStats(srv.ID)
	if err != nil {
		t.Fatalf("GetIndexStats failed: %v", err)
	}
	if stats.TotalFiles != 1 {
		t.Fatalf("expected upsert to dedupe into 1 row, got %d", stats.TotalFiles)
	}

	results, err := store.SearchIndexedFiles("same file", 0, "", 10)
	if err != nil {
		t.Fatalf("SearchIndexedFiles failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].HitCount != 2 {
		t.Errorf("expected hit_count 2 after second upsert, got %d", results[0].HitCount)
	}
	if results[0].PackNumber == nil || *results[0].PackNumber != 2 {
		t.Errorf("expected pack_number to refresh to 2, got %v", results[0].PackNumber)
	}
	if results[0].Filesize == nil || *results[0].Filesize != "1.1G" {
		t.Errorf("expected filesize to refresh to 1.1G, got %v", results[0].Filesize)
	}
}

func TestSQLiteStore_SearchIndexedFiles_MultiWordAndScoping(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv1 := &Server{Name: "srv1", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	srv2 := &Server{Name: "srv2", Host: "b.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv1); err != nil {
		t.Fatalf("CreateServer srv1 failed: %v", err)
	}
	if err := store.CreateServer(srv2); err != nil {
		t.Fatalf("CreateServer srv2 failed: %v", err)
	}

	entries := []*IndexedFile{
		{ServerID: srv1.ID, Channel: "#chan1", BotNick: "bot1", Filename: "The.Matrix.1999.mkv", RawLine: "r1"},
		{ServerID: srv1.ID, Channel: "#chan1", BotNick: "bot1", Filename: "Some.Other.Show.mkv", RawLine: "r2"},
		{ServerID: srv2.ID, Channel: "#chan2", BotNick: "bot2", Filename: "The.Matrix.Reloaded.mkv", RawLine: "r3"},
	}
	for _, e := range entries {
		if err := store.UpsertIndexedFile(e); err != nil {
			t.Fatalf("UpsertIndexedFile failed: %v", err)
		}
	}

	// Multi-word AND query across all servers.
	all, err := store.SearchIndexedFiles("the matrix", 0, "", 10)
	if err != nil {
		t.Fatalf("SearchIndexedFiles failed: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 matches across servers, got %d", len(all))
	}

	// Scoped to srv1 only.
	scoped, err := store.SearchIndexedFiles("matrix", srv1.ID, "", 10)
	if err != nil {
		t.Fatalf("SearchIndexedFiles (scoped) failed: %v", err)
	}
	if len(scoped) != 1 || scoped[0].Filename != "The.Matrix.1999.mkv" {
		t.Fatalf("expected 1 scoped match from srv1, got %+v", scoped)
	}

	// A query that doesn't match every word should return nothing.
	none, err := store.SearchIndexedFiles("matrix xyz123", 0, "", 10)
	if err != nil {
		t.Fatalf("SearchIndexedFiles (no match) failed: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(none))
	}
}

func TestSQLiteStore_ClearIndex(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv1 := &Server{Name: "srv1", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	srv2 := &Server{Name: "srv2", Host: "b.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv1); err != nil {
		t.Fatalf("CreateServer srv1 failed: %v", err)
	}
	if err := store.CreateServer(srv2); err != nil {
		t.Fatalf("CreateServer srv2 failed: %v", err)
	}
	if err := store.UpsertIndexedFile(&IndexedFile{ServerID: srv1.ID, Channel: "#c1", BotNick: "b1", Filename: "a.mkv", RawLine: "r"}); err != nil {
		t.Fatalf("UpsertIndexedFile failed: %v", err)
	}
	if err := store.UpsertIndexedFile(&IndexedFile{ServerID: srv2.ID, Channel: "#c2", BotNick: "b2", Filename: "b.mkv", RawLine: "r"}); err != nil {
		t.Fatalf("UpsertIndexedFile failed: %v", err)
	}

	// Clearing one server's index should leave the other server's entries intact.
	if err := store.ClearIndex(srv1.ID); err != nil {
		t.Fatalf("ClearIndex(srv1) failed: %v", err)
	}
	stats, err := store.GetIndexStats(0)
	if err != nil {
		t.Fatalf("GetIndexStats failed: %v", err)
	}
	if stats.TotalFiles != 1 {
		t.Fatalf("expected 1 remaining indexed file, got %d", stats.TotalFiles)
	}

	// Clearing with serverID=0 clears everything.
	if err := store.ClearIndex(0); err != nil {
		t.Fatalf("ClearIndex(0) failed: %v", err)
	}
	stats, err = store.GetIndexStats(0)
	if err != nil {
		t.Fatalf("GetIndexStats failed: %v", err)
	}
	if stats.TotalFiles != 0 {
		t.Fatalf("expected 0 remaining indexed files, got %d", stats.TotalFiles)
	}
}

// TestSQLiteStore_SearchIndexedFiles_PrefixMatching verifies FTS5 prefix
// matching: a partial word like "matr" should still find "Matrix", and
// punctuation-heavy filenames should tokenize sensibly.
func TestSQLiteStore_SearchIndexedFiles_PrefixMatching(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}
	if err := store.UpsertIndexedFile(&IndexedFile{ServerID: srv.ID, Channel: "#c", BotNick: "b", Filename: "The.Matrix.1999.mkv", RawLine: "r"}); err != nil {
		t.Fatalf("UpsertIndexedFile failed: %v", err)
	}

	results, err := store.SearchIndexedFiles("matr", 0, "", 10)
	if err != nil {
		t.Fatalf("SearchIndexedFiles (prefix) failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected prefix query 'matr' to match Matrix, got %d results", len(results))
	}

	// A query containing FTS5-special characters (quotes) must not error out
	// or be interpreted as query syntax.
	if _, err := store.SearchIndexedFiles(`matr"ix`, 0, "", 10); err != nil {
		t.Fatalf("SearchIndexedFiles with quote character errored: %v", err)
	}
}

// TestSQLiteStore_FTS5Backfill_ExistingRowsBecomeSearchable simulates
// upgrading an installation that already has indexed_files rows from before
// the FTS5 index existed: Migrate() must backfill them so they're
// immediately searchable, not just newly-inserted rows.
func TestSQLiteStore_FTS5Backfill_ExistingRowsBecomeSearchable(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	// Insert directly via raw SQL, bypassing UpsertIndexedFile (and therefore
	// the AFTER INSERT trigger), to simulate a row that predates the FTS5
	// migration.
	if _, err := store.db.Exec(
		`INSERT INTO indexed_files (server_id, channel, bot_nick, filename, raw_line, hit_count) VALUES (?, ?, ?, ?, ?, 1)`,
		srv.ID, "#c", "b", "Legacy.Pre.Fts.File.mkv", "raw",
	); err != nil {
		t.Fatalf("raw insert failed: %v", err)
	}
	// Drop the FTS table+triggers to simulate a DB that never had them, then
	// re-run Migrate() to recreate and backfill them.
	if _, err := store.db.Exec(`DROP TRIGGER IF EXISTS indexed_files_ai`); err != nil {
		t.Fatalf("drop trigger failed: %v", err)
	}
	if _, err := store.db.Exec(`DROP TRIGGER IF EXISTS indexed_files_ad`); err != nil {
		t.Fatalf("drop trigger failed: %v", err)
	}
	if _, err := store.db.Exec(`DROP TRIGGER IF EXISTS indexed_files_au`); err != nil {
		t.Fatalf("drop trigger failed: %v", err)
	}
	if _, err := store.db.Exec(`DROP TABLE IF EXISTS indexed_files_fts`); err != nil {
		t.Fatalf("drop fts table failed: %v", err)
	}

	if err := store.Migrate(); err != nil {
		t.Fatalf("re-running Migrate() failed: %v", err)
	}

	results, err := store.SearchIndexedFiles("legacy", 0, "", 10)
	if err != nil {
		t.Fatalf("SearchIndexedFiles failed: %v", err)
	}
	if len(results) != 1 || results[0].Filename != "Legacy.Pre.Fts.File.mkv" {
		t.Fatalf("expected backfilled legacy row to be searchable, got %+v", results)
	}
}

func TestSQLiteStore_PruneSearchResults(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}
	if err := store.CreateSearchResult(&SearchResult{ServerID: srv.ID, Channel: "#c", BotNick: "b", RawLine: "raw", SearchQuery: "q", Parsed: false}); err != nil {
		t.Fatalf("CreateSearchResult failed: %v", err)
	}

	// A cutoff safely in the past should prune nothing.
	deleted, err := store.PruneSearchResults(time.Now().Add(-24 * time.Hour))
	if err != nil {
		t.Fatalf("PruneSearchResults (past cutoff) failed: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("expected 0 rows pruned with a past cutoff, got %d", deleted)
	}

	// A cutoff in the future means the freshly-created row counts as
	// older-than-cutoff, exercising the same comparison a real 14-day-old
	// row would eventually satisfy.
	deleted, err = store.PruneSearchResults(time.Now().Add(1 * time.Hour))
	if err != nil {
		t.Fatalf("PruneSearchResults (future cutoff) failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 row pruned, got %d", deleted)
	}

	remaining, err := store.GetAllUnparsedSince(time.Now().Add(-24 * time.Hour))
	if err != nil {
		t.Fatalf("GetAllUnparsedSince failed: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected pruned row to be gone, got %d remaining", len(remaining))
	}
}

func TestSQLiteStore_EnforceIndexCap(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	for _, name := range []string{"a.mkv", "b.mkv", "c.mkv", "d.mkv", "e.mkv"} {
		if err := store.UpsertIndexedFile(&IndexedFile{ServerID: srv.ID, Channel: "#c", BotNick: "b", Filename: name, RawLine: "r"}); err != nil {
			t.Fatalf("UpsertIndexedFile(%s) failed: %v", name, err)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// A cap higher than the current count should evict nothing.
	evicted, err := store.EnforceIndexCap(10)
	if err != nil {
		t.Fatalf("EnforceIndexCap(10) failed: %v", err)
	}
	if evicted != 0 {
		t.Fatalf("expected 0 evicted when cap exceeds count, got %d", evicted)
	}

	// A cap of 0 disables enforcement entirely.
	evicted, err = store.EnforceIndexCap(0)
	if err != nil {
		t.Fatalf("EnforceIndexCap(0) failed: %v", err)
	}
	if evicted != 0 {
		t.Fatalf("expected 0 evicted when cap is disabled, got %d", evicted)
	}

	evicted, err = store.EnforceIndexCap(3)
	if err != nil {
		t.Fatalf("EnforceIndexCap(3) failed: %v", err)
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

	remaining, err := store.SearchIndexedFiles("mkv", 0, "", 10)
	if err != nil {
		t.Fatalf("SearchIndexedFiles failed: %v", err)
	}
	for _, r := range remaining {
		if r.Filename == "a.mkv" || r.Filename == "b.mkv" {
			t.Errorf("expected %s to be evicted as least-recently-seen, but it remains", r.Filename)
		}
	}
}

func TestSQLiteStore_DeleteServer_CascadesToRelatedData(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

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
	dl := &Download{ServerID: srv.ID, Channel: "#c", BotNick: "b", PackNumber: 1, Filename: "f"}
	if err := store.CreateDownload(dl); err != nil {
		t.Fatalf("CreateDownload failed: %v", err)
	}
	// A server-scoped parse pattern should be cleaned up too; a global one
	// (ServerID nil) must survive.
	if err := store.CreateParsePattern(&ParsePattern{Name: "scoped", Regex: "x", FieldMapping: "{}", ServerID: &srv.ID, Channel: "#c"}); err != nil {
		t.Fatalf("CreateParsePattern (scoped) failed: %v", err)
	}
	if err := store.CreateParsePattern(&ParsePattern{Name: "global", Regex: "y", FieldMapping: "{}"}); err != nil {
		t.Fatalf("CreateParsePattern (global) failed: %v", err)
	}
	realm := &Realm{ServerID: srv.ID, Name: "#c"}
	if err := store.CreateRealm(realm); err != nil {
		t.Fatalf("CreateRealm failed: %v", err)
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

	searchResults, err := store.GetAllSearchResults("q", nil)
	if err != nil {
		t.Fatalf("GetAllSearchResults failed: %v", err)
	}
	if len(searchResults) != 0 {
		t.Errorf("expected search_results to be cleaned up, got %d remaining", len(searchResults))
	}

	saved, err := store.GetSavedSearches()
	if err != nil {
		t.Fatalf("GetSavedSearches failed: %v", err)
	}
	if len(saved) != 0 {
		t.Errorf("expected saved_searches to be cleaned up, got %d remaining", len(saved))
	}

	downloads, err := store.GetDownloads("")
	if err != nil {
		t.Fatalf("GetDownloads failed: %v", err)
	}
	if len(downloads) != 0 {
		t.Errorf("expected downloads to be cleaned up, got %d remaining", len(downloads))
	}

	patterns, err := store.GetAllParsePatterns()
	if err != nil {
		t.Fatalf("GetAllParsePatterns failed: %v", err)
	}
	for _, p := range patterns {
		if p.Name == "scoped" {
			t.Error("expected server-scoped parse pattern to be cleaned up")
		}
	}
	foundGlobal := false
	for _, p := range patterns {
		if p.Name == "global" {
			foundGlobal = true
		}
	}
	if !foundGlobal {
		t.Error("expected global parse pattern to survive server deletion")
	}

	realms, err := store.GetRealms(srv.ID)
	if err != nil {
		t.Fatalf("GetRealms failed: %v", err)
	}
	if len(realms) != 0 {
		t.Errorf("expected realms to cascade-delete at the DB level, got %d remaining", len(realms))
	}
}

func TestSQLiteStore_UpsertIndexedFile_EvictsRotatedPack(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	pack := 7
	old := &IndexedFile{ServerID: srv.ID, Channel: "#chan", BotNick: "xdcc", PackNumber: &pack, Filename: "Old.Content.mkv", RawLine: "raw"}
	if err := store.UpsertIndexedFile(old); err != nil {
		t.Fatalf("first UpsertIndexedFile failed: %v", err)
	}

	// The bot re-uses pack #7 for new content — the old entry must be evicted,
	// even when advertised on a different channel of the same server.
	fresh := &IndexedFile{ServerID: srv.ID, Channel: "#other", BotNick: "xdcc", PackNumber: &pack, Filename: "New.Content.mkv", RawLine: "raw2"}
	if err := store.UpsertIndexedFile(fresh); err != nil {
		t.Fatalf("second UpsertIndexedFile failed: %v", err)
	}

	stats, _ := store.GetIndexStats(srv.ID)
	if stats.TotalFiles != 1 {
		t.Fatalf("expected rotated pack to evict old entry (1 row), got %d", stats.TotalFiles)
	}
	results, _ := store.SearchIndexedFiles("content", 0, "", 10)
	if len(results) != 1 || results[0].Filename != "New.Content.mkv" {
		t.Fatalf("expected only New.Content.mkv to remain, got %+v", results)
	}

	// A different bot's pack #7 must NOT be affected.
	otherBot := &IndexedFile{ServerID: srv.ID, Channel: "#chan", BotNick: "xdcc2", PackNumber: &pack, Filename: "Other.Bot.File.mkv", RawLine: "raw3"}
	if err := store.UpsertIndexedFile(otherBot); err != nil {
		t.Fatalf("third UpsertIndexedFile failed: %v", err)
	}
	stats, _ = store.GetIndexStats(srv.ID)
	if stats.TotalFiles != 2 {
		t.Fatalf("expected 2 rows (one per bot), got %d", stats.TotalFiles)
	}

	// EvictStaleIndexedFiles with keepFilename="" removes the bot+pack entirely
	// (bot reported invalid pack).
	if err := store.EvictStaleIndexedFiles(srv.ID, "xdcc", pack, ""); err != nil {
		t.Fatalf("EvictStaleIndexedFiles failed: %v", err)
	}
	results, _ = store.SearchIndexedFiles("content", 0, "", 10)
	if len(results) != 0 {
		t.Fatalf("expected xdcc pack 7 gone after eviction, got %+v", results)
	}
	results, _ = store.SearchIndexedFiles("other bot", 0, "", 10)
	if len(results) != 1 {
		t.Fatalf("expected other bot's entry to survive, got %d", len(results))
	}
}
