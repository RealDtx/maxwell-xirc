package discovery

import (
	"reflect"
	"testing"
)

func TestParseListLine(t *testing.T) {
	tests := []struct {
		line string
		e    ListEntry
		end  bool
		ok   bool
	}{
		{":irc.example.org 322 me #Example 42 :\x02XDCC\x02 packs - see #example-chat", ListEntry{"#Example", 42, "XDCC packs - see #example-chat"}, false, true},
		{":irc.example.org 322 me #bare 3 :", ListEntry{"#bare", 3, ""}, false, true},
		{":irc.example.org 323 me :End of /LIST", ListEntry{}, true, true},
		{":irc.example.org 366 me #x :End", ListEntry{}, false, false},
		{":irc.example.org 322 me #x notnum :t", ListEntry{}, false, false},
		{"garbage", ListEntry{}, false, false},
	}
	for _, tc := range tests {
		e, end, ok := ParseListLine(tc.line)
		if e != tc.e || end != tc.end || ok != tc.ok {
			t.Errorf("ParseListLine(%q) = %v,%v,%v want %v,%v,%v", tc.line, e, end, ok, tc.e, tc.end, tc.ok)
		}
	}
}

func TestParseNumericChannel(t *testing.T) {
	if c, ok := ParseTooManyChannels(":irc.example.org 405 me #foo :You have joined too many channels"); !ok || c != "#foo" {
		t.Errorf("405: %q %v", c, ok)
	}
	if _, ok := ParseTooManyChannels(":irc.example.org 366 me #foo :x"); ok {
		t.Error("405 matched 366")
	}
	if c, ok := ParseEndOfNames(":irc.example.org 366 me #foo :End of /NAMES list."); !ok || c != "#foo" {
		t.Errorf("366: %q %v", c, ok)
	}
	if _, ok := ParseEndOfNames(":irc.example.org 405 me #foo :x"); ok {
		t.Error("366 matched 405")
	}
}

func TestMatchKeyword(t *testing.T) {
	tests := []struct {
		name, topic string
		extra       []string
		want        string
	}{
		{"#movies", "Fast XDCC here", nil, "xdcc"},
		{"#anime-dl", "", []string{"anime"}, "anime"},
		{"#chat", "hello", nil, ""},
	}
	for _, tc := range tests {
		if got := MatchKeyword(tc.name, tc.topic, tc.extra); got != tc.want {
			t.Errorf("MatchKeyword(%q,%q,%v)=%q want %q", tc.name, tc.topic, tc.extra, got, tc.want)
		}
	}
}

func TestExtractChannels(t *testing.T) {
	got := ExtractChannels("join #Example-Chat, and #other. also #Example-chat")
	want := []string{"#example-chat", "#other"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		ch, partner string
		cAds, pAds  int
		want        string
	}{
		{"#example-chat", "#example", 0, 0, "download"},
		{"#example", "#example-chat", 0, 0, "chat"},
		{"#a", "#b", 5, 0, "download"},
		{"#a", "#b", 0, 5, "chat"},
		{"#a-chat", "#b-chat", 0, 0, "chat"},
		{"#a", "", 0, 0, "unknown"},
		{"#a", "", 3, 0, "download"},
	}
	for _, tc := range tests {
		if got := Classify(tc.ch, tc.partner, tc.cAds, tc.pAds); got != tc.want {
			t.Errorf("Classify(%q,%q,%d,%d)=%q want %q", tc.ch, tc.partner, tc.cAds, tc.pAds, got, tc.want)
		}
	}
}

func TestRealmPrefill(t *testing.T) {
	if n, d := RealmPrefill("#example-chat", "#example", "download"); n != "#example" || d != "#example-chat" {
		t.Errorf("got %q %q", n, d)
	}
	if n, d := RealmPrefill("#example", "#example-chat", "chat"); n != "#example" || d != "#example-chat" {
		t.Errorf("got %q %q", n, d)
	}
}
