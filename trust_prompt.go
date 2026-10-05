package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
)

// ifAddrs is one network interface as seen by detectSubnets (a plain struct
// so the filtering is testable without real interfaces).
type ifAddrs struct {
	Name  string
	Up    bool
	Addrs []net.Addr
}

func systemInterfaces() []ifAddrs {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []ifAddrs
	for _, i := range ifs {
		addrs, _ := i.Addrs()
		out = append(out, ifAddrs{Name: i.Name, Up: i.Flags&net.FlagUp != 0 && i.Flags&net.FlagLoopback == 0, Addrs: addrs})
	}
	return out
}

func hasPrefix(name string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0).To4(), Mask: net.CIDRMask(10, 32)}

// detectSubnets returns the private IPv4 subnets of the machine's LAN
// interfaces (home) and VPN interfaces (vpn), skipping container bridges.
// ponytail: name-prefix heuristic; an oddly named NIC lands in neither list,
// and option 4 lets the user type it.
func detectSubnets(ifs []ifAddrs) (home, vpn []string) {
	seen := map[string]bool{}
	for _, i := range ifs {
		if !i.Up || hasPrefix(i.Name, "lo", "docker", "br-", "veth", "virbr", "cali", "vxlan", "tunl", "cni", "flannel", "podman") {
			continue
		}
		isVPN := hasPrefix(i.Name, "tun", "wg", "tailscale", "zt", "utun")
		for _, a := range i.Addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil {
				continue
			}
			var cidr string
			switch {
			case isVPN && cgnat.Contains(n.IP): // Tailscale: each node is a /32
				cidr = cgnat.String()
			case n.IP.IsPrivate() && !isHost(n): // a /32 is an overlay endpoint, not a LAN
				cidr = (&net.IPNet{IP: n.IP.Mask(n.Mask), Mask: n.Mask}).String()
			default:
				continue
			}
			if seen[cidr] {
				continue
			}
			seen[cidr] = true
			if isVPN {
				vpn = append(vpn, cidr)
			} else {
				home = append(home, cidr)
			}
		}
	}
	return home, vpn
}

func isHost(n *net.IPNet) bool {
	ones, bits := n.Mask.Size()
	return ones == bits
}

func validNets(list []string) bool {
	for _, s := range list {
		if _, _, err := net.ParseCIDR(s); err != nil && net.ParseIP(s) == nil {
			return false
		}
	}
	return len(list) > 0
}

func splitNets(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\r' })
}

// askTrust asks who skips login on first start. It returns the networks that
// become admin without login (nil = everyone logs in) and whether they are
// LAN networks, which need the server to listen beyond loopback. Unreadable
// or unknown answers mean "nobody": the safe default.
func askTrust(in *bufio.Reader, out io.Writer, home, vpn []string) (nets []string, lan bool) {
	fmt.Fprintf(out, "\nWho may use maXwell IRC without logging in?\n")
	fmt.Fprintf(out, "  1) Nobody: everyone logs in, the first browser visit creates the admin  [default]\n")
	fmt.Fprintf(out, "  2) Only this computer (127.0.0.1, ::1) as admin\n")
	for i, h := range home {
		fmt.Fprintf(out, "  %d) Home network %s as admin\n", i+3, h)
	}
	other := len(home) + 3
	fmt.Fprintf(out, "  %d) Other networks as admin (home, VPN, …: you type them)\n", other)
	fmt.Fprintf(out, "Choice [1]: ")
	line, _ := in.ReadString('\n')
	var n int
	fmt.Sscanf(strings.TrimSpace(line), "%d", &n)
	switch {
	case n == 2:
		return []string{"127.0.0.1/32", "::1/128"}, false
	case n >= 3 && n < other:
		return []string{home[n-3]}, true
	case n != other:
		return nil, false
	}

	suggest := append(append([]string{}, home...), vpn...)
	if len(home)+len(vpn) > 0 {
		var labels []string
		for _, h := range home {
			labels = append(labels, h+" (home)")
		}
		for _, v := range vpn {
			labels = append(labels, v+" (VPN)")
		}
		fmt.Fprintf(out, "     Detected: %s\n", strings.Join(labels, ", "))
	}
	fmt.Fprintf(out, "     Common:   192.168.0.0/16 (any 192.168.x home), 10.0.0.0/8, 172.16.0.0/12,\n")
	fmt.Fprintf(out, "               10.8.0.0/24 (OpenVPN), 10.6.0.0/24 (WireGuard/PiVPN), 100.64.0.0/10 (Tailscale)\n")
	for tries := 0; tries < 3; tries++ {
		fmt.Fprintf(out, "  Networks, comma-separated [%s]: ", strings.Join(suggest, ", "))
		line, err := in.ReadString('\n')
		list := splitNets(line)
		if len(list) == 0 {
			list = suggest
		}
		if validNets(list) {
			return list, true
		}
		if err != nil {
			break
		}
		fmt.Fprintf(out, "     Not a valid IP or CIDR list, try again.\n")
	}
	fmt.Fprintf(out, "     No networks set: everyone logs in.\n")
	return nil, false
}
