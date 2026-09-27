package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	if cfg.Database.Path != "./data/maxwell-irc.db" {
		t.Errorf("expected path ./data/maxwell-irc.db, got %s", cfg.Database.Path)
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
}

func TestLoad_IgnoresRemovedKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	old := `storage:
  downloads_dir: /dl
  critical_free_space: 500MB
  auto_extract:
    enabled: true
dcc:
  passive_enabled: true
  passive_ports: "30000-30010"
notifications:
  quiet_hours_start: "22:00"
`
	if err := os.WriteFile(path, []byte(old), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.DownloadsDir != "/dl" {
		t.Errorf("downloads_dir = %q", cfg.Storage.DownloadsDir)
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
	// Missing files are now handled gracefully with defaults + env
	_, err := Load("/nonexistent/path.yaml")
	if err != nil {
		t.Errorf("missing file should not error: %v", err)
	}
}

func TestSaveKeys_UpdatingStorageDirs(t *testing.T) {
	original := `server:
  host: 127.0.0.1
  port: 8085

storage:
  media_dir: /old/media
  downloads_dir: /old/downloads
  temp_dir: /tmp
  min_free_space: 1GB
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	if err := SaveKeys(path, map[string]any{
		"storage": map[string]string{
			"media_dir":     "/new/media",
			"downloads_dir": "/new/downloads",
		},
	}); err != nil {
		t.Fatalf("SaveKeys: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "media_dir: /new/media") {
		t.Errorf("media_dir not updated:\n%s", content)
	}
	if !strings.Contains(content, "downloads_dir: /new/downloads") {
		t.Errorf("downloads_dir not updated:\n%s", content)
	}
	// Verify other fields preserved
	if !strings.Contains(content, "temp_dir: /tmp") {
		t.Errorf("temp_dir lost:\n%s", content)
	}
	if !strings.Contains(content, "host: 127.0.0.1") {
		t.Errorf("server.host lost:\n%s", content)
	}
}

func TestSaveKeys_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("storage:\n  media_dir: /a\n  downloads_dir: /b\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := SaveKeys(path, map[string]any{
		"storage": map[string]string{"media_dir": "/new/a", "downloads_dir": "/new/b"},
	}); err != nil {
		t.Fatal(err)
	}

	// Temp files should be cleaned up
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temp files not cleaned up: %v", entries)
	}
}

func TestLoad_MissingFileUsesDefaultsAndEnv(t *testing.T) {
	t.Setenv("XIRC_STORAGE_DOWNLOADS_DIR", "/downloads")
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("missing config should not error: %v", err)
	}
	if cfg.Server.Port != 8085 {
		t.Errorf("port default: got %d", cfg.Server.Port)
	}
	if cfg.Storage.DownloadsDir != "/downloads" {
		t.Errorf("env override: got %q", cfg.Storage.DownloadsDir)
	}
}

func TestAuthDefaultsAndEnvList(t *testing.T) {
	t.Setenv("XIRC_AUTH_TRUSTED_NETWORKS", "192.168.0.0/16, 10.0.0.0/8")
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.TrustedRole != "admin" {
		t.Errorf("trusted_role default: %q", cfg.Auth.TrustedRole)
	}
	if len(cfg.Auth.TrustedProxies) != 2 {
		t.Errorf("trusted_proxies default: %v", cfg.Auth.TrustedProxies)
	}
	want := []string{"192.168.0.0/16", "10.0.0.0/8"}
	if !reflect.DeepEqual(cfg.Auth.TrustedNetworks, want) {
		t.Errorf("trusted_networks: got %v want %v", cfg.Auth.TrustedNetworks, want)
	}
}

func TestLoad_BadYAMLStillErrors(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.yaml")
	os.WriteFile(p, []byte("server: [\n"), 0644)
	if _, err := Load(p); err == nil {
		t.Fatal("expected parse error")
	}
}
