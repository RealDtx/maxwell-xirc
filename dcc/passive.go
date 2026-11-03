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

// ipLookupURLs answer with the caller's public IPv4 as plain text. Several,
// because DNS filters (Pi-hole etc.) often block one or the other.
var ipLookupURLs = []string{
	"https://api.ipv4.ipify.org",
	"https://ipv4.icanhazip.com",
	"https://checkip.amazonaws.com",
}

// openDNSResolver answers "myip.opendns.com" with the asking address. It is
// queried directly, so a local DNS filter can't interfere.
var openDNSResolver = "208.67.222.222:53"

var publicIP struct {
	sync.Mutex
	ip net.IP
	at time.Time
}

// PublicIPv4 returns override if set (an IPv4 or a hostname such as a DynDNS
// name, resolved each call), else the public IPv4 as seen from the
// internet (cached 10 min). The bot must reach this address, so the
// router's WAN IP is needed, not the LAN one.
func PublicIPv4(override string) (net.IP, error) {
	if override = strings.TrimSpace(override); override != "" {
		if ip := net.ParseIP(override); ip != nil {
			if ip.To4() == nil {
				return nil, fmt.Errorf("public IP %q is not an IPv4 address", override)
			}
			return ip.To4(), nil
		}
		// A DynDNS hostname: resolved on every offer, so IP changes follow.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		addrs, err := net.DefaultResolver.LookupIP(ctx, "ip4", override)
		if err != nil || len(addrs) == 0 {
			return nil, fmt.Errorf("resolving public host %q: %v", override, err)
		}
		return addrs[0].To4(), nil
	}
	publicIP.Lock()
	defer publicIP.Unlock()
	if publicIP.ip != nil && time.Since(publicIP.at) < 10*time.Minute {
		return publicIP.ip, nil
	}
	ip, err := detectPublicIPv4(ipLookupURLs, openDNSResolver)
	if err != nil {
		return nil, err
	}
	publicIP.ip, publicIP.at = ip, time.Now()
	return ip, nil
}

// detectPublicIPv4 tries each URL, then the OpenDNS resolver, and returns
// the first IPv4 found.
func detectPublicIPv4(urls []string, resolver string) (net.IP, error) {
	var errs []string
	for _, u := range urls {
		ip, err := ipFromURL(u)
		if err == nil {
			return ip, nil
		}
		errs = append(errs, err.Error())
	}
	if resolver != "" {
		ip, err := ipFromOpenDNS(resolver)
		if err == nil {
			return ip, nil
		}
		errs = append(errs, "opendns: "+err.Error())
	}
	return nil, fmt.Errorf("detecting public IP failed (set downloads.public_ip): %s", strings.Join(errs, "; "))
}

func ipFromURL(u string) (net.IP, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
	ip := net.ParseIP(strings.TrimSpace(string(b))).To4()
	if ip == nil {
		return nil, fmt.Errorf("%s: unexpected answer %q", u, b)
	}
	return ip, nil
}

func ipFromOpenDNS(resolver string) (net.IP, error) {
	r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp", resolver)
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addrs, err := r.LookupIP(ctx, "ip4", "myip.opendns.com")
	if err != nil {
		return nil, err
	}
	for _, a := range addrs {
		if v4 := a.To4(); v4 != nil {
			return v4, nil
		}
	}
	return nil, fmt.Errorf("no IPv4 answer")
}
