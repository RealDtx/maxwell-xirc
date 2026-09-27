// Package library organizes finished downloads into a typed taxonomy
// (series/movie/show/music/magazine/ebook/game/software), configured via a
// categories.yaml file that lives next to config.yaml. An unmatched file
// falls back to storage.downloads_dir, flat (see queue.Engine.runTransfer).
package library

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Category is one taxonomy entry: which kind of file it accepts, where it
// lives on disk, and how a matched file's destination path is built.
type Category struct {
	ID            string   `yaml:"id" json:"id"`
	Kind          string   `yaml:"kind" json:"kind"`
	Name          string   `yaml:"name" json:"name"`
	Enabled       bool     `yaml:"enabled" json:"enabled"`
	Dir           string   `yaml:"dir" json:"dir"`
	Path          string   `yaml:"path" json:"path"`
	SeasonDir     string   `yaml:"season_dir,omitempty" json:"season_dir,omitempty"`
	CreateFolders bool     `yaml:"create_folders" json:"create_folders"`
	AutoExtract   bool     `yaml:"auto_extract" json:"auto_extract"`
	DeleteArchive bool     `yaml:"delete_archive" json:"delete_archive"`
	Priority      int      `yaml:"priority" json:"priority"`
	Extensions    []string `yaml:"extensions" json:"extensions"`
	Patterns      []string `yaml:"patterns" json:"patterns"`
}

// Config is the whole categories.yaml document.
type Config struct {
	// Version tracks one-time migrations applied by Load (see
	// upgradeArchiveCategories). Zero means "written before versioning
	// existed" — Detect stamps CurrentVersion on every config it creates.
	Version      int        `yaml:"version" json:"version"`
	AutoOrganize bool       `yaml:"auto_organize" json:"auto_organize"`
	SearchDepth  int        `yaml:"search_depth" json:"search_depth"`
	MediaRoot    string     `yaml:"media_root" json:"media_root"`
	Categories   []Category `yaml:"categories" json:"categories"`
}

// CurrentVersion is the categories.yaml schema version.
const CurrentVersion = 1

// archiveExtsToAdd are the extensions upgradeArchiveCategories adds to
// series/movie categories, matching the defaults in detect.go's kindDefaults.
var archiveExtsToAdd = []string{"tar", "zip", "rar", "7z"}

// upgradeArchiveCategories is Load's one-time migration for categories.yaml
// files written before series/movie categories could unpack archives (season
// packs and movie releases shared as tar/zip/rar/7z): it adds the missing
// archive extensions and turns extraction + archive deletion on for those two
// kinds — but only where the file omits the key. An explicit value (even the
// false every Save-written v0 file carries) is kept: flipping it could
// extract and then permanently delete a user's archives. raw is the file's
// YAML, used to tell "absent" from "false". Tracked by Config.Version so it
// runs at most once per file.
func upgradeArchiveCategories(cfg *Config, raw []byte) bool {
	if cfg.Version >= CurrentVersion {
		return false
	}
	hasExtract, hasDelete := archiveKeysPresent(raw)
	for i := range cfg.Categories {
		cat := &cfg.Categories[i]
		if cat.Kind != "series" && cat.Kind != "movie" {
			continue
		}
		for _, ext := range archiveExtsToAdd {
			if !hasExt(cat.Extensions, ext) {
				cat.Extensions = append(cat.Extensions, ext)
			}
		}
		if i >= len(hasExtract) || !hasExtract[i] {
			cat.AutoExtract = true
		}
		if i >= len(hasDelete) || !hasDelete[i] {
			cat.DeleteArchive = true
		}
	}
	cfg.Version = CurrentVersion
	return true
}

// archiveKeysPresent reports, per category index, whether raw sets
// auto_extract / delete_archive at all.
func archiveKeysPresent(raw []byte) (extract, del []bool) {
	var probe struct {
		Categories []struct {
			AutoExtract   *bool `yaml:"auto_extract"`
			DeleteArchive *bool `yaml:"delete_archive"`
		} `yaml:"categories"`
	}
	_ = yaml.Unmarshal(raw, &probe) // raw already parsed once by Load
	for _, c := range probe.Categories {
		extract = append(extract, c.AutoExtract != nil)
		del = append(del, c.DeleteArchive != nil)
	}
	return extract, del
}

func hasExt(exts []string, ext string) bool {
	for _, e := range exts {
		if strings.EqualFold(strings.TrimPrefix(e, "."), ext) {
			return true
		}
	}
	return false
}

// validKinds are the parsers implemented in parse.go.
var validKinds = map[string]bool{
	"series": true, "movie": true, "show": true, "music": true,
	"magazine": true, "ebook": true, "game": true, "software": true,
}

// Load reads and parses a categories.yaml file. upgraded reports whether the
// file predates CurrentVersion and was migrated in memory — the caller is
// responsible for saving it back (Save is a separate, deliberate step).
func Load(path string) (cfg *Config, upgraded bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	cfg = &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, false, fmt.Errorf("parsing categories file: %w", err)
	}
	upgraded = upgradeArchiveCategories(cfg, data)
	return cfg, upgraded, nil
}

// Save writes cfg to path atomically and durably: a uniquely named temp file
// in the same directory (so concurrent saves never share one), fsynced, then
// renamed over path.
func Save(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encoding categories file: %w", err)
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("writing temp categories file: %w", err)
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Chmod(0644)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("writing temp categories file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replacing categories file: %w", err)
	}
	return nil
}

// Validate checks a config before it is saved: media_root must exist, kinds
// must be known, ids unique, path/season_dir templates well-formed, and
// every pattern a valid regexp.
func Validate(cfg *Config) error {
	if cfg.MediaRoot == "" {
		return fmt.Errorf("media_root is required")
	}
	if info, err := os.Stat(cfg.MediaRoot); err != nil || !info.IsDir() {
		return fmt.Errorf("media_root %q does not exist", cfg.MediaRoot)
	}
	seen := make(map[string]bool, len(cfg.Categories))
	for _, cat := range cfg.Categories {
		if cat.ID == "" {
			return fmt.Errorf("category with kind %q: id is required", cat.Kind)
		}
		if seen[cat.ID] {
			return fmt.Errorf("duplicate category id %q", cat.ID)
		}
		seen[cat.ID] = true
		if !validKinds[cat.Kind] {
			return fmt.Errorf("category %q: invalid kind %q", cat.ID, cat.Kind)
		}
		if err := validateTemplate(cat.Path); err != nil {
			return fmt.Errorf("category %q: path template: %w", cat.ID, err)
		}
		if cat.Kind == "series" {
			if err := validateTemplate(cat.SeasonDir); err != nil {
				return fmt.Errorf("category %q: season_dir template: %w", cat.ID, err)
			}
		}
		for _, p := range cat.Patterns {
			if _, err := regexp.Compile(p); err != nil {
				return fmt.Errorf("category %q: invalid pattern %q: %w", cat.ID, p, err)
			}
		}
	}
	return nil
}

var bracedRe = regexp.MustCompile(`\{[^{}]*\}`)

func validateTemplate(tmpl string) error {
	if strings.Count(tmpl, "{") != strings.Count(tmpl, "}") {
		return fmt.Errorf("unbalanced braces in %q", tmpl)
	}
	for _, m := range bracedRe.FindAllString(tmpl, -1) {
		if !placeholderRe.MatchString(m) {
			return fmt.Errorf("invalid placeholder %q", m)
		}
	}
	return nil
}

// Manager holds the active library config in memory (guarded by a lock) and
// persists changes to its categories.yaml file.
type Manager struct {
	mu   sync.RWMutex
	cfg  Config
	path string
}

func NewManager(path string, cfg Config) *Manager {
	return &Manager{cfg: cfg, path: path}
}

// Path is the categories.yaml location this manager saves to.
func (m *Manager) Path() string { return m.path }

// Get returns a copy of the current config.
func (m *Manager) Get() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// Set validates cfg, saves it to disk, and swaps the in-memory copy — all
// under the lock, so concurrent Sets serialize and disk always matches memory.
func (m *Manager) Set(cfg Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := Validate(&cfg); err != nil {
		return err
	}
	if err := Save(m.path, &cfg); err != nil {
		return err
	}
	m.cfg = cfg
	return nil
}
