package config

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	Host   string `yaml:"host"`
	Port   int    `yaml:"port"`
	Prefix string `yaml:"prefix"`
}

type DatabaseConfig struct {
	Driver string `yaml:"driver"`
	Path   string `yaml:"path"`
	DSN    string `yaml:"dsn"`
}

type StorageConfig struct {
	MediaDir     string `yaml:"media_dir"`
	DownloadsDir string `yaml:"downloads_dir"`
	TempDir      string `yaml:"temp_dir"`
	MinFreeSpace string `yaml:"min_free_space"`
	// CategoriesFile is the library taxonomy config (library package).
	// Defaults to categories.yaml next to the --config file when empty.
	CategoriesFile string `yaml:"categories_file"`
}

type DownloadsConfig struct {
	MaxConcurrent int `yaml:"max_concurrent" json:"max_concurrent"`
}

type PatternConfig struct {
	Name         string         `yaml:"name"`
	Regex        string         `yaml:"regex"`
	FieldMapping map[string]int `yaml:"field_mapping"`
	Priority     int            `yaml:"priority"`
	Tags         []string       `yaml:"tags"`
}

// MaintenanceConfig controls the background job that prunes stale
// search_results rows and caps the self-collected file index so both stay
// bounded instead of growing forever.
type MaintenanceConfig struct {
	SearchResultRetentionDays int `yaml:"search_result_retention_days" json:"search_result_retention_days"`
	IndexMaxFiles             int `yaml:"index_max_files" json:"index_max_files"`
	IntervalHours             int `yaml:"interval_hours" json:"interval_hours"`
}

type AuthConfig struct {
	// TrustedNetworks are CIDRs whose clients skip login. Empty = always log in.
	TrustedNetworks []string `yaml:"trusted_networks" json:"trusted_networks"`
	// TrustedRole is the role granted to trusted-network clients: admin|user.
	TrustedRole string `yaml:"trusted_role" json:"trusted_role"`
	// TrustedProxies are peers whose X-Forwarded-For/-Proto headers are believed.
	TrustedProxies []string `yaml:"trusted_proxies" json:"trusted_proxies"`
}

// UIConfig holds web UI behaviour defaults.
type UIConfig struct {
	// HelpDefault controls whether in-app help blocks start open:
	// first_time (until closed once per browser) | always | never.
	HelpDefault string `yaml:"help_default" json:"help_default"`
}

type Config struct {
	Server      ServerConfig      `yaml:"server"`
	Database    DatabaseConfig    `yaml:"database"`
	Storage     StorageConfig     `yaml:"storage"`
	Downloads   DownloadsConfig   `yaml:"downloads"`
	Patterns    []PatternConfig   `yaml:"patterns"`
	Maintenance MaintenanceConfig `yaml:"maintenance"`
	Auth        AuthConfig        `yaml:"auth"`
	UI          UIConfig          `yaml:"ui"`
}

// EditableStorage is the part of storage the admin UI may change at runtime.
type EditableStorage struct {
	DownloadsDir string `yaml:"downloads_dir" json:"downloads_dir"`
	TempDir      string `yaml:"temp_dir" json:"temp_dir"`
	MinFreeSpace string `yaml:"min_free_space" json:"min_free_space"`
}

// Editable is the subset of Config the admin settings page edits and the
// server applies live. Server and database settings are deliberately absent.
type Editable struct {
	Storage     EditableStorage   `yaml:"storage" json:"storage"`
	Downloads   DownloadsConfig   `yaml:"downloads" json:"downloads"`
	Maintenance MaintenanceConfig `yaml:"maintenance" json:"maintenance"`
	Auth        AuthConfig        `yaml:"auth" json:"auth"`
	UI          UIConfig          `yaml:"ui" json:"ui"`
}

func (c *Config) Editable() Editable {
	return Editable{
		Storage:     EditableStorage{c.Storage.DownloadsDir, c.Storage.TempDir, c.Storage.MinFreeSpace},
		Downloads:   c.Downloads,
		Maintenance: c.Maintenance,
		// Cloned so decoding a request onto the result can't write into the
		// live config's backing arrays.
		Auth: AuthConfig{slices.Clone(c.Auth.TrustedNetworks), c.Auth.TrustedRole, slices.Clone(c.Auth.TrustedProxies)},
		UI:   c.UI,
	}
}

func (c *Config) ApplyEditable(e Editable) {
	c.Storage.DownloadsDir, c.Storage.TempDir, c.Storage.MinFreeSpace = e.Storage.DownloadsDir, e.Storage.TempDir, e.Storage.MinFreeSpace
	c.Downloads, c.Maintenance, c.Auth = e.Downloads, e.Maintenance, e.Auth
	c.UI = e.UI
}

// EnvLockedKeys lists the dotted yaml keys of Editable whose XIRC_* override
// is set: a value saved from the UI would be replaced by the env var on the
// next start, so the UI shows these read-only.
func EnvLockedKeys() []string {
	var out []string
	t := reflect.TypeOf(Editable{})
	for i := 0; i < t.NumField(); i++ {
		sec := t.Field(i)
		secName := strings.Split(sec.Tag.Get("yaml"), ",")[0]
		for j := 0; j < sec.Type.NumField(); j++ {
			key := strings.Split(sec.Type.Field(j).Tag.Get("yaml"), ",")[0]
			if _, ok := os.LookupEnv("XIRC_" + strings.ToUpper(secName) + "_" + strings.ToUpper(key)); ok {
				out = append(out, secName+"."+key)
			}
		}
	}
	return out
}

func defaults() Config {
	return Config{
		Server: ServerConfig{
			Host: "127.0.0.1",
			Port: 8085,
		},
		Database: DatabaseConfig{
			Driver: "sqlite",
			Path:   "./data/maxwell-irc.db",
		},
		Storage: StorageConfig{
			MediaDir:     "/srv/dlna/media",
			DownloadsDir: "/srv/downloads",
			MinFreeSpace: "1GB",
		},
		Downloads: DownloadsConfig{
			MaxConcurrent: 3,
		},
		Maintenance: MaintenanceConfig{
			SearchResultRetentionDays: 14,
			IndexMaxFiles:             200000,
			IntervalHours:             6,
		},
		Auth: AuthConfig{
			TrustedRole:    "admin",
			TrustedProxies: []string{"127.0.0.1/32", "::1/128"},
		},
		UI: UIConfig{HelpDefault: "first_time"},
	}
}

func Load(path string) (*Config, error) {
	cfg := defaults()

	data, err := ioutil.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		log.Printf("config: %s not found, using defaults + environment", path)
	case err != nil:
		return nil, fmt.Errorf("reading config file: %w", err)
	default:
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parsing config file: %w", err)
		}
	}

	applyEnvOverrides(&cfg)
	// Unset temp_dir follows downloads_dir, so moving downloads (wizard,
	// hand edit) doesn't leave temp behind on an unwritable default.
	if cfg.Storage.TempDir == "" {
		cfg.Storage.TempDir = filepath.Join(cfg.Storage.DownloadsDir, ".tmp")
	}

	return &cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	applyEnvToStruct(reflect.ValueOf(cfg).Elem(), "XIRC")
}

func applyEnvToStruct(v reflect.Value, prefix string) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)

		name := strings.Split(fieldType.Tag.Get("yaml"), ",")[0]
		if name == "" || name == "-" {
			name = fieldType.Name
		}
		envKey := prefix + "_" + strings.ToUpper(name)

		if field.Kind() == reflect.Struct {
			applyEnvToStruct(field, envKey)
			continue
		}

		envVal, ok := os.LookupEnv(envKey)
		if !ok {
			continue
		}

		switch field.Kind() {
		case reflect.String:
			field.SetString(envVal)
		case reflect.Int:
			if n, err := strconv.Atoi(envVal); err == nil {
				field.SetInt(int64(n))
			}
		case reflect.Bool:
			if b, err := strconv.ParseBool(envVal); err == nil {
				field.SetBool(b)
			}
		case reflect.Slice:
			if field.Type().Elem().Kind() == reflect.String {
				var parts []string
				for _, p := range strings.Split(envVal, ",") {
					if p = strings.TrimSpace(p); p != "" {
						parts = append(parts, p)
					}
				}
				field.Set(reflect.ValueOf(parts))
			}
		}
	}
}
