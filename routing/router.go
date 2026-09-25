package routing

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

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

	// Log why rename failed (e.g. "invalid cross-device link" = EXDEV, temp
	// and destination are on different mounts) and the file size, so the
	// server log tells us whether the copy fallback is expected.
	size := int64(-1)
	if info, statErr := os.Stat(srcPath); statErr == nil {
		size = info.Size()
	}
	log.Printf("rename %q -> %q failed (%v), falling back to copy (size=%d bytes)", srcPath, destPath, err, size)

	// Fall back to copy + delete (cross-filesystem)
	if err := copyFile(srcPath, destPath); err != nil {
		return "", err
	}
	if err := os.Remove(srcPath); err != nil {
		return destPath, fmt.Errorf("removing source after copy: %w", err)
	}
	return destPath, nil
}

// InferSubdir picks the immediate subdirectory of ruleDir whose name best
// matches filename, so e.g. "Scrubs.2026.S01E02.mkv" lands in an existing
// "Scrubs" subfolder instead of directly in ruleDir. Both names are
// normalized (lowercased, [a-z0-9] only) and the subdir whose normalized
// name is the longest prefix of the normalized filename wins; a minimum
// normalized length of 3 avoids short false positives (e.g. "S1"). Hidden
// directories are skipped. Falls back to ruleDir when the dir can't be read
// or no subdir qualifies.
// ponytail: prefix match only; fuzzy/series parsing if prefixes miss too often
func InferSubdir(ruleDir, filename string) string {
	entries, err := os.ReadDir(ruleDir)
	if err != nil {
		return ruleDir
	}
	normFilename := normalizeSubdirName(filename)
	best := ruleDir
	bestLen := 2 // matched name must be longer than this (min length 3)
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		normName := normalizeSubdirName(e.Name())
		if len(normName) > bestLen && strings.HasPrefix(normFilename, normName) {
			best = filepath.Join(ruleDir, e.Name())
			bestLen = len(normName)
		}
	}
	return best
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
