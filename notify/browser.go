package notify

import "github.com/RealDtx/maxwell-irc/irc"

type BrowserNotifier struct {
	bus *irc.EventBus
}

func NewBrowserNotifier(bus *irc.EventBus) *BrowserNotifier {
	return &BrowserNotifier{bus: bus}
}

func (b *BrowserNotifier) Name() string {
	return "browser"
}

func (b *BrowserNotifier) Send(event NotificationEvent) error {
	if b.bus == nil {
		return nil
	}
	b.bus.Publish(irc.Event{
		Type: irc.EventNotification,
		Data: event,
	})
	return nil
}

func (b *BrowserNotifier) SupportedEvents() []EventType {
	return []EventType{
		EventDownloadCompleted,
		EventDownloadFailed,
		EventPassiveDCC,
		EventBotHint,
		EventDiskLow,
		EventDiskCritical,
		EventHookFailed,
		EventServerDisconnected,
		EventServerReconnected,
	}
}
