package irc

import (
	"fmt"
	"log"
	"strings"
	"sync"
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
	realms         []db.Realm
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

func NewConnection(server *db.Server, realms []db.Realm, bus *EventBus) *Connection {
	return &Connection{
		server:       server,
		realms:       realms,
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
	c.mu.RLock()
	defer c.mu.RUnlock()

	var pairs []ChannelPair
	for _, r := range c.realms {
		if !r.Enabled {
			continue
		}
		dl := r.DownloadChannel
		if dl == "" {
			dl = r.Name
		}
		cmd := r.SearchCommand
		if cmd == "" {
			cmd = "!s"
		}
		pairs = append(pairs, ChannelPair{
			SearchChannel:   r.Name,
			DownloadChannel: dl,
			SearchCommand:   cmd,
			AutoJoin:        r.AutoJoin,
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

// UpdateRealms replaces the channel list and, if currently connected,
// joins any enabled+autojoin channels that are not already being tracked.
// IRC JOIN is idempotent so rejoining an existing channel is harmless.
func (c *Connection) UpdateRealms(realms []db.Realm) {
	c.mu.Lock()
	old := c.realms
	c.realms = realms
	status := c.status
	client := c.client
	c.mu.Unlock()

	if status != StatusConnected || client == nil {
		return
	}

	oldNames := make(map[string]bool, len(old))
	for _, r := range old {
		oldNames[r.Name] = true
	}

	for _, r := range realms {
		if !r.Enabled || !r.AutoJoin {
			continue
		}
		if !oldNames[r.Name] {
			client.Join(r.Name, r.Key)
		}
		if r.DownloadChannel != "" && r.DownloadChannel != r.Name && !oldNames[r.DownloadChannel] {
			client.Join(r.DownloadChannel, "")
		}
	}
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
		c.mu.RLock()
		realms := make([]db.Realm, len(c.realms))
		copy(realms, c.realms)
		c.mu.RUnlock()

		for _, r := range realms {
			if !r.Enabled || !r.AutoJoin {
				continue
			}
			client.Join(r.Name, r.Key)
			// Also join download channel if different
			if r.DownloadChannel != "" && r.DownloadChannel != r.Name {
				client.Join(r.DownloadChannel, "")
			}
		}
	})

	client.OnDisconnect(func() {
		c.setStatus(StatusDisconnected)
		log.Printf("[%s] disconnected", c.server.Name)
	})

	client.OnMessage(func(nick, target, message string) {
		channel := target
		// DM detection: if target is not a channel (doesn't start with #, &, !, +),
		// it's a direct message to us. Key it by the sender's nick.
		if len(target) > 0 && target[0] != '#' && target[0] != '&' && target[0] != '!' && target[0] != '+' {
			channel = nick
		}
		c.bus.Publish(Event{
			Type:      EventIRCMessage,
			ServerID:  c.server.ID,
			Channel:   channel,
			Nick:      nick,
			Timestamp: time.Now().Format(time.RFC3339Nano),
			Data: map[string]string{
				"type":    "privmsg",
				"message": message,
			},
		})
	})

	client.OnNotice(func(nick, target, message string) {
		channel := target
		if len(target) > 0 && target[0] != '#' && target[0] != '&' && target[0] != '!' && target[0] != '+' {
			channel = nick
		}
		c.bus.Publish(Event{
			Type:      EventIRCMessage,
			ServerID:  c.server.ID,
			Channel:   channel,
			Nick:      nick,
			Timestamp: time.Now().Format(time.RFC3339Nano),
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
		// Echo own sent message into the bus so the MessageBuffer captures it.
		// IRC servers don't echo our own PRIVMSGs back, so without this the
		// message would disappear from the chat buffer on any reload.
		c.bus.Publish(Event{
			Type:      EventIRCMessage,
			ServerID:  c.server.ID,
			Channel:   target,
			Nick:      c.server.Nickname,
			Timestamp: time.Now().Format(time.RFC3339Nano),
			Data: map[string]string{
				"type":    "privmsg",
				"message": message,
			},
		})
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
	// PONG — handle latency tracking, don't publish.
	if strings.HasPrefix(line, "PONG ") || strings.Contains(line, " PONG ") {
		c.mu.Lock()
		if c.lagMs < 0 {
			sentNano := -c.lagMs
			c.lagMs = (time.Now().UnixNano() - sentNano) / int64(time.Millisecond)
		}
		c.mu.Unlock()
		return
	}

	// Skip user PRIVMSG and NOTICE raw lines — they are already handled by
	// OnMessage/OnNotice and published with the correct channel target.
	// Publishing them again here would cause duplicates in the server view.
	parts := strings.SplitN(line, " ", 5)
	if len(parts) >= 3 {
		cmd := strings.ToUpper(parts[1])
		if cmd == "PRIVMSG" || cmd == "NOTICE" {
			// Server notices have no '!' in the prefix (nick!user@host format).
			// User messages always have a '!' in their prefix. Skip user messages only.
			prefix := parts[0]
			if strings.Contains(prefix, "!") {
				return
			}
			// Server NOTICE (no '!' in prefix) — fall through to publish as raw server message.
		}
	}

	ts := time.Now().Format(time.RFC3339Nano)

	// Publish server-level raw lines (numerics, MODE, JOIN, PART, QUIT, etc.)
	c.bus.Publish(Event{
		Type:      EventIRCMessage,
		ServerID:  c.server.ID,
		Channel:   "",
		Nick:      "",
		Timestamp: ts,
		Data: map[string]string{
			"type":    "raw",
			"message": line,
		},
	})

	// Also publish a copy to the relevant channel buffer so JOIN/PART/MODE/TOPIC
	// activity appears in the channel chat view.
	if ch := extractChannelFromRaw(parts); ch != "" {
		c.bus.Publish(Event{
			Type:      EventIRCMessage,
			ServerID:  c.server.ID,
			Channel:   ch,
			Nick:      "",
			Timestamp: ts,
			Data: map[string]string{
				"type":    "raw",
				"message": line,
			},
		})
	}

	// Handle NAMES (353/366) replies
	c.handleNamesReply(parts)
}

// extractChannelFromRaw returns the channel name from a raw IRC line (JOIN, PART, MODE, KICK,
// TOPIC, 332) so that those events can also be stored in the channel-specific buffer.
// parts must be from strings.SplitN(line, " ", 5). Returns "" if no channel found.
func extractChannelFromRaw(parts []string) string {
	if len(parts) < 3 {
		return ""
	}
	cmd := strings.ToUpper(parts[1])
	isChannel := func(s string) bool {
		s = strings.TrimPrefix(s, ":")
		return len(s) > 0 && (s[0] == '#' || s[0] == '&' || s[0] == '!' || s[0] == '+')
	}
	switch cmd {
	case "JOIN":
		// :nick!user@host JOIN :#channel  OR  :nick!user@host JOIN #channel
		if len(parts) >= 3 {
			return strings.TrimPrefix(strings.TrimPrefix(parts[2], ":"), ":")
		}
	case "PART", "MODE", "KICK", "TOPIC":
		// :nick!user@host PART #channel :reason
		if len(parts) >= 3 && isChannel(parts[2]) {
			return strings.TrimPrefix(parts[2], ":")
		}
	case "332", "333", "366":
		// :server 332 nick #channel :topic
		if len(parts) >= 4 && isChannel(parts[3]) {
			return strings.TrimPrefix(parts[3], ":")
		}
	case "353":
		// :server 353 nick = #channel :nicks  — parts[4] starts with "= #channel" or "@ #channel"
		if len(parts) >= 5 {
			p := strings.TrimPrefix(parts[4], ":")
			fields := strings.Fields(p)
			if len(fields) >= 2 && isChannel(fields[1]) {
				return fields[1]
			}
			if len(fields) >= 1 && isChannel(fields[0]) {
				return fields[0]
			}
		}
	}
	return ""
}

// handleNamesReply processes 353 (RPL_NAMREPLY) and 366 (RPL_ENDOFNAMES) numerics.
// parts must be from strings.SplitN(line, " ", 5).
func (c *Connection) handleNamesReply(parts []string) {
	if len(parts) < 4 {
		return
	}
	code := parts[1]
	switch code {
	case "353":
		if len(parts) < 5 {
			return
		}
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
