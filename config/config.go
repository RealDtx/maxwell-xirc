package config

import (
	"fmt"
	"io/ioutil"
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
	MediaDir          string `yaml:"media_dir"`
	DownloadsDir      string `yaml:"downloads_dir"`
	TempDir           string `yaml:"temp_dir"`
	MinFreeSpace      string `yaml:"min_free_space"`
	CriticalFreeSpace string `yaml:"critical_free_space"`
}

type DCCConfig struct {
	PassiveEnabled bool   `yaml:"passive_enabled"`
	PassivePorts   string `yaml:"passive_ports"`
	ExternalIP     string `yaml:"external_ip"`
}

type DownloadsConfig struct {
	MaxConcurrent int `yaml:"max_concurrent"`
}

type NotificationsConfig struct {
	QuietHoursStart string `yaml:"quiet_hours_start"`
	QuietHoursEnd   string `yaml:"quiet_hours_end"`
}

type PatternConfig struct {
	Name         string         `yaml:"name"`
	Regex        string         `yaml:"regex"`
	FieldMapping map[string]int `yaml:"field_mapping"`
	Priority     int            `yaml:"priority"`
	Tags         []string       `yaml:"tags"`
}

type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Database      DatabaseConfig      `yaml:"database"`
	Storage       StorageConfig       `yaml:"storage"`
	DCC           DCCConfig           `yaml:"dcc"`
	Downloads     DownloadsConfig     `yaml:"downloads"`
	Notifications NotificationsConfig `yaml:"notifications"`
	Patterns      []PatternConfig     `yaml:"patterns"`
}

func defaults() Config {
	return Config{
		Server: ServerConfig{
			Host: "127.0.0.1",
			Port: 8085,
		},
		Database: DatabaseConfig{
			Driver: "sqlite",
			Path:   "./data/xirc.db",
		},
		Storage: StorageConfig{
			MediaDir:          "/srv/dlna/media",
			DownloadsDir:      "/srv/downloads",
			TempDir:           "/srv/downloads/.tmp",
			MinFreeSpace:      "1GB",
			CriticalFreeSpace: "500MB",
		},
		DCC: DCCConfig{
			PassivePorts: "30000-30010",
		},
		Downloads: DownloadsConfig{
			MaxConcurrent: 3,
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := defaults()

	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
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

		envKey := prefix + "_" + strings.ToUpper(fieldType.Name)

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
		}
	}
}
