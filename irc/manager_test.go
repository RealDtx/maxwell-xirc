package irc

import (
	"fmt"
	"testing"
	"time"

	"github.com/RealDtx/maxwell-irc/db"
)

type mockStore struct {
	servers []db.Server
	realms  map[int64][]db.Realm
}

func (m *mockStore) GetServers() ([]db.Server, error) { return m.servers, nil }
func (m *mockStore) GetServer(id int64) (*db.Server, error) {
	for i := range m.servers {
		if m.servers[i].ID == id {
			return &m.servers[i], nil
		}
	}
	return nil, fmt.Errorf("server %d not found", id)
}
func (m *mockStore) CreateServer(s *db.Server) error {
	if s.ID == 0 {
		s.ID = int64(len(m.servers) + 1)
	}
	m.servers = append(m.servers, *s)
	return nil
}
func (m *mockStore) UpdateServer(s *db.Server) error                     { return nil }
func (m *mockStore) DeleteServer(id int64) error                         { return nil }
func (m *mockStore) GetRealms(serverID int64) ([]db.Realm, error)        { return m.realms[serverID], nil }
func (m *mockStore) GetRealm(id int64) (*db.Realm, error)                { return nil, nil }
func (m *mockStore) CreateRealm(r *db.Realm) error                       { return nil }
func (m *mockStore) UpdateRealm(r *db.Realm) error                       { return nil }
func (m *mockStore) DeleteRealm(id int64) error                          { return nil }
func (m *mockStore) UpdateRealmSearchBot(id int64, botNick string) error { return nil }
func (m *mockStore) GetDownloads(status string) ([]db.Download, error) {
	return nil, nil
}
func (m *mockStore) GetDownload(id int64) (*db.Download, error) { return nil, nil }
func (m *mockStore) CreateDownload(d *db.Download) error        { return nil }
func (m *mockStore) UpdateDownload(d *db.Download) error        { return nil }
func (m *mockStore) DeleteDownloads(ids []int64) (int64, error) { return 0, nil }
func (m *mockStore) DeleteDownloadsByStatus(status string) (int64, error) {
	return 0, nil
}
func (m *mockStore) GetSearchResults(q string, sid int64, r string) ([]db.SearchResult, error) {
	return nil, nil
}
func (m *mockStore) GetAllSearchResults(q string, since *time.Time) ([]db.SearchResult, error) {
	return nil, nil
}
func (m *mockStore) CreateSearchResult(r *db.SearchResult) error { return nil }
func (m *mockStore) DeleteSearchResults(serverID int64, channel string) error {
	return nil
}
func (m *mockStore) GetSavedSearches() ([]db.SavedSearch, error)     { return nil, nil }
func (m *mockStore) CreateSavedSearch(s *db.SavedSearch) error       { return nil }
func (m *mockStore) DeleteSavedSearch(id int64) error                { return nil }
func (m *mockStore) GetParsePatterns() ([]db.ParsePattern, error)    { return nil, nil }
func (m *mockStore) GetAllParsePatterns() ([]db.ParsePattern, error) { return nil, nil }
func (m *mockStore) GetParsePatternsForChannel(_ int64, _ string) ([]db.ParsePattern, error) {
	return nil, nil
}
func (m *mockStore) UpdateParsePattern(p *db.ParsePattern) error                     { return nil }
func (m *mockStore) RecordPatternMatch(int64, int, time.Time) error                  { return nil }
func (m *mockStore) CreateParsePattern(p *db.ParsePattern) error                     { return nil }
func (m *mockStore) CreateDownloadStat(s *db.DownloadStat) error                     { return nil }
func (m *mockStore) GetDownloadStatsSummary() (*db.DownloadStatsSummary, error)      { return nil, nil }
func (m *mockStore) GetDownloadHistory(offset, limit int) ([]db.DownloadStat, error) { return nil, nil }
func (m *mockStore) GetUnparsedSearchSamples(serverID int64, query string, since time.Time, limit int) ([]db.SearchResult, error) {
	return nil, nil
}
func (m *mockStore) GetAllUnparsedSince(since time.Time) ([]db.SearchResult, error) { return nil, nil }
func (m *mockStore) MarkSearchResultParsed(id int64, botNick string, packNumber *int, filename *string, filesize *string, downloadsCount *int) error {
	return nil
}
func (m *mockStore) UpsertIndexedFile(f *db.IndexedFile) error { return nil }
func (m *mockStore) EvictStaleIndexedFiles(serverID int64, botNick string, packNumber int, keepFilename string) error {
	return nil
}
func (m *mockStore) SearchIndexedFiles(query string, serverID int64, channel string, limit int) ([]db.IndexedFile, error) {
	return nil, nil
}
func (m *mockStore) GetIndexStats(serverID int64) (*db.IndexStats, error) {
	return &db.IndexStats{}, nil
}
func (m *mockStore) ClearIndex(serverID int64) error { return nil }
func (m *mockStore) GetIndexStatsDetail() (*db.IndexStatsDetail, error) {
	return &db.IndexStatsDetail{}, nil
}
func (m *mockStore) PruneSearchResults(olderThan time.Time) (int64, error) { return 0, nil }
func (m *mockStore) EnforceIndexCap(maxFiles int64) (int64, error)         { return 0, nil }
func (m *mockStore) ForEachIndexedFile(serverIDs []int64, fn func(*db.IndexedFile) error) error {
	return nil
}
func (m *mockStore) BulkMergeIndexedFiles(files []db.IndexedFile) (db.MergeResult, error) {
	return db.MergeResult{}, nil
}
func (m *mockStore) Close() error                                             { return nil }
func (m *mockStore) Migrate() error                                           { return nil }
func (m *mockStore) CreateUser(u *db.User) error                              { return nil }
func (m *mockStore) GetUser(id int64) (*db.User, error)                       { return nil, nil }
func (m *mockStore) GetUserByName(name string) (*db.User, error)              { return nil, nil }
func (m *mockStore) ListUsers() ([]db.User, error)                            { return nil, nil }
func (m *mockStore) UpdateUser(u *db.User) error                              { return nil }
func (m *mockStore) DeleteUser(id int64) error                                { return nil }
func (m *mockStore) CountUsers() (int, error)                                 { return 0, nil }
func (m *mockStore) CountAdmins() (int, error)                                { return 0, nil }
func (m *mockStore) CreateSession(s *db.Session) error                        { return nil }
func (m *mockStore) GetSession(tokenHash string) (*db.Session, error)         { return nil, nil }
func (m *mockStore) TouchSession(tokenHash string, expiresAt time.Time) error { return nil }
func (m *mockStore) DeleteSession(tokenHash string) error                     { return nil }
func (m *mockStore) DeleteUserSessions(userID int64) error                    { return nil }
func (m *mockStore) DeleteExpiredSessions(now time.Time) (int64, error)       { return 0, nil }

func TestManager_New(t *testing.T) {
	store := &mockStore{
		servers: []db.Server{
			{ID: 1, Name: "srv1", Host: "a.com", Port: 6667, Nickname: "bot", AutoConnect: true, Enabled: true},
			{ID: 2, Name: "srv2", Host: "b.com", Port: 6697, Nickname: "bot", AutoConnect: false, Enabled: true},
		},
		realms: map[int64][]db.Realm{
			1: {{ID: 1, ServerID: 1, Name: "#test", AutoJoin: true, Enabled: true}},
			2: {{ID: 2, ServerID: 2, Name: "#other", AutoJoin: true, Enabled: true}},
		},
	}

	bus := NewEventBus()
	mgr := NewManager(store, bus)

	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestManager_GetStatus(t *testing.T) {
	store := &mockStore{
		servers: []db.Server{
			{ID: 1, Name: "srv1", Host: "a.com", Port: 6667, Nickname: "bot", AutoConnect: false, Enabled: true},
		},
		realms: map[int64][]db.Realm{
			1: {{ID: 1, ServerID: 1, Name: "#test", AutoJoin: true, Enabled: true}},
		},
	}

	bus := NewEventBus()
	mgr := NewManager(store, bus)

	if err := mgr.LoadFromStore(); err != nil {
		t.Fatalf("LoadFromStore failed: %v", err)
	}

	statuses := mgr.GetStatuses()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].ServerName != "srv1" {
		t.Errorf("expected server name srv1, got %s", statuses[0].ServerName)
	}
	if statuses[0].Status != StatusDisconnected {
		t.Errorf("expected disconnected, got %s", statuses[0].Status)
	}
}

func TestManager_ReloadRealms_UpdatesChannelList(t *testing.T) {
	store := &mockStore{
		servers: []db.Server{
			{ID: 1, Name: "srv1", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true},
		},
		realms: map[int64][]db.Realm{
			1: {{ID: 1, ServerID: 1, Name: "#orig", AutoJoin: true, Enabled: true}},
		},
	}

	bus := NewEventBus()
	mgr := NewManager(store, bus)
	if err := mgr.LoadFromStore(); err != nil {
		t.Fatalf("LoadFromStore: %v", err)
	}

	// Add a second channel to the store, then reload.
	store.realms[1] = append(store.realms[1], db.Realm{ID: 2, ServerID: 1, Name: "#new", AutoJoin: true, Enabled: true})

	if err := mgr.ReloadRealms(1); err != nil {
		t.Fatalf("ReloadRealms: %v", err)
	}

	conn := mgr.GetConnection(1)
	if conn == nil {
		t.Fatal("expected connection")
	}
	names := conn.AllChannelNames()
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["#orig"] || !found["#new"] {
		t.Errorf("expected both realms after reload, got %v", names)
	}
}

func TestManager_ReloadRealms_NoopForUnknownServer(t *testing.T) {
	store := &mockStore{
		servers: []db.Server{},
		realms:  map[int64][]db.Realm{},
	}

	bus := NewEventBus()
	mgr := NewManager(store, bus)

	// Server 99 has no connection; ReloadRealms should not error.
	if err := mgr.ReloadRealms(99); err != nil {
		t.Fatalf("expected no error for unknown server, got %v", err)
	}
}

func TestManager_ReloadRealms_DoesNotDisconnect(t *testing.T) {
	store := &mockStore{
		servers: []db.Server{
			{ID: 1, Name: "srv1", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true},
		},
		realms: map[int64][]db.Realm{
			1: {{ID: 1, ServerID: 1, Name: "#test", AutoJoin: true, Enabled: true}},
		},
	}

	bus := NewEventBus()
	mgr := NewManager(store, bus)
	if err := mgr.LoadFromStore(); err != nil {
		t.Fatalf("LoadFromStore: %v", err)
	}

	conn := mgr.GetConnection(1)
	conn.mu.Lock()
	conn.status = StatusConnected
	conn.mu.Unlock()

	if err := mgr.ReloadRealms(1); err != nil {
		t.Fatalf("ReloadRealms: %v", err)
	}

	// Connection must still be the same object and still connected.
	if mgr.GetConnection(1) != conn {
		t.Error("ReloadRealms replaced the connection; it should not have")
	}
	if conn.Status() != StatusConnected {
		t.Errorf("expected still connected after ReloadRealms, got %s", conn.Status())
	}
}

func TestManager_ReloadServer_DoesNotConnectIfWasDisconnected(t *testing.T) {
	store := &mockStore{
		servers: []db.Server{
			{ID: 1, Name: "srv1", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true},
		},
		realms: map[int64][]db.Realm{
			1: {{ID: 1, ServerID: 1, Name: "#test", AutoJoin: true, Enabled: true}},
		},
	}

	bus := NewEventBus()
	mgr := NewManager(store, bus)
	if err := mgr.LoadFromStore(); err != nil {
		t.Fatalf("LoadFromStore: %v", err)
	}

	if err := mgr.ReloadServer(1); err != nil {
		t.Fatalf("ReloadServer: %v", err)
	}

	conn := mgr.GetConnection(1)
	if conn == nil {
		t.Fatal("expected connection after reload")
	}
	if conn.Status() != StatusDisconnected {
		t.Errorf("expected disconnected after reload of disconnected server, got %s", conn.Status())
	}
}

func TestManager_ReloadServer_ReconnectsIfWasConnected(t *testing.T) {
	store := &mockStore{
		servers: []db.Server{
			{ID: 1, Name: "srv1", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true},
		},
		realms: map[int64][]db.Realm{
			1: {{ID: 1, ServerID: 1, Name: "#test", AutoJoin: true, Enabled: true}},
		},
	}

	bus := NewEventBus()
	mgr := NewManager(store, bus)
	if err := mgr.LoadFromStore(); err != nil {
		t.Fatalf("LoadFromStore: %v", err)
	}

	// Simulate the connection being active (connected).
	orig := mgr.GetConnection(1)
	orig.mu.Lock()
	orig.status = StatusConnected
	orig.mu.Unlock()

	if err := mgr.ReloadServer(1); err != nil {
		t.Fatalf("ReloadServer: %v", err)
	}

	newConn := mgr.GetConnection(1)
	if newConn == nil {
		t.Fatal("expected new connection after reload")
	}
	if newConn == orig {
		t.Fatal("expected a new connection object, got the same one")
	}
	// Connect() sets StatusConnecting synchronously before launching the goroutine.
	if newConn.Status() != StatusConnecting {
		t.Errorf("expected connecting after reload of active server, got %s", newConn.Status())
	}

	// Clean up: stop the connect loop.
	newConn.Disconnect()
}

func TestManager_ReloadServer_ReconnectsIfWasConnecting(t *testing.T) {
	store := &mockStore{
		servers: []db.Server{
			{ID: 1, Name: "srv1", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true},
		},
		realms: map[int64][]db.Realm{
			1: {},
		},
	}

	bus := NewEventBus()
	mgr := NewManager(store, bus)
	if err := mgr.LoadFromStore(); err != nil {
		t.Fatalf("LoadFromStore: %v", err)
	}

	orig := mgr.GetConnection(1)
	orig.mu.Lock()
	orig.status = StatusConnecting
	orig.mu.Unlock()

	if err := mgr.ReloadServer(1); err != nil {
		t.Fatalf("ReloadServer: %v", err)
	}

	newConn := mgr.GetConnection(1)
	if newConn.Status() != StatusConnecting {
		t.Errorf("expected connecting after reload of connecting server, got %s", newConn.Status())
	}

	newConn.Disconnect()
}

func TestManager_GetConnection(t *testing.T) {
	store := &mockStore{
		servers: []db.Server{
			{ID: 1, Name: "srv1", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true},
		},
		realms: map[int64][]db.Realm{
			1: {},
		},
	}

	bus := NewEventBus()
	mgr := NewManager(store, bus)
	mgr.LoadFromStore()

	conn := mgr.GetConnection(1)
	if conn == nil {
		t.Fatal("expected connection for server 1")
	}

	conn2 := mgr.GetConnection(999)
	if conn2 != nil {
		t.Error("expected nil for nonexistent server")
	}
}

// Disconnect closes a connection's stopCh for good; Connect afterwards must
// not leave the server stuck in "connecting" (the UI's yellow dot).
func TestManager_ConnectAfterDisconnect(t *testing.T) {
	store := &mockStore{servers: []db.Server{
		{ID: 1, Name: "srv1", Host: "127.0.0.1", Port: 1, Nickname: "bot", Enabled: true},
	}}
	mgr := NewManager(store, NewEventBus())
	if err := mgr.LoadFromStore(); err != nil {
		t.Fatal(err)
	}
	if err := mgr.ConnectServer(1); err != nil {
		t.Fatal(err)
	}
	mgr.DisconnectServer(1)
	old := mgr.GetConnection(1)
	if err := mgr.ConnectServer(1); err != nil {
		t.Fatal(err)
	}
	conn := mgr.GetConnection(1)
	if conn == old {
		t.Fatal("expected a fresh connection after disconnect")
	}
	select {
	case <-conn.stopCh:
		t.Fatal("fresh connection must not be stopped")
	default:
	}
	defer conn.Disconnect()
	if err := conn.Connect(); err != nil { // second Connect while running: no-op
		t.Fatal(err)
	}
	conn.mu.RLock()
	running := conn.running
	conn.mu.RUnlock()
	if !running {
		t.Error("connect loop should be marked running")
	}
}
