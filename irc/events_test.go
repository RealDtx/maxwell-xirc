package irc

import (
	"sync"
	"testing"
	"time"
)

func TestEventBus_Subscribe_ReceivesEvents(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	go bus.Publish(Event{Type: EventIRCMessage, ServerID: 1, Channel: "#test", Data: "hello"})

	select {
	case ev := <-ch:
		if ev.Type != EventIRCMessage {
			t.Errorf("expected type %s, got %s", EventIRCMessage, ev.Type)
		}
		if ev.Channel != "#test" {
			t.Errorf("expected channel #test, got %s", ev.Channel)
		}
		if ev.Data != "hello" {
			t.Errorf("expected data hello, got %v", ev.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestEventBus_MultipleSubscribers(t *testing.T) {
	bus := NewEventBus()
	ch1 := bus.Subscribe()
	ch2 := bus.Subscribe()
	defer bus.Unsubscribe(ch1)
	defer bus.Unsubscribe(ch2)

	go bus.Publish(Event{Type: EventConnectionStatus, ServerID: 1, Data: "connected"})

	for _, ch := range []<-chan Event{ch1, ch2} {
		select {
		case ev := <-ch:
			if ev.Type != EventConnectionStatus {
				t.Errorf("expected type %s, got %s", EventConnectionStatus, ev.Type)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for event")
		}
	}
}

func TestEventBus_Unsubscribe_StopsReceiving(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe()
	bus.Unsubscribe(ch)

	bus.Publish(Event{Type: EventIRCMessage, Data: "should not arrive"})

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("received event after unsubscribe")
		}
	case <-time.After(100 * time.Millisecond):
		// Channel was closed, good
	}
}

func TestEventBus_SlowSubscriber_DoesNotBlock(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	// Don't read from ch — should not block the publisher
	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			bus.Publish(Event{Type: EventIRCMessage, Data: i})
		}
		close(done)
	}()

	select {
	case <-done:
		// Publisher didn't block, good
	case <-time.After(2 * time.Second):
		t.Fatal("publisher blocked on slow subscriber")
	}
}

func TestEventBus_ConcurrentPublish(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			bus.Publish(Event{Type: EventIRCMessage, Data: n})
		}(i)
	}

	wg.Wait()
	// If we get here without panic/deadlock, concurrent publish is safe
}
