// Package discovery holds pure helpers for finding XDCC channels via LIST.
package discovery

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/RealDtx/maxwell-irc/parser"
)

// ListEntry is one 322 RPL_LIST row.
type ListEntry struct {
	Channel string
	Users   int
	Topic   string
}

var builtinKeywords = []string{"xdcc", "dcc", "download"}

// numeric splits a raw line ":srv NNN me rest" into its numeric and the text after the own nick.
func numeric(line string) (num, rest string, ok bool) {
	p := strings.SplitN(line, " ", 4)
	if len(p) < 3 || !strings.HasPrefix(p[0], ":") {
		return "", "", false
	}
	if len(p) == 4 {
		rest = p[3]
	}
	return p[1], rest, true
}

// ParseListLine parses a 322 (entry) or 323 (end of list) line.
func ParseListLine(line string) (e ListEntry, end bool, ok bool) {
	num, rest, k := numeric(line)
	if !k {
		return
	}
	switch num {
	case "323":
		return e, true, true
	case "322":
		f := strings.SplitN(rest, " ", 3)
		if len(f) < 2 {
			return
		}
		n, err := strconv.Atoi(f[1])
		if err != nil {
			return
		}
		e = ListEntry{Channel: f[0], Users: n}
		if len(f) == 3 {
			e.Topic = parser.StripFormatting(strings.TrimPrefix(f[2], ":"))
		}
		return e, false, true
	}
	return
}

func numericChannel(line, want string) (string, bool) {
	num, rest, ok := numeric(line)
	if !ok || num != want {
		return "", false
	}
	if ch := strings.Fields(rest); len(ch) > 0 {
		return ch[0], true
	}
	return "", false
}

// ParseTooManyChannels parses a 405 ERR_TOOMANYCHANNELS line.
func ParseTooManyChannels(line string) (string, bool) { return numericChannel(line, "405") }

// ParseEndOfNames parses a 366 RPL_ENDOFNAMES line.
func ParseEndOfNames(line string) (string, bool) { return numericChannel(line, "366") }

// MatchKeyword returns the first keyword (built-ins, then extra) found
// case-insensitively in name or topic, or "".
func MatchKeyword(name, topic string, extra []string) string {
	hay := strings.ToLower(name + " " + topic)
	for _, kw := range append(append([]string{}, builtinKeywords...), extra...) {
		if k := strings.ToLower(strings.TrimSpace(kw)); k != "" && strings.Contains(hay, k) {
			return k
		}
	}
	return ""
}

var chanRe = regexp.MustCompile(`[#&][\w.\-]+`)

// ExtractChannels returns lowercased, deduplicated channel tokens in text order.
func ExtractChannels(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range chanRe.FindAllString(text, -1) {
		m = strings.ToLower(strings.TrimRight(m, ".-"))
		if len(m) > 1 && !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// Classify returns "download", "chat" or "unknown" for channel given its partner.
func Classify(channel, partner string, channelAds, partnerAds int) string {
	if strings.HasSuffix(channel, "-chat") && !strings.HasSuffix(partner, "-chat") {
		return "download"
	}
	switch {
	case channelAds > 0 && partnerAds == 0:
		return "download"
	case partnerAds > 0 && channelAds == 0:
		return "chat"
	case partner != "":
		return "chat"
	case channelAds > 0:
		return "download"
	}
	return "unknown"
}

// RealmPrefill returns the (name, downloadChannel) to prefill a new realm with.
// Keep identical to addDiscoveredAsRealm in web/js/app.js.
func RealmPrefill(channel, partner, typ string) (name, downloadChannel string) {
	if typ == "download" && partner != "" {
		return partner, channel
	}
	return channel, partner
}
