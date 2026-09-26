package server

import (
	"net/http/httptest"
	"testing"
)

func TestWebSocketRejectsForeignOrigin(t *testing.T) {
	r := httptest.NewRequest("GET", "http://xirc.lan/ws", nil)
	r.Header.Set("Origin", "http://evil.example")
	if upgrader.CheckOrigin != nil && upgrader.CheckOrigin(r) {
		t.Fatal("custom CheckOrigin accepts any origin; remove it so gorilla's same-host check applies")
	}
}
