# Multi-user login, installer, Docker, FS capabilities — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make xirc safe to expose (login, admin/user roles), easy to install (wizard for bare metal with nginx or Apache, Docker image), resilient to missing filesystem permissions, and free of author-specific data — then release `v0.4.0-beta`.

**Architecture:** Auth is a middleware in front of the existing `http.ServeMux` (`server/auth.go`) backed by two new tables (`users`, `sessions`) whose SQL is shared by both stores (`db/users.go`). FS permissions are probed by a new `fscheck` package; the server keeps a `Capabilities` snapshot, exposes it at `/api/capabilities`, pushes changes over the existing event bus → WebSocket. Deployment gets one set of proxy templates (`scripts/proxy-templates.sh`) used by a new on-host `scripts/install.sh`, the existing `scripts/preconfig.sh`, and the checked-in examples.

**Tech Stack:** Go 1.27.1, `golang.org/x/crypto/bcrypt`, SQLite (modernc) + MySQL, Alpine.js SPA, bash, Docker (Alpine 3.24), GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-09-26-multiuser-deploy-design.md`

## Global Constraints

- Build/test only via `make build` / `make test` (Go at `~/go-install/go`; never `/usr/bin/go`). Do **not** start the server.
- Latest stable versions: Go **1.27.1**, Alpine **3.24**, `golang.org/x/crypto@latest`.
- Passwords: bcrypt (`bcrypt.DefaultCost`), per-hash salt is built in. Min length **8**.
- Session token: 32 random bytes, base64url in cookie `xirc_session`; DB stores hex SHA-256 only. Lifetime **30 days**, sliding, touched at most once per hour.
- Roles: exactly `admin` and `user`. Usernames are `strings.ToLower(strings.TrimSpace(x))`, 1–32 chars of `[a-z0-9._-]`.
- Login rate limit: **5** failures per client IP per **1 minute** → 429.
- `auth.trusted_role` default `admin`; `auth.trusted_proxies` default `127.0.0.1/32`, `::1/128`; `auth.trusted_networks` default empty.
- Env override names derive from yaml tags: `XIRC_<SECTION>_<YAML_KEY_UPPER>` (e.g. `XIRC_STORAGE_DOWNLOADS_DIR`); `[]string` fields are comma-separated.
- Installer artefacts are named `xirc` (binary, system user, `xirc.service`, `/opt/xirc`). `make build` output name unchanged.
- Placeholder host `maxwell.local`, placeholder SSH user `maxwell`.
- GitHub repo: `RealDtx/maxwell-xirc`; image `ghcr.io/realdtx/xirc`.
- Builtin parse-pattern **names** are not renamed (seed-upgrade keys).
- Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **Spoofed `X-Forwarded-For` from an untrusted peer** must not grant trusted-network access → test in Task 5.
2. **Cross-site request from a browser on the LAN** (trusted-network principal has no cookie to protect it): a cross-origin `text/plain` POST must get 415, and a cross-origin WebSocket handshake must be rejected → tests in Task 5.
3. **Password reset / user delete** must log that user out everywhere immediately → test in Task 6.
4. **Server started behind a sub-path prefix** — cookie `Path` must be the prefix, otherwise the browser never sends it back → test in Task 6.
5. **Media dir turns read-only while running** (USB unplugged, remount ro): a finished download must still land in `downloads_dir` with a readable reason, never stay in `.tmp`, never 500 → test in Task 10.

## File Structure

| File | Responsibility |
|---|---|
| `config/config.go` | + `AuthConfig`, yaml-tag env names, `[]string` env, missing-file fallback |
| `db/models.go` | + `User`, `Session` |
| `db/users.go` (new) | shared SQL for users/sessions (both stores delegate) |
| `db/migrations.go`, `db/mysql.go` | + `users`, `sessions` tables |
| `db/store.go` | + user/session methods on `Store` |
| `server/auth.go` (new) | `Auth` (config parse), client IP, principal, policy table, middleware |
| `server/auth_handlers.go` (new) | login/logout/me/setup, users CRUD, rate limiter |
| `server/capabilities.go` (new) | capability snapshot, endpoint, recheck, periodic refresh |
| `fscheck/fscheck.go` (new) | `Probe`, `Describe`, `IsPermission` |
| `server/ws_handler.go` | drop permissive `CheckOrigin` |
| `server/files_handler.go`, `server/download_handlers.go`, `server/library_handlers.go` | permission errors → 403 with reason; downloads 503 when disabled |
| `queue/engine.go` | readable move-failure note via `fscheck.Describe` |
| `maintenance/maintenance.go` | prune expired sessions |
| `main.go` | wiring, `--create-admin`, no exit on unwritable dirs |
| `web/js/api.js`, `web/js/app.js`, `web/index.html` | login/setup overlay, `isAdmin` gating, Users tab, capability UI |
| `deploy/Dockerfile`, `deploy/docker-compose.yaml`, `deploy/.env.example` | container |
| `.github/workflows/release.yml` | binaries + GHCR image |
| `scripts/proxy-templates.sh` (new), `scripts/test-proxy-templates.sh` (new) | nginx/Apache renderers |
| `scripts/install.sh` (new) | on-host install wizard |
| `scripts/preconfig.sh`, `Makefile` | use templates, Apache option, `maxwell.local` |
| `deploy/nginx-xirc.conf`, `deploy/apache-xirc.conf` | generated examples (replace `deploy/nginx-maxwell-irc.conf`) |
| `README.md`, `docs/install.md` | install paths |

---

### Task 1: Toolchain upgrade to Go 1.27.1

**Files:**
- Modify: `go.mod` (go directive), `CLAUDE.md` (Go version line)

- [ ] **Step 1: Install Go 1.27.1 into `~/go-install`**

```bash
cd /tmp && curl -fsSLO https://go.dev/dl/go1.27.1.linux-amd64.tar.gz
rm -rf ~/go-install/go && tar -C ~/go-install -xzf go1.27.1.linux-amd64.tar.gz
~/go-install/go/bin/go version   # expect: go version go1.27.1 linux/amd64
```

- [ ] **Step 2: Bump module**

```bash
~/go-install/go/bin/go mod edit -go=1.27.0
~/go-install/go/bin/go mod tidy
```

In `CLAUDE.md` change `(currently 1.26.2)` → `(currently 1.27.1)`.

- [ ] **Step 3: Verify**

Run: `make build && make test`
Expected: build succeeds, all packages `ok`.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum CLAUDE.md
git commit -m "chore: upgrade to Go 1.27.1"
```

---

### Task 2: Personal-info cleanup

**Files:**
- Modify: `Makefile:13-14`, `scripts/preconfig.sh` (SSH host/user defaults), `docs/install.md`, `deploy/docker-compose.yaml` (TZ line), `parser/parser.go:248`, `irc/connection.go:15`, `parser/seed.go:68,74,99` (comments only), `parser/parser_test.go`, `parser/learn_test.go`, `docs/2026-07-08-index-stats-design.md`

- [ ] **Step 1: Replace host/user defaults**

```bash
sed -i 's/PI_HOST     ?= 192.168.20.2/PI_HOST     ?= maxwell.local/; s/PI_USER     ?= pi/PI_USER     ?= maxwell/' Makefile
sed -i 's/"192.168.20.2" PI_HOST/"maxwell.local" PI_HOST/; s/(must have passwordless sudo)" "pi" PI_USER/(must have passwordless sudo)" "maxwell" PI_USER/' scripts/preconfig.sh
sed -i 's/192\.168\.20\.2/maxwell.local/g; s/PI_USER=dtx/PI_USER=maxwell/g; s/`pi` | Must have/`maxwell` | Must have/; s/ssh pi@/ssh maxwell@/g' docs/install.md
sed -i 's/^      - TZ=Europe\/Berlin$/      # - TZ=Europe\/Berlin   # set your timezone/' deploy/docker-compose.yaml
```

- [ ] **Step 2: Neutralise channel/bot names (comments, docs, tests)**

```bash
files="parser/parser.go irc/connection.go parser/seed.go parser/parser_test.go parser/learn_test.go docs/2026-07-08-index-stats-design.md"
sed -i 's/#mg-chat/#example-chat/g; s/#MovieGods/#Example-DL/g; s/#moviegods/#example-dl/g; s/MG-BOT|01/ExampleBot|01/g; s/BotReign/ExampleBot/g; s/\[EWG\]Rich-01/[EX]Bot-01/g; s/\[EWG\]-\[STR8UP\]-2/[EX]-[BOT]-2/g' $files
sed -i 's|// BotReign/search-bot format|// Search-bot pipe format|; s|// EliteWarez/EWG format|// Command-in-parens format|; s|// New-pack announcement (EWG)|// New-pack announcement|' parser/seed.go
```

Do **not** change the `Name:` strings in `parser/seed.go` or `deploy/patterns/common.yaml`, nor `parser/seed_test.go` `check("ewg-command-xdcc", …)`.

- [ ] **Step 3: Verify nothing personal is left**

Run:
```bash
git grep -nIiE '192\.168\.20|dtx\b|Europe/Berlin|moviegods|mg-chat|BotReign|EliteWarez' -- ':!docs/superpowers' ':!go.sum'
```
Expected: only `botreign-pipe-xdcc` pattern-name hits in `parser/seed.go`, `deploy/patterns/common.yaml`, and `parser/seed_test.go` (if any).

Run: `make test` — Expected: all `ok` (test fixtures renamed consistently).

- [ ] **Step 4: Commit**

```bash
git add -A Makefile scripts docs deploy parser irc
git commit -m "chore: remove site-specific hosts, users, channel names"
```

---

### Task 3: Config — auth section, env naming, missing-file fallback

**Files:**
- Modify: `config/config.go`
- Test: `config/config_test.go`

**Interfaces:**
- Produces: `config.AuthConfig{TrustedNetworks []string; TrustedRole string; TrustedProxies []string}`, `Config.Auth`; `config.Load(path)` returns defaults+env when the file is missing.

- [ ] **Step 1: Write failing tests** (append to `config/config_test.go`)

```go
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
```
(Add `path/filepath`, `reflect` imports if missing.)

- [ ] **Step 2: Run to verify failure**

Run: `~/go-install/go/bin/go test ./config/ -run 'MissingFile|AuthDefaults|BadYAML' -v`
Expected: FAIL (`reading config file` error; `cfg.Auth` undefined).

- [ ] **Step 3: Implement**

In `config/config.go`:

```go
type AuthConfig struct {
	// TrustedNetworks are CIDRs whose clients skip login. Empty = always log in.
	TrustedNetworks []string `yaml:"trusted_networks"`
	// TrustedRole is the role granted to trusted-network clients: admin|user.
	TrustedRole string `yaml:"trusted_role"`
	// TrustedProxies are peers whose X-Forwarded-For/-Proto headers are believed.
	TrustedProxies []string `yaml:"trusted_proxies"`
}
```

Add `Auth AuthConfig \`yaml:"auth"\`` to `Config`; in `defaults()`:

```go
		Auth: AuthConfig{
			TrustedRole:    "admin",
			TrustedProxies: []string{"127.0.0.1/32", "::1/128"},
		},
```

Replace the read in `Load`:

```go
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
```
(add `"log"` import.)

In `applyEnvToStruct` replace the key derivation and add the slice case:

```go
		name := strings.Split(fieldType.Tag.Get("yaml"), ",")[0]
		if name == "" || name == "-" {
			name = fieldType.Name
		}
		envKey := prefix + "_" + strings.ToUpper(name)
```

```go
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
```

Existing single-word keys (`XIRC_SERVER_PORT`, `XIRC_DATABASE_DRIVER`) keep working because their yaml tag equals the lowercase field name.

- [ ] **Step 4: Verify**

Run: `~/go-install/go/bin/go test ./config/ -v` — Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add config/
git commit -m "feat(config): auth section, yaml-named env overrides, run without config file"
```

---

### Task 4: DB — users and sessions

**Files:**
- Modify: `db/models.go`, `db/store.go`, `db/migrations.go` (append to `migrationStatements`), `db/mysql.go` (append to `mysqlMigrationStatements`), `db/sqlite.go`, `db/mysql.go` (delegating methods), `irc/manager_test.go` (mockStore stubs; also any other `db.Store` fakes found by `grep -rn "func (m \*mockStore)" --include=*_test.go .`)
- Create: `db/users.go`, `db/users_test.go`

**Interfaces:**
- Produces (on `db.Store`):
  - `CreateUser(u *User) error` (sets `u.ID`), `GetUser(id int64) (*User, error)`, `GetUserByName(name string) (*User, error)` — both return `(nil, nil)` when not found
  - `ListUsers() ([]User, error)` (ordered by username), `UpdateUser(u *User) error` (username, password_hash, role), `DeleteUser(id int64) error` (also deletes the user's sessions)
  - `CountUsers() (int, error)`, `CountAdmins() (int, error)`
  - `CreateSession(s *Session) error`, `GetSession(tokenHash string) (*Session, error)` (`nil, nil` when not found), `TouchSession(tokenHash string, expiresAt time.Time) error`, `DeleteSession(tokenHash string) error`, `DeleteUserSessions(userID int64) error`, `DeleteExpiredSessions(now time.Time) (int64, error)`
- Types:
```go
type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
}

type Session struct {
	TokenHash string
	UserID    int64
	ExpiresAt time.Time
}
```
Times are stored as unix seconds (`INTEGER`/`BIGINT`) in both stores.

- [ ] **Step 1: Write failing test** `db/users_test.go`

```go
package db

import (
	"testing"
	"time"
)

func TestUsersAndSessions_SQLite(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()
	testUsersAndSessions(t, store)
}

func TestUsersAndSessions_MySQL(t *testing.T) {
	store := newTestMySQLStore(t) // skips without XIRC_TEST_MYSQL_DSN
	store.db.Exec("DELETE FROM sessions")
	store.db.Exec("DELETE FROM users")
	testUsersAndSessions(t, store)
}

func testUsersAndSessions(t *testing.T, s Store) {
	t.Helper()
	if n, _ := s.CountUsers(); n != 0 {
		t.Fatalf("fresh db users = %d", n)
	}
	u := &User{Username: "alice", PasswordHash: "h1", Role: "admin", CreatedAt: time.Now()}
	if err := s.CreateUser(u); err != nil || u.ID == 0 {
		t.Fatalf("create: %v id=%d", err, u.ID)
	}
	if err := s.CreateUser(&User{Username: "alice", PasswordHash: "x", Role: "user", CreatedAt: time.Now()}); err == nil {
		t.Fatal("duplicate username accepted")
	}
	s.CreateUser(&User{Username: "bob", PasswordHash: "h2", Role: "user", CreatedAt: time.Now()})

	got, err := s.GetUserByName("alice")
	if err != nil || got == nil || got.PasswordHash != "h1" || got.Role != "admin" {
		t.Fatalf("get by name: %+v %v", got, err)
	}
	if missing, err := s.GetUserByName("nobody"); missing != nil || err != nil {
		t.Fatalf("missing user: %+v %v", missing, err)
	}
	if n, _ := s.CountAdmins(); n != 1 {
		t.Errorf("admins = %d", n)
	}
	list, _ := s.ListUsers()
	if len(list) != 2 || list[0].Username != "alice" {
		t.Errorf("list: %+v", list)
	}

	got.Role = "user"
	got.PasswordHash = "h3"
	if err := s.UpdateUser(got); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.GetUser(got.ID); again.Role != "user" || again.PasswordHash != "h3" {
		t.Errorf("update not persisted: %+v", again)
	}

	now := time.Now()
	s.CreateSession(&Session{TokenHash: "live", UserID: u.ID, ExpiresAt: now.Add(time.Hour)})
	s.CreateSession(&Session{TokenHash: "dead", UserID: u.ID, ExpiresAt: now.Add(-time.Hour)})
	if sess, _ := s.GetSession("live"); sess == nil || sess.UserID != u.ID {
		t.Fatalf("get session: %+v", sess)
	}
	if n, _ := s.DeleteExpiredSessions(now); n != 1 {
		t.Errorf("expired deleted = %d", n)
	}
	later := now.Add(48 * time.Hour).Truncate(time.Second)
	s.TouchSession("live", later)
	if sess, _ := s.GetSession("live"); !sess.ExpiresAt.Equal(later) {
		t.Errorf("touch: %v want %v", sess.ExpiresAt, later)
	}
	if err := s.DeleteUserSessions(u.ID); err != nil {
		t.Fatal(err)
	}
	if sess, _ := s.GetSession("live"); sess != nil {
		t.Error("user sessions not deleted")
	}

	s.CreateSession(&Session{TokenHash: "x", UserID: u.ID, ExpiresAt: now.Add(time.Hour)})
	if err := s.DeleteUser(u.ID); err != nil {
		t.Fatal(err)
	}
	if sess, _ := s.GetSession("x"); sess != nil {
		t.Error("DeleteUser left sessions behind")
	}
	if n, _ := s.CountUsers(); n != 1 {
		t.Errorf("users after delete = %d", n)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `~/go-install/go/bin/go test ./db/ -run UsersAndSessions -v`
Expected: compile FAIL (`CountUsers` undefined).

- [ ] **Step 3: Implement**

Append to `migrationStatements()` (SQLite):

```go
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'user',
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			token_hash TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at)`,
```

Append to `mysqlMigrationStatements()`:

```go
		`CREATE TABLE IF NOT EXISTS users (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			username VARCHAR(64) NOT NULL UNIQUE,
			password_hash VARCHAR(255) NOT NULL,
			role VARCHAR(16) NOT NULL DEFAULT 'user',
			created_at BIGINT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			token_hash CHAR(64) PRIMARY KEY,
			user_id BIGINT NOT NULL,
			expires_at BIGINT NOT NULL,
			INDEX idx_sessions_expires (expires_at),
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
```

Add the types to `db/models.go` (see Interfaces) and the methods to `Store` in `db/store.go` under a `// Users & sessions` comment.

Create `db/users.go` (placeholders `?` work on both drivers):

```go
package db

import (
	"database/sql"
	"errors"
	"time"
)

// Shared users/sessions SQL — identical for SQLite and MySQL (like indexstats.go).
// Times are unix seconds.

const userCols = "id, username, password_hash, role, created_at"

func scanUser(row interface{ Scan(...interface{}) error }) (*User, error) {
	var u User
	var created int64
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	u.CreatedAt = time.Unix(created, 0)
	return &u, nil
}

func createUser(db *sql.DB, u *User) error {
	res, err := db.Exec("INSERT INTO users (username, password_hash, role, created_at) VALUES (?, ?, ?, ?)",
		u.Username, u.PasswordHash, u.Role, u.CreatedAt.Unix())
	if err != nil {
		return err
	}
	u.ID, err = res.LastInsertId()
	return err
}

func getUser(db *sql.DB, id int64) (*User, error) {
	return scanUser(db.QueryRow("SELECT "+userCols+" FROM users WHERE id = ?", id))
}

func getUserByName(db *sql.DB, name string) (*User, error) {
	return scanUser(db.QueryRow("SELECT "+userCols+" FROM users WHERE username = ?", name))
}

func listUsers(db *sql.DB) ([]User, error) {
	rows, err := db.Query("SELECT " + userCols + " FROM users ORDER BY username")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

func updateUser(db *sql.DB, u *User) error {
	_, err := db.Exec("UPDATE users SET username = ?, password_hash = ?, role = ? WHERE id = ?",
		u.Username, u.PasswordHash, u.Role, u.ID)
	return err
}

func deleteUser(db *sql.DB, id int64) error {
	// Explicit session delete: don't rely on FK enforcement being on.
	if _, err := db.Exec("DELETE FROM sessions WHERE user_id = ?", id); err != nil {
		return err
	}
	_, err := db.Exec("DELETE FROM users WHERE id = ?", id)
	return err
}

func countUsers(db *sql.DB, where string, args ...interface{}) (int, error) {
	var n int
	err := db.QueryRow("SELECT COUNT(*) FROM users"+where, args...).Scan(&n)
	return n, err
}

func createSession(db *sql.DB, s *Session) error {
	_, err := db.Exec("INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)",
		s.TokenHash, s.UserID, s.ExpiresAt.Unix())
	return err
}

func getSession(db *sql.DB, tokenHash string) (*Session, error) {
	var s Session
	var exp int64
	err := db.QueryRow("SELECT token_hash, user_id, expires_at FROM sessions WHERE token_hash = ?", tokenHash).
		Scan(&s.TokenHash, &s.UserID, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.ExpiresAt = time.Unix(exp, 0)
	return &s, nil
}

func touchSession(db *sql.DB, tokenHash string, expiresAt time.Time) error {
	_, err := db.Exec("UPDATE sessions SET expires_at = ? WHERE token_hash = ?", expiresAt.Unix(), tokenHash)
	return err
}

func deleteSession(db *sql.DB, tokenHash string) error {
	_, err := db.Exec("DELETE FROM sessions WHERE token_hash = ?", tokenHash)
	return err
}

func deleteUserSessions(db *sql.DB, userID int64) error {
	_, err := db.Exec("DELETE FROM sessions WHERE user_id = ?", userID)
	return err
}

func deleteExpiredSessions(db *sql.DB, now time.Time) (int64, error) {
	res, err := db.Exec("DELETE FROM sessions WHERE expires_at < ?", now.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
```

Delegating methods — add to **both** `db/sqlite.go` (receiver `*SQLiteStore`) and `db/mysql.go` (receiver `*MySQLStore`), identical bodies:

```go
// --- Users & sessions (SQL shared in users.go) ---

func (s *SQLiteStore) CreateUser(u *User) error                 { return createUser(s.db, u) }
func (s *SQLiteStore) GetUser(id int64) (*User, error)          { return getUser(s.db, id) }
func (s *SQLiteStore) GetUserByName(n string) (*User, error)    { return getUserByName(s.db, n) }
func (s *SQLiteStore) ListUsers() ([]User, error)               { return listUsers(s.db) }
func (s *SQLiteStore) UpdateUser(u *User) error                 { return updateUser(s.db, u) }
func (s *SQLiteStore) DeleteUser(id int64) error                { return deleteUser(s.db, id) }
func (s *SQLiteStore) CountUsers() (int, error)                 { return countUsers(s.db, "") }
func (s *SQLiteStore) CountAdmins() (int, error)                { return countUsers(s.db, " WHERE role = ?", "admin") }
func (s *SQLiteStore) CreateSession(x *Session) error           { return createSession(s.db, x) }
func (s *SQLiteStore) GetSession(h string) (*Session, error)    { return getSession(s.db, h) }
func (s *SQLiteStore) TouchSession(h string, t time.Time) error { return touchSession(s.db, h, t) }
func (s *SQLiteStore) DeleteSession(h string) error             { return deleteSession(s.db, h) }
func (s *SQLiteStore) DeleteUserSessions(id int64) error        { return deleteUserSessions(s.db, id) }
func (s *SQLiteStore) DeleteExpiredSessions(now time.Time) (int64, error) {
	return deleteExpiredSessions(s.db, now)
}
```

Add no-op stubs for all 14 methods to `mockStore` in `irc/manager_test.go` (return zero values, e.g. `func (m *mockStore) CountUsers() (int, error) { return 0, nil }`).

- [ ] **Step 4: Verify**

Run: `make test` — Expected: all `ok`; `TestUsersAndSessions_SQLite` PASS; MySQL variant SKIP.

- [ ] **Step 5: Commit**

```bash
git add db/ irc/manager_test.go
git commit -m "feat(db): users and sessions tables for both stores"
```

---

### Task 5: Auth middleware — client IP, principal, policy

**Files:**
- Create: `server/auth.go`, `server/auth_test.go`
- Modify: `server/server.go` (field `auth *Auth`, `SetAuth`, `Handler()`), `server/ws_handler.go` (remove `CheckOrigin`)

**Interfaces:**
- Consumes: `config.AuthConfig` (Task 3), `db.Store` user/session methods (Task 4).
- Produces:
  - `func NewAuth(cfg config.AuthConfig, store db.Store, prefix string) (*Auth, error)` — error on bad CIDR or role
  - `type Principal struct { UserID int64; Username, Role, Via string }` (`Via`: `"session"` | `"network"`); `func (p *Principal) IsAdmin() bool`
  - `func principalFrom(ctx context.Context) *Principal` (nil = anonymous)
  - `func (a *Auth) clientIP(r *http.Request) net.IP`, `func (a *Auth) isHTTPS(r *http.Request) bool`
  - `func (s *Server) SetAuth(a *Auth)`
  - `const sessionCookie = "xirc_session"`, `func hashToken(tok string) string` (hex sha256)
  - `func adminOnly(method, path string) bool`

**Spec deviation (deliberate):** the spec lets anonymous clients reach `/api/setup/*` while no user exists. Here `/api/setup/*` (the missing-directory wizard) is admin-only always; the first-admin flow is `/api/auth/setup`, and the frontend runs the directory wizard right after the admin is created. One fewer anonymous surface, same user flow.

**Design note:** `Handler()` wraps the mux only when `SetAuth` was called. All existing tests construct servers without auth and keep passing; `main.go` always calls `SetAuth` (Task 7). Paths are matched after the proxy strips the prefix, exactly like the existing mux routes.

- [ ] **Step 1: Write failing tests** `server/auth_test.go`

```go
package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RealDtx/maxwell-irc/config"
	"github.com/RealDtx/maxwell-irc/db"
)

func newAuthTestServer(t *testing.T, cfg config.AuthConfig) (*Server, db.Store) {
	t.Helper()
	srv, store, cleanup := newTestServerWithStore(t)
	t.Cleanup(cleanup)
	if cfg.TrustedRole == "" {
		cfg.TrustedRole = "admin"
	}
	if cfg.TrustedProxies == nil {
		cfg.TrustedProxies = []string{"127.0.0.1/32", "::1/128"}
	}
	a, err := NewAuth(cfg, store, "")
	if err != nil {
		t.Fatal(err)
	}
	srv.SetAuth(a)
	return srv, store
}

func do(srv *Server, method, path, remote string, hdr map[string]string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = remote
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

func TestNewAuth_RejectsBadConfig(t *testing.T) {
	if _, err := NewAuth(config.AuthConfig{TrustedRole: "root"}, nil, ""); err == nil {
		t.Error("bad role accepted")
	}
	if _, err := NewAuth(config.AuthConfig{TrustedRole: "user", TrustedNetworks: []string{"10.0.0.0/33"}}, nil, ""); err == nil {
		t.Error("bad CIDR accepted")
	}
}

func TestClientIP(t *testing.T) {
	a, _ := NewAuth(config.AuthConfig{TrustedRole: "admin", TrustedProxies: []string{"127.0.0.1/32", "::1/128"}}, nil, "")
	cases := []struct {
		remote, xff, want string
	}{
		{"203.0.113.9:5000", "", "203.0.113.9"},
		{"203.0.113.9:5000", "192.168.1.5", "203.0.113.9"},            // untrusted peer: XFF ignored
		{"127.0.0.1:5000", "192.168.1.5", "192.168.1.5"},              // trusted proxy
		{"127.0.0.1:5000", "6.6.6.6, 192.168.1.5", "192.168.1.5"},     // right-most untrusted wins
		{"127.0.0.1:5000", "192.168.1.5, 127.0.0.1", "192.168.1.5"},   // skip trusted hops
		{"[::1]:5000", "2001:db8::7", "2001:db8::7"},
		{"127.0.0.1:5000", "garbage", "127.0.0.1"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := a.clientIP(r).String(); got != c.want {
			t.Errorf("remote=%s xff=%q: got %s want %s", c.remote, c.xff, got, c.want)
		}
	}
}

func TestAnonymousBlockedFromAPI(t *testing.T) {
	srv, _ := newAuthTestServer(t, config.AuthConfig{})
	for _, p := range []string{"/api/downloads", "/api/servers", "/ws"} {
		if w := do(srv, "GET", p, "203.0.113.9:1", nil, ""); w.Code != http.StatusUnauthorized {
			t.Errorf("%s: got %d want 401", p, w.Code)
		}
	}
	for _, p := range []string{"/api/health", "/api/auth/me"} {
		if w := do(srv, "GET", p, "203.0.113.9:1", nil, ""); w.Code == http.StatusUnauthorized {
			t.Errorf("%s must be reachable anonymously", p)
		}
	}
}

func TestSpoofedXFFDoesNotGrantTrustedNetwork(t *testing.T) {
	srv, _ := newAuthTestServer(t, config.AuthConfig{TrustedNetworks: []string{"192.168.0.0/16"}})
	w := do(srv, "GET", "/api/downloads", "203.0.113.9:1", map[string]string{"X-Forwarded-For": "192.168.1.5"}, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("spoofed XFF: got %d want 401", w.Code)
	}
	w = do(srv, "GET", "/api/downloads", "127.0.0.1:1", map[string]string{"X-Forwarded-For": "192.168.1.5"}, "")
	if w.Code != http.StatusOK {
		t.Fatalf("real LAN client via proxy: got %d want 200", w.Code)
	}
}

func TestTrustedNetworkRole(t *testing.T) {
	srv, _ := newAuthTestServer(t, config.AuthConfig{TrustedNetworks: []string{"192.168.0.0/16"}, TrustedRole: "user"})
	if w := do(srv, "GET", "/api/servers", "192.168.1.5:1", nil, ""); w.Code != http.StatusOK {
		t.Errorf("user GET servers: %d", w.Code)
	}
	w := do(srv, "POST", "/api/servers", "192.168.1.5:1", map[string]string{"Content-Type": "application/json"}, `{}`)
	if w.Code != http.StatusForbidden {
		t.Errorf("user POST servers: got %d want 403", w.Code)
	}
}

func TestAdminOnlyTable(t *testing.T) {
	admin := []struct{ m, p string }{
		{"POST", "/api/irc/raw"}, {"POST", "/api/irc/connect"}, {"POST", "/api/irc/disconnect"}, {"POST", "/api/irc/join"},
		{"POST", "/api/servers"}, {"PUT", "/api/servers/3"}, {"DELETE", "/api/realms/2"}, {"PUT", "/api/library"},
		{"POST", "/api/library/preview"}, {"GET", "/api/search/patterns"}, {"GET", "/api/search/unmatched"},
		{"POST", "/api/search/patterns/learn"}, {"POST", "/api/index/clear"}, {"GET", "/api/errors"},
		{"GET", "/api/users"}, {"DELETE", "/api/users/4"}, {"GET", "/api/setup/status"}, {"POST", "/api/capabilities/recheck"},
		{"POST", "/api/files"}, {"GET", "/api/browse"}, {"POST", "/api/downloads/delete"}, {"POST", "/api/downloads/clear"},
		{"POST", "/api/downloads/set-target"}, {"POST", "/api/downloads/move"},
	}
	for _, c := range admin {
		if !adminOnly(c.m, c.p) {
			t.Errorf("%s %s should be admin-only", c.m, c.p)
		}
	}
	user := []struct{ m, p string }{
		{"GET", "/api/servers"}, {"GET", "/api/realms"}, {"GET", "/api/library"}, {"POST", "/api/search/start"},
		{"GET", "/api/index/search"}, {"POST", "/api/downloads/request"}, {"POST", "/api/downloads/cancel"},
		{"POST", "/api/downloads/retry"}, {"POST", "/api/downloads/set-auto-extract"}, {"GET", "/api/files"},
		{"POST", "/api/irc/message"}, {"GET", "/api/irc/status"}, {"GET", "/api/stats/downloads"},
		{"GET", "/api/storage"}, {"GET", "/api/capabilities"}, {"POST", "/api/search/saved"}, {"GET", "/ws"},
	}
	for _, c := range user {
		if adminOnly(c.m, c.p) {
			t.Errorf("%s %s should be allowed for users", c.m, c.p)
		}
	}
}

func TestCrossSiteRequestsRejected(t *testing.T) {
	srv, _ := newAuthTestServer(t, config.AuthConfig{TrustedNetworks: []string{"192.168.0.0/16"}})
	// A form/fetch "simple request" from a foreign page on a LAN browser.
	w := do(srv, "POST", "/api/downloads/clear", "192.168.1.5:1", map[string]string{"Content-Type": "text/plain"}, `{"status":"completed"}`)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain POST: got %d want 415", w.Code)
	}
}

func TestSessionCookieAuthenticates(t *testing.T) {
	srv, store := newAuthTestServer(t, config.AuthConfig{})
	u := &db.User{Username: "bob", PasswordHash: "x", Role: "user", CreatedAt: time.Now()}
	store.CreateUser(u)
	store.CreateSession(&db.Session{TokenHash: hashToken("tok"), UserID: u.ID, ExpiresAt: time.Now().Add(time.Hour)})
	hdr := map[string]string{"Cookie": sessionCookie + "=tok"}
	if w := do(srv, "GET", "/api/downloads", "203.0.113.9:1", hdr, ""); w.Code != http.StatusOK {
		t.Errorf("valid session: %d", w.Code)
	}
	store.CreateSession(&db.Session{TokenHash: hashToken("old"), UserID: u.ID, ExpiresAt: time.Now().Add(-time.Hour)})
	hdr = map[string]string{"Cookie": sessionCookie + "=old"}
	if w := do(srv, "GET", "/api/downloads", "203.0.113.9:1", hdr, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("expired session: got %d want 401", w.Code)
	}
}
```

Also add to `server/ws_handler_test.go` (new file):

```go
package server

import (
	"net/http/httptest"
	"testing"
)

func TestWebSocketRejectsForeignOrigin(t *testing.T) {
	r := httptest.NewRequest("GET", "http://xirc.lan/ws", nil)
	r.Header.Set("Origin", "http://evil.example")
	if upgrader.CheckOrigin != nil && upgrader.CheckOrigin(r) {
		t.Fatal("custom CheckOrigin accepts any origin; remove it so gorilla's same-host check applies")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `~/go-install/go/bin/go test ./server/ -run 'Auth|ClientIP|Anonymous|Spoofed|Trusted|AdminOnly|CrossSite|SessionCookie|WebSocket' -v`
Expected: compile FAIL (`NewAuth` undefined).

- [ ] **Step 3: Implement `server/auth.go`**

```go
package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/RealDtx/maxwell-irc/config"
	"github.com/RealDtx/maxwell-irc/db"
)

const (
	sessionCookie = "xirc_session"
	sessionTTL    = 30 * 24 * time.Hour
	touchEvery    = time.Hour
)

type Principal struct {
	UserID   int64  `json:"-"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Via      string `json:"via"` // "session" | "network"
}

func (p *Principal) IsAdmin() bool { return p != nil && p.Role == "admin" }

type ctxKey struct{}

func principalFrom(ctx context.Context) *Principal {
	p, _ := ctx.Value(ctxKey{}).(*Principal)
	return p
}

type Auth struct {
	store       db.Store
	trustedNets []*net.IPNet
	proxies     []*net.IPNet
	trustedRole string
	cookiePath  string
	limiter     *loginLimiter // Task 6
}

func parseCIDRs(list []string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, s := range list {
		if !strings.Contains(s, "/") {
			if ip := net.ParseIP(s); ip != nil && ip.To4() != nil {
				s += "/32"
			} else {
				s += "/128"
			}
		}
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", s, err)
		}
		out = append(out, n)
	}
	return out, nil
}

func NewAuth(cfg config.AuthConfig, store db.Store, prefix string) (*Auth, error) {
	if cfg.TrustedRole != "admin" && cfg.TrustedRole != "user" {
		return nil, fmt.Errorf("auth.trusted_role must be admin or user, got %q", cfg.TrustedRole)
	}
	nets, err := parseCIDRs(cfg.TrustedNetworks)
	if err != nil {
		return nil, fmt.Errorf("auth.trusted_networks: %w", err)
	}
	proxies, err := parseCIDRs(cfg.TrustedProxies)
	if err != nil {
		return nil, fmt.Errorf("auth.trusted_proxies: %w", err)
	}
	path := strings.TrimRight(prefix, "/")
	if path == "" {
		path = "/"
	}
	return &Auth{store: store, trustedNets: nets, proxies: proxies, trustedRole: cfg.TrustedRole,
		cookiePath: path, limiter: newLoginLimiter()}, nil
}

func inNets(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func remoteIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(host)
}

// clientIP believes X-Forwarded-For only from trusted proxies, and then takes
// the right-most hop that is not itself a trusted proxy (left entries are
// client-controlled).
func (a *Auth) clientIP(r *http.Request) net.IP {
	ip := remoteIP(r)
	if ip == nil || !inNets(ip, a.proxies) {
		return ip
	}
	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		h := net.ParseIP(strings.TrimSpace(hops[i]))
		if h == nil {
			break
		}
		if !inNets(h, a.proxies) {
			return h
		}
	}
	return ip
}

func (a *Auth) isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	ip := remoteIP(r)
	return ip != nil && inNets(ip, a.proxies) && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// sessionPrincipal resolves the cookie to a user, sliding the expiry.
func (a *Auth) sessionPrincipal(r *http.Request) *Principal {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	h := hashToken(c.Value)
	sess, err := a.store.GetSession(h)
	if err != nil || sess == nil {
		return nil
	}
	now := time.Now()
	if now.After(sess.ExpiresAt) {
		a.store.DeleteSession(h)
		return nil
	}
	u, err := a.store.GetUser(sess.UserID)
	if err != nil || u == nil {
		return nil
	}
	if sess.ExpiresAt.Sub(now) < sessionTTL-touchEvery {
		a.store.TouchSession(h, now.Add(sessionTTL))
	}
	return &Principal{UserID: u.ID, Username: u.Username, Role: u.Role, Via: "session"}
}

func (a *Auth) principal(r *http.Request) *Principal {
	if p := a.sessionPrincipal(r); p != nil {
		return p
	}
	if ip := a.clientIP(r); ip != nil && inNets(ip, a.trustedNets) {
		return &Principal{Username: "lan", Role: a.trustedRole, Via: "network"}
	}
	return nil
}

// anonymousOK lists what a not-logged-in client may reach. Static assets are
// everything outside /api/ and /ws (the SPA draws the login screen itself).
func anonymousOK(path string) bool {
	switch path {
	case "/api/health", "/api/auth/login", "/api/auth/me", "/api/auth/setup":
		return true
	}
	return !strings.HasPrefix(path, "/api/") && path != "/ws"
}

type rule struct {
	prefix     string
	nonGetOnly bool
}

// adminRules mirrors the permission table in the spec. Prefix match on
// path segments: "/api/servers" covers "/api/servers" and "/api/servers/…".
var adminRules = []rule{
	{"/api/irc/raw", false}, {"/api/irc/connect", false}, {"/api/irc/disconnect", false}, {"/api/irc/join", false},
	{"/api/servers", true}, {"/api/realms", true}, {"/api/library", true},
	{"/api/search/patterns", false}, {"/api/search/unmatched", false},
	{"/api/index/clear", false}, {"/api/errors", false}, {"/api/users", false}, {"/api/setup", false},
	{"/api/capabilities/recheck", false}, {"/api/browse", false},
	{"/api/files", true},
	{"/api/downloads/delete", false}, {"/api/downloads/clear", false},
	{"/api/downloads/set-target", false}, {"/api/downloads/move", false},
}

func adminOnly(method, path string) bool {
	for _, r := range adminRules {
		if path != r.prefix && !strings.HasPrefix(path, r.prefix+"/") {
			continue
		}
		if r.nonGetOnly && (method == http.MethodGet || method == http.MethodHead) {
			return false
		}
		return true
	}
	return false
}

func (a *Auth) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CSRF: browsers can only send JSON cross-origin after a CORS
		// preflight, which we never answer — so requiring JSON on writes
		// blocks form/text-plain attacks, including against LAN principals
		// that have no cookie to protect them.
		if strings.HasPrefix(r.URL.Path, "/api/") && (r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch) {
			if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
				writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
				return
			}
		}
		p := a.principal(r)
		if p == nil && !anonymousOK(r.URL.Path) {
			writeError(w, http.StatusUnauthorized, "login required")
			return
		}
		if adminOnly(r.Method, r.URL.Path) && !p.IsAdmin() {
			if p == nil {
				writeError(w, http.StatusUnauthorized, "login required")
			} else {
				writeError(w, http.StatusForbidden, "admin only")
			}
			return
		}
		if p != nil {
			r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, p))
		}
		next.ServeHTTP(w, r)
	})
}

func logAuthConfig(a *Auth) {
	if len(a.trustedNets) == 0 {
		log.Printf("auth: login required for all clients")
	} else {
		log.Printf("auth: %d trusted network(s) skip login as role %q", len(a.trustedNets), a.trustedRole)
	}
}
```

Add a temporary stub so this compiles before Task 6 (Task 6 replaces it):

```go
// server/ratelimit.go
package server

type loginLimiter struct{}

func newLoginLimiter() *loginLimiter { return &loginLimiter{} }
```

In `server/server.go`: add field `auth *Auth`, and

```go
// SetAuth enables authentication. main() always calls it; tests that don't
// exercise auth leave it unset and get the bare mux.
func (s *Server) SetAuth(a *Auth) {
	s.auth = a
	logAuthConfig(a)
}

func (s *Server) Handler() http.Handler {
	if s.auth == nil {
		return s.mux
	}
	return s.auth.middleware(s.mux)
}
```

In `server/ws_handler.go` replace the upgrader with:

```go
// Default CheckOrigin: Origin host must equal Host. Proxies must preserve
// Host (both shipped configs do). Blocks cross-site WebSocket hijacking.
var upgrader = websocket.Upgrader{}
```

- [ ] **Step 4: Verify**

Run: `~/go-install/go/bin/go test ./server/ -v -run 'Auth|ClientIP|Anonymous|Spoofed|Trusted|AdminOnly|CrossSite|SessionCookie|WebSocket'` → PASS.
Note: `TestAnonymousBlockedFromAPI` expects `/api/auth/me` not to 401 — until Task 6 registers it, the mux returns 404, which satisfies the test.
Run: `make test` → all `ok`.

- [ ] **Step 5: Commit**

```bash
git add server/
git commit -m "feat(auth): session/network principal middleware with admin policy table"
```

---

### Task 6: Auth endpoints, users CRUD, rate limiter, session pruning

**Files:**
- Create: `server/auth_handlers.go`, `server/auth_handlers_test.go`
- Modify: `server/ratelimit.go` (replace stub), `server/server.go` (routes), `maintenance/maintenance.go`, `maintenance/maintenance_test.go`, `go.mod` (`golang.org/x/crypto`)

**Interfaces:**
- Consumes: Task 4 store methods; Task 5 `Auth`, `Principal`, `hashToken`, `sessionCookie`, `principalFrom`.
- Produces:
  - `func HashPassword(pw string) (string, error)` (errors if `len(pw) < 8`)
  - `func NormalizeUsername(s string) (string, error)`
  - `func CreateOrResetAdmin(store db.Store, name, password string) error` (used by `--create-admin`, Task 7)
  - Routes: `POST /api/auth/login`, `POST /api/auth/logout`, `GET /api/auth/me`, `POST /api/auth/setup`, `GET|POST /api/users`, `PUT|DELETE /api/users/{id}`
  - `/api/auth/me` responses: principal JSON `{username, role, via}`; `{"setup_required": true}` (200) when no users exist; 401 otherwise.

- [ ] **Step 1: Add dependency**

```bash
~/go-install/go/bin/go get golang.org/x/crypto@latest && ~/go-install/go/bin/go mod tidy
```

- [ ] **Step 2: Write failing tests** `server/auth_handlers_test.go`

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RealDtx/maxwell-irc/config"
)

var jsonHdr = map[string]string{"Content-Type": "application/json"}

func cookieOf(w *httptest.ResponseRecorder) string {
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			return c.Value
		}
	}
	return ""
}

func withCookie(tok string) map[string]string {
	return map[string]string{"Content-Type": "application/json", "Cookie": sessionCookie + "=" + tok}
}

func TestSetupThenLogin(t *testing.T) {
	srv, _ := newAuthTestServer(t, config.AuthConfig{})
	const ip = "203.0.113.9:1"

	w := do(srv, "GET", "/api/auth/me", ip, nil, "")
	if !strings.Contains(w.Body.String(), `"setup_required":true`) {
		t.Fatalf("fresh me: %d %s", w.Code, w.Body)
	}
	if w := do(srv, "POST", "/api/auth/setup", ip, jsonHdr, `{"username":"Admin","password":"short"}`); w.Code != http.StatusBadRequest {
		t.Errorf("short password: %d", w.Code)
	}
	w = do(srv, "POST", "/api/auth/setup", ip, jsonHdr, `{"username":"Admin","password":"correct horse"}`)
	if w.Code != http.StatusOK || cookieOf(w) == "" {
		t.Fatalf("setup: %d %s", w.Code, w.Body)
	}
	if w := do(srv, "POST", "/api/auth/setup", ip, jsonHdr, `{"username":"x","password":"another pw"}`); w.Code != http.StatusConflict {
		t.Errorf("second setup: got %d want 409", w.Code)
	}

	if w := do(srv, "POST", "/api/auth/login", ip, jsonHdr, `{"username":"admin","password":"wrong pass"}`); w.Code != http.StatusUnauthorized {
		t.Errorf("bad pw: %d", w.Code)
	}
	w = do(srv, "POST", "/api/auth/login", ip, jsonHdr, `{"username":" ADMIN ","password":"correct horse"}`)
	tok := cookieOf(w)
	if w.Code != http.StatusOK || tok == "" {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	var me Principal
	json.NewDecoder(do(srv, "GET", "/api/auth/me", ip, withCookie(tok), "").Body).Decode(&me)
	if me.Username != "admin" || me.Role != "admin" || me.Via != "session" {
		t.Errorf("me: %+v", me)
	}

	do(srv, "POST", "/api/auth/logout", ip, withCookie(tok), `{}`)
	if w := do(srv, "GET", "/api/downloads", ip, withCookie(tok), ""); w.Code != http.StatusUnauthorized {
		t.Errorf("after logout: %d", w.Code)
	}
}

func TestLoginRateLimit(t *testing.T) {
	srv, _ := newAuthTestServer(t, config.AuthConfig{})
	do(srv, "POST", "/api/auth/setup", "203.0.113.1:1", jsonHdr, `{"username":"a","password":"password1"}`)
	for i := 0; i < 5; i++ {
		do(srv, "POST", "/api/auth/login", "203.0.113.9:1", jsonHdr, `{"username":"a","password":"nope-nope"}`)
	}
	w := do(srv, "POST", "/api/auth/login", "203.0.113.9:1", jsonHdr, `{"username":"a","password":"password1"}`)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("6th attempt: got %d want 429", w.Code)
	}
	if w := do(srv, "POST", "/api/auth/login", "203.0.113.10:1", jsonHdr, `{"username":"a","password":"password1"}`); w.Code != http.StatusOK {
		t.Errorf("other IP must not be limited: %d", w.Code)
	}
}

func TestCookiePathUsesPrefix(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()
	a, _ := NewAuth(config.AuthConfig{TrustedRole: "admin"}, store, "/xirc")
	srv.SetAuth(a)
	w := do(srv, "POST", "/api/auth/setup", "203.0.113.1:1", jsonHdr, `{"username":"a","password":"password1"}`)
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			if c.Path != "/xirc" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
				t.Errorf("cookie attrs: path=%q httponly=%v samesite=%v", c.Path, c.HttpOnly, c.SameSite)
			}
			if c.Secure {
				t.Error("Secure on plain http")
			}
			return
		}
	}
	t.Fatal("no cookie")
}

func TestSecureCookieBehindHTTPSProxy(t *testing.T) {
	srv, _ := newAuthTestServer(t, config.AuthConfig{})
	hdr := map[string]string{"Content-Type": "application/json", "X-Forwarded-Proto": "https"}
	w := do(srv, "POST", "/api/auth/setup", "127.0.0.1:1", hdr, `{"username":"a","password":"password1"}`)
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie && !c.Secure {
			t.Error("Secure missing behind https proxy")
		}
	}
}

func TestUsersCRUDAndLastAdminGuard(t *testing.T) {
	srv, store := newAuthTestServer(t, config.AuthConfig{})
	const ip = "203.0.113.9:1"
	admin := cookieOf(do(srv, "POST", "/api/auth/setup", ip, jsonHdr, `{"username":"root","password":"password1"}`))

	w := do(srv, "POST", "/api/users", ip, withCookie(admin), `{"username":"bob","password":"password2","role":"user"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("create user: %d %s", w.Code, w.Body)
	}
	bob, _ := store.GetUserByName("bob")
	bobTok := cookieOf(do(srv, "POST", "/api/auth/login", ip, jsonHdr, `{"username":"bob","password":"password2"}`))

	if w := do(srv, "GET", "/api/users", ip, withCookie(bobTok), ""); w.Code != http.StatusForbidden {
		t.Errorf("user listing users: %d", w.Code)
	}
	if strings.Contains(do(srv, "GET", "/api/users", ip, withCookie(admin), "").Body.String(), "password") {
		t.Error("password hash leaked in list")
	}

	// Password reset logs bob out everywhere.
	path := "/api/users/" + itoa(bob.ID)
	if w := do(srv, "PUT", path, ip, withCookie(admin), `{"password":"newpassword"}`); w.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", w.Code, w.Body)
	}
	if w := do(srv, "GET", "/api/downloads", ip, withCookie(bobTok), ""); w.Code != http.StatusUnauthorized {
		t.Errorf("bob still logged in after reset: %d", w.Code)
	}

	root, _ := store.GetUserByName("root")
	rootPath := "/api/users/" + itoa(root.ID)
	if w := do(srv, "PUT", rootPath, ip, withCookie(admin), `{"role":"user"}`); w.Code != http.StatusConflict {
		t.Errorf("demote last admin: got %d want 409", w.Code)
	}
	if w := do(srv, "DELETE", rootPath, ip, withCookie(admin), ""); w.Code != http.StatusConflict {
		t.Errorf("delete last admin: got %d want 409", w.Code)
	}
	if w := do(srv, "DELETE", path, ip, withCookie(admin), ""); w.Code != http.StatusOK {
		t.Errorf("delete bob: %d", w.Code)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
```
(add `"strconv"` to the imports.)

Append to `maintenance/maintenance_test.go` a test using the existing test store helper in that file (read it first; if it uses a real SQLite store, reuse that constructor):

```go
func TestRunOnce_PrunesExpiredSessions(t *testing.T) {
	store := newTestStore(t) // use the helper already defined in this file
	u := &db.User{Username: "a", PasswordHash: "x", Role: "user", CreatedAt: time.Now()}
	store.CreateUser(u)
	store.CreateSession(&db.Session{TokenHash: "old", UserID: u.ID, ExpiresAt: time.Now().Add(-time.Minute)})
	New(store, config.MaintenanceConfig{}).runOnce()
	if s, _ := store.GetSession("old"); s != nil {
		t.Error("expired session not pruned")
	}
}
```

- [ ] **Step 3: Run to verify failure**

Run: `~/go-install/go/bin/go test ./server/ ./maintenance/ -run 'Setup|RateLimit|CookiePath|SecureCookie|UsersCRUD|PrunesExpired' -v`
Expected: FAIL (routes 404 / `HashPassword` undefined).

- [ ] **Step 4: Implement**

`server/ratelimit.go` (replace stub):

```go
package server

import (
	"sync"
	"time"
)

const (
	loginMaxFails = 5
	loginWindow   = time.Minute
)

// loginLimiter counts failed logins per client IP in a fixed window.
// ponytail: in-memory, per process; fine for a single instance.
type loginLimiter struct {
	mu    sync.Mutex
	fails map[string][]time.Time
}

func newLoginLimiter() *loginLimiter { return &loginLimiter{fails: map[string][]time.Time{}} }

func (l *loginLimiter) recent(ip string, now time.Time) []time.Time {
	kept := l.fails[ip][:0]
	for _, t := range l.fails[ip] {
		if now.Sub(t) < loginWindow {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.fails, ip)
		return nil
	}
	l.fails[ip] = kept
	return kept
}

func (l *loginLimiter) blocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(ip, time.Now())) >= loginMaxFails
}

func (l *loginLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.fails[ip] = append(l.recent(ip, now), now)
}
```

`server/auth_handlers.go`:

```go
package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/RealDtx/maxwell-irc/db"
	"golang.org/x/crypto/bcrypt"
)

var usernameRe = regexp.MustCompile(`^[a-z0-9._-]{1,32}$`)

// dummyHash keeps unknown-user logins as slow as wrong-password ones.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("xirc-dummy-password"), bcrypt.DefaultCost)

func NormalizeUsername(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if !usernameRe.MatchString(s) {
		return "", errors.New("username must be 1-32 characters of a-z 0-9 . _ -")
	}
	return s, nil
}

func HashPassword(pw string) (string, error) {
	if len(pw) < 8 {
		return "", errors.New("password must be at least 8 characters")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(h), err
}

// CreateOrResetAdmin creates name as admin, or resets its password and
// promotes it. Existing sessions of that user are revoked.
func CreateOrResetAdmin(store db.Store, name, password string) error {
	name, err := NormalizeUsername(name)
	if err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	u, err := store.GetUserByName(name)
	if err != nil {
		return err
	}
	if u == nil {
		return store.CreateUser(&db.User{Username: name, PasswordHash: hash, Role: "admin", CreatedAt: time.Now()})
	}
	u.PasswordHash, u.Role = hash, "admin"
	if err := store.UpdateUser(u); err != nil {
		return err
	}
	return store.DeleteUserSessions(u.ID)
}

func (a *Auth) startSession(w http.ResponseWriter, r *http.Request, u *db.User) error {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	tok := base64.RawURLEncoding.EncodeToString(buf)
	if err := a.store.CreateSession(&db.Session{TokenHash: hashToken(tok), UserID: u.ID, ExpiresAt: time.Now().Add(sessionTTL)}); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: tok, Path: a.cookiePath,
		MaxAge: int(sessionTTL / time.Second), HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: a.isHTTPS(r),
	})
	return nil
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ip := s.auth.clientIP(r).String()
	if s.auth.limiter.blocked(ip) {
		writeError(w, http.StatusTooManyRequests, "too many failed logins, wait a minute")
		return
	}
	var c credentials
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name, _ := NormalizeUsername(c.Username)
	u, err := s.store.GetUserByName(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	hash := dummyHash
	if u != nil {
		hash = []byte(u.PasswordHash)
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(c.Password)) != nil || u == nil {
		s.auth.limiter.fail(ip)
		writeError(w, http.StatusUnauthorized, "wrong username or password")
		return
	}
	if err := s.auth.startSession(w, r, u); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, Principal{Username: u.Username, Role: u.Role, Via: "session"})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.store.DeleteSession(hashToken(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: s.auth.cookiePath, MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if p := principalFrom(r.Context()); p != nil {
		writeJSON(w, http.StatusOK, p)
		return
	}
	if n, err := s.store.CountUsers(); err == nil && n == 0 {
		writeJSON(w, http.StatusOK, map[string]bool{"setup_required": true})
		return
	}
	writeError(w, http.StatusUnauthorized, "login required")
}

// handleAuthSetup creates the first admin. Open to anyone, but only while no
// user exists.
func (s *Server) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var c credentials
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if n, err := s.store.CountUsers(); err != nil || n > 0 {
		writeError(w, http.StatusConflict, "setup already done")
		return
	}
	name, err := NormalizeUsername(c.Username)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := HashPassword(c.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	u := &db.User{Username: name, PasswordHash: hash, Role: "admin", CreatedAt: time.Now()}
	if err := s.store.CreateUser(u); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.auth.startSession(w, r, u); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, Principal{Username: u.Username, Role: u.Role, Via: "session"})
}

type userRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// /api/users (admin; enforced by middleware)
func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		users, err := s.store.ListUsers()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, users)
	case http.MethodPost:
		var req userRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		name, err := NormalizeUsername(req.Username)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.Role != "admin" && req.Role != "user" {
			writeError(w, http.StatusBadRequest, "role must be admin or user")
			return
		}
		hash, err := HashPassword(req.Password)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if existing, _ := s.store.GetUserByName(name); existing != nil {
			writeError(w, http.StatusConflict, "username already exists")
			return
		}
		u := &db.User{Username: name, PasswordHash: hash, Role: req.Role, CreatedAt: time.Now()}
		if err := s.store.CreateUser(u); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, u)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// /api/users/{id}: PUT {role?, password?}, DELETE
func (s *Server) handleUserByID(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/users/"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	s.setupMu.Lock() // serialises last-admin checks
	defer s.setupMu.Unlock()
	u, err := s.store.GetUser(id)
	if err != nil || u == nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	lastAdmin := func() bool {
		n, _ := s.store.CountAdmins()
		return u.Role == "admin" && n <= 1
	}
	switch r.Method {
	case http.MethodPut:
		var req userRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.Role != "" {
			if req.Role != "admin" && req.Role != "user" {
				writeError(w, http.StatusBadRequest, "role must be admin or user")
				return
			}
			if req.Role == "user" && lastAdmin() {
				writeError(w, http.StatusConflict, "cannot demote the last admin")
				return
			}
			u.Role = req.Role
		}
		if req.Password != "" {
			if u.PasswordHash, err = HashPassword(req.Password); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		if err := s.store.UpdateUser(u); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// Role or password change: revoke sessions so it applies immediately.
		s.store.DeleteUserSessions(u.ID)
		writeJSON(w, http.StatusOK, u)
	case http.MethodDelete:
		if lastAdmin() {
			writeError(w, http.StatusConflict, "cannot delete the last admin")
			return
		}
		if err := s.store.DeleteUser(id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
```

In `server/server.go`: add field `setupMu sync.Mutex` (import `sync`), and in `routes()` before the setup endpoints:

```go
	// Auth & users (handlers require SetAuth; only registered routes reached via the middleware)
	s.mux.HandleFunc("/api/auth/login", s.handleLogin)
	s.mux.HandleFunc("/api/auth/logout", s.handleLogout)
	s.mux.HandleFunc("/api/auth/me", s.handleMe)
	s.mux.HandleFunc("/api/auth/setup", s.handleAuthSetup)
	s.mux.HandleFunc("/api/users", s.handleUsers)
	s.mux.HandleFunc("/api/users/", s.handleUserByID)
```

`handleLogin`, `handleLogout`, `handleAuthSetup` dereference `s.auth`; guard each with
```go
	if s.auth == nil {
		writeError(w, http.StatusNotFound, "auth disabled")
		return
	}
```
at the top.

`maintenance/maintenance.go` end of `runOnce()`:

```go
	if n, err := m.store.DeleteExpiredSessions(time.Now()); err != nil {
		log.Printf("maintenance: prune sessions failed: %v", err)
	} else if n > 0 {
		log.Printf("maintenance: pruned %d expired session(s)", n)
	}
```

- [ ] **Step 5: Verify**

Run: `make test` → all `ok`, new tests PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum server/ maintenance/
git commit -m "feat(auth): login/logout/setup, users CRUD with last-admin guard, rate limit"
```

---

### Task 7: main.go wiring and `--create-admin`

**Files:**
- Modify: `main.go`
- Test: `main_test.go`

**Interfaces:**
- Consumes: `server.NewAuth`, `server.CreateOrResetAdmin`, `cfg.Auth`.
- Produces: `func runCreateAdmin(store db.Store, name string, in io.Reader, isTTY bool) error`.

- [ ] **Step 1: Failing test** (append to `main_test.go`)

```go
func TestRunCreateAdmin_EnvPassword(t *testing.T) {
	store, err := db.NewSQLiteStore(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.Migrate()
	t.Setenv("XIRC_ADMIN_PASSWORD", "password123")
	if err := runCreateAdmin(store, "Boss", strings.NewReader(""), false); err != nil {
		t.Fatal(err)
	}
	u, _ := store.GetUserByName("boss")
	if u == nil || u.Role != "admin" {
		t.Fatalf("admin not created: %+v", u)
	}
}

func TestRunCreateAdmin_NoPasswordNonTTY(t *testing.T) {
	store, _ := db.NewSQLiteStore(filepath.Join(t.TempDir(), "t.db"))
	defer store.Close()
	store.Migrate()
	os.Unsetenv("XIRC_ADMIN_PASSWORD")
	if err := runCreateAdmin(store, "boss", strings.NewReader(""), false); err == nil {
		t.Fatal("expected error without password source")
	}
}
```

- [ ] **Step 2: Run** `~/go-install/go/bin/go test . -run CreateAdmin -v` → FAIL (undefined).

- [ ] **Step 3: Implement**

Add flag in `main()`:
```go
	createAdmin := flag.String("create-admin", "", "create or reset an admin user, then exit (password from XIRC_ADMIN_PASSWORD or prompt)")
```

After `store.Migrate()`:
```go
	if *createAdmin != "" {
		if err := runCreateAdmin(store, *createAdmin, os.Stdin, isTerminal()); err != nil {
			log.Fatalf("create-admin: %v", err)
		}
		fmt.Printf("admin %q ready\n", strings.ToLower(strings.TrimSpace(*createAdmin)))
		return
	}

	auth, err := server.NewAuth(cfg.Auth, store, cfg.Server.Prefix)
	if err != nil {
		log.Printf("FATAL (config): %v (exit 78)", err)
		os.Exit(exitcodes.ExitConfig)
	}
```

After `srv.SetDownloadsDir(...)`: `srv.SetAuth(auth)`.

Helper:
```go
// runCreateAdmin reads the password from XIRC_ADMIN_PASSWORD, or prompts
// twice on a TTY (echo is visible; ponytail: add x/term for hidden input if wanted).
func runCreateAdmin(store db.Store, name string, in io.Reader, isTTY bool) error {
	pw := os.Getenv("XIRC_ADMIN_PASSWORD")
	if pw == "" {
		if !isTTY {
			return errors.New("set XIRC_ADMIN_PASSWORD or run interactively (docker exec -it)")
		}
		r := bufio.NewReader(in)
		fmt.Fprint(os.Stderr, "Password: ")
		a, _ := r.ReadString('\n')
		fmt.Fprint(os.Stderr, "Repeat:   ")
		b, _ := r.ReadString('\n')
		if strings.TrimRight(a, "\r\n") != strings.TrimRight(b, "\r\n") {
			return errors.New("passwords do not match")
		}
		pw = strings.TrimRight(a, "\r\n")
	}
	return server.CreateOrResetAdmin(store, name, pw)
}
```
(imports: `io`, `errors` already present? add as needed.)

- [ ] **Step 4: Verify** `make build && make test` → ok.

- [ ] **Step 5: Commit**

```bash
git add main.go main_test.go
git commit -m "feat(auth): wire auth into server, add --create-admin"
```

---

### Task 8: Frontend — login/setup overlay, admin gating, Users tab

**Files:**
- Modify: `web/js/api.js`, `web/js/app.js`, `web/index.html`

**Interfaces:**
- Consumes: `/api/auth/*`, `/api/users*` (Task 6).
- Produces (Alpine state): `me` (`{username, role, via}` or null), `isAdmin` (getter), `authMode` (`''|'login'|'setup'`), `authForm {username,password,password2,error}`, `users[]`, methods `loadMe()`, `submitAuth()`, `logout()`, `loadUsers()`, `addUser()`, `setUserRole(u, role)`, `resetUserPassword(u)`, `deleteUser(u)`.

- [ ] **Step 1: `api.js` — single request helper**

Replace `get`, `post`, `put`, `del` with:

```js
    async request(method, path, body) {
        const opts = { method, headers: {} };
        if (body !== undefined) {
            opts.headers['Content-Type'] = 'application/json';
            opts.body = JSON.stringify(body);
        } else if (method === 'POST' || method === 'PUT') {
            opts.headers['Content-Type'] = 'application/json';
            opts.body = '{}';
        }
        const res = await fetch(_apiBase + path, opts);
        if (res.status === 401 && !path.startsWith('/auth/')) {
            window.dispatchEvent(new CustomEvent('xirc:unauthorized'));
        }
        if (!res.ok && method !== 'GET') {
            const err = await res.json().catch(() => ({ error: res.statusText }));
            throw new Error(err.error || res.statusText);
        }
        return res.json();
    },
    get(path)        { return this.request('GET', path); },
    post(path, body) { return this.request('POST', path, body); },
    put(path, body)  { return this.request('PUT', path, body); },
    del(path)        { return this.request('DELETE', path); },
```
(Keeps existing semantics: GET returns JSON even on error; writes throw.)

Append API methods:

```js
    // Auth & users
    me()                  { return this.get('/auth/me'); },
    login(username, password) { return this.post('/auth/login', { username, password }); },
    setupAdmin(username, password) { return this.post('/auth/setup', { username, password }); },
    logout()              { return this.post('/auth/logout', {}); },
    getUsers()            { return this.get('/users'); },
    createUser(u)         { return this.post('/users', u); },
    updateUser(id, patch) { return this.put('/users/' + id, patch); },
    deleteUser(id)        { return this.del('/users/' + id); },
```

`listFiles` (api.js ~line 98) uses `fetch` directly — read it and route it through `this.request('GET', …)` if it doesn't already handle 401.

- [ ] **Step 2: `app.js` — state and methods**

Add to the data object (near `setupRequired`):

```js
        me: null,
        authMode: '',
        authForm: { username: '', password: '', password2: '', error: '' },
        users: [],
        newUser: { username: '', password: '', role: 'user' },
        get isAdmin() { return !!this.me && this.me.role === 'admin'; },
```

Add methods (next to the setup wizard block):

```js
        // --- Auth ---

        async loadMe() {
            const res = await fetch(_apiBase + '/auth/me');
            const data = await res.json().catch(() => ({}));
            if (res.ok && data.username) { this.me = data; this.authMode = ''; return true; }
            this.me = null;
            this.authMode = data.setup_required ? 'setup' : 'login';
            return false;
        },

        async submitAuth() {
            const f = this.authForm;
            f.error = '';
            if (this.authMode === 'setup' && f.password !== f.password2) { f.error = 'Passwords do not match'; return; }
            try {
                this.me = this.authMode === 'setup'
                    ? await api.setupAdmin(f.username, f.password)
                    : await api.login(f.username, f.password);
                this.authForm = { username: '', password: '', password2: '', error: '' };
                this.authMode = '';
                await this.init();
            } catch (e) { f.error = e.message; }
        },

        async logout() {
            await api.logout().catch(() => {});
            location.reload();
        },

        async loadUsers() { this.users = await api.getUsers().catch(() => []) || []; },

        async addUser() {
            try {
                await api.createUser(this.newUser);
                this.newUser = { username: '', password: '', role: 'user' };
                await this.loadUsers();
            } catch (e) { alert(e.message); }
        },

        async setUserRole(u, role) {
            try { await api.updateUser(u.id, { role }); } catch (e) { alert(e.message); }
            await this.loadUsers();
        },

        async resetUserPassword(u) {
            const pw = prompt('New password for ' + u.username + ' (min 8 chars)');
            if (!pw) return;
            try { await api.updateUser(u.id, { password: pw }); alert('Password changed; ' + u.username + ' was logged out.'); }
            catch (e) { alert(e.message); }
        },

        async deleteUser(u) {
            if (!confirm('Delete user ' + u.username + '?')) return;
            try { await api.deleteUser(u.id); } catch (e) { alert(e.message); }
            await this.loadUsers();
        },
```

At the top of `init()`:

```js
            if (!this._authListener) {
                this._authListener = true;
                window.addEventListener('xirc:unauthorized', () => { this.me = null; this.authMode = 'login'; });
            }
            if (!(await this.loadMe())) return; // overlay shown; submitAuth() re-runs init()
            if (this.isAdmin) await this.checkSetup();
```
and remove the old unconditional `await this.checkSetup();`. Guard the WebSocket so a re-run of `init()` doesn't open a second socket: in `wsConnect()` return early if `this.ws && this.ws.readyState <= 1` (read `wsConnect` first to use its actual socket field name).

`loadErrors()` is admin-only: in `init()` change `await this.loadErrors();` to `if (this.isAdmin) await this.loadErrors();`.

- [ ] **Step 3: `index.html` — overlay, gating, Users tab**

Before the "Startup wizard overlay" add:

```html
    <!-- Login / first-admin overlay -->
    <div x-show="authMode" x-cloak
         style="position:fixed; inset:0; z-index:10000; background:var(--bg-primary,#12121c); display:flex; align-items:center; justify-content:center; padding:16px;">
        <form @submit.prevent="submitAuth()"
              style="background:var(--bg-secondary,#1e1e2e); border-radius:8px; padding:32px; width:100%; max-width:360px; box-shadow:0 8px 32px rgba(0,0,0,0.5);">
            <h2 style="margin:0 0 6px; font-size:18px;" x-text="authMode === 'setup' ? 'Create admin account' : 'Sign in'"></h2>
            <p x-show="authMode === 'setup'" style="color:var(--text-secondary,#888); margin:0 0 16px; font-size:13px;">
                First start: this account can manage servers, users and settings.
            </p>
            <label style="display:block; font-size:12px; margin:12px 0 4px;">Username</label>
            <input class="input" style="width:100%;" x-model="authForm.username" autocomplete="username" autofocus required>
            <label style="display:block; font-size:12px; margin:12px 0 4px;">Password</label>
            <input class="input" style="width:100%;" type="password" x-model="authForm.password"
                   :autocomplete="authMode === 'setup' ? 'new-password' : 'current-password'" required minlength="8">
            <template x-if="authMode === 'setup'">
                <div>
                    <label style="display:block; font-size:12px; margin:12px 0 4px;">Repeat password</label>
                    <input class="input" style="width:100%;" type="password" x-model="authForm.password2" autocomplete="new-password" required minlength="8">
                </div>
            </template>
            <div x-show="authForm.error" x-text="authForm.error" role="alert" style="color:#f87171; font-size:13px; margin-top:12px;"></div>
            <button type="submit" class="btn btn-primary" style="width:100%; margin-top:20px;"
                    x-text="authMode === 'setup' ? 'Create account' : 'Sign in'"></button>
            <button type="button" class="btn" x-show="me" @click="authMode = ''" style="width:100%; margin-top:8px;">Cancel</button>
        </form>
    </div>
```

(Use the existing `input`/`btn`/`btn-primary` classes if present in `web/css/style.css`; otherwise copy the inline style used by the setup wizard inputs/buttons.)

Nav (`<nav class="sidebar-nav">`): change the Settings link `x-show="appMode === 'advanced'"` → `x-show="appMode === 'advanced' && isAdmin"`, and append after it:

```html
            <a x-show="me && me.via === 'session'" @click="logout()" :title="'Signed in as ' + (me && me.username)">Log out</a>
            <a x-show="me && me.via === 'network'" @click="authMode = 'login'">Log in</a>
```

Settings tabs (index.html ~line 967): add
```html
                        <div class="tab" :class="{ active: settingsTab === 'users' }" @click="settingsTab = 'users'; loadUsers()">Users</div>
```
and a tab body after the `general` template:

```html
                    <template x-if="settingsTab === 'users'">
                        <div>
                            <table class="table" style="width:100%;">
                                <thead><tr><th>User</th><th>Role</th><th></th></tr></thead>
                                <tbody>
                                    <template x-for="u in users" :key="u.id">
                                        <tr>
                                            <td x-text="u.username"></td>
                                            <td>
                                                <select :value="u.role" @change="setUserRole(u, $event.target.value)" :aria-label="'Role of ' + u.username">
                                                    <option value="user">user</option>
                                                    <option value="admin">admin</option>
                                                </select>
                                            </td>
                                            <td style="text-align:right;">
                                                <button class="btn" @click="resetUserPassword(u)">Reset password</button>
                                                <button class="btn btn-danger" @click="deleteUser(u)" :disabled="me && u.username === me.username">Delete</button>
                                            </td>
                                        </tr>
                                    </template>
                                </tbody>
                            </table>
                            <form @submit.prevent="addUser()" style="display:flex; gap:8px; flex-wrap:wrap; margin-top:16px;">
                                <input class="input" placeholder="username" x-model="newUser.username" required aria-label="New username">
                                <input class="input" type="password" placeholder="password (min 8)" x-model="newUser.password" required minlength="8" autocomplete="new-password" aria-label="New user password">
                                <select x-model="newUser.role" aria-label="New user role"><option value="user">user</option><option value="admin">admin</option></select>
                                <button type="submit" class="btn btn-primary">Add user</button>
                            </form>
                        </div>
                    </template>
```

Admin-only UI elsewhere — wrap with `x-show="isAdmin"` (search each and add to the existing `x-show` with `&&` if one exists):
- raw IRC command input (grep `sendRaw` in index.html/app.js for the input's markup)
- connect/disconnect/join buttons (grep `connectServer(`, `disconnectServer(`, `joinChannel(`)
- file manager mutating buttons (grep `fileAction(` callers: move, delete, rename, mkdir)
- downloads "delete"/"clear"/"move"/"set target" controls (grep `deleteDownloads(`, `clearDownloads(`, `moveDownload(`, `setDownloadTarget(`)
- "Clear index" button (grep `clearIndex`)
- pattern-trainer / "Teach the Parser" entry points (grep `openTrainer`)
- errors panel (grep `errors` view/panel)

- [ ] **Step 4: Verify (static)**

Run: `make build` (web is embedded; build must succeed).
Run: `node -e "require('fs').readFileSync('web/js/app.js','utf8')" && node --check web/js/app.js && node --check web/js/api.js` (if `node` is available; otherwise skip and note it).
Manual check list for the user (server is started by the user): fresh DB → setup overlay; login; user role hides Settings; network principal shows "Log in".

- [ ] **Step 5: Commit**

```bash
git add web/
git commit -m "feat(web): login and first-admin overlay, admin-only UI, users management"
```

---

### Task 9: `fscheck` package

**Files:**
- Create: `fscheck/fscheck.go`, `fscheck/fscheck_test.go`

**Interfaces:**
- Produces:
```go
type DirStatus struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Read   bool   `json:"read"`
	Write  bool   `json:"write"`
	Reason string `json:"reason,omitempty"`
}
func Probe(path string) DirStatus
func IsPermission(err error) bool          // EACCES, EPERM, EROFS
func Describe(err error, path string) error // wraps permission errors with owner/mode/uid; others unchanged
```

- [ ] **Step 1: Failing tests**

```go
package fscheck

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func skipIfRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission bits don't apply to root")
	}
}

func TestProbe(t *testing.T) {
	skipIfRoot(t)
	base := t.TempDir()
	rw := filepath.Join(base, "rw")
	ro := filepath.Join(base, "ro")
	none := filepath.Join(base, "none")
	os.Mkdir(rw, 0755)
	os.Mkdir(ro, 0555)
	os.Mkdir(none, 0000)
	t.Cleanup(func() { os.Chmod(ro, 0755); os.Chmod(none, 0755) })

	cases := []struct {
		path                string
		exists, read, write bool
	}{
		{rw, true, true, true},
		{ro, true, true, false},
		{none, true, false, false},
		{filepath.Join(base, "missing"), false, false, false},
	}
	for _, c := range cases {
		s := Probe(c.path)
		if s.Exists != c.exists || s.Read != c.read || s.Write != c.write {
			t.Errorf("%s: got %+v", filepath.Base(c.path), s)
		}
		if (!c.write) && s.Reason == "" {
			t.Errorf("%s: missing reason", filepath.Base(c.path))
		}
	}
	if entries, _ := os.ReadDir(rw); len(entries) != 0 {
		t.Error("probe file left behind")
	}
	if s := Probe(ro); !strings.Contains(s.Reason, "not writable") || !strings.Contains(s.Reason, "uid") {
		t.Errorf("reason lacks detail: %q", s.Reason)
	}
}

func TestDescribe(t *testing.T) {
	skipIfRoot(t)
	dir := t.TempDir()
	os.Chmod(dir, 0555)
	defer os.Chmod(dir, 0755)
	_, err := os.Create(filepath.Join(dir, "x"))
	d := Describe(err, dir)
	if !IsPermission(d) || !errors.Is(d, os.ErrPermission) {
		t.Errorf("Describe lost the permission error: %v", d)
	}
	if !strings.Contains(d.Error(), "permission denied: "+dir) {
		t.Errorf("message: %q", d.Error())
	}
	other := errors.New("boom")
	if Describe(other, dir) != other {
		t.Error("non-permission errors must pass through")
	}
	if !IsPermission(&os.PathError{Op: "open", Path: "/x", Err: syscall.EROFS}) {
		t.Error("EROFS should count as permission")
	}
}
```

- [ ] **Step 2: Run** `~/go-install/go/bin/go test ./fscheck/ -v` → FAIL (package missing).

- [ ] **Step 3: Implement** `fscheck/fscheck.go`

```go
// Package fscheck probes directory permissions and turns permission errors
// into messages that say who owns what and who xirc runs as.
package fscheck

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

type DirStatus struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Read   bool   `json:"read"`
	Write  bool   `json:"write"`
	Reason string `json:"reason,omitempty"`
}

func IsPermission(err error) bool {
	return errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EROFS)
}

func name(id uint32, lookup func(string) (string, error)) string {
	s := strconv.FormatUint(uint64(id), 10)
	if n, err := lookup(s); err == nil {
		return n
	}
	return s
}

// owner describes path's owner and mode, e.g. "owner root:root 0755".
func owner(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "unreadable"
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Sprintf("mode %04o", info.Mode().Perm())
	}
	u := name(st.Uid, func(s string) (string, error) { x, err := user.LookupId(s); if err != nil { return "", err }; return x.Username, nil })
	g := name(st.Gid, func(s string) (string, error) { x, err := user.LookupGroupId(s); if err != nil { return "", err }; return x.Name, nil })
	return fmt.Sprintf("owner %s:%s %04o", u, g, info.Mode().Perm())
}

func self() string {
	return fmt.Sprintf("xirc runs as uid %d gid %d", os.Geteuid(), os.Getegid())
}

func Probe(path string) DirStatus {
	s := DirStatus{Path: path}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.Reason = "does not exist: " + path
		} else {
			s.Reason = Describe(err, path).Error()
		}
		return s
	}
	if !info.IsDir() {
		s.Reason = "not a directory: " + path
		return s
	}
	s.Exists = true
	if _, err := os.ReadDir(path); err == nil {
		s.Read = true
	}
	probe := filepath.Join(path, ".xirc_write_check")
	if f, err := os.Create(probe); err == nil {
		f.Close()
		os.Remove(probe)
		s.Write = true
	}
	switch {
	case !s.Read:
		s.Reason = fmt.Sprintf("not readable: %s (%s; %s)", path, owner(path), self())
	case !s.Write:
		s.Reason = fmt.Sprintf("not writable: %s (%s; %s)", path, owner(path), self())
	}
	return s
}

type permError struct {
	msg string
	err error
}

func (e *permError) Error() string { return e.msg }
func (e *permError) Unwrap() error { return e.err }

// Describe wraps permission errors with the directory's owner/mode and the
// process identity. dir is the directory the operation targeted.
func Describe(err error, dir string) error {
	if err == nil || !IsPermission(err) {
		return err
	}
	if errors.Is(err, syscall.EROFS) {
		return &permError{fmt.Sprintf("permission denied: %s is on a read-only filesystem", dir), err}
	}
	return &permError{fmt.Sprintf("permission denied: %s (%s; %s)", dir, owner(dir), self()), err}
}
```
(Format the two lookup closures with gofmt — multi-line.)

- [ ] **Step 4: Verify** `~/go-install/go/bin/go test ./fscheck/ -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add fscheck/
git commit -m "feat(fscheck): directory permission probe and descriptive permission errors"
```

---

### Task 10: Capabilities — server snapshot, graceful degradation, point-of-use errors

**Files:**
- Create: `server/capabilities.go`, `server/capabilities_test.go`
- Modify: `server/server.go` (field + route), `server/download_handlers.go` (503 on request), `server/files_handler.go` (`statusFor`, `Describe`), `server/library_handlers.go` (PUT error mapping), `library/config.go` (`Manager.Path()`), `queue/engine.go` (moveNote), `irc/events.go` (`EventCapabilities`), `main.go` (no exit, logging dir, wiring)
- Test: `queue/move_fallback_test.go` (extend), `server/files_handler_test.go` (extend)

**Interfaces:**
- Consumes: `fscheck.Probe`, `fscheck.Describe`, `fscheck.IsPermission`.
- Produces:
```go
type Capability struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}
type Capabilities struct {
	Downloads     Capability          `json:"downloads"`
	Library       Capability          `json:"library"`
	LibraryRead   Capability          `json:"library_read"`
	LibraryConfig Capability          `json:"library_config"`
	Logging       Capability          `json:"logging"`
	Roots         []fscheck.DirStatus `json:"roots"`
	Docker        bool                `json:"docker"`
}
func (s *Server) SetCapabilityInputs(tempDir, logDir string, bus *irc.EventBus)
func (s *Server) RecheckCapabilities() Capabilities   // recompute, store, publish if changed
func (s *Server) capabilities() Capabilities          // current snapshot
func (s *Server) StartCapabilityRefresh(every time.Duration, stop <-chan struct{})
```
- Routes: `GET /api/capabilities`, `POST /api/capabilities/recheck` (admin via middleware).
- Event: `irc.EventCapabilities EventType = "capabilities"`, `Data: Capabilities`.

**Spec deviation (deliberate):** the spec's `auto_extract` capability is dropped — extraction targets the directory the file was just moved into, so a failed move (which already falls back and is reported) is the only way extraction can lack permission. Library config is covered by `library_config`.

- [ ] **Step 1: Failing tests** `server/capabilities_test.go`

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCapabilities_ReadOnlyDownloadsDisablesRequests(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()
	dl := t.TempDir()
	os.Chmod(dl, 0555)
	defer os.Chmod(dl, 0755)
	srv.SetDownloadsDir(dl)
	srv.SetCapabilityInputs(filepath.Join(dl, ".tmp"), t.TempDir(), nil)
	caps := srv.RecheckCapabilities()
	if caps.Downloads.OK || !strings.Contains(caps.Downloads.Reason, "not writable") {
		t.Fatalf("downloads capability: %+v", caps.Downloads)
	}

	req := httptest.NewRequest("POST", "/api/downloads/request", strings.NewReader(`{"server_id":1,"channel":"#x","bot_nick":"b","pack_number":1}`))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "not writable") {
		t.Errorf("request while disabled: %d %s", w.Code, w.Body)
	}

	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/capabilities", nil))
	var got Capabilities
	json.NewDecoder(w.Body).Decode(&got)
	if got.Downloads.OK || len(got.Roots) == 0 {
		t.Errorf("GET capabilities: %+v", got)
	}
}

func TestCapabilities_MissingTempDirIsFineIfParentWritable(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()
	dl := t.TempDir()
	srv.SetDownloadsDir(dl)
	srv.SetCapabilityInputs(filepath.Join(dl, ".tmp"), t.TempDir(), nil) // .tmp not created yet: engine MkdirAlls it
	if caps := srv.RecheckCapabilities(); !caps.Downloads.OK {
		t.Errorf("downloads should be OK: %+v", caps.Downloads)
	}
}
```

Extend `server/files_handler_test.go`:

```go
func TestFileManager_ReadOnlyRootReturns403WithReason(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.mkv"), []byte("x"), 0644)
	os.Chmod(root, 0555)
	defer os.Chmod(root, 0755)
	srv.SetDownloadsDir(root)

	body := `{"action":"rename","dir":"` + root + `","name":"a.mkv","new_name":"b.mkv"}`
	req := httptest.NewRequest("POST", "/api/files", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "permission denied: "+root) {
		t.Errorf("got %d %s", w.Code, w.Body)
	}
}
```

Extend `queue/move_fallback_test.go` (Review Focus #5) — read the file's existing fallback test first and copy its engine setup; the new case makes the **category/target dir** read-only (0555) and asserts:
```go
	if dl.Status != "completed" {
		t.Errorf("status %q", dl.Status)
	}
	if filepath.Dir(dl.DestinationPath) != downloadsDir {
		t.Errorf("file not in downloads dir: %s", dl.DestinationPath)
	}
	if !strings.Contains(dl.ErrorMessage, "permission denied: "+targetDir) {
		t.Errorf("reason not descriptive: %q", dl.ErrorMessage)
	}
	if entries, _ := os.ReadDir(tempDir); len(entries) != 0 {
		t.Error("file left in temp dir")
	}
```
(If the existing test records the note in a field other than `ErrorMessage`, assert on that field — check how `moveNote` is persisted in `finishTransfer`.)

- [ ] **Step 2: Run** `~/go-install/go/bin/go test ./server/ ./queue/ -run 'Capabilities|ReadOnlyRoot|Fallback' -v` → FAIL.

- [ ] **Step 3: Implement**

`irc/events.go`: add `EventCapabilities EventType = "capabilities"` to the const block.

`library/config.go`: add
```go
// Path is the categories.yaml location this manager saves to.
func (m *Manager) Path() string { return m.path }
```
(check the struct's field name for the path and use it.)

`server/capabilities.go`:

```go
package server

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/RealDtx/maxwell-irc/fscheck"
	"github.com/RealDtx/maxwell-irc/irc"
)

type Capability struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

type Capabilities struct {
	Downloads     Capability          `json:"downloads"`
	Library       Capability          `json:"library"`
	LibraryRead   Capability          `json:"library_read"`
	LibraryConfig Capability          `json:"library_config"`
	Logging       Capability          `json:"logging"`
	Roots         []fscheck.DirStatus `json:"roots"`
	Docker        bool                `json:"docker"`
}

type capState struct {
	mu      sync.Mutex
	tempDir string
	logDir  string
	bus     *irc.EventBus
	current *Capabilities
}

func (s *Server) SetCapabilityInputs(tempDir, logDir string, bus *irc.EventBus) {
	s.caps.mu.Lock()
	s.caps.tempDir, s.caps.logDir, s.caps.bus = tempDir, logDir, bus
	s.caps.mu.Unlock()
}

// writableOrCreatable: an existing dir must be writable; a missing one is
// fine when its nearest existing parent is (the engine MkdirAlls it).
func writableOrCreatable(dir string) Capability {
	for d := dir; ; d = filepath.Dir(d) {
		st := fscheck.Probe(d)
		if st.Exists {
			return Capability{OK: st.Write, Reason: st.Reason}
		}
		if filepath.Dir(d) == d {
			return Capability{Reason: st.Reason}
		}
	}
}

func firstFailure(dirs ...string) Capability {
	for _, d := range dirs {
		if d == "" {
			continue
		}
		if c := writableOrCreatable(d); !c.OK {
			return c
		}
	}
	return Capability{OK: true}
}

func (s *Server) computeCapabilities() Capabilities {
	s.caps.mu.Lock()
	tempDir, logDir := s.caps.tempDir, s.caps.logDir
	s.caps.mu.Unlock()

	c := Capabilities{Roots: []fscheck.DirStatus{}}
	_, err := os.Stat("/.dockerenv")
	c.Docker = err == nil

	for _, r := range s.configuredRoots() {
		c.Roots = append(c.Roots, fscheck.Probe(r))
	}
	c.Downloads = firstFailure(s.downloadsDir, tempDir)
	c.Logging = firstFailure(logDir)
	c.Library, c.LibraryRead, c.LibraryConfig = Capability{OK: true}, Capability{OK: true}, Capability{OK: true}
	if s.library != nil {
		cfg := s.library.Get()
		var dirs []string
		dirs = append(dirs, cfg.MediaRoot)
		for _, cat := range cfg.Categories {
			if !cat.Enabled || cat.Dir == "" {
				continue
			}
			d := cat.Dir
			if !filepath.IsAbs(d) {
				d = filepath.Join(cfg.MediaRoot, d)
			}
			dirs = append(dirs, d)
		}
		c.Library = firstFailure(dirs...)
		if st := fscheck.Probe(cfg.MediaRoot); !st.Read {
			c.LibraryRead = Capability{Reason: st.Reason}
		}
		c.LibraryConfig = firstFailure(filepath.Dir(s.library.Path()))
	}
	return c
}

func (s *Server) RecheckCapabilities() Capabilities {
	c := s.computeCapabilities()
	s.caps.mu.Lock()
	changed := s.caps.current == nil || !reflect.DeepEqual(*s.caps.current, c)
	s.caps.current = &c
	bus := s.caps.bus
	s.caps.mu.Unlock()
	if changed && bus != nil {
		bus.Publish(irc.Event{Type: irc.EventCapabilities, Data: c})
	}
	return c
}

func (s *Server) capabilities() Capabilities {
	s.caps.mu.Lock()
	cur := s.caps.current
	s.caps.mu.Unlock()
	if cur == nil {
		return s.RecheckCapabilities()
	}
	return *cur
}

func (s *Server) StartCapabilityRefresh(every time.Duration, stop <-chan struct{}) {
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				s.RecheckCapabilities()
			case <-stop:
				return
			}
		}
	}()
}

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, s.capabilities())
}

func (s *Server) handleCapabilitiesRecheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, s.RecheckCapabilities())
}
```

`server/server.go`: field `caps capState`; routes:
```go
	s.mux.HandleFunc("/api/capabilities", s.handleCapabilities)
	s.mux.HandleFunc("/api/capabilities/recheck", s.handleCapabilitiesRecheck)
```

`server/download_handlers.go` in `handleRequestDownload`, right after the method check:
```go
	if c := s.capabilities().Downloads; !c.OK {
		writeError(w, http.StatusServiceUnavailable, "downloads disabled: "+c.Reason)
		return
	}
```

`server/files_handler.go`:
- In `statusFor` add first case: `case fscheck.IsPermission(err): return http.StatusForbidden`.
- Wrap each mutation's error with the directory it targeted: `moveItem` return `fscheck.Describe(os.Rename(src, dst), destDir)`; delete → `fscheck.Describe(os.RemoveAll(p), dir)`; rename → `err = fscheck.Describe(os.Rename(src, dst), dir)`; mkdir: before the generic 500, `if fscheck.IsPermission(err) { writeError(w, http.StatusForbidden, fscheck.Describe(err, dir).Error()); return }`.
- In `handleListFiles`, replace `writeError(w, http.StatusForbidden, "permission denied")` with `writeError(w, http.StatusForbidden, fscheck.Describe(err, clean).Error())`.

`server/library_handlers.go` PUT branch: where `s.library.Set(...)` fails, map `fscheck.IsPermission(err)` → 403 with `fscheck.Describe(err, filepath.Dir(s.library.Path())).Error()` (keep existing status otherwise).

`queue/engine.go` in the move-failure branch:
```go
			moveNote = fmt.Sprintf("could not move to %s: %v", destDir, fscheck.Describe(err, destDir))
```
(Check that `routing.MoveFile` returns the raw `*os.PathError` — it does for rename/mkdir — so `IsPermission` sees it through `%w` wrapping in "creating destination dir: %w".)

`main.go`:
- Replace the body of `checkDirectories` permission branch: instead of `os.Exit`, log
  `log.Printf("warning: %s — dependent features disabled until fixed (see /api/capabilities)", fscheck.Probe(dir).Reason)` and `continue`. Keep the missing-dir → wizard path. Keep the transient-error exit.
- Before `msgBuf.SetLogDir(...)`:
```go
	logDir := filepath.Join(filepath.Dir(cfg.Database.Path), "logs")
	os.MkdirAll(logDir, 0o755)
	if st := fscheck.Probe(logDir); st.Write {
		msgBuf.SetLogDir(logDir)
	} else {
		log.Printf("warning: channel logging disabled: %s", st.Reason)
	}
```
(replaces the existing `msgBuf.SetLogDir(filepath.Dir(cfg.Database.Path) + "/logs")`).
- After `srv.SetAuth(auth)`:
```go
	srv.SetCapabilityInputs(cfg.Storage.TempDir, logDir, bus)
	caps := srv.RecheckCapabilities()
	for _, c := range []struct {
		name string
		cap  server.Capability
	}{{"downloads", caps.Downloads}, {"library", caps.Library}, {"library config", caps.LibraryConfig}} {
		if !c.cap.OK {
			log.Printf("warning: %s disabled: %s", c.name, c.cap.Reason)
		}
	}
	capStop := make(chan struct{})
	srv.StartCapabilityRefresh(5*time.Minute, capStop)
```
Close `capStop` in the shutdown goroutine.

- [ ] **Step 4: Verify** `make test` → all ok (new tests PASS; skipped as root).

- [ ] **Step 5: Commit**

```bash
git add fscheck/ server/ queue/ library/ irc/events.go main.go
git commit -m "feat: filesystem capabilities, degrade instead of exit on missing permissions"
```

---

### Task 11: Frontend — capability UI

**Files:**
- Modify: `web/js/api.js`, `web/js/app.js`, `web/index.html`

**Interfaces:**
- Consumes: `GET /api/capabilities`, `POST /api/capabilities/recheck`, WS event `type === 'capabilities'` (payload in `data.data`).
- Produces: Alpine state `caps` (Capabilities or null), `capIssues` getter, `rootWritable(path)`, `recheckCaps()`.

- [ ] **Step 1: api.js**

```js
    getCapabilities()    { return this.get('/capabilities'); },
    recheckCapabilities(){ return this.post('/capabilities/recheck', {}); },
```

- [ ] **Step 2: app.js**

State: `caps: null,`

```js
        get capIssues() {
            if (!this.caps) return [];
            const names = { downloads: 'Downloads', library: 'Library sorting', library_read: 'Library browsing', library_config: 'Library settings', logging: 'Channel logging' };
            return Object.keys(names).filter(k => this.caps[k] && !this.caps[k].ok)
                .map(k => ({ name: names[k], reason: this.caps[k].reason }));
        },
        capFix(reason) {
            const m = /: (\/\S+)/.exec(reason || '');
            const dir = m ? m[1] : '<dir>';
            return this.caps && this.caps.docker
                ? 'Set PUID/PGID in docker-compose to the owner of the host directory mounted at ' + dir
                : 'sudo chown -R xirc:xirc ' + dir;
        },
        rootWritable(path) {
            if (!this.caps) return true;
            const r = (this.caps.roots || []).find(x => path === x.path || path.startsWith(x.path + '/'));
            return !r || r.write;
        },
        async loadCaps() { this.caps = await api.getCapabilities().catch(() => null); },
        async recheckCaps() { this.caps = await api.recheckCapabilities().catch(() => this.caps); },
```

In `init()` after `loadMe()` succeeds: `await this.loadCaps();`.
In the WebSocket message handler (find the `switch`/`if` on `data.type`), add: `if (data.type === 'capabilities') { this.caps = data.data; }`.

- [ ] **Step 3: index.html**

Admin banner (top of main content area, before the first `<template x-if="activeView === 'channel'">`):

```html
        <div x-show="isAdmin && capIssues.length" x-cloak role="status"
             style="background:#5c3a1a; color:#fff; padding:10px 16px; font-size:13px; border-radius:6px; margin:8px;">
            <template x-for="i in capIssues" :key="i.name">
                <div style="margin-bottom:6px;">
                    <strong x-text="i.name + ' disabled'"></strong> — <span x-text="i.reason"></span>
                    <div style="font-family:monospace; opacity:.85;" x-text="'Fix: ' + capFix(i.reason)"></div>
                </div>
            </template>
            <button class="btn" @click="recheckCaps()">Recheck</button>
        </div>
```

Disable-with-reason (everyone): on every "download" button (grep `requestDownload(` in index.html), add
`:disabled="caps && !caps.downloads.ok" :title="caps && !caps.downloads.ok ? caps.downloads.reason : ''"`
(merge with any existing `:disabled` via `||`).
On file-manager mutating buttons (already `x-show="isAdmin"` from Task 8), add `:disabled="!rootWritable(filesDir)"` using the variable holding the current directory (read the files view to find its name) and a matching `:title`.
On the Library settings save button: `:disabled="caps && !caps.library_config.ok"` + `:title`.

- [ ] **Step 4: Verify** `make build`; `node --check web/js/app.js web/js/api.js` if node exists.

- [ ] **Step 5: Commit**

```bash
git add web/
git commit -m "feat(web): show disabled features with reason, admin fix banner and recheck"
```

---

### Task 12: Docker image, compose, release workflow

**Files:**
- Modify: `deploy/Dockerfile`, `deploy/docker-compose.yaml`, `.github/workflows/release.yml`, `.github/workflows/ci.yml` (Go version source), `Makefile` (`docker` target tag)
- Create: `deploy/.env.example`, `.dockerignore`

- [ ] **Step 1: `deploy/Dockerfile`**

```dockerfile
FROM golang:1.27-alpine AS builder
ARG VERSION=dev
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=${VERSION}" -o /xirc .

FROM alpine:3.24
ARG PUID=1000
ARG PGID=1000
RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -g ${PGID} xirc \
 && adduser -D -H -u ${PUID} -G xirc xirc \
 && mkdir -p /data /downloads /media \
 && chown xirc:xirc /data /downloads /media
COPY --from=builder /xirc /usr/local/bin/xirc

# No config file is baked in: defaults + these env vars; mount
# /data/config.yaml to override anything.
ENV XIRC_SERVER_HOST=0.0.0.0 \
    XIRC_DATABASE_PATH=/data/xirc.db \
    XIRC_STORAGE_DOWNLOADS_DIR=/downloads \
    XIRC_STORAGE_TEMP_DIR=/downloads/.tmp \
    XIRC_STORAGE_MEDIA_DIR=/media \
    XIRC_STORAGE_CATEGORIES_FILE=/data/categories.yaml

USER xirc
WORKDIR /data
VOLUME ["/data", "/downloads", "/media"]
EXPOSE 8085
HEALTHCHECK --interval=30s --timeout=5s CMD wget -qO- http://127.0.0.1:8085/api/health || exit 1
ENTRYPOINT ["xirc", "--config", "/data/config.yaml"]
```

- [ ] **Step 2: `.dockerignore`**

```
.git
data
.maxwell
.*/
xirc
xirc-*
maxwell-irc
maxwell-irc-*
```
(Check `.dockerignore` doesn't exclude `web/` — it's embedded.)

- [ ] **Step 3: `deploy/docker-compose.yaml`**

```yaml
services:
  xirc:
    image: ghcr.io/realdtx/xirc:latest
    build:
      context: ..
      dockerfile: deploy/Dockerfile
      args:
        PUID: ${PUID:-1000}
        PGID: ${PGID:-1000}
    container_name: xirc
    restart: unless-stopped
    ports:
      # Behind a reverse proxy on this host? Use "127.0.0.1:8085:8085".
      - "8085:8085"
    volumes:
      - ./data:/data
      # Keep downloads and media on the same filesystem so finished files are
      # renamed instantly instead of copied.
      - ${DOWNLOADS_DIR:?set DOWNLOADS_DIR in .env}:/downloads
      - ${MEDIA_DIR:?set MEDIA_DIR in .env}:/media
    environment:
      - TZ=${TZ:-UTC}
      # LAN clients skip login (comma-separated CIDRs). Only safe when the
      # container sees real client IPs (no NAT in between) or sits behind a
      # proxy listed in XIRC_AUTH_TRUSTED_PROXIES.
      - XIRC_AUTH_TRUSTED_NETWORKS=${TRUSTED_NETWORKS:-}
      - XIRC_AUTH_TRUSTED_ROLE=${TRUSTED_ROLE:-admin}
    networks:
      - xirc

  db:
    image: mariadb:11
    container_name: xirc-db
    restart: unless-stopped
    profiles:
      - mariadb
    environment:
      - MYSQL_ROOT_PASSWORD=${MYSQL_ROOT_PASSWORD:?set in .env}
      - MYSQL_DATABASE=xirc
      - MYSQL_USER=xirc
      - MYSQL_PASSWORD=${MYSQL_PASSWORD:?set in .env}
    volumes:
      - db_data:/var/lib/mysql
    networks:
      - xirc

networks:
  xirc:
    driver: bridge

volumes:
  db_data:
```

Note: `${VAR:?}` on the MariaDB service is evaluated for the whole file. Put the two MySQL vars in `.env.example` with a placeholder value and a comment, so SQLite users only need to leave them as-is.

- [ ] **Step 4: `deploy/.env.example`**

```bash
# Copy to deploy/.env and adjust.
# Host directories (must exist, owned by PUID:PGID below)
DOWNLOADS_DIR=/srv/downloads
MEDIA_DIR=/srv/media
# Owner of those directories on the host: `id -u` / `id -g`
PUID=1000
PGID=1000
TZ=UTC
# Optional: LAN ranges that skip login, e.g. 192.168.0.0/16,10.0.0.0/8
TRUSTED_NETWORKS=
TRUSTED_ROLE=admin
# Only used with `--profile mariadb` (then set database.driver/dsn via env or config)
MYSQL_ROOT_PASSWORD=change-me-root
MYSQL_PASSWORD=change-me
```

- [ ] **Step 5: `.github/workflows/release.yml`**

Read the current file; then make the build job use `go-version-file: go.mod`, rename outputs to `xirc-linux-arm64`, `xirc-linux-armv7`, `xirc-linux-amd64` (same `go build` flags), add `prerelease: ${{ contains(github.ref_name, '-') }}` to the `softprops/action-gh-release@v2` step, and append a job:

```yaml
  image:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
    steps:
      - uses: actions/checkout@v4
      - uses: docker/setup-qemu-action@v3
      - uses: docker/setup-buildx-action@v3
      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - name: Lowercase owner
        id: owner
        run: echo "name=${GITHUB_REPOSITORY_OWNER,,}" >> "$GITHUB_OUTPUT"
      - uses: docker/build-push-action@v6
        with:
          context: .
          file: deploy/Dockerfile
          platforms: linux/amd64,linux/arm64
          push: true
          build-args: VERSION=${{ github.ref_name }}
          tags: |
            ghcr.io/${{ steps.owner.outputs.name }}/xirc:${{ github.ref_name }}
            ghcr.io/${{ steps.owner.outputs.name }}/xirc:latest
```

In `.github/workflows/ci.yml`, if it pins a Go version, switch to `go-version-file: go.mod`.

`Makefile` docker target: `docker build -f deploy/Dockerfile --build-arg VERSION=$(VERSION) -t xirc .`

- [ ] **Step 6: Verify**

```bash
docker build -f deploy/Dockerfile -t xirc:test . 2>&1 | tail -3   # if docker is available
python3 -c "import yaml,sys; [yaml.safe_load(open(f)) for f in ['deploy/docker-compose.yaml','.github/workflows/release.yml','.github/workflows/ci.yml']]" && echo yaml-ok
```
Expected: `yaml-ok`; docker build succeeds if docker exists (otherwise note "docker not available locally; verified by CI on tag").

- [ ] **Step 7: Commit**

```bash
git add deploy/ .dockerignore .github/ Makefile
git commit -m "feat(docker): config-less image with PUID/PGID, compose .env, GHCR release"
```

---

### Task 13: Reverse-proxy templates

**Files:**
- Create: `scripts/proxy-templates.sh`, `scripts/test-proxy-templates.sh`, `deploy/nginx-xirc.conf`, `deploy/apache-xirc.conf`
- Delete: `deploy/nginx-maxwell-irc.conf`

**Interfaces:**
- Produces (bash, sourced): `render_nginx_site`, `render_nginx_subpath`, `render_apache_site`, `render_apache_subpath`, `proxy_enable_hint <nginx|apache> <site|subpath>`. Inputs via env: `PORT` (default 8085), `PREFIX` (e.g. `/xirc`, subpath only), `SERVER_NAME` (site only). Output: config text on stdout.

- [ ] **Step 1: Write the test script first** `scripts/test-proxy-templates.sh`

```bash
#!/usr/bin/env bash
# Renders every proxy variant and checks the essentials; runs the real
# config testers when nginx/apache are installed.
set -euo pipefail
cd "$(dirname "$0")"
source ./proxy-templates.sh
fail=0
check() { # name, text, pattern...
    local name="$1" text="$2"; shift 2
    for p in "$@"; do
        grep -qF -- "$p" <<<"$text" || { echo "FAIL $name: missing '$p'"; fail=1; }
    done
}
export PORT=8085 SERVER_NAME=xirc.example.com PREFIX=/xirc
check nginx_site    "$(render_nginx_site)"    "server_name xirc.example.com" "proxy_pass http://127.0.0.1:8085" 'Upgrade $http_upgrade' "X-Forwarded-Proto"
check nginx_subpath "$(render_nginx_subpath)" "location /xirc/" "proxy_pass http://127.0.0.1:8085/" "location /xirc/ws" 'Upgrade $http_upgrade'
check apache_site   "$(render_apache_site)"   "ServerName xirc.example.com" "ProxyPass / http://127.0.0.1:8085/" "ws://127.0.0.1:8085/" "ProxyPreserveHost On" "X-Forwarded-Proto"
check apache_subpath "$(render_apache_subpath)" "ProxyPass /xirc/ http://127.0.0.1:8085/" "ws://127.0.0.1:8085/ws" "ProxyPreserveHost On"
if command -v nginx >/dev/null; then
    tmp=$(mktemp -d); { echo "events {} http {"; render_nginx_site; echo "}"; } >"$tmp/n.conf"
    nginx -t -c "$tmp/n.conf" -p "$tmp" 2>&1 | tail -1 || fail=1
fi
[[ $fail == 0 ]] && echo "proxy templates ok"
exit $fail
```

- [ ] **Step 2: Run** `bash scripts/test-proxy-templates.sh` → FAIL (`proxy-templates.sh` missing).

- [ ] **Step 3: Implement** `scripts/proxy-templates.sh`

```bash
#!/usr/bin/env bash
# Reverse-proxy config renderers for xirc — the single source for install.sh,
# preconfig.sh and the examples in deploy/. Source this file, set PORT
# (default 8085) plus SERVER_NAME (site) or PREFIX (subpath), call a render_*.
# Subpath variants strip the prefix; set server.prefix to the same value.

render_nginx_site() {
    local port="${PORT:-8085}"
    cat <<EOF
# xirc — nginx site. Enable: see proxy_enable_hint / docs/install.md
server {
    listen 80;
    server_name ${SERVER_NAME};

    # Optional: restrict to your LAN (xirc has its own login, so this is extra).
    # allow 192.168.0.0/16;
    # allow 10.0.0.0/8;
    # allow 172.16.0.0/12;
    # deny all;

    location / {
        proxy_pass http://127.0.0.1:${port};
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }

    location /ws {
        proxy_pass http://127.0.0.1:${port};
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host \$host;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_read_timeout 86400;
    }
}
EOF
}

render_nginx_subpath() {
    local port="${PORT:-8085}" p="${PREFIX%/}"
    cat <<EOF
# xirc — nginx locations for an existing server { } block:
#   include /etc/nginx/snippets/xirc.conf;
# Requires server.prefix: ${p} in xirc's config.yaml.
location = ${p} {
    return 301 ${p}/;
}

location ${p}/ {
    proxy_pass http://127.0.0.1:${port}/;
    proxy_set_header Host \$host;
    proxy_set_header X-Real-IP \$remote_addr;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto \$scheme;
}

location ${p}/ws {
    proxy_pass http://127.0.0.1:${port}/ws;
    proxy_http_version 1.1;
    proxy_set_header Upgrade \$http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host \$host;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto \$scheme;
    proxy_read_timeout 86400;
}
EOF
}

render_apache_site() {
    local port="${PORT:-8085}"
    cat <<EOF
# xirc — Apache site. Requires:
#   a2enmod proxy proxy_http proxy_wstunnel rewrite headers
<VirtualHost *:80>
    ServerName ${SERVER_NAME}

    ProxyPreserveHost On
    ProxyTimeout 86400
    RequestHeader set X-Forwarded-Proto expr=%{REQUEST_SCHEME}

    RewriteEngine On
    RewriteCond %{HTTP:Upgrade} websocket [NC]
    RewriteCond %{HTTP:Connection} upgrade [NC]
    RewriteRule ^/ws$ ws://127.0.0.1:${port}/ws [P,L]

    ProxyPass / http://127.0.0.1:${port}/
    ProxyPassReverse / http://127.0.0.1:${port}/
</VirtualHost>
EOF
}

render_apache_subpath() {
    local port="${PORT:-8085}" p="${PREFIX%/}"
    cat <<EOF
# xirc — Apache snippet for an existing <VirtualHost> (Include it there).
# Requires: a2enmod proxy proxy_http proxy_wstunnel rewrite headers
# Requires server.prefix: ${p} in xirc's config.yaml.
ProxyPreserveHost On
RequestHeader set X-Forwarded-Proto expr=%{REQUEST_SCHEME}
RedirectMatch 301 ^${p}\$ ${p}/

RewriteEngine On
RewriteCond %{HTTP:Upgrade} websocket [NC]
RewriteCond %{HTTP:Connection} upgrade [NC]
RewriteRule ^${p}/ws\$ ws://127.0.0.1:${port}/ws [P,L]

ProxyPass ${p}/ http://127.0.0.1:${port}/ timeout=86400
ProxyPassReverse ${p}/ http://127.0.0.1:${port}/
EOF
}

# proxy_enable_hint <nginx|apache> <site|subpath> — prints manual enable steps.
proxy_enable_hint() {
    case "$1:$2" in
        nginx:site)    echo "sudo cp xirc.conf /etc/nginx/sites-available/xirc && sudo ln -sf /etc/nginx/sites-available/xirc /etc/nginx/sites-enabled/xirc && sudo nginx -t && sudo systemctl reload nginx" ;;
        nginx:subpath) echo "sudo cp xirc.conf /etc/nginx/snippets/xirc.conf, add 'include /etc/nginx/snippets/xirc.conf;' to your server { } block, then: sudo nginx -t && sudo systemctl reload nginx" ;;
        apache:site)   echo "sudo a2enmod proxy proxy_http proxy_wstunnel rewrite headers && sudo cp xirc.conf /etc/apache2/sites-available/xirc.conf && sudo a2ensite xirc && sudo apachectl configtest && sudo systemctl reload apache2" ;;
        apache:subpath) echo "sudo a2enmod proxy proxy_http proxy_wstunnel rewrite headers && sudo cp xirc.conf /etc/apache2/conf-available/xirc.conf, add 'Include conf-available/xirc.conf' inside your <VirtualHost>, then: sudo apachectl configtest && sudo systemctl reload apache2" ;;
    esac
}
```

- [ ] **Step 4: Generate checked-in examples**

```bash
source scripts/proxy-templates.sh
PORT=8085 SERVER_NAME=xirc.example.com render_nginx_site  > deploy/nginx-xirc.conf
PORT=8085 SERVER_NAME=xirc.example.com render_apache_site > deploy/apache-xirc.conf
git rm deploy/nginx-maxwell-irc.conf
grep -rn "nginx-maxwell-irc.conf" --include=*.md --include=Makefile . | grep -v docs/superpowers   # fix references to point at deploy/nginx-xirc.conf
```

- [ ] **Step 5: Verify** `bash scripts/test-proxy-templates.sh` → `proxy templates ok`. `bash -n scripts/proxy-templates.sh`.

- [ ] **Step 6: Commit**

```bash
git add scripts/ deploy/
git commit -m "feat(deploy): shared nginx/Apache proxy templates with WebSocket + subpath"
```

---

### Task 14: preconfig.sh + Makefile use templates, Apache option

**Files:**
- Modify: `scripts/preconfig.sh`, `Makefile` (`deploy` target)

**Interfaces:**
- Consumes: Task 13 renderers.
- Produces in profile dir: `xirc.service` (was `maxwell-irc.service` — Makefile already expects `xirc.service`), and one of `nginx-xirc.conf` | `nginx-xirc-location.conf` | `apache-xirc.conf` | `apache-xirc-location.conf`; `config.yaml` gains an `auth:` block.

- [ ] **Step 1: Refactor preconfig.sh**

- At top after `set -euo pipefail`: `source "$(dirname "$0")/proxy-templates.sh"`.
- Replace the `# ── nginx ──` question block with:

```bash
echo
echo "  -- Reverse proxy --"
ask "Reverse proxy (nginx / apache / none)" "nginx" PROXY
PROXY="${PROXY,,}"
PROXY_MODE=""; PROXY_PREFIX=""; PROXY_SERVER_NAME=""
if [[ "$PROXY" == "nginx" || "$PROXY" == "apache" ]]; then
    echo "    s) Own site   — new ${PROXY} site with its own hostname"
    echo "    i) Subpath    — snippet to include in an existing site"
    ask "Mode" "i" PROXY_MODE
    if [[ "${PROXY_MODE,,}" == s* ]]; then
        PROXY_MODE="site"
        ask "Hostname for the site" "xirc.local" PROXY_SERVER_NAME
    else
        PROXY_MODE="subpath"
        ask "URL prefix (no trailing slash)" "/xirc" PROXY_PREFIX
    fi
else
    PROXY="none"
    echo "  Without a proxy, put xirc behind TLS before exposing it to the internet."
fi

echo
echo "  -- Login --"
ask "Networks that skip login (comma-separated CIDRs, empty = always log in)" "" TRUSTED_NETWORKS
ask "Role for those networks (admin / user)" "admin" TRUSTED_ROLE
```

- Replace the `# --- nginx-maxwell-irc.conf (or sentinel) ---` generation block with:

```bash
rm -f "${PROFILE_DIR}"/nginx-*.conf "${PROFILE_DIR}"/apache-*.conf "${PROFILE_DIR}/.no-nginx"
if [[ "$PROXY" != "none" ]]; then
    suffix=""; [[ "$PROXY_MODE" == "subpath" ]] && suffix="-location"
    PORT="$XIRC_PORT" PREFIX="$PROXY_PREFIX" SERVER_NAME="$PROXY_SERVER_NAME" \
        "render_${PROXY}_${PROXY_MODE}" > "${PROFILE_DIR}/${PROXY}-xirc${suffix}.conf"
fi
```

- `PREFIX_LINE`: use `$PROXY_PREFIX` instead of `$NGINX_PREFIX`.
- Rename generated unit file to `${PROFILE_DIR}/xirc.service`; set `ExecStart=${INSTALL_DIR}/xirc --config ${INSTALL_DIR}/config.yaml`.
- Default install dir `/opt/xirc`, service user `xirc`, SQLite path `${INSTALL_DIR}/data/xirc.db`.
- In the config heredoc, append:

```bash
auth:
  trusted_networks: [${TRUSTED_NETWORKS}]
  trusted_role: ${TRUSTED_ROLE}
```
- Update the summary echo lines to the new file names.

- [ ] **Step 2: Makefile `deploy`**

Replace the two nginx `@if` blocks with:

```make
	@for f in nginx-xirc.conf nginx-xirc-location.conf apache-xirc.conf apache-xirc-location.conf; do \
	  [ -f "$(PROFILE_DIR)/$$f" ] || continue; \
	  rsync -avz "$(PROFILE_DIR)/$$f" "$(PI_USER)@$(PI_HOST)":/tmp/; \
	  case $$f in \
	    nginx-xirc.conf) ssh "$(PI_USER)@$(PI_HOST)" "sudo cp /tmp/$$f /etc/nginx/sites-available/xirc && sudo ln -sf /etc/nginx/sites-available/xirc /etc/nginx/sites-enabled/xirc && sudo nginx -t && sudo systemctl reload nginx" ;; \
	    nginx-xirc-location.conf) ssh "$(PI_USER)@$(PI_HOST)" "sudo cp /tmp/$$f /etc/nginx/snippets/xirc.conf && sudo nginx -t && sudo systemctl reload nginx" ;; \
	    apache-xirc.conf) ssh "$(PI_USER)@$(PI_HOST)" "sudo a2enmod -q proxy proxy_http proxy_wstunnel rewrite headers && sudo cp /tmp/$$f /etc/apache2/sites-available/xirc.conf && sudo a2ensite -q xirc && sudo apachectl configtest && sudo systemctl reload apache2" ;; \
	    apache-xirc-location.conf) ssh "$(PI_USER)@$(PI_HOST)" "sudo a2enmod -q proxy proxy_http proxy_wstunnel rewrite headers && sudo cp /tmp/$$f /etc/apache2/conf-available/xirc.conf && sudo apachectl configtest && sudo systemctl reload apache2" ;; \
	  esac; \
	done
```

Backward compatibility: the author's existing profile has `nginx-xirc.conf` — handled by the first case.

- [ ] **Step 3: Verify**

```bash
bash -n scripts/preconfig.sh
printf 'ptest\nmaxwell.local\nmaxwell\n/opt/xirc\nxirc\n8085\n/srv/downloads\n/srv/media\n\nn\nsqlite\n\nn\napache\ns\nxirc.local\n192.168.0.0/16\nadmin\n' | PROFILE=ptest bash scripts/preconfig.sh >/dev/null
ls .ptest/ && grep -A3 '^auth:' .ptest/config.yaml && grep ServerName .ptest/apache-xirc.conf
rm -rf .ptest && git checkout .gitignore
make -n deploy PROFILE=maxwell | grep -c ssh   # dry-run parses
```
(Adjust the answer sequence to the prompts' actual order if the script asks something else; the goal is one full non-interactive run.)
Expected: files `apache-xirc.conf config.yaml settings.mk xirc.service`, auth block with the CIDR, `ServerName xirc.local`.

- [ ] **Step 4: Commit**

```bash
git add scripts/preconfig.sh Makefile
git commit -m "feat(deploy): preconfig offers nginx or Apache, login settings; deploy installs either"
```

---

### Task 15: `scripts/install.sh` — on-host wizard

**Files:**
- Create: `scripts/install.sh`

**Interfaces:**
- Consumes: Task 13 renderers, `xirc --create-admin` not needed (web setup), `deploy/maxwell-irc.service` contents as reference for the unit.
- Produces: `/opt/xirc/xirc`, `/opt/xirc/config.yaml`, `/etc/systemd/system/xirc.service`, optional proxy site. Flags: `--print-proxy nginx|apache [--prefix P] [--server-name H] [--port N]`, `--help`.

- [ ] **Step 1: Write the script**

```bash
#!/usr/bin/env bash
# xirc installer — run on the target machine:  sudo scripts/install.sh
# Print a proxy config only (no root, no changes):
#   scripts/install.sh --print-proxy apache --server-name xirc.example.com
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"
source "$SCRIPT_DIR/proxy-templates.sh"
GITHUB_REPO="RealDtx/maxwell-xirc"

ask() { local p="$1" d="$2" v="$3" i; printf '%s [%s]: ' "$p" "$d" >&2; read -r i; printf -v "$v" '%s' "${i:-$d}"; }
ask_yn() {
    local p="$1" d="$2" v="$3" i
    while true; do
        printf '%s [%s]: ' "$p" "$d" >&2; read -r i; i="${i:-$d}"
        case "${i,,}" in y|yes) printf -v "$v" y; return;; n|no) printf -v "$v" n; return;; esac
    done
}
say() { echo "  $*"; }
hr() { echo "  ──────────────────────────────────────────────────────────"; }

# ── --print-proxy ─────────────────────────────────────────────────────────────
if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
    sed -n '2,4p' "$0"; exit 0
fi
if [[ "${1:-}" == "--print-proxy" ]]; then
    kind="${2:-}"; shift 2 || true
    PORT=8085 PREFIX="" SERVER_NAME="xirc.example.com"
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --prefix) PREFIX="$2"; shift 2;;
            --server-name) SERVER_NAME="$2"; shift 2;;
            --port) PORT="$2"; shift 2;;
            *) echo "unknown option $1" >&2; exit 2;;
        esac
    done
    mode=site; [[ -n "$PREFIX" ]] && mode=subpath
    [[ "$kind" == nginx || "$kind" == apache ]] || { echo "usage: $0 --print-proxy nginx|apache [--prefix /xirc] [--server-name host] [--port 8085]" >&2; exit 2; }
    export PORT PREFIX SERVER_NAME
    "render_${kind}_${mode}"
    echo; echo "# Enable with:"; echo "#   $(proxy_enable_hint "$kind" "$mode")"
    [[ "$mode" == subpath ]] && echo "#   and set 'server.prefix: ${PREFIX%/}' in xirc's config.yaml"
    exit 0
fi

[[ $EUID -eq 0 ]] || { echo "Run as root: sudo $0" >&2; exit 1; }

echo; say "xirc installer"; hr; say "Press Enter to accept the default in [brackets]."; echo

# ── paths ─────────────────────────────────────────────────────────────────────
ask "Install directory" "/opt/xirc" INSTALL_DIR
ask "Downloads directory (files arrive here)" "/srv/downloads" DOWNLOADS_DIR
ask "Temp directory (in-progress transfers)" "${DOWNLOADS_DIR}/.tmp" TEMP_DIR
ask "Media library (finished downloads are sorted here)" "/srv/media" MEDIA_DIR
ask "Database (sqlite / mysql)" "sqlite" DB_DRIVER
if [[ "${DB_DRIVER,,}" == mysql* || "${DB_DRIVER,,}" == maria* ]]; then
    DB_DRIVER=mysql; DB_PATH=""
    ask "MySQL DSN (user:pass@tcp(host:3306)/db)" "xirc:change-me@tcp(127.0.0.1:3306)/xirc" DB_DSN
else
    DB_DRIVER=sqlite; DB_DSN=""
    ask "SQLite database file" "${INSTALL_DIR}/data/xirc.db" DB_PATH
fi
ask "Port xirc listens on (localhost only)" "8085" PORT

# ── binary ────────────────────────────────────────────────────────────────────
goarch() { case "$(uname -m)" in x86_64) echo amd64;; aarch64|arm64) echo arm64;; armv7l) echo armv7;; *) return 1;; esac; }
need_go() { sed -n 's/^go \([0-9.]*\).*/\1/p' "$REPO_DIR/go.mod"; }
BIN_SRC=""
if [[ -x "$SCRIPT_DIR/xirc" ]]; then BIN_SRC="$SCRIPT_DIR/xirc"
elif [[ -x "$REPO_DIR/xirc" ]]; then BIN_SRC="$REPO_DIR/xirc"
elif command -v go >/dev/null && [[ -f "$REPO_DIR/go.mod" ]] && \
     [[ "$(printf '%s\n%s\n' "$(need_go)" "$(go env GOVERSION | sed 's/^go//')" | sort -V | head -1)" == "$(need_go)" ]]; then
    say "Building from source with $(go version)…"
    (cd "$REPO_DIR" && CGO_ENABLED=0 go build -ldflags="-s -w" -o "$REPO_DIR/xirc" .)
    BIN_SRC="$REPO_DIR/xirc"
elif A="$(goarch)"; then
    say "Downloading the newest release for linux-${A}…"
    URL="$(curl -fsSL "https://api.github.com/repos/${GITHUB_REPO}/releases" \
        | grep -o "https://[^\"]*/xirc-linux-${A}\"" | head -1 | tr -d '"')" || true
    if [[ -n "$URL" ]]; then
        curl -fsSL -o /tmp/xirc.download "$URL" && chmod +x /tmp/xirc.download && BIN_SRC=/tmp/xirc.download
    fi
fi
if [[ -z "$BIN_SRC" ]]; then
    say "No xirc binary found. Either:"
    say "  - install Go $(need_go)+ and re-run (builds from this checkout), or"
    say "  - build elsewhere: GOOS=linux GOARCH=$(goarch || echo amd64) go build -o xirc . and copy it next to this script."
    exit 1
fi

# ── user + dirs ───────────────────────────────────────────────────────────────
id xirc >/dev/null 2>&1 || { useradd --system --home-dir "$INSTALL_DIR" --shell /usr/sbin/nologin xirc; say "Created system user xirc"; }
install -d -o xirc -g xirc "$INSTALL_DIR" "$INSTALL_DIR/data"
[[ -n "$DB_PATH" ]] && install -d -o xirc -g xirc "$(dirname "$DB_PATH")"
install -m 0755 "$BIN_SRC" "$INSTALL_DIR/xirc"

check_dir() { # $1 dir, $2 label
    local d="$1" label="$2" a
    if [[ ! -d "$d" ]]; then
        ask_yn "$label $d does not exist. Create it (owned by xirc)?" y a
        [[ $a == y ]] && install -d -o xirc -g xirc "$d"
    fi
    [[ -d "$d" ]] || { say "! $label missing — the related feature stays disabled until it exists."; return; }
    if runuser -u xirc -- test -w "$d" -a -r "$d"; then say "✓ $label $d is writable by xirc"; return; fi
    local grp; grp="$(stat -c %G "$d")"
    say "! xirc cannot write to $d (owner $(stat -c '%U:%G %a' "$d"))."
    echo "    1) chown -R xirc:xirc $d"
    echo "    2) add xirc to group '$grp' and make it group-writable (keeps current owner)"
    echo "    3) leave it — xirc will disable the feature and show why"
    ask "Choose" "2" a
    case "$a" in
        1) chown -R xirc:xirc "$d";;
        2) usermod -aG "$grp" xirc && chmod -R g+rwX "$d" && find "$d" -type d -exec chmod g+s {} +;;
        *) say "Left unchanged.";;
    esac
}
check_dir "$DOWNLOADS_DIR" "Downloads dir"
check_dir "$TEMP_DIR" "Temp dir"
check_dir "$MEDIA_DIR" "Media dir"

# ── login ─────────────────────────────────────────────────────────────────────
echo
say "Login: everyone must log in, unless their network is listed here."
ask "Networks that skip login (comma-separated, e.g. 192.168.0.0/16; empty = none)" "" TRUSTED_NETWORKS
TRUSTED_ROLE=admin
[[ -n "$TRUSTED_NETWORKS" ]] && ask "Role for those networks (admin / user)" "admin" TRUSTED_ROLE

# ── proxy choice (before config: subpath sets server.prefix) ──────────────────
HAS_NGINX=n; HAS_APACHE=n
command -v nginx >/dev/null && HAS_NGINX=y
{ command -v apache2ctl >/dev/null || command -v apachectl >/dev/null || command -v httpd >/dev/null; } && HAS_APACHE=y
echo
say "Detected: nginx=$HAS_NGINX apache=$HAS_APACHE"
default_proxy=none; [[ $HAS_APACHE == y ]] && default_proxy=apache; [[ $HAS_NGINX == y ]] && default_proxy=nginx
ask "Reverse proxy to configure (nginx / apache / both / none)" "$default_proxy" PROXY
PROXY="${PROXY,,}"; PREFIX=""; SERVER_NAME=""; MODE=site
if [[ "$PROXY" != none ]]; then
    ask "Own site with a hostname (s) or a subpath of an existing site (p)" "s" m
    if [[ "${m,,}" == p* ]]; then MODE=subpath; ask "URL prefix" "/xirc" PREFIX; PREFIX="${PREFIX%/}"
    else ask "Hostname (e.g. xirc.example.com)" "$(hostname -f 2>/dev/null || hostname)" SERVER_NAME; fi
fi

# ── config + service ──────────────────────────────────────────────────────────
CONFIG="$INSTALL_DIR/config.yaml"
write_config=y
[[ -f "$CONFIG" ]] && ask_yn "$CONFIG exists. Overwrite?" n write_config
if [[ $write_config == y ]]; then
    nets=""; [[ -n "$TRUSTED_NETWORKS" ]] && nets="$(sed 's/ *, */, /g' <<<"$TRUSTED_NETWORKS")"
    {
        echo "server:"; echo "  host: 127.0.0.1"; echo "  port: ${PORT}"
        [[ -n "$PREFIX" ]] && echo "  prefix: ${PREFIX}"
        echo; echo "database:"; echo "  driver: ${DB_DRIVER}"
        if [[ $DB_DRIVER == sqlite ]]; then echo "  path: ${DB_PATH}"; else echo "  dsn: \"${DB_DSN}\""; fi
        cat <<EOF

storage:
  media_dir: ${MEDIA_DIR}
  downloads_dir: ${DOWNLOADS_DIR}
  temp_dir: ${TEMP_DIR}
  min_free_space: 1GB
  critical_free_space: 500MB

auth:
  trusted_networks: [${nets}]
  trusted_role: ${TRUSTED_ROLE}
EOF
    } > "$CONFIG"
    chown xirc:xirc "$CONFIG"; chmod 0640 "$CONFIG"
    say "Wrote $CONFIG"
fi

cat > /etc/systemd/system/xirc.service <<EOF
[Unit]
Description=xirc - XDCC IRC web client
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=xirc
Group=xirc
WorkingDirectory=${INSTALL_DIR}
ExecStart=${INSTALL_DIR}/xirc --config ${CONFIG}
Restart=always
RestartSec=5
RestartPreventExitStatus=78
NoNewPrivileges=yes
UMask=0002

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now xirc
systemctl restart xirc
say "Service xirc started (logs: journalctl -u xirc -f)"

# ── proxy install ─────────────────────────────────────────────────────────────
install_proxy() { # $1 nginx|apache
    local kind="$1" conf a
    conf="$(PORT="$PORT" PREFIX="$PREFIX" SERVER_NAME="$SERVER_NAME" "render_${kind}_${MODE}")"
    ask_yn "Install the ${kind} config automatically?" y a
    if [[ $a == y ]]; then
        local target test reload
        case "$kind:$MODE" in
            nginx:site)     target=/etc/nginx/sites-available/xirc; test="nginx -t"; reload="systemctl reload nginx";;
            nginx:subpath)  target=/etc/nginx/snippets/xirc.conf; test="nginx -t"; reload="systemctl reload nginx";;
            apache:site)    target=/etc/apache2/sites-available/xirc.conf; test="apachectl configtest"; reload="systemctl reload apache2";;
            apache:subpath) target=/etc/apache2/conf-available/xirc.conf; test="apachectl configtest"; reload="systemctl reload apache2";;
        esac
        if [[ -d "$(dirname "$target")" ]]; then
            printf '%s\n' "$conf" > "$target"
            if [[ $kind == apache ]]; then a2enmod -q proxy proxy_http proxy_wstunnel rewrite headers; [[ $MODE == site ]] && a2ensite -q xirc; fi
            [[ "$kind:$MODE" == nginx:site ]] && ln -sf "$target" /etc/nginx/sites-enabled/xirc
            if $test >/tmp/xirc-proxy-test.log 2>&1; then
                $reload; say "✓ ${kind} configured"
                [[ $MODE == subpath ]] && say "  Now include it in your site: $(proxy_enable_hint "$kind" subpath | sed 's/.*add //; s/, then.*//')"
                return
            fi
            say "! ${kind} config test failed — reverting:"; sed 's/^/    /' /tmp/xirc-proxy-test.log
            rm -f "$target" /etc/nginx/sites-enabled/xirc
            [[ "$kind:$MODE" == apache:site ]] && a2dissite -q xirc 2>/dev/null || true
        else
            say "! $(dirname "$target") not found (non-Debian layout?)"
        fi
    fi
    echo; say "Manual setup — save this as xirc.conf:"; hr
    printf '%s\n' "$conf"; hr
    say "Then: $(proxy_enable_hint "$kind" "$MODE")"
}
case "$PROXY" in
    nginx|apache) install_proxy "$PROXY";;
    both) install_proxy nginx; install_proxy apache;;
esac

# ── summary ───────────────────────────────────────────────────────────────────
echo; hr
if [[ "$PROXY" == none ]]; then URL="http://127.0.0.1:${PORT}/ (localhost only — add a reverse proxy for other devices)"
elif [[ $MODE == site ]]; then URL="http://${SERVER_NAME}/"
else URL="http://<your-site>${PREFIX}/"; fi
say "Open: $URL"
say "The first visit asks you to create the admin account."
[[ "$PROXY" != none ]] && say "HTTPS: sudo certbot --${PROXY/both/nginx}   (the login cookie is marked Secure over HTTPS)"
say "Re-run this script any time; it asks before changing existing files."
hr
```

- [ ] **Step 2: Verify**

```bash
chmod +x scripts/install.sh
bash -n scripts/install.sh
scripts/install.sh --print-proxy apache --server-name xirc.example.com | grep -q "ServerName xirc.example.com" && echo site-ok
scripts/install.sh --print-proxy nginx --prefix /xirc | grep -q "server.prefix: /xirc" && echo subpath-ok
command -v shellcheck && shellcheck -S warning scripts/install.sh scripts/proxy-templates.sh scripts/preconfig.sh
```
Expected: `site-ok`, `subpath-ok`, no shellcheck warnings (if installed). Do **not** run the interactive part as root on this dev machine.

Add to `.github/workflows/ci.yml` a step: `run: bash -n scripts/*.sh && bash scripts/test-proxy-templates.sh`.

- [ ] **Step 3: Commit**

```bash
git add scripts/install.sh .github/workflows/ci.yml
git commit -m "feat(deploy): on-host install wizard with permission checks and proxy setup"
```

---

### Task 16: Docs

**Files:**
- Modify: `README.md`, `docs/install.md`

- [ ] **Step 1: README.md** — replace the install section with three short paths:

````markdown
## Install

**On a Linux server (Debian/Ubuntu/Raspberry Pi OS)**
```bash
git clone https://github.com/RealDtx/maxwell-xirc.git && cd maxwell-xirc
sudo scripts/install.sh
```
The wizard asks for download/media/database paths, checks permissions,
installs a systemd service and optionally configures nginx or Apache.
Open the printed URL — the first visit creates the admin account.

**Docker**
```bash
cd deploy && cp .env.example .env   # set DOWNLOADS_DIR, MEDIA_DIR, PUID/PGID
docker compose up -d
```
Reset a password: `docker exec -it xirc xirc --create-admin <name>`.

**From a dev machine to a Pi** — `make preconfig`, then `make deploy`; see
[docs/install.md](docs/install.md).

Only need a reverse-proxy config? `scripts/install.sh --print-proxy apache --server-name xirc.example.com`
````

Update the env-var line: `XIRC_<SECTION>_<KEY>` using the yaml key, e.g. `XIRC_STORAGE_DOWNLOADS_DIR=/downloads`, lists comma-separated.

- [ ] **Step 2: docs/install.md** — add sections (after the existing preconfig flow, which gets `maxwell.local` / `maxwell` defaults from Task 2 and the new Apache/login prompts in its table):
  - **Users & login**: first-visit admin creation; Settings → Users; roles table (copy from spec §2 permission table); `auth.trusted_networks` / `trusted_role` / `trusted_proxies` with example; `--create-admin` for lockouts (`sudo -u xirc /opt/xirc/xirc --config /opt/xirc/config.yaml --create-admin admin`).
  - **Reverse proxy**: nginx and Apache — both variants via `--print-proxy`; required Apache modules; subpath needs `server.prefix`; HTTPS note (certbot; Secure cookie).
  - **Docker**: volumes, PUID/PGID, `.env`, MariaDB profile, trusted networks caveat (Docker NAT hides client IPs unless `network_mode: host` or a proxy on the host).
  - **Permissions**: what gets disabled when a directory isn't writable, where to see it (admin banner), fix commands.

- [ ] **Step 3: Verify** `git grep -n "nginx-maxwell-irc\|192.168.20" -- README.md docs/install.md` → no hits.

- [ ] **Step 4: Commit**

```bash
git add README.md docs/install.md
git commit -m "docs: installer, Docker, Apache/nginx, users and permissions"
```

---

### Task 17: Final verification, author's profile, release tag

**Files:**
- Modify (untracked, not committed): `.maxwell/config.yaml`

- [ ] **Step 1: Full verification**

```bash
make build && make test
bash scripts/test-proxy-templates.sh
git grep -nIiE '192\.168\.20|Europe/Berlin|moviegods|mg-chat' -- ':!docs/superpowers' || echo clean
```
Expected: build ok, all tests `ok`, `proxy templates ok`, `clean`.

- [ ] **Step 2: Keep the author's Pi working without a login prompt**

Append to `.maxwell/config.yaml` (untracked; ask the user before `make deploy`):

```yaml
auth:
  trusted_networks: [192.168.0.0/16]
  trusted_role: admin
```

Tell the user: after deploy, first visit from outside the LAN (or any browser, to create named users) needs the admin account created once.

- [ ] **Step 3: Whole-branch review** — run the final reviewer (per the chosen execution method) before tagging.

- [ ] **Step 4: Tag and push** (the user pre-approved "push the 0.4 tag as soon as all is done"; confirm `git status` is clean and all commits are pushed first)

```bash
git push origin master
git tag -a v0.4.0-beta -m "v0.4.0-beta: multi-user login, installer, Docker, Apache"
git push origin v0.4.0-beta
```

Then report the Actions run URL (`https://github.com/RealDtx/maxwell-xirc/actions`) and that binaries + `ghcr.io/realdtx/xirc:v0.4.0-beta` appear once it finishes. Note: a new GHCR package is private by default — the user may need to set it public in the package settings for anonymous `docker pull`.
