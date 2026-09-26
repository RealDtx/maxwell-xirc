package parser

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/RealDtx/maxwell-irc/db"
	"github.com/RealDtx/maxwell-irc/internal/debug"
	"github.com/RealDtx/maxwell-irc/irc"
)

// ircFormattingRe matches IRC color/formatting control codes.
var ircFormattingRe = regexp.MustCompile(`\x03(?:[0-9]{1,2}(?:,[0-9]{1,2})?)?|[\x02\x0f\x16\x1d\x1e\x1f]`)

// stripIRCFormatting removes IRC color and formatting control codes from a string.
func stripIRCFormatting(s string) string {
	return ircFormattingRe.ReplaceAllString(s, "")
}

// fallbackWindow is the maximum age of a session that is eligible for
// server-wide fallback matching.  Messages arriving on channels (or via DM)
// that don't have an explicit session are only routed to a session created
// within this window — this prevents periodic bot announcements from polluting
// search results long after the search was started.
const fallbackWindow = 5 * time.Minute

type searchSession struct {
	ServerID      int64
	Channel       string
	Query         string
	SearchBot     string    // if non-empty, only accept results from this nick
	RealmID       int64     // realm ID for auto-detect persistence (0 = no realm)
	CreatedAt     int64     // monotonic counter for ordering
	CreatedAtTime time.Time // wall-clock time for fallback window

	// autoDetectHits tracks, per nick, how many query-matching listing lines
	// we've seen since the session started. Used to distinguish a real search
	// bot (which bursts multiple results) from a download/pack bot whose
	// periodic broadcast happens to contain the query word once.
	autoDetectHits map[string]int
}

// autoDetectThreshold is the number of query-matching listing lines required
// from a single nick before that nick is locked in as the search bot.
const autoDetectThreshold = 2

type Parser struct {
	store           db.Store
	bus             *irc.EventBus
	eventCh         <-chan irc.Event
	stopCh          chan struct{}
	mu              sync.RWMutex
	activeSessions  map[string]*searchSession // key: "serverID:channel"
	sessionCounter  int64                     // monotonic counter for session ordering
	botPatternCache map[string]int64          // key: "serverID:channel:botNick", value: patternID

	// dlChannels caches, per server, the set of realm download channels
	// (lowercased) so the hot message path doesn't hit the DB for every line.
	dlChannels map[int64]*dlChannelCache

	// broadcastPatterns caches parse patterns per download channel for the
	// passive-indexing path, which runs on every broadcast line forever. The
	// interactive search path stays uncached so freshly trained patterns
	// apply immediately.
	broadcastPatterns map[string]*patternCache

	// matchCounts accumulates per-pattern hits (searches + passive indexing)
	// between flushes, so the hot path never writes a counter row per line.
	matchCounts map[int64]int
}

// statsFlushInterval is how often batched pattern match counts hit the DB.
const statsFlushInterval = time.Minute

// dlChannelCacheTTL bounds how stale the download-channel set (and the
// broadcast pattern set) can get after realms or patterns are edited.
// ponytail: TTL poll instead of change invalidation.
const dlChannelCacheTTL = time.Minute

type dlChannelCache struct {
	channels  map[string]bool
	fetchedAt time.Time
}

type patternCache struct {
	patterns  []db.ParsePattern
	fetchedAt time.Time
}

func New(store db.Store, bus *irc.EventBus) *Parser {
	return &Parser{
		store:             store,
		bus:               bus,
		stopCh:            make(chan struct{}),
		activeSessions:    make(map[string]*searchSession),
		botPatternCache:   make(map[string]int64),
		dlChannels:        make(map[int64]*dlChannelCache),
		broadcastPatterns: make(map[string]*patternCache),
		matchCounts:       make(map[int64]int),
	}
}

func (p *Parser) Start() {
	p.eventCh = p.bus.Subscribe()
	go p.loop()
}

func (p *Parser) Stop() {
	close(p.stopCh)
	p.bus.Unsubscribe(p.eventCh)
	p.flushMatchCounts()
}

func (p *Parser) StartSearch(serverID int64, channel, query, searchBot string, realmID int64) {
	key := sessionKey(serverID, channel)
	p.mu.Lock()
	p.sessionCounter++
	p.activeSessions[key] = &searchSession{
		ServerID:       serverID,
		Channel:        channel,
		Query:          query,
		SearchBot:      searchBot,
		RealmID:        realmID,
		CreatedAt:      p.sessionCounter,
		CreatedAtTime:  time.Now(),
		autoDetectHits: map[string]int{},
	}
	p.mu.Unlock()
}

func (p *Parser) StopSearch(serverID int64, channel string) {
	key := sessionKey(serverID, channel)
	p.mu.Lock()
	delete(p.activeSessions, key)
	p.mu.Unlock()
}

func (p *Parser) GetCachedPatternID(serverID int64, channel, botNick string) int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.botPatternCache[fmt.Sprintf("%d:%s:%s", serverID, channel, botNick)]
}

func botCacheKey(serverID int64, channel, botNick string) string {
	return fmt.Sprintf("%d:%s:%s", serverID, channel, botNick)
}

func sessionKey(serverID int64, channel string) string {
	return fmt.Sprintf("%d:%s", serverID, channel)
}

// messageMatchesQuery returns true if the message line contains all words
// of the search query (case-insensitive).  This filters out periodic bot
// broadcasts that list ALL packages — those won't contain the search terms.
func messageMatchesQuery(message, query string) bool {
	lower := strings.ToLower(message)
	words := strings.Fields(strings.ToLower(query))
	for _, w := range words {
		if !strings.Contains(lower, w) {
			return false
		}
	}
	return true
}

func (p *Parser) loop() {
	ticker := time.NewTicker(statsFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.flushMatchCounts()
		case ev, ok := <-p.eventCh:
			if !ok {
				return
			}
			if ev.Type == irc.EventIRCMessage {
				p.handleMessage(ev)
			}
		}
	}
}

// stripCTCP removes CTCP framing (\x01...\x01) from a message.
func stripCTCP(msg string) string {
	if len(msg) >= 2 && msg[0] == '\x01' && msg[len(msg)-1] == '\x01' {
		return strings.TrimSpace(msg[1 : len(msg)-1])
	}
	return msg
}

// looksLikeListing returns true if msg resembles a file listing line.
// Used to filter out regular chat from unmatched search responses.
var (
	listingExtRe  = regexp.MustCompile(`(?i)\.\w{2,5}(?:\s|$|\|)`)
	listingSizeRe = regexp.MustCompile(`\d+\.?\d*\s*[KMGTP]`)
	listingPackRe = regexp.MustCompile(`(?:^|\s)#\d+\s|^\d+\)\s`)
)

func looksLikeListing(msg string) bool {
	if len(msg) < 15 {
		return false
	}
	return listingExtRe.MatchString(msg) ||
		listingSizeRe.MatchString(msg) ||
		listingPackRe.MatchString(msg)
}

func (p *Parser) handleMessage(ev irc.Event) {
	data, ok := ev.Data.(map[string]string)
	if !ok {
		return
	}

	msgType := data["type"]
	message := stripIRCFormatting(stripCTCP(data["message"]))

	// Only process privmsg and notice
	if msgType != "privmsg" && msgType != "notice" {
		debug.Debugf("parser: dropping type=%s server=%d channel=%s nick=%s", msgType, ev.ServerID, ev.Channel, ev.Nick)
		return
	}

	// Passive indexing: bots continuously advertise their packs on the realm
	// download channels (e.g. "#299 0x [491M] Some.File.mkv"). Feed every
	// parseable advertisement into the persistent file index, independent of
	// any search session. Falls through so live searches still work.
	if p.isDownloadChannel(ev.ServerID, ev.Channel) {
		p.indexBroadcast(ev, message)
	}

	// Check if there's an active search session for this channel
	key := sessionKey(ev.ServerID, ev.Channel)
	p.mu.RLock()
	session, hasSession := p.activeSessions[key]
	p.mu.RUnlock()

	if !hasSession {
		// Fall back to the most recently created session on the same server,
		// but only if it was created within the fallback window.  This covers
		// both DMs from bots AND bot responses on different channels (e.g.
		// #moviegods responding to a search started on #mg-chat) while
		// preventing stale sessions from capturing periodic bot announcements.
		now := time.Now()
		prefix := fmt.Sprintf("%d:", ev.ServerID)
		p.mu.RLock()
		// Two-pass fallback: prefer a session whose configured bot matches the
		// sender (strong signal), but accept any recent session as a last resort
		// so single-server setups with misconfigured or absent search_bot still
		// receive messages.
		var bestMatchAt, bestAnyAt int64
		var anySession *searchSession
		for k, s := range p.activeSessions {
			if !strings.HasPrefix(k, prefix) || now.Sub(s.CreatedAtTime) > fallbackWindow {
				continue
			}
			if s.SearchBot == "" || s.SearchBot == ev.Nick {
				if s.CreatedAt > bestMatchAt {
					session = s
					hasSession = true
					bestMatchAt = s.CreatedAt
				}
			} else {
				if s.CreatedAt > bestAnyAt {
					anySession = s
					bestAnyAt = s.CreatedAt
				}
			}
		}
		if !hasSession && anySession != nil {
			session = anySession
			hasSession = true
		}
		p.mu.RUnlock()
		if !hasSession {
			debug.Debugf("parser: no active session for server=%d channel=%s nick=%s msg=%q", ev.ServerID, ev.Channel, ev.Nick, debug.RedactForLog(message))
			return
		}
	}

	// Bot-nick filter: if the session has a configured search bot, only
	// process messages from that specific nick.
	if session.SearchBot != "" && ev.Nick != session.SearchBot {
		debug.Debugf("parser: rejecting nick=%s (expected %s) server=%d channel=%s", ev.Nick, session.SearchBot, ev.ServerID, ev.Channel)
		return
	}

	// Auto-detect gate: when the realm has no configured search bot, count
	// how many query-matching listing lines each nick has sent since the
	// session started. Only once a nick crosses the threshold do we lock it
	// in — this avoids grabbing a download/pack bot whose periodic broadcast
	// happened to contain the query word once. A real search bot bursts
	// multiple matches in response to our command.
	if session.SearchBot == "" && session.RealmID > 0 &&
		looksLikeListing(message) && messageMatchesQuery(message, session.Query) {
		p.mu.Lock()
		if session.autoDetectHits == nil {
			session.autoDetectHits = map[string]int{}
		}
		session.autoDetectHits[ev.Nick]++
		hits := session.autoDetectHits[ev.Nick]
		locked := false
		if hits >= autoDetectThreshold {
			session.SearchBot = ev.Nick
			locked = true
		}
		p.mu.Unlock()

		if locked {
			if err := p.store.UpdateRealmSearchBot(session.RealmID, ev.Nick); err != nil {
				log.Printf("failed to persist auto-detected search bot %q for realm %d: %v", ev.Nick, session.RealmID, err)
			} else {
				log.Printf("auto-detected search bot %q for realm %d after %d matches", ev.Nick, session.RealmID, hits)
			}
			p.bus.Publish(irc.Event{
				Type:     irc.EventSearchBotDetected,
				ServerID: ev.ServerID,
				Channel:  session.Channel,
				Nick:     ev.Nick,
				Data: map[string]string{
					"realm_id": fmt.Sprintf("%d", session.RealmID),
					"bot_nick": ev.Nick,
				},
			})
		}
	}

	// Load patterns scoped to this channel (globals + channel-specific)
	patterns, err := p.store.GetParsePatternsForChannel(session.ServerID, session.Channel)
	if err != nil {
		log.Printf("failed to load parse patterns: %v", err)
		return
	}
	debug.Debugf("parser: loaded %d patterns for server=%d channel=%s", len(patterns), session.ServerID, session.Channel)
	// Check bot-pattern cache first
	cacheKey := botCacheKey(session.ServerID, session.Channel, ev.Nick)
	p.mu.RLock()
	cachedPatternID := p.botPatternCache[cacheKey]
	p.mu.RUnlock()

	var result *ParsedResult
	var matchedPatternID int64

	if cachedPatternID > 0 {
		// Try cached pattern first
		for _, pat := range patterns {
			if pat.ID == cachedPatternID {
				r, pid, err := MatchLine(message, []db.ParsePattern{pat})
				if err == nil {
					result = r
					matchedPatternID = pid
				}
				break
			}
		}
	}

	// If cache miss, try all patterns
	if result == nil {
		r, pid, err := MatchLine(message, patterns)
		if err == nil {
			result = r
			matchedPatternID = pid
		}
	}

	// Build search result — use session.Channel so DM results are stored under the
	// searched channel, not the bot's nick.
	sr := &db.SearchResult{
		ServerID:    ev.ServerID,
		Channel:     session.Channel,
		BotNick:     ev.Nick,
		RawLine:     message,
		SearchQuery: session.Query,
		Parsed:      result != nil,
	}

	if result != nil {
		sr.PackNumber = result.PackNumber
		sr.Filename = result.Filename
		sr.Filesize = result.Filesize
		sr.DownloadsCount = result.DownloadsCount
		if result.BotNick != nil {
			sr.BotNick = *result.BotNick
		}

		// Update bot-pattern cache
		p.mu.Lock()
		p.botPatternCache[botCacheKey(session.ServerID, session.Channel, ev.Nick)] = matchedPatternID
		p.mu.Unlock()

		p.countMatch(matchedPatternID)
	}

	if result == nil {
		// Store unmatched lines that look like listings for the pattern trainer.
		if looksLikeListing(message) {
			if err := p.store.CreateSearchResult(sr); err != nil {
				log.Printf("failed to store unmatched search result: %v", err)
			}
		}
		debug.Debugf("parser: no pattern matched server=%d nick=%s line=%q", ev.ServerID, ev.Nick, debug.RedactForLog(message))
		return
	}

	// Query relevance filter: when no search bot is locked in yet, reject
	// parsed results whose raw line doesn't contain the search terms.  This
	// prevents periodic pack-bot broadcasts from polluting results during the
	// auto-detect window.  Once a specific bot is identified (explicit config
	// or auto-detect lock-in) we trust its responses unconditionally — the
	// bot was sent the query and its replies are inherently relevant even if
	// the codec alias differs ("HEVC" vs "x265", "H.264" vs "x264", etc.).
	if session.SearchBot == "" && !messageMatchesQuery(message, session.Query) {
		debug.Debugf("parser: query filter dropped result query=%q line=%q", debug.RedactForLog(session.Query), debug.RedactForLog(message))
		return
	}

	debug.Debugf("parser: matched pattern=%d server=%d nick=%s file=%v", matchedPatternID, ev.ServerID, ev.Nick, result.Filename)

	// Store result
	if err := p.store.CreateSearchResult(sr); err != nil {
		log.Printf("failed to store search result: %v", err)
	}

	// Feed the persistent, cross-search file index (best-effort). A filename
	// is required to index — if the matched pattern didn't map one, skip.
	if sr.Filename != nil && *sr.Filename != "" {
		if err := p.store.UpsertIndexedFile(&db.IndexedFile{
			ServerID:       ev.ServerID,
			Channel:        session.Channel,
			BotNick:        sr.BotNick,
			PackNumber:     sr.PackNumber,
			Filename:       *sr.Filename,
			Filesize:       sr.Filesize,
			DownloadsCount: sr.DownloadsCount,
			RawLine:        sr.RawLine,
		}); err != nil {
			log.Printf("failed to upsert indexed file: %v", err)
		}
	}

	// Publish parsed result event for WebSocket
	p.bus.Publish(irc.Event{
		Type:     irc.EventSearchResult,
		ServerID: ev.ServerID,
		Channel:  session.Channel,
		Nick:     ev.Nick,
		Data:     sr,
	})
}

// isDownloadChannel reports whether channel is a realm download channel of
// the given server. Backed by a TTL cache so the per-message cost is a map
// lookup, not a DB query.
func (p *Parser) isDownloadChannel(serverID int64, channel string) bool {
	if channel == "" || (channel[0] != '#' && channel[0] != '&') {
		return false
	}

	p.mu.RLock()
	c := p.dlChannels[serverID]
	p.mu.RUnlock()

	if c == nil || time.Since(c.fetchedAt) > dlChannelCacheTTL {
		channels := map[string]bool{}
		realms, err := p.store.GetRealms(serverID)
		if err != nil {
			log.Printf("failed to load realms for download-channel index: %v", err)
			// Cache the empty set anyway so a broken DB doesn't get hammered.
		} else {
			for _, r := range realms {
				if r.Enabled && r.DownloadChannel != "" {
					channels[strings.ToLower(r.DownloadChannel)] = true
				}
			}
		}
		c = &dlChannelCache{channels: channels, fetchedAt: time.Now()}
		p.mu.Lock()
		p.dlChannels[serverID] = c
		p.mu.Unlock()
	}

	return c.channels[strings.ToLower(channel)]
}

// broadcastPatternsFor returns the parse patterns for a download channel,
// cached with a TTL so the per-line cost is a map lookup.
func (p *Parser) broadcastPatternsFor(serverID int64, channel string) ([]db.ParsePattern, error) {
	key := sessionKey(serverID, channel)
	p.mu.RLock()
	c := p.broadcastPatterns[key]
	p.mu.RUnlock()
	if c != nil && time.Since(c.fetchedAt) <= dlChannelCacheTTL {
		return c.patterns, nil
	}

	patterns, err := p.store.GetParsePatternsForChannel(serverID, channel)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.broadcastPatterns[key] = &patternCache{patterns: patterns, fetchedAt: time.Now()}
	p.mu.Unlock()
	return patterns, nil
}

// indexBroadcast parses a download-channel advertisement line and upserts it
// into the persistent file index. Lines that don't parse to a pack number and
// filename (bandwidth notices, slot announcements, chat) are ignored.
func (p *Parser) indexBroadcast(ev irc.Event, message string) {
	patterns, err := p.broadcastPatternsFor(ev.ServerID, ev.Channel)
	if err != nil {
		log.Printf("failed to load parse patterns for broadcast indexing: %v", err)
		return
	}

	result, patternID, err := MatchLine(message, patterns)
	if err != nil || result.PackNumber == nil || result.Filename == nil || *result.Filename == "" {
		return
	}
	p.countMatch(patternID)

	botNick := ev.Nick
	if result.BotNick != nil && *result.BotNick != "" {
		botNick = *result.BotNick
	}

	if err := p.store.UpsertIndexedFile(&db.IndexedFile{
		ServerID:       ev.ServerID,
		Channel:        ev.Channel,
		BotNick:        botNick,
		PackNumber:     result.PackNumber,
		Filename:       *result.Filename,
		Filesize:       result.Filesize,
		DownloadsCount: result.DownloadsCount,
		RawLine:        message,
	}); err != nil {
		log.Printf("failed to index broadcast from %s: %v", botNick, err)
	}
}

func (p *Parser) countMatch(patternID int64) {
	p.mu.Lock()
	p.matchCounts[patternID]++
	p.mu.Unlock()
}

// flushMatchCounts writes the batched match counts to the DB. Counts that
// fail to persist are dropped — they are statistics, not data.
func (p *Parser) flushMatchCounts() {
	p.mu.Lock()
	counts := p.matchCounts
	p.matchCounts = make(map[int64]int)
	p.mu.Unlock()

	now := time.Now()
	for id, n := range counts {
		if err := p.store.RecordPatternMatch(id, n, now); err != nil {
			log.Printf("WARN: failed to record %d matches for pattern id=%d: %v", n, id, err)
		}
	}
}
