package library

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/RealDtx/maxwell-irc/routing"
)

// musicAudioExts are music's "always matches" extensions — the archive
// extensions (zip/rar/7z) additionally need every pattern to hit (see
// matchesCategory), since a bare "any pattern matches" OR would also let
// unrelated game/software archives through.
var musicAudioExts = map[string]bool{
	"mp3": true, "flac": true, "m4a": true, "ogg": true, "opus": true, "wav": true, "aac": true,
}

// movieVideoExts are movie's "always matches" extensions, mirroring
// musicAudioExts: the archive extensions (tar/zip/rar/7z) it shares with
// game/software also need every pattern to hit (a year, by default) so an
// unrelated software/game archive doesn't get claimed as a movie.
var movieVideoExts = map[string]bool{
	"mkv": true, "mp4": true, "avi": true, "m4v": true, "ts": true, "wmv": true, "webm": true,
}

// nativeExtsByKind maps a kind to the extensions that match it unconditionally
// (Patterns skipped); every other extension in the kind's Extensions list is
// an archive format shared with other kinds, so it additionally needs every
// pattern to hit. See matchesCategory.
var nativeExtsByKind = map[string]map[string]bool{
	"music": musicAudioExts,
	"movie": movieVideoExts,
}

func extInList(exts []string, ext string) bool {
	if len(exts) == 0 {
		return true
	}
	for _, e := range exts {
		if strings.EqualFold(strings.TrimPrefix(e, "."), ext) {
			return true
		}
	}
	return false
}

func matchesPatterns(cat Category, filename string) bool {
	if len(cat.Patterns) == 0 {
		return true
	}
	for _, p := range cat.Patterns {
		if re, err := regexp.Compile(p); err == nil && re.MatchString(filename) {
			return true
		}
	}
	return false
}

// matchesCategory applies the B1 matching rule (extension in Extensions AND
// a pattern matches) with one special case, per nativeExtsByKind: a kind's
// native extensions skip the pattern check entirely, while an archive
// extension it shares with other kinds (zip/rar/7z/tar) requires ALL
// patterns to hit, not just one — otherwise any such archive would need its
// own pattern to rule the kind out.
func matchesCategory(cat Category, filename string) bool {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	if !extInList(cat.Extensions, ext) {
		return false
	}
	if native, ok := nativeExtsByKind[cat.Kind]; ok {
		if native[ext] {
			return true
		}
		if len(cat.Patterns) == 0 {
			return false
		}
		for _, p := range cat.Patterns {
			re, err := regexp.Compile(p)
			if err != nil || !re.MatchString(filename) {
				return false
			}
		}
		return true
	}
	return matchesPatterns(cat, filename)
}

func matchCategory(cfg *Config, filename string) *Category {
	cats := make([]Category, len(cfg.Categories))
	copy(cats, cfg.Categories)
	sort.SliceStable(cats, func(i, j int) bool { return cats[i].Priority > cats[j].Priority })
	for i := range cats {
		c := &cats[i]
		if !c.Enabled {
			continue
		}
		if matchesCategory(*c, filename) {
			return c
		}
	}
	return nil
}

// Resolve picks the highest-priority enabled category matching filename,
// parses its fields, and computes (creating folders as needed, per
// Category.CreateFolders) the destination directory. ok is false when no
// category matches.
func Resolve(cfg *Config, filename string) (dir string, cat *Category, fields map[string]string, ok bool) {
	return resolve(cfg, filename, false)
}

// PreviewResult is one entry of POST /api/library/preview's response.
type PreviewResult struct {
	Filename string            `json:"filename"`
	Category string            `json:"category"`
	Kind     string            `json:"kind"`
	Fields   map[string]string `json:"fields"`
	Path     string            `json:"path"`
	Exists   bool              `json:"exists"`
}

// Preview computes the same result as Resolve but never touches disk: any
// folder that would be created is only reflected in the returned path.
func Preview(cfg *Config, filename string) PreviewResult {
	dir, cat, fields, ok := resolve(cfg, filename, true)
	res := PreviewResult{Filename: filename, Fields: fields}
	if !ok {
		res.Fields = map[string]string{}
		return res
	}
	res.Category = cat.ID
	res.Kind = cat.Kind
	res.Path = dir
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		res.Exists = true
	}
	return res
}

func resolve(cfg *Config, filename string, dryRun bool) (dir string, cat *Category, fields map[string]string, ok bool) {
	c := matchCategory(cfg, filename)
	if c == nil {
		return "", nil, nil, false
	}
	fields = ParseFields(*c, filename)
	categoryDir := c.Dir
	if !filepath.IsAbs(categoryDir) {
		categoryDir = filepath.Join(cfg.MediaRoot, categoryDir)
	}
	return resolveDirForKind(c, categoryDir, fields, filename, dryRun), c, fields, true
}

// resolveSegment finds an existing sibling of parentDir matching name (exact
// normalized match, else the longest normalized prefix match of at least 3
// chars) and returns it. If none matches: create_folders on creates one
// (skipped when dryRun) and found=true, isNew=true; create_folders off
// leaves dir at parentDir and found=false — the caller stops descending.
func resolveSegment(parentDir, name string, createFolders, dryRun bool) (dir string, found, isNew bool) {
	if best := findExistingMatch(parentDir, name); best != "" {
		return filepath.Join(parentDir, best), true, false
	}
	if !createFolders {
		return parentDir, false, false
	}
	full := filepath.Join(parentDir, cleanPathSegment(name))
	if !dryRun {
		os.MkdirAll(full, 0775)
	}
	return full, true, true
}

// findExistingMatch is B3's "reusing existing folders" rule: normalize
// (lowercase, & -> and, drop a leading "the", drop a trailing "(year)",
// keep [a-z0-9]), exact match wins, else the longest prefix match >= 3 chars.
func findExistingMatch(parentDir, target string) string {
	normTarget := normalizeTitle(target)
	if normTarget == "" {
		return ""
	}
	best := ""
	bestLen := 2
	for _, d := range routing.ListSubdirs(parentDir, 1) {
		name := filepath.Base(d)
		norm := normalizeTitle(name)
		if norm == "" {
			continue
		}
		if norm == normTarget {
			return name
		}
		if len(norm) > bestLen && strings.HasPrefix(normTarget, norm) {
			best = name
			bestLen = len(norm)
		}
	}
	return best
}

var trailingYearRe = regexp.MustCompile(`\(\d{4}\)\s*$`)

func normalizeTitle(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "&", "and")
	s = trailingYearRe.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "the ")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// cleanPathSegment keeps a computed folder name safe: no path separators,
// "..", or control characters.
func cleanPathSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '/' || r == filepath.Separator || unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(strings.ReplaceAll(b.String(), "..", ""))
}

func resolveDirForKind(cat *Category, categoryDir string, fields map[string]string, filename string, dryRun bool) string {
	switch cat.Kind {
	case "series":
		return resolveSeriesDir(cat, categoryDir, fields, filename, dryRun)
	case "movie":
		return resolveMovieDir(cat, categoryDir, fields, dryRun)
	default:
		return resolveTemplateDir(cat, categoryDir, fields, dryRun)
	}
}

// resolveTemplateDir walks cat.Path's segments under categoryDir, reusing or
// creating each in turn (resolveSegment), stopping at the deepest existing
// folder when create_folders is off.
func resolveTemplateDir(cat *Category, categoryDir string, fields map[string]string, dryRun bool) string {
	dir := categoryDir
	for _, seg := range renderTemplateSegments(cat.Path, fields) {
		next, found, _ := resolveSegment(dir, seg, cat.CreateFolders, dryRun)
		dir = next
		if !found {
			break
		}
	}
	return dir
}

// resolveMovieDir reuses an existing collection folder (e.g. "James Bond")
// whose normalized name is a prefix of the title, before falling back to
// the path template (flat by default).
func resolveMovieDir(cat *Category, categoryDir string, fields map[string]string, dryRun bool) string {
	if title := fields["title"]; title != "" {
		if match := findExistingMatch(categoryDir, title); match != "" {
			return filepath.Join(categoryDir, match)
		}
	}
	if cat.Path == "" {
		return categoryDir
	}
	return resolveTemplateDir(cat, categoryDir, fields, dryRun)
}

var seasonDirRe = regexp.MustCompile(`(?i)^(s|season|staffel)[ ._-]*(\d{1,3})$`)

type seasonEntry struct {
	name string
	num  int
}

func findSeasonDirs(dir string) []seasonEntry {
	var out []seasonEntry
	for _, d := range routing.ListSubdirs(dir, 1) {
		name := filepath.Base(d)
		if m := seasonDirRe.FindStringSubmatch(name); m != nil {
			n, _ := strconv.Atoi(m[2])
			out = append(out, seasonEntry{name: name, num: n})
		}
	}
	return out
}

// resolveSeriesDir resolves the show folder (reusing e.g. "Rick & Morty" /
// "Simpsons" via findExistingMatch), then the season folder:
//   - an existing season-folder style is followed (reuse the matching
//     number, or add a new one in that style when create_folders is on);
//   - a show folder with NO season subfolders but files directly in it
//     (Scrubs, Simpsons) stays flat, following its own layout;
//   - a brand new show folder uses cat.SeasonDir as its layout.
func resolveSeriesDir(cat *Category, categoryDir string, fields map[string]string, filename string, dryRun bool) string {
	title := fields["title"]
	if title == "" {
		return categoryDir
	}
	showDir, found, created := resolveSegment(categoryDir, title, cat.CreateFolders, dryRun)
	if !found {
		return showDir
	}
	seasonNum, hasSeason := 0, false
	if s, ok := fields["season"]; ok {
		if n, err := strconv.Atoi(s); err == nil {
			seasonNum, hasSeason = n, true
		}
	}
	existing := findSeasonDirs(showDir)
	if len(existing) > 0 {
		for _, sd := range existing {
			if hasSeason && sd.num == seasonNum {
				return filepath.Join(showDir, sd.name)
			}
		}
		if hasSeason && cat.CreateFolders {
			return createSeasonDir(cat, showDir, seasonNum, dryRun)
		}
		return showDir
	}
	// No established season-folder layout: a freshly created show dir gets
	// one in cat.SeasonDir's style; an existing flat show dir stays flat.
	if created && hasSeason && cat.CreateFolders {
		return createSeasonDir(cat, showDir, seasonNum, dryRun)
	}
	return showDir
}

func createSeasonDir(cat *Category, showDir string, seasonNum int, dryRun bool) string {
	segs := renderTemplateSegments(cat.SeasonDir, map[string]string{"season": strconv.Itoa(seasonNum)})
	if len(segs) == 0 {
		return showDir
	}
	full := filepath.Join(showDir, cleanPathSegment(segs[0]))
	if !dryRun {
		os.MkdirAll(full, 0775)
	}
	return full
}

// placeholderRe matches {field} or {field:0N} template placeholders.
var placeholderRe = regexp.MustCompile(`\{([a-zA-Z_]+)(?::(\d+))?\}`)
var parenGroupRe = regexp.MustCompile(`\([^()]*\)`)

// substitutePlaceholders replaces every {field}/{field:0N} in s with
// fields[field] (zero-padded to the given width when numeric). anyEmpty
// reports whether any placeholder substituted to "".
func substitutePlaceholders(s string, fields map[string]string) (out string, anyEmpty bool) {
	out = placeholderRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := placeholderRe.FindStringSubmatch(m)
		name, width := sub[1], sub[2]
		val := fields[name]
		if val == "" {
			anyEmpty = true
			return ""
		}
		if width != "" {
			if w, err := strconv.Atoi(width); err == nil {
				if n, err := strconv.Atoi(val); err == nil {
					return fmt0Pad(n, w)
				}
			}
		}
		return val
	})
	return out, anyEmpty
}

func fmt0Pad(n, width int) string {
	s := strconv.Itoa(n)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

// renderSegment fills one template segment. A parenthetical group whose
// only placeholder is empty is dropped entirely (so "{album} ({year})"
// with no year becomes "{album}"); an empty result after that means the
// whole segment is dropped.
func renderSegment(tmpl string, fields map[string]string) string {
	resolved := parenGroupRe.ReplaceAllStringFunc(tmpl, func(m string) string {
		inner, empty := substitutePlaceholders(m[1:len(m)-1], fields)
		if empty {
			return ""
		}
		return "(" + inner + ")"
	})
	out, _ := substitutePlaceholders(resolved, fields)
	return strings.TrimSpace(collapseSpaces(out))
}

// renderTemplateSegments splits tmpl on "/" and renders each segment,
// dropping any that end up empty.
func renderTemplateSegments(tmpl string, fields map[string]string) []string {
	if tmpl == "" {
		return nil
	}
	var out []string
	for _, seg := range strings.Split(tmpl, "/") {
		if r := renderSegment(seg, fields); r != "" {
			out = append(out, r)
		}
	}
	return out
}
