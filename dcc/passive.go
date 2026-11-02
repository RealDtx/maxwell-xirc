package dcc

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ParsePortRange parses "50000-50010" or "50000". Empty means passive DCC is off (0, 0).
func ParsePortRange(s string) (lo, hi int, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, nil
	}
	a, b, isRange := strings.Cut(s, "-")
	lo, err1 := strconv.Atoi(strings.TrimSpace(a))
	hi = lo
	var err2 error
	if isRange {
		hi, err2 = strconv.Atoi(strings.TrimSpace(b))
	}
	if err1 != nil || err2 != nil || lo < 1024 || hi > 65535 || hi < lo {
		return 0, 0, fmt.Errorf("expected a port or range within 1024–65535, e.g. 50000-50010")
	}
	return lo, hi, nil
}

// ListenRange listens on the first free TCP port in [lo, hi] on all interfaces.
func ListenRange(lo, hi int) (net.Listener, int, error) {
	for p := lo; p <= hi; p++ {
		if l, err := net.Listen("tcp4", ":"+strconv.Itoa(p)); err == nil {
			return l, p, nil
		}
	}
	return nil, 0, fmt.Errorf("no free port in %d-%d (all in use by other transfers?)", lo, hi)
}

// ipLookupURL answers with the caller's public IPv4 as plain text.
var ipLookupURL = "https://api.ipv4.ipify.org"

var publicIP struct {
	sync.Mutex
	ip net.IP
	at time.Time
}

// PublicIPv4 returns override if set, else the public IPv4 as seen from the
// internet (looked up via ipLookupURL, cached 10 min). The bot must reach this
// address, so the router's WAN IP is needed, not the LAN one.
func PublicIPv4(override string) (net.IP, error) {
	if override = strings.TrimSpace(override); override != "" {
		ip := net.ParseIP(override).To4()
		if ip == nil {
			return nil, fmt.Errorf("public IP %q is not an IPv4 address", override)
		}
		return ip, nil
	}
	publicIP.Lock()
	defer publicIP.Unlock()
	if publicIP.ip != nil && time.Since(publicIP.at) < 10*time.Minute {
		return publicIP.ip, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ipLookupURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("detecting public IP: %w (set downloads.public_ip)", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
	ip := net.ParseIP(strings.TrimSpace(string(b))).To4()
	if ip == nil {
		return nil, fmt.Errorf("detecting public IP: unexpected answer %q (set downloads.public_ip)", b)
	}
	publicIP.ip, publicIP.at = ip, time.Now()
	return ip, nil
}
