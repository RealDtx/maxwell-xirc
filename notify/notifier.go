package notify

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
	SeverityAction   Severity = "action"
)

type EventType string

const (
	EventDownloadCompleted  EventType = "download_completed"
	EventDownloadFailed     EventType = "download_failed"
	EventPassiveDCC         EventType = "passive_dcc"
	EventBotHint            EventType = "bot_hint"
	EventDiskLow            EventType = "disk_low"
	EventDiskCritical       EventType = "disk_critical"
	EventHookFailed         EventType = "hook_failed"
	EventServerDisconnected EventType = "server_disconnected"
	EventServerReconnected  EventType = "server_reconnected"
)

type NotificationEvent struct {
	Type     EventType   `json:"type"`
	Severity Severity    `json:"severity"`
	Title    string      `json:"title"`
	Message  string      `json:"message"`
	Data     interface{} `json:"data,omitempty"`
}

// Notifier is the interface for notification backends.
// Implement this to add webhook, Telegram, Home Assistant, etc.
type Notifier interface {
	Name() string
	Send(event NotificationEvent) error
	SupportedEvents() []EventType
}
