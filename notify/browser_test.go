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
