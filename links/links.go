// Package links implements xirc:// pack links: one link names one XDCC pack
// on one network, so packs can be shared as plain text between instances.
package links

import (
	"errors"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/RealDtx/maxwell-irc/db"
)

// Link is one XDCC pack. Host identifies the network; Name, Size, First and
// Last are optional.
type Link struct {
	Host, Channel, Bot string
	Pack               int
	Name, Size         string
	First, Last        time.Time
}

// LineError reports a token that looked like a link but didn't parse.
type LineError struct {
	Line   string `json:"line"`
	Reason string `json:"reason"`
}

// Format renders l as xirc://host/channel/bot/pack?name=…&size=…&first=…&last=….
func Format(l Link) string {
	q := url.Values{}
	if l.Name != "" {
		q.Set("name", l.Name)
	}
	if l.Size != "" {
		q.Set("size", l.Size)
	}
	if !l.First.IsZero() {
		q.Set("first", l.First.UTC().Format(time.RFC3339))
	}
	if !l.Last.IsZero() {
		q.Set("last", l.Last.UTC().Format(time.RFC3339))
	}
	s := "xirc://" + l.Host + "/" + url.PathEscape(l.Channel) + "/" + url.PathEscape(l.Bot) + "/" + strconv.Itoa(l.Pack)
	if len(q) > 0 {
		s += "?" + q.Encode()
	}
	return s
}

var tokenRE = regexp.MustCompile(`(?i)(?:web\+)?xirc://[^\s<>"']+`)

// Parse extracts every xirc:// (or web+xirc://) link from arbitrary text.
// Malformed links are returned as LineErrors; exact duplicates collapse.
func Parse(text string) ([]Link, []LineError) {
	var out []Link
	var bad []LineError
	seen := map[string]bool{}
	for _, tok := range tokenRE.FindAllString(text, -1) {
		tok = strings.TrimRight(tok, ".,;)") // punctuation from surrounding prose
		l, err := parseOne(tok)
		if err != nil {
			bad = append(bad, LineError{Line: tok, Reason: err.Error()})
			continue
		}
		if key := Format(l); !seen[key] {
			seen[key] = true
			out = append(out, l)
		}
	}
	return out, bad
}

func parseOne(tok string) (Link, error) {
	raw := tok[strings.Index(strings.ToLower(tok), "xirc://"):]
	// A hand-typed "#chan" would start a URL fragment; make it literal.
	raw = strings.ReplaceAll(raw, "#", "%23")
	u, err := url.Parse(raw)
	if err != nil {
		return Link{}, errors.New("invalid link")
	}
	parts := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	if u.Hostname() == "" || len(parts) != 3 {
		return Link{}, errors.New("expected xirc://host/channel/bot/pack")
	}
	ch, err1 := url.PathUnescape(parts[0])
	bot, err2 := url.PathUnescape(parts[1])
	if err1 != nil || err2 != nil || ch == "" || bot == "" || strings.ContainsAny(ch+bot, " \r\n") {
		return Link{}, errors.New("invalid channel or bot")
	}
	pack, err := strconv.Atoi(parts[2])
	if err != nil || pack <= 0 {
		return Link{}, errors.New("invalid pack number")
	}
	q := u.Query()
	l := Link{Host: strings.ToLower(u.Hostname()), Channel: ch, Bot: bot, Pack: pack, Name: q.Get("name"), Size: q.Get("size")}
	// Optional dates: an unparsable value is dropped, not an error.
	l.First, _ = time.Parse(time.RFC3339, q.Get("first"))
	l.Last, _ = time.Parse(time.RFC3339, q.Get("last"))
	return l, nil
}

// ResolveServer maps a link host to a configured server: exact host first
// (case-insensitive), else the only server on the same domain.
func ResolveServer(host string, servers []db.Server) (*db.Server, bool) {
	host = strings.ToLower(host)
	for i := range servers {
		if strings.EqualFold(servers[i].Host, host) {
			return &servers[i], true
		}
	}
	d := domain(host)
	if d == "" {
		return nil, false
	}
	var match *db.Server
	for i := range servers {
		if domain(strings.ToLower(servers[i].Host)) == d {
			if match != nil {
				return nil, false // ambiguous
			}
			match = &servers[i]
		}
	}
	return match, match != nil
}

// domain returns the last two labels of h, or "" for IPs and bare names.
func domain(h string) string {
	if net.ParseIP(h) != nil {
		return ""
	}
	p := strings.Split(h, ".")
	if len(p) < 2 {
		return ""
	}
	return strings.Join(p[len(p)-2:], ".")
}
