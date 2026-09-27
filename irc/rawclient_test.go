package irc

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

// TestRawClientRefusesLineBreaks: a CR/LF inside a user-supplied argument
// must never become a second IRC command on the wire.
func TestRawClientRefusesLineBreaks(t *testing.T) {
	local, remote := net.Pipe()
	defer local.Close()
	defer remote.Close()
	c := NewRawClient(RawClientConfig{Nick: "me"})
	c.conn = local

	lines := make(chan string, 10)
	go func() {
		sc := bufio.NewScanner(remote)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()

	c.Privmsg("ExampleBot", "xdcc send #1\r\nQUIT :pwned")
	c.Privmsg("Bot\nQUIT", "hi")
	c.SendLine("NAMES #chan\r\nQUIT :pwned")
	c.SendLine("NAMES #a\x00b")
	c.Privmsg("ExampleBot", "xdcc send #2") // sentinel: must arrive
	local.Close()

	var got []string
	timeout := time.After(2 * time.Second)
	for done := false; !done; {
		select {
		case l, ok := <-lines:
			if !ok {
				done = true
				break
			}
			got = append(got, l)
		case <-timeout:
			t.Fatal("timed out reading wire")
		}
	}
	if len(got) != 1 || got[0] != "PRIVMSG ExampleBot :xdcc send #2" {
		t.Fatalf("wire lines = %q, want only the sentinel PRIVMSG", got)
	}
	for _, l := range got {
		if strings.Contains(l, "QUIT") {
			t.Fatalf("injected QUIT reached the wire: %q", got)
		}
	}
}

func TestHasLineBreak(t *testing.T) {
	if HasLineBreak("#chan", "hello") {
		t.Error("clean input flagged")
	}
	for _, s := range []string{"a\rb", "a\nb", "a\x00b"} {
		if !HasLineBreak("ok", s) {
			t.Errorf("%q not flagged", s)
		}
	}
}
