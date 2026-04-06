package db

import (
	"os"
	"testing"
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
	for _, table := range []string{"file_routing_rules", "post_hooks", "parse_patterns", "saved_searches", "search_results", "downloads", "channels", "servers"} {
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

func TestMySQLStore_CreateAndGetChannel(t *testing.T) {
	store := newTestMySQLStore(t)
	defer store.Close()

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	ch := &Channel{
		ServerID:        srv.ID,
		Name:            "#test",
		SearchCommand:   "!search",
		DownloadChannel: "#test-dl",
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
	if channels[0].DownloadChannel != "#test-dl" {
		t.Errorf("expected download_channel #test-dl, got %s", channels[0].DownloadChannel)
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
