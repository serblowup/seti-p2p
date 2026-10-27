package netutil

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

func ValidateHostPort(a string) (string, error) {
	if a == "" {
		return "", fmt.Errorf("empty address")
	}
	host, portStr, err := net.SplitHostPort(a)
	if err != nil {
		return "", fmt.Errorf("split host:port: %w", err)
	}
	if host == "" {
		return "", fmt.Errorf("empty host")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", fmt.Errorf("bad port %q: %w", portStr, err)
	}
	if port <= 0 || port > 65535 {
		return "", fmt.Errorf("port out of range: %d", port)
	}
	normalizedHost, isIPv6, err := normalizeHost(host)
	if err != nil {
		return "", err
	}
	if isIPv6 {
		return "[" + normalizedHost + "]:" + strconv.Itoa(port), nil
	}
	return normalizedHost + ":" + strconv.Itoa(port), nil
}

func normalizeHost(host string) (string, bool, error) {
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return v4.String(), false, nil
		}
		return ip.String(), true, nil
	}
	if looksLikeIPv4(host) {
		return "", false, fmt.Errorf("invalid IPv4 literal: %q", host)
	}
	if err := validateHostname(host); err != nil {
		return "", false, err
	}
	return strings.ToLower(host), false, nil
}

func looksLikeIPv4(host string) bool {
	if host == "" {
		return false
	}
	dots := 0
	for _, r := range host {
		switch {
		case r >= '0' && r <= '9':
		case r == '.':
			dots++
		default:
			return false
		}
	}
	return dots == 3
}

func validateHostname(host string) error {
	if len(host) == 0 || len(host) > 253 {
		return fmt.Errorf("hostname length must be 1..253: %q", host)
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 {
			return fmt.Errorf("hostname label length must be 1..63: %q", host)
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("hostname label must not start/end with '-': %q", host)
		}
		for _, r := range label {
			switch {
			case r >= 'a' && r <= 'z',
				r >= 'A' && r <= 'Z',
				r >= '0' && r <= '9',
				r == '-':
			default:
				return fmt.Errorf("hostname has invalid char %q: %q", r, host)
			}
		}
	}
	return nil
}