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

type searchSession struct {
	ServerID int64
	Channel  string
	Query    string
}

type Parser struct {
	store                db.Store
	bus                  *irc.EventBus
	eventCh              <-chan irc.Event
	stopCh               chan struct{}
	mu                   sync.RWMutex
	activeSessions       map[string]*searchSession // key: "serverID:channel"
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

func (p *Parser) StartSearch(serverID int64, channel, query string) {
	key := sessionKey(serverID, channel)
	p.mu.Lock()
	p.activeSessions[key] = &searchSession{
		ServerID: serverID,
		Channel:  channel,
		Query:    query,
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

	wasDM := false
	if !hasSession {
		// DMs: ev.Channel is the sender's nick (not a channel).
		// Bots often reply to !s commands via PRIVMSG to the user's nick.
		// Find any active session on this server.
		isDM := len(ev.Channel) == 0 || (ev.Channel[0] != '#' && ev.Channel[0] != '&' && ev.Channel[0] != '!' && ev.Channel[0] != '+')
		if isDM {
			prefix := fmt.Sprintf("%d:", ev.ServerID)
			p.mu.RLock()
			for k, s := range p.activeSessions {
				if strings.HasPrefix(k, prefix) {
					session = s
					hasSession = true
					wasDM = true
					break
				}
			}
			p.mu.RUnlock()
		}
		if !hasSession {
			return
		}
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

		// Update bot-pattern cache
		p.mu.Lock()
		p.botPatternCache[ev.Nick] = matchedPatternID
		p.mu.Unlock()

		// Update pattern match count
		p.updatePatternStats(matchedPatternID, true)
	} else if wasDM {
		// Only track failures for DM messages — those are actual (potential) bot responses.
		// Regular channel messages that don't match are not bot output and must not
		// degrade pattern stats.
		for _, pat := range patterns {
			p.updatePatternStats(pat.ID, false)
		}
	}

	// Only store and publish if the line actually matched a parse pattern.
	// Unmatched lines are regular user chat — not valid search results.
	if result == nil {
		return
	}

	// Store result
	if err := p.store.CreateSearchResult(sr); err != nil {
		log.Printf("failed to store search result: %v", err)
	}

	// Publish parsed result event for WebSocket
	p.bus.Publish(irc.Event{
		Type:     irc.EventSearchResult,
		ServerID: ev.ServerID,
		Channel:  ev.Channel,
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
