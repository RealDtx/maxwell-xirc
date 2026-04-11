package irc

import (
	"sync"
)

// ErrorBuffer is an in-memory ring buffer for ErrorEvent values.
// It subscribes to the EventBus and stores EventError events.
type ErrorBuffer struct {
	mu     sync.RWMutex
	once   sync.Once
	bus    *EventBus
	cap    int
	items  []ErrorEvent
	stopCh chan struct{}
}

func NewErrorBuffer(bus *EventBus, capacity int) *ErrorBuffer {
	return &ErrorBuffer{
		bus:    bus,
		cap:    capacity,
		stopCh: make(chan struct{}),
	}
}

func (eb *ErrorBuffer) Start() {
	ch := eb.bus.Subscribe()
	go func() {
		defer eb.bus.Unsubscribe(ch)
		for {
			select {
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if ev.Type != EventError {
					continue
				}
				errEv, ok := ev.Data.(ErrorEvent)
				if !ok {
					continue
				}
				eb.mu.Lock()
				eb.items = append(eb.items, errEv)
				if len(eb.items) > eb.cap {
					eb.items = eb.items[len(eb.items)-eb.cap:]
				}
				eb.mu.Unlock()
			case <-eb.stopCh:
				return
			}
		}
	}()
}

func (eb *ErrorBuffer) Stop() {
	eb.once.Do(func() { close(eb.stopCh) })
}

// GetErrors returns up to limit recent errors (most recent last).
func (eb *ErrorBuffer) GetErrors(limit int) []ErrorEvent {
	eb.mu.RLock()
	src := eb.items
	snap := make([]ErrorEvent, len(src))
	copy(snap, src)
	eb.mu.RUnlock()

	if len(snap) == 0 {
		return nil
	}
	if limit > 0 && len(snap) > limit {
		snap = snap[len(snap)-limit:]
	}
	return snap
}
