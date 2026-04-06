package irc

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/lrstanley/girc"
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
	mu       sync.RWMutex
	server   *db.Server
	channels []db.Channel
	bus      *EventBus
	client   *girc.Client
	status   ConnectionStatus
	stopCh   chan struct{}
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

	cfg := girc.Config{
		Server: c.server.Host,
		Port:   c.server.Port,
		Nick:   c.server.Nickname,
		User:   c.server.Nickname,
		Name:   "xirc XDCC client",
		SSL:    c.server.SSL,
	}

	if c.server.AuthMethod == "sasl" && c.server.AuthPassword != "" {
		cfg.SASL = &girc.SASLPlain{
			User: c.server.Nickname,
			Pass: c.server.AuthPassword,
		}
	}

	client := girc.New(cfg)

	client.Handlers.Add(girc.CONNECTED, func(cl *girc.Client, e girc.Event) {
		c.setStatus(StatusConnected)
		log.Printf("[%s] connected", c.server.Name)

		// NickServ auth after connect
		if c.server.AuthMethod == "nickserv" && c.server.AuthPassword != "" {
			cl.Cmd.Message("NickServ", "IDENTIFY "+c.server.AuthPassword)
			log.Printf("[%s] sent NickServ IDENTIFY", c.server.Name)
		}

		// Auto-join channels
		for _, ch := range c.channels {
			if !ch.Enabled || !ch.AutoJoin {
				continue
			}
			if ch.Key != "" {
				cl.Cmd.JoinKey(ch.Name, ch.Key)
			} else {
				cl.Cmd.Join(ch.Name)
			}
			// Also join download channel if different
			if ch.DownloadChannel != "" && ch.DownloadChannel != ch.Name {
				cl.Cmd.Join(ch.DownloadChannel)
			}
		}
	})

	client.Handlers.Add(girc.DISCONNECTED, func(cl *girc.Client, e girc.Event) {
		c.setStatus(StatusDisconnected)
		log.Printf("[%s] disconnected", c.server.Name)
	})

	// Route all PRIVMSG to event bus; also detect and route CTCP messages
	client.Handlers.Add(girc.PRIVMSG, func(cl *girc.Client, e girc.Event) {
		// Check if this is a CTCP message
		if ctcp := girc.DecodeCTCP(&e); ctcp != nil {
			nick := ""
			if ctcp.Source != nil {
				nick = ctcp.Source.Name
			}
			c.bus.Publish(Event{
				Type:     EventIRCMessage,
				ServerID: c.server.ID,
				Nick:     nick,
				Data: map[string]string{
					"type":    "ctcp",
					"command": ctcp.Command,
					"message": ctcp.Text,
				},
			})
			return
		}

		nick := ""
		if e.Source != nil {
			nick = e.Source.Name
		}
		c.bus.Publish(Event{
			Type:     EventIRCMessage,
			ServerID: c.server.ID,
			Channel:  e.Params[0],
			Nick:     nick,
			Data: map[string]string{
				"type":    "privmsg",
				"message": e.Last(),
			},
		})
	})

	client.Handlers.Add(girc.NOTICE, func(cl *girc.Client, e girc.Event) {
		target := ""
		if len(e.Params) > 0 {
			target = e.Params[0]
		}
		nick := ""
		if e.Source != nil {
			nick = e.Source.Name
		}
		c.bus.Publish(Event{
			Type:     EventIRCMessage,
			ServerID: c.server.ID,
			Channel:  target,
			Nick:     nick,
			Data: map[string]string{
				"type":    "notice",
				"message": e.Last(),
			},
		})
	})

	c.mu.Lock()
	c.client = client
	c.mu.Unlock()

	// Connect with reconnect loop in a goroutine
	go c.connectLoop()

	return nil
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
		err := c.client.Connect()
		if err == nil {
			return // Clean disconnect (e.g., via Disconnect())
		}

		select {
		case <-c.stopCh:
			return
		default:
		}

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
	}
}

func (c *Connection) Disconnect() {
	close(c.stopCh)

	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client != nil {
		client.Quit("xirc shutting down")
		client.Close()
	}
	c.setStatus(StatusDisconnected)
}

func (c *Connection) JoinChannel(name, key string) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return
	}
	if key != "" {
		client.Cmd.JoinKey(name, key)
	} else {
		client.Cmd.Join(name)
	}
}

func (c *Connection) PartChannel(name string) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client != nil {
		client.Cmd.Part(name)
	}
}

func (c *Connection) SendMessage(target, message string) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client != nil {
		client.Cmd.Message(target, message)
	}
}

func (c *Connection) SendRaw(raw string) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client != nil {
		client.Cmd.SendRaw(raw) //nolint:errcheck
	}
}

func (c *Connection) IsInChannel(name string) bool {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return false
	}

	channels := client.ChannelList()
	target := strings.ToLower(name)
	for _, ch := range channels {
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
	client.Cmd.Message(searchChannel, msg)
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

	if !c.IsInChannel(channel) {
		return fmt.Errorf("not in channel %s — join it first", channel)
	}

	msg := fmt.Sprintf("xdcc send #%d", packNumber)
	client.Cmd.Message(botNick, msg)
	return nil
}
