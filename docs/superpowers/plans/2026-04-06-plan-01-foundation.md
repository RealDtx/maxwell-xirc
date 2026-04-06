# xirc Plan 1: Foundation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create the project scaffold with configuration loading, database abstraction (SQLite + MariaDB), schema migrations, and a basic HTTP server with health endpoint.

**Architecture:** A Go module with clean separation between config, database, and HTTP layers. The database layer uses a `Store` interface with SQLite and MySQL implementations, selected at startup based on config. The HTTP server is minimal — just enough to prove the binary runs and the DB connects.

**Tech Stack:** Go 1.22+, `modernc.org/sqlite`, `go-sql-driver/mysql`, `gopkg.in/yaml.v3`, standard library `net/http`

---

## File Structure

```
maxwell-xirc/
├── main.go                     # Entry point: parse flags, load config, init DB, start server
├── go.mod
├── go.sum
├── config/
│   ├── config.go               # Config struct + Load() from YAML + env override
│   └── config_test.go
├── db/
│   ├── store.go                # Store interface definition
│   ├── models.go               # All data model structs
│   ├── migrations.go           # Schema SQL + migration runner
│   ├── migrations_test.go
│   ├── sqlite.go               # SQLite implementation of Store
│   ├── sqlite_test.go
│   ├── mysql.go                # MariaDB/MySQL implementation of Store
│   └── mysql_test.go
├── server/
│   ├── server.go               # HTTP server setup + routes
│   └── server_test.go
├── config.yaml                 # Example config file
└── testdata/
    ├── config_full.yaml        # Test fixture: all fields set
    └── config_minimal.yaml     # Test fixture: only required fields
```

---

### Task 1: Project Scaffold

**Files:**
- Create: `go.mod`
- Create: `main.go`

- [ ] **Step 1: Initialize Go module**

Run:
```bash
go mod init github.com/maxwell-xirc/xirc
```

Expected: `go.mod` created with module path.

- [ ] **Step 2: Create minimal main.go**

Create `main.go`:

```go
package main

import "fmt"

func main() {
	fmt.Println("xirc starting...")
}
```

- [ ] **Step 3: Verify it compiles and runs**

Run:
```bash
go run main.go
```

Expected output: `xirc starting...`

- [ ] **Step 4: Commit**

```bash
git add go.mod main.go
git commit -m "feat: initialize Go module and main entry point"
```

---

### Task 2: Configuration — Struct and YAML Loading

**Files:**
- Create: `config/config.go`
- Create: `config/config_test.go`
- Create: `testdata/config_full.yaml`
- Create: `testdata/config_minimal.yaml`

- [ ] **Step 1: Create test fixtures**

Create `testdata/config_full.yaml`:

```yaml
server:
  host: 127.0.0.1
  port: 8085

database:
  driver: sqlite
  path: ./data/xirc.db
  dsn: ""

storage:
  media_dir: /srv/dlna/media
  downloads_dir: /srv/downloads
  temp_dir: /srv/downloads/.tmp
  min_free_space: 1GB
  critical_free_space: 500MB

dcc:
  passive_enabled: false
  passive_ports: "30000-30010"
  external_ip: ""

downloads:
  max_concurrent: 3

notifications:
  quiet_hours_start: ""
  quiet_hours_end: ""
```

Create `testdata/config_minimal.yaml`:

```yaml
database:
  driver: sqlite
  path: ./test.db
```

- [ ] **Step 2: Write failing tests for config loading**

Create `config/config_test.go`:

```go
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
```

- [ ] **Step 3: Run tests to verify they fail**

Run:
```bash
cd config && go test -v ./...
```

Expected: compilation error — `config` package doesn't exist yet.

- [ ] **Step 4: Implement config loading**

Create `config/config.go`:

```go
package config

import (
	"fmt"
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

	data, err := os.ReadFile(path)
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
```

- [ ] **Step 5: Install dependency and run tests**

Run:
```bash
go get gopkg.in/yaml.v3
cd config && go test -v ./...
```

Expected: all 4 tests PASS.

- [ ] **Step 6: Commit**

```bash
git add config/ testdata/ go.mod go.sum
git commit -m "feat: add config loading with YAML + env overrides"
```

---

### Task 3: Data Models

**Files:**
- Create: `db/models.go`

- [ ] **Step 1: Create all model structs**

Create `db/models.go`:

```go
package db

import "time"

type Server struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Host         string    `json:"host"`
	Port         int       `json:"port"`
	SSL          bool      `json:"ssl"`
	Nickname     string    `json:"nickname"`
	AltNicknames []string  `json:"alt_nicknames"`
	AuthMethod   string    `json:"auth_method"`
	AuthPassword string    `json:"-"`
	AutoConnect  bool      `json:"auto_connect"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Channel struct {
	ID              int64  `json:"id"`
	ServerID        int64  `json:"server_id"`
	Name            string `json:"name"`
	Key             string `json:"-"`
	SearchCommand   string `json:"search_command"`
	DownloadChannel string `json:"download_channel"`
	AutoJoin        bool   `json:"auto_join"`
	Enabled         bool   `json:"enabled"`
}

type Download struct {
	ID              int64      `json:"id"`
	ServerID        int64      `json:"server_id"`
	Channel         string     `json:"channel"`
	BotNick         string     `json:"bot_nick"`
	PackNumber      int        `json:"pack_number"`
	Filename        string     `json:"filename"`
	Filesize        int64      `json:"filesize"`
	DownloadedBytes int64      `json:"downloaded_bytes"`
	Status          string     `json:"status"`
	DestinationPath string     `json:"destination_path"`
	ErrorMessage    string     `json:"error_message"`
	PeakSpeed       int64      `json:"peak_speed"`
	AverageSpeed    int64      `json:"average_speed"`
	StartedAt       *time.Time `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

type SearchResult struct {
	ID             int64     `json:"id"`
	ServerID       int64     `json:"server_id"`
	Channel        string    `json:"channel"`
	BotNick        string    `json:"bot_nick"`
	PackNumber     *int      `json:"pack_number"`
	Filename       *string   `json:"filename"`
	Filesize       *string   `json:"filesize"`
	DownloadsCount *int      `json:"downloads_count"`
	RawLine        string    `json:"raw_line"`
	SearchQuery    string    `json:"search_query"`
	Parsed         bool      `json:"parsed"`
	CreatedAt      time.Time `json:"created_at"`
}

type SavedSearch struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	ServerID  int64     `json:"server_id"`
	Channel   string    `json:"channel"`
	Query     string    `json:"query"`
	CreatedAt time.Time `json:"created_at"`
}

type ParsePattern struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	Regex         string     `json:"regex"`
	FieldMapping  string     `json:"field_mapping"`
	Priority      int        `json:"priority"`
	Builtin       bool       `json:"builtin"`
	Enabled       bool       `json:"enabled"`
	MatchCount    int        `json:"match_count"`
	FailCount     int        `json:"fail_count"`
	LastMatchedAt *time.Time `json:"last_matched_at"`
	AutoDisabled  bool       `json:"auto_disabled"`
}

type PostHook struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Scope    string `json:"scope"`
	ScopeID  *int64 `json:"scope_id"`
	HookType string `json:"hook_type"`
	Config   string `json:"config"`
	Enabled  bool   `json:"enabled"`
}

type FileRoutingRule struct {
	ID             int64  `json:"id"`
	Pattern        string `json:"pattern"`
	DestinationDir string `json:"destination_dir"`
	Priority       int    `json:"priority"`
	Builtin        bool   `json:"builtin"`
	Enabled        bool   `json:"enabled"`
}
```

- [ ] **Step 2: Verify it compiles**

Run:
```bash
go build ./db/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add db/models.go
git commit -m "feat: add database model structs"
```

---

### Task 4: Store Interface

**Files:**
- Create: `db/store.go`

- [ ] **Step 1: Define the Store interface**

Create `db/store.go`:

```go
package db

type Store interface {
	// Lifecycle
	Close() error
	Migrate() error

	// Servers
	GetServers() ([]Server, error)
	GetServer(id int64) (*Server, error)
	CreateServer(s *Server) error
	UpdateServer(s *Server) error
	DeleteServer(id int64) error

	// Channels
	GetChannels(serverID int64) ([]Channel, error)
	GetChannel(id int64) (*Channel, error)
	CreateChannel(c *Channel) error
	UpdateChannel(c *Channel) error
	DeleteChannel(id int64) error

	// Downloads
	GetDownloads(status string) ([]Download, error)
	GetDownload(id int64) (*Download, error)
	CreateDownload(d *Download) error
	UpdateDownload(d *Download) error

	// Search Results
	GetSearchResults(query string, serverID int64, channel string) ([]SearchResult, error)
	CreateSearchResult(r *SearchResult) error

	// Saved Searches
	GetSavedSearches() ([]SavedSearch, error)
	CreateSavedSearch(s *SavedSearch) error
	DeleteSavedSearch(id int64) error

	// Parse Patterns
	GetParsePatterns() ([]ParsePattern, error)
	UpdateParsePattern(p *ParsePattern) error
	CreateParsePattern(p *ParsePattern) error

	// Post Hooks
	GetPostHooks(scope string, scopeID *int64) ([]PostHook, error)
	CreatePostHook(h *PostHook) error
	UpdatePostHook(h *PostHook) error
	DeletePostHook(id int64) error

	// File Routing Rules
	GetFileRoutingRules() ([]FileRoutingRule, error)
	CreateFileRoutingRule(r *FileRoutingRule) error
	UpdateFileRoutingRule(r *FileRoutingRule) error
	DeleteFileRoutingRule(id int64) error
}
```

- [ ] **Step 2: Verify it compiles**

Run:
```bash
go build ./db/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add db/store.go
git commit -m "feat: add Store interface for database abstraction"
```

---

### Task 5: Schema Migrations

**Files:**
- Create: `db/migrations.go`
- Create: `db/migrations_test.go`

- [ ] **Step 1: Write failing test for migrations**

Create `db/migrations_test.go`:

```go
package db

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrationSQL_IsValid(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory sqlite: %v", err)
	}
	defer db.Close()

	for i, stmt := range migrationStatements() {
		_, err := db.Exec(stmt)
		if err != nil {
			t.Fatalf("migration statement %d failed: %v\nSQL: %s", i, err, stmt)
		}
	}
}

func TestMigrationSQL_CreatesAllTables(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory sqlite: %v", err)
	}
	defer db.Close()

	for _, stmt := range migrationStatements() {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("migration failed: %v", err)
		}
	}

	expectedTables := []string{
		"servers", "channels", "downloads", "search_results",
		"saved_searches", "parse_patterns", "post_hooks", "file_routing_rules",
	}

	for _, table := range expectedTables {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("expected table %q to exist, but it doesn't", table)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
go get modernc.org/sqlite
cd db && go test -v -run TestMigration ./...
```

Expected: compilation error — `migrationStatements` doesn't exist.

- [ ] **Step 3: Implement migrations**

Create `db/migrations.go`:

```go
package db

import "database/sql"

func migrationStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS servers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			host TEXT NOT NULL,
			port INTEGER NOT NULL DEFAULT 6667,
			ssl INTEGER NOT NULL DEFAULT 0,
			nickname TEXT NOT NULL DEFAULT 'xirc_user',
			alt_nicknames TEXT NOT NULL DEFAULT '[]',
			auth_method TEXT NOT NULL DEFAULT 'none',
			auth_password TEXT NOT NULL DEFAULT '',
			auto_connect INTEGER NOT NULL DEFAULT 1,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS channels (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			key TEXT NOT NULL DEFAULT '',
			search_command TEXT NOT NULL DEFAULT '!s',
			download_channel TEXT NOT NULL DEFAULT '',
			auto_join INTEGER NOT NULL DEFAULT 1,
			enabled INTEGER NOT NULL DEFAULT 1,
			FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
		)`,

		`CREATE TABLE IF NOT EXISTS downloads (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id INTEGER NOT NULL,
			channel TEXT NOT NULL,
			bot_nick TEXT NOT NULL,
			pack_number INTEGER NOT NULL,
			filename TEXT NOT NULL DEFAULT '',
			filesize INTEGER NOT NULL DEFAULT 0,
			downloaded_bytes INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'queued',
			destination_path TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			peak_speed INTEGER NOT NULL DEFAULT 0,
			average_speed INTEGER NOT NULL DEFAULT 0,
			started_at DATETIME,
			completed_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (server_id) REFERENCES servers(id)
		)`,

		`CREATE TABLE IF NOT EXISTS search_results (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id INTEGER NOT NULL,
			channel TEXT NOT NULL,
			bot_nick TEXT NOT NULL,
			pack_number INTEGER,
			filename TEXT,
			filesize TEXT,
			downloads_count INTEGER,
			raw_line TEXT NOT NULL,
			search_query TEXT NOT NULL,
			parsed INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (server_id) REFERENCES servers(id)
		)`,

		`CREATE TABLE IF NOT EXISTS saved_searches (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			server_id INTEGER NOT NULL,
			channel TEXT NOT NULL,
			query TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (server_id) REFERENCES servers(id)
		)`,

		`CREATE TABLE IF NOT EXISTS parse_patterns (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			regex TEXT NOT NULL,
			field_mapping TEXT NOT NULL DEFAULT '{}',
			priority INTEGER NOT NULL DEFAULT 0,
			builtin INTEGER NOT NULL DEFAULT 0,
			enabled INTEGER NOT NULL DEFAULT 1,
			match_count INTEGER NOT NULL DEFAULT 0,
			fail_count INTEGER NOT NULL DEFAULT 0,
			last_matched_at DATETIME,
			auto_disabled INTEGER NOT NULL DEFAULT 0
		)`,

		`CREATE TABLE IF NOT EXISTS post_hooks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			scope TEXT NOT NULL DEFAULT 'global',
			scope_id INTEGER,
			hook_type TEXT NOT NULL,
			config TEXT NOT NULL DEFAULT '{}',
			enabled INTEGER NOT NULL DEFAULT 1
		)`,

		`CREATE TABLE IF NOT EXISTS file_routing_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			pattern TEXT NOT NULL,
			destination_dir TEXT NOT NULL,
			priority INTEGER NOT NULL DEFAULT 0,
			builtin INTEGER NOT NULL DEFAULT 0,
			enabled INTEGER NOT NULL DEFAULT 1
		)`,
	}
}

func runMigrations(db *sql.DB) error {
	for _, stmt := range migrationStatements() {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
cd db && go test -v -run TestMigration ./...
```

Expected: both tests PASS.

- [ ] **Step 5: Commit**

```bash
git add db/migrations.go db/migrations_test.go go.mod go.sum
git commit -m "feat: add database schema migrations"
```

---

### Task 6: SQLite Store Implementation

**Files:**
- Create: `db/sqlite.go`
- Create: `db/sqlite_test.go`

- [ ] **Step 1: Write failing tests for SQLite store**

Create `db/sqlite_test.go`:

```go
package db

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestSQLiteStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSQLiteStore_CreateAndGetServer(t *testing.T) {
	store := newTestSQLiteStore(t)

	srv := &Server{
		Name:         "test-server",
		Host:         "irc.example.com",
		Port:         6667,
		SSL:          true,
		Nickname:     "testbot",
		AltNicknames: []string{"testbot_", "testbot__"},
		AuthMethod:   "nickserv",
		AuthPassword: "secret",
		AutoConnect:  true,
		Enabled:      true,
	}

	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}
	if srv.ID == 0 {
		t.Error("expected ID to be set after create")
	}

	got, err := store.GetServer(srv.ID)
	if err != nil {
		t.Fatalf("GetServer failed: %v", err)
	}
	if got.Name != "test-server" {
		t.Errorf("expected name test-server, got %s", got.Name)
	}
	if got.Host != "irc.example.com" {
		t.Errorf("expected host irc.example.com, got %s", got.Host)
	}
	if !got.SSL {
		t.Error("expected SSL to be true")
	}
	if len(got.AltNicknames) != 2 || got.AltNicknames[0] != "testbot_" {
		t.Errorf("unexpected alt_nicknames: %v", got.AltNicknames)
	}
	if got.AuthMethod != "nickserv" {
		t.Errorf("expected auth_method nickserv, got %s", got.AuthMethod)
	}
}

func TestSQLiteStore_ListServers(t *testing.T) {
	store := newTestSQLiteStore(t)

	store.CreateServer(&Server{Name: "srv1", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true})
	store.CreateServer(&Server{Name: "srv2", Host: "b.com", Port: 6697, Nickname: "bot", Enabled: true})

	servers, err := store.GetServers()
	if err != nil {
		t.Fatalf("GetServers failed: %v", err)
	}
	if len(servers) != 2 {
		t.Errorf("expected 2 servers, got %d", len(servers))
	}
}

func TestSQLiteStore_UpdateServer(t *testing.T) {
	store := newTestSQLiteStore(t)

	srv := &Server{Name: "old", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	srv.Name = "new"
	srv.Port = 6697
	if err := store.UpdateServer(srv); err != nil {
		t.Fatalf("UpdateServer failed: %v", err)
	}

	got, _ := store.GetServer(srv.ID)
	if got.Name != "new" {
		t.Errorf("expected name new, got %s", got.Name)
	}
	if got.Port != 6697 {
		t.Errorf("expected port 6697, got %d", got.Port)
	}
}

func TestSQLiteStore_DeleteServer(t *testing.T) {
	store := newTestSQLiteStore(t)

	srv := &Server{Name: "deleteme", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	if err := store.DeleteServer(srv.ID); err != nil {
		t.Fatalf("DeleteServer failed: %v", err)
	}

	_, err := store.GetServer(srv.ID)
	if err == nil {
		t.Error("expected error after deleting server")
	}
}

func TestSQLiteStore_CreateAndGetChannel(t *testing.T) {
	store := newTestSQLiteStore(t)

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	ch := &Channel{
		ServerID:        srv.ID,
		Name:            "#test",
		Key:             "secret",
		SearchCommand:   "!search",
		DownloadChannel: "#test-downloads",
		AutoJoin:        true,
		Enabled:         true,
	}

	if err := store.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel failed: %v", err)
	}

	channels, err := store.GetChannels(srv.ID)
	if err != nil {
		t.Fatalf("GetChannels failed: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(channels))
	}
	if channels[0].Name != "#test" {
		t.Errorf("expected channel #test, got %s", channels[0].Name)
	}
	if channels[0].DownloadChannel != "#test-downloads" {
		t.Errorf("expected download_channel #test-downloads, got %s", channels[0].DownloadChannel)
	}
	if channels[0].SearchCommand != "!search" {
		t.Errorf("expected search_command !search, got %s", channels[0].SearchCommand)
	}
}

func TestSQLiteStore_CreateAndGetDownload(t *testing.T) {
	store := newTestSQLiteStore(t)

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	dl := &Download{
		ServerID:   srv.ID,
		Channel:    "#test",
		BotNick:    "xdcc_bot",
		PackNumber: 42,
		Filename:   "movie.mkv",
		Filesize:   1500000000,
		Status:     "queued",
	}

	if err := store.CreateDownload(dl); err != nil {
		t.Fatalf("CreateDownload failed: %v", err)
	}

	downloads, err := store.GetDownloads("")
	if err != nil {
		t.Fatalf("GetDownloads failed: %v", err)
	}
	if len(downloads) != 1 {
		t.Fatalf("expected 1 download, got %d", len(downloads))
	}
	if downloads[0].Filename != "movie.mkv" {
		t.Errorf("expected filename movie.mkv, got %s", downloads[0].Filename)
	}
	if downloads[0].Status != "queued" {
		t.Errorf("expected status queued, got %s", downloads[0].Status)
	}
}

func TestSQLiteStore_GetDownloads_FilterByStatus(t *testing.T) {
	store := newTestSQLiteStore(t)

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	store.CreateDownload(&Download{ServerID: srv.ID, Channel: "#t", BotNick: "b1", PackNumber: 1, Status: "queued"})
	store.CreateDownload(&Download{ServerID: srv.ID, Channel: "#t", BotNick: "b2", PackNumber: 2, Status: "downloading"})
	store.CreateDownload(&Download{ServerID: srv.ID, Channel: "#t", BotNick: "b3", PackNumber: 3, Status: "queued"})

	queued, _ := store.GetDownloads("queued")
	if len(queued) != 2 {
		t.Errorf("expected 2 queued downloads, got %d", len(queued))
	}

	downloading, _ := store.GetDownloads("downloading")
	if len(downloading) != 1 {
		t.Errorf("expected 1 downloading, got %d", len(downloading))
	}

	all, _ := store.GetDownloads("")
	if len(all) != 3 {
		t.Errorf("expected 3 total downloads, got %d", len(all))
	}
}

func TestSQLiteStore_FileRoutingRules(t *testing.T) {
	store := newTestSQLiteStore(t)

	store.CreateFileRoutingRule(&FileRoutingRule{
		Pattern: "*.mkv", DestinationDir: "/media", Priority: 100, Enabled: true,
	})
	store.CreateFileRoutingRule(&FileRoutingRule{
		Pattern: "*", DestinationDir: "/downloads", Priority: 0, Builtin: true, Enabled: true,
	})

	rules, err := store.GetFileRoutingRules()
	if err != nil {
		t.Fatalf("GetFileRoutingRules failed: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	// Should be ordered by priority descending
	if rules[0].Priority != 100 {
		t.Errorf("expected first rule priority 100, got %d", rules[0].Priority)
	}
	if rules[1].Priority != 0 {
		t.Errorf("expected second rule priority 0, got %d", rules[1].Priority)
	}
}

func TestSQLiteStore_PersistsToDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "persist.db")

	store1, _ := NewSQLiteStore(path)
	store1.Migrate()
	store1.CreateServer(&Server{Name: "persist-test", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true})
	store1.Close()

	store2, _ := NewSQLiteStore(path)
	store2.Migrate()
	defer store2.Close()

	servers, _ := store2.GetServers()
	if len(servers) != 1 {
		t.Fatalf("expected 1 server after reopen, got %d", len(servers))
	}
	if servers[0].Name != "persist-test" {
		t.Errorf("expected name persist-test, got %s", servers[0].Name)
	}
	_ = os.Remove(path)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd db && go test -v -run TestSQLiteStore ./...
```

Expected: compilation error — `SQLiteStore` and `NewSQLiteStore` don't exist.

- [ ] **Step 3: Implement SQLite store**

Create `db/sqlite.go`:

```go
package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("creating db directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(wal)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("opening sqlite: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging sqlite: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) Migrate() error {
	return runMigrations(s.db)
}

// --- Servers ---

func (s *SQLiteStore) GetServers() ([]Server, error) {
	rows, err := s.db.Query("SELECT id, name, host, port, ssl, nickname, alt_nicknames, auth_method, auth_password, auto_connect, enabled, created_at, updated_at FROM servers ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []Server
	for rows.Next() {
		var srv Server
		var altJSON string
		err := rows.Scan(&srv.ID, &srv.Name, &srv.Host, &srv.Port, &srv.SSL, &srv.Nickname,
			&altJSON, &srv.AuthMethod, &srv.AuthPassword, &srv.AutoConnect, &srv.Enabled,
			&srv.CreatedAt, &srv.UpdatedAt)
		if err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(altJSON), &srv.AltNicknames)
		if srv.AltNicknames == nil {
			srv.AltNicknames = []string{}
		}
		servers = append(servers, srv)
	}
	return servers, rows.Err()
}

func (s *SQLiteStore) GetServer(id int64) (*Server, error) {
	var srv Server
	var altJSON string
	err := s.db.QueryRow(
		"SELECT id, name, host, port, ssl, nickname, alt_nicknames, auth_method, auth_password, auto_connect, enabled, created_at, updated_at FROM servers WHERE id=?", id,
	).Scan(&srv.ID, &srv.Name, &srv.Host, &srv.Port, &srv.SSL, &srv.Nickname,
		&altJSON, &srv.AuthMethod, &srv.AuthPassword, &srv.AutoConnect, &srv.Enabled,
		&srv.CreatedAt, &srv.UpdatedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(altJSON), &srv.AltNicknames)
	if srv.AltNicknames == nil {
		srv.AltNicknames = []string{}
	}
	return &srv, nil
}

func (s *SQLiteStore) CreateServer(srv *Server) error {
	altJSON, _ := json.Marshal(srv.AltNicknames)
	now := time.Now()
	result, err := s.db.Exec(
		`INSERT INTO servers (name, host, port, ssl, nickname, alt_nicknames, auth_method, auth_password, auto_connect, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		srv.Name, srv.Host, srv.Port, srv.SSL, srv.Nickname, string(altJSON),
		srv.AuthMethod, srv.AuthPassword, srv.AutoConnect, srv.Enabled, now, now,
	)
	if err != nil {
		return err
	}
	srv.ID, _ = result.LastInsertId()
	srv.CreatedAt = now
	srv.UpdatedAt = now
	return nil
}

func (s *SQLiteStore) UpdateServer(srv *Server) error {
	altJSON, _ := json.Marshal(srv.AltNicknames)
	now := time.Now()
	_, err := s.db.Exec(
		`UPDATE servers SET name=?, host=?, port=?, ssl=?, nickname=?, alt_nicknames=?, auth_method=?, auth_password=?, auto_connect=?, enabled=?, updated_at=? WHERE id=?`,
		srv.Name, srv.Host, srv.Port, srv.SSL, srv.Nickname, string(altJSON),
		srv.AuthMethod, srv.AuthPassword, srv.AutoConnect, srv.Enabled, now, srv.ID,
	)
	if err == nil {
		srv.UpdatedAt = now
	}
	return err
}

func (s *SQLiteStore) DeleteServer(id int64) error {
	_, err := s.db.Exec("DELETE FROM servers WHERE id=?", id)
	return err
}

// --- Channels ---

func (s *SQLiteStore) GetChannels(serverID int64) ([]Channel, error) {
	rows, err := s.db.Query(
		"SELECT id, server_id, name, key, search_command, download_channel, auto_join, enabled FROM channels WHERE server_id=? ORDER BY name", serverID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []Channel
	for rows.Next() {
		var ch Channel
		if err := rows.Scan(&ch.ID, &ch.ServerID, &ch.Name, &ch.Key, &ch.SearchCommand, &ch.DownloadChannel, &ch.AutoJoin, &ch.Enabled); err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

func (s *SQLiteStore) GetChannel(id int64) (*Channel, error) {
	var ch Channel
	err := s.db.QueryRow(
		"SELECT id, server_id, name, key, search_command, download_channel, auto_join, enabled FROM channels WHERE id=?", id,
	).Scan(&ch.ID, &ch.ServerID, &ch.Name, &ch.Key, &ch.SearchCommand, &ch.DownloadChannel, &ch.AutoJoin, &ch.Enabled)
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

func (s *SQLiteStore) CreateChannel(ch *Channel) error {
	result, err := s.db.Exec(
		`INSERT INTO channels (server_id, name, key, search_command, download_channel, auto_join, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ch.ServerID, ch.Name, ch.Key, ch.SearchCommand, ch.DownloadChannel, ch.AutoJoin, ch.Enabled,
	)
	if err != nil {
		return err
	}
	ch.ID, _ = result.LastInsertId()
	return nil
}

func (s *SQLiteStore) UpdateChannel(ch *Channel) error {
	_, err := s.db.Exec(
		`UPDATE channels SET name=?, key=?, search_command=?, download_channel=?, auto_join=?, enabled=? WHERE id=?`,
		ch.Name, ch.Key, ch.SearchCommand, ch.DownloadChannel, ch.AutoJoin, ch.Enabled, ch.ID,
	)
	return err
}

func (s *SQLiteStore) DeleteChannel(id int64) error {
	_, err := s.db.Exec("DELETE FROM channels WHERE id=?", id)
	return err
}

// --- Downloads ---

func (s *SQLiteStore) GetDownloads(status string) ([]Download, error) {
	var rows *sql.Rows
	var err error
	query := "SELECT id, server_id, channel, bot_nick, pack_number, filename, filesize, downloaded_bytes, status, destination_path, error_message, peak_speed, average_speed, started_at, completed_at, created_at FROM downloads"
	if status != "" {
		rows, err = s.db.Query(query+" WHERE status=? ORDER BY created_at DESC", status)
	} else {
		rows, err = s.db.Query(query + " ORDER BY created_at DESC")
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var downloads []Download
	for rows.Next() {
		var dl Download
		if err := rows.Scan(&dl.ID, &dl.ServerID, &dl.Channel, &dl.BotNick, &dl.PackNumber,
			&dl.Filename, &dl.Filesize, &dl.DownloadedBytes, &dl.Status, &dl.DestinationPath,
			&dl.ErrorMessage, &dl.PeakSpeed, &dl.AverageSpeed, &dl.StartedAt, &dl.CompletedAt,
			&dl.CreatedAt); err != nil {
			return nil, err
		}
		downloads = append(downloads, dl)
	}
	return downloads, rows.Err()
}

func (s *SQLiteStore) GetDownload(id int64) (*Download, error) {
	var dl Download
	err := s.db.QueryRow(
		"SELECT id, server_id, channel, bot_nick, pack_number, filename, filesize, downloaded_bytes, status, destination_path, error_message, peak_speed, average_speed, started_at, completed_at, created_at FROM downloads WHERE id=?", id,
	).Scan(&dl.ID, &dl.ServerID, &dl.Channel, &dl.BotNick, &dl.PackNumber,
		&dl.Filename, &dl.Filesize, &dl.DownloadedBytes, &dl.Status, &dl.DestinationPath,
		&dl.ErrorMessage, &dl.PeakSpeed, &dl.AverageSpeed, &dl.StartedAt, &dl.CompletedAt,
		&dl.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &dl, nil
}

func (s *SQLiteStore) CreateDownload(dl *Download) error {
	now := time.Now()
	result, err := s.db.Exec(
		`INSERT INTO downloads (server_id, channel, bot_nick, pack_number, filename, filesize, downloaded_bytes, status, destination_path, error_message, peak_speed, average_speed, started_at, completed_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		dl.ServerID, dl.Channel, dl.BotNick, dl.PackNumber, dl.Filename, dl.Filesize,
		dl.DownloadedBytes, dl.Status, dl.DestinationPath, dl.ErrorMessage,
		dl.PeakSpeed, dl.AverageSpeed, dl.StartedAt, dl.CompletedAt, now,
	)
	if err != nil {
		return err
	}
	dl.ID, _ = result.LastInsertId()
	dl.CreatedAt = now
	return nil
}

func (s *SQLiteStore) UpdateDownload(dl *Download) error {
	_, err := s.db.Exec(
		`UPDATE downloads SET channel=?, bot_nick=?, pack_number=?, filename=?, filesize=?, downloaded_bytes=?, status=?, destination_path=?, error_message=?, peak_speed=?, average_speed=?, started_at=?, completed_at=? WHERE id=?`,
		dl.Channel, dl.BotNick, dl.PackNumber, dl.Filename, dl.Filesize,
		dl.DownloadedBytes, dl.Status, dl.DestinationPath, dl.ErrorMessage,
		dl.PeakSpeed, dl.AverageSpeed, dl.StartedAt, dl.CompletedAt, dl.ID,
	)
	return err
}

// --- Search Results ---

func (s *SQLiteStore) GetSearchResults(query string, serverID int64, channel string) ([]SearchResult, error) {
	rows, err := s.db.Query(
		"SELECT id, server_id, channel, bot_nick, pack_number, filename, filesize, downloads_count, raw_line, search_query, parsed, created_at FROM search_results WHERE search_query=? AND server_id=? AND channel=? ORDER BY created_at DESC",
		query, serverID, channel,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.ID, &r.ServerID, &r.Channel, &r.BotNick, &r.PackNumber,
			&r.Filename, &r.Filesize, &r.DownloadsCount, &r.RawLine, &r.SearchQuery,
			&r.Parsed, &r.CreatedAt); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func (s *SQLiteStore) CreateSearchResult(r *SearchResult) error {
	now := time.Now()
	result, err := s.db.Exec(
		`INSERT INTO search_results (server_id, channel, bot_nick, pack_number, filename, filesize, downloads_count, raw_line, search_query, parsed, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ServerID, r.Channel, r.BotNick, r.PackNumber, r.Filename, r.Filesize,
		r.DownloadsCount, r.RawLine, r.SearchQuery, r.Parsed, now,
	)
	if err != nil {
		return err
	}
	r.ID, _ = result.LastInsertId()
	r.CreatedAt = now
	return nil
}

// --- Saved Searches ---

func (s *SQLiteStore) GetSavedSearches() ([]SavedSearch, error) {
	rows, err := s.db.Query("SELECT id, name, server_id, channel, query, created_at FROM saved_searches ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var searches []SavedSearch
	for rows.Next() {
		var ss SavedSearch
		if err := rows.Scan(&ss.ID, &ss.Name, &ss.ServerID, &ss.Channel, &ss.Query, &ss.CreatedAt); err != nil {
			return nil, err
		}
		searches = append(searches, ss)
	}
	return searches, rows.Err()
}

func (s *SQLiteStore) CreateSavedSearch(ss *SavedSearch) error {
	now := time.Now()
	result, err := s.db.Exec(
		"INSERT INTO saved_searches (name, server_id, channel, query, created_at) VALUES (?, ?, ?, ?, ?)",
		ss.Name, ss.ServerID, ss.Channel, ss.Query, now,
	)
	if err != nil {
		return err
	}
	ss.ID, _ = result.LastInsertId()
	ss.CreatedAt = now
	return nil
}

func (s *SQLiteStore) DeleteSavedSearch(id int64) error {
	_, err := s.db.Exec("DELETE FROM saved_searches WHERE id=?", id)
	return err
}

// --- Parse Patterns ---

func (s *SQLiteStore) GetParsePatterns() ([]ParsePattern, error) {
	rows, err := s.db.Query(
		"SELECT id, name, regex, field_mapping, priority, builtin, enabled, match_count, fail_count, last_matched_at, auto_disabled FROM parse_patterns WHERE enabled=1 AND auto_disabled=0 ORDER BY priority DESC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var patterns []ParsePattern
	for rows.Next() {
		var p ParsePattern
		if err := rows.Scan(&p.ID, &p.Name, &p.Regex, &p.FieldMapping, &p.Priority,
			&p.Builtin, &p.Enabled, &p.MatchCount, &p.FailCount, &p.LastMatchedAt,
			&p.AutoDisabled); err != nil {
			return nil, err
		}
		patterns = append(patterns, p)
	}
	return patterns, rows.Err()
}

func (s *SQLiteStore) CreateParsePattern(p *ParsePattern) error {
	result, err := s.db.Exec(
		`INSERT INTO parse_patterns (name, regex, field_mapping, priority, builtin, enabled, match_count, fail_count, auto_disabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Regex, p.FieldMapping, p.Priority, p.Builtin, p.Enabled,
		p.MatchCount, p.FailCount, p.AutoDisabled,
	)
	if err != nil {
		return err
	}
	p.ID, _ = result.LastInsertId()
	return nil
}

func (s *SQLiteStore) UpdateParsePattern(p *ParsePattern) error {
	_, err := s.db.Exec(
		`UPDATE parse_patterns SET name=?, regex=?, field_mapping=?, priority=?, enabled=?, match_count=?, fail_count=?, last_matched_at=?, auto_disabled=? WHERE id=?`,
		p.Name, p.Regex, p.FieldMapping, p.Priority, p.Enabled,
		p.MatchCount, p.FailCount, p.LastMatchedAt, p.AutoDisabled, p.ID,
	)
	return err
}

// --- Post Hooks ---

func (s *SQLiteStore) GetPostHooks(scope string, scopeID *int64) ([]PostHook, error) {
	var rows *sql.Rows
	var err error
	if scope == "" {
		rows, err = s.db.Query("SELECT id, name, scope, scope_id, hook_type, config, enabled FROM post_hooks WHERE enabled=1 ORDER BY id")
	} else if scopeID != nil {
		rows, err = s.db.Query("SELECT id, name, scope, scope_id, hook_type, config, enabled FROM post_hooks WHERE enabled=1 AND scope=? AND scope_id=? ORDER BY id", scope, *scopeID)
	} else {
		rows, err = s.db.Query("SELECT id, name, scope, scope_id, hook_type, config, enabled FROM post_hooks WHERE enabled=1 AND scope=? ORDER BY id", scope)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hooks []PostHook
	for rows.Next() {
		var h PostHook
		if err := rows.Scan(&h.ID, &h.Name, &h.Scope, &h.ScopeID, &h.HookType, &h.Config, &h.Enabled); err != nil {
			return nil, err
		}
		hooks = append(hooks, h)
	}
	return hooks, rows.Err()
}

func (s *SQLiteStore) CreatePostHook(h *PostHook) error {
	result, err := s.db.Exec(
		"INSERT INTO post_hooks (name, scope, scope_id, hook_type, config, enabled) VALUES (?, ?, ?, ?, ?, ?)",
		h.Name, h.Scope, h.ScopeID, h.HookType, h.Config, h.Enabled,
	)
	if err != nil {
		return err
	}
	h.ID, _ = result.LastInsertId()
	return nil
}

func (s *SQLiteStore) UpdatePostHook(h *PostHook) error {
	_, err := s.db.Exec(
		"UPDATE post_hooks SET name=?, scope=?, scope_id=?, hook_type=?, config=?, enabled=? WHERE id=?",
		h.Name, h.Scope, h.ScopeID, h.HookType, h.Config, h.Enabled, h.ID,
	)
	return err
}

func (s *SQLiteStore) DeletePostHook(id int64) error {
	_, err := s.db.Exec("DELETE FROM post_hooks WHERE id=?", id)
	return err
}

// --- File Routing Rules ---

func (s *SQLiteStore) GetFileRoutingRules() ([]FileRoutingRule, error) {
	rows, err := s.db.Query("SELECT id, pattern, destination_dir, priority, builtin, enabled FROM file_routing_rules WHERE enabled=1 ORDER BY priority DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []FileRoutingRule
	for rows.Next() {
		var r FileRoutingRule
		if err := rows.Scan(&r.ID, &r.Pattern, &r.DestinationDir, &r.Priority, &r.Builtin, &r.Enabled); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

func (s *SQLiteStore) CreateFileRoutingRule(r *FileRoutingRule) error {
	result, err := s.db.Exec(
		"INSERT INTO file_routing_rules (pattern, destination_dir, priority, builtin, enabled) VALUES (?, ?, ?, ?, ?)",
		r.Pattern, r.DestinationDir, r.Priority, r.Builtin, r.Enabled,
	)
	if err != nil {
		return err
	}
	r.ID, _ = result.LastInsertId()
	return nil
}

func (s *SQLiteStore) UpdateFileRoutingRule(r *FileRoutingRule) error {
	_, err := s.db.Exec(
		"UPDATE file_routing_rules SET pattern=?, destination_dir=?, priority=?, enabled=? WHERE id=?",
		r.Pattern, r.DestinationDir, r.Priority, r.Enabled, r.ID,
	)
	return err
}

func (s *SQLiteStore) DeleteFileRoutingRule(id int64) error {
	_, err := s.db.Exec("DELETE FROM file_routing_rules WHERE id=?", id)
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
cd db && go test -v -run TestSQLiteStore ./...
```

Expected: all SQLite store tests PASS.

- [ ] **Step 5: Commit**

```bash
git add db/sqlite.go db/sqlite_test.go go.mod go.sum
git commit -m "feat: add SQLite store implementation"
```

---

### Task 7: MySQL/MariaDB Store Implementation

**Files:**
- Create: `db/mysql.go`
- Create: `db/mysql_test.go`

- [ ] **Step 1: Write failing tests for MySQL store**

Create `db/mysql_test.go`:

```go
package db

import (
	"os"
	"testing"
)

// MySQL tests require a running MariaDB instance.
// Set XIRC_TEST_MYSQL_DSN to enable, e.g.:
//   XIRC_TEST_MYSQL_DSN="xirc:password@tcp(localhost:3306)/xirc_test" go test -v -run TestMySQLStore ./...
//
// Without the env var, these tests are skipped.

func mysqlTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("XIRC_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("XIRC_TEST_MYSQL_DSN not set, skipping MySQL tests")
	}
	return dsn
}

func newTestMySQLStore(t *testing.T) *MySQLStore {
	t.Helper()
	dsn := mysqlTestDSN(t)
	store, err := NewMySQLStore(dsn)
	if err != nil {
		t.Fatalf("failed to create mysql store: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}
	// Clean tables for test isolation
	for _, table := range []string{"file_routing_rules", "post_hooks", "parse_patterns", "saved_searches", "search_results", "downloads", "channels", "servers"} {
		store.db.Exec("DELETE FROM " + table)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestMySQLStore_CreateAndGetServer(t *testing.T) {
	store := newTestMySQLStore(t)

	srv := &Server{
		Name:         "test-server",
		Host:         "irc.example.com",
		Port:         6667,
		SSL:          true,
		Nickname:     "testbot",
		AltNicknames: []string{"testbot_", "testbot__"},
		AuthMethod:   "nickserv",
		AuthPassword: "secret",
		AutoConnect:  true,
		Enabled:      true,
	}

	if err := store.CreateServer(srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}
	if srv.ID == 0 {
		t.Error("expected ID to be set after create")
	}

	got, err := store.GetServer(srv.ID)
	if err != nil {
		t.Fatalf("GetServer failed: %v", err)
	}
	if got.Name != "test-server" {
		t.Errorf("expected name test-server, got %s", got.Name)
	}
	if got.Host != "irc.example.com" {
		t.Errorf("expected host irc.example.com, got %s", got.Host)
	}
	if !got.SSL {
		t.Error("expected SSL to be true")
	}
	if len(got.AltNicknames) != 2 {
		t.Errorf("unexpected alt_nicknames: %v", got.AltNicknames)
	}
}

func TestMySQLStore_ListServers(t *testing.T) {
	store := newTestMySQLStore(t)

	store.CreateServer(&Server{Name: "srv1", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true})
	store.CreateServer(&Server{Name: "srv2", Host: "b.com", Port: 6697, Nickname: "bot", Enabled: true})

	servers, err := store.GetServers()
	if err != nil {
		t.Fatalf("GetServers failed: %v", err)
	}
	if len(servers) != 2 {
		t.Errorf("expected 2 servers, got %d", len(servers))
	}
}

func TestMySQLStore_CreateAndGetChannel(t *testing.T) {
	store := newTestMySQLStore(t)

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	ch := &Channel{
		ServerID:        srv.ID,
		Name:            "#test",
		SearchCommand:   "!search",
		DownloadChannel: "#test-dl",
		AutoJoin:        true,
		Enabled:         true,
	}
	if err := store.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel failed: %v", err)
	}

	channels, err := store.GetChannels(srv.ID)
	if err != nil {
		t.Fatalf("GetChannels failed: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(channels))
	}
	if channels[0].DownloadChannel != "#test-dl" {
		t.Errorf("expected download_channel #test-dl, got %s", channels[0].DownloadChannel)
	}
}

func TestMySQLStore_Downloads(t *testing.T) {
	store := newTestMySQLStore(t)

	srv := &Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	store.CreateDownload(&Download{ServerID: srv.ID, Channel: "#t", BotNick: "b1", PackNumber: 1, Status: "queued"})
	store.CreateDownload(&Download{ServerID: srv.ID, Channel: "#t", BotNick: "b2", PackNumber: 2, Status: "downloading"})

	all, _ := store.GetDownloads("")
	if len(all) != 2 {
		t.Errorf("expected 2 downloads, got %d", len(all))
	}

	queued, _ := store.GetDownloads("queued")
	if len(queued) != 1 {
		t.Errorf("expected 1 queued, got %d", len(queued))
	}
}

func TestMySQLStore_FileRoutingRules(t *testing.T) {
	store := newTestMySQLStore(t)

	store.CreateFileRoutingRule(&FileRoutingRule{Pattern: "*.mkv", DestinationDir: "/media", Priority: 100, Enabled: true})
	store.CreateFileRoutingRule(&FileRoutingRule{Pattern: "*", DestinationDir: "/dl", Priority: 0, Builtin: true, Enabled: true})

	rules, _ := store.GetFileRoutingRules()
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].Priority != 100 {
		t.Errorf("expected first rule priority 100, got %d", rules[0].Priority)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd db && go test -v -run TestMySQLStore ./...
```

Expected: compilation error — `MySQLStore` and `NewMySQLStore` don't exist. (Without `XIRC_TEST_MYSQL_DSN`, tests would skip — but the compilation still fails.)

- [ ] **Step 3: Implement MySQL store**

Create `db/mysql.go`:

```go
package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// mysqlMigrationStatements returns MySQL-compatible CREATE TABLE statements.
// The only differences from SQLite: AUTO_INCREMENT instead of AUTOINCREMENT,
// TEXT types for JSON, DATETIME for timestamps, and explicit ENGINE=InnoDB.
func mysqlMigrationStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS servers (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			host VARCHAR(255) NOT NULL,
			port INT NOT NULL DEFAULT 6667,
			ssl BOOLEAN NOT NULL DEFAULT FALSE,
			nickname VARCHAR(255) NOT NULL DEFAULT 'xirc_user',
			alt_nicknames TEXT NOT NULL,
			auth_method VARCHAR(50) NOT NULL DEFAULT 'none',
			auth_password VARCHAR(255) NOT NULL DEFAULT '',
			auto_connect BOOLEAN NOT NULL DEFAULT TRUE,
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS channels (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			server_id BIGINT NOT NULL,
			name VARCHAR(255) NOT NULL,
			` + "`key`" + ` VARCHAR(255) NOT NULL DEFAULT '',
			search_command VARCHAR(50) NOT NULL DEFAULT '!s',
			download_channel VARCHAR(255) NOT NULL DEFAULT '',
			auto_join BOOLEAN NOT NULL DEFAULT TRUE,
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS downloads (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			server_id BIGINT NOT NULL,
			channel VARCHAR(255) NOT NULL,
			bot_nick VARCHAR(255) NOT NULL,
			pack_number INT NOT NULL,
			filename VARCHAR(500) NOT NULL DEFAULT '',
			filesize BIGINT NOT NULL DEFAULT 0,
			downloaded_bytes BIGINT NOT NULL DEFAULT 0,
			status VARCHAR(50) NOT NULL DEFAULT 'queued',
			destination_path VARCHAR(500) NOT NULL DEFAULT '',
			error_message TEXT NOT NULL,
			peak_speed BIGINT NOT NULL DEFAULT 0,
			average_speed BIGINT NOT NULL DEFAULT 0,
			started_at DATETIME,
			completed_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (server_id) REFERENCES servers(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS search_results (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			server_id BIGINT NOT NULL,
			channel VARCHAR(255) NOT NULL,
			bot_nick VARCHAR(255) NOT NULL,
			pack_number INT,
			filename VARCHAR(500),
			filesize VARCHAR(50),
			downloads_count INT,
			raw_line TEXT NOT NULL,
			search_query VARCHAR(255) NOT NULL,
			parsed BOOLEAN NOT NULL DEFAULT FALSE,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (server_id) REFERENCES servers(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS saved_searches (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			server_id BIGINT NOT NULL,
			channel VARCHAR(255) NOT NULL,
			query VARCHAR(255) NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (server_id) REFERENCES servers(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS parse_patterns (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			regex TEXT NOT NULL,
			field_mapping TEXT NOT NULL,
			priority INT NOT NULL DEFAULT 0,
			builtin BOOLEAN NOT NULL DEFAULT FALSE,
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			match_count INT NOT NULL DEFAULT 0,
			fail_count INT NOT NULL DEFAULT 0,
			last_matched_at DATETIME,
			auto_disabled BOOLEAN NOT NULL DEFAULT FALSE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS post_hooks (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			scope VARCHAR(50) NOT NULL DEFAULT 'global',
			scope_id BIGINT,
			hook_type VARCHAR(50) NOT NULL,
			config TEXT NOT NULL,
			enabled BOOLEAN NOT NULL DEFAULT TRUE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS file_routing_rules (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			pattern VARCHAR(255) NOT NULL,
			destination_dir VARCHAR(500) NOT NULL,
			priority INT NOT NULL DEFAULT 0,
			builtin BOOLEAN NOT NULL DEFAULT FALSE,
			enabled BOOLEAN NOT NULL DEFAULT TRUE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	}
}

type MySQLStore struct {
	db *sql.DB
}

func NewMySQLStore(dsn string) (*MySQLStore, error) {
	db, err := sql.Open("mysql", dsn+"?parseTime=true")
	if err != nil {
		return nil, fmt.Errorf("opening mysql: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging mysql: %w", err)
	}

	return &MySQLStore{db: db}, nil
}

func (s *MySQLStore) Close() error {
	return s.db.Close()
}

func (s *MySQLStore) Migrate() error {
	for _, stmt := range mysqlMigrationStatements() {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("migration failed: %w\nSQL: %s", err, stmt)
		}
	}
	return nil
}

// --- Servers ---

func (s *MySQLStore) GetServers() ([]Server, error) {
	rows, err := s.db.Query("SELECT id, name, host, port, ssl, nickname, alt_nicknames, auth_method, auth_password, auto_connect, enabled, created_at, updated_at FROM servers ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []Server
	for rows.Next() {
		var srv Server
		var altJSON string
		err := rows.Scan(&srv.ID, &srv.Name, &srv.Host, &srv.Port, &srv.SSL, &srv.Nickname,
			&altJSON, &srv.AuthMethod, &srv.AuthPassword, &srv.AutoConnect, &srv.Enabled,
			&srv.CreatedAt, &srv.UpdatedAt)
		if err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(altJSON), &srv.AltNicknames)
		if srv.AltNicknames == nil {
			srv.AltNicknames = []string{}
		}
		servers = append(servers, srv)
	}
	return servers, rows.Err()
}

func (s *MySQLStore) GetServer(id int64) (*Server, error) {
	var srv Server
	var altJSON string
	err := s.db.QueryRow(
		"SELECT id, name, host, port, ssl, nickname, alt_nicknames, auth_method, auth_password, auto_connect, enabled, created_at, updated_at FROM servers WHERE id=?", id,
	).Scan(&srv.ID, &srv.Name, &srv.Host, &srv.Port, &srv.SSL, &srv.Nickname,
		&altJSON, &srv.AuthMethod, &srv.AuthPassword, &srv.AutoConnect, &srv.Enabled,
		&srv.CreatedAt, &srv.UpdatedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(altJSON), &srv.AltNicknames)
	if srv.AltNicknames == nil {
		srv.AltNicknames = []string{}
	}
	return &srv, nil
}

func (s *MySQLStore) CreateServer(srv *Server) error {
	altJSON, _ := json.Marshal(srv.AltNicknames)
	if srv.AltNicknames == nil {
		altJSON = []byte("[]")
	}
	now := time.Now()
	result, err := s.db.Exec(
		`INSERT INTO servers (name, host, port, ssl, nickname, alt_nicknames, auth_method, auth_password, auto_connect, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		srv.Name, srv.Host, srv.Port, srv.SSL, srv.Nickname, string(altJSON),
		srv.AuthMethod, srv.AuthPassword, srv.AutoConnect, srv.Enabled, now, now,
	)
	if err != nil {
		return err
	}
	srv.ID, _ = result.LastInsertId()
	srv.CreatedAt = now
	srv.UpdatedAt = now
	return nil
}

func (s *MySQLStore) UpdateServer(srv *Server) error {
	altJSON, _ := json.Marshal(srv.AltNicknames)
	now := time.Now()
	_, err := s.db.Exec(
		`UPDATE servers SET name=?, host=?, port=?, ssl=?, nickname=?, alt_nicknames=?, auth_method=?, auth_password=?, auto_connect=?, enabled=?, updated_at=? WHERE id=?`,
		srv.Name, srv.Host, srv.Port, srv.SSL, srv.Nickname, string(altJSON),
		srv.AuthMethod, srv.AuthPassword, srv.AutoConnect, srv.Enabled, now, srv.ID,
	)
	if err == nil {
		srv.UpdatedAt = now
	}
	return err
}

func (s *MySQLStore) DeleteServer(id int64) error {
	_, err := s.db.Exec("DELETE FROM servers WHERE id=?", id)
	return err
}

// --- Channels ---

func (s *MySQLStore) GetChannels(serverID int64) ([]Channel, error) {
	rows, err := s.db.Query(
		"SELECT id, server_id, name, `key`, search_command, download_channel, auto_join, enabled FROM channels WHERE server_id=? ORDER BY name", serverID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []Channel
	for rows.Next() {
		var ch Channel
		if err := rows.Scan(&ch.ID, &ch.ServerID, &ch.Name, &ch.Key, &ch.SearchCommand, &ch.DownloadChannel, &ch.AutoJoin, &ch.Enabled); err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

func (s *MySQLStore) GetChannel(id int64) (*Channel, error) {
	var ch Channel
	err := s.db.QueryRow(
		"SELECT id, server_id, name, `key`, search_command, download_channel, auto_join, enabled FROM channels WHERE id=?", id,
	).Scan(&ch.ID, &ch.ServerID, &ch.Name, &ch.Key, &ch.SearchCommand, &ch.DownloadChannel, &ch.AutoJoin, &ch.Enabled)
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

func (s *MySQLStore) CreateChannel(ch *Channel) error {
	result, err := s.db.Exec(
		"INSERT INTO channels (server_id, name, `key`, search_command, download_channel, auto_join, enabled) VALUES (?, ?, ?, ?, ?, ?, ?)",
		ch.ServerID, ch.Name, ch.Key, ch.SearchCommand, ch.DownloadChannel, ch.AutoJoin, ch.Enabled,
	)
	if err != nil {
		return err
	}
	ch.ID, _ = result.LastInsertId()
	return nil
}

func (s *MySQLStore) UpdateChannel(ch *Channel) error {
	_, err := s.db.Exec(
		"UPDATE channels SET name=?, `key`=?, search_command=?, download_channel=?, auto_join=?, enabled=? WHERE id=?",
		ch.Name, ch.Key, ch.SearchCommand, ch.DownloadChannel, ch.AutoJoin, ch.Enabled, ch.ID,
	)
	return err
}

func (s *MySQLStore) DeleteChannel(id int64) error {
	_, err := s.db.Exec("DELETE FROM channels WHERE id=?", id)
	return err
}

// --- Downloads ---

func (s *MySQLStore) GetDownloads(status string) ([]Download, error) {
	query := "SELECT id, server_id, channel, bot_nick, pack_number, filename, filesize, downloaded_bytes, status, destination_path, error_message, peak_speed, average_speed, started_at, completed_at, created_at FROM downloads"
	var rows *sql.Rows
	var err error
	if status != "" {
		rows, err = s.db.Query(query+" WHERE status=? ORDER BY created_at DESC", status)
	} else {
		rows, err = s.db.Query(query + " ORDER BY created_at DESC")
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var downloads []Download
	for rows.Next() {
		var dl Download
		if err := rows.Scan(&dl.ID, &dl.ServerID, &dl.Channel, &dl.BotNick, &dl.PackNumber,
			&dl.Filename, &dl.Filesize, &dl.DownloadedBytes, &dl.Status, &dl.DestinationPath,
			&dl.ErrorMessage, &dl.PeakSpeed, &dl.AverageSpeed, &dl.StartedAt, &dl.CompletedAt,
			&dl.CreatedAt); err != nil {
			return nil, err
		}
		downloads = append(downloads, dl)
	}
	return downloads, rows.Err()
}

func (s *MySQLStore) GetDownload(id int64) (*Download, error) {
	var dl Download
	err := s.db.QueryRow(
		"SELECT id, server_id, channel, bot_nick, pack_number, filename, filesize, downloaded_bytes, status, destination_path, error_message, peak_speed, average_speed, started_at, completed_at, created_at FROM downloads WHERE id=?", id,
	).Scan(&dl.ID, &dl.ServerID, &dl.Channel, &dl.BotNick, &dl.PackNumber,
		&dl.Filename, &dl.Filesize, &dl.DownloadedBytes, &dl.Status, &dl.DestinationPath,
		&dl.ErrorMessage, &dl.PeakSpeed, &dl.AverageSpeed, &dl.StartedAt, &dl.CompletedAt,
		&dl.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &dl, nil
}

func (s *MySQLStore) CreateDownload(dl *Download) error {
	now := time.Now()
	result, err := s.db.Exec(
		`INSERT INTO downloads (server_id, channel, bot_nick, pack_number, filename, filesize, downloaded_bytes, status, destination_path, error_message, peak_speed, average_speed, started_at, completed_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		dl.ServerID, dl.Channel, dl.BotNick, dl.PackNumber, dl.Filename, dl.Filesize,
		dl.DownloadedBytes, dl.Status, dl.DestinationPath, dl.ErrorMessage,
		dl.PeakSpeed, dl.AverageSpeed, dl.StartedAt, dl.CompletedAt, now,
	)
	if err != nil {
		return err
	}
	dl.ID, _ = result.LastInsertId()
	dl.CreatedAt = now
	return nil
}

func (s *MySQLStore) UpdateDownload(dl *Download) error {
	_, err := s.db.Exec(
		`UPDATE downloads SET channel=?, bot_nick=?, pack_number=?, filename=?, filesize=?, downloaded_bytes=?, status=?, destination_path=?, error_message=?, peak_speed=?, average_speed=?, started_at=?, completed_at=? WHERE id=?`,
		dl.Channel, dl.BotNick, dl.PackNumber, dl.Filename, dl.Filesize,
		dl.DownloadedBytes, dl.Status, dl.DestinationPath, dl.ErrorMessage,
		dl.PeakSpeed, dl.AverageSpeed, dl.StartedAt, dl.CompletedAt, dl.ID,
	)
	return err
}

// --- Search Results ---

func (s *MySQLStore) GetSearchResults(query string, serverID int64, channel string) ([]SearchResult, error) {
	rows, err := s.db.Query(
		"SELECT id, server_id, channel, bot_nick, pack_number, filename, filesize, downloads_count, raw_line, search_query, parsed, created_at FROM search_results WHERE search_query=? AND server_id=? AND channel=? ORDER BY created_at DESC",
		query, serverID, channel,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.ID, &r.ServerID, &r.Channel, &r.BotNick, &r.PackNumber,
			&r.Filename, &r.Filesize, &r.DownloadsCount, &r.RawLine, &r.SearchQuery,
			&r.Parsed, &r.CreatedAt); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func (s *MySQLStore) CreateSearchResult(r *SearchResult) error {
	now := time.Now()
	result, err := s.db.Exec(
		`INSERT INTO search_results (server_id, channel, bot_nick, pack_number, filename, filesize, downloads_count, raw_line, search_query, parsed, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ServerID, r.Channel, r.BotNick, r.PackNumber, r.Filename, r.Filesize,
		r.DownloadsCount, r.RawLine, r.SearchQuery, r.Parsed, now,
	)
	if err != nil {
		return err
	}
	r.ID, _ = result.LastInsertId()
	r.CreatedAt = now
	return nil
}

// --- Saved Searches ---

func (s *MySQLStore) GetSavedSearches() ([]SavedSearch, error) {
	rows, err := s.db.Query("SELECT id, name, server_id, channel, query, created_at FROM saved_searches ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var searches []SavedSearch
	for rows.Next() {
		var ss SavedSearch
		if err := rows.Scan(&ss.ID, &ss.Name, &ss.ServerID, &ss.Channel, &ss.Query, &ss.CreatedAt); err != nil {
			return nil, err
		}
		searches = append(searches, ss)
	}
	return searches, rows.Err()
}

func (s *MySQLStore) CreateSavedSearch(ss *SavedSearch) error {
	now := time.Now()
	result, err := s.db.Exec(
		"INSERT INTO saved_searches (name, server_id, channel, query, created_at) VALUES (?, ?, ?, ?, ?)",
		ss.Name, ss.ServerID, ss.Channel, ss.Query, now,
	)
	if err != nil {
		return err
	}
	ss.ID, _ = result.LastInsertId()
	ss.CreatedAt = now
	return nil
}

func (s *MySQLStore) DeleteSavedSearch(id int64) error {
	_, err := s.db.Exec("DELETE FROM saved_searches WHERE id=?", id)
	return err
}

// --- Parse Patterns ---

func (s *MySQLStore) GetParsePatterns() ([]ParsePattern, error) {
	rows, err := s.db.Query(
		"SELECT id, name, regex, field_mapping, priority, builtin, enabled, match_count, fail_count, last_matched_at, auto_disabled FROM parse_patterns WHERE enabled=1 AND auto_disabled=0 ORDER BY priority DESC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var patterns []ParsePattern
	for rows.Next() {
		var p ParsePattern
		if err := rows.Scan(&p.ID, &p.Name, &p.Regex, &p.FieldMapping, &p.Priority,
			&p.Builtin, &p.Enabled, &p.MatchCount, &p.FailCount, &p.LastMatchedAt,
			&p.AutoDisabled); err != nil {
			return nil, err
		}
		patterns = append(patterns, p)
	}
	return patterns, rows.Err()
}

func (s *MySQLStore) CreateParsePattern(p *ParsePattern) error {
	result, err := s.db.Exec(
		`INSERT INTO parse_patterns (name, regex, field_mapping, priority, builtin, enabled, match_count, fail_count, auto_disabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Regex, p.FieldMapping, p.Priority, p.Builtin, p.Enabled,
		p.MatchCount, p.FailCount, p.AutoDisabled,
	)
	if err != nil {
		return err
	}
	p.ID, _ = result.LastInsertId()
	return nil
}

func (s *MySQLStore) UpdateParsePattern(p *ParsePattern) error {
	_, err := s.db.Exec(
		`UPDATE parse_patterns SET name=?, regex=?, field_mapping=?, priority=?, enabled=?, match_count=?, fail_count=?, last_matched_at=?, auto_disabled=? WHERE id=?`,
		p.Name, p.Regex, p.FieldMapping, p.Priority, p.Enabled,
		p.MatchCount, p.FailCount, p.LastMatchedAt, p.AutoDisabled, p.ID,
	)
	return err
}

// --- Post Hooks ---

func (s *MySQLStore) GetPostHooks(scope string, scopeID *int64) ([]PostHook, error) {
	var rows *sql.Rows
	var err error
	if scope == "" {
		rows, err = s.db.Query("SELECT id, name, scope, scope_id, hook_type, config, enabled FROM post_hooks WHERE enabled=1 ORDER BY id")
	} else if scopeID != nil {
		rows, err = s.db.Query("SELECT id, name, scope, scope_id, hook_type, config, enabled FROM post_hooks WHERE enabled=1 AND scope=? AND scope_id=? ORDER BY id", scope, *scopeID)
	} else {
		rows, err = s.db.Query("SELECT id, name, scope, scope_id, hook_type, config, enabled FROM post_hooks WHERE enabled=1 AND scope=? ORDER BY id", scope)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hooks []PostHook
	for rows.Next() {
		var h PostHook
		if err := rows.Scan(&h.ID, &h.Name, &h.Scope, &h.ScopeID, &h.HookType, &h.Config, &h.Enabled); err != nil {
			return nil, err
		}
		hooks = append(hooks, h)
	}
	return hooks, rows.Err()
}

func (s *MySQLStore) CreatePostHook(h *PostHook) error {
	result, err := s.db.Exec(
		"INSERT INTO post_hooks (name, scope, scope_id, hook_type, config, enabled) VALUES (?, ?, ?, ?, ?, ?)",
		h.Name, h.Scope, h.ScopeID, h.HookType, h.Config, h.Enabled,
	)
	if err != nil {
		return err
	}
	h.ID, _ = result.LastInsertId()
	return nil
}

func (s *MySQLStore) UpdatePostHook(h *PostHook) error {
	_, err := s.db.Exec(
		"UPDATE post_hooks SET name=?, scope=?, scope_id=?, hook_type=?, config=?, enabled=? WHERE id=?",
		h.Name, h.Scope, h.ScopeID, h.HookType, h.Config, h.Enabled, h.ID,
	)
	return err
}

func (s *MySQLStore) DeletePostHook(id int64) error {
	_, err := s.db.Exec("DELETE FROM post_hooks WHERE id=?", id)
	return err
}

// --- File Routing Rules ---

func (s *MySQLStore) GetFileRoutingRules() ([]FileRoutingRule, error) {
	rows, err := s.db.Query("SELECT id, pattern, destination_dir, priority, builtin, enabled FROM file_routing_rules WHERE enabled=1 ORDER BY priority DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []FileRoutingRule
	for rows.Next() {
		var r FileRoutingRule
		if err := rows.Scan(&r.ID, &r.Pattern, &r.DestinationDir, &r.Priority, &r.Builtin, &r.Enabled); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

func (s *MySQLStore) CreateFileRoutingRule(r *FileRoutingRule) error {
	result, err := s.db.Exec(
		"INSERT INTO file_routing_rules (pattern, destination_dir, priority, builtin, enabled) VALUES (?, ?, ?, ?, ?)",
		r.Pattern, r.DestinationDir, r.Priority, r.Builtin, r.Enabled,
	)
	if err != nil {
		return err
	}
	r.ID, _ = result.LastInsertId()
	return nil
}

func (s *MySQLStore) UpdateFileRoutingRule(r *FileRoutingRule) error {
	_, err := s.db.Exec(
		"UPDATE file_routing_rules SET pattern=?, destination_dir=?, priority=?, enabled=? WHERE id=?",
		r.Pattern, r.DestinationDir, r.Priority, r.Enabled, r.ID,
	)
	return err
}

func (s *MySQLStore) DeleteFileRoutingRule(id int64) error {
	_, err := s.db.Exec("DELETE FROM file_routing_rules WHERE id=?", id)
	return err
}
```

- [ ] **Step 4: Install dependency and verify compilation**

Run:
```bash
go get github.com/go-sql-driver/mysql
go build ./db/...
```

Expected: compiles without error. (MySQL tests will skip without `XIRC_TEST_MYSQL_DSN`.)

- [ ] **Step 5: Run all DB tests**

Run:
```bash
cd db && go test -v ./...
```

Expected: all SQLite tests PASS, all MySQL tests SKIP (no DSN set).

- [ ] **Step 6: Commit**

```bash
git add db/mysql.go db/mysql_test.go go.mod go.sum
git commit -m "feat: add MySQL/MariaDB store implementation"
```

---

### Task 8: Store Factory

**Files:**
- Create: `db/factory.go`
- Create: `db/factory_test.go`

- [ ] **Step 1: Write failing test for factory**

Create `db/factory_test.go`:

```go
package db

import (
	"path/filepath"
	"testing"
)

func TestNewStore_SQLite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "factory.db")

	store, err := NewStore("sqlite", path)
	if err != nil {
		t.Fatalf("NewStore sqlite failed: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// Verify it works by creating a server
	err = store.CreateServer(&Server{Name: "test", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true})
	if err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	servers, err := store.GetServers()
	if err != nil {
		t.Fatalf("GetServers failed: %v", err)
	}
	if len(servers) != 1 {
		t.Errorf("expected 1 server, got %d", len(servers))
	}
}

func TestNewStore_UnknownDriver(t *testing.T) {
	_, err := NewStore("postgres", "something")
	if err == nil {
		t.Error("expected error for unknown driver")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd db && go test -v -run TestNewStore ./...
```

Expected: compilation error — `NewStore` doesn't exist.

- [ ] **Step 3: Implement store factory**

Create `db/factory.go`:

```go
package db

import "fmt"

// NewStore creates a Store backed by the given driver.
// For "sqlite", dsn is the file path.
// For "mysql", dsn is a go-sql-driver/mysql DSN string.
func NewStore(driver, dsn string) (Store, error) {
	switch driver {
	case "sqlite":
		return NewSQLiteStore(dsn)
	case "mysql":
		return NewMySQLStore(dsn)
	default:
		return nil, fmt.Errorf("unsupported database driver: %s (expected 'sqlite' or 'mysql')", driver)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
cd db && go test -v -run TestNewStore ./...
```

Expected: both tests PASS.

- [ ] **Step 5: Commit**

```bash
git add db/factory.go db/factory_test.go
git commit -m "feat: add store factory for driver selection"
```

---

### Task 9: HTTP Server with Health Endpoint

**Files:**
- Create: `server/server.go`
- Create: `server/server_test.go`

- [ ] **Step 1: Write failing tests for HTTP server**

Create `server/server_test.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	srv := New(nil)
	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %s", resp["status"])
	}
}

func TestNotFoundReturns404(t *testing.T) {
	srv := New(nil)
	req := httptest.NewRequest("GET", "/api/nonexistent", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd server && go test -v ./...
```

Expected: compilation error — `server` package doesn't exist.

- [ ] **Step 3: Implement HTTP server**

Create `server/server.go`:

```go
package server

import (
	"encoding/json"
	"net/http"

	"github.com/maxwell-xirc/xirc/db"
)

type Server struct {
	store db.Store
	mux   *http.ServeMux
}

func New(store db.Store) *Server {
	s := &Server{
		store: store,
		mux:   http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
cd server && go test -v ./...
```

Expected: both tests PASS.

- [ ] **Step 5: Commit**

```bash
git add server/
git commit -m "feat: add HTTP server with health endpoint"
```

---

### Task 10: Wire Everything Together in main.go

**Files:**
- Modify: `main.go`
- Create: `config.yaml`

- [ ] **Step 1: Create example config file**

Create `config.yaml`:

```yaml
server:
  host: 127.0.0.1
  port: 8085

database:
  driver: sqlite
  path: ./data/xirc.db

storage:
  media_dir: /srv/dlna/media
  downloads_dir: /srv/downloads
  temp_dir: /srv/downloads/.tmp
  min_free_space: 1GB
  critical_free_space: 500MB

dcc:
  passive_enabled: false
  passive_ports: "30000-30010"
  external_ip: ""

downloads:
  max_concurrent: 3

notifications:
  quiet_hours_start: ""
  quiet_hours_end: ""
```

- [ ] **Step 2: Update main.go to wire config, DB, and server**

Replace `main.go` with:

```go
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/maxwell-xirc/xirc/config"
	"github.com/maxwell-xirc/xirc/db"
	"github.com/maxwell-xirc/xirc/server"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	dsn := cfg.Database.Path
	if cfg.Database.Driver == "mysql" {
		dsn = cfg.Database.DSN
	}

	store, err := db.NewStore(cfg.Database.Driver, dsn)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	srv := server.New(store)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	httpServer := &http.Server{
		Addr:    addr,
		Handler: srv.Handler(),
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down...")
		httpServer.Close()
	}()

	log.Printf("xirc starting on %s", addr)
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
```

- [ ] **Step 3: Verify it compiles**

Run:
```bash
go build -o xirc .
```

Expected: binary `xirc` is created.

- [ ] **Step 4: Run it and test the health endpoint**

Run in one terminal:
```bash
./xirc --config config.yaml
```

Expected output: `xirc starting on 127.0.0.1:8085`

Test in another terminal (or use curl):
```bash
curl http://127.0.0.1:8085/api/health
```

Expected: `{"status":"ok"}`

Then Ctrl+C the server. Expected: `shutting down...`

- [ ] **Step 5: Create .gitignore**

Create `.gitignore`:

```
xirc
data/
*.db
.superpowers/
```

- [ ] **Step 6: Run all tests one final time**

Run:
```bash
go test ./...
```

Expected: all tests in `config/`, `db/`, `server/` PASS.

- [ ] **Step 7: Commit**

```bash
git add main.go config.yaml .gitignore
git commit -m "feat: wire config, database, and HTTP server in main"
```

---

## End State

After completing Plan 1, you have:

- A Go binary that starts, loads config from YAML (with env overrides), connects to SQLite or MariaDB, runs schema migrations, and serves a health endpoint on HTTP.
- Full test coverage for config loading, both database backends, and the HTTP server.
- Clean separation: `config/`, `db/`, `server/` packages with defined interfaces.
- Ready for Plan 2 (IRC Core) to add IRC connection management on top of this foundation.
