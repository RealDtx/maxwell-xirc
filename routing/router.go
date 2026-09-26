package routing

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/RealDtx/maxwell-irc/db"
)

// MatchRule returns the destination directory for a filename based on routing rules.
// Rules are tried in priority order (highest first). Returns empty string if no match.
func MatchRule(filename string, rules []db.FileRoutingRule) string {
	sorted := make([]db.FileRoutingRule, len(rules))
	copy(sorted, rules)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority > sorted[j].Priority
	})

	for _, rule := range sorted {
		if !rule.Enabled {
			continue
		}
		matched, _ := filepath.Match(rule.Pattern, filename)
		if matched {
			return rule.DestinationDir
		}
	}
	return ""
}

// MoveFile moves a file from srcPath to destDir, creating destDir if needed.
// Returns the final path. Tries rename first (fast, same filesystem),
// falls back to copy+delete for cross-filesystem moves.
// On a cross-filesystem move where the copy succeeds but source removal fails,
// the returned path will be non-empty (the destination file is valid) alongside
// the non-nil error. Callers should treat the destination as valid in this case.
func MoveFile(srcPath, destDir string) (string, error) {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("creating destination dir: %w", err)
	}

	filename := filepath.Base(srcPath)
	destPath := filepath.Join(destDir, filename)

	// Try atomic rename first
	err := os.Rename(srcPath, destPath)
	if err == nil {
		return destPath, nil
	}

	// Only a cross-device rename (EXDEV) is worth a copy; anything else
	// (permission denied, missing dir) would fail the copy the same way.
	if !errors.Is(err, syscall.EXDEV) {
		return "", err
	}
	size := int64(-1)
	if info, statErr := os.Stat(srcPath); statErr == nil {
		size = info.Size()
	}
	log.Printf("rename %q -> %q crosses filesystems, falling back to copy (size=%d bytes)", srcPath, destPath, size)

	// Fall back to copy + delete (cross-filesystem)
	if err := copyFile(srcPath, destPath); err != nil {
		return "", err
	}
	if err := os.Remove(srcPath); err != nil {
		return destPath, fmt.Errorf("removing source after copy: %w", err)
	}
	return destPath, nil
}

// DefaultSubdirDepth and MaxSubdirDepth bound how many folder levels below a
// routing destination are listed as targets and searched by InferSubdir.
const (
	DefaultSubdirDepth = 3
	MaxSubdirDepth     = 5
)

// ListSubdirs returns every non-hidden directory below root down to depth
// levels, breadth-first (shallower dirs first). root itself is not included.
func ListSubdirs(root string, depth int) []string {
	var out []string
	level := []string{root}
	for d := 0; d < depth && len(level) > 0; d++ {
		var next []string
		for _, dir := range level {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
					next = append(next, filepath.Join(dir, e.Name()))
				}
			}
		}
		out = append(out, next...)
		level = next
	}
	return out
}

var (
	// Season number in a release name: "S04E10", "S04.", "Season 4", "Staffel 4".
	seasonInFilename = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:s|season|staffel)[ ._-]?(\d{1,3})(?:e\d|[^a-z0-9]|$)`)
	// Season folder names: "S04", "S4", "Season 4", "Season.04", "Staffel 4".
	seasonDirName = regexp.MustCompile(`(?i)^(?:s|season|staffel)[ ._-]*(\d{1,3})$`)
)

// InferSubdir picks the folder below ruleDir (up to depth levels) whose name
// best matches filename, so "Scrubs.2026.S01E02.mkv" lands in an existing
// ".../Series/Scrubs" folder, and further in its "Season 1"/"S01" subfolder
// when one exists. Names are normalized (lowercased, [a-z0-9] only); the
// folder whose normalized name is the longest prefix of the normalized
// filename wins (shallower wins ties), with a minimum length of 3 to avoid
// short false positives. Falls back to ruleDir when nothing matches.
// ponytail: prefix match only; fuzzy/series parsing if prefixes miss too often
func InferSubdir(ruleDir, filename string, depth int) string {
	normFilename := normalizeSubdirName(filename)
	best := ruleDir
	bestLen := 2 // matched name must be longer than this (min length 3)
	for _, dir := range ListSubdirs(ruleDir, depth) {
		normName := normalizeSubdirName(filepath.Base(dir))
		if len(normName) > bestLen && strings.HasPrefix(normFilename, normName) {
			best = dir
			bestLen = len(normName)
		}
	}
	if best == ruleDir {
		return ruleDir // no show folder, so don't guess a season folder either
	}
	if season := seasonSubdir(best, filename); season != "" {
		return season
	}
	return best
}

// seasonSubdir returns the existing season folder inside showDir matching the
// season number in filename, or "" if the filename has none or no folder matches.
func seasonSubdir(showDir, filename string) string {
	m := seasonInFilename.FindStringSubmatch(filename)
	if m == nil {
		return ""
	}
	want, _ := strconv.Atoi(m[1])
	for _, dir := range ListSubdirs(showDir, 1) {
		if dm := seasonDirName.FindStringSubmatch(filepath.Base(dir)); dm != nil {
			if n, _ := strconv.Atoi(dm[1]); n == want {
				return dir
			}
		}
	}
	return ""
}

func normalizeSubdirName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst) // best-effort cleanup of partial write
		return err
	}
	return out.Close()
}
