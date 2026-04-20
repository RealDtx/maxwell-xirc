package debug

import (
	"strings"
	"testing"
)

func TestRedactForLog_StripsControls(t *testing.T) {
	in := "hello\x1b[31mworld\x00\r\n!"
	got := RedactForLog(in)
	if strings.ContainsAny(got, "\x00\r\n\x1b") {
		t.Fatalf("expected control chars removed, got %q", got)
	}
	if !strings.Contains(got, "hello") || !strings.Contains(got, "world") || !strings.Contains(got, "!") {
		t.Fatalf("expected visible content preserved, got %q", got)
	}
}

func TestRedactForLog_Truncates(t *testing.T) {
	in := strings.Repeat("A", maxRedactedLen+50)
	got := RedactForLog(in)
	if len(got) > maxRedactedLen+4 {
		t.Fatalf("expected truncation to ~%d chars, got %d", maxRedactedLen, len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected ellipsis suffix, got %q", got)
	}
}

func TestRedactForLog_Empty(t *testing.T) {
	if RedactForLog("") != "" {
		t.Fatal("empty input should yield empty output")
	}
}

func TestRedactForLog_CollapsesWhitespace(t *testing.T) {
	if got := RedactForLog("  foo   bar  "); got != "foo   bar" {
		t.Fatalf("unexpected: %q", got)
	}
}
