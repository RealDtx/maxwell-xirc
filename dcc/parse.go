package dcc

import (
	"fmt"
	"regexp"
	"strconv"
)

type DCCOffer struct {
	Filename string
	IP       string
	Port     int
	Size     int64
	Passive  bool
}

// Matches: DCC SEND "filename" ip port size  OR  DCC SEND filename ip port size
var dccSendQuoted = regexp.MustCompile(`^DCC SEND "([^"]+)"\s+(\d+)\s+(\d+)\s+(\d+)$`)
var dccSendUnquoted = regexp.MustCompile(`^DCC SEND (\S+)\s+(\d+)\s+(\d+)\s+(\d+)$`)

func ParseDCCSend(msg string) (*DCCOffer, error) {
	var matches []string

	matches = dccSendQuoted.FindStringSubmatch(msg)
	if matches == nil {
		matches = dccSendUnquoted.FindStringSubmatch(msg)
	}
	if matches == nil {
		return nil, fmt.Errorf("not a valid DCC SEND message: %s", msg)
	}

	ipInt, err := strconv.ParseUint(matches[2], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid IP integer: %s", matches[2])
	}

	port, err := strconv.Atoi(matches[3])
	if err != nil {
		return nil, fmt.Errorf("invalid port: %s", matches[3])
	}

	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("invalid port %d: out of range", port)
	}

	size, err := strconv.ParseInt(matches[4], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid size: %s", matches[4])
	}

	return &DCCOffer{
		Filename: matches[1],
		IP:       intToIP(uint32(ipInt)),
		Port:     port,
		Size:     size,
		Passive:  port == 0,
	}, nil
}

func intToIP(n uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		(n>>24)&0xFF,
		(n>>16)&0xFF,
		(n>>8)&0xFF,
		n&0xFF,
	)
}
