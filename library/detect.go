package library

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/RealDtx/maxwell-irc/routing"
)

// kindDefault is the factory setting for one kind, used to seed a category
// when categories.yaml doesn't exist yet (Detect) — dir/season_dir/path may
// then be overridden by what's actually found on disk.
type kindDefault struct {
	Name          string
	DirAliases    []string
	Path          string
	SeasonDir     string
	Priority      int
	Extensions    []string
	Patterns      []string
	AutoExtract   bool
	DeleteArchive bool
}

// kindOrder is also priority order (highest first) for Detect's output.
var kindOrder = []string{"series", "show", "movie", "music", "magazine", "ebook", "game", "software"}

var kindDefaults = map[string]kindDefault{
	"series": {
		Name: "Series", DirAliases: []string{"Series", "TV", "TV Shows", "Serien"},
		Path: "{title}/{season_dir}", SeasonDir: "S{season:02}", Priority: 100,
		Extensions: []string{"mkv", "mp4", "avi", "m4v", "ts", "wmv", "webm"},
		Patterns: []string{
			`(?i)[^a-z0-9]s\d{1,3}e\d{1,3}`,
			`(?i)[^a-z0-9](s\d{1,3}|season[ ._-]?\d+|staffel[ ._-]?\d+)[^a-z0-9]`,
		},
	},
	"show": {
		Name: "Show", DirAliases: []string{"Show", "Shows", "Concerts", "Comedy"},
		Path: "", Priority: 90,
		Extensions: []string{"mkv", "mp4", "avi", "m4v", "ts", "wmv", "webm"},
		Patterns:   []string{`(?i)(live\.at|live\.in|concert|glastonbury|wacken|festival|stand.?up|comedy.special)`},
	},
	"movie": {
		Name: "Movies", DirAliases: []string{"Movies", "Filme", "Films"},
		Path: "", Priority: 50,
		Extensions: []string{"mkv", "mp4", "avi", "m4v", "ts", "wmv", "webm"},
	},
	"music": {
		Name: "Music", DirAliases: []string{"Music", "Musik"},
		Path: "{artist}/{album} ({year})", Priority: 80,
		Extensions:    []string{"mp3", "flac", "m4a", "ogg", "opus", "wav", "aac", "zip", "rar", "7z"},
		Patterns:      []string{`(?i)(flac|mp3|320|v0|web|cd|vinyl)`, `(?i)(19|20)\d{2}`},
		AutoExtract:   true,
		DeleteArchive: true,
	},
	"magazine": {
		Name: "Magazines", DirAliases: []string{"Magazines", "Zeitschriften"},
		Path: "{title}/{year}", Priority: 70,
		Extensions: []string{"pdf", "epub", "cbz"},
		Patterns:   []string{`(?i)(magazin(e)?|nr\.?\d|ausgabe|issue|ebook-mg)`},
	},
	"ebook": {
		Name: "Books", DirAliases: []string{"Books", "eBooks", "Bücher"},
		Path: "{author}", Priority: 60,
		Extensions: []string{"pdf", "epub", "mobi", "azw3", "cbz", "cbr"},
	},
	"game": {
		Name: "Games", DirAliases: []string{"Games", "Spiele"},
		Path: "{platform}/{title}", Priority: 40,
		Extensions: []string{"iso", "zip", "rar", "7z", "exe", "nsp", "xci"},
		Patterns: []string{
			`(?i)-(CODEX|RUNE|TENOKE|FLT|SKIDROW|PLAZA|DOGE|RAZOR1911|GOG|FitGirl|DODI|ElAmigos)\b`,
			`(?i)\b(NSW|PS4|PS5|XBOX)\b`,
		},
	},
	// Detected after game (lower priority): a scene/repack or platform tag
	// claims a zip/iso/exe for "game" first, so software only sees the rest.
	"software": {
		Name: "Software", DirAliases: []string{"Software", "Programs", "Apps"},
		Path: "{title}", Priority: 30,
		Extensions: []string{"exe", "msi", "dmg", "pkg", "deb", "rpm", "appimage", "zip"},
		Patterns:   []string{`(?i)\b(x64|x86|win|macos|linux|portable)\b`, `(?i)\bv?\d+\.\d+`},
	},
}

// Detect builds a default config from what's actually on disk under
// mediaRoot: which alias each category's folder matches (falling back to
// the English name when none exists yet), the series season-folder style in
// use, and whether movies sit flat or in "{title} ({year})" folders.
func Detect(mediaRoot string) Config {
	cfg := Config{AutoOrganize: true, SearchDepth: routing.DefaultSubdirDepth, MediaRoot: mediaRoot}
	for _, kind := range kindOrder {
		kd := kindDefaults[kind]
		dirName := findDirAlias(mediaRoot, kd.DirAliases)
		cat := Category{
			ID: kind, Kind: kind, Name: kd.Name, Enabled: true,
			Dir: dirName, Path: kd.Path, SeasonDir: kd.SeasonDir,
			CreateFolders: true, AutoExtract: kd.AutoExtract, DeleteArchive: kd.DeleteArchive,
			Priority:   kd.Priority,
			Extensions: append([]string(nil), kd.Extensions...),
			Patterns:   append([]string(nil), kd.Patterns...),
		}
		fullDir := filepath.Join(mediaRoot, dirName)
		switch kind {
		case "series":
			if style := detectSeasonDirStyle(fullDir); style != "" {
				cat.SeasonDir = style
			}
		case "movie":
			if !detectMovieFlat(fullDir) {
				cat.Path = "{title} ({year})"
			}
		}
		cfg.Categories = append(cfg.Categories, cat)
	}
	return cfg
}

// findDirAlias returns the actual on-disk name of the first alias that
// exists as a top-level directory under mediaRoot (case-insensitive), or
// aliases[0] (the English default) when none is found.
func findDirAlias(mediaRoot string, aliases []string) string {
	entries, err := os.ReadDir(mediaRoot)
	if err == nil {
		byLower := make(map[string]string, len(entries))
		for _, e := range entries {
			if e.IsDir() {
				byLower[strings.ToLower(e.Name())] = e.Name()
			}
		}
		for _, a := range aliases {
			if name, ok := byLower[strings.ToLower(a)]; ok {
				return name
			}
		}
	}
	return aliases[0]
}

// detectSeasonDirStyle scans every show's season folders under seriesDir and
// returns the most common style (e.g. "S{season:02}"), including its case.
func detectSeasonDirStyle(seriesDir string) string {
	counts := map[string]int{}
	for _, show := range routing.ListSubdirs(seriesDir, 1) {
		for _, season := range routing.ListSubdirs(show, 1) {
			if style := seasonDirStyle(filepath.Base(season)); style != "" {
				counts[style]++
			}
		}
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic tie-break
	best, bestCount := "", 0
	for _, k := range keys {
		if counts[k] > bestCount {
			best, bestCount = k, counts[k]
		}
	}
	return best
}

func seasonDirStyle(name string) string {
	m := seasonDirRe.FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	prefix, digits := m[1], m[2]
	if len(digits) > 1 && digits[0] == '0' {
		return prefix + "{season:0" + strconv.Itoa(len(digits)) + "}"
	}
	return prefix + "{season}"
}

// detectMovieFlat reports whether more than half the movie files sit
// directly in moviesDir rather than in a subfolder (collection or
// "{title} ({year})").
func detectMovieFlat(moviesDir string) bool {
	entries, err := os.ReadDir(moviesDir)
	if err != nil {
		return true
	}
	flat, total := 0, 0
	for _, e := range entries {
		if e.IsDir() {
			sub, err := os.ReadDir(filepath.Join(moviesDir, e.Name()))
			if err != nil {
				continue
			}
			for _, se := range sub {
				if !se.IsDir() {
					total++
				}
			}
			continue
		}
		flat++
		total++
	}
	if total == 0 {
		return true
	}
	return flat*2 > total
}
