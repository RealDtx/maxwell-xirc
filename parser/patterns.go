package parser

import (
	"encoding/json"
	"errors"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/RealDtx/maxwell-irc/db"
)

var ErrNoMatch = errors.New("no pattern matched")

type ParsedResult struct {
	PackNumber     *int
	Filename       *string
	Filesize       *string
	DownloadsCount *int
	BotNick        *string
}

type fieldMapping struct {
	PackNumber     int `json:"pack_number"`
	DownloadsCount int `json:"downloads_count"`
	Filesize       int `json:"filesize"`
	Filename       int `json:"filename"`
	BotNick        int `json:"bot_nick"`
}

// regexCache avoids recompiling the same regex on every call.
var (
	regexCacheMu sync.RWMutex
	regexCache   = make(map[string]*regexp.Regexp)
)

func getRegexp(pattern string) (*regexp.Regexp, error) {
	regexCacheMu.RLock()
	re, ok := regexCache[pattern]
	regexCacheMu.RUnlock()
	if ok {
		return re, nil
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	regexCacheMu.Lock()
	regexCache[pattern] = re
	regexCacheMu.Unlock()
	return re, nil
}

// MatchLine tries each enabled pattern (sorted by priority descending) against the line.
// Returns the parsed result and the matching pattern's ID, or ErrNoMatch.
func MatchLine(line string, patterns []db.ParsePattern) (*ParsedResult, int64, error) {
	// Sort by priority descending
	sorted := make([]db.ParsePattern, len(patterns))
	copy(sorted, patterns)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority > sorted[j].Priority
	})

	for _, p := range sorted {
		if !p.Enabled {
			continue
		}

		re, err := getRegexp(p.Regex)
		if err != nil {
			log.Printf("invalid regex in pattern %q (id=%d): %v", p.Name, p.ID, err)
			continue
		}

		matches := re.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		// Named groups ((?<filename>…)) are the primary mapping; a legacy
		// positional field_mapping is only consulted when present.
		var fm fieldMapping
		if p.FieldMapping != "" {
			if err := json.Unmarshal([]byte(p.FieldMapping), &fm); err != nil {
				log.Printf("invalid field_mapping in pattern %q (id=%d): %v", p.Name, p.ID, err)
				continue
			}
		}
		for i, name := range re.SubexpNames() {
			switch name {
			case "pack_number":
				fm.PackNumber = i
			case "downloads_count":
				fm.DownloadsCount = i
			case "filesize":
				fm.Filesize = i
			case "filename":
				fm.Filename = i
			case "bot_nick":
				fm.BotNick = i
			}
		}

		result := &ParsedResult{}

		if fm.PackNumber > 0 && fm.PackNumber < len(matches) {
			if n, err := strconv.Atoi(matches[fm.PackNumber]); err == nil {
				result.PackNumber = &n
			}
		}
		if fm.Filename > 0 && fm.Filename < len(matches) {
			s := strings.TrimSpace(matches[fm.Filename])
			result.Filename = &s
		}
		if fm.Filesize > 0 && fm.Filesize < len(matches) {
			s := strings.TrimSpace(matches[fm.Filesize])
			result.Filesize = &s
		}
		if fm.DownloadsCount > 0 && fm.DownloadsCount < len(matches) {
			if n, err := strconv.Atoi(matches[fm.DownloadsCount]); err == nil {
				result.DownloadsCount = &n
			}
		}
		if fm.BotNick > 0 && fm.BotNick < len(matches) {
			s := strings.TrimSpace(matches[fm.BotNick])
			result.BotNick = &s
		}

		return result, p.ID, nil
	}

	return nil, 0, ErrNoMatch
}
