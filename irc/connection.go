package irc

import (
	"fmt"
	"log"
	"sync"
	"strings"
	"time"

	"github.com/maxwell-xirc/xirc/db"
)

type ConnectionStatus string

const (
	StatusDisconnected ConnectionStatus = "disconnected"
	StatusConnecting   ConnectionStatus = "connecting"
	StatusConnected    ConnectionStatus = "connected"
)

type ChannelPair struct {
	SearchChannel   string
	DownloadChannel string
	SearchCommand   string
	AutoJoin        bool
}

type Connection struct {
	mu             sync.RWMutex
	server         *db.Server
	channels       []db.Channel
	bus            *EventBus
	client         IRCClient
	status         ConnectionStatus
	stopCh         chan struct{}
	stopOnce       sync.Once
	connectedAt    *time.Time
	reconnectCount int
	lagMs          int64
	namesMu        sync.Mutex
	namesPending   map[string]chan []string // channel name -> result chan
	pingDone       chan struct{}
}

// ConnectionStatusEvent is the Data payload for EventConnectionStatus events.
type ConnectionStatusEvent struct {
	Status         string     `json:"status"`
	ConnectedAt    *time.Time `json:"connected_at,omitempty"`
	ReconnectCount int        `json:"reconnect_count"`
	LagMs          int64      `json:"lag_ms"`
}

// newIRCClient creates the appropriate IRCClient for the given server config.
func newIRCClient(srv *db.Server) IRCClient {
	addr := fmt.Sprintf("%s:%d", srv.Host, srv.Port)
	nick := srv.Nickname
	if nick == "" {
		nick = "xirc"
	}
	cfg := RawClientConfig{
		Addr:     addr,
		Nick:     nick,
		User:     nick,
		Realname: "xirc XDCC client",
		UseTLS:   srv.SSL,
	}
	switch srv.AuthMethod {
	case "sasl":
		cfg.SASLUser = nick
		cfg.SASLPass = srv.AuthPassword
	case "nickserv":
		cfg.NickServPass = srv.AuthPassword
	}
	return NewRawClient(cfg)
}

func NewConnection(server *db.Server, channels []db.Channel, bus *EventBus) *Connection {
	return &Connection{
		server:       server,
		channels:     channels,
		bus:          bus,
		status:       StatusDisconnected,
		stopCh:       make(chan struct{}),
		namesPending: make(map[string]chan []string),
		pingDone:     make(chan struct{}),
	}
}

func (c *Connection) ServerID() int64 {
	return c.server.ID
}

func (c *Connection) ServerName() string {
	return c.server.Name
}

func (c *Connection) Status() ConnectionStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

func (c *Connection) setStatus(s ConnectionStatus) {
	c.mu.Lock()
	c.status = s
	connectedAt := c.connectedAt
	reconnectCount := c.reconnectCount
	lagMs := c.lagMs
	if lagMs < 0 {
		lagMs = 0
	}
	c.mu.Unlock()

	c.bus.Publish(Event{
		Type:     EventConnectionStatus,
		ServerID: c.server.ID,
		Data: ConnectionStatusEvent{
			Status:         string(s),
			ConnectedAt:    connectedAt,
			ReconnectCount: reconnectCount,
			LagMs:          lagMs,
		},
	})
}

func (c *Connection) ChannelPairs() []ChannelPair {
	var pairs []ChannelPair
	for _, ch := range c.channels {
		if !ch.Enabled {
			continue
		}
		dl := ch.DownloadChannel
		if dl == "" {
			dl = ch.Name
		}
		cmd := ch.SearchCommand
		if cmd == "" {
			cmd = "!s"
		}
		pairs = append(pairs, ChannelPair{
			SearchChannel:   ch.Name,
			DownloadChannel: dl,
			SearchCommand:   cmd,
			AutoJoin:        ch.AutoJoin,
		})
	}
	return pairs
}

func (c *Connection) AllChannelNames() []string {
	seen := map[string]bool{}
	var names []string
	for _, pair := range c.ChannelPairs() {
		for _, name := range []string{pair.SearchChannel, pair.DownloadChannel} {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	return names
}

func (c *Connection) Connect() error {
	c.setStatus(StatusConnecting)
	go c.connectLoop()
	return nil
}

// applyHandlers wires all event handlers onto a freshly-created IRCClient.
// It must be called before the client's Connect() is invoked.
func (c *Connection) applyHandlers(client IRCClient) {
	client.OnConnect(func() {
		now := time.Now()
		c.mu.Lock()
		c.connectedAt = &now
		c.mu.Unlock()

		c.setStatus(StatusConnected)
		log.Printf("[%s] connected", c.server.Name)

		c.mu.Lock()
		// Stop any prior pingLoop by closing the old done channel, then replace it.
		select {
		case <-c.pingDone:
			// already closed, make a new one
		default:
			close(c.pingDone)
		}
		c.pingDone = make(chan struct{})
		done := c.pingDone
		c.mu.Unlock()
		go c.pingLoop(client, done)

		// Auto-join channels
		for _, ch := range c.channels {
			if !ch.Enabled || !ch.AutoJoin {
				continue
			}
			client.Join(ch.Name, ch.Key)
			// Also join download channel if different
			if ch.DownloadChannel != "" && ch.DownloadChannel != ch.Name {
				client.Join(ch.DownloadChannel, "")
			}
		}
	})

	client.OnDisconnect(func() {
		c.setStatus(StatusDisconnected)
		log.Printf("[%s] disconnected", c.server.Name)
	})

	client.OnMessage(func(nick, target, message string) {
		c.bus.Publish(Event{
			Type:     EventIRCMessage,
			ServerID: c.server.ID,
			Channel:  target,
			Nick:     nick,
			Data: map[string]string{
				"type":    "privmsg",
				"message": message,
			},
		})
	})

	client.OnNotice(func(nick, target, message string) {
		c.bus.Publish(Event{
			Type:     EventIRCMessage,
			ServerID: c.server.ID,
			Channel:  target,
			Nick:     nick,
			Data: map[string]string{
				"type":    "notice",
				"message": message,
			},
		})
	})

	client.OnRaw(func(line string) {
		c.handleRawLine(line)
	})
}

func (c *Connection) connectLoop() {
	backoff := []time.Duration{5, 10, 30, 60, 120}
	attempt := 0

	for {
		select {
		case <-c.stopCh:
			return
		default:
		}

		if attempt > 0 {
			c.mu.Lock()
			c.reconnectCount++
			c.mu.Unlock()
		}

		c.setStatus(StatusConnecting)

		// Create a fresh client for each attempt so handlers and state are clean.
		client := newIRCClient(c.server)
		c.applyHandlers(client)

		c.mu.Lock()
		c.client = client
		c.mu.Unlock()

		err := client.Connect()
		select {
		case <-c.stopCh:
			return // clean shutdown, not a failure
		default:
		}

		if err != nil {
			delay := backoff[len(backoff)-1]
			if attempt < len(backoff) {
				delay = backoff[attempt]
			}
			attempt++

			log.Printf("[%s] connection failed: %v, retrying in %ds", c.server.Name, err, delay)
			c.bus.Publish(Event{
				Type:     EventError,
				ServerID: c.server.ID,
				Data: ErrorEvent{
					ErrorType: "irc_disconnect",
					ServerID:  c.server.ID,
					Message:   fmt.Sprintf("[%s] connection failed: %v", c.server.Name, err),
					Timestamp: time.Now().Format(time.RFC3339),
				},
			})
			c.setStatus(StatusDisconnected)

			select {
			case <-time.After(delay * time.Second):
			case <-c.stopCh:
				return
			}
			continue
		}

		// err == nil: clean disconnect (e.g. via Disconnect() → Close()).
		return
	}
}

func (c *Connection) Disconnect() {
	c.stopOnce.Do(func() { close(c.stopCh) })

	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client != nil {
		client.Close()
	}
	c.setStatus(StatusDisconnected)
}

func (c *Connection) JoinChannel(name, key string) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client != nil {
		client.Join(name, key)
	}
}

func (c *Connection) PartChannel(name string) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client != nil {
		client.Part(name)
	}
}

func (c *Connection) SendMessage(target, message string) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client != nil {
		client.Privmsg(target, message)
	}
}

func (c *Connection) SendRaw(raw string) {
	// Raw send is not exposed on IRCClient; log a warning.
	// For XDCC this path is not exercised; use SendMessage for all real sends.
	log.Printf("[%s] SendRaw called but not supported by IRCClient: %s", c.server.Name, raw)
}

func (c *Connection) IsInChannel(name string) bool {
	// Without girc's channel tracking, we maintain our own joined-channel set
	// via the auto-join list. For now, check if name is among our channel list.
	target := strings.ToLower(name)
	for _, ch := range c.AllChannelNames() {
		if strings.ToLower(ch) == target {
			return true
		}
	}
	return false
}

func (c *Connection) Search(searchChannel, command, query string) error {
	c.mu.RLock()
	client := c.client
	status := c.status
	c.mu.RUnlock()

	if client == nil || status != StatusConnected {
		return fmt.Errorf("not connected to %s", c.server.Name)
	}

	msg := fmt.Sprintf("%s %s", command, query)
	client.Privmsg(searchChannel, msg)
	return nil
}

func (c *Connection) RequestPack(channel, botNick string, packNumber int) error {
	c.mu.RLock()
	client := c.client
	status := c.status
	c.mu.RUnlock()

	if client == nil || status != StatusConnected {
		return fmt.Errorf("not connected to %s", c.server.Name)
	}

	msg := fmt.Sprintf("xdcc send #%d", packNumber)
	client.Privmsg(botNick, msg)
	return nil
}

func (c *Connection) pingLoop(client IRCClient, done <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			sent := time.Now().UnixNano()
			c.mu.Lock()
			c.lagMs = -sent // negative = ping in flight
			c.mu.Unlock()
			client.SendLine(fmt.Sprintf("PING :xirc%d", sent))
		case <-c.stopCh:
			return
		case <-done:
			return
		}
	}
}

func (c *Connection) handleRawLine(line string) {
	// PONG :<token>  (bare or server-prefixed: ":server PONG server :token")
	if strings.HasPrefix(line, "PONG ") || strings.Contains(line, " PONG ") {
		c.mu.Lock()
		if c.lagMs < 0 {
			sentNano := -c.lagMs
			c.lagMs = (time.Now().UnixNano() - sentNano) / int64(time.Millisecond)
		}
		c.mu.Unlock()
		return
	}

	// Publish every non-PONG raw line to the server-level message stream
	// (channel="") so the server view can display it.
	c.bus.Publish(Event{
		Type:     EventIRCMessage,
		ServerID: c.server.ID,
		Channel:  "",
		Nick:     "",
		Data: map[string]string{
			"type":    "raw",
			"message": line,
		},
	})

	// :server 353 nick = #channel :nick1 nick2 ...
	// :server 366 nick #channel :End of NAMES
	parts := strings.SplitN(line, " ", 5)
	if len(parts) < 4 {
		return
	}
	code := parts[1]
	switch code {
	case "353":
		if len(parts) < 5 {
			return
		}
		// parts[4] = "#channel :nick1 nick2 ..."  (parts[3] is the mode symbol "=", "@", "*")
		chanAndNicks := strings.SplitN(parts[4], " :", 2)
		if len(chanAndNicks) < 2 {
			return
		}
		ch := strings.TrimSpace(chanAndNicks[0])
		nicks := strings.Fields(chanAndNicks[1])
		c.namesMu.Lock()
		if resCh, ok := c.namesPending[ch]; ok {
			resCh <- nicks
		}
		c.namesMu.Unlock()
	case "366":
		ch := parts[3]
		c.namesMu.Lock()
		if resCh, ok := c.namesPending[ch]; ok {
			close(resCh)
			delete(c.namesPending, ch)
		}
		c.namesMu.Unlock()
	}
}

// Names sends a NAMES command and collects the response (timeout 5s).
func (c *Connection) Names(channel string) ([]string, error) {
	c.mu.RLock()
	client := c.client
	status := c.status
	c.mu.RUnlock()

	if client == nil || status != StatusConnected {
		return nil, fmt.Errorf("not connected")
	}

	resCh := make(chan []string, 20)
	c.namesMu.Lock()
	c.namesPending[channel] = resCh
	c.namesMu.Unlock()

	client.SendLine("NAMES " + channel)

	var nicks []string
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case batch, ok := <-resCh:
			if !ok {
				return nicks, nil
			}
			nicks = append(nicks, batch...)
		case <-timer.C:
			c.namesMu.Lock()
			delete(c.namesPending, channel)
			c.namesMu.Unlock()
			return nicks, fmt.Errorf("names timeout")
		}
	}
}

// Stats returns current connection statistics.
func (c *Connection) Stats() (connectedAt *time.Time, reconnectCount int, lagMs int64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	lag := c.lagMs
	if lag < 0 {
		lag = 0 // ping in flight, report 0
	}
	return c.connectedAt, c.reconnectCount, lag
}
