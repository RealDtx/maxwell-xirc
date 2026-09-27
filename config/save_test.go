package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const commented = `# top comment
server:
  host: 127.0.0.1 # bind
  port: 8085
storage:
  media_dir: /media   # seeds categories
  downloads_dir: /old
patterns:
  - name: x
    regex: "a(b)"
auth:
  trusted_networks: [10.0.0.0/8]
`

func TestSaveKeys_PreservesCommentsAndOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(commented), 0600); err != nil {
		t.Fatal(err)
	}
	e := Editable{
		Storage:     EditableStorage{DownloadsDir: "/new", TempDir: "/new/.tmp", MinFreeSpace: "2GB"},
		Downloads:   DownloadsConfig{MaxConcurrent: 4},
		Maintenance: MaintenanceConfig{SearchResultRetentionDays: 7, IndexMaxFiles: 1000, IntervalHours: 2},
		Auth:        AuthConfig{TrustedNetworks: []string{"192.168.0.0/16"}, TrustedRole: "user", TrustedProxies: []string{"127.0.0.1/32"}},
	}
	if err := SaveKeys(path, e); err != nil {
		t.Fatalf("SaveKeys: %v", err)
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	for _, want := range []string{"# top comment", "# bind", "# seeds categories", "media_dir: /media", `regex: "a(b)"`} {
		if !strings.Contains(s, want) {
			t.Errorf("lost %q:\n%s", want, s)
		}
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0600 {
		t.Errorf("mode = %v, want 0600", st.Mode().Perm())
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Editable(); got.Storage.DownloadsDir != "/new" || got.Downloads.MaxConcurrent != 4 ||
		got.Maintenance.IntervalHours != 2 || got.Auth.TrustedRole != "user" ||
		len(got.Auth.TrustedNetworks) != 1 || got.Auth.TrustedNetworks[0] != "192.168.0.0/16" {
		t.Errorf("round trip: %+v", got)
	}
	if cfg.Server.Port != 8085 || cfg.Storage.MediaDir != "/media" || len(cfg.Patterns) != 1 {
		t.Errorf("unrelated keys changed: %+v", cfg)
	}
}

func TestSaveKeys_CreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := SaveKeys(path, map[string]any{"downloads": map[string]int{"max_concurrent": 2}}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0640 {
		t.Fatalf("stat: %v %v", st, err)
	}
	cfg, _ := Load(path)
	if cfg.Downloads.MaxConcurrent != 2 {
		t.Errorf("max_concurrent = %d", cfg.Downloads.MaxConcurrent)
	}
}

func TestSaveKeys_LeavesNoTempOnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(": not yaml : [\n"), 0644)
	if err := SaveKeys(path, map[string]any{"a": 1}); err == nil {
		t.Fatal("want parse error")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestEnvLockedKeys(t *testing.T) {
	t.Setenv("XIRC_STORAGE_DOWNLOADS_DIR", "/x")
	t.Setenv("XIRC_AUTH_TRUSTED_NETWORKS", "10.0.0.0/8")
	got := strings.Join(EnvLockedKeys(), ",")
	if got != "storage.downloads_dir,auth.trusted_networks" {
		t.Errorf("EnvLockedKeys = %q", got)
	}
}

func TestSaveKeys_CreatesMissingDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "etc", "xirc", "config.yaml")
	if err := SaveKeys(path, map[string]any{"downloads": map[string]any{"max_concurrent": 2}}); err != nil {
		t.Fatalf("SaveKeys: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || !strings.Contains(string(data), "max_concurrent: 2") {
		t.Fatalf("got %q, %v", data, err)
	}
}

// A symlinked config.yaml stays a symlink; its target gets the new content.
func TestSaveKeys_FollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("server:\n  port: 8085\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.yaml")
	if err := os.Symlink(filepath.Join("real", "config.yaml"), link); err != nil {
		t.Fatal(err)
	}
	if err := SaveKeys(link, map[string]any{"downloads": map[string]any{"max_concurrent": 2}}); err != nil {
		t.Fatalf("SaveKeys: %v", err)
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("config.yaml is no longer a symlink: %v %v", st, err)
	}
	data, err := os.ReadFile(target)
	if err != nil || !strings.Contains(string(data), "max_concurrent: 2") || !strings.Contains(string(data), "port: 8085") {
		t.Fatalf("target not updated: %q %v", data, err)
	}
	if st, _ := os.Stat(target); st.Mode().Perm() != 0o600 {
		t.Errorf("target mode: got %v want 0600", st.Mode().Perm())
	}
}
