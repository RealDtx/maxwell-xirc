package routing

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

var archiveExtensions = []string{
	".tar", ".tar.gz", ".tgz", ".tar.bz2", ".tbz2",
	".tar.xz", ".txz", ".tar.zst",
}

// IsArchive reports whether filename is a supported tar-based archive.
func IsArchive(filename string) bool {
	lower := strings.ToLower(filename)
	for _, ext := range archiveExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// Extract unpacks archivePath into destDir and returns the paths of all
// extracted regular files. Extraction is done in-place — no temporary staging
// directory is created. Requires tar(1).
func Extract(archivePath, destDir string) ([]string, error) {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("creating extract dir: %w", err)
	}
	// -x extract, -v list extracted names, -f read from file, -C change to destDir
	out, err := exec.Command("tar", "-xvf", archivePath, "-C", destDir).Output()
	if err != nil {
		return nil, fmt.Errorf("tar: %w", err)
	}

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "./"))
		if line == "" || strings.HasSuffix(line, "/") {
			continue // skip blank lines and directory entries
		}
		fullPath := filepath.Join(destDir, line)
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			files = append(files, fullPath)
		}
	}
	return files, nil
}

// RemoveEmptyDirs removes empty subdirectories under root (but not root itself),
// working deepest-first so that parent directories become empty once their
// children are removed.
func RemoveEmptyDirs(root string) {
	var dirs []string
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && path != root {
			dirs = append(dirs, path)
		}
		return nil
	})
	// Sort deepest first so children are removed before their parents are checked.
	sort.Slice(dirs, func(i, j int) bool {
		return strings.Count(dirs[i], string(filepath.Separator)) >
			strings.Count(dirs[j], string(filepath.Separator))
	})
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err == nil && len(entries) == 0 {
			os.Remove(dir)
		}
	}
}
