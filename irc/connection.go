package irc

import (
	"errors"
	"fmt"
	"log"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RealDtx/maxwell-irc/db"
	"github.com/RealDtx/maxwell-irc/internal/debug"
)

// detectDownloadChanRe matches IRC channel names ending in "-chat" (e.g. #example-chat).
var detectDownloadChanRe = regexp.MustCompile(`(?i)(#[\w-]+-chat)\b`)

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
	mu                        sync.RWMutex
	server                    *db.Server
	realms                    []db.Realm
	bus                       *EventBus
	client                    IRCClient
	status                    ConnectionStatus
	stopCh                    chan struct{}
	stopOnce                  sync.Once
	connectedAt               *time.Time
	reconnectCount            int
	lagMs                     int64
	namesMu                   sync.Mutex
	namesPending              map[string]chan []string // channel name -> result chan
	pingDone                  chan struct{}
	running                   bool          // a connectLoop goroutine is active
	identWait                 chan struct{} // closed on NickServ identification; nil when nobody waits
	identSeen                 bool          // an identification signal arrived during this connection
	onDownloadChannelDetected func(channel, detected string)
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
	addr := net.JoinHostPort(srv.Host, strconv.Itoa(srv.Port))
	nick := srv.Nickname
	if nick == "" {
		nick = "mxirc"
	}
	cfg := RawClientConfig{
		Addr:     addr,
		Nick:     nick,
		User:     nick,
		Realname: "maXwell IRC XDCC client",
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

// ErrConnectionStopped: Disconnect closed stopCh for good; the Manager
// replaces the Connection to connect again.
var ErrConnectionStopped = errors.New("connection stopped")

// Connect starts the connect loop. It is a no-op while a loop is already
// running (connecting, backing off or connected), so a second click can't
// spawn a second IRC client.
func (c *Connection) Connect() error {
	select {
	case <-c.stopCh:
		return ErrConnectionStopped
	default:
	}
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = true
	c.mu.Unlock()
	c.setStatus(StatusConnecting)
	go func() {
		c.connectLoop()
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
	}()
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

		// With NickServ auth, IDENTIFY went out on 001; joining right away would
		// hit +R channels (477) before services answer, so hold the joins.
		c.mu.Lock()
		c.identSeen = false
		c.identWait = nil
		var wait chan struct{}
		if c.server.AuthMethod == "nickserv" && c.server.AuthPassword != "" {
			wait = make(chan struct{})
			c.identWait = wait
		}
		c.mu.Unlock()
		if wait == nil {
			c.autoJoin(client)
			return
		}
		go func() {
			select {
			case <-wait:
			// ponytail: fixed 10s fallback for services whose wording isIdentifiedSignal misses
			case <-time.After(10 * time.Second):
				c.mu.Lock()
				if c.identWait == wait {
					c.identWait = nil
				}
				c.mu.Unlock()
			case <-done:
				return
			case <-c.stopCh:
				return
			}
			c.autoJoin(client)
		}()
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

		// Detect CTCP: messages wrapped in \x01...\x01 (e.g. DCC SEND)
		msgType := "privmsg"
		msgContent := message
		if len(message) >= 2 && message[0] == '\x01' && message[len(message)-1] == '\x01' {
			msgType = "ctcp"
			msgContent = message[1 : len(message)-1]
		}

		c.bus.Publish(Event{
			Type:      EventIRCMessage,
			ServerID:  c.server.ID,
			Channel:   channel,
			Nick:      nick,
			Timestamp: time.Now().Format(time.RFC3339Nano),
			Data: map[string]string{
				"type":    msgType,
				"message": msgContent,
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
		// Private notices (NickServ, bots) also land in the server stream,
		// where the command that triggered them was typed. Server notices
		// (prefix has a dot, nicks never do) already arrive there via handleRawLine.
		if channel != target && !strings.Contains(nick, ".") {
			c.bus.Publish(Event{
				Type:      EventIRCMessage,
				ServerID:  c.server.ID,
				Nick:      nick,
				Timestamp: time.Now().Format(time.RFC3339Nano),
				Data: map[string]string{
					"type":    "notice",
					"message": message,
				},
			})
		}
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
		c.mu.RLock()
		srv := *c.server // RegisterNick may update auth fields concurrently
		c.mu.RUnlock()
		client := newIRCClient(&srv)
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

// RegisterNick sends NickServ REGISTER and adopts the password for future
// connects. Unlike SendMessage it does not echo into the bus: the line holds
// the password and would otherwise land in the message buffer and log files.
func (c *Connection) RegisterNick(password, email string) error {
	c.mu.Lock()
	client := c.client
	if client == nil || c.status != StatusConnected {
		c.mu.Unlock()
		return fmt.Errorf("not connected to server %d", c.server.ID)
	}
	c.server.AuthPassword = password
	if c.server.AuthMethod == "" || c.server.AuthMethod == "none" {
		c.server.AuthMethod = "nickserv"
	}
	c.mu.Unlock()
	client.Privmsg("NickServ", "REGISTER "+password+" "+email)
	return nil
}

func (c *Connection) SendMessage(target, message string) error {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		log.Printf("WARNING: SendMessage called but client is nil for server %d target=%s", c.server.ID, target)
		return fmt.Errorf("not connected to server %d", c.server.ID)
	}
	if HasLineBreak(target, message) {
		return fmt.Errorf("target/message must not contain line breaks")
	}
	debug.Debugf("irc: SendMessage target=%s msg=%.50q", target, message)
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
	return nil
}

// SendRaw sends a line typed by the user ("/" already stripped) and echoes
// it — secrets masked — into the server buffer so request and reply appear
// together in the server stream and its log.
func (c *Connection) SendRaw(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" || HasLineBreak(raw) {
		return fmt.Errorf("command must be a single non-empty line")
	}
	c.mu.RLock()
	client, status := c.client, c.status
	c.mu.RUnlock()
	if client == nil || status != StatusConnected {
		return fmt.Errorf("not connected to server %d", c.server.ID)
	}
	line := expandClientAlias(raw)
	client.SendLine(line)
	c.bus.Publish(Event{
		Type:      EventIRCMessage,
		ServerID:  c.server.ID,
		Nick:      client.Nick(),
		Timestamp: time.Now().Format(time.RFC3339Nano),
		Data: map[string]string{
			"type":    "sent",
			"message": "-> " + maskSecrets(line),
		},
	})
	return nil
}

// expandClientAlias turns client-side "/msg target text" (and /query) into
// the PRIVMSG the server understands; everything else goes out verbatim.
func expandClientAlias(raw string) string {
	f := strings.SplitN(raw, " ", 3)
	if len(f) == 3 && (strings.EqualFold(f[0], "MSG") || strings.EqualFold(f[0], "QUERY")) {
		return "PRIVMSG " + f[1] + " :" + f[2]
	}
	return raw
}

// secretKeywords: everything after one of these words is a password.
// ponytail: keyword list, extend when a network uses another verb.
var secretKeywords = map[string]bool{
	"IDENTIFY": true, "REGISTER": true, "GHOST": true, "RECOVER": true, "RELEASE": true,
	"PASSWORD": true, "PASS": true, "OPER": true, "LOGIN": true, "AUTH": true, "AUTHENTICATE": true,
}

// maskSecrets replaces the arguments after the first secret keyword with
// "****" so echoed commands never put passwords in buffers or logs.
func maskSecrets(line string) string {
	words := strings.Split(line, " ")
	for i, w := range words {
		if secretKeywords[strings.ToUpper(strings.TrimPrefix(w, ":"))] && i < len(words)-1 {
			return strings.Join(append(words[:i+1:i+1], "****"), " ")
		}
	}
	return line
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
	return c.xdcc(botNick, fmt.Sprintf("xdcc send #%d", packNumber))
}

// RemovePack withdraws a queued request for packNumber from the bot's queue,
// so an abandoned request isn't served later in place of a newer one.
func (c *Connection) RemovePack(botNick string, packNumber int) error {
	return c.xdcc(botNick, fmt.Sprintf("xdcc remove %d", packNumber))
}

func (c *Connection) xdcc(botNick, msg string) error {
	c.mu.RLock()
	client := c.client
	status := c.status
	c.mu.RUnlock()

	if client == nil || status != StatusConnected {
		return fmt.Errorf("not connected to %s", c.server.Name)
	}

	if HasLineBreak(botNick) {
		return fmt.Errorf("invalid bot nick %q", botNick)
	}
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
			client.SendLine(fmt.Sprintf("PING :mxirc%d", sent))
		case <-c.stopCh:
			return
		case <-done:
			return
		}
	}
}

// autoJoin joins every enabled auto-join realm and its download channel.
// JOINing a channel we are already in is a no-op server-side, so a repeat is harmless.
func (c *Connection) autoJoin(client IRCClient) {
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
}

// onIdentified handles the first identification signal of a connection: it
// releases the waiting auto-join, or — if the wait already timed out, or the
// nick was only just registered — re-joins so channels refused with 477 open.
func (c *Connection) onIdentified() {
	c.mu.Lock()
	if c.identSeen || c.status != StatusConnected || c.server.AuthMethod == "sasl" || c.client == nil {
		c.mu.Unlock()
		return
	}
	c.identSeen = true
	wait, client := c.identWait, c.client
	c.identWait = nil
	c.mu.Unlock()
	if wait != nil {
		close(wait)
		return
	}
	c.autoJoin(client)
}

// isIdentifiedSignal reports whether a raw line says our nick is now
// identified with services: 900 RPL_LOGGEDIN, user mode +r on our nick, or a
// NickServ notice confirming it (Anope "Password accepted - you are now
// recognized", Atheme "You are now identified for ...").
func isIdentifiedSignal(line, ownNick string) bool {
	p := parseLine(line)
	if p == nil {
		return false
	}
	switch p.command {
	case "900":
		return true
	case "MODE":
		if len(p.params) == 0 || !strings.EqualFold(p.params[0], ownNick) {
			return false
		}
		modes := p.trailing
		if len(p.params) > 1 {
			modes = p.params[1]
		}
		adding := true
		for _, m := range modes {
			switch m {
			case '+':
				adding = true
			case '-':
				adding = false
			case 'r':
				if adding {
					return true
				}
			}
		}
	case "NOTICE":
		if !strings.EqualFold(p.prefix.nick, "NickServ") {
			return false
		}
		msg := strings.ToLower(p.trailing)
		if strings.Contains(msg, "not ") {
			return false
		}
		for _, w := range []string{"identified", "recognized", "accepted"} {
			if strings.Contains(msg, w) {
				return true
			}
		}
	}
	return false
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

	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()
	if client != nil && isIdentifiedSignal(line, client.Nick()) {
		c.onIdentified()
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

	// Auto-detect download channel from topic (332 RPL_TOPIC)
	// Only fires when the realm has no download_channel configured yet.
	if len(parts) >= 5 && strings.ToUpper(parts[1]) == "332" && c.onDownloadChannelDetected != nil {
		ch := strings.TrimPrefix(parts[3], ":")
		if c.realmNeedsDownloadChannel(ch) {
			topic := strings.TrimPrefix(parts[4], ":")
			if m := detectDownloadChanRe.FindStringSubmatch(topic); len(m) >= 2 {
				detected := strings.ToLower(m[1])
				if !strings.EqualFold(detected, ch) {
					c.onDownloadChannelDetected(ch, detected)
				}
			}
		}
	}
}

// realmNeedsDownloadChannel returns true if the realm for the given channel
// has no download_channel set yet.
func (c *Connection) realmNeedsDownloadChannel(channel string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, r := range c.realms {
		if strings.EqualFold(r.Name, channel) {
			return r.DownloadChannel == ""
		}
	}
	return false
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
	case "332", "333", "366", "471", "473", "474", "475", "477":
		// 47x: join refused (full, invite-only, banned, bad key, needs registered nick)
		// :server 332 nick #channel :topic
		if len(parts) >= 4 && isChannel(parts[3]) {
			return strings.TrimPrefix(parts[3], ":")
		}
	case "353":
		// :server 353 nick = #channel :nicks  — with SplitN(line," ",5):
		// parts[3]="=" and parts[4]="#channel :nick1 nick2"
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
