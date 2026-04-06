# xirc Plan 8: Notifications & Deployment — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the notification system (browser push now, extensible for webhooks later), and create all deployment artifacts — Dockerfile, docker-compose, systemd service, nginx config.

**Architecture:** A `notify` package defines a `Notifier` interface. The browser push backend is implemented via the existing WebSocket (notification events are just another event type on the bus). Deployment files are templates ready to use.

**Tech Stack:** Go 1.22+, Docker, systemd, nginx

**Depends on:** Plans 1-7 must be complete.

---

## File Structure

```
maxwell-xirc/
├── notify/
│   ├── notifier.go          # Notifier interface + event types
│   ├── notifier_test.go
│   ├── browser.go           # Browser notification backend (via event bus)
│   └── browser_test.go
├── deploy/
│   ├── Dockerfile
│   ├── docker-compose.yaml
│   ├── xirc.service          # systemd unit
│   └── nginx-xirc.conf       # nginx site config
├── Makefile                   # Build targets for all platforms
```

---

### Task 1: Notification Interface and Browser Backend

**Files:**
- Create: `notify/notifier.go`
- Create: `notify/notifier_test.go`
- Create: `notify/browser.go`
- Create: `notify/browser_test.go`

- [ ] **Step 1: Write failing tests**

Create `notify/notifier_test.go`:

```go
package notify

import "testing"

func TestEventSeverities(t *testing.T) {
	if SeverityInfo != "info" {
		t.Error("unexpected info value")
	}
	if SeverityWarning != "warning" {
		t.Error("unexpected warning value")
	}
	if SeverityCritical != "critical" {
		t.Error("unexpected critical value")
	}
	if SeverityAction != "action" {
		t.Error("unexpected action value")
	}
}

func TestNotificationEvent_Fields(t *testing.T) {
	ev := NotificationEvent{
		Type:     EventDownloadCompleted,
		Severity: SeverityInfo,
		Title:    "Download complete",
		Message:  "movie.mkv completed",
	}
	if ev.Type != EventDownloadCompleted {
		t.Error("unexpected type")
	}
}
```

Create `notify/browser_test.go`:

```go
package notify

import (
	"testing"
	"time"

	"github.com/maxwell-xirc/xirc/irc"
)

func TestBrowserNotifier_PublishesEvent(t *testing.T) {
	bus := irc.NewEventBus()
	bn := NewBrowserNotifier(bus)

	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	ev := NotificationEvent{
		Type:     EventDownloadCompleted,
		Severity: SeverityInfo,
		Title:    "Done",
		Message:  "movie.mkv done",
	}

	if err := bn.Send(ev); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	select {
	case got := <-ch:
		if got.Type != irc.EventNotification {
			t.Errorf("expected notification event type, got %s", got.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for notification event")
	}
}

func TestBrowserNotifier_Name(t *testing.T) {
	bn := NewBrowserNotifier(nil)
	if bn.Name() != "browser" {
		t.Errorf("expected name 'browser', got %s", bn.Name())
	}
}

func TestBrowserNotifier_SupportedEvents(t *testing.T) {
	bn := NewBrowserNotifier(nil)
	events := bn.SupportedEvents()
	if len(events) == 0 {
		t.Error("expected supported events")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd notify && go test -v ./...`

- [ ] **Step 3: Implement notification types**

Create `notify/notifier.go`:

```go
package notify

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
	SeverityAction   Severity = "action"
)

type EventType string

const (
	EventDownloadCompleted EventType = "download_completed"
	EventDownloadFailed    EventType = "download_failed"
	EventPassiveDCC        EventType = "passive_dcc"
	EventBotHint           EventType = "bot_hint"
	EventDiskLow           EventType = "disk_low"
	EventDiskCritical      EventType = "disk_critical"
	EventHookFailed        EventType = "hook_failed"
	EventServerDisconnected EventType = "server_disconnected"
	EventServerReconnected EventType = "server_reconnected"
)

type NotificationEvent struct {
	Type     EventType   `json:"type"`
	Severity Severity    `json:"severity"`
	Title    string      `json:"title"`
	Message  string      `json:"message"`
	Data     interface{} `json:"data,omitempty"`
}

// Notifier is the interface for notification backends.
// Implement this to add webhook, Telegram, Home Assistant, etc.
type Notifier interface {
	Name() string
	Send(event NotificationEvent) error
	SupportedEvents() []EventType
}
```

- [ ] **Step 4: Implement browser notifier**

Create `notify/browser.go`:

```go
package notify

import "github.com/maxwell-xirc/xirc/irc"

type BrowserNotifier struct {
	bus *irc.EventBus
}

func NewBrowserNotifier(bus *irc.EventBus) *BrowserNotifier {
	return &BrowserNotifier{bus: bus}
}

func (b *BrowserNotifier) Name() string {
	return "browser"
}

func (b *BrowserNotifier) Send(event NotificationEvent) error {
	if b.bus == nil {
		return nil
	}
	b.bus.Publish(irc.Event{
		Type: irc.EventNotification,
		Data: event,
	})
	return nil
}

func (b *BrowserNotifier) SupportedEvents() []EventType {
	return []EventType{
		EventDownloadCompleted,
		EventDownloadFailed,
		EventPassiveDCC,
		EventBotHint,
		EventDiskLow,
		EventDiskCritical,
		EventHookFailed,
		EventServerDisconnected,
		EventServerReconnected,
	}
}
```

- [ ] **Step 5: Run tests**

Run: `cd notify && go test -v ./...`

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add notify/
git commit -m "feat: add notification system with browser backend"
```

---

### Task 2: Deployment — Dockerfile

**Files:**
- Create: `deploy/Dockerfile`

- [ ] **Step 1: Create Dockerfile**

Create `deploy/Dockerfile`:

```dockerfile
FROM golang:1.22-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o xirc .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
RUN adduser -D -h /app xirc
USER xirc
WORKDIR /app

COPY --from=builder /build/xirc .
COPY --from=builder /build/config.yaml ./data/config.yaml

EXPOSE 8085
VOLUME ["/app/data", "/srv/dlna/media", "/srv/downloads"]

ENTRYPOINT ["./xirc", "--config", "/app/data/config.yaml"]
```

- [ ] **Step 2: Commit**

```bash
git add deploy/Dockerfile
git commit -m "feat: add Dockerfile for container deployment"
```

---

### Task 3: Deployment — Docker Compose

**Files:**
- Create: `deploy/docker-compose.yaml`

- [ ] **Step 1: Create docker-compose.yaml**

Create `deploy/docker-compose.yaml`:

```yaml
services:
  xirc:
    build:
      context: ..
      dockerfile: deploy/Dockerfile
    container_name: xirc
    restart: unless-stopped
    ports:
      - "127.0.0.1:8085:8085"
    volumes:
      - ../data:/app/data
      - /srv/dlna/media:/srv/dlna/media
      - /srv/downloads:/srv/downloads
    environment:
      - TZ=Europe/Berlin
    networks:
      - xirc

  db:
    image: mariadb:11
    container_name: xirc-db
    restart: unless-stopped
    profiles:
      - mariadb
    environment:
      - MYSQL_ROOT_PASSWORD=changeme
      - MYSQL_DATABASE=xirc
      - MYSQL_USER=xirc
      - MYSQL_PASSWORD=changeme
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

- [ ] **Step 2: Commit**

```bash
git add deploy/docker-compose.yaml
git commit -m "feat: add docker-compose with SQLite default and MariaDB profile"
```

---

### Task 4: Deployment — Systemd Service

**Files:**
- Create: `deploy/xirc.service`

- [ ] **Step 1: Create systemd unit**

Create `deploy/xirc.service`:

```ini
[Unit]
Description=xirc - XDCC IRC Web Client
After=network.target
Wants=network-online.target

[Service]
Type=simple
User=xirc
Group=xirc
WorkingDirectory=/opt/xirc
ExecStart=/opt/xirc/xirc --config /opt/xirc/config.yaml
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

# Hardening
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=/opt/xirc/data /srv/dlna/media /srv/downloads
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
```

- [ ] **Step 2: Commit**

```bash
git add deploy/xirc.service
git commit -m "feat: add systemd service unit"
```

---

### Task 5: Deployment — Nginx Config

**Files:**
- Create: `deploy/nginx-xirc.conf`

- [ ] **Step 1: Create nginx site config**

Create `deploy/nginx-xirc.conf`:

```nginx
server {
    listen 80;
    server_name xirc.local;

    # Restrict to internal networks
    allow 192.168.0.0/16;
    allow 10.0.0.0/8;
    allow 172.16.0.0/12;
    deny all;

    # Proxy to xirc
    location / {
        proxy_pass http://127.0.0.1:8085;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # WebSocket
    location /ws {
        proxy_pass http://127.0.0.1:8085;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_read_timeout 86400;
    }
}
```

- [ ] **Step 2: Commit**

```bash
git add deploy/nginx-xirc.conf
git commit -m "feat: add nginx reverse proxy config"
```

---

### Task 6: Makefile

**Files:**
- Create: `Makefile`

- [ ] **Step 1: Create Makefile**

Create `Makefile`:

```makefile
.PHONY: build test clean docker run

BINARY=xirc
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

build:
	go build -ldflags="-s -w -X main.version=$(VERSION)" -o $(BINARY) .

test:
	go test ./...

test-verbose:
	go test -v ./...

# Cross-compile for Raspberry Pi 4 (ARM64)
build-pi:
	GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o $(BINARY)-arm64 .

# Cross-compile for Raspberry Pi 3 (ARM)
build-pi3:
	GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="-s -w" -o $(BINARY)-arm .

docker:
	docker build -f deploy/Dockerfile -t xirc .

docker-up:
	cd deploy && docker compose up -d

docker-up-mariadb:
	cd deploy && docker compose --profile mariadb up -d

docker-down:
	cd deploy && docker compose down

run: build
	./$(BINARY) --config config.yaml

clean:
	rm -f $(BINARY) $(BINARY)-arm64 $(BINARY)-arm
```

- [ ] **Step 2: Verify build works**

Run:
```bash
make test
make build
```

Expected: tests pass, binary built.

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "feat: add Makefile with build, test, and deployment targets"
```

---

### Task 7: Wire Notification System into Main

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Add notification setup to main.go**

After creating the event bus, create the browser notifier and pass it where needed. The notifier is already implicitly working — notification events published to the bus are forwarded via WebSocket to browser clients. This task just makes it explicit and sets up for future backends.

Add to main.go:
```go
import "github.com/maxwell-xirc/xirc/notify"

// After bus creation:
browserNotifier := notify.NewBrowserNotifier(bus)
_ = browserNotifier // Used by engine for explicit notifications
```

Wire the notifier into the engine so it can send typed notifications instead of raw event bus publishes.

- [ ] **Step 2: Run all tests one final time**

Run:
```bash
make test
```

Expected: all tests PASS across all packages.

- [ ] **Step 3: Commit**

```bash
git add main.go
git commit -m "feat: wire notification system and finalize deployment"
```

---

## End State

After Plan 8, xirc is production-ready:
- Notification system with browser push, extensible for webhooks/Telegram/HA
- Docker deployment (SQLite or MariaDB via profiles)
- Systemd deployment for bare-metal
- Nginx reverse proxy config (internal-only)
- Makefile for building, testing, and cross-compiling for Raspberry Pi
- All 8 plans complete — the full XDCC web client is built.
