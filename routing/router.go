package routing

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

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
// destination root are listed as download targets and allowed for isValidTargetDir.
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
