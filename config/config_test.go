package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFullConfig(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "testdata", "config_full.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("expected host 127.0.0.1, got %s", cfg.Server.Host)
	}
	if cfg.Server.Port != 8085 {
		t.Errorf("expected port 8085, got %d", cfg.Server.Port)
	}
	if cfg.Database.Driver != "sqlite" {
		t.Errorf("expected driver sqlite, got %s", cfg.Database.Driver)
	}
	if cfg.Database.Path != "./data/xirc.db" {
		t.Errorf("expected path ./data/xirc.db, got %s", cfg.Database.Path)
	}
	if cfg.Storage.MediaDir != "/srv/dlna/media" {
		t.Errorf("expected media_dir /srv/dlna/media, got %s", cfg.Storage.MediaDir)
	}
	if cfg.Storage.MinFreeSpace != "1GB" {
		t.Errorf("expected min_free_space 1GB, got %s", cfg.Storage.MinFreeSpace)
	}
	if cfg.Downloads.MaxConcurrent != 3 {
		t.Errorf("expected max_concurrent 3, got %d", cfg.Downloads.MaxConcurrent)
	}
}

func TestLoadMinimalConfigUsesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "testdata", "config_minimal.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("expected default host 127.0.0.1, got %s", cfg.Server.Host)
	}
	if cfg.Server.Port != 8085 {
		t.Errorf("expected default port 8085, got %d", cfg.Server.Port)
	}
	if cfg.Downloads.MaxConcurrent != 3 {
		t.Errorf("expected default max_concurrent 3, got %d", cfg.Downloads.MaxConcurrent)
	}
	if cfg.Storage.MinFreeSpace != "1GB" {
		t.Errorf("expected default min_free_space 1GB, got %s", cfg.Storage.MinFreeSpace)
	}
	if cfg.Storage.CriticalFreeSpace != "500MB" {
		t.Errorf("expected default critical_free_space 500MB, got %s", cfg.Storage.CriticalFreeSpace)
	}
}

func TestLoadConfigEnvOverride(t *testing.T) {
	os.Setenv("XIRC_SERVER_PORT", "9090")
	os.Setenv("XIRC_DATABASE_DRIVER", "mysql")
	defer os.Unsetenv("XIRC_SERVER_PORT")
	defer os.Unsetenv("XIRC_DATABASE_DRIVER")

	cfg, err := Load(filepath.Join("..", "testdata", "config_minimal.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Server.Port != 9090 {
		t.Errorf("expected env override port 9090, got %d", cfg.Server.Port)
	}
	if cfg.Database.Driver != "mysql" {
		t.Errorf("expected env override driver mysql, got %s", cfg.Database.Driver)
	}
}

func TestLoadConfigFileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}
