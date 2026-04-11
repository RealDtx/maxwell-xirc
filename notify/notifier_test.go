package notify

import "testing"

func TestEventSeverities(t *testing.T) {
	if SeverityInfo != "info" {
		t.Error("unexpected info value")
	}
	if SeverityWarning != "warning" {
		t.Error("unexpected warning value")
	}
	if SeverityCritical != "critical" {
		t.Error("unexpected critical value")
	}
	if SeverityAction != "action" {
		t.Error("unexpected action value")
	}
}

func TestNotificationEvent_Fields(t *testing.T) {
	ev := NotificationEvent{
		Type:     EventDownloadCompleted,
		Severity: SeverityInfo,
		Title:    "Download complete",
		Message:  "movie.mkv completed",
	}
	if ev.Type != EventDownloadCompleted {
		t.Error("unexpected type")
	}
}
