package dcc

import (
	"io"
	"net"
	"os"
	"strconv"
	"testing"
)

func TestParseDCCSend_Normal(t *testing.T) {
	// DCC SEND filename ip port filesize
	msg := `DCC SEND "Some.Movie.2024.mkv" 3232235777 4500 1500000000`
	offer, err := ParseDCCSend(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if offer.Filename != "Some.Movie.2024.mkv" {
		t.Errorf("expected filename Some.Movie.2024.mkv, got %s", offer.Filename)
	}
	if offer.IP != "192.168.1.1" {
		t.Errorf("expected IP 192.168.1.1, got %s", offer.IP)
	}
	if offer.Port != 4500 {
		t.Errorf("expected port 4500, got %d", offer.Port)
	}
	if offer.Size != 1500000000 {
		t.Errorf("expected size 1500000000, got %d", offer.Size)
	}
	if offer.Passive {
		t.Error("expected non-passive")
	}
}

func TestParseDCCSend_Passive(t *testing.T) {
	msg := `DCC SEND "file.mkv" 3232235777 0 700000000`
	offer, err := ParseDCCSend(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !offer.Passive {
		t.Error("expected passive (port 0)")
	}
	if offer.Size != 700000000 {
		t.Errorf("expected size 700000000, got %d", offer.Size)
	}
}

func TestParseDCCSend_NoQuotes(t *testing.T) {
	msg := `DCC SEND movie.mkv 3232235777 4500 1500000000`
	offer, err := ParseDCCSend(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if offer.Filename != "movie.mkv" {
		t.Errorf("expected filename movie.mkv, got %s", offer.Filename)
	}
}

func TestParseDCCSend_SpacesInQuotedFilename(t *testing.T) {
	msg := `DCC SEND "Some Movie 2024.mkv" 3232235777 4500 1500000000`
	offer, err := ParseDCCSend(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if offer.Filename != "Some Movie 2024.mkv" {
		t.Errorf("expected 'Some Movie 2024.mkv', got %s", offer.Filename)
	}
}

func TestParseDCCSend_InvalidFormat(t *testing.T) {
	_, err := ParseDCCSend("not a dcc send message")
	if err == nil {
		t.Error("expected error for invalid message")
	}
}

func TestParseDCCSend_InvalidPort(t *testing.T) {
	_, err := ParseDCCSend(`DCC SEND file.mkv 3232235777 99999 1500000000`)
	if err == nil {
		t.Error("expected error for out-of-range port")
	}
}

func TestIntToIP(t *testing.T) {
	tests := []struct {
		input    uint32
		expected string
	}{
		{3232235777, "192.168.1.1"},
		{2130706433, "127.0.0.1"},
		{0, "0.0.0.0"},
	}
	for _, tt := range tests {
		got := intToIP(tt.input)
		if got != tt.expected {
			t.Errorf("intToIP(%d) = %s, want %s", tt.input, got, tt.expected)
		}
	}
}

func TestPassiveTokenAndReply(t *testing.T) {
	o, err := ParseDCCSend(`DCC SEND "My File.mkv" 3232235777 0 700000000 42`)
	if err != nil || !o.Passive || o.Token != "42" {
		t.Fatalf("offer=%+v err=%v", o, err)
	}
	got, err := PassiveReply(o, net.ParseIP("1.2.3.4"), 50001)
	if want := `DCC SEND "My File.mkv" 16909060 50001 700000000 42`; err != nil || got != want {
		t.Errorf("got %q err=%v, want %q", got, err, want)
	}
	for in, ok := range map[string]bool{"": true, "50000": true, "50000-50010": true, "80": false, "5-1": false, "x": false, "50010-50000": false} {
		if _, _, err := ParsePortRange(in); (err == nil) != ok {
			t.Errorf("ParsePortRange(%q) err=%v", in, err)
		}
	}
}

func TestPassiveTransferAccepts(t *testing.T) {
	l, port, err := ListenRange(50100, 50120)
	if err != nil {
		t.Skip(err)
	}
	dest := t.TempDir() + "/f.bin"
	tr := NewTransfer(&DCCOffer{Filename: "f.bin", Size: 5, Passive: true}, dest, nil)
	tr.SetListener(l)
	go func() {
		c, err := net.Dial("tcp4", "127.0.0.1:"+strconv.Itoa(port))
		if err == nil {
			c.Write([]byte("hello"))
			io.Copy(io.Discard, c) // drain acks until we close
			c.Close()
		}
	}()
	if err := tr.Start(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "hello" {
		t.Errorf("got %q", b)
	}
}
