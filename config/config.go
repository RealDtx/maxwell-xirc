package config

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"reflect"
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
	MaxConcurrent int `yaml:"max_concurrent"`
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
	SearchResultRetentionDays int `yaml:"search_result_retention_days"`
	IndexMaxFiles             int `yaml:"index_max_files"`
	IntervalHours             int `yaml:"interval_hours"`
}

type AuthConfig struct {
	// TrustedNetworks are CIDRs whose clients skip login. Empty = always log in.
	TrustedNetworks []string `yaml:"trusted_networks"`
	// TrustedRole is the role granted to trusted-network clients: admin|user.
	TrustedRole string `yaml:"trusted_role"`
	// TrustedProxies are peers whose X-Forwarded-For/-Proto headers are believed.
	TrustedProxies []string `yaml:"trusted_proxies"`
}

type Config struct {
	Server      ServerConfig      `yaml:"server"`
	Database    DatabaseConfig    `yaml:"database"`
	Storage     StorageConfig     `yaml:"storage"`
	Downloads   DownloadsConfig   `yaml:"downloads"`
	Patterns    []PatternConfig   `yaml:"patterns"`
	Maintenance MaintenanceConfig `yaml:"maintenance"`
	Auth        AuthConfig        `yaml:"auth"`
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
			TempDir:      "/srv/downloads/.tmp",
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

	return &cfg, nil
}

// WriteStorageDirs rewrites storage.media_dir and storage.downloads_dir in the
// config file at path, preserving all other content. The write is atomic: a
// temp file is written first, then renamed over the original.
func WriteStorageDirs(path, mediaDir, downloadsDir string) error {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	inStorage := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Detect storage: section header (no leading whitespace)
		if trimmed == "storage:" {
			inStorage = true
			continue
		}
		// Leave storage section when we hit a top-level key
		if inStorage && len(line) > 0 && line[0] != ' ' && line[0] != '\t' && line[0] != '#' {
			inStorage = false
		}
		if inStorage {
			if strings.HasPrefix(trimmed, "media_dir:") {
				lines[i] = "  media_dir: " + mediaDir
			} else if strings.HasPrefix(trimmed, "downloads_dir:") {
				lines[i] = "  downloads_dir: " + downloadsDir
			}
		}
	}

	result := []byte(strings.Join(lines, "\n"))
	tmp := path + ".tmp"
	if err := ioutil.WriteFile(tmp, result, 0644); err != nil {
		return fmt.Errorf("writing temp config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replacing config: %w", err)
	}
	return nil
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
