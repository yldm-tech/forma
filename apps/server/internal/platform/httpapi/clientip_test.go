package httpapi

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

func resolver(hops int, logs *bytes.Buffer) *ClientIPResolver {
	return NewClientIPResolver(config.Config{TrustedProxyHopCount: hops}, slog.New(slog.NewTextHandler(logs, nil)))
}

func headers(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i < len(kv); i += 2 {
		h.Add(kv[i], kv[i+1])
	}
	return h
}

const peer = "192.0.2.50:51234"

func TestResolveSelectsTheTrustedHopFromTheRight(t *testing.T) {
	ip, ok := resolver(2, &bytes.Buffer{}).Resolve(headers("X-Forwarded-For", "9.9.9.9, 8.8.8.8, 203.0.113.7, 10.0.0.1"), peer)
	if !ok || ip != "203.0.113.7" {
		t.Fatalf("Resolve = %q, %v; want 203.0.113.7", ip, ok)
	}
}

func TestResolveJoinsRepeatedForwardedForHeaders(t *testing.T) {
	ip, ok := resolver(1, &bytes.Buffer{}).Resolve(headers("X-Forwarded-For", "9.9.9.9", "X-Forwarded-For", "203.0.113.7"), peer)
	if !ok || ip != "203.0.113.7" {
		t.Fatalf("Resolve = %q, %v; want the last entry of the combined chain", ip, ok)
	}
}

func TestResolveCanonicalizesTheSelectedEntry(t *testing.T) {
	cases := map[string]string{
		"203.0.113.7:80":                          "203.0.113.7",
		"203.0.113.7:443":                         "203.0.113.7",
		"203.0.113.7:65535":                       "203.0.113.7",
		"[2001:DB8:abcd:12::99]":                  "2001:0db8:abcd:0012:0000:0000:0000:0000",
		"[2001:DB8:abcd:12::99]:443":              "2001:0db8:abcd:0012:0000:0000:0000:0000",
		"::ffff:192.0.2.128":                      "192.0.2.128",
		"2001:DB8:abcd:12::1":                     "2001:0db8:abcd:0012:0000:0000:0000:0000",
		"2001:0db8:abcd:0012:ffff:eeee:dddd:cccc": "2001:0db8:abcd:0012:0000:0000:0000:0000",
		// A valid raw IPv6 value is an address, never an unbracketed socket.
		"2001:db8::1:443": "2001:0db8:0000:0000:0000:0000:0000:0000",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			ip, ok := resolver(1, &bytes.Buffer{}).Resolve(headers("X-Forwarded-For", in), peer)
			if !ok || ip != want {
				t.Fatalf("Resolve(%q) = %q, %v; want %q", in, ip, ok, want)
			}
		})
	}
}

func TestResolveKeepsDistinctClientsDistinct(t *testing.T) {
	r := resolver(1, &bytes.Buffer{})
	a, _ := r.Resolve(headers("X-Forwarded-For", "203.0.113.7"), peer)
	b, _ := r.Resolve(headers("X-Forwarded-For", "203.0.113.8"), peer)
	if a == b {
		t.Fatalf("two clients resolved to %q", a)
	}
}

func TestResolveRejectsMalformedOrAmbiguousEntries(t *testing.T) {
	for _, in := range []string{
		"",
		"not-an-ip",
		"203.0.113.7:0",
		"203.0.113.7:65536",
		"203.0.113.7:http",
		"[203.0.113.7]:443",
		"[2001:db8::1",
		"[2001:db8::1]:",
		"[2001:db8::1]:443:1",
		"2001:db8::1%eth0",
	} {
		t.Run(in, func(t *testing.T) {
			if ip, ok := resolver(1, &bytes.Buffer{}).Resolve(headers("X-Forwarded-For", in), peer); ok {
				t.Fatalf("Resolve(%q) = %q, want rejected", in, ip)
			}
		})
	}
}

func TestResolveFailsClosed(t *testing.T) {
	cases := map[string]struct {
		hops int
		h    http.Header
	}{
		"absent chain": {1, headers()},
		"short chain":  {2, headers("X-Forwarded-For", "203.0.113.7")},
		"no fallback to x-real-ip or cf-connecting-ip": {1, headers("Cf-Connecting-Ip", "198.51.100.5", "X-Real-Ip", "203.0.113.9")},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if ip, ok := resolver(tc.hops, &bytes.Buffer{}).Resolve(tc.h, peer); ok {
				t.Fatalf("Resolve = %q, want no trusted address", ip)
			}
		})
	}
}

// With no proxy in front, the TCP peer is the client and forwarding headers are caller-controlled noise.
func TestResolveWithoutProxyUsesThePeerAddress(t *testing.T) {
	r := resolver(0, &bytes.Buffer{})
	if ip, ok := r.Resolve(headers("X-Forwarded-For", "203.0.113.7"), peer); !ok || ip != "192.0.2.50" {
		t.Fatalf("Resolve = %q, %v; want the peer 192.0.2.50", ip, ok)
	}
	if ip, ok := r.Resolve(headers(), "[2001:db8:abcd:12::99]:443"); !ok || ip != "2001:0db8:abcd:0012:0000:0000:0000:0000" {
		t.Fatalf("Resolve = %q, %v; want the peer's /64", ip, ok)
	}
}

func TestResolveWarningsAreThrottledAndNeverIncludeHeaderValues(t *testing.T) {
	var logs bytes.Buffer
	invalidValue := "sensitive-invalid-forwarded-value"
	ignoredValue := "sensitive-untrusted-forwarding-value"

	resolver(0, &logs).Resolve(headers("X-Forwarded-For", "203.0.113.7"), peer)
	r1 := resolver(1, &logs)
	r1.Resolve(headers("X-Real-Ip", ignoredValue), peer)
	r1.Resolve(headers("X-Real-Ip", ignoredValue), peer)
	resolver(2, &logs).Resolve(headers("X-Forwarded-For", "203.0.113.7"), peer)
	resolver(1, &logs).Resolve(headers("X-Forwarded-For", invalidValue), peer)

	text := logs.String()
	if n := strings.Count(text, "level=WARN"); n != 4 {
		t.Fatalf("%d warnings, want 4 (one per reason):\n%s", n, text)
	}
	for _, want := range []string{"TRUSTED_PROXY_HOP_COUNT is 0", "X-Forwarded-For is absent", "fewer entries", "not a valid supported IP address"} {
		if !strings.Contains(text, want) {
			t.Errorf("no warning containing %q", want)
		}
	}
	if strings.Contains(text, invalidValue) || strings.Contains(text, ignoredValue) {
		t.Fatalf("a forwarded value reached the log:\n%s", text)
	}
}

func TestResolveWarnsAgainAfterTheThrottleWindow(t *testing.T) {
	var logs bytes.Buffer
	r := resolver(2, &logs)
	now := time.Unix(0, 0)
	r.now = func() time.Time { return now }
	short := headers("X-Forwarded-For", "203.0.113.7")

	r.Resolve(short, peer)
	now = now.Add(9 * time.Minute)
	r.Resolve(short, peer)
	now = now.Add(2 * time.Minute)
	r.Resolve(short, peer)

	if n := strings.Count(logs.String(), "level=WARN"); n != 2 {
		t.Fatalf("%d warnings, want 2", n)
	}
}

func TestResolveKeepsHeaderlessRequestsQuiet(t *testing.T) {
	var logs bytes.Buffer
	resolver(0, &logs).Resolve(headers(), peer)
	resolver(1, &logs).Resolve(headers(), peer)
	if logs.Len() != 0 {
		t.Fatalf("headerless requests logged:\n%s", logs.String())
	}
}

func TestMiddlewareStoresTheAddress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		xff  string
		want string
	}{
		{"203.0.113.7", "203.0.113.7"},
		{"not-an-ip", UntrustedClientIP},
		{"", UntrustedClientIP},
	}
	for _, tc := range cases {
		var got string
		r := gin.New()
		r.Use(resolver(1, &bytes.Buffer{}).Middleware())
		r.GET("/", func(c *gin.Context) { got = ClientIP(c.Request.Context()) })
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if tc.xff != "" {
			req.Header.Set("X-Forwarded-For", tc.xff)
		}
		r.ServeHTTP(httptest.NewRecorder(), req)
		if got != tc.want {
			t.Errorf("X-Forwarded-For %q: ClientIP = %q, want %q", tc.xff, got, tc.want)
		}
	}
	if got := ClientIP(context.Background()); got != UntrustedClientIP {
		t.Errorf("ClientIP outside a request = %q, want %q", got, UntrustedClientIP)
	}
}

// headerResolver is a resolver in the trusted-header mode: CF-Connecting-IP is read from peers in 10.42.0.0/16 and 2001:db8:42::/48.
func headerResolver(t *testing.T, hops int, logs *bytes.Buffer) *ClientIPResolver {
	t.Helper()
	cfg := config.Config{TrustedProxyHopCount: hops, ClientIPHeader: "cf-connecting-ip"}
	if err := cfg.TrustedProxyCIDRs.UnmarshalText([]byte("10.42.0.0/16, 2001:db8:42::/48")); err != nil {
		t.Fatal(err)
	}
	return NewClientIPResolver(cfg, slog.New(slog.NewTextHandler(logs, nil)))
}

func TestResolveTrustedHeader(t *testing.T) {
	const (
		proxy       = "10.42.3.4:40000"
		proxy6      = "[2001:db8:42::7]:40000"
		stranger    = "192.0.2.50:51234"
		client      = "198.51.100.5"
		cloudflared = "192.168.88.10"
	)
	cases := []struct {
		name string
		hops int
		peer string
		h    http.Header
		want string
		ok   bool
	}{
		{"trusted peer with the header", 1, proxy, headers("Cf-Connecting-Ip", client, "X-Forwarded-For", cloudflared), client, true},
		{"trusted IPv6 peer, IPv6 client collapsed to its /64", 1, proxy6, headers("Cf-Connecting-Ip", "2001:DB8:abcd:12::99"), "2001:0db8:abcd:0012:0000:0000:0000:0000", true},
		{"IPv4-mapped trusted peer", 1, "[::ffff:10.42.0.9]:40000", headers("Cf-Connecting-Ip", client), client, true},
		{"IPv4-mapped client", 1, proxy, headers("Cf-Connecting-Ip", "::ffff:"+client), client, true},
		{"surrounding spaces", 1, proxy, headers("Cf-Connecting-Ip", " "+client+" "), client, true},
		{"trusted peer with hop count 0", 0, proxy, headers("Cf-Connecting-Ip", client), client, true},
		{"trusted peer with a longer chain configured", 2, proxy, headers("Cf-Connecting-Ip", client), client, true},
		{"untrusted peer: the header is ignored and the chain decides", 1, stranger, headers("Cf-Connecting-Ip", client, "X-Forwarded-For", "203.0.113.7"), "203.0.113.7", true},
		{"untrusted peer without a chain fails closed", 1, stranger, headers("Cf-Connecting-Ip", client), "", false},
		{"untrusted peer with hop count 0 is the client", 0, stranger, headers("Cf-Connecting-Ip", client), "192.0.2.50", true},
		{"peer next to the trusted range", 1, "10.43.0.1:40000", headers("Cf-Connecting-Ip", client), "", false},
		{"unparseable peer", 1, "@", headers("Cf-Connecting-Ip", client, "X-Forwarded-For", "203.0.113.7"), "203.0.113.7", true},
		{"trusted peer without the header falls back to the chain", 1, proxy, headers("X-Forwarded-For", "203.0.113.7"), "203.0.113.7", true},
		{"trusted peer without the header or a chain", 1, proxy, headers(), "", false},
		{"invalid header fails closed instead of using the chain", 1, proxy, headers("Cf-Connecting-Ip", "not-an-ip", "X-Forwarded-For", "203.0.113.7"), "", false},
		{"empty header", 1, proxy, headers("Cf-Connecting-Ip", "", "X-Forwarded-For", "203.0.113.7"), "", false},
		{"a list in the header", 1, proxy, headers("Cf-Connecting-Ip", client+", 203.0.113.7"), "", false},
		{"the header repeated", 1, proxy, headers("Cf-Connecting-Ip", client, "Cf-Connecting-Ip", "203.0.113.7"), "", false},
		{"an address with a port", 1, proxy, headers("Cf-Connecting-Ip", client+":443"), "", false},
		{"a zoned address", 1, proxy, headers("Cf-Connecting-Ip", "fe80::1%eth0"), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ip, ok := headerResolver(t, tc.hops, &bytes.Buffer{}).Resolve(tc.h, tc.peer)
			if ip != tc.want || ok != tc.ok {
				t.Fatalf("Resolve = %q, %v; want %q, %v", ip, ok, tc.want, tc.ok)
			}
		})
	}
}

// The point of the trusted-header mode: clients behind one proxy no longer share one address, so they no longer share one rate-limit bucket.
func TestResolveTrustedHeaderKeepsClientsBehindOneProxyDistinct(t *testing.T) {
	r := headerResolver(t, 1, &bytes.Buffer{})
	seen := map[string]bool{}
	for _, client := range []string{"198.51.100.5", "198.51.100.6", "2001:db8:aaaa:1::1", "2001:db8:aaaa:2::1"} {
		ip, ok := r.Resolve(headers("Cf-Connecting-Ip", client, "X-Forwarded-For", "192.168.88.10"), "10.42.3.4:40000")
		if !ok || seen[ip] {
			t.Fatalf("client %s resolved to %q, %v; want a distinct trusted address", client, ip, ok)
		}
		seen[ip] = true
	}
}

// Without CLIENT_IP_HEADER the header is never read, even from a peer that would otherwise be trusted.
func TestResolveIgnoresTheHeaderWhenTheModeIsOff(t *testing.T) {
	cfg := config.Config{TrustedProxyHopCount: 1}
	if err := cfg.TrustedProxyCIDRs.UnmarshalText([]byte("10.42.0.0/16")); err != nil {
		t.Fatal(err)
	}
	r := NewClientIPResolver(cfg, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if ip, ok := r.Resolve(headers("Cf-Connecting-Ip", "198.51.100.5", "X-Forwarded-For", "203.0.113.7"), "10.42.3.4:40000"); !ok || ip != "203.0.113.7" {
		t.Fatalf("Resolve = %q, %v; want the chain's 203.0.113.7", ip, ok)
	}
}

func TestResolveWarnsAboutPrivateClientAddresses(t *testing.T) {
	cases := []struct {
		name string
		r    func(*bytes.Buffer) *ClientIPResolver
		h    http.Header
		peer string
		want string
	}{
		{"peer", func(b *bytes.Buffer) *ClientIPResolver { return resolver(0, b) }, headers(), "10.0.0.5:1234", "is the TCP peer"},
		{"forwarded hop", func(b *bytes.Buffer) *ClientIPResolver { return resolver(1, b) }, headers("X-Forwarded-For", "203.0.113.7, 192.168.88.10"), peer, "selected from X-Forwarded-For"},
		{"forwarded IPv6 unique local", func(b *bytes.Buffer) *ClientIPResolver { return resolver(1, b) }, headers("X-Forwarded-For", "fd00:1:2:3::4"), peer, "selected from X-Forwarded-For"},
		{"header", func(b *bytes.Buffer) *ClientIPResolver { return headerResolver(t, 1, b) }, headers("Cf-Connecting-Ip", "172.16.0.9"), "10.42.3.4:40000", "read from CLIENT_IP_HEADER"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			r := tc.r(&logs)
			for range 3 {
				if _, ok := r.Resolve(tc.h, tc.peer); !ok {
					t.Fatal("a private address must still resolve: the warning is advice, not a refusal")
				}
			}
			text := logs.String()
			if n := strings.Count(text, "level=WARN"); n != 1 || !strings.Contains(text, tc.want) {
				t.Fatalf("%d warnings, want one containing %q:\n%s", n, tc.want, text)
			}
			for _, leaked := range []string{"10.0.0.5", "192.168.88.10", "fd00", "172.16.0.9"} {
				if strings.Contains(text, leaked) {
					t.Fatalf("the address reached the log:\n%s", text)
				}
			}
		})
	}
}

func TestResolveKeepsPublicAndLoopbackAddressesQuiet(t *testing.T) {
	var logs bytes.Buffer
	resolver(0, &logs).Resolve(headers(), "127.0.0.1:1234")
	resolver(0, &logs).Resolve(headers(), "[::1]:1234")
	resolver(1, &logs).Resolve(headers("X-Forwarded-For", "203.0.113.7"), peer)
	headerResolver(t, 1, &logs).Resolve(headers("Cf-Connecting-Ip", "198.51.100.5"), "10.42.3.4:40000")
	if logs.Len() != 0 {
		t.Fatalf("public or loopback addresses logged:\n%s", logs.String())
	}
}

func TestResolveWarnsAboutAnInvalidTrustedHeaderWithoutItsValue(t *testing.T) {
	var logs bytes.Buffer
	value := "sensitive-invalid-header-value"
	headerResolver(t, 1, &logs).Resolve(headers("Cf-Connecting-Ip", value), "10.42.3.4:40000")
	text := logs.String()
	if !strings.Contains(text, "not exactly one valid IP address") || strings.Contains(text, value) {
		t.Fatalf("want one warning without the value:\n%s", text)
	}
}
