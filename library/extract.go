package library

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
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

// ArchiveSet describes a (possibly multi-volume) archive: whether the given
// name is the volume to extract from, the name without archive/volume
// suffixes, and every volume of the set present in the directory.
type ArchiveSet struct {
	First   bool
	Stem    string
	Volumes []string
}

var (
	rePartRar = regexp.MustCompile(`(?i)^(.+)\.part(\d+)\.rar$`)
	reOldRar  = regexp.MustCompile(`(?i)^(.+)\.(rar|r\d{2,})$`)
	reZip     = regexp.MustCompile(`(?i)^(.+)\.(zip|z\d{2,})$`)
	re7zSplit = regexp.MustCompile(`(?i)^(.+)\.7z\.(\d{3})$`)
)

// ArchiveVolumes classifies name (a file in dir) as an archive volume.
// ok is false when name isn't an archive at all.
func ArchiveVolumes(dir, name string) (ArchiveSet, bool) {
	lower := strings.ToLower(name)
	var stem string
	var first bool
	var member *regexp.Regexp // matches the set's volumes; group 1 = stem
	switch {
	case rePartRar.MatchString(name):
		m := rePartRar.FindStringSubmatch(name)
		stem, member = m[1], rePartRar
		n := strings.TrimLeft(m[2], "0")
		first = n == "1"
	case re7zSplit.MatchString(name):
		m := re7zSplit.FindStringSubmatch(name)
		stem, member, first = m[1], re7zSplit, m[2] == "001"
	case reOldRar.MatchString(name):
		m := reOldRar.FindStringSubmatch(name)
		stem, member, first = m[1], reOldRar, strings.EqualFold(m[2], "rar")
	case reZip.MatchString(name):
		m := reZip.FindStringSubmatch(name)
		stem, member, first = m[1], reZip, strings.EqualFold(m[2], "zip")
	case strings.HasSuffix(lower, ".7z"):
		return ArchiveSet{First: true, Stem: name[:len(name)-3], Volumes: []string{name}}, true
	case routing.IsArchive(name): // tar family
		for _, ext := range []string{".tar.gz", ".tar.bz2", ".tar.xz", ".tar.zst", ".tgz", ".tbz2", ".txz", ".tar"} {
			if strings.HasSuffix(lower, ext) {
				return ArchiveSet{First: true, Stem: name[:len(name)-len(ext)], Volumes: []string{name}}, true
			}
		}
		return ArchiveSet{}, false
	default:
		return ArchiveSet{}, false
	}
	set := ArchiveSet{First: first, Stem: stem}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if m := member.FindStringSubmatch(e.Name()); m != nil && m[1] == stem && !e.IsDir() {
			// A part-rar name also matches reOldRar ("X.part01" + ".rar");
			// keep old-style sets free of part-rar files.
			if member == reOldRar && rePartRar.MatchString(e.Name()) {
				continue
			}
			set.Volumes = append(set.Volumes, e.Name())
		}
	}
	sort.Strings(set.Volumes)
	return set, true
}

// ExtractAny unpacks any supported archive (zip/rar/7z incl. split 7z via
// Extract/7z, tar family via routing.Extract) into a staging dir inside
// destDir. Shared by the download engine and the file manager.
func ExtractAny(archivePath, destDir string) ([]string, string, error) {
	if re7zSplit.MatchString(filepath.Base(archivePath)) {
		return extractWithTool(archivePath, destDir)
	}
	if IsArchive(archivePath) {
		return Extract(archivePath, destDir)
	}
	return routing.Extract(archivePath, destDir)
}
