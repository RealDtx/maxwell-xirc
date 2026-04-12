package irc

import (
	"testing"
	"time"
)

func TestErrorBuffer_StoresEvent(t *testing.T) {
	bus := NewEventBus()
	buf := NewErrorBuffer(bus, 10)
	buf.Start()

	bus.Publish(Event{
		Type:     EventError,
		ServerID: 1,
		Data: ErrorEvent{
			ErrorType: "irc_disconnect",
			ServerID:  1,
			Message:   "connection reset",
			Timestamp: time.Now().Format(time.RFC3339),
		},
	})
	time.Sleep(50 * time.Millisecond)

	errs := buf.GetErrors(50)
	if len(errs) != 1 {
		t.Fatalf("expected 1, got %d", len(errs))
	}
	if errs[0].Message != "connection reset" {
		t.Errorf("unexpected message: %s", errs[0].Message)
	}
}

func TestErrorBuffer_BoundedByCapacity(t *testing.T) {
	bus := NewEventBus()
	buf := NewErrorBuffer(bus, 3)
	buf.Start()

	for i := 0; i < 5; i++ {
		bus.Publish(Event{
			Type: EventError,
			Data: ErrorEvent{ErrorType: "test", Message: "err", Timestamp: time.Now().Format(time.RFC3339)},
		})
	}
	time.Sleep(50 * time.Millisecond)

	if len(buf.GetErrors(100)) != 3 {
		t.Fatalf("expected 3 (capacity)")
	}
}
