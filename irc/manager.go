package irc

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/maxwell-xirc/xirc/db"
)

type ServerStatus struct {
	ServerID       int64            `json:"server_id"`
	ServerName     string           `json:"server_name"`
	Status         ConnectionStatus `json:"status"`
	Channels       []string         `json:"channels"`
	ConnectedAt    *time.Time       `json:"connected_at,omitempty"`
	ReconnectCount int              `json:"reconnect_count"`
	LagMs          int64            `json:"lag_ms"`
}

type Manager struct {
	mu          sync.RWMutex
	store       db.Store
	bus         *EventBus
	connections map[int64]*Connection
}

func NewManager(store db.Store, bus *EventBus) *Manager {
	return &Manager{
		store:       store,
		bus:         bus,
		connections: make(map[int64]*Connection),
	}
}

func (m *Manager) EventBus() *EventBus {
	return m.bus
}

func (m *Manager) LoadFromStore() error {
	servers, err := m.store.GetServers()
	if err != nil {
		return fmt.Errorf("loading servers: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, srv := range servers {
		if !srv.Enabled {
			continue
		}

		realms, err := m.store.GetRealms(srv.ID)
		if err != nil {
			return fmt.Errorf("loading realms for server %s: %w", srv.Name, err)
		}

		srvCopy := srv
		conn := NewConnection(&srvCopy, realms, m.bus)
		m.connections[srv.ID] = conn
	}

	return nil
}

func (m *Manager) ConnectAutoConnect() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, conn := range m.connections {
		if conn.server.AutoConnect {
			log.Printf("auto-connecting to %s", conn.server.Name)
			if err := conn.Connect(); err != nil {
				log.Printf("failed to connect to %s: %v", conn.server.Name, err)
			}
		}
	}
}

func (m *Manager) ConnectServer(serverID int64) error {
	m.mu.RLock()
	conn, ok := m.connections[serverID]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("server %d not found", serverID)
	}

	return conn.Connect()
}

func (m *Manager) DisconnectServer(serverID int64) error {
	m.mu.RLock()
	conn, ok := m.connections[serverID]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("server %d not found", serverID)
	}

	conn.Disconnect()
	return nil
}

func (m *Manager) GetConnection(serverID int64) *Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connections[serverID]
}

func (m *Manager) GetStatuses() []ServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var statuses []ServerStatus
	for _, conn := range m.connections {
		connectedAt, reconnectCount, lagMs := conn.Stats()
		statuses = append(statuses, ServerStatus{
			ServerID:       conn.ServerID(),
			ServerName:     conn.ServerName(),
			Status:         conn.Status(),
			Channels:       conn.AllChannelNames(),
			ConnectedAt:    connectedAt,
			ReconnectCount: reconnectCount,
			LagMs:          lagMs,
		})
	}
	return statuses
}

func (m *Manager) ReloadServer(serverID int64) error {
	srv, err := m.store.GetServer(serverID)
	if err != nil {
		return fmt.Errorf("loading server %d: %w", serverID, err)
	}

	realms, err := m.store.GetRealms(serverID)
	if err != nil {
		return fmt.Errorf("loading realms for server %d: %w", serverID, err)
	}

	m.mu.Lock()
	var wasActive bool
	if existing, ok := m.connections[serverID]; ok {
		wasActive = existing.Status() != StatusDisconnected
		existing.Disconnect()
		delete(m.connections, serverID)
	}
	if !srv.Enabled {
		m.mu.Unlock()
		return nil
	}
	conn := NewConnection(srv, realms, m.bus)
	m.connections[serverID] = conn
	m.mu.Unlock()

	if wasActive {
		return conn.Connect()
	}
	return nil
}

// ReloadRealms refreshes the realm list for a server without
// disconnecting the IRC connection. Use this for channel CRUD operations
// when the server's connection settings haven't changed.
func (m *Manager) ReloadRealms(serverID int64) error {
	realms, err := m.store.GetRealms(serverID)
	if err != nil {
		return fmt.Errorf("loading realms for server %d: %w", serverID, err)
	}

	m.mu.RLock()
	conn, ok := m.connections[serverID]
	m.mu.RUnlock()

	if ok {
		conn.UpdateRealms(realms)
	}
	return nil
}

func (m *Manager) Shutdown() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, conn := range m.connections {
		conn.Disconnect()
	}
}

func (m *Manager) SendMessage(serverID int64, target, message string) error {
	conn := m.GetConnection(serverID)
	if conn == nil {
		return fmt.Errorf("server %d not found", serverID)
	}
	return conn.SendMessage(target, message)
}

func (m *Manager) Names(serverID int64, channel string) ([]string, error) {
	conn := m.GetConnection(serverID)
	if conn == nil {
		return nil, fmt.Errorf("server %d not found", serverID)
	}
	return conn.Names(channel)
}

func (m *Manager) SendRaw(serverID int64, raw string) error {
	conn := m.GetConnection(serverID)
	if conn == nil {
		return fmt.Errorf("server %d not found", serverID)
	}
	conn.SendRaw(raw)
	return nil
}

func (m *Manager) JoinChannel(serverID int64, channel, key string) error {
	conn := m.GetConnection(serverID)
	if conn == nil {
		return fmt.Errorf("server %d not found", serverID)
	}
	conn.JoinChannel(channel, key)
	return nil
}
