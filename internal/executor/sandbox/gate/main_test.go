package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/executor/sandbox"
)

func TestIsPrivateOrReservedIP(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{"loopback v4", "127.0.0.1", true},
		{"loopback v6", "::1", true},
		{"link-local incl. cloud metadata", "169.254.169.254", true},
		{"rfc1918 10/8", "10.0.0.5", true},
		{"rfc1918 172.16/12", "172.16.5.5", true},
		{"rfc1918 192.168/16", "192.168.1.1", true},
		{"unique local v6", "fdc4:f303:9324::254", true},
		{"unspecified v4", "0.0.0.0", true},
		{"unspecified v6", "::", true},
		{"public v4 (google dns)", "8.8.8.8", false},
		{"public v6", "2606:4700:4700::1111", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("net.ParseIP(%q) failed", tt.ip)
			}
			if got := isPrivateOrReservedIP(ip); got != tt.want {
				t.Errorf("isPrivateOrReservedIP(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

// fakeResolver lets tests control exactly what a hostname resolves to
// without touching real DNS.
type fakeResolver map[string][]net.IPAddr

func (f fakeResolver) lookup(_ context.Context, host string) ([]net.IPAddr, error) {
	addrs, ok := f[host]
	if !ok {
		return nil, errors.New("fakeResolver: no such host")
	}
	return addrs, nil
}

func ipAddr(s string) net.IPAddr { return net.IPAddr{IP: net.ParseIP(s)} }

// TestServeHTTP_RejectsRebindingToPrivateIP proves the fix for a
// second-review finding: a hostname that matches the allowlist by *name*
// but resolves to a private IP is still rejected, by default — the
// allowlist match alone is not sufficient.
func TestServeHTTP_RejectsRebindingToPrivateIP(t *testing.T) {
	g := &gate{
		allow:   sandbox.EgressAllowlist{Version: 1, Allow: []sandbox.EgressRule{{Host: "rebind.example.com", Port: 443}}},
		resolve: fakeResolver{"rebind.example.com": {ipAddr("169.254.169.254")}}.lookup,
		dial: func(context.Context, string, string) (net.Conn, error) {
			t.Fatalf("dial must not be called when the resolved IP is private/reserved")
			return nil, nil
		},
		allowPrivate: false,
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodConnect, "/", nil)
	req.Host = "rebind.example.com:443"
	g.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for a rebinding target, got %d", rec.Code)
	}
}

// TestServeHTTP_AllowPrivateFlagBypassesTheCheck proves the escape hatch
// this package's own tests rely on (mock providers reached via a private/
// link-local docker-gateway address) actually works, and is opt-in, not the
// default.
func TestServeHTTP_AllowPrivateFlagBypassesTheCheck(t *testing.T) {
	dialed := false
	g := &gate{
		allow:   sandbox.EgressAllowlist{Version: 1, Allow: []sandbox.EgressRule{{Host: "mock.test", Port: 443}}},
		resolve: fakeResolver{"mock.test": {ipAddr("192.168.1.50")}}.lookup,
		dial: func(context.Context, string, string) (net.Conn, error) {
			dialed = true
			return nil, errors.New("stop before a real dial in this unit test")
		},
		allowPrivate: true,
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodConnect, "/", nil)
	req.Host = "mock.test:443"
	g.ServeHTTP(rec, req)

	if !dialed {
		t.Errorf("expected dial to be attempted once allowPrivate bypasses the resolve check")
	}
}

// TestServeHTTP_DeniesHostNotOnAllowlist proves the pre-existing allowlist
// check (unaffected by this fix) still runs before any resolve/dial.
func TestServeHTTP_DeniesHostNotOnAllowlist(t *testing.T) {
	g := &gate{
		allow: sandbox.EgressAllowlist{Version: 1, Allow: []sandbox.EgressRule{{Host: "api.anthropic.com", Port: 443}}},
		resolve: func(context.Context, string) ([]net.IPAddr, error) {
			t.Fatalf("resolve must not be called for a host that already failed the allowlist check")
			return nil, nil
		},
		dial: func(context.Context, string, string) (net.Conn, error) {
			t.Fatalf("dial must not be called for a host that already failed the allowlist check")
			return nil, nil
		},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodConnect, "/", nil)
	req.Host = "evil.example.com:443"
	g.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for a non-allowlisted host, got %d", rec.Code)
	}
}

// TestServeHTTP_RejectsNonConnectMethod proves the CONNECT-only contract
// (unrelated to this fix, but a cheap regression guard now that this file
// exists).
func TestServeHTTP_RejectsNonConnectMethod(t *testing.T) {
	g := &gate{allow: sandbox.EgressAllowlist{Version: 1}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	g.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("expected 501 for a non-CONNECT method, got %d", rec.Code)
	}
}
