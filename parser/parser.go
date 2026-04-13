package parser

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/maxwell-xirc/xirc/db"
	"github.com/maxwell-xirc/xirc/irc"
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
}

type Parser struct {
	store                db.Store
	bus                  *irc.EventBus
	eventCh              <-chan irc.Event
	stopCh               chan struct{}
	mu                   sync.RWMutex
	activeSessions       map[string]*searchSession // key: "serverID:channel"
	sessionCounter       int64                     // monotonic counter for session ordering
	botPatternCache      map[string]int64          // key: botNick, value: patternID
	degradationThreshold int
}

func New(store db.Store, bus *irc.EventBus) *Parser {
	return &Parser{
		store:                store,
		bus:                  bus,
		stopCh:               make(chan struct{}),
		activeSessions:       make(map[string]*searchSession),
		botPatternCache:      make(map[string]int64),
		degradationThreshold: 50,
	}
}

func (p *Parser) SetDegradationThreshold(n int) {
	p.degradationThreshold = n
}

func (p *Parser) Start() {
	p.eventCh = p.bus.Subscribe()
	go p.loop()
}

func (p *Parser) Stop() {
	close(p.stopCh)
	p.bus.Unsubscribe(p.eventCh)
}

func (p *Parser) StartSearch(serverID int64, channel, query, searchBot string, realmID int64) {
	key := sessionKey(serverID, channel)
	p.mu.Lock()
	p.sessionCounter++
	p.activeSessions[key] = &searchSession{
		ServerID:      serverID,
		Channel:       channel,
		Query:         query,
		SearchBot:     searchBot,
		RealmID:       realmID,
		CreatedAt:     p.sessionCounter,
		CreatedAtTime: time.Now(),
	}
	p.mu.Unlock()
}

func (p *Parser) StopSearch(serverID int64, channel string) {
	key := sessionKey(serverID, channel)
	p.mu.Lock()
	delete(p.activeSessions, key)
	p.mu.Unlock()
}

func (p *Parser) GetCachedPatternID(botNick string) int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.botPatternCache[botNick]
}

func sessionKey(serverID int64, channel string) string {
	return fmt.Sprintf("%d:%s", serverID, channel)
}

func (p *Parser) loop() {
	for {
		select {
		case <-p.stopCh:
			return
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
		return
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
		var bestCreatedAt int64
		for k, s := range p.activeSessions {
			if strings.HasPrefix(k, prefix) && s.CreatedAt > bestCreatedAt && now.Sub(s.CreatedAtTime) <= fallbackWindow {
				session = s
				hasSession = true
				bestCreatedAt = s.CreatedAt
			}
		}
		p.mu.RUnlock()
		if !hasSession {
			return
		}
	}

	// Bot-nick filter: if the session has a configured search bot, only
	// process messages from that specific nick.
	if session.SearchBot != "" && ev.Nick != session.SearchBot {
		return
	}

	// Load patterns
	patterns, err := p.store.GetParsePatterns()
	if err != nil {
		log.Printf("failed to load parse patterns: %v", err)
		return
	}
	// Check bot-pattern cache first
	p.mu.RLock()
	cachedPatternID := p.botPatternCache[ev.Nick]
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
		p.botPatternCache[ev.Nick] = matchedPatternID
		p.mu.Unlock()

		// Update pattern match count (success only — no auto-disable on failure)
		p.updatePatternStats(matchedPatternID, true)
	}

	if result == nil {
		// Store unmatched lines that look like listings for the pattern trainer.
		if looksLikeListing(message) {
			if err := p.store.CreateSearchResult(sr); err != nil {
				log.Printf("failed to store unmatched search result: %v", err)
			}
		}
		return
	}

	// Store result
	if err := p.store.CreateSearchResult(sr); err != nil {
		log.Printf("failed to store search result: %v", err)
	}

	// Auto-detect: if no search bot was configured and this is the first
	// parseable result, lock in this bot nick for the session and persist
	// it to the realm.
	if session.SearchBot == "" && session.RealmID > 0 {
		p.mu.Lock()
		session.SearchBot = ev.Nick
		p.mu.Unlock()

		if err := p.store.UpdateRealmSearchBot(session.RealmID, ev.Nick); err != nil {
			log.Printf("failed to persist auto-detected search bot %q for realm %d: %v", ev.Nick, session.RealmID, err)
		} else {
			log.Printf("auto-detected search bot %q for realm %d", ev.Nick, session.RealmID)
		}

		// Notify frontend
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

	// Publish parsed result event for WebSocket
	p.bus.Publish(irc.Event{
		Type:     irc.EventSearchResult,
		ServerID: ev.ServerID,
		Channel:  session.Channel,
		Nick:     ev.Nick,
		Data:     sr,
	})
}

func (p *Parser) updatePatternStats(patternID int64, matched bool) {
	patterns, err := p.store.GetParsePatterns()
	if err != nil {
		return
	}

	for _, pat := range patterns {
		if pat.ID != patternID {
			continue
		}

		if matched {
			pat.MatchCount++
			pat.FailCount = 0
			now := time.Now()
			pat.LastMatchedAt = &now
		} else {
			pat.FailCount++
			if pat.FailCount >= p.degradationThreshold {
				pat.AutoDisabled = true
				log.Printf("auto-disabled pattern %q (id=%d): %d consecutive failures", pat.Name, pat.ID, pat.FailCount)
				p.bus.Publish(irc.Event{
					Type: irc.EventNotification,
					Data: map[string]string{
						"severity": "warning",
						"message":  fmt.Sprintf("Parse pattern %q auto-disabled after %d failures", pat.Name, pat.FailCount),
					},
				})
			}
		}

		if err := p.store.UpdateParsePattern(&pat); err != nil {
			log.Printf("WARN: failed to persist pattern stats for %q: %v", pat.Name, err)
		}
		break
	}
}
