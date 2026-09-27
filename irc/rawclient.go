package irc

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

// RawClient is a minimal stdlib IRC client implementing IRCClient.
type RawClient struct {
	addr     string // host:port
	nick     string
	user     string
	realname string
	useTLS   bool

	// SASL credentials — set to enable SASL PLAIN authentication.
	saslUser string
	saslPass string

	// NickServ password — set to enable NickServ IDENTIFY after connect.
	nickServPass string

	onMessage    func(nick, target, msg string)
	onNotice     func(nick, target, msg string)
	onConnect    func()
	onDisconnect func()
	onRaw        func(line string)

	mu        sync.Mutex
	conn      net.Conn
	done      chan struct{}
	closeOnce sync.Once
}

// RawClientConfig holds connection parameters.
type RawClientConfig struct {
	Addr         string // host:port
	Nick         string
	User         string
	Realname     string
	UseTLS       bool
	SASLUser     string // leave empty to skip SASL
	SASLPass     string
	NickServPass string // leave empty to skip NickServ
}

// NewRawClient creates a new RawClient.
func NewRawClient(cfg RawClientConfig) *RawClient {
	return &RawClient{
		addr:         cfg.Addr,
		nick:         cfg.Nick,
		user:         cfg.User,
		realname:     cfg.Realname,
		useTLS:       cfg.UseTLS,
		saslUser:     cfg.SASLUser,
		saslPass:     cfg.SASLPass,
		nickServPass: cfg.NickServPass,
		done:         make(chan struct{}),
	}
}

func (c *RawClient) OnMessage(fn func(string, string, string)) { c.onMessage = fn }
func (c *RawClient) OnNotice(fn func(string, string, string))  { c.onNotice = fn }
func (c *RawClient) OnConnect(fn func())                       { c.onConnect = fn }
func (c *RawClient) OnDisconnect(fn func())                    { c.onDisconnect = fn }
func (c *RawClient) OnRaw(fn func(string))                     { c.onRaw = fn }
func (c *RawClient) Nick() string                              { return c.nick }

func (c *RawClient) dial() (net.Conn, error) {
	if c.useTLS {
		host, _, _ := net.SplitHostPort(c.addr)
		dialer := &net.Dialer{Timeout: 30 * time.Second}
		return tls.DialWithDialer(dialer, "tcp", c.addr, &tls.Config{
			ServerName:         host,
			InsecureSkipVerify: true, //nolint:gosec // IRC servers commonly use self-signed certs
		})
	}
	return net.DialTimeout("tcp", c.addr, 30*time.Second)
}

func (c *RawClient) send(conn net.Conn, format string, args ...interface{}) {
	writeLine(conn, fmt.Sprintf(format, args...))
}

// writeLine is the single exit to the wire. A line carrying CR/LF/NUL (e.g.
// from a user-supplied target or message) is dropped, never split into
// extra IRC commands.
func writeLine(conn net.Conn, line string) {
	if HasLineBreak(line) {
		log.Printf("irc send refused: line contains CR/LF/NUL: %.80q", line)
		return
	}
	if _, err := fmt.Fprintf(conn, "%s\r\n", line); err != nil {
		log.Printf("irc send error: %v", err)
	}
}

// Connect connects and blocks until disconnected.
// SASL PLAIN is used when SASLUser/SASLPass are set; otherwise plain registration.
// NickServ IDENTIFY is sent after successful registration when NickServPass is set.
func (c *RawClient) Connect() error {
	conn, err := c.dial()
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	defer func() {
		conn.Close()
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
		if c.onDisconnect != nil {
			c.onDisconnect()
		}
	}()

	useSASL := c.saslUser != "" && c.saslPass != ""

	if useSASL {
		c.send(conn, "CAP REQ :sasl")
	}
	c.send(conn, "NICK %s", c.nick)
	c.send(conn, "USER %s 0 * :%s", c.user, c.realname)

	registered := false
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Text()
		if c.onRaw != nil {
			c.onRaw(line)
		}

		parsed := parseLine(line)
		if parsed == nil {
			continue
		}

		switch parsed.command {
		case "PING":
			token := parsed.trailing
			if token == "" && len(parsed.params) > 0 {
				token = parsed.params[0]
			}
			c.send(conn, "PONG :%s", token)

		case "CAP":
			// CAP * ACK :sasl — server confirmed SASL capability
			if useSASL && strings.Contains(parsed.trailing, "sasl") {
				c.send(conn, "AUTHENTICATE PLAIN")
			}

		case "AUTHENTICATE":
			// Server is ready for our AUTHENTICATE payload
			if useSASL && parsed.trailing == "+" {
				payload := base64.StdEncoding.EncodeToString(
					[]byte(fmt.Sprintf("%s\x00%s\x00%s", c.saslUser, c.saslUser, c.saslPass)))
				c.send(conn, "AUTHENTICATE %s", payload)
			}

		case "903": // RPL_SASLSUCCESS
			c.send(conn, "CAP END")

		case "904", "905": // ERR_SASLFAIL / ERR_SASLTOOLONG
			return fmt.Errorf("SASL authentication failed")

		case "001": // RPL_WELCOME — registration complete
			if !registered {
				registered = true
				if c.nickServPass != "" {
					c.send(conn, "PRIVMSG NickServ :IDENTIFY %s", c.nickServPass)
				}
				if c.onConnect != nil {
					c.onConnect()
				}
			}

		case "PRIVMSG":
			if len(parsed.params) < 1 {
				continue
			}
			target := parsed.params[0]
			if c.onMessage != nil {
				c.onMessage(parsed.prefix.nick, target, parsed.trailing)
			}

		case "NOTICE":
			if len(parsed.params) < 1 {
				continue
			}
			target := parsed.params[0]
			if c.onNotice != nil {
				c.onNotice(parsed.prefix.nick, target, parsed.trailing)
			}

		case "433": // ERR_NICKNAMEINUSE
			c.nick = c.nick + "_"
			c.send(conn, "NICK %s", c.nick)

		case "ERROR":
			return fmt.Errorf("server error: %s", parsed.trailing)
		}

		select {
		case <-c.done:
			return nil
		default:
		}
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		return err
	}
	return nil
}

func (c *RawClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		fmt.Fprintf(c.conn, "QUIT :bye\r\n") //nolint:errcheck
		c.conn.Close()
	}
	c.closeOnce.Do(func() { close(c.done) })
}

func (c *RawClient) Join(channel, key string) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return
	}
	if key != "" {
		c.send(conn, "JOIN %s %s", channel, key)
	} else {
		c.send(conn, "JOIN %s", channel)
	}
}

func (c *RawClient) Part(channel string) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return
	}
	c.send(conn, "PART %s", channel)
}

func (c *RawClient) Privmsg(target, message string) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return
	}
	c.send(conn, "PRIVMSG %s :%s", target, message)
}

func (c *RawClient) SendLine(line string) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return
	}
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	writeLine(conn, line)
	conn.SetWriteDeadline(time.Time{}) // clear deadline
}

// ircPrefix holds parsed prefix info.
type ircPrefix struct {
	nick string
	user string
	host string
}

// ircLine is a parsed IRC message.
type ircLine struct {
	prefix   ircPrefix
	command  string
	params   []string
	trailing string
}

// parseLine parses a raw IRC line per RFC 1459.
func parseLine(line string) *ircLine {
	if line == "" {
		return nil
	}
	result := &ircLine{}

	// Tags (IRCv3) — skip them
	if strings.HasPrefix(line, "@") {
		idx := strings.Index(line, " ")
		if idx < 0 {
			return nil
		}
		line = line[idx+1:]
	}

	// Prefix
	if strings.HasPrefix(line, ":") {
		idx := strings.Index(line, " ")
		if idx < 0 {
			return nil
		}
		prefixStr := line[1:idx]
		line = line[idx+1:]
		if i := strings.Index(prefixStr, "!"); i >= 0 {
			result.prefix.nick = prefixStr[:i]
			rest := prefixStr[i+1:]
			if j := strings.Index(rest, "@"); j >= 0 {
				result.prefix.user = rest[:j]
				result.prefix.host = rest[j+1:]
			}
		} else {
			result.prefix.nick = prefixStr
		}
	}

	// Trailing
	if idx := strings.Index(line, " :"); idx >= 0 {
		result.trailing = line[idx+2:]
		line = line[:idx]
	}

	// Command + params
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return nil
	}
	result.command = strings.ToUpper(parts[0])
	if len(parts) > 1 {
		result.params = parts[1:]
	}
	return result
}

// HasLineBreak reports whether any s contains CR, LF or NUL — characters that
// would split or truncate an IRC protocol line.
func HasLineBreak(s ...string) bool {
	for _, v := range s {
		if strings.ContainsAny(v, "\r\n\x00") {
			return true
		}
	}
	return false
}
