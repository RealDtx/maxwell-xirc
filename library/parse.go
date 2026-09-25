package library

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// kindFields lists, for each kind, which fields ParseFields may set — used
// by GET /api/library/kinds so the UI can hint path-template placeholders.
var kindFields = map[string][]string{
	"series":   {"title", "year", "season", "episode"},
	"show":     {"title", "year"},
	"movie":    {"title", "year"},
	"music":    {"artist", "album", "year"},
	"magazine": {"title", "year"},
	"ebook":    {"author", "title"},
	"game":     {"title", "platform"},
	"software": {"title", "version"},
}

// KindFields returns a copy of the field names each kind's parser can produce.
func KindFields() map[string][]string {
	out := make(map[string][]string, len(kindFields))
	for k, v := range kindFields {
		cp := make([]string, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

var (
	yearRe = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)

	// seasonMarkerRe finds a season (and optional episode) marker in a
	// filename: "S04E10", "S04", "Season 4", "Staffel 4". Mirrors
	// routing.seasonInFilename/seasonDirName but also captures the episode.
	// ponytail: duplicated rather than exporting routing's private regexes
	// for two fields library doesn't otherwise need from that package.
	seasonMarkerRe = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(s|season|staffel)[ ._-]?(\d{1,3})(?:e(\d{1,3}))?`)

	dashSeparatedRe = regexp.MustCompile(`^([^-]+?)\s+-\s+(.+)$`)

	gamePlatformRe = regexp.MustCompile(`(?i)\b(NSW|PS4|PS5|XBOX|SWITCH|PC)\b`)
	gameGroupRe    = regexp.MustCompile(`(?i)-(CODEX|RUNE|TENOKE|FLT|SKIDROW|PLAZA|DOGE|RAZOR1911|GOG|FitGirl|DODI|ElAmigos)\b`)

	softwareVersionRe = regexp.MustCompile(`(?i)\bv?(\d+(?:\.\d+)+)\b`)
	softwareTagRe     = regexp.MustCompile(`(?i)\b(x64|x86|win|macos|linux|portable)\b`)

	magazineCutRe = regexp.MustCompile(`(?i)\b(nr\.?|ausgabe|issue|magazin\w*)\b`)
)

// ParseSeasonEpisode finds the season/episode marker in s (already
// dots/underscores-to-spaces normalized or raw — the separator class
// includes both). matchStart is the index of the marker (including any
// leading separator), or -1 when none was found.
func ParseSeasonEpisode(s string) (season, episode int, hasSeason, hasEpisode bool, matchStart int) {
	loc := seasonMarkerRe.FindStringSubmatchIndex(s)
	if loc == nil {
		return 0, 0, false, false, -1
	}
	matchStart = loc[0]
	if loc[4] != -1 {
		season, _ = strconv.Atoi(s[loc[4]:loc[5]])
		hasSeason = true
	}
	if loc[6] != -1 {
		episode, _ = strconv.Atoi(s[loc[6]:loc[7]])
		hasEpisode = true
	}
	return
}

// ParseFields extracts the fields for cat.Kind out of filename. Unknown
// kinds return an empty map.
func ParseFields(cat Category, filename string) map[string]string {
	switch cat.Kind {
	case "series":
		return parseSeries(filename)
	case "movie", "show":
		return parseTitleYear(filename)
	case "music":
		return parseMusic(filename)
	case "magazine":
		return parseMagazine(filename)
	case "ebook":
		return parseEbook(filename)
	case "game":
		return parseGame(filename)
	case "software":
		return parseSoftware(filename)
	default:
		return map[string]string{}
	}
}

func stripExt(filename string) string {
	return strings.TrimSuffix(filename, filepath.Ext(filename))
}

func dotsToSpaces(s string) string {
	s = strings.NewReplacer(".", " ", "_", " ").Replace(s)
	return collapseSpaces(s)
}

func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func removeYearToken(s string) string {
	return collapseSpaces(yearRe.ReplaceAllString(s, " "))
}

func parseSeries(filename string) map[string]string {
	norm := dotsToSpaces(stripExt(filename))
	fields := map[string]string{}
	season, episode, hasSeason, hasEpisode, cut := ParseSeasonEpisode(norm)
	title := norm
	if cut >= 0 {
		title = norm[:cut]
	}
	fields["title"] = strings.TrimSpace(removeYearToken(title))
	if hasSeason {
		fields["season"] = strconv.Itoa(season)
	}
	if hasEpisode {
		fields["episode"] = strconv.Itoa(episode)
	}
	return fields
}

// parseTitleYear handles both movie and show: title is the text before the
// first 19xx/20xx year token.
func parseTitleYear(filename string) map[string]string {
	norm := dotsToSpaces(stripExt(filename))
	fields := map[string]string{}
	title := norm
	if loc := yearRe.FindStringIndex(norm); loc != nil {
		title = norm[:loc[0]]
		fields["year"] = norm[loc[0]:loc[1]]
	}
	fields["title"] = strings.TrimSpace(title)
	return fields
}

// parseMusic handles two forms: "Artist - Title" (single track, no album)
// and the hyphen-joined scene form "Artist-Album-TAGS-YEAR-GROUP".
func parseMusic(filename string) map[string]string {
	base := stripExt(filename)
	fields := map[string]string{}
	if m := dashSeparatedRe.FindStringSubmatch(base); m != nil {
		fields["artist"] = strings.TrimSpace(dotsToSpaces(m[1]))
		return fields
	}
	norm := strings.ReplaceAll(base, "_", " ")
	parts := strings.Split(norm, "-")
	if len(parts) >= 2 {
		fields["artist"] = strings.TrimSpace(parts[0])
		fields["album"] = strings.TrimSpace(parts[1])
	}
	if y := yearRe.FindString(base); y != "" {
		fields["year"] = y
	}
	return fields
}

func parseMagazine(filename string) map[string]string {
	norm := dotsToSpaces(stripExt(filename))
	fields := map[string]string{}
	title := norm
	if loc := magazineCutRe.FindStringIndex(norm); loc != nil {
		title = norm[:loc[0]]
	}
	fields["title"] = strings.TrimSpace(title)
	if y := yearRe.FindString(norm); y != "" {
		fields["year"] = y
	}
	return fields
}

func parseEbook(filename string) map[string]string {
	base := stripExt(filename)
	fields := map[string]string{}
	if m := dashSeparatedRe.FindStringSubmatch(base); m != nil {
		fields["author"] = strings.TrimSpace(dotsToSpaces(m[1]))
		fields["title"] = strings.TrimSpace(dotsToSpaces(m[2]))
		return fields
	}
	fields["title"] = strings.TrimSpace(dotsToSpaces(base))
	return fields
}

func parseGame(filename string) map[string]string {
	base := stripExt(filename)
	norm := dotsToSpaces(base)
	title := norm
	if loc := gameGroupRe.FindStringIndex(base); loc != nil {
		title = dotsToSpaces(base[:loc[0]])
	} else if loc := yearRe.FindStringIndex(norm); loc != nil {
		title = norm[:loc[0]]
	}
	fields := map[string]string{"title": strings.TrimSpace(title)}
	if pm := gamePlatformRe.FindString(norm); pm != "" {
		fields["platform"] = strings.ToUpper(pm)
	}
	return fields
}

func parseSoftware(filename string) map[string]string {
	base := stripExt(filename)
	title := dotsToSpaces(base)
	fields := map[string]string{}
	if loc := softwareVersionRe.FindStringSubmatchIndex(base); loc != nil {
		fields["version"] = base[loc[2]:loc[3]]
		title = dotsToSpaces(base[:loc[0]])
	} else if loc := softwareTagRe.FindStringIndex(base); loc != nil {
		title = dotsToSpaces(base[:loc[0]])
	}
	fields["title"] = strings.TrimSpace(title)
	return fields
}
