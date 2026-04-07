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
	mu        sync.RWMutex
	server    *db.Server
	channels  []db.Channel
	bus       *EventBus
	client    IRCClient
	status    ConnectionStatus
	stopCh    chan struct{}
	stopOnce  sync.Once
}

// newIRCClient creates the appropriate IRCClient for the given server config.
func newIRCClient(srv *db.Server) IRCClient {
	addr := fmt.Sprintf("%s:%d", srv.Host, srv.Port)
	cfg := RawClientConfig{
		Addr:     addr,
		Nick:     srv.Nickname,
		User:     srv.Nickname,
		Realname: "xirc XDCC client",
		UseTLS:   srv.SSL,
	}
	switch srv.AuthMethod {
	case "sasl":
		cfg.SASLUser = srv.Nickname
		cfg.SASLPass = srv.AuthPassword
	case "nickserv":
		cfg.NickServPass = srv.AuthPassword
	}
	return NewRawClient(cfg)
}

func NewConnection(server *db.Server, channels []db.Channel, bus *EventBus) *Connection {
	return &Connection{
		server:   server,
		channels: channels,
		bus:      bus,
		status:   StatusDisconnected,
		stopCh:   make(chan struct{}),
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
	c.mu.Unlock()

	c.bus.Publish(Event{
		Type:     EventConnectionStatus,
		ServerID: c.server.ID,
		Data:     string(s),
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
		c.setStatus(StatusConnected)
		log.Printf("[%s] connected", c.server.Name)

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
