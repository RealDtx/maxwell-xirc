package dcc

import "testing"

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
