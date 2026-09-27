package library

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/RealDtx/maxwell-irc/routing"
)

// IsArchive reports whether filename is a zip/rar/7z archive — the formats
// this package extracts (routing.Extract still handles the tar family).
func IsArchive(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".zip", ".rar", ".7z":
		return true
	}
	return false
}

// Extract unpacks a zip/rar/7z archive into a staging directory inside
// destDir (so it lives on the same filesystem, same approach as
// routing.Extract) and returns the extracted regular files plus the staging
// directory.
func Extract(archivePath, destDir string) ([]string, string, error) {
	switch strings.ToLower(filepath.Ext(archivePath)) {
	case ".zip":
		return extractZip(archivePath, destDir)
	case ".rar", ".7z":
		return extractWithTool(archivePath, destDir)
	default:
		return nil, "", fmt.Errorf("unsupported archive type %q", filepath.Ext(archivePath))
	}
}

func extractZip(archivePath, destDir string) ([]string, string, error) {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, "", fmt.Errorf("creating extract dir: %w", err)
	}
	tempDir, err := os.MkdirTemp(destDir, ".extract_")
	if err != nil {
		return nil, "", fmt.Errorf("creating temp extract dir: %w", err)
	}
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		os.RemoveAll(tempDir)
		return nil, "", fmt.Errorf("opening zip: %w", err)
	}
	defer r.Close()

	var files []string
	for _, f := range r.File {
		target := filepath.Join(tempDir, f.Name)
		// zip-slip guard: reject any entry whose cleaned path would land
		// outside tempDir (e.g. "../../etc/passwd").
		if target != tempDir && !strings.HasPrefix(target, tempDir+string(filepath.Separator)) {
			continue
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			os.RemoveAll(tempDir)
			return nil, "", fmt.Errorf("creating %q: %w", filepath.Dir(target), err)
		}
		if err := extractZipEntry(f, target); err != nil {
			os.RemoveAll(tempDir)
			return nil, "", err
		}
		files = append(files, target)
	}
	return files, tempDir, nil
}

func extractZipEntry(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("reading zip entry %q: %w", f.Name, err)
	}
	defer rc.Close()

	out, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("creating %q: %w", target, err)
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return fmt.Errorf("writing %q: %w", target, err)
	}
	return out.Close()
}

// extractWithTool unpacks a rar/7z archive via the "7z" binary, falling
// back to "unrar" when 7z isn't installed. Requires one of them on PATH.
func extractWithTool(archivePath, destDir string) ([]string, string, error) {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, "", fmt.Errorf("creating extract dir: %w", err)
	}
	tempDir, err := os.MkdirTemp(destDir, ".extract_")
	if err != nil {
		return nil, "", fmt.Errorf("creating temp extract dir: %w", err)
	}

	var runErr error
	switch {
	case toolAvailable("7z"):
		runErr = exec.Command("7z", "x", "-y", "-o"+tempDir, archivePath).Run()
	case toolAvailable("unrar"):
		runErr = exec.Command("unrar", "x", "-y", archivePath, tempDir+string(filepath.Separator)).Run()
	default:
		os.RemoveAll(tempDir)
		return nil, "", fmt.Errorf("no archive tool available (7z or unrar required)")
	}
	if runErr != nil {
		os.RemoveAll(tempDir)
		return nil, "", fmt.Errorf("extracting %s: %w", filepath.Base(archivePath), runErr)
	}

	return routing.RegularFiles(tempDir), tempDir, nil
}

func toolAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
