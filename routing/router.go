package routing

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/maxwell-xirc/xirc/db"
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

	// Fall back to copy + delete (cross-filesystem)
	if err := copyFile(srcPath, destPath); err != nil {
		return "", err
	}
	if err := os.Remove(srcPath); err != nil {
		return destPath, fmt.Errorf("removing source after copy: %w", err)
	}
	return destPath, nil
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
		return err
	}
	return out.Close()
}
