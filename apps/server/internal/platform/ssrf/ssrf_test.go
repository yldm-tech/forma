package ssrf

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// fakeResolver answers lookups from a table; a missing name does not resolve, and a nil entry blocks until the context ends.
type fakeResolver map[string][]string

func (f fakeResolver) LookupNetIP(ctx context.Context, _, host string) ([]netip.Addr, error) {
	addrs, ok := f[host]
	if !ok {
		return nil, errors.New("no such host")
	}
	if addrs == nil {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	out := []netip.Addr{}
	for _, a := range addrs {
		// An answer that is not an address reads as the invalid address, which is blocked.
		addr, _ := netip.ParseAddr(a)
		out = append(out, addr)
	}
	return out, nil
}

func guard(allowInternal bool, r fakeResolver) *Guard {
	g := New(config.Config{DangerouslyAllowWebhookInternalURLs: allowInternal})
	g.resolver = r
	return g
}

func validationMessage(t *testing.T, err error) string {
	t.Helper()
	e, ok := apperr.As(err)
	if !ok || e.Status != http.StatusBadRequest {
		t.Fatalf("error = %v, want a 400", err)
	}
	return e.Message
}

func TestValidateAcceptsPublicURLs(t *testing.T) {
	t.Parallel()
	g := guard(false, fakeResolver{"example.com": {"93.184.216.34"}, "v6.example.com": {"2606:2800:220:1:248:1893:25c8:1946"}})
	for _, u := range []string{
		"https://example.com/webhook",
		"http://example.com/webhook",
		"https://example.com:8443/api/v1/webhook",
		"https://v6.example.com/webhook",
		"https://93.184.216.34/webhook",
		"https://EXAMPLE.com/webhook",
	} {
		if _, err := g.Validate(context.Background(), u); err != nil {
			t.Errorf("Validate(%q) = %v", u, err)
		}
	}
}

func TestValidateRejectsFormatAndProtocol(t *testing.T) {
	t.Parallel()
	g := guard(false, fakeResolver{})
	cases := map[string]string{
		"not-a-url":                          MessageInvalidURL,
		"":                                   MessageInvalidURL,
		"https://":                           MessageInvalidURL,
		"http://0x7g.1/":                     MessageInvalidURL,
		"http://1.2.3.4.5/":                  MessageInvalidURL,
		"http://256.1/":                      MessageInvalidURL,
		"ftp://example.com/file":             MessageProtocol,
		"file:///etc/passwd":                 MessageProtocol,
		"javascript:alert(1)":                MessageProtocol,
		"http://localhost/admin":             MessageLocalHost,
		"https://LOCALHOST/":                 MessageLocalHost,
		"https://localhost.localdomain/path": MessageLocalHost,
		"http://ip6-localhost/":              MessageLocalHost,
		"http://metadata.google.internal/computeMetadata/v1/": MessageLocalHost,
	}
	for u, want := range cases {
		_, err := g.Validate(context.Background(), u)
		if got := validationMessage(t, err); got != want {
			t.Errorf("Validate(%q) = %q, want %q", u, got, want)
		}
	}
}

// The private literals and reserved ranges of forma2's validate-webhook-url.test.ts, the alternative IPv4 notations its URL parser normalized, and the IPv4-mapped forms.
func TestValidateRejectsPrivateAddresses(t *testing.T) {
	t.Parallel()
	g := guard(false, fakeResolver{})
	for _, host := range []string{
		"127.0.0.1", "127.0.0.53", "10.0.0.1", "172.16.0.1", "172.31.255.255", "192.168.1.1", "169.254.169.254", "0.0.0.0", "100.64.0.1",
		"168.63.129.16", "[2002:7f00:1::1]", "[64:ff9b::a9fe:a9fe]", "[64:ff9b::7f00:1]", "[64:ff9b:1::1]", "[ff02::1]", "[fec0::1]",
		"[fe9f::1]", "[febf::1]", "225.0.0.1", "239.255.255.250", "241.0.0.1", "[ff05::1]", "[ff0e::1]",
		"[::7f00:1]", "[::127.0.0.1]", "[::a9fe:a9fe]", "[::a00:1]", "[::ffff:0:7f00:1]", "[::ffff:0:a9fe:a9fe]", "[::ffff:0:a00:1]",
		"[2001:0:1234::1]", "[2001:db8::1]", "[100::1]", "[::]", "[::1]", "0.1.2.3", "255.255.255.255",
		"0x7f000001", "2130706433", "0177.0.0.1", "127.1", "127.0.0.1.",
		"[::ffff:127.0.0.1]", "[::ffff:7f00:1]", "[::FFFF:7F00:1]", "[::ffff:169.254.169.254]", "[::ffff:10.0.0.1]", "[::ffff:100.64.0.1]",
		"[fe80::1%25eth0]",
	} {
		_, err := g.Validate(context.Background(), "http://"+host+"/")
		if got := validationMessage(t, err); got != MessagePrivateAddress {
			t.Errorf("Validate(%s) = %q", host, got)
		}
	}
}

func TestValidateAcceptsBoundaryAddresses(t *testing.T) {
	t.Parallel()
	g := guard(false, fakeResolver{})
	for _, host := range []string{
		"100.128.0.1", "100.129.0.1", "100.130.0.1", "100.63.255.255", "172.32.0.1", "192.0.3.1", "198.20.0.1", "11.0.0.1", "223.255.255.255",
		"[2003::1]", "[64:ff9c::1]", "[fe7f::1]", "[2606:2800:220:1:248:1893:25c8:1946]", "[2001:4860:4860::8888]", "[2001:db9::1]", "[101::1]",
		"[::ffff:93.184.216.34]",
	} {
		if _, err := g.Validate(context.Background(), "https://"+host+"/webhook"); err != nil {
			t.Errorf("Validate(%s) = %v", host, err)
		}
	}
}

func TestValidateChecksEveryResolvedAddress(t *testing.T) {
	t.Parallel()
	g := guard(false, fakeResolver{
		"loopback.example": {"127.0.0.1"},
		"mixed.example":    {"93.184.216.34", "10.0.0.1"},
		"mixed6.example":   {"2606:2800:220:1:248:1893:25c8:1946", "fd00::1"},
		"sneaky.example":   {"::ffff:10.0.0.1"},
		"garbage.example":  {"not-an-ip"},
		"slow.example":     nil,
	})
	cases := map[string]string{
		"https://loopback.example/": MessagePrivateAddress,
		"https://mixed.example/":    MessagePrivateAddress,
		"https://mixed6.example/":   MessagePrivateAddress,
		"https://sneaky.example/":   MessagePrivateAddress,
		"https://garbage.example/":  MessagePrivateAddress,
		"https://nowhere.example/":  "Could not resolve webhook URL hostname: nowhere.example",
	}
	for u, want := range cases {
		_, err := g.Validate(context.Background(), u)
		if got := validationMessage(t, err); got != want {
			t.Errorf("Validate(%q) = %q, want %q", u, got, want)
		}
	}
	start := time.Now()
	_, err := g.Validate(context.Background(), "https://slow.example/")
	if got := validationMessage(t, err); got != "DNS resolution timed out for webhook URL hostname: slow.example" {
		t.Errorf("slow lookup = %q", got)
	}
	if elapsed := time.Since(start); elapsed < dnsTimeout || elapsed > dnsTimeout+2*time.Second {
		t.Errorf("slow lookup took %v", elapsed)
	}
}

func TestValidatePinsTheFirstAddress(t *testing.T) {
	t.Parallel()
	g := guard(false, fakeResolver{"multi.example": {"93.184.216.34", "93.184.216.35"}, "v6only.example": {"2606:4700::1111"}})
	cases := map[string]string{
		"https://multi.example/":          "93.184.216.34",
		"https://v6only.example/":         "2606:4700::1111",
		"https://[2001:4860:4860::8888]/": "2001:4860:4860::8888",
		"https://93.184.216.34/":          "93.184.216.34",
	}
	for u, want := range cases {
		target, err := g.Validate(context.Background(), u)
		if err != nil {
			t.Fatalf("Validate(%q) = %v", u, err)
		}
		if target.Addr.String() != want {
			t.Errorf("Validate(%q) pinned %s, want %s", u, target.Addr, want)
		}
	}
}

// DANGEROUSLY_ALLOW_WEBHOOK_INTERNAL_URLS lets private and local targets through, leaves a local name unpinned so the system resolver answers it, and still refuses a name that does not resolve.
func TestValidateWithInternalURLsAllowed(t *testing.T) {
	t.Parallel()
	g := guard(true, fakeResolver{"internal.example": {"10.0.0.5"}})
	for u, want := range map[string]string{
		"http://127.0.0.1/hook":         "127.0.0.1",
		"http://[::1]/hook":             "::1",
		"http://internal.example/hook":  "10.0.0.5",
		"http://169.254.169.254/latest": "169.254.169.254",
	} {
		target, err := g.Validate(context.Background(), u)
		if err != nil {
			t.Fatalf("Validate(%q) = %v", u, err)
		}
		if target.Addr.String() != want {
			t.Errorf("Validate(%q) pinned %s, want %s", u, target.Addr, want)
		}
	}
	target, err := g.Validate(context.Background(), "http://localhost:8080/hook")
	if err != nil || target.Addr.IsValid() {
		t.Fatalf("localhost = %+v, %v; want an unpinned target", target, err)
	}
	_, err = g.Validate(context.Background(), "http://nowhere.example/")
	if got := validationMessage(t, err); got != "Could not resolve webhook URL hostname: nowhere.example" {
		t.Errorf("unresolvable = %q", got)
	}
	_, err = g.Validate(context.Background(), "ftp://example.com/")
	if got := validationMessage(t, err); got != MessageProtocol {
		t.Errorf("ftp = %q", got)
	}
}

// receiver is an HTTP server on loopback that the tests reach through a public-looking name: the guard's resolver answers a public address, and its dialer is routed to the listener, so the connection itself proves which address was dialled.
func receiver(t *testing.T, handler http.HandlerFunc) (g *Guard, dialled *atomic.Value) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	g = guard(false, fakeResolver{"hooks.example": {"93.184.216.34"}})
	dialled = &atomic.Value{}
	listener := strings.TrimPrefix(srv.URL, "http://")
	g.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialled.Store(address)
		return (&net.Dialer{}).DialContext(ctx, network, listener)
	}
	return g, dialled
}

// The connection goes to the address that was validated, even when the name resolves elsewhere by the time the request is made.
func TestPostDialsThePinnedAddress(t *testing.T) {
	t.Parallel()
	var got http.Header
	g, dialled := receiver(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	})
	target, err := g.Validate(context.Background(), "http://hooks.example:8080/hook")
	if err != nil {
		t.Fatal(err)
	}
	g.resolver = fakeResolver{"hooks.example": {"10.0.0.1"}}
	status, err := g.Post(context.Background(), target, http.Header{"Content-Type": {"application/json"}}, []byte(`{}`), time.Second)
	if err != nil || status != http.StatusNoContent {
		t.Fatalf("Post = %d, %v", status, err)
	}
	if dialled.Load() != "93.184.216.34:8080" {
		t.Errorf("dialled %v, want the pinned address", dialled.Load())
	}
	if got.Get("Content-Type") != "application/json" {
		t.Errorf("headers = %v", got)
	}
}

func TestPostReturnsRedirectsUnfollowed(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	g, _ := receiver(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusTemporaryRedirect)
	})
	target, err := g.Validate(context.Background(), "http://hooks.example/hook")
	if err != nil {
		t.Fatal(err)
	}
	status, err := g.Post(context.Background(), target, nil, nil, time.Second)
	if err != nil || status != http.StatusTemporaryRedirect || hits.Load() != 1 {
		t.Fatalf("Post = %d, %v after %d hits", status, err, hits.Load())
	}
}

func TestPostTimesOut(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	g, _ := receiver(t, func(http.ResponseWriter, *http.Request) { <-release })
	// Registered after the server's Close, so it runs first and lets the handler return.
	t.Cleanup(func() { close(release) })
	target, err := g.Validate(context.Background(), "http://hooks.example/hook")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Post(context.Background(), target, nil, nil, 200*time.Millisecond); !errors.Is(err, ErrTimeout) {
		t.Fatalf("Post = %v, want ErrTimeout", err)
	}
}

// The dialer refuses a blocked address even when one reaches it, for instance through a redirect the internal-URL switch did not allow.
func TestDialerRefusesBlockedAddresses(t *testing.T) {
	t.Parallel()
	if err := refuseBlocked("tcp", "127.0.0.1:80", nil); err == nil {
		t.Error("loopback was not refused")
	}
	if err := refuseBlocked("tcp6", "[::ffff:10.0.0.1]:443", nil); err == nil {
		t.Error("mapped private address was not refused")
	}
	if err := refuseBlocked("tcp", "93.184.216.34:443", nil); err != nil {
		t.Errorf("public address refused: %v", err)
	}
	// The production dialer carries the check: a live loopback listener is never reached, and the refusal is the guard's own, not the operating system's.
	var reached atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached.Add(1) }))
	defer srv.Close()
	g := New(config.Config{})
	target := &Target{URL: mustURL(t, srv.URL+"/hook"), Addr: netip.MustParseAddr("127.0.0.1")}
	if _, err := g.Post(context.Background(), target, nil, nil, time.Second); err == nil || !strings.Contains(err.Error(), "webhook connection to a private or internal address refused") {
		t.Fatalf("Post to loopback = %v, want the dialer's refusal", err)
	}
	if reached.Load() != 0 {
		t.Fatal("the loopback listener was reached")
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
