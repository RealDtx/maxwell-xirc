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
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
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

type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Database      DatabaseConfig      `yaml:"database"`
	Storage       StorageConfig       `yaml:"storage"`
	DCC           DCCConfig           `yaml:"dcc"`
	Downloads     DownloadsConfig     `yaml:"downloads"`
	Notifications NotificationsConfig `yaml:"notifications"`
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
