package main

import (
	"bufio"
	"io"
	"net"
	"reflect"
	"strings"
	"testing"
)

func ipnet(s string) net.Addr {
	ip, n, _ := net.ParseCIDR(s)
	n.IP = ip
	return n
}

func TestDetectSubnets(t *testing.T) {
	home, vpn := detectSubnets([]ifAddrs{
		{Name: "lo", Up: true, Addrs: []net.Addr{ipnet("127.0.0.1/8")}},
		{Name: "eth0", Up: true, Addrs: []net.Addr{ipnet("192.168.20.5/24"), ipnet("fe80::1/64")}},
		{Name: "wlan0", Up: true, Addrs: []net.Addr{ipnet("192.168.20.7/24")}}, // same subnet: once
		{Name: "eth1", Up: false, Addrs: []net.Addr{ipnet("10.1.0.2/16")}},
		{Name: "docker0", Up: true, Addrs: []net.Addr{ipnet("172.17.0.1/16")}},
		{Name: "br-1a2b", Up: true, Addrs: []net.Addr{ipnet("172.18.0.1/16")}},
		{Name: "wan0", Up: true, Addrs: []net.Addr{ipnet("84.1.2.3/24")}}, // public
		{Name: "vxlan.calico", Up: true, Addrs: []net.Addr{ipnet("10.244.0.128/32")}},
		{Name: "tunl0", Up: true, Addrs: []net.Addr{ipnet("10.2.0.1/32")}},
		{Name: "eth2", Up: true, Addrs: []net.Addr{ipnet("10.3.0.9/32")}},
		{Name: "tun0", Up: true, Addrs: []net.Addr{ipnet("10.8.0.1/24")}},
		{Name: "tailscale0", Up: true, Addrs: []net.Addr{ipnet("100.101.102.103/32")}},
	})
	if want := []string{"192.168.20.0/24"}; !reflect.DeepEqual(home, want) {
		t.Errorf("home = %v, want %v", home, want)
	}
	if want := []string{"10.8.0.0/24", "100.64.0.0/10"}; !reflect.DeepEqual(vpn, want) {
		t.Errorf("vpn = %v, want %v", vpn, want)
	}
}

func TestAskTrust(t *testing.T) {
	home, vpn := []string{"192.168.20.0/24"}, []string{"10.8.0.0/24"}
	cases := []struct {
		input string
		nets  []string
		lan   bool
	}{
		{"\n", nil, false},
		{"", nil, false}, // EOF
		{"1\n", nil, false},
		{"garbage\n", nil, false},
		{"9\n", nil, false},
		{"2\n", []string{"127.0.0.1/32", "::1/128"}, false},
		{"3\n", []string{"192.168.20.0/24"}, true},
		{"4\n\n", []string{"192.168.20.0/24", "10.8.0.0/24"}, true}, // accept suggestion
		{"4\n10.0.0.0/8, 192.168.1.7\n", []string{"10.0.0.0/8", "192.168.1.7"}, true},
		{"4\nnope\n172.16.0.0/12\n", []string{"172.16.0.0/12"}, true}, // retry after invalid
		{"4\nnope\nnope\nnope\n", nil, false},
		{"4\nnope", nil, false}, // EOF after invalid
	}
	for _, c := range cases {
		nets, lan := askTrust(bufio.NewReader(strings.NewReader(c.input)), io.Discard, home, vpn)
		if !reflect.DeepEqual(nets, c.nets) || lan != c.lan {
			t.Errorf("input %q: got %v lan=%v, want %v lan=%v", c.input, nets, lan, c.nets, c.lan)
		}
	}
	// No detected subnets: "Other" is option 3, and its empty default is rejected.
	if nets, _ := askTrust(bufio.NewReader(strings.NewReader("3\n\n\n\n")), io.Discard, nil, nil); nets != nil {
		t.Errorf("empty suggestion accepted: %v", nets)
	}
}
