package db

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
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
		store.Close()
		os.RemoveAll(dir)
		t.Fatalf("failed to run migrations: %v", err)
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

func TestSQLiteStore_CreateAndGetChannel(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	ch := &Channel{
		ServerID:        srv.ID,
		Name:            "#test",
		Key:             "secret",
		SearchCommand:   "!search",
		DownloadChannel: "#test-downloads",
		AutoJoin:        true,
		Enabled:         true,
	}

	if err := store.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel failed: %v", err)
	}

	channels, err := store.GetChannels(srv.ID)
	if err != nil {
		t.Fatalf("GetChannels failed: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(channels))
	}
	if channels[0].Name != "#test" {
		t.Errorf("expected channel #test, got %s", channels[0].Name)
	}
	if channels[0].DownloadChannel != "#test-downloads" {
		t.Errorf("expected download_channel #test-downloads, got %s", channels[0].DownloadChannel)
	}
	if channels[0].SearchCommand != "!search" {
		t.Errorf("expected search_command !search, got %s", channels[0].SearchCommand)
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
