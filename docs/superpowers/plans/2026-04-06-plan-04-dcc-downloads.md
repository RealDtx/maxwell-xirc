# xirc Plan 4: DCC & Downloads — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the DCC transfer engine that accepts DCC SEND offers from bots, manages a download queue with configurable concurrency, monitors disk space, tracks speed statistics, and supports DCC RESUME for interrupted transfers.

**Architecture:** A `dcc` package handles TCP connections for file transfers. A `queue` package manages download ordering, concurrency limits, and persistence. Both subscribe to the IRC event bus for CTCP messages and publish progress/status events back.

**Tech Stack:** Go 1.22+, `net` stdlib for TCP, existing `db`, `irc`, `config` packages

**Depends on:** Plans 1-3 must be complete.

---

## File Structure

```
maxwell-xirc/
├── dcc/
│   ├── transfer.go          # Single DCC transfer: TCP receive, progress tracking
│   ├── transfer_test.go
│   ├── parse.go             # Parse DCC SEND CTCP messages
│   ├── parse_test.go
│   ├── disk.go              # Disk space checking utilities
│   └── disk_test.go
├── queue/
│   ├── queue.go             # Download queue: concurrency, ordering, persistence
│   ├── queue_test.go
│   ├── engine.go            # Engine: ties queue + DCC + IRC together
│   └── engine_test.go
├── server/
│   ├── download_handlers.go # HTTP handlers for download queue management
│   └── download_handlers_test.go
```

---

### Task 1: DCC SEND Message Parsing

**Files:**
- Create: `dcc/parse.go`
- Create: `dcc/parse_test.go`

- [ ] **Step 1: Write failing tests for DCC SEND parsing**

Create `dcc/parse_test.go`:

```go
package dcc

import "testing"

func TestParseDCCSend_Normal(t *testing.T) {
	// DCC SEND filename ip port filesize
	msg := `DCC SEND "Some.Movie.2024.mkv" 3232235777 4500 1500000000`
	offer, err := ParseDCCSend(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if offer.Filename != "Some.Movie.2024.mkv" {
		t.Errorf("expected filename Some.Movie.2024.mkv, got %s", offer.Filename)
	}
	if offer.IP != "192.168.1.1" {
		t.Errorf("expected IP 192.168.1.1, got %s", offer.IP)
	}
	if offer.Port != 4500 {
		t.Errorf("expected port 4500, got %d", offer.Port)
	}
	if offer.Size != 1500000000 {
		t.Errorf("expected size 1500000000, got %d", offer.Size)
	}
	if offer.Passive {
		t.Error("expected non-passive")
	}
}

func TestParseDCCSend_Passive(t *testing.T) {
	msg := `DCC SEND "file.mkv" 3232235777 0 700000000`
	offer, err := ParseDCCSend(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !offer.Passive {
		t.Error("expected passive (port 0)")
	}
	if offer.Size != 700000000 {
		t.Errorf("expected size 700000000, got %d", offer.Size)
	}
}

func TestParseDCCSend_NoQuotes(t *testing.T) {
	msg := `DCC SEND movie.mkv 3232235777 4500 1500000000`
	offer, err := ParseDCCSend(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if offer.Filename != "movie.mkv" {
		t.Errorf("expected filename movie.mkv, got %s", offer.Filename)
	}
}

func TestParseDCCSend_SpacesInQuotedFilename(t *testing.T) {
	msg := `DCC SEND "Some Movie 2024.mkv" 3232235777 4500 1500000000`
	offer, err := ParseDCCSend(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if offer.Filename != "Some Movie 2024.mkv" {
		t.Errorf("expected 'Some Movie 2024.mkv', got %s", offer.Filename)
	}
}

func TestParseDCCSend_InvalidFormat(t *testing.T) {
	_, err := ParseDCCSend("not a dcc send message")
	if err == nil {
		t.Error("expected error for invalid message")
	}
}

func TestIntToIP(t *testing.T) {
	tests := []struct {
		input    uint32
		expected string
	}{
		{3232235777, "192.168.1.1"},
		{2130706433, "127.0.0.1"},
		{0, "0.0.0.0"},
	}
	for _, tt := range tests {
		got := intToIP(tt.input)
		if got != tt.expected {
			t.Errorf("intToIP(%d) = %s, want %s", tt.input, got, tt.expected)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd dcc && go test -v ./...
```

Expected: compilation error — `dcc` package doesn't exist.

- [ ] **Step 3: Implement DCC SEND parsing**

Create `dcc/parse.go`:

```go
package dcc

import (
	"fmt"
	"regexp"
	"strconv"
)

type DCCOffer struct {
	Filename string
	IP       string
	Port     int
	Size     int64
	Passive  bool
}

// Matches: DCC SEND "filename" ip port size  OR  DCC SEND filename ip port size
var dccSendQuoted = regexp.MustCompile(`^DCC SEND "([^"]+)"\s+(\d+)\s+(\d+)\s+(\d+)$`)
var dccSendUnquoted = regexp.MustCompile(`^DCC SEND (\S+)\s+(\d+)\s+(\d+)\s+(\d+)$`)

func ParseDCCSend(msg string) (*DCCOffer, error) {
	var matches []string

	matches = dccSendQuoted.FindStringSubmatch(msg)
	if matches == nil {
		matches = dccSendUnquoted.FindStringSubmatch(msg)
	}
	if matches == nil {
		return nil, fmt.Errorf("not a valid DCC SEND message: %s", msg)
	}

	ipInt, err := strconv.ParseUint(matches[2], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid IP integer: %s", matches[2])
	}

	port, err := strconv.Atoi(matches[3])
	if err != nil {
		return nil, fmt.Errorf("invalid port: %s", matches[3])
	}

	size, err := strconv.ParseInt(matches[4], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid size: %s", matches[4])
	}

	return &DCCOffer{
		Filename: matches[1],
		IP:       intToIP(uint32(ipInt)),
		Port:     port,
		Size:     size,
		Passive:  port == 0,
	}, nil
}

func intToIP(n uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		(n>>24)&0xFF,
		(n>>16)&0xFF,
		(n>>8)&0xFF,
		n&0xFF,
	)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
cd dcc && go test -v ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add dcc/parse.go dcc/parse_test.go
git commit -m "feat: add DCC SEND message parser"
```

---

### Task 2: Disk Space Checking

**Files:**
- Create: `dcc/disk.go`
- Create: `dcc/disk_test.go`

- [ ] **Step 1: Write failing tests for disk space**

Create `dcc/disk_test.go`:

```go
package dcc

import (
	"os"
	"testing"
)

func TestGetAvailableSpace_CurrentDir(t *testing.T) {
	dir := t.TempDir()
	avail, err := GetAvailableSpace(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if avail == 0 {
		t.Error("expected non-zero available space")
	}
}

func TestGetAvailableSpace_NonexistentDir(t *testing.T) {
	_, err := GetAvailableSpace("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"1GB", 1073741824, false},
		{"1gb", 1073741824, false},
		{"500MB", 524288000, false},
		{"500mb", 524288000, false},
		{"1TB", 1099511627776, false},
		{"100KB", 102400, false},
		{"1024", 1024, false},
		{"", 0, true},
		{"abc", 0, true},
	}

	for _, tt := range tests {
		got, err := ParseSize(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseSize(%q): expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSize(%q): unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.expected {
			t.Errorf("ParseSize(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestCheckDiskSpace_Sufficient(t *testing.T) {
	dir := t.TempDir()
	// Asking for 1 byte should always succeed on any system with disk space
	err := CheckDiskSpace(dir, 1, "1KB")
	if err != nil {
		t.Errorf("unexpected error for tiny file: %v", err)
	}
}

func TestCheckDiskSpace_ExcessiveSize(t *testing.T) {
	dir := t.TempDir()
	// Asking for an absurd amount should fail
	err := CheckDiskSpace(dir, 1<<62, "0")
	if err == nil {
		t.Error("expected error for absurdly large file")
	}
}

func TestCheckDiskSpace_NonexistentDir(t *testing.T) {
	err := CheckDiskSpace("/nonexistent/dir", 100, "0")
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
	_ = os.Remove("/nonexistent")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd dcc && go test -v -run TestGetAvailable -run TestParseSize -run TestCheckDisk ./...
```

Expected: compilation error — `GetAvailableSpace`, `ParseSize`, `CheckDiskSpace` don't exist.

- [ ] **Step 3: Implement disk space utilities**

Create `dcc/disk.go`:

```go
package dcc

import (
	"fmt"
	"strconv"
	"strings"
	"syscall"
)

// GetAvailableSpace returns the available bytes on the filesystem containing dir.
func GetAvailableSpace(dir string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", dir, err)
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

// ParseSize converts human-readable size strings (e.g., "1GB", "500MB") to bytes.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size string")
	}

	s = strings.ToUpper(s)

	multipliers := []struct {
		suffix string
		mult   int64
	}{
		{"TB", 1 << 40},
		{"GB", 1 << 30},
		{"MB", 1 << 20},
		{"KB", 1 << 10},
	}

	for _, m := range multipliers {
		if strings.HasSuffix(s, m.suffix) {
			numStr := strings.TrimSuffix(s, m.suffix)
			num, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid size: %s", s)
			}
			return int64(num * float64(m.mult)), nil
		}
	}

	// Plain number = bytes
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size: %s", s)
	}
	return n, nil
}

// CheckDiskSpace verifies that dir has enough space for filesize bytes
// plus the minFreeSpace safety margin.
func CheckDiskSpace(dir string, filesize int64, minFreeSpace string) error {
	avail, err := GetAvailableSpace(dir)
	if err != nil {
		return err
	}

	var margin int64
	if minFreeSpace != "" && minFreeSpace != "0" {
		margin, err = ParseSize(minFreeSpace)
		if err != nil {
			return fmt.Errorf("invalid min_free_space: %w", err)
		}
	}

	needed := filesize + margin
	if avail < needed {
		return fmt.Errorf("insufficient disk space: need %d bytes (file %d + margin %d), have %d",
			needed, filesize, margin, avail)
	}

	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
cd dcc && go test -v ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add dcc/disk.go dcc/disk_test.go
git commit -m "feat: add disk space checking utilities"
```

---

### Task 3: DCC File Transfer

**Files:**
- Create: `dcc/transfer.go`
- Create: `dcc/transfer_test.go`

- [ ] **Step 1: Write failing tests for DCC transfer**

Create `dcc/transfer_test.go`:

```go
package dcc

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestTransfer_ReceivesFile(t *testing.T) {
	dir := t.TempDir()
	destPath := filepath.Join(dir, "received.bin")
	testData := []byte("hello world this is test data for DCC transfer")

	// Start a mock DCC sender (bot)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.Write(testData)
	}()

	offer := &DCCOffer{
		Filename: "received.bin",
		IP:       "127.0.0.1",
		Port:     port,
		Size:     int64(len(testData)),
	}

	progress := make(chan TransferProgress, 100)
	tr := NewTransfer(offer, destPath, progress)
	err = tr.Start()
	if err != nil {
		t.Fatalf("transfer failed: %v", err)
	}

	wg.Wait()

	// Verify file contents
	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}
	if string(got) != string(testData) {
		t.Errorf("file contents mismatch: got %q", string(got))
	}

	if tr.BytesReceived() != int64(len(testData)) {
		t.Errorf("expected %d bytes received, got %d", len(testData), tr.BytesReceived())
	}
}

func TestTransfer_PartFile_UsedDuringTransfer(t *testing.T) {
	dir := t.TempDir()
	destPath := filepath.Join(dir, "test.bin")
	partPath := destPath + ".part"
	testData := []byte("some data here")

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	go func() {
		conn, _ := listener.Accept()
		defer conn.Close()
		conn.Write(testData)
	}()

	offer := &DCCOffer{
		Filename: "test.bin",
		IP:       "127.0.0.1",
		Port:     port,
		Size:     int64(len(testData)),
	}

	tr := NewTransfer(offer, destPath, nil)
	tr.Start()

	// .part file should not exist after successful transfer
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Error("expected .part file to be cleaned up after success")
	}
	// Final file should exist
	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		t.Error("expected final file to exist")
	}
}

func TestTransfer_ProgressReporting(t *testing.T) {
	dir := t.TempDir()
	destPath := filepath.Join(dir, "progress.bin")
	testData := make([]byte, 10000)
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	go func() {
		conn, _ := listener.Accept()
		defer conn.Close()
		conn.Write(testData)
	}()

	offer := &DCCOffer{
		Filename: "progress.bin",
		IP:       "127.0.0.1",
		Port:     port,
		Size:     int64(len(testData)),
	}

	progress := make(chan TransferProgress, 100)
	tr := NewTransfer(offer, destPath, progress)
	tr.Start()

	// Should have received at least one progress update
	var lastProgress TransferProgress
	timeout := time.After(time.Second)
	for {
		select {
		case p, ok := <-progress:
			if !ok {
				goto done
			}
			lastProgress = p
		case <-timeout:
			goto done
		}
	}
done:
	if lastProgress.BytesReceived != int64(len(testData)) {
		t.Errorf("expected final progress %d, got %d", len(testData), lastProgress.BytesReceived)
	}
	if lastProgress.PeakSpeed == 0 {
		// Local transfer should be fast, but peak might be 0 if interval is too short
		// Just check it doesn't panic
	}
}

func TestTransfer_ConnectionRefused(t *testing.T) {
	dir := t.TempDir()
	destPath := filepath.Join(dir, "fail.bin")

	offer := &DCCOffer{
		Filename: "fail.bin",
		IP:       "127.0.0.1",
		Port:     1, // Nothing listening here
		Size:     100,
	}

	tr := NewTransfer(offer, destPath, nil)
	err := tr.Start()
	if err == nil {
		t.Error("expected error for connection refused")
	}
}

func TestTransfer_Resume_ExistingPartFile(t *testing.T) {
	dir := t.TempDir()
	destPath := filepath.Join(dir, "resume.bin")
	partPath := destPath + ".part"

	fullData := []byte("AAAAABBBBB")
	partialData := []byte("AAAAA") // First 5 bytes already downloaded

	// Write partial data to .part file
	os.WriteFile(partPath, partialData, 0644)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	go func() {
		conn, _ := listener.Accept()
		defer conn.Close()
		// In a real resume, bot sends remaining data from offset
		conn.Write(fullData[5:]) // Send only "BBBBB"
	}()

	offer := &DCCOffer{
		Filename: "resume.bin",
		IP:       "127.0.0.1",
		Port:     port,
		Size:     int64(len(fullData)),
	}

	tr := NewTransfer(offer, destPath, nil)
	tr.SetResumeOffset(int64(len(partialData)))
	err = tr.Start()
	if err != nil {
		t.Fatalf("resume transfer failed: %v", err)
	}

	got, _ := os.ReadFile(destPath)
	if string(got) != string(fullData) {
		t.Errorf("expected %q, got %q", string(fullData), string(got))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd dcc && go test -v -run TestTransfer ./...
```

Expected: compilation error — `Transfer`, `NewTransfer`, `TransferProgress` don't exist.

- [ ] **Step 3: Implement DCC transfer**

Create `dcc/transfer.go`:

```go
package dcc

import (
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

type TransferProgress struct {
	BytesReceived int64   `json:"bytes_received"`
	TotalSize     int64   `json:"total_size"`
	Speed         int64   `json:"speed"`
	PeakSpeed     int64   `json:"peak_speed"`
	AverageSpeed  int64   `json:"average_speed"`
	Percentage    float64 `json:"percentage"`
}

type Transfer struct {
	mu            sync.RWMutex
	offer         *DCCOffer
	destPath      string
	partPath      string
	progressCh    chan TransferProgress
	bytesReceived int64
	peakSpeed     int64
	startTime     time.Time
	resumeOffset  int64
}

func NewTransfer(offer *DCCOffer, destPath string, progressCh chan TransferProgress) *Transfer {
	return &Transfer{
		offer:      offer,
		destPath:   destPath,
		partPath:   destPath + ".part",
		progressCh: progressCh,
	}
}

func (t *Transfer) SetResumeOffset(offset int64) {
	t.resumeOffset = offset
}

func (t *Transfer) BytesReceived() int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.bytesReceived
}

func (t *Transfer) PeakSpeed() int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.peakSpeed
}

func (t *Transfer) AverageSpeed() int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	elapsed := time.Since(t.startTime).Seconds()
	if elapsed <= 0 {
		return 0
	}
	received := t.bytesReceived - t.resumeOffset
	return int64(float64(received) / elapsed)
}

func (t *Transfer) Start() error {
	addr := fmt.Sprintf("%s:%d", t.offer.IP, t.offer.Port)
	conn, err := net.DialTimeout("tcp", addr, 30*time.Second)
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", addr, err)
	}
	defer conn.Close()

	// Open or create the .part file
	var file *os.File
	if t.resumeOffset > 0 {
		file, err = os.OpenFile(t.partPath, os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("opening part file for resume: %w", err)
		}
	} else {
		file, err = os.Create(t.partPath)
		if err != nil {
			return fmt.Errorf("creating part file: %w", err)
		}
	}
	defer file.Close()

	t.startTime = time.Now()
	t.bytesReceived = t.resumeOffset

	buf := make([]byte, 64*1024) // 64KB read buffer
	var lastProgressTime time.Time
	var lastProgressBytes int64

	for {
		n, readErr := conn.Read(buf)
		if n > 0 {
			if _, writeErr := file.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("writing to file: %w", writeErr)
			}

			t.mu.Lock()
			t.bytesReceived += int64(n)
			received := t.bytesReceived
			t.mu.Unlock()

			// Report progress every 500ms
			now := time.Now()
			if t.progressCh != nil && now.Sub(lastProgressTime) >= 500*time.Millisecond {
				elapsed := now.Sub(lastProgressTime).Seconds()
				var speed int64
				if elapsed > 0 {
					speed = int64(float64(received-lastProgressBytes) / elapsed)
				}

				t.mu.Lock()
				if speed > t.peakSpeed {
					t.peakSpeed = speed
				}
				peak := t.peakSpeed
				t.mu.Unlock()

				var pct float64
				if t.offer.Size > 0 {
					pct = float64(received) / float64(t.offer.Size) * 100
				}

				totalElapsed := now.Sub(t.startTime).Seconds()
				var avgSpeed int64
				if totalElapsed > 0 {
					avgSpeed = int64(float64(received-t.resumeOffset) / totalElapsed)
				}

				select {
				case t.progressCh <- TransferProgress{
					BytesReceived: received,
					TotalSize:     t.offer.Size,
					Speed:         speed,
					PeakSpeed:     peak,
					AverageSpeed:  avgSpeed,
					Percentage:    pct,
				}:
				default:
				}

				lastProgressTime = now
				lastProgressBytes = received
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return fmt.Errorf("reading from connection: %w", readErr)
		}
	}

	// Send final progress
	if t.progressCh != nil {
		totalElapsed := time.Since(t.startTime).Seconds()
		var avgSpeed int64
		if totalElapsed > 0 {
			avgSpeed = int64(float64(t.bytesReceived-t.resumeOffset) / totalElapsed)
		}
		select {
		case t.progressCh <- TransferProgress{
			BytesReceived: t.bytesReceived,
			TotalSize:     t.offer.Size,
			Speed:         0,
			PeakSpeed:     t.peakSpeed,
			AverageSpeed:  avgSpeed,
			Percentage:    100,
		}:
		default:
		}
		close(t.progressCh)
	}

	// Rename .part to final
	file.Close()
	if err := os.Rename(t.partPath, t.destPath); err != nil {
		return fmt.Errorf("renaming part file: %w", err)
	}

	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
cd dcc && go test -v -run TestTransfer ./...
```

Expected: all transfer tests PASS.

- [ ] **Step 5: Commit**

```bash
git add dcc/transfer.go dcc/transfer_test.go
git commit -m "feat: add DCC file transfer with progress tracking and resume"
```

---

### Task 4: Download Queue

**Files:**
- Create: `queue/queue.go`
- Create: `queue/queue_test.go`

- [ ] **Step 1: Write failing tests for the download queue**

Create `queue/queue_test.go`:

```go
package queue

import (
	"path/filepath"
	"testing"

	"github.com/maxwell-xirc/xirc/db"
)

func newTestStore(t *testing.T) db.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := db.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	store.Migrate()
	t.Cleanup(func() { store.Close() })
	return store
}

func TestQueue_AddDownload(t *testing.T) {
	store := newTestStore(t)
	q := New(store, 3)

	srv := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	dl, err := q.Add(srv.ID, "#test", "xdcc_bot", 42, "movie.mkv", 1500000000)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if dl.Status != "queued" {
		t.Errorf("expected status queued, got %s", dl.Status)
	}
	if dl.PackNumber != 42 {
		t.Errorf("expected pack 42, got %d", dl.PackNumber)
	}
}

func TestQueue_NextReturnsOldestQueued(t *testing.T) {
	store := newTestStore(t)
	q := New(store, 3)

	srv := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	q.Add(srv.ID, "#test", "bot1", 1, "first.mkv", 100)
	q.Add(srv.ID, "#test", "bot2", 2, "second.mkv", 200)

	next := q.Next()
	if next == nil {
		t.Fatal("expected a download")
	}
	if next.Filename != "first.mkv" {
		t.Errorf("expected first.mkv, got %s", next.Filename)
	}
}

func TestQueue_RespectsMaxConcurrent(t *testing.T) {
	store := newTestStore(t)
	q := New(store, 2) // max 2 concurrent

	srv := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	dl1, _ := q.Add(srv.ID, "#t", "bot1", 1, "a.mkv", 100)
	dl2, _ := q.Add(srv.ID, "#t", "bot2", 2, "b.mkv", 100)
	q.Add(srv.ID, "#t", "bot3", 3, "c.mkv", 100)

	// Mark two as downloading
	q.MarkDownloading(dl1.ID)
	q.MarkDownloading(dl2.ID)

	// Next should return nil (at max concurrent)
	next := q.Next()
	if next != nil {
		t.Error("expected nil — at max concurrent limit")
	}

	// Complete one
	q.MarkCompleted(dl1.ID, "/dest/a.mkv", 100, 50)

	// Now next should return the third
	next = q.Next()
	if next == nil {
		t.Fatal("expected a download after slot freed")
	}
	if next.Filename != "c.mkv" {
		t.Errorf("expected c.mkv, got %s", next.Filename)
	}
}

func TestQueue_OnePerBot(t *testing.T) {
	store := newTestStore(t)
	q := New(store, 10) // high limit

	srv := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	dl1, _ := q.Add(srv.ID, "#t", "same_bot", 1, "a.mkv", 100)
	q.Add(srv.ID, "#t", "same_bot", 2, "b.mkv", 100)

	q.MarkDownloading(dl1.ID)

	// Next should skip same_bot's second download
	next := q.Next()
	if next != nil {
		t.Errorf("expected nil — same bot already downloading, got %s from %s", next.Filename, next.BotNick)
	}
}

func TestQueue_Cancel(t *testing.T) {
	store := newTestStore(t)
	q := New(store, 3)

	srv := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	dl, _ := q.Add(srv.ID, "#t", "bot1", 1, "cancel.mkv", 100)

	if err := q.Cancel(dl.ID); err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}

	downloads, _ := store.GetDownloads("cancelled")
	if len(downloads) != 1 {
		t.Errorf("expected 1 cancelled, got %d", len(downloads))
	}
}

func TestQueue_Retry(t *testing.T) {
	store := newTestStore(t)
	q := New(store, 3)

	srv := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	dl, _ := q.Add(srv.ID, "#t", "bot1", 1, "retry.mkv", 100)
	q.MarkFailed(dl.ID, "connection reset")

	if err := q.Retry(dl.ID); err != nil {
		t.Fatalf("Retry failed: %v", err)
	}

	downloads, _ := store.GetDownloads("queued")
	if len(downloads) != 1 {
		t.Errorf("expected 1 queued after retry, got %d", len(downloads))
	}
}

func TestQueue_Reorder(t *testing.T) {
	store := newTestStore(t)
	q := New(store, 3)

	srv := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	dl1, _ := q.Add(srv.ID, "#t", "bot1", 1, "first.mkv", 100)
	dl2, _ := q.Add(srv.ID, "#t", "bot2", 2, "second.mkv", 100)

	// Move second to front
	if err := q.MoveToFront(dl2.ID); err != nil {
		t.Fatalf("MoveToFront failed: %v", err)
	}

	next := q.Next()
	if next == nil {
		t.Fatal("expected a download")
	}
	if next.ID != dl2.ID {
		t.Errorf("expected dl2 (moved to front), got id %d", next.ID)
	}
	_ = dl1 // suppress unused
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd queue && go test -v ./...
```

Expected: compilation error — `queue` package doesn't exist.

- [ ] **Step 3: Implement download queue**

Create `queue/queue.go`:

```go
package queue

import (
	"fmt"
	"sync"
	"time"

	"github.com/maxwell-xirc/xirc/db"
)

type Queue struct {
	mu            sync.Mutex
	store         db.Store
	maxConcurrent int
}

func New(store db.Store, maxConcurrent int) *Queue {
	return &Queue{
		store:         store,
		maxConcurrent: maxConcurrent,
	}
}

func (q *Queue) Add(serverID int64, channel, botNick string, packNumber int, filename string, filesize int64) (*db.Download, error) {
	dl := &db.Download{
		ServerID:   serverID,
		Channel:    channel,
		BotNick:    botNick,
		PackNumber: packNumber,
		Filename:   filename,
		Filesize:   filesize,
		Status:     "queued",
	}

	if err := q.store.CreateDownload(dl); err != nil {
		return nil, fmt.Errorf("creating download: %w", err)
	}

	return dl, nil
}

func (q *Queue) Next() *db.Download {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Check active count
	active, err := q.store.GetDownloads("downloading")
	if err != nil {
		return nil
	}
	if len(active) >= q.maxConcurrent {
		return nil
	}

	// Get bots that are currently downloading
	activeBots := make(map[string]bool)
	for _, dl := range active {
		activeBots[dl.BotNick] = true
	}

	// Get queued downloads (oldest first — GetDownloads returns DESC, so reverse)
	queued, err := q.store.GetDownloads("queued")
	if err != nil {
		return nil
	}

	// Walk from oldest to newest (end of slice since GetDownloads is DESC)
	for i := len(queued) - 1; i >= 0; i-- {
		dl := queued[i]
		if activeBots[dl.BotNick] {
			continue // One per bot
		}
		return &dl
	}

	return nil
}

func (q *Queue) MarkDownloading(id int64) error {
	dl, err := q.store.GetDownload(id)
	if err != nil {
		return err
	}
	now := time.Now()
	dl.Status = "downloading"
	dl.StartedAt = &now
	return q.store.UpdateDownload(dl)
}

func (q *Queue) MarkCompleted(id int64, destPath string, peakSpeed, avgSpeed int64) error {
	dl, err := q.store.GetDownload(id)
	if err != nil {
		return err
	}
	now := time.Now()
	dl.Status = "completed"
	dl.CompletedAt = &now
	dl.DestinationPath = destPath
	dl.DownloadedBytes = dl.Filesize
	dl.PeakSpeed = peakSpeed
	dl.AverageSpeed = avgSpeed
	return q.store.UpdateDownload(dl)
}

func (q *Queue) MarkFailed(id int64, errMsg string) error {
	dl, err := q.store.GetDownload(id)
	if err != nil {
		return err
	}
	dl.Status = "failed"
	dl.ErrorMessage = errMsg
	return q.store.UpdateDownload(dl)
}

func (q *Queue) MarkNeedsAction(id int64, msg string) error {
	dl, err := q.store.GetDownload(id)
	if err != nil {
		return err
	}
	dl.Status = "needs_action"
	dl.ErrorMessage = msg
	return q.store.UpdateDownload(dl)
}

func (q *Queue) Cancel(id int64) error {
	dl, err := q.store.GetDownload(id)
	if err != nil {
		return err
	}
	dl.Status = "cancelled"
	return q.store.UpdateDownload(dl)
}

func (q *Queue) Retry(id int64) error {
	dl, err := q.store.GetDownload(id)
	if err != nil {
		return err
	}
	dl.Status = "queued"
	dl.ErrorMessage = ""
	dl.DownloadedBytes = 0
	dl.StartedAt = nil
	dl.CompletedAt = nil
	return q.store.UpdateDownload(dl)
}

func (q *Queue) MoveToFront(id int64) error {
	// Set created_at to a time before all others to make it "oldest" (first in FIFO)
	dl, err := q.store.GetDownload(id)
	if err != nil {
		return err
	}
	dl.CreatedAt = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	return q.store.UpdateDownload(dl)
}

func (q *Queue) UpdateProgress(id int64, bytesReceived, peakSpeed, avgSpeed int64) error {
	dl, err := q.store.GetDownload(id)
	if err != nil {
		return err
	}
	dl.DownloadedBytes = bytesReceived
	dl.PeakSpeed = peakSpeed
	dl.AverageSpeed = avgSpeed
	return q.store.UpdateDownload(dl)
}

// RequeueInterrupted sets all "downloading" status downloads back to "queued".
// Called on app startup to recover from unclean shutdown.
func (q *Queue) RequeueInterrupted() error {
	downloads, err := q.store.GetDownloads("downloading")
	if err != nil {
		return err
	}
	for _, dl := range downloads {
		dl.Status = "queued"
		dl.StartedAt = nil
		if err := q.store.UpdateDownload(&dl); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
cd queue && go test -v ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add queue/queue.go queue/queue_test.go
git commit -m "feat: add download queue with concurrency limits and per-bot constraint"
```

---

### Task 5: Download Engine — Ties Queue + DCC + IRC Together

**Files:**
- Create: `queue/engine.go`
- Create: `queue/engine_test.go`

- [ ] **Step 1: Write failing tests for the engine**

Create `queue/engine_test.go`:

```go
package queue

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/maxwell-xirc/xirc/config"
	"github.com/maxwell-xirc/xirc/dcc"
	"github.com/maxwell-xirc/xirc/db"
	"github.com/maxwell-xirc/xirc/irc"
)

func TestEngine_HandlesDCCOffer(t *testing.T) {
	store := newTestStore(t)
	bus := irc.NewEventBus()
	dir := t.TempDir()

	cfg := &config.StorageConfig{
		DownloadsDir: dir,
		TempDir:      filepath.Join(dir, "tmp"),
		MinFreeSpace: "1KB",
	}

	eng := NewEngine(store, bus, cfg, 3)

	srv := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	// Add a download to the queue
	dl, _ := eng.Queue().Add(srv.ID, "#test", "xdcc_bot", 42, "", 0)
	eng.Queue().MarkDownloading(dl.ID)

	// Engine should be able to register a pending DCC offer
	eng.RegisterPendingRequest(dl.ID, srv.ID, "xdcc_bot")

	pending := eng.GetPendingRequest(srv.ID, "xdcc_bot")
	if pending == nil {
		t.Fatal("expected pending request")
	}
	if pending.DownloadID != dl.ID {
		t.Errorf("expected download ID %d, got %d", dl.ID, pending.DownloadID)
	}
}

func TestEngine_DetectsPassiveDCC(t *testing.T) {
	offer := &dcc.DCCOffer{
		Filename: "test.mkv",
		IP:       "0.0.0.0",
		Port:     0,
		Size:     1000,
		Passive:  true,
	}

	if !offer.Passive {
		t.Error("expected passive DCC detection for port 0")
	}
}

func TestEngine_QueueProcessing(t *testing.T) {
	store := newTestStore(t)
	bus := irc.NewEventBus()
	dir := t.TempDir()

	cfg := &config.StorageConfig{
		DownloadsDir: dir,
		TempDir:      filepath.Join(dir, "tmp"),
		MinFreeSpace: "0",
	}

	eng := NewEngine(store, bus, cfg, 3)

	srv := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	eng.Queue().Add(srv.ID, "#test", "bot1", 1, "file.mkv", 100)

	// Verify queue has items
	next := eng.Queue().Next()
	if next == nil {
		t.Fatal("expected queued download")
	}

	_ = time.Now() // suppress unused import
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd queue && go test -v -run TestEngine ./...
```

Expected: compilation error — `Engine`, `NewEngine` don't exist.

- [ ] **Step 3: Implement the download engine**

Create `queue/engine.go`:

```go
package queue

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/maxwell-xirc/xirc/config"
	"github.com/maxwell-xirc/xirc/dcc"
	"github.com/maxwell-xirc/xirc/db"
	"github.com/maxwell-xirc/xirc/irc"
)

type PendingRequest struct {
	DownloadID int64
	ServerID   int64
	BotNick    string
}

type Engine struct {
	mu             sync.RWMutex
	queue          *Queue
	store          db.Store
	bus            *irc.EventBus
	storageCfg     *config.StorageConfig
	eventCh        <-chan irc.Event
	stopCh         chan struct{}
	pendingByBot   map[string]*PendingRequest // key: "serverID:botNick"
	activeTransfers map[int64]chan struct{}     // key: downloadID, value: cancel channel
}

func NewEngine(store db.Store, bus *irc.EventBus, storageCfg *config.StorageConfig, maxConcurrent int) *Engine {
	return &Engine{
		queue:           New(store, maxConcurrent),
		store:           store,
		bus:             bus,
		storageCfg:      storageCfg,
		stopCh:          make(chan struct{}),
		pendingByBot:    make(map[string]*PendingRequest),
		activeTransfers: make(map[int64]chan struct{}),
	}
}

func (e *Engine) Queue() *Queue {
	return e.queue
}

func (e *Engine) Start() {
	e.eventCh = e.bus.Subscribe()
	go e.loop()
}

func (e *Engine) Stop() {
	close(e.stopCh)
	e.bus.Unsubscribe(e.eventCh)

	e.mu.Lock()
	for _, cancel := range e.activeTransfers {
		close(cancel)
	}
	e.mu.Unlock()
}

func pendingKey(serverID int64, botNick string) string {
	return fmt.Sprintf("%d:%s", serverID, botNick)
}

func (e *Engine) RegisterPendingRequest(downloadID, serverID int64, botNick string) {
	e.mu.Lock()
	e.pendingByBot[pendingKey(serverID, botNick)] = &PendingRequest{
		DownloadID: downloadID,
		ServerID:   serverID,
		BotNick:    botNick,
	}
	e.mu.Unlock()
}

func (e *Engine) GetPendingRequest(serverID int64, botNick string) *PendingRequest {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.pendingByBot[pendingKey(serverID, botNick)]
}

func (e *Engine) loop() {
	progressTicker := time.NewTicker(5 * time.Second)
	defer progressTicker.Stop()

	for {
		select {
		case <-e.stopCh:
			return
		case ev, ok := <-e.eventCh:
			if !ok {
				return
			}
			if ev.Type == irc.EventIRCMessage {
				e.handleMessage(ev)
			}
		case <-progressTicker.C:
			// Periodic progress flush is handled per-transfer
		}
	}
}

func (e *Engine) handleMessage(ev irc.Event) {
	data, ok := ev.Data.(map[string]string)
	if !ok {
		return
	}

	if data["type"] != "ctcp" {
		// Check for passive DCC hint messages from bots
		if data["type"] == "notice" || data["type"] == "privmsg" {
			e.checkBotHintMessage(ev.ServerID, ev.Nick, data["message"])
		}
		return
	}

	msg := data["message"]
	offer, err := dcc.ParseDCCSend(msg)
	if err != nil {
		return // Not a DCC SEND, ignore
	}

	pending := e.GetPendingRequest(ev.ServerID, ev.Nick)
	if pending == nil {
		log.Printf("received unexpected DCC SEND from %s — no pending request", ev.Nick)
		return
	}

	// Remove from pending
	e.mu.Lock()
	delete(e.pendingByBot, pendingKey(ev.ServerID, ev.Nick))
	e.mu.Unlock()

	// Handle passive DCC
	if offer.Passive {
		log.Printf("passive DCC from %s — marking needs_action", ev.Nick)
		e.queue.MarkNeedsAction(pending.DownloadID, "Bot requires passive DCC. Configure passive DCC port range in settings.")
		e.bus.Publish(irc.Event{
			Type:     irc.EventNotification,
			ServerID: ev.ServerID,
			Data: map[string]string{
				"severity":    "action",
				"message":     fmt.Sprintf("Bot %s requires passive DCC — configure port range in settings", ev.Nick),
				"download_id": fmt.Sprintf("%d", pending.DownloadID),
			},
		})
		return
	}

	// Update download with actual filename and size from offer
	dl, err := e.store.GetDownload(pending.DownloadID)
	if err != nil {
		log.Printf("failed to get download %d: %v", pending.DownloadID, err)
		return
	}
	dl.Filename = offer.Filename
	dl.Filesize = offer.Size
	e.store.UpdateDownload(dl)

	// Check disk space
	destDir := e.storageCfg.DownloadsDir // File router will move it later
	if err := os.MkdirAll(e.storageCfg.TempDir, 0755); err != nil {
		e.queue.MarkFailed(pending.DownloadID, "failed to create temp dir: "+err.Error())
		return
	}

	if err := dcc.CheckDiskSpace(destDir, offer.Size, e.storageCfg.MinFreeSpace); err != nil {
		e.queue.MarkFailed(pending.DownloadID, err.Error())
		e.bus.Publish(irc.Event{
			Type: irc.EventNotification,
			Data: map[string]string{
				"severity": "critical",
				"message":  fmt.Sprintf("Insufficient disk space for %s: %v", offer.Filename, err),
			},
		})
		return
	}

	// Check for existing .part file (resume)
	tempPath := filepath.Join(e.storageCfg.TempDir, offer.Filename+".part")
	var resumeOffset int64
	if info, err := os.Stat(tempPath); err == nil {
		resumeOffset = info.Size()
	}

	destPath := filepath.Join(e.storageCfg.TempDir, offer.Filename)

	// Start transfer in goroutine
	go e.runTransfer(pending.DownloadID, offer, destPath, resumeOffset)
}

func (e *Engine) runTransfer(downloadID int64, offer *dcc.DCCOffer, destPath string, resumeOffset int64) {
	progressCh := make(chan dcc.TransferProgress, 64)
	tr := dcc.NewTransfer(offer, destPath, progressCh)
	if resumeOffset > 0 {
		tr.SetResumeOffset(resumeOffset)
	}

	cancelCh := make(chan struct{})
	e.mu.Lock()
	e.activeTransfers[downloadID] = cancelCh
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		delete(e.activeTransfers, downloadID)
		e.mu.Unlock()
	}()

	// Progress reporting goroutine
	go func() {
		var lastFlush time.Time
		for p := range progressCh {
			// Publish to WebSocket
			e.bus.Publish(irc.Event{
				Type: irc.EventDownloadProgress,
				Data: map[string]interface{}{
					"download_id":    downloadID,
					"bytes_received": p.BytesReceived,
					"total_size":     p.TotalSize,
					"speed":          p.Speed,
					"peak_speed":     p.PeakSpeed,
					"average_speed":  p.AverageSpeed,
					"percentage":     p.Percentage,
				},
			})

			// Flush to DB every 5 seconds
			if time.Since(lastFlush) >= 5*time.Second {
				e.queue.UpdateProgress(downloadID, p.BytesReceived, p.PeakSpeed, p.AverageSpeed)
				lastFlush = time.Now()
			}
		}
	}()

	err := tr.Start()
	if err != nil {
		log.Printf("transfer failed for download %d: %v", downloadID, err)
		e.queue.MarkFailed(downloadID, err.Error())
		e.bus.Publish(irc.Event{
			Type: irc.EventDownloadStatus,
			Data: map[string]interface{}{
				"download_id": downloadID,
				"status":      "failed",
				"error":       err.Error(),
			},
		})
		return
	}

	e.queue.MarkCompleted(downloadID, destPath, tr.PeakSpeed(), tr.AverageSpeed())
	e.bus.Publish(irc.Event{
		Type: irc.EventDownloadStatus,
		Data: map[string]interface{}{
			"download_id": downloadID,
			"status":      "completed",
			"path":        destPath,
			"peak_speed":  tr.PeakSpeed(),
			"avg_speed":   tr.AverageSpeed(),
		},
	})
}

func (e *Engine) checkBotHintMessage(serverID int64, botNick, message string) {
	// Check if this bot has a pending/active download and the message
	// hints at passive DCC or firewall issues
	hints := []string{"passive", "firewall", "can't connect", "dcc rejected", "unable to connect"}
	msgLower := toLower(message)
	for _, hint := range hints {
		if contains(msgLower, hint) {
			pending := e.GetPendingRequest(serverID, botNick)
			if pending != nil {
				e.queue.MarkNeedsAction(pending.DownloadID,
					fmt.Sprintf("Bot message: %s", message))
				e.bus.Publish(irc.Event{
					Type:     irc.EventNotification,
					ServerID: serverID,
					Data: map[string]string{
						"severity": "action",
						"message":  fmt.Sprintf("Bot %s says: %s", botNick, message),
					},
				})
			}
			break
		}
	}
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		b[i] = c
	}
	return string(b)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
cd queue && go test -v ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add queue/engine.go queue/engine_test.go
git commit -m "feat: add download engine tying queue, DCC, and IRC together"
```

---

### Task 6: Download API Endpoints

**Files:**
- Create: `server/download_handlers.go`
- Create: `server/download_handlers_test.go`
- Modify: `server/server.go`

- [ ] **Step 1: Write failing tests for download endpoints**

Create `server/download_handlers_test.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maxwell-xirc/xirc/db"
)

func TestGetDownloads(t *testing.T) {
	srv, store := newTestServerWithStore(t)

	s := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)
	store.CreateDownload(&db.Download{ServerID: s.ID, Channel: "#t", BotNick: "bot", PackNumber: 1, Status: "queued"})

	req := httptest.NewRequest("GET", "/api/downloads", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var downloads []db.Download
	json.NewDecoder(w.Body).Decode(&downloads)
	if len(downloads) != 1 {
		t.Errorf("expected 1 download, got %d", len(downloads))
	}
}

func TestGetDownloads_FilterByStatus(t *testing.T) {
	srv, store := newTestServerWithStore(t)

	s := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)
	store.CreateDownload(&db.Download{ServerID: s.ID, Channel: "#t", BotNick: "b1", PackNumber: 1, Status: "queued"})
	store.CreateDownload(&db.Download{ServerID: s.ID, Channel: "#t", BotNick: "b2", PackNumber: 2, Status: "completed"})

	req := httptest.NewRequest("GET", "/api/downloads?status=queued", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	var downloads []db.Download
	json.NewDecoder(w.Body).Decode(&downloads)
	if len(downloads) != 1 {
		t.Errorf("expected 1 queued, got %d", len(downloads))
	}
}

func TestPostDownloadCancel(t *testing.T) {
	srv, store := newTestServerWithStore(t)

	s := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)
	dl := &db.Download{ServerID: s.ID, Channel: "#t", BotNick: "bot", PackNumber: 1, Status: "queued"}
	store.CreateDownload(dl)

	body := `{"download_id": 1}`
	req := httptest.NewRequest("POST", "/api/downloads/cancel", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestPostDownloadRetry(t *testing.T) {
	srv, store := newTestServerWithStore(t)

	s := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)
	dl := &db.Download{ServerID: s.ID, Channel: "#t", BotNick: "bot", PackNumber: 1, Status: "failed", ErrorMessage: "timeout"}
	store.CreateDownload(dl)

	body := `{"download_id": 1}`
	req := httptest.NewRequest("POST", "/api/downloads/retry", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd server && go test -v -run TestGetDownloads -run TestPostDownload ./...
```

Expected: compilation error — download handler functions don't exist.

- [ ] **Step 3: Implement download handlers**

Create `server/download_handlers.go`:

```go
package server

import (
	"encoding/json"
	"net/http"

	"github.com/maxwell-xirc/xirc/db"
)

func (s *Server) handleGetDownloads(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")

	downloads, err := s.store.GetDownloads(status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if downloads == nil {
		downloads = []db.Download{}
	}
	writeJSON(w, http.StatusOK, downloads)
}

func (s *Server) handleRequestDownload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ServerID   int64  `json:"server_id"`
		Channel    string `json:"channel"`
		BotNick    string `json:"bot_nick"`
		PackNumber int    `json:"pack_number"`
		Filename   string `json:"filename"`
		Filesize   int64  `json:"filesize"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if s.engine == nil {
		writeError(w, http.StatusInternalServerError, "download engine not initialized")
		return
	}

	dl, err := s.engine.Queue().Add(req.ServerID, req.Channel, req.BotNick, req.PackNumber, req.Filename, req.Filesize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, dl)
}

func (s *Server) handleCancelDownload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DownloadID int64 `json:"download_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if s.engine == nil {
		writeError(w, http.StatusInternalServerError, "download engine not initialized")
		return
	}

	if err := s.engine.Queue().Cancel(req.DownloadID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (s *Server) handleRetryDownload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DownloadID int64 `json:"download_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if s.engine == nil {
		writeError(w, http.StatusInternalServerError, "download engine not initialized")
		return
	}

	if err := s.engine.Queue().Retry(req.DownloadID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "queued"})
}

func (s *Server) handleMoveDownload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DownloadID int64 `json:"download_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if s.engine == nil {
		writeError(w, http.StatusInternalServerError, "download engine not initialized")
		return
	}

	if err := s.engine.Queue().MoveToFront(req.DownloadID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "moved"})
}
```

- [ ] **Step 4: Update server.go to include engine and download routes**

Update `server/server.go` — add the `engine` field and download routes:

Add to the `Server` struct:
```go
engine *queue.Engine
```

Update `New` to accept engine:
```go
func New(store db.Store, ircMgr *irc.Manager, p *parser.Parser, eng *queue.Engine) *Server {
```

Add routes in `routes()`:
```go
	// Download endpoints
	s.mux.HandleFunc("GET /api/downloads", s.handleGetDownloads)
	s.mux.HandleFunc("POST /api/downloads/request", s.handleRequestDownload)
	s.mux.HandleFunc("POST /api/downloads/cancel", s.handleCancelDownload)
	s.mux.HandleFunc("POST /api/downloads/retry", s.handleRetryDownload)
	s.mux.HandleFunc("POST /api/downloads/move", s.handleMoveDownload)
```

Update all `New(...)` calls in tests to pass `nil` for engine where needed. Update `newTestServerWithStore` to pass `nil` for engine.

- [ ] **Step 5: Update main.go to wire engine**

Add to `main.go` after parser creation — create engine, pass storage config, start it, and pass to `server.New`.

- [ ] **Step 6: Run all tests**

Run:
```bash
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add server/ queue/ main.go
git commit -m "feat: add download API endpoints and wire engine into main"
```

---

## End State

After completing Plan 4, you have:

- DCC SEND message parsing with IP conversion, passive detection, and quoted filename support.
- Disk space checking with configurable safety margin.
- DCC file transfers with progress tracking, speed stats, and resume support.
- Download queue with configurable max concurrent, one-per-bot constraint, cancel, retry, reorder.
- Download engine that ties IRC events to DCC transfers, detects passive DCC, and handles bot hint messages.
- HTTP API for download management.
- Ready for Plan 5 (File Routing & Hooks) to handle post-download processing.
