package irc

import (
	"testing"
	"time"
)

func TestMessageBuffer_StoresAndRetrieves(t *testing.T) {
	bus := NewEventBus()
	buf := NewMessageBuffer(bus, 10)
	buf.Start()

	bus.Publish(Event{
		Type:     EventIRCMessage,
		ServerID: 1,
		Channel:  "#test",
		Nick:     "bob",
		Data:     map[string]string{"message": "hello", "type": "privmsg"},
	})

	time.Sleep(50 * time.Millisecond)

	msgs := buf.GetMessages(1, "#test", time.Time{}, 10)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Nick != "bob" || msgs[0].Text != "hello" {
		t.Errorf("unexpected message: %+v", msgs[0])
	}
}

func TestMessageBuffer_BoundedByCapacity(t *testing.T) {
	bus := NewEventBus()
	buf := NewMessageBuffer(bus, 3)
	buf.Start()

	for i := 0; i < 5; i++ {
		bus.Publish(Event{
			Type:     EventIRCMessage,
			ServerID: 1,
			Channel:  "#test",
			Nick:     "bot",
			Data:     map[string]string{"message": "msg", "type": "privmsg"},
		})
	}
	time.Sleep(50 * time.Millisecond)

	msgs := buf.GetMessages(1, "#test", time.Time{}, 100)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 (capacity), got %d", len(msgs))
	}
}

func TestMessageBuffer_BeforeFilter(t *testing.T) {
	bus := NewEventBus()
	buf := NewMessageBuffer(bus, 100)
	buf.Start()

	now := time.Now()
	time.Sleep(10 * time.Millisecond)

	bus.Publish(Event{
		Type:     EventIRCMessage,
		ServerID: 1,
		Channel:  "#test",
		Nick:     "bob",
		Data:     map[string]string{"message": "after", "type": "privmsg"},
	})
	time.Sleep(50 * time.Millisecond)

	msgs := buf.GetMessages(1, "#test", now, 10)
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages before cutoff, got %d", len(msgs))
	}
}

func TestMessageBuffer_BeforeFilter_Includes(t *testing.T) {
	bus := NewEventBus()
	buf := NewMessageBuffer(bus, 100)
	buf.Start()

	bus.Publish(Event{
		Type:     EventIRCMessage,
		ServerID: 1,
		Channel:  "#test",
		Nick:     "alice",
		Data:     map[string]string{"message": "before", "type": "privmsg"},
	})
	time.Sleep(20 * time.Millisecond)

	cutoff := time.Now()
	time.Sleep(20 * time.Millisecond)

	bus.Publish(Event{
		Type:     EventIRCMessage,
		ServerID: 1,
		Channel:  "#test",
		Nick:     "bob",
		Data:     map[string]string{"message": "after", "type": "privmsg"},
	})
	time.Sleep(50 * time.Millisecond)

	msgs := buf.GetMessages(1, "#test", cutoff, 10)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message before cutoff, got %d", len(msgs))
	}
	if msgs[0].Nick != "alice" {
		t.Errorf("expected alice, got %s", msgs[0].Nick)
	}
}
