package ws

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/RealDtx/maxwell-irc/irc"
)

func TestHub_BroadcastsEvents(t *testing.T) {
	bus := irc.NewEventBus()
	hub := NewHub(bus)
	hub.Start()
	defer hub.Stop()

	clientCh := make(chan []byte, 10)
	hub.Register(clientCh)
	defer hub.Unregister(clientCh)

	bus.Publish(irc.Event{
		Type:     irc.EventIRCMessage,
		ServerID: 1,
		Channel:  "#test",
		Data:     "hello",
	})

	select {
	case msg := <-clientCh:
		var ev irc.Event
		if err := json.Unmarshal(msg, &ev); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if ev.Type != irc.EventIRCMessage {
			t.Errorf("expected type irc_message, got %s", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast")
	}
}

func TestHub_MultipleClients(t *testing.T) {
	bus := irc.NewEventBus()
	hub := NewHub(bus)
	hub.Start()
	defer hub.Stop()

	ch1 := make(chan []byte, 10)
	ch2 := make(chan []byte, 10)
	hub.Register(ch1)
	hub.Register(ch2)
	defer hub.Unregister(ch1)
	defer hub.Unregister(ch2)

	bus.Publish(irc.Event{Type: irc.EventNotification, Data: "test"})

	for _, ch := range []chan []byte{ch1, ch2} {
		select {
		case <-ch:
			// Good
		case <-time.After(time.Second):
			t.Fatal("timed out")
		}
	}
}

func TestHub_UnregisterStopsReceiving(t *testing.T) {
	bus := irc.NewEventBus()
	hub := NewHub(bus)
	hub.Start()
	defer hub.Stop()

	ch := make(chan []byte, 10)
	hub.Register(ch)
	hub.Unregister(ch)

	bus.Publish(irc.Event{Type: irc.EventNotification, Data: "test"})

	time.Sleep(100 * time.Millisecond)
	select {
	case <-ch:
		t.Error("received after unregister")
	default:
		// Good
	}
}

func TestHub_ClientCount(t *testing.T) {
	bus := irc.NewEventBus()
	hub := NewHub(bus)
	hub.Start()
	defer hub.Stop()

	if hub.ClientCount() != 0 {
		t.Errorf("expected 0 clients initially, got %d", hub.ClientCount())
	}

	ch1 := make(chan []byte, 10)
	ch2 := make(chan []byte, 10)
	hub.Register(ch1)
	if hub.ClientCount() != 1 {
		t.Errorf("expected 1 client after register, got %d", hub.ClientCount())
	}
	hub.Register(ch2)
	if hub.ClientCount() != 2 {
		t.Errorf("expected 2 clients after second register, got %d", hub.ClientCount())
	}
	hub.Unregister(ch1)
	if hub.ClientCount() != 1 {
		t.Errorf("expected 1 client after unregister, got %d", hub.ClientCount())
	}
}
