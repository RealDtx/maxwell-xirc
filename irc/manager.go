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

		channels, err := m.store.GetChannels(srv.ID)
		if err != nil {
			return fmt.Errorf("loading channels for server %s: %w", srv.Name, err)
		}

		srvCopy := srv
		conn := NewConnection(&srvCopy, channels, m.bus)
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
	m.mu.Lock()
	defer m.mu.Unlock()

	// Disconnect existing if present
	if existing, ok := m.connections[serverID]; ok {
		existing.Disconnect()
		delete(m.connections, serverID)
	}

	srv, err := m.store.GetServer(serverID)
	if err != nil {
		return fmt.Errorf("loading server %d: %w", serverID, err)
	}
	if !srv.Enabled {
		return nil
	}

	channels, err := m.store.GetChannels(serverID)
	if err != nil {
		return fmt.Errorf("loading channels for server %d: %w", serverID, err)
	}

	conn := NewConnection(srv, channels, m.bus)
	m.connections[serverID] = conn
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
	conn.SendMessage(target, message)
	return nil
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
