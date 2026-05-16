package main

import (
	"net/http"
	"strings"
	"sync"
	"testing"

	"golang.org/x/time/rate"
)

func TestValidUsername(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"ab", false},
		{"abc", true},
		{"user_01", true},
		{"User.Name-9", true},
		{"", false},
		{"a b", false},
		{strings.Repeat("a", 33), false},
	}
	for _, tc := range cases {
		if got := validUsername(tc.in); got != tc.want {
			t.Errorf("validUsername(%q) = %v want %v", tc.in, got, tc.want)
		}
	}
}

func TestValidEmail(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"not-an-email", false},
		{"user@example.com", true},
		{"User+tag@example.co.uk", true},
	}
	for _, tc := range cases {
		if got := validEmail(tc.in); got != tc.want {
			t.Errorf("validEmail(%q) = %v want %v", tc.in, got, tc.want)
		}
	}
}

func TestRandHexAndRandDigits(t *testing.T) {
	h, err := randHex(16)
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 32 {
		t.Fatalf("randHex(16) len got %d want 32", len(h))
	}
	d, err := randDigits(6)
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != 6 {
		t.Fatalf("randDigits(6) len got %d", len(d))
	}
	for i := 0; i < len(d); i++ {
		if d[i] < '0' || d[i] > '9' {
			t.Fatalf("non-digit in %q", d)
		}
	}
}

func TestClientIP(t *testing.T) {
	// Default (no trusted-proxy header configured): X-Forwarded-For must be
	// ignored so a direct attacker cannot spoof their source IP and bypass
	// per-IP rate limits.
	prev := trustedProxyHeader
	trustedProxyHeader = ""
	defer func() { trustedProxyHeader = prev }()

	r := &http.Request{Header: http.Header{}, RemoteAddr: "203.0.113.7:49152"}
	if got := clientIP(r); got != "203.0.113.7" {
		t.Fatalf("RemoteAddr: got %q", got)
	}
	rSpoof := &http.Request{Header: http.Header{"X-Forwarded-For": {"198.51.100.2, 10.0.0.1"}}, RemoteAddr: "127.0.0.1:1"}
	if got := clientIP(rSpoof); got != "127.0.0.1" {
		t.Fatalf("X-Forwarded-For must be ignored when no trusted proxy: got %q", got)
	}
	r4 := &http.Request{Header: http.Header{}, RemoteAddr: "not-a-hostport"}
	if got := clientIP(r4); got != "not-a-hostport" {
		t.Fatalf("bad RemoteAddr: got %q", got)
	}

	// With a trusted proxy header (e.g. Fly-Client-IP), use the leftmost value.
	trustedProxyHeader = "Fly-Client-IP"
	rFly := &http.Request{Header: http.Header{}, RemoteAddr: "127.0.0.1:1"}
	rFly.Header.Set("Fly-Client-IP", "198.51.100.2")
	if got := clientIP(rFly); got != "198.51.100.2" {
		t.Fatalf("Fly-Client-IP: got %q", got)
	}
	rXFF := &http.Request{Header: http.Header{}, RemoteAddr: "127.0.0.1:1"}
	rXFF.Header.Set("Fly-Client-IP", " 198.51.100.9 , 10.0.0.1 ")
	if got := clientIP(rXFF); got != "198.51.100.9" {
		t.Fatalf("trusted multi-hop: got %q", got)
	}
	rEmpty := &http.Request{Header: http.Header{}, RemoteAddr: "10.0.0.5:1"}
	rEmpty.Header.Set("Fly-Client-IP", "")
	if got := clientIP(rEmpty); got != "10.0.0.5" {
		t.Fatalf("empty trusted header should fall back to RemoteAddr: got %q", got)
	}
}

func TestPstnBridgeEnabled_env(t *testing.T) {
	t.Setenv("PHAZE_ENABLE_PSTN", "")
	if pstnBridgeEnabled() {
		t.Fatal("empty env should be off")
	}
	t.Setenv("PHAZE_ENABLE_PSTN", "true")
	if !pstnBridgeEnabled() {
		t.Fatal("true should enable")
	}
	t.Setenv("PHAZE_ENABLE_PSTN", "TRUE")
	if !pstnBridgeEnabled() {
		t.Fatal("TRUE should enable")
	}
}

func TestIPLimiterConcurrentSameIP(t *testing.T) {
	// High limit + burst so concurrent Allows do not deterministically exhaust tokens.
	lim := newIPLimiter(rate.Limit(1e9), 500_000)
	const ip = "192.0.2.1"
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				lim.allow(ip)
			}
		}()
	}
	wg.Wait()
	_ = lim.allow(ip)
}
