package unit

import (
	"testing"

	"seti-p2p/internal/netutil"
)

func TestValidateHostPortIPv4(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"127.0.0.1:9101", "127.0.0.1:9101"},
		{"0.0.0.0:1", "0.0.0.0:1"},
		{"255.255.255.255:65535", "255.255.255.255:65535"},
	}
	for _, c := range cases {
		got, err := netutil.ValidateHostPort(c.in)
		if err != nil {
			t.Fatalf("ValidateHostPort(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("ValidateHostPort(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestValidateHostPortIPv6(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"[::1]:9101", "[::1]:9101"},
		{"[0:0:0:0:0:0:0:1]:9101", "[::1]:9101"},
		{"[2001:db8::1]:9101", "[2001:db8::1]:9101"},
		{"[2001:0db8:0000:0000:0000:0000:0000:0001]:9101", "[2001:db8::1]:9101"},
	}
	for _, c := range cases {
		got, err := netutil.ValidateHostPort(c.in)
		if err != nil {
			t.Fatalf("ValidateHostPort(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("ValidateHostPort(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestValidateHostPortIPv6Unbracketed(t *testing.T) {
	for _, in := range []string{
		"::1:9101",
		"2001:db8::1:9101",
	} {
		if _, err := netutil.ValidateHostPort(in); err == nil {
			t.Fatalf("ValidateHostPort(%q) must reject unbracketed IPv6", in)
		}
	}
}

func TestValidateHostPortHostname(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"localhost:9101", "localhost:9101"},
		{"Node-01.Local:9101", "node-01.local:9101"},
		{"a.b.c.example.com:9101", "a.b.c.example.com:9101"},
	}
	for _, c := range cases {
		got, err := netutil.ValidateHostPort(c.in)
		if err != nil {
			t.Fatalf("ValidateHostPort(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("ValidateHostPort(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestValidateHostPortRejects(t *testing.T) {
	cases := []string{
		"",
		":9101",
		"127.0.0.1",
		"127.0.0.1:",
		"127.0.0.1:0",
		"127.0.0.1:70000",
		"127.0.0.1:abc",
		"127.000.000.001:9101",
		"127.0.0.256:9101",
		"999.999.999.999:9101",
		"not a host:9101",
		"[::1]",
		"[::1]:",
		"[::1]:70000",
		"-bad.local:9101",
		"bad-.local:9101",
		"a..b:9101",
		"toolonglabel0123456789012345678901234567890123456789012345678901234567890:9101",
	}
	for _, in := range cases {
		if _, err := netutil.ValidateHostPort(in); err == nil {
			t.Fatalf("ValidateHostPort(%q) must be rejected", in)
		}
	}
}