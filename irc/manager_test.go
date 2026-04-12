package irc

import (
	"fmt"
	"testing"

	"github.com/maxwell-xirc/xirc/db"
)

type mockStore struct {
	servers  []db.Server
	channels map[int64][]db.Channel
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
func (m *mockStore) UpdateServer(s *db.Server) error                  { return nil }
func (m *mockStore) DeleteServer(id int64) error                      { return nil }
func (m *mockStore) GetChannels(serverID int64) ([]db.Channel, error) { return m.channels[serverID], nil }
func (m *mockStore) GetChannel(id int64) (*db.Channel, error)         { return nil, nil }
func (m *mockStore) CreateChannel(c *db.Channel) error                { return nil }
func (m *mockStore) UpdateChannel(c *db.Channel) error                { return nil }
func (m *mockStore) DeleteChannel(id int64) error                     { return nil }
func (m *mockStore) GetDownloads(status string) ([]db.Download, error) {
	return nil, nil
}
func (m *mockStore) GetDownload(id int64) (*db.Download, error)   { return nil, nil }
func (m *mockStore) CreateDownload(d *db.Download) error          { return nil }
func (m *mockStore) UpdateDownload(d *db.Download) error          { return nil }
func (m *mockStore) GetSearchResults(q string, sid int64, ch string) ([]db.SearchResult, error) {
	return nil, nil
}
func (m *mockStore) CreateSearchResult(r *db.SearchResult) error        { return nil }
func (m *mockStore) GetSavedSearches() ([]db.SavedSearch, error)        { return nil, nil }
func (m *mockStore) CreateSavedSearch(s *db.SavedSearch) error          { return nil }
func (m *mockStore) DeleteSavedSearch(id int64) error                   { return nil }
func (m *mockStore) GetParsePatterns() ([]db.ParsePattern, error)       { return nil, nil }
func (m *mockStore) UpdateParsePattern(p *db.ParsePattern) error        { return nil }
func (m *mockStore) CreateParsePattern(p *db.ParsePattern) error        { return nil }
func (m *mockStore) GetPostHooks(scope string, scopeID *int64) ([]db.PostHook, error) {
	return nil, nil
}
func (m *mockStore) GetPostHookByID(id int64) (*db.PostHook, error)     { return nil, nil }
func (m *mockStore) CreatePostHook(h *db.PostHook) error                { return nil }
func (m *mockStore) UpdatePostHook(h *db.PostHook) error                { return nil }
func (m *mockStore) DeletePostHook(id int64) error                      { return nil }
func (m *mockStore) GetFileRoutingRules() ([]db.FileRoutingRule, error)             { return nil, nil }
func (m *mockStore) GetAllFileRoutingRules() ([]db.FileRoutingRule, error)          { return nil, nil }
func (m *mockStore) GetFileRoutingRuleByID(id int64) (*db.FileRoutingRule, error)   { return nil, nil }
func (m *mockStore) CreateFileRoutingRule(r *db.FileRoutingRule) error             { return nil }
func (m *mockStore) UpdateFileRoutingRule(r *db.FileRoutingRule) error             { return nil }
func (m *mockStore) DeleteFileRoutingRule(id int64) error                              { return nil }
func (m *mockStore) CreateDownloadStat(s *db.DownloadStat) error                       { return nil }
func (m *mockStore) GetDownloadStatsSummary() (*db.DownloadStatsSummary, error)        { return nil, nil }
func (m *mockStore) GetDownloadHistory(offset, limit int) ([]db.DownloadStat, error)   { return nil, nil }
func (m *mockStore) Close() error                                                      { return nil }
func (m *mockStore) Migrate() error                                                    { return nil }

func TestManager_New(t *testing.T) {
	store := &mockStore{
		servers: []db.Server{
			{ID: 1, Name: "srv1", Host: "a.com", Port: 6667, Nickname: "bot", AutoConnect: true, Enabled: true},
			{ID: 2, Name: "srv2", Host: "b.com", Port: 6697, Nickname: "bot", AutoConnect: false, Enabled: true},
		},
		channels: map[int64][]db.Channel{
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
		channels: map[int64][]db.Channel{
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

func TestManager_GetConnection(t *testing.T) {
	store := &mockStore{
		servers: []db.Server{
			{ID: 1, Name: "srv1", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true},
		},
		channels: map[int64][]db.Channel{
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
