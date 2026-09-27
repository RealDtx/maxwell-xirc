package routing

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var archiveExtensions = []string{
	".tar", ".tar.gz", ".tgz", ".tar.bz2", ".tbz2",
	".tar.xz", ".txz", ".tar.zst",
	".zip", ".rar", ".7z",
}

// IsArchive reports whether filename is a supported archive: the tar family
// (extracted by Extract, below, via tar(1)) or zip/rar/7z (extracted by the
// library package via archive/zip or the 7z/unrar binaries).
func IsArchive(filename string) bool {
	lower := strings.ToLower(filename)
	for _, ext := range archiveExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// Extract unpacks archivePath into a fresh staging directory inside destDir
// (so it lives on the same filesystem) and returns the regular files it
// contains plus the staging directory itself. On failure the staging
// directory is removed, leaving the archive untouched. Requires tar(1).
func Extract(archivePath, destDir string) ([]string, string, error) {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, "", fmt.Errorf("creating extract dir: %w", err)
	}
	tempDir, err := os.MkdirTemp(destDir, ".extract_")
	if err != nil {
		return nil, "", fmt.Errorf("creating temp extract dir: %w", err)
	}
	if out, err := exec.Command("tar", "-xf", archivePath, "-C", tempDir).CombinedOutput(); err != nil {
		os.RemoveAll(tempDir) // clean up any partial extraction
		return nil, "", fmt.Errorf("tar: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return RegularFiles(tempDir), tempDir, nil
}

// RegularFiles lists every regular file under dir (symlinks, devices and
// other special entries are skipped). The list comes from the filesystem,
// never from a tool's text output, so names in any encoding survive.
func RegularFiles(dir string) []string {
	var files []string
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			files = append(files, path)
		}
		return nil
	})
	return files
}
