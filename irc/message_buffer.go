package irc

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// BufferedMessage is one IRC message stored in the ring buffer.
type BufferedMessage struct {
	Timestamp time.Time `json:"timestamp"`
	ServerID  int64     `json:"server_id"`
	Channel   string    `json:"channel"`
	Nick      string    `json:"nick"`
	Text      string    `json:"text"`
	MsgType   string    `json:"msg_type"` // "privmsg" or "notice"
}

// MessageBuffer is an in-memory ring buffer per (server_id, channel).
// It subscribes to the EventBus and buffers EventIRCMessage events.
type MessageBuffer struct {
	mu       sync.RWMutex
	bus      *EventBus
	cap      int
	bufs     map[string][]BufferedMessage
	stopCh   chan struct{}
	once     sync.Once
	logDir   string
	logFiles map[string]*os.File
	logMu    sync.Mutex
}

// NewMessageBuffer creates a MessageBuffer with the given per-channel capacity.
func NewMessageBuffer(bus *EventBus, capacity int) *MessageBuffer {
	return &MessageBuffer{
		bus:      bus,
		cap:      capacity,
		bufs:     make(map[string][]BufferedMessage),
		stopCh:   make(chan struct{}),
		logFiles: make(map[string]*os.File),
	}
}

func (mb *MessageBuffer) SetLogDir(dir string) {
	mb.logMu.Lock()
	defer mb.logMu.Unlock()
	mb.logDir = dir
}

// Start begins consuming events from the bus in a background goroutine.
func (mb *MessageBuffer) Start() {
	ch := mb.bus.Subscribe()
	go func() {
		defer mb.bus.Unsubscribe(ch)
		for {
			select {
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if ev.Type != EventIRCMessage {
					continue
				}
				data, ok := ev.Data.(map[string]string)
				if !ok {
					continue
				}
				msg := BufferedMessage{
					Timestamp: time.Now(),
					ServerID:  ev.ServerID,
					Channel:   ev.Channel,
					Nick:      ev.Nick,
					Text:      data["message"],
					MsgType:   data["type"],
				}
				mb.append(ev.ServerID, ev.Channel, msg)
			case <-mb.stopCh:
				return
			}
		}
	}()
}

// Stop terminates the background goroutine.
func (mb *MessageBuffer) Stop() {
	mb.once.Do(func() {
		close(mb.stopCh)
		mb.logMu.Lock()
		defer mb.logMu.Unlock()
		for key, file := range mb.logFiles {
			if err := file.Close(); err != nil {
				log.Printf("message buffer: close log file %s failed: %v", key, err)
			}
		}
		mb.logFiles = make(map[string]*os.File)
	})
}

func (mb *MessageBuffer) key(serverID int64, channel string) string {
	return fmt.Sprintf("%d:%s", serverID, channel)
}

func (mb *MessageBuffer) append(serverID int64, channel string, msg BufferedMessage) {
	k := mb.key(serverID, channel)
	mb.mu.Lock()
	buf := mb.bufs[k]
	buf = append(buf, msg)
	if len(buf) > mb.cap {
		buf = buf[len(buf)-mb.cap:]
	}
	mb.bufs[k] = buf
	mb.mu.Unlock()
	mb.writeLogLine(serverID, channel, msg)
}

func (mb *MessageBuffer) writeLogLine(serverID int64, channel string, msg BufferedMessage) {
	mb.logMu.Lock()
	defer mb.logMu.Unlock()
	if mb.logDir == "" {
		return
	}
	channelSafe := strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(channel)
	dir := filepath.Join(mb.logDir, fmt.Sprintf("%d", serverID))
	path := filepath.Join(dir, channelSafe+".log")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("message buffer: create log dir %s failed: %v", dir, err)
		return
	}
	f := mb.logFiles[path]
	if f == nil {
		var err error
		f, err = os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			log.Printf("message buffer: open log file %s failed: %v", path, err)
			return
		}
		mb.logFiles[path] = f
	}
	line := fmt.Sprintf("[%s] <%s> %s\n", msg.Timestamp.Format(time.RFC3339), msg.Nick, msg.Text)
	if _, err := f.WriteString(line); err != nil {
		log.Printf("message buffer: write log file %s failed: %v", path, err)
	}
}

// GetMessages returns up to limit messages with timestamp strictly before
// the before parameter. Pass zero time to get the most recent messages.
func (mb *MessageBuffer) GetMessages(serverID int64, channel string, before time.Time, limit int) []BufferedMessage {
	k := mb.key(serverID, channel)
	mb.mu.RLock()
	src := mb.bufs[k]
	snap := make([]BufferedMessage, len(src))
	copy(snap, src)
	mb.mu.RUnlock()

	if len(snap) == 0 {
		return nil
	}

	var filtered []BufferedMessage
	for _, m := range snap {
		if before.IsZero() || m.Timestamp.Before(before) {
			filtered = append(filtered, m)
		}
	}

	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	return filtered
}
