package irc

import (
	"fmt"
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
	mu     sync.RWMutex
	bus    *EventBus
	cap    int
	bufs   map[string][]BufferedMessage
	stopCh chan struct{}
	once   sync.Once
}

// NewMessageBuffer creates a MessageBuffer with the given per-channel capacity.
func NewMessageBuffer(bus *EventBus, capacity int) *MessageBuffer {
	return &MessageBuffer{
		bus:    bus,
		cap:    capacity,
		bufs:   make(map[string][]BufferedMessage),
		stopCh: make(chan struct{}),
	}
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
	mb.once.Do(func() { close(mb.stopCh) })
}

func (mb *MessageBuffer) key(serverID int64, channel string) string {
	return fmt.Sprintf("%d:%s", serverID, channel)
}

func (mb *MessageBuffer) append(serverID int64, channel string, msg BufferedMessage) {
	k := mb.key(serverID, channel)
	mb.mu.Lock()
	defer mb.mu.Unlock()
	buf := mb.bufs[k]
	buf = append(buf, msg)
	if len(buf) > mb.cap {
		buf = buf[len(buf)-mb.cap:]
	}
	mb.bufs[k] = buf
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
