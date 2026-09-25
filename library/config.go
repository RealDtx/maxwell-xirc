// Package library organizes finished downloads into a typed taxonomy
// (series/movie/show/music/magazine/ebook/game/software), configured via a
// categories.yaml file that lives next to config.yaml. It runs before the
// existing routing rules (routing package), which stay as the fallback.
package library

import (
	"fmt"
	"os"
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
	AutoOrganize bool       `yaml:"auto_organize" json:"auto_organize"`
	SearchDepth  int        `yaml:"search_depth" json:"search_depth"`
	MediaRoot    string     `yaml:"media_root" json:"media_root"`
	Categories   []Category `yaml:"categories" json:"categories"`
}

// validKinds are the parsers implemented in parse.go.
var validKinds = map[string]bool{
	"series": true, "movie": true, "show": true, "music": true,
	"magazine": true, "ebook": true, "game": true, "software": true,
}

// Load reads and parses a categories.yaml file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing categories file: %w", err)
	}
	return &cfg, nil
}

// Save writes cfg to path atomically: temp file + rename, the same approach
// as config.WriteStorageDirs.
func Save(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encoding categories file: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
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

// Get returns a copy of the current config.
func (m *Manager) Get() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// Set validates cfg, saves it to disk, and swaps the in-memory copy.
func (m *Manager) Set(cfg Config) error {
	if err := Validate(&cfg); err != nil {
		return err
	}
	if err := Save(m.path, &cfg); err != nil {
		return err
	}
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
	return nil
}
