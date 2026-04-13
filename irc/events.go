package irc

import (
	"sync"
)

type EventType string

const (
	EventIRCMessage       EventType = "irc_message"
	EventConnectionStatus EventType = "connection_status"
	EventSearchResult     EventType = "search_result"
	EventDownloadProgress EventType = "download_progress"
	EventDownloadStatus   EventType = "download_status"
	EventNotification     EventType = "notification"
	EventError            EventType = "error_event"
)

// ErrorEvent is the payload for EventError bus events.
type ErrorEvent struct {
	ErrorType string `json:"error_type"` // "irc_disconnect", "download_failed", "hook_failed"
	ServerID  int64  `json:"server_id,omitempty"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"` // RFC3339
}

type Event struct {
	Type      EventType   `json:"type"`
	ServerID  int64       `json:"server_id"`
	Channel   string      `json:"channel,omitempty"`
	Nick      string      `json:"nick,omitempty"`
	Timestamp string      `json:"timestamp"` // RFC3339Nano
	Data      interface{} `json:"data"`
}

type EventBus struct {
	mu          sync.RWMutex
	subscribers map[chan Event]struct{}
}

func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[chan Event]struct{}),
	}
}

func (b *EventBus) Subscribe() <-chan Event {
	ch := make(chan Event, 128)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *EventBus) Unsubscribe(rch <-chan Event) {
	// We need a bidirectional channel to delete from the map and close.
	// We iterate to find the matching channel.
	b.mu.Lock()
	for ch := range b.subscribers {
		if (<-chan Event)(ch) == rch {
			delete(b.subscribers, ch)
			b.mu.Unlock()
			close(ch)
			return
		}
	}
	b.mu.Unlock()
}

func (b *EventBus) Publish(ev Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for ch := range b.subscribers {
		select {
		case ch <- ev:
		default:
			// Slow subscriber, drop event rather than block
		}
	}
}
