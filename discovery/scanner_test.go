package discovery

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RealDtx/maxwell-irc/config"
	"github.com/RealDtx/maxwell-irc/db"
	"github.com/RealDtx/maxwell-irc/irc"
)

func TestMain(m *testing.M) {
	joinInterval = 2 * time.Millisecond
	entryGrace = 5 * time.Millisecond
	listTimeout = 500 * time.Millisecond
	connectDelay = 10 * time.Millisecond
	noticeWindow = time.Second
	stayUnit = time.Millisecond
	os.Exit(m.Run())
}

type fakeConn struct {
	mu        sync.Mutex
	bus       *irc.EventBus
	sid       int64
	raws      []string
	joins     []string
	parts     []string
	prejoined map[string]bool
	held      map[string]bool
	reject    map[string]bool // reply 405 once
	autoNames bool            // reply 366 to every join
	saw405    bool
	maxAfter  int // max held after the first 405
}

func (f *fakeConn) SendRaw(raw string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.raws = append(f.raws, raw)
	return nil
}

func (f *fakeConn) JoinChannel(name, key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.joins = append(f.joins, name)
	if f.reject[name] {
		delete(f.reject, name)
		f.saw405 = true
		raw(f.bus, f.sid, ":irc.example.org 405 me "+name+" :You have joined too many channels")
		return
	}
	f.held[name] = true
	if f.saw405 && len(f.held) > f.maxAfter {
		f.maxAfter = len(f.held)
	}
	if f.autoNames {
		raw(f.bus, f.sid, ":irc.example.org 366 me "+name+" :End of /NAMES list.")
	}
}

func (f *fakeConn) PartChannel(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.parts = append(f.parts, name)
	delete(f.held, name)
}

func (f *fakeConn) IsInChannel(name string) bool { return f.prejoined[strings.ToLower(name)] }

func (f *fakeConn) snapshot() (raws, joins, parts []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.raws...), append([]string{}, f.joins...), append([]string{}, f.parts...)
}

func raw(bus *irc.EventBus, sid int64, line string) {
	bus.Publish(irc.Event{Type: irc.EventIRCMessage, ServerID: sid, Data: map[string]string{"type": "raw", "message": line}})
}

func msg(bus *irc.EventBus, sid int64, typ, channel, nick, text string) {
	bus.Publish(irc.Event{Type: irc.EventIRCMessage, ServerID: sid, Channel: channel, Nick: nick, Data: map[string]string{"type": typ, "message": text}})
}

type env struct {
	t     *testing.T
	store db.Store
	bus   *irc.EventBus
	fc    *fakeConn
	sc    *Scanner
	sid   int64
}

func newEnv(t *testing.T, mode string, cfg config.DiscoveryConfig) *env {
	t.Helper()
	dir := t.TempDir()
	store, err := db.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	store.Migrate()
	srv := &db.Server{Name: "Example", Host: "irc.example.org", Port: 6667, Nickname: "me", Enabled: true, DiscoveryMode: mode}
	if err := store.CreateServer(srv); err != nil {
		t.Fatal(err)
	}
	bus := irc.NewEventBus()
	fc := &fakeConn{bus: bus, sid: srv.ID, prejoined: map[string]bool{}, held: map[string]bool{}, reject: map[string]bool{}, autoNames: true}
	sc := New(store, bus, func(int64) Conn { return fc }, cfg)
	sc.Start()
	t.Cleanup(func() { sc.Stop(); store.Close() })
	return &env{t: t, store: store, bus: bus, fc: fc, sc: sc, sid: srv.ID}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func (e *env) state() string { return e.sc.Status(e.sid).State }

func (e *env) waitState(s string) {
	e.t.Helper()
	waitFor(e.t, "state "+s, func() bool { return e.state() == s })
}

// list starts a scan and feeds the given LIST entries plus 323.
func (e *env) list(entries ...string) {
	e.t.Helper()
	if err := e.sc.Scan(e.sid); err != nil {
		e.t.Fatal(err)
	}
	waitFor(e.t, "LIST sent", func() bool { r, _, _ := e.fc.snapshot(); return len(r) > 0 && r[len(r)-1] == "LIST" })
	for _, l := range entries {
		raw(e.bus, e.sid, ":irc.example.org 322 me "+l)
	}
	raw(e.bus, e.sid, ":irc.example.org 323 me :End of /LIST")
}

func (e *env) rows() map[string]db.DiscoveredChannel {
	rows, err := e.store.GetDiscoveredChannels(e.sid)
	if err != nil {
		e.t.Fatal(err)
	}
	m := map[string]db.DiscoveredChannel{}
	for _, r := range rows {
		m[r.Channel] = r
	}
	return m
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestScanner_NormalFindsKeywordChannelAndPartner(t *testing.T) {
	e := newEnv(t, "normal", config.DiscoveryConfig{})
	sub := e.bus.Subscribe()
	e.list("#example 42 :XDCC – chat in #example-chat", "#random 10 :hello")
	e.waitState("done")

	_, joins, parts := e.fc.snapshot()
	if len(joins) != 1 || joins[0] != "#example" || len(parts) != 1 || parts[0] != "#example" {
		t.Fatalf("joins=%v parts=%v", joins, parts)
	}
	r, ok := e.rows()["#example"]
	if !ok || len(e.rows()) != 1 {
		t.Fatalf("rows=%v", e.rows())
	}
	if r.Partner != "#example-chat" || r.Type != "chat" || r.Reason != "keyword: xdcc" || r.Users != 42 {
		t.Fatalf("row=%+v", r)
	}
	if st := e.sc.Status(e.sid); st.Found != 1 || st.Mode != "normal" || st.StartedAt == nil {
		t.Fatalf("status=%+v", st)
	}
	sawDone := false
	for len(sub) > 0 {
		ev := <-sub
		if st, ok := ev.Data.(ScanStatus); ok && ev.Type == irc.EventDiscovery && st.State == "done" && ev.ServerID == e.sid {
			sawDone = true
		}
	}
	if !sawDone {
		t.Fatal("no done EventDiscovery published")
	}
}

func TestScanner_PartnerFromJoinNotice(t *testing.T) {
	e := newEnv(t, "normal", config.DiscoveryConfig{})
	e.fc.autoNames = false
	e.list("#example 5 :fast dcc")
	waitFor(t, "join", func() bool { _, j, _ := e.fc.snapshot(); return len(j) == 1 })
	msg(e.bus, e.sid, "notice", "ChanServ", "ChanServ", "[#example] Please also join #example-chat")
	raw(e.bus, e.sid, ":irc.example.org 366 me #example :End of /NAMES list.")
	e.waitState("done")
	if r := e.rows()["#example"]; r.Partner != "#example-chat" || r.Reason != "keyword: dcc" {
		t.Fatalf("row=%+v", r)
	}
}

func TestScanner_AggressiveCountsAds(t *testing.T) {
	e := newEnv(t, "aggressive", config.DiscoveryConfig{AggressiveStaySeconds: 100})
	if err := e.store.CreateParsePattern(&db.ParsePattern{Name: "test", Regex: `^#(?<pack_number>\d+)\s+\d+x\s+\[(?<filesize>[^\]]+)\]\s+(?<filename>.+)$`, Priority: 1000, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	e.list("#a 3 :", "#b 2 :")
	waitFor(t, "two joins", func() bool { _, j, _ := e.fc.snapshot(); return len(j) == 2 })
	msg(e.bus, e.sid, "privmsg", "#a", "BotA", "#1 5x [700M] File.mkv")
	e.waitState("done")
	_, _, parts := e.fc.snapshot()
	if !contains(parts, "#a") || !contains(parts, "#b") {
		t.Fatalf("parts=%v", parts)
	}
	rows := e.rows()
	r, ok := rows["#a"]
	if !ok || len(rows) != 1 {
		t.Fatalf("rows=%v", rows)
	}
	if r.AdCount != 1 || r.Reason != "1 bot ads" || r.Type != "download" {
		t.Fatalf("row=%+v", r)
	}
}

func TestScanner_AggressiveCapsListEntries(t *testing.T) {
	old, oldJ := maxAggressiveChannels, joinInterval
	maxAggressiveChannels, joinInterval = 500, 200*time.Microsecond
	defer func() { maxAggressiveChannels, joinInterval = old, oldJ }()
	e := newEnv(t, "aggressive", config.DiscoveryConfig{})
	if err := e.sc.Scan(e.sid); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "LIST sent", func() bool { r, _, _ := e.fc.snapshot(); return len(r) == 1 })
	// Feed in chunks the loop has consumed, so the bus buffer never drops lines.
	for i := 0; i < 600; i++ {
		raw(e.bus, e.sid, fmt.Sprintf(":irc.example.org 322 me #c%d %d :", i, i+1))
		if i%50 == 49 {
			n := i + 1
			waitFor(t, "entries consumed", func() bool { return e.sc.listed(e.sid) == n })
		}
	}
	raw(e.bus, e.sid, ":irc.example.org 323 me :End of /LIST")
	e.waitState("done")
	if st := e.sc.Status(e.sid); st.Total != 500 || st.Joined != 500 {
		t.Fatalf("status=%+v", st)
	}
	_, joins, _ := e.fc.snapshot()
	for i := 0; i < 100; i++ {
		if contains(joins, fmt.Sprintf("#c%d", i)) {
			t.Fatalf("low-user #c%d joined", i)
		}
	}
}

func TestScanner_NeverPartsRealmOrPrejoined(t *testing.T) {
	e := newEnv(t, "normal", config.DiscoveryConfig{})
	if err := e.store.CreateRealm(&db.Realm{ServerID: e.sid, Name: "#example", DownloadChannel: "#example-chat", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	e.fc.prejoined["#joined"] = true
	e.list("#Example 9 :xdcc", "#example-chat 9 :xdcc", "#joined 9 :xdcc", "#xdcc-new 9 :")
	e.waitState("done")
	_, joins, parts := e.fc.snapshot()
	if len(joins) != 1 || joins[0] != "#xdcc-new" || len(parts) != 1 || parts[0] != "#xdcc-new" {
		t.Fatalf("joins=%v parts=%v", joins, parts)
	}
	if rows := e.rows(); len(rows) != 1 {
		t.Fatalf("rows=%v", rows)
	}
}

func TestScanner_TooManyChannelsLowersCap(t *testing.T) {
	old := maxHeld
	maxHeld = 3
	defer func() { maxHeld = old }()
	e := newEnv(t, "normal", config.DiscoveryConfig{})
	e.fc.autoNames = false
	e.fc.reject["#xdcc3"] = true
	e.list("#xdcc1 1 :", "#xdcc2 1 :", "#xdcc3 1 :", "#xdcc4 1 :", "#xdcc5 1 :")
	waitFor(t, "third join", func() bool { _, j, _ := e.fc.snapshot(); return len(j) >= 3 })
	named := map[string]bool{}
	waitFor(t, "done", func() bool {
		e.fc.mu.Lock()
		var todo []string
		for ch := range e.fc.held {
			if !named[ch] {
				named[ch] = true
				todo = append(todo, ch)
			}
		}
		e.fc.mu.Unlock()
		for _, ch := range todo {
			raw(e.bus, e.sid, ":irc.example.org 366 me "+ch+" :End of /NAMES list.")
		}
		return e.state() == "done"
	})
	_, joins, _ := e.fc.snapshot()
	for i := 1; i <= 5; i++ {
		if !contains(joins, fmt.Sprintf("#xdcc%d", i)) {
			t.Fatalf("#xdcc%d never joined: %v", i, joins)
		}
	}
	if !e.fc.saw405 || e.fc.maxAfter > 2 {
		t.Fatalf("held %d after 405", e.fc.maxAfter)
	}
}

func TestScanner_AbortsOnDisconnect(t *testing.T) {
	e := newEnv(t, "normal", config.DiscoveryConfig{})
	e.fc.autoNames = false
	e.list("#xdcc1 1 :", "#xdcc2 1 :")
	waitFor(t, "join", func() bool { _, j, _ := e.fc.snapshot(); return len(j) >= 1 })
	e.bus.Publish(irc.Event{Type: irc.EventConnectionStatus, ServerID: e.sid, Data: irc.ConnectionStatusEvent{Status: "disconnected"}})
	e.waitState("aborted")
	_, j1, p1 := e.fc.snapshot()
	raw(e.bus, e.sid, ":irc.example.org 366 me #xdcc1 :End of /NAMES list.")
	time.Sleep(30 * time.Millisecond)
	_, j2, p2 := e.fc.snapshot()
	if len(j1) != len(j2) || len(p1) != len(p2) {
		t.Fatalf("calls after abort: joins %v→%v parts %v→%v", j1, j2, p1, p2)
	}
}

func TestScanner_CancelPartsHeld(t *testing.T) {
	e := newEnv(t, "normal", config.DiscoveryConfig{})
	e.fc.autoNames = false
	e.list("#xdcc1 1 :", "#xdcc2 1 :")
	waitFor(t, "two joins", func() bool { _, j, _ := e.fc.snapshot(); return len(j) == 2 })
	e.sc.Cancel(e.sid)
	if s := e.state(); s != "cancelled" {
		t.Fatalf("state=%s", s)
	}
	_, _, parts := e.fc.snapshot()
	if !contains(parts, "#xdcc1") || !contains(parts, "#xdcc2") {
		t.Fatalf("parts=%v", parts)
	}
	if err := e.sc.Scan(e.sid); err != nil {
		t.Fatalf("rescan: %v", err)
	}
}

func TestScanner_ScanOnConnect(t *testing.T) {
	e := newEnv(t, "normal", config.DiscoveryConfig{})
	connected := irc.Event{Type: irc.EventConnectionStatus, ServerID: e.sid, Data: irc.ConnectionStatusEvent{Status: "connected"}}
	e.bus.Publish(connected)
	time.Sleep(40 * time.Millisecond)
	if r, _, _ := e.fc.snapshot(); len(r) != 0 {
		t.Fatalf("scanned with on-connect off: %v", r)
	}
	srv, _ := e.store.GetServer(e.sid)
	srv.DiscoveryOnConnect = true
	if err := e.store.UpdateServer(srv); err != nil {
		t.Fatal(err)
	}
	e.bus.Publish(connected)
	waitFor(t, "LIST on connect", func() bool { r, _, _ := e.fc.snapshot(); return len(r) == 1 && r[0] == "LIST" })
}

func TestScanner_RejectsConcurrentScan(t *testing.T) {
	e := newEnv(t, "normal", config.DiscoveryConfig{})
	if err := e.sc.Scan(e.sid); err != nil {
		t.Fatal(err)
	}
	if err := e.sc.Scan(e.sid); err != ErrScanRunning {
		t.Fatalf("err=%v", err)
	}
	e.sc.Cancel(e.sid)
}

// listed reports how many LIST entries the running scan has collected.
func (s *Scanner) listed(serverID int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.scans[serverID].list)
}
