package discovery

import (
	"container/heap"
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
	AllChannelNames() []string // channels of the server's enabled realms
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
	finished bool // parted after its stay
	written  bool // its row (or its pair's row) is settled
}

// byUsers is a min-heap keeping the aggressive mode's busiest channels.
type byUsers []*cand

func (h byUsers) Len() int           { return len(h) }
func (h byUsers) Less(i, j int) bool { return h[i].users < h[j].users }
func (h byUsers) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *byUsers) Push(x any)        { *h = append(*h, x.(*cand)) }
func (h *byUsers) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

type scan struct {
	serverID int64
	conn     Conn
	st       ScanStatus
	skip     map[string]bool // realm channels
	entries  int             // 322 lines consumed
	top      byUsers         // aggressive candidates while listing
	listEnd  chan struct{}   // closed on 323
	queue    []*cand
	cands    map[string]*cand // every queued candidate, for pairing
	held     map[string]*cand
	found    map[string]bool // channels upserted
	cap      int
	lastPub  time.Time
	stop     chan struct{} // closed on cancel/abort
}

func (sc *scan) running() bool { return sc.st.State == "listing" || sc.st.State == "joining" }

// Scanner runs LIST-based channel discovery scans, one per server.
// DB I/O never runs under mu, so the bus loop keeps draining events.
type Scanner struct {
	store  db.Store
	bus    *irc.EventBus
	conn   func(serverID int64) Conn // must return an untyped nil when not connected
	mu     sync.RWMutex
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
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sc := s.scans[serverID]; sc != nil {
		return sc.st
	}
	return ScanStatus{State: "idle"}
}

// Holding reports whether a running scan of serverID currently holds channel
// joined. Cheap: it runs per IRC line (message buffer, parser).
func (s *Scanner) Holding(serverID int64, channel string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sc := s.scans[serverID]
	return sc != nil && sc.running() && sc.held[strings.ToLower(channel)] != nil
}

// Listing reports whether a scan of serverID is waiting for LIST replies.
func (s *Scanner) Listing(serverID int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sc := s.scans[serverID]
	return sc != nil && sc.st.State == "listing"
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
	skip := map[string]bool{}
	realms, rerr := s.store.GetRealms(serverID)
	for _, r := range realms {
		skip[strings.ToLower(r.Name)] = true
		skip[strings.ToLower(r.DownloadChannel)] = true
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if sc := s.scans[serverID]; sc != nil && sc.running() {
		return ErrScanRunning
	}
	now := time.Now()
	sc := &scan{
		serverID: serverID, conn: c, cap: maxHeld, skip: skip,
		st:      ScanStatus{State: "listing", Mode: mode, StartedAt: &now},
		listEnd: make(chan struct{}), held: map[string]*cand{}, found: map[string]bool{},
		stop: make(chan struct{}),
	}
	s.scans[serverID] = sc
	if rerr == nil {
		rerr = c.SendRaw("LIST")
	}
	if rerr != nil {
		log.Printf("discovery: scan of server %d aborted: %v", serverID, rerr)
		sc.st.State = "aborted"
		s.publish(sc)
		return rerr
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
	realms := realmChannels(sc.conn)
	for _, c := range sc.held {
		if !realms[c.name] {
			sc.conn.PartChannel(c.name)
		}
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
	q := s.prepare(sc)
	s.mu.Unlock()
	// Patterns are only read once a candidate is held, which happens under mu after this.
	for _, c := range q {
		var err error
		if c.patterns, err = s.store.GetParsePatternsForChannel(sc.serverID, c.name); err != nil {
			log.Printf("discovery: patterns for %s: %v", c.name, err)
		}
	}
	t := time.NewTicker(joinInterval)
	defer t.Stop()
	for {
		select {
		case <-sc.stop:
			s.finish(sc, "")
			return
		case <-s.stopCh:
			return
		case <-t.C:
		}
		s.mu.Lock()
		rows, empty := s.tick(sc)
		s.mu.Unlock()
		s.write(sc, rows)
		if empty {
			s.finish(sc, "done")
			return
		}
	}
}

// prepare turns the collected candidates into the join queue. Caller holds mu.
func (s *Scanner) prepare(sc *scan) []*cand {
	if sc.st.State != "listing" {
		return nil
	}
	if sc.st.Mode == "aggressive" {
		sc.queue = append([]*cand{}, sc.top...)
		sort.SliceStable(sc.queue, func(i, j int) bool { return sc.queue[i].users > sc.queue[j].users })
		sc.top = nil
	}
	sc.cands = map[string]*cand{}
	for _, c := range sc.queue {
		sc.cands[c.name] = c
	}
	sc.st.Total, sc.st.State = len(sc.queue), "joining"
	s.publish(sc)
	return append([]*cand{}, sc.queue...)
}

// offer filters one LIST entry into the candidates as it arrives. Caller holds mu.
func (s *Scanner) offer(sc *scan, e ListEntry) {
	sc.entries++
	name := strings.ToLower(e.Channel)
	if sc.skip[name] || sc.conn.IsInChannel(name) {
		return
	}
	aggressive := sc.st.Mode == "aggressive"
	kw := MatchKeyword(name, e.Topic, s.cfg.Keywords)
	if kw == "" && !aggressive {
		return
	}
	sc.skip[name] = true // de-dup repeated 322 lines
	c := &cand{name: name, users: e.Users, keyword: kw}
	clue(c, e.Topic)
	if !aggressive {
		sc.queue = append(sc.queue, c)
		return
	}
	heap.Push(&sc.top, c)
	if sc.top.Len() > maxAggressiveChannels {
		heap.Pop(&sc.top)
	}
}

// tick parts finished channels and joins the next one. Caller holds mu.
// Returns the rows to write and whether nothing is queued or held.
func (s *Scanner) tick(sc *scan) (rows []*db.DiscoveredChannel, empty bool) {
	if !sc.running() {
		return nil, false
	}
	now := time.Now()
	stay := s.cfg.NormalStaySeconds
	if sc.st.Mode == "aggressive" {
		stay = s.cfg.AggressiveStaySeconds
	}
	var realms map[string]bool // channels that became realms mid-scan stay joined
	for key, c := range sc.held {
		// ponytail: no 366 within listTimeout (ban/invite-only/key) counts as entered, so the slot frees.
		entered := !c.namesAt.IsZero() && now.After(c.namesAt.Add(entryGrace)) || now.After(c.joinedAt.Add(listTimeout))
		if !entered || now.Before(c.joinedAt.Add(time.Duration(stay)*stayUnit)) {
			continue
		}
		if realms == nil {
			realms = realmChannels(sc.conn)
		}
		if !realms[c.name] {
			sc.conn.PartChannel(c.name)
		}
		delete(sc.held, key)
		c.finished = true
		rows = append(rows, settle(sc, c, false)...)
	}
	// A queued channel the user joined meanwhile is skipped: never joined, never parted.
	for len(sc.queue) > 0 && sc.conn.IsInChannel(sc.queue[0].name) {
		sc.queue = sc.queue[1:]
		sc.st.Total--
	}
	if len(sc.held) < sc.cap && len(sc.queue) > 0 {
		c := sc.queue[0]
		sc.queue = sc.queue[1:]
		c.joinedAt, c.namesAt = now, time.Time{}
		sc.held[c.name] = c
		sc.st.Joined++
		sc.conn.JoinChannel(c.name, "")
		if now.Sub(sc.lastPub) >= progressEvery {
			s.publish(sc)
		}
	}
	return rows, len(sc.queue) == 0 && len(sc.held) == 0
}

// finish writes the rows still pending at the end of a scan and, for a
// normal end, sets state. Runs on the scan goroutine.
func (s *Scanner) finish(sc *scan, state string) {
	s.mu.Lock()
	var rows []*db.DiscoveredChannel
	for _, c := range sc.cands {
		if c.finished && !c.written {
			rows = append(rows, settle(sc, c, true)...)
		}
	}
	s.mu.Unlock()
	s.write(sc, rows)
	s.mu.Lock()
	defer s.mu.Unlock()
	if state != "" && sc.running() {
		sc.st.State = state
		s.publish(sc)
	} else if len(rows) > 0 {
		s.publish(sc)
	}
}

// write upserts rows (outside mu) and updates Found.
func (s *Scanner) write(sc *scan, rows []*db.DiscoveredChannel) {
	var ok []string
	for _, d := range rows {
		if err := s.store.UpsertDiscoveredChannel(d); err != nil {
			log.Printf("discovery: upsert %s: %v", d.Channel, err)
			continue
		}
		ok = append(ok, d.Channel)
	}
	if len(ok) == 0 {
		return
	}
	s.mu.Lock()
	for _, ch := range ok {
		sc.found[ch] = true
	}
	sc.st.Found = len(sc.found)
	s.mu.Unlock()
}

// settle decides x's row once it is finished. When both halves of a pair are
// candidates of this scan they share one row: download side, partner = chat.
// x waits while its pair half is still queued or held, unless final. Caller holds mu.
func settle(sc *scan, x *cand, final bool) []*db.DiscoveredChannel {
	y := sc.cands[x.partner]
	if y == nil {
		for _, c := range sc.cands {
			if c != x && c.partner == x.name {
				y = c
				break
			}
		}
	}
	switch {
	case y == nil || !y.finished && final:
		x.written = true
		if reason(x) == "" {
			return nil
		}
		return []*db.DiscoveredChannel{row(sc, x, x.partner, Classify(x.name, x.partner, x.ads, 0), reason(x))}
	case !y.finished:
		return nil // wait for y
	case y.written:
		// y already has its own row: pair onto it rather than add a second.
		x.written = true
		if y.partner != "" && y.partner != x.name { // y is paired elsewhere: x gets its own row
			if reason(x) == "" {
				return nil
			}
			return []*db.DiscoveredChannel{row(sc, x, x.partner, Classify(x.name, x.partner, x.ads, 0), reason(x))}
		}
		if reason(x)+reason(y) == "" {
			return nil
		}
		return []*db.DiscoveredChannel{row(sc, y, x.name, Classify(y.name, x.name, y.ads, x.ads), firstNonEmpty(reason(y), reason(x)))}
	}
	x.written, y.written = true, true
	d, c := x, y
	if Classify(x.name, y.name, x.ads, y.ads) != "download" {
		d, c = y, x
	}
	r := firstNonEmpty(reason(d), reason(c))
	if r == "" {
		return nil
	}
	return []*db.DiscoveredChannel{row(sc, d, c.name, "download", r)}
}

// realmChannels is the lowercased set of the connection's realm channels.
func realmChannels(c Conn) map[string]bool {
	m := map[string]bool{}
	for _, n := range c.AllChannelNames() {
		m[strings.ToLower(n)] = true
	}
	return m
}

func row(sc *scan, c *cand, partner, typ, reason string) *db.DiscoveredChannel {
	return &db.DiscoveredChannel{ServerID: sc.serverID, Channel: c.name, Partner: partner, Users: c.users, Type: typ, Reason: reason, AdCount: c.ads}
}

// reason is why c qualifies for a row, or "" (aggressive channel without keyword or ads).
func reason(c *cand) string {
	switch {
	case c.keyword != "":
		return "keyword: " + c.keyword
	case c.ads > 0:
		return fmt.Sprintf("%d bot ads", c.ads)
	}
	return ""
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
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

// onConnect schedules a scan-on-connect; the server is read after the delay.
func (s *Scanner) onConnect(serverID int64) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		select {
		case <-time.After(connectDelay):
		case <-s.stopCh:
			return
		}
		if srv, err := s.store.GetServer(serverID); err != nil || !srv.DiscoveryOnConnect {
			return
		}
		if err := s.Scan(serverID); err != nil {
			log.Printf("discovery: scan on connect for server %d: %v", serverID, err)
		}
	}()
}

// onRaw handles numerics. Caller holds mu.
func (s *Scanner) onRaw(sc *scan, line string) {
	if sc.st.State == "listing" {
		if e, end, ok := ParseListLine(line); ok {
			if !end {
				s.offer(sc, e)
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
