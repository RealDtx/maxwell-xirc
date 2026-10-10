package links

import (
	"strings"
	"testing"
	"time"

	"github.com/RealDtx/maxwell-irc/db"
)

func TestFormatParseRoundTrip(t *testing.T) {
	first := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	last := time.Date(2026, 10, 9, 8, 13, 0, 0, time.UTC)
	for _, bot := range []string{"ExampleBot", "[Example]Bot", "Bot|DL", "Bot^", "{x}`\\"} {
		in := Link{Host: "irc.example.net", Channel: "#example-downloads", Bot: bot, Pack: 123,
			Name: "Some File (2026) [x265].mkv", Size: "1.4G", First: first, Last: last}
		s := Format(in)
		if !strings.HasPrefix(s, "xirc://irc.example.net/%23example-downloads/") {
			t.Fatalf("unexpected link %q", s)
		}
		got, bad := Parse(s)
		if len(bad) != 0 || len(got) != 1 {
			t.Fatalf("Parse(%q) = %+v, %+v", s, got, bad)
		}
		g := got[0]
		if !g.First.Equal(in.First) || !g.Last.Equal(in.Last) {
			t.Errorf("dates: got %v/%v want %v/%v", g.First, g.Last, in.First, in.Last)
		}
		g.First, g.Last, in.First, in.Last = time.Time{}, time.Time{}, time.Time{}, time.Time{}
		if g != in {
			t.Errorf("round trip mismatch:\n got %+v\nwant %+v", g, in)
		}
	}
}

func TestParseMinimalAndLenient(t *testing.T) {
	text := "hey, grab these: xirc://IRC.Example.net:6697/%23example-downloads/ExampleBot/5.\r\n" +
		"and web+xirc://irc.example.net/#example-downloads/ExampleBot/6?name=A.mkv&foo=bar, thanks\n" +
		"dup xirc://irc.example.net/%23example-downloads/ExampleBot/5"
	got, bad := Parse(text)
	if len(bad) != 0 {
		t.Fatalf("unexpected errors %+v", bad)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 links (duplicate collapsed), got %+v", got)
	}
	if got[0].Host != "irc.example.net" || got[0].Channel != "#example-downloads" || got[0].Pack != 5 || got[0].Name != "" {
		t.Errorf("first link = %+v", got[0])
	}
	if got[1].Pack != 6 || got[1].Name != "A.mkv" || got[1].Channel != "#example-downloads" {
		t.Errorf("second link = %+v", got[1])
	}
}

func TestParseErrors(t *testing.T) {
	for _, s := range []string{
		"xirc://irc.example.net/%23c/Bot",        // no pack
		"xirc://irc.example.net/%23c/Bot/0",      // pack must be > 0
		"xirc://irc.example.net/%23c/Bot/x",      // not a number
		"xirc:///%23c/Bot/5",                     // no host
		"xirc://irc.example.net/%23c/Bot/5/more", // too many parts
	} {
		got, bad := Parse(s)
		if len(got) != 0 || len(bad) != 1 || bad[0].Reason == "" {
			t.Errorf("Parse(%q) = %+v, %+v; want one error", s, got, bad)
		}
	}
	if got, bad := Parse("no links here"); len(got)+len(bad) != 0 {
		t.Errorf("plain text should yield nothing, got %+v %+v", got, bad)
	}
}

func TestResolveServer(t *testing.T) {
	servers := []db.Server{
		{ID: 1, Host: "irc.example.net"},
		{ID: 2, Host: "irc.other.org"},
		{ID: 3, Host: "irc2.other.org"},
		{ID: 4, Host: "10.0.0.5"},
	}
	cases := []struct {
		host string
		id   int64
		ok   bool
	}{
		{"IRC.EXAMPLE.NET", 1, true},  // exact, case-insensitive
		{"irc2.example.net", 1, true}, // same domain, unique
		{"irc3.other.org", 0, false},  // same domain, ambiguous
		{"irc.other.org", 2, true},    // exact beats ambiguity
		{"10.0.0.7", 0, false},        // IPs never domain-match
		{"irc.unknown.net", 0, false},
	}
	for _, c := range cases {
		srv, ok := ResolveServer(c.host, servers)
		if ok != c.ok || (ok && srv.ID != c.id) {
			t.Errorf("ResolveServer(%q) = %+v, %v; want id %d, %v", c.host, srv, ok, c.id, c.ok)
		}
	}
}
