package dcc

import (
	"encoding/binary"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

type DCCOffer struct {
	Filename string
	IP       string
	Port     int
	Size     int64
	Passive  bool
	Token    string // passive (reverse) DCC: echoed back in our reply
	SSL      bool   // DCC SSEND: the transfer runs over TLS
}

// Matches: DCC SEND "filename" ip port size [token]  OR  DCC SEND filename ip port size [token].
// Passive (reverse) DCC sends port 0 plus a token: DCC SEND file ip 0 size token.
// DCC SSEND is the same offer for a TLS transfer (bots answering "xdcc ssend").
var dccSendQuoted = regexp.MustCompile(`^DCC (S?SEND) "([^"]+)"\s+(\d+)\s+(\d+)\s+(\d+)(?:\s+(\S+))?$`)
var dccSendUnquoted = regexp.MustCompile(`^DCC (S?SEND) (\S+)\s+(\d+)\s+(\d+)\s+(\d+)(?:\s+(\S+))?$`)

func ParseDCCSend(msg string) (*DCCOffer, error) {
	var matches []string

	matches = dccSendQuoted.FindStringSubmatch(msg)
	if matches == nil {
		matches = dccSendUnquoted.FindStringSubmatch(msg)
	}
	if matches == nil {
		return nil, fmt.Errorf("not a valid DCC SEND message: %s", msg)
	}

	ipInt, err := strconv.ParseUint(matches[3], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid IP integer: %s", matches[3])
	}

	port, err := strconv.Atoi(matches[4])
	if err != nil {
		return nil, fmt.Errorf("invalid port: %s", matches[4])
	}

	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("invalid port %d: out of range", port)
	}

	size, err := strconv.ParseInt(matches[5], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid size: %s", matches[5])
	}

	return &DCCOffer{
		Filename: matches[2],
		IP:       intToIP(uint32(ipInt)),
		Port:     port,
		Size:     size,
		Passive:  port == 0,
		Token:    matches[6],
		SSL:      matches[1] == "SSEND",
	}, nil
}

// PassiveReply is the receiver's answer to a passive offer: the same SEND
// with our reachable IPv4 and listening port, echoing the sender's token
// (https://en.wikipedia.org/wiki/Direct_Client-to-Client, Reverse DCC).
// The sender then connects to us.
func PassiveReply(o *DCCOffer, ip net.IP, port int) (string, error) {
	v4 := ip.To4()
	if v4 == nil {
		return "", fmt.Errorf("passive DCC needs an IPv4 address, got %v", ip)
	}
	name := o.Filename
	if strings.ContainsAny(name, " \t") {
		name = `"` + name + `"`
	}
	verb := "SEND"
	if o.SSL {
		verb = "SSEND" // we listen with TLS; same reply, S-verb
	}
	return fmt.Sprintf("DCC %s %s %d %d %d %s", verb, name, binary.BigEndian.Uint32(v4), port, o.Size, o.Token), nil
}

func intToIP(n uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		(n>>24)&0xFF,
		(n>>16)&0xFF,
		(n>>8)&0xFF,
		n&0xFF,
	)
}
