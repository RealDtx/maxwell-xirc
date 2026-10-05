package irc

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RealDtx/maxwell-irc/db"
)

type testIRCClient struct {
	onMessage    func(nick, target, message string)
	onNotice     func(nick, target, message string)
	onConnect    func()
	onDisconnect func()
	onRaw        func(line string)
}

func (c *testIRCClient) Connect() error                                  { return nil }
func (c *testIRCClient) Close()                                          {}
func (c *testIRCClient) Join(channel, key string)                        {}
func (c *testIRCClient) Part(channel string)                             {}
func (c *testIRCClient) Privmsg(target, message string)                  {}
func (c *testIRCClient) Nick() string                                    { return "testbot" }
func (c *testIRCClient) OnMessage(fn func(nick, target, message string)) { c.onMessage = fn }
func (c *testIRCClient) OnNotice(fn func(nick, target, message string))  { c.onNotice = fn }
func (c *testIRCClient) OnConnect(fn func())                             { c.onConnect = fn }
func (c *testIRCClient) OnDisconnect(fn func())                          { c.onDisconnect = fn }
func (c *testIRCClient) OnRaw(fn func(line string))                      { c.onRaw = fn }
func (c *testIRCClient) SendLine(line string)                            {}

func TestNewConnection_SetsFields(t *testing.T) {
	bus := NewEventBus()
	srv := &db.Server{
		ID:           1,
		Name:         "test",
		Host:         "irc.example.com",
		Port:         6667,
		SSL:          false,
		Nickname:     "testbot",
		AltNicknames: []string{"testbot_"},
		AuthMethod:   "nickserv",
		AuthPassword: "secret",
		Enabled:      true,
	}
	realms := []db.Realm{
		{ID: 1, ServerID: 1, Name: "#search", SearchCommand: "!s", DownloadChannel: "#downloads", AutoJoin: true, Enabled: true},
		{ID: 2, ServerID: 1, Name: "#downloads", AutoJoin: true, Enabled: true},
	}

	conn := NewConnection(srv, realms, bus)

	if conn.ServerID() != int64(1) {
		t.Errorf("expected server ID 1, got %d", conn.ServerID())
	}
	if conn.Status() != StatusDisconnected {
		t.Errorf("expected status disconnected, got %s", conn.Status())
	}
}

func TestConnection_ChannelPairs(t *testing.T) {
	bus := NewEventBus()
	srv := &db.Server{ID: 1, Name: "test", Host: "irc.example.com", Port: 6667, Nickname: "testbot", Enabled: true}
	realms := []db.Realm{
		{ID: 1, ServerID: 1, Name: "#search", SearchCommand: "!s", DownloadChannel: "#downloads", AutoJoin: true, Enabled: true},
		{ID: 2, ServerID: 1, Name: "#simple", SearchCommand: "!s", AutoJoin: true, Enabled: true},
	}

	conn := NewConnection(srv, realms, bus)

	pairs := conn.ChannelPairs()
	if len(pairs) != 2 {
		t.Fatalf("expected 2 channel pairs, got %d", len(pairs))
	}

	// #search → #downloads
	if pairs[0].SearchChannel != "#search" {
		t.Errorf("expected search channel #search, got %s", pairs[0].SearchChannel)
	}
	if pairs[0].DownloadChannel != "#downloads" {
		t.Errorf("expected download channel #downloads, got %s", pairs[0].DownloadChannel)
	}

	// #simple → #simple (same channel)
	if pairs[1].SearchChannel != "#simple" {
		t.Errorf("expected search channel #simple, got %s", pairs[1].SearchChannel)
	}
	if pairs[1].DownloadChannel != "#simple" {
		t.Errorf("expected download channel #simple (same), got %s", pairs[1].DownloadChannel)
	}
}

func TestConnection_AllChannelNames(t *testing.T) {
	bus := NewEventBus()
	srv := &db.Server{ID: 1, Name: "test", Host: "irc.example.com", Port: 6667, Nickname: "testbot", Enabled: true}
	realms := []db.Realm{
		{ID: 1, ServerID: 1, Name: "#search", DownloadChannel: "#downloads", AutoJoin: true, Enabled: true},
	}

	conn := NewConnection(srv, realms, bus)

	names := conn.AllChannelNames()
	if len(names) != 2 {
		t.Fatalf("expected 2 unique channel names, got %d: %v", len(names), names)
	}

	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["#search"] || !found["#downloads"] {
		t.Errorf("expected #search and #downloads, got %v", names)
	}
}

func TestConnectionStatus_StringValues(t *testing.T) {
	if StatusDisconnected != "disconnected" {
		t.Errorf("unexpected value: %s", StatusDisconnected)
	}
	if StatusConnecting != "connecting" {
		t.Errorf("unexpected value: %s", StatusConnecting)
	}
	if StatusConnected != "connected" {
		t.Errorf("unexpected value: %s", StatusConnected)
	}
}

// --- newIRCClient nickname fallback tests ---

func TestNewIRCClient_UsesNicknameWhenSet(t *testing.T) {
	srv := &db.Server{
		ID:       1,
		Name:     "test",
		Host:     "irc.example.com",
		Port:     6667,
		Nickname: "mynick",
		Enabled:  true,
	}
	client := newIRCClient(srv)
	if client.Nick() != "mynick" {
		t.Errorf("expected nick %q, got %q", "mynick", client.Nick())
	}
}

func TestNewIRCClient_FallsBackToXircWhenEmpty(t *testing.T) {
	srv := &db.Server{
		ID:       2,
		Name:     "test",
		Host:     "irc.example.com",
		Port:     6667,
		Nickname: "",
		Enabled:  true,
	}
	client := newIRCClient(srv)
	if client.Nick() != "mxirc" {
		t.Errorf("expected fallback nick %q, got %q", "mxirc", client.Nick())
	}
}

// --- handleRawLine event bus publishing tests ---

func TestHandleRawLine_PublishesNonPongLine(t *testing.T) {
	bus := NewEventBus()
	srv := &db.Server{ID: 42, Name: "test", Host: "irc.example.com", Port: 6667, Nickname: "bot", Enabled: true}
	conn := NewConnection(srv, []db.Realm{}, bus)

	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	conn.handleRawLine(":server.example.com 001 bot :Welcome to IRC")

	select {
	case ev := <-ch:
		if ev.Type != EventIRCMessage {
			t.Errorf("expected event type %s, got %s", EventIRCMessage, ev.Type)
		}
		if ev.Channel != "" {
			t.Errorf("expected empty channel for server-level message, got %q", ev.Channel)
		}
		if ev.ServerID != 42 {
			t.Errorf("expected server_id 42, got %d", ev.ServerID)
		}
		data, ok := ev.Data.(map[string]string)
		if !ok {
			t.Fatalf("expected Data to be map[string]string, got %T", ev.Data)
		}
		if data["type"] != "raw" {
			t.Errorf("expected data[type]=%q, got %q", "raw", data["type"])
		}
		if data["message"] != ":server.example.com 001 bot :Welcome to IRC" {
			t.Errorf("expected raw line in data[message], got %q", data["message"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for raw line event")
	}
}

func TestHandleRawLine_DoesNotPublishPongLine(t *testing.T) {
	bus := NewEventBus()
	srv := &db.Server{ID: 1, Name: "test", Host: "irc.example.com", Port: 6667, Nickname: "bot", Enabled: true}
	conn := NewConnection(srv, []db.Realm{}, bus)

	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	conn.handleRawLine("PONG :token123")

	select {
	case ev := <-ch:
		t.Errorf("expected no event for PONG line, got %+v", ev)
	case <-time.After(100 * time.Millisecond):
		// correct: nothing published
	}
}

func TestHandleRawLine_DoesNotPublishServerPrefixedPongLine(t *testing.T) {
	bus := NewEventBus()
	srv := &db.Server{ID: 1, Name: "test", Host: "irc.example.com", Port: 6667, Nickname: "bot", Enabled: true}
	conn := NewConnection(srv, []db.Realm{}, bus)

	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	conn.handleRawLine(":irc.server.net PONG irc.server.net :token123")

	select {
	case ev := <-ch:
		t.Errorf("expected no event for server-prefixed PONG line, got %+v", ev)
	case <-time.After(100 * time.Millisecond):
		// correct: nothing published
	}
}

func TestHandleRawLine_DoesNotPublishUserPrivmsgRawLine(t *testing.T) {
	bus := NewEventBus()
	srv := &db.Server{ID: 1, Name: "test", Host: "irc.example.com", Port: 6667, Nickname: "bot", Enabled: true}
	conn := NewConnection(srv, []db.Realm{}, bus)

	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	conn.handleRawLine(":alice!u@h PRIVMSG #room :hello")

	select {
	case ev := <-ch:
		t.Errorf("expected no raw publish for user PRIVMSG line, got %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestHandleRawLine_DoesNotPublishUserNoticeRawLine(t *testing.T) {
	bus := NewEventBus()
	srv := &db.Server{ID: 1, Name: "test", Host: "irc.example.com", Port: 6667, Nickname: "bot", Enabled: true}
	conn := NewConnection(srv, []db.Realm{}, bus)

	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	conn.handleRawLine(":alice!u@h NOTICE #room :notice")

	select {
	case ev := <-ch:
		t.Errorf("expected no raw publish for user NOTICE line, got %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestApplyHandlers_OnMessageRoutesDMToSenderNick(t *testing.T) {
	bus := NewEventBus()
	srv := &db.Server{ID: 1, Name: "test", Host: "irc.example.com", Port: 6667, Nickname: "testbot", Enabled: true}
	conn := NewConnection(srv, []db.Realm{}, bus)
	client := &testIRCClient{}
	conn.applyHandlers(client)

	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	client.onMessage("alice", "testbot", "hello")

	select {
	case ev := <-ch:
		if ev.Channel != "alice" {
			t.Fatalf("expected DM channel to be sender nick, got %q", ev.Channel)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for OnMessage event")
	}
}

func TestApplyHandlers_OnNoticeRoutesDMToSenderNick(t *testing.T) {
	bus := NewEventBus()
	srv := &db.Server{ID: 1, Name: "test", Host: "irc.example.com", Port: 6667, Nickname: "testbot", Enabled: true}
	conn := NewConnection(srv, []db.Realm{}, bus)
	client := &testIRCClient{}
	conn.applyHandlers(client)

	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	client.onNotice("alice", "testbot", "hello")

	select {
	case ev := <-ch:
		if ev.Channel != "alice" {
			t.Fatalf("expected DM notice channel to be sender nick, got %q", ev.Channel)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for OnNotice event")
	}
}

// TestSendMessage_NilClient verifies that SendMessage returns an error (and does
// not panic) when the underlying IRC client has not been set.
func TestSendMessage_NilClient(t *testing.T) {
	bus := NewEventBus()
	srv := &db.Server{
		ID:       7,
		Name:     "no-client-server",
		Host:     "irc.example.com",
		Port:     6667,
		Nickname: "testbot",
		Enabled:  true,
	}
	conn := NewConnection(srv, []db.Realm{}, bus)
	// client is nil by default (NewConnection does not set it)

	err := conn.SendMessage("#channel", "hello")
	if err == nil {
		t.Fatal("expected error when client is nil, got nil")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "not connected") {
		t.Errorf("expected error to contain %q, got %q", "not connected", errMsg)
	}
	// Verify the server ID is included in the error message for debuggability.
	if !strings.Contains(errMsg, "7") {
		t.Errorf("expected error to contain server ID 7, got %q", errMsg)
	}
}

func TestConnection_UpdateRealms_ThreadSafe(t *testing.T) {
	bus := NewEventBus()
	srv := &db.Server{ID: 1, Name: "test", Host: "irc.example.com", Port: 6667, Nickname: "testbot", Enabled: true}
	conn := NewConnection(srv, []db.Realm{
		{ID: 1, ServerID: 1, Name: "#search", AutoJoin: true, Enabled: true},
	}, bus)

	newRealms := []db.Realm{
		{ID: 2, ServerID: 1, Name: "#downloads", AutoJoin: true, Enabled: true},
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = conn.AllChannelNames()
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			conn.UpdateRealms(newRealms)
		}
	}()

	wg.Wait()

	if len(conn.AllChannelNames()) == 0 {
		t.Fatal("expected channel names after concurrent update/read")
	}
}

func TestExtractChannelFromRaw_JoinErrors(t *testing.T) {
	for _, num := range []string{"471", "473", "474", "475", "477"} {
		line := ":irc.example.net " + num + " me #locked :Cannot join channel"
		if got := extractChannelFromRaw(strings.SplitN(line, " ", 5)); got != "#locked" {
			t.Errorf("%s: got %q, want #locked", num, got)
		}
	}
}

func TestIsIdentifiedSignal(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{":services.example.net 900 me me!u@h me :You are now logged in as me", true},
		{":me MODE me :+r", true},
		{":me!u@h MODE me +ir", true},
		{":me MODE me :-r", false},
		{":me MODE me :+i-r", false},
		{":op!u@h MODE #chan +r", false},
		{":NickServ!service@example.net NOTICE me :Password accepted - you are now recognized.", true},
		{":NickServ!NickServ@services. NOTICE me :You are now identified for \x02me\x02.", true},
		{":NickServ!service@example.net NOTICE me :You are not identified.", false},
		{":NickServ!service@example.net NOTICE me :This nickname is registered. Please IDENTIFY.", false},
		{":someone!u@h NOTICE me :you are now identified, trust me", false},
	}
	for _, c := range cases {
		if got := isIdentifiedSignal(c.line, "me"); got != c.want {
			t.Errorf("%q: got %v, want %v", c.line, got, c.want)
		}
	}
}

type privmsgRecorder struct {
	IRCClient // only Privmsg is used
	sent      []string
}

func (p *privmsgRecorder) Privmsg(target, msg string) { p.sent = append(p.sent, target+" "+msg) }

func TestRegisterNick_SendsQuietlyAndAdoptsAuth(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)
	c := NewConnection(&db.Server{ID: 1, AuthMethod: "none"}, nil, bus)

	if err := c.RegisterNick("pw", "a@example.com"); err == nil {
		t.Fatal("expected error while disconnected")
	}

	rec := &privmsgRecorder{}
	c.client, c.status = rec, StatusConnected
	if err := c.RegisterNick("pw", "a@example.com"); err != nil {
		t.Fatal(err)
	}
	if len(rec.sent) != 1 || rec.sent[0] != "NickServ REGISTER pw a@example.com" {
		t.Errorf("sent %q", rec.sent)
	}
	if c.server.AuthMethod != "nickserv" || c.server.AuthPassword != "pw" {
		t.Errorf("auth not adopted: %q/%q", c.server.AuthMethod, c.server.AuthPassword)
	}
	select {
	case ev := <-ch:
		t.Errorf("REGISTER must not be echoed to the bus, got %+v", ev)
	default:
	}
}

type lineRecorder struct {
	IRCClient
	lines []string
}

func (l *lineRecorder) SendLine(s string) { l.lines = append(l.lines, s) }
func (l *lineRecorder) Nick() string     { return "me" }

func TestSendRaw_SendsEchoesAndMasks(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)
	c := NewConnection(&db.Server{ID: 1}, nil, bus)
	if err := c.SendRaw("NICKSERV IDENTIFY pw"); err == nil {
		t.Fatal("expected error while disconnected")
	}
	rec := &lineRecorder{}
	c.client, c.status = rec, StatusConnected

	cases := []struct{ in, sent, echo string }{
		{"NICKSERV IDENTIFY s3cret", "NICKSERV IDENTIFY s3cret", "NICKSERV IDENTIFY ****"},
		{"msg NickServ identify me s3cret", "PRIVMSG NickServ :identify me s3cret", "PRIVMSG NickServ :identify ****"},
		{"PRIVMSG NickServ :REGISTER pw a@b.c", "PRIVMSG NickServ :REGISTER pw a@b.c", "PRIVMSG NickServ :REGISTER ****"},
		{"ns set password new1", "ns set password new1", "ns set password ****"},
		{"WHOIS someone", "WHOIS someone", "WHOIS someone"},
	}
	for _, tc := range cases {
		rec.lines = nil
		if err := c.SendRaw(tc.in); err != nil {
			t.Fatal(err)
		}
		if len(rec.lines) != 1 || rec.lines[0] != tc.sent {
			t.Errorf("%q: sent %q, want %q", tc.in, rec.lines, tc.sent)
		}
		ev := <-ch
		if ev.Channel != "" || ev.Data.(map[string]string)["message"] != "-> "+tc.echo {
			t.Errorf("%q: echo %+v, want server buffer %q", tc.in, ev, "-> "+tc.echo)
		}
	}
	if err := c.SendRaw("JOIN #a\r\nQUIT"); err == nil {
		t.Error("line breaks must be refused")
	}
}
