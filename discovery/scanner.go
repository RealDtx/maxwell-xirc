package discovery

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RealDtx/maxwell-irc/config"
	"github.com/RealDtx/maxwell-irc/db"
	"github.com/RealDtx/maxwell-irc/irc"
	"github.com/RealDtx/maxwell-irc/parser"
)

// Timing and limits; vars so tests can shrink them.
var (
	joinInterval          = 2 * time.Second
	entryGrace            = 5 * time.Second
	listTimeout           = 60 * time.Second
	connectDelay          = 20 * time.Second
	maxHeld               = 20
	maxAggressiveChannels = 500
	noticeWindow          = 10 * time.Second
	stayUnit              = time.Second // unit of the config stay values
)

const progressEvery = 2 * time.Second

var (
	ErrScanRunning  = errors.New("discovery scan already running")
	ErrNotConnected = errors.New("server not connected")
)

// Conn is the part of *irc.Connection the scanner drives.
type Conn interface {
	SendRaw(raw string) error
	JoinChannel(name, key string)
	PartChannel(name string)
	IsInChannel(name string) bool
}

// ScanStatus is the per-server scan state, also the EventDiscovery payload.
type ScanStatus struct {
	State     string     `json:"state"` // idle|listing|joining|done|cancelled|aborted
	Mode      string     `json:"mode"`
	Joined    int        `json:"joined"`
	Total     int        `json:"total"`
	Found     int        `json:"found"`
	StartedAt *time.Time `json:"started_at"`
}

type cand struct {
	name     string // lowercased
	users    int
	keyword  string
	partner  string
	ads      int
	patterns []db.ParsePattern
	joinedAt time.Time
	namesAt  time.Time // 366 received
	requeued bool
}

type scan struct {
	serverID int64
	conn     Conn
	st       ScanStatus
	list     []ListEntry
	listEnd  chan struct{} // closed on 323
	queue    []*cand
	held     map[string]*cand
	cap      int
	lastPub  time.Time
	stop     chan struct{} // closed on cancel/abort
}

func (sc *scan) running() bool { return sc.st.State == "listing" || sc.st.State == "joining" }

// Scanner runs LIST-based channel discovery scans, one per server.
type Scanner struct {
	store  db.Store
	bus    *irc.EventBus
	conn   func(serverID int64) Conn // must return an untyped nil when not connected
	mu     sync.Mutex
	cfg    config.DiscoveryConfig
	scans  map[int64]*scan
	events <-chan irc.Event
	stopCh chan struct{}
	wg     sync.WaitGroup
}

func New(store db.Store, bus *irc.EventBus, conn func(serverID int64) Conn, cfg config.DiscoveryConfig) *Scanner {
	return &Scanner{store: store, bus: bus, conn: conn, cfg: cfg, scans: map[int64]*scan{}, stopCh: make(chan struct{})}
}

func (s *Scanner) Start() {
	s.events = s.bus.Subscribe()
	s.wg.Add(1)
	go s.loop()
}

func (s *Scanner) Stop() {
	close(s.stopCh)
	s.bus.Unsubscribe(s.events)
	s.wg.Wait()
}

func (s *Scanner) SetConfig(cfg config.DiscoveryConfig) {
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
}

func (s *Scanner) Status(serverID int64) ScanStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sc := s.scans[serverID]; sc != nil {
		return sc.st
	}
	return ScanStatus{State: "idle"}
}

// Scan starts a scan of serverID in the server's configured mode.
func (s *Scanner) Scan(serverID int64) error {
	srv, err := s.store.GetServer(serverID)
	if err != nil {
		return err
	}
	c := s.conn(serverID)
	if c == nil {
		return ErrNotConnected
	}
	mode := "normal"
	if srv.DiscoveryMode == "aggressive" {
		mode = "aggressive"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if sc := s.scans[serverID]; sc != nil && sc.running() {
		return ErrScanRunning
	}
	now := time.Now()
	sc := &scan{
		serverID: serverID, conn: c, cap: maxHeld,
		st:      ScanStatus{State: "listing", Mode: mode, StartedAt: &now},
		listEnd: make(chan struct{}), held: map[string]*cand{}, stop: make(chan struct{}),
	}
	s.scans[serverID] = sc
	if err := c.SendRaw("LIST"); err != nil {
		sc.st.State = "aborted"
		s.publish(sc)
		return err
	}
	s.publish(sc)
	s.wg.Add(1)
	go s.run(sc)
	return nil
}

// Cancel parts every channel the scan holds and marks it cancelled.
func (s *Scanner) Cancel(serverID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sc := s.scans[serverID]
	if sc == nil || !sc.running() {
		return
	}
	for _, c := range sc.held {
		sc.conn.PartChannel(c.name)
	}
	sc.held = map[string]*cand{}
	s.end(sc, "cancelled")
}

// end sets a terminal state that stops the scan goroutine. Caller holds mu.
func (s *Scanner) end(sc *scan, state string) {
	sc.st.State = state
	close(sc.stop)
	s.publish(sc)
}

// publish emits the scan status. Caller holds mu.
func (s *Scanner) publish(sc *scan) {
	sc.lastPub = time.Now()
	s.bus.Publish(irc.Event{Type: irc.EventDiscovery, ServerID: sc.serverID, Timestamp: sc.lastPub.Format(time.RFC3339Nano), Data: sc.st})
}

func (s *Scanner) run(sc *scan) {
	defer s.wg.Done()
	select {
	case <-sc.listEnd:
	case <-time.After(listTimeout):
	case <-sc.stop:
		return
	case <-s.stopCh:
		return
	}
	s.mu.Lock()
	ok := s.prepare(sc)
	s.mu.Unlock()
	if !ok {
		return
	}
	t := time.NewTicker(joinInterval)
	defer t.Stop()
	for {
		select {
		case <-sc.stop:
			return
		case <-s.stopCh:
			return
		case <-t.C:
		}
		s.mu.Lock()
		finished := s.tick(sc)
		s.mu.Unlock()
		if finished {
			return
		}
	}
}

// prepare turns the LIST result into the join queue. Caller holds mu.
func (s *Scanner) prepare(sc *scan) bool {
	if sc.st.State != "listing" {
		return false
	}
	skip := map[string]bool{}
	realms, _ := s.store.GetRealms(sc.serverID)
	for _, r := range realms {
		skip[strings.ToLower(r.Name)] = true
		skip[strings.ToLower(r.DownloadChannel)] = true
	}
	aggressive := sc.st.Mode == "aggressive"
	var q []*cand
	for _, e := range sc.list {
		name := strings.ToLower(e.Channel)
		if skip[name] || sc.conn.IsInChannel(name) {
			continue
		}
		skip[name] = true
		kw := MatchKeyword(name, e.Topic, s.cfg.Keywords)
		if kw == "" && !aggressive {
			continue
		}
		c := &cand{name: name, users: e.Users, keyword: kw}
		clue(c, e.Topic)
		q = append(q, c)
	}
	if aggressive {
		sort.SliceStable(q, func(i, j int) bool { return q[i].users > q[j].users })
		if len(q) > maxAggressiveChannels {
			q = q[:maxAggressiveChannels]
		}
	}
	sc.list, sc.queue = nil, q
	sc.st.Total, sc.st.State = len(q), "joining"
	s.publish(sc)
	return true
}

// tick parts finished channels and joins the next one. Caller holds mu.
// Returns true when the scan goroutine should exit.
func (s *Scanner) tick(sc *scan) bool {
	if !sc.running() {
		return true
	}
	now := time.Now()
	stay := s.cfg.NormalStaySeconds
	if sc.st.Mode == "aggressive" {
		stay = s.cfg.AggressiveStaySeconds
	}
	for key, c := range sc.held {
		// ponytail: no 366 within listTimeout (ban/invite-only/key) counts as entered, so the slot frees.
		entered := !c.namesAt.IsZero() && now.After(c.namesAt.Add(entryGrace)) || now.After(c.joinedAt.Add(listTimeout))
		if !entered || now.Before(c.joinedAt.Add(time.Duration(stay)*stayUnit)) {
			continue
		}
		sc.conn.PartChannel(c.name)
		delete(sc.held, key)
		s.record(sc, c)
	}
	if len(sc.held) < sc.cap && len(sc.queue) > 0 {
		c := sc.queue[0]
		sc.queue = sc.queue[1:]
		c.patterns, _ = s.store.GetParsePatternsForChannel(sc.serverID, c.name)
		c.joinedAt, c.namesAt = now, time.Time{}
		sc.held[c.name] = c
		sc.st.Joined++
		sc.conn.JoinChannel(c.name, "")
		if now.Sub(sc.lastPub) >= progressEvery {
			s.publish(sc)
		}
	}
	if len(sc.queue) == 0 && len(sc.held) == 0 {
		sc.st.State = "done"
		s.publish(sc)
		return true
	}
	return false
}

// record upserts the channel's row if it qualifies. Caller holds mu.
func (s *Scanner) record(sc *scan, c *cand) {
	if c.keyword == "" && c.ads == 0 {
		return // only reachable in aggressive mode
	}
	reason := fmt.Sprintf("%d bot ads", c.ads)
	if c.keyword != "" {
		reason = "keyword: " + c.keyword
	}
	d := &db.DiscoveredChannel{
		ServerID: sc.serverID, Channel: c.name, Partner: c.partner, Users: c.users,
		Type: Classify(c.name, c.partner, c.ads, 0), Reason: reason, AdCount: c.ads,
	}
	if err := s.store.UpsertDiscoveredChannel(d); err != nil {
		log.Printf("discovery: upsert %s: %v", c.name, err)
		return
	}
	sc.st.Found++
}

// clue sets c's partner from the first other channel mentioned in text.
func clue(c *cand, text string) {
	if c.partner != "" {
		return
	}
	for _, ch := range ExtractChannels(text) {
		if ch != c.name {
			c.partner = ch
			return
		}
	}
}

func (s *Scanner) loop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.stopCh:
			return
		case ev, ok := <-s.events:
			if !ok {
				return
			}
			s.handle(ev)
		}
	}
}

func (s *Scanner) handle(ev irc.Event) {
	switch ev.Type {
	case irc.EventConnectionStatus:
		st, _ := ev.Data.(irc.ConnectionStatusEvent)
		switch st.Status {
		case "connected":
			s.onConnect(ev.ServerID)
		case "disconnected":
			s.mu.Lock()
			if sc := s.scans[ev.ServerID]; sc != nil && sc.running() {
				s.end(sc, "aborted")
			}
			s.mu.Unlock()
		}
	case irc.EventIRCMessage:
		data, _ := ev.Data.(map[string]string)
		s.mu.Lock()
		defer s.mu.Unlock()
		sc := s.scans[ev.ServerID]
		if sc == nil || !sc.running() {
			return
		}
		switch data["type"] {
		case "raw":
			if ev.Channel == "" { // the Channel-set copy would double count
				s.onRaw(sc, data["message"])
			}
		case "privmsg", "notice":
			onMsg(sc, ev, data["type"], parser.StripFormatting(data["message"]))
		}
	}
}

func (s *Scanner) onConnect(serverID int64) {
	srv, err := s.store.GetServer(serverID)
	if err != nil || !srv.DiscoveryOnConnect {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		select {
		case <-time.After(connectDelay):
			if err := s.Scan(serverID); err != nil {
				log.Printf("discovery: scan on connect for server %d: %v", serverID, err)
			}
		case <-s.stopCh:
		}
	}()
}

// onRaw handles numerics. Caller holds mu.
func (s *Scanner) onRaw(sc *scan, line string) {
	if sc.st.State == "listing" {
		if e, end, ok := ParseListLine(line); ok {
			if !end {
				sc.list = append(sc.list, e)
			} else if !isClosed(sc.listEnd) {
				close(sc.listEnd)
			}
			return
		}
	}
	if ch, ok := ParseEndOfNames(line); ok {
		if c := sc.held[strings.ToLower(ch)]; c != nil && c.namesAt.IsZero() {
			c.namesAt = time.Now()
		}
		return
	}
	if ch, ok := ParseTooManyChannels(line); ok {
		key := strings.ToLower(ch)
		c := sc.held[key]
		if c == nil {
			return
		}
		sc.cap = max(1, len(sc.held)-1)
		delete(sc.held, key)
		sc.st.Joined--
		if !c.requeued {
			c.requeued = true
			sc.queue = append(sc.queue, c)
		}
		return
	}
	if num, rest, ok := numeric(line); ok && num == "332" {
		if f := strings.SplitN(rest, " ", 2); len(f) == 2 {
			if c := sc.held[strings.ToLower(f[0])]; c != nil {
				clue(c, parser.StripFormatting(strings.TrimPrefix(f[1], ":")))
			}
		}
	}
}

// onMsg collects clues and ad counts from PRIVMSG/NOTICE. Caller holds mu.
func onMsg(sc *scan, ev irc.Event, typ, text string) {
	if c := sc.held[strings.ToLower(ev.Channel)]; c != nil {
		clue(c, text)
		if typ == "privmsg" {
			if _, _, err := parser.MatchLine(text, c.patterns); err == nil {
				c.ads++
			}
		}
		return
	}
	if ev.Channel == "" || ev.Channel != ev.Nick {
		return // not a DM
	}
	mentioned := false
	for _, ch := range ExtractChannels(text) {
		if c := sc.held[ch]; c != nil {
			clue(c, text)
			mentioned = true
		}
	}
	if mentioned {
		return
	}
	var recent *cand
	n := 0
	for _, c := range sc.held {
		if time.Since(c.joinedAt) <= noticeWindow {
			recent, n = c, n+1
		}
	}
	if n == 1 {
		clue(recent, text)
	}
}

func isClosed(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
