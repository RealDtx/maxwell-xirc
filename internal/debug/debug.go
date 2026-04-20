package debug

import (
	"log"
	"os"
	"strings"
	"unicode"
)

// Enabled controls whether debug logging is active.
// Set via --debug flag or XIRC_DEBUG=1 environment variable.
var Enabled bool

func init() {
	if os.Getenv("XIRC_DEBUG") == "1" {
		Enabled = true
	}
}

// Debugf logs a formatted message when debug mode is enabled.
func Debugf(format string, args ...interface{}) {
	if Enabled {
		log.Printf("[DEBUG] "+format, args...)
	}
}

const maxRedactedLen = 160

// RedactForLog sanitises an arbitrary string for inclusion in a debug log.
// It strips control characters (which could contain ANSI/CSI sequences or
// break log parsers), collapses inner whitespace, and truncates to a fixed
// upper bound so a hostile remote peer cannot flood the log with one line.
func RedactForLog(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	lastSpace := false
	for _, r := range s {
		if unicode.IsControl(r) || r == '\u007f' {
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
			continue
		}
		b.WriteRune(r)
		lastSpace = r == ' '
	}
	out := strings.TrimSpace(b.String())
	if len(out) > maxRedactedLen {
		out = out[:maxRedactedLen] + "…"
	}
	return out
}
