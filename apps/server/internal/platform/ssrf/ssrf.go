// Package ssrf sends requests to URLs that users supply (webhooks) without letting them reach the server's own network: forma2's validateAndResolveWebhookUrl and createPinnedDispatcher (apps/web/lib/utils/validate-webhook-url.ts). A URL must be http or https and must not name a local host; every address its host resolves to must be public; the connection is pinned to the address that was checked, so DNS cannot answer differently between the check and the request; the dialer refuses a blocked address again at connect time; and redirects are not followed.
//
// DANGEROUSLY_ALLOW_WEBHOOK_INTERNAL_URLS turns the address checks off and follows redirects, as in forma2, for self-hosted receivers on the internal network. A host name that does not resolve is refused either way.
package ssrf

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// Messages of the validation failures, forma2's InvalidInputError texts.
const (
	MessageInvalidURL     = "Invalid webhook URL format"
	MessageProtocol       = "Webhook URL must use HTTPS or HTTP protocol"
	MessageLocalHost      = "Webhook URL must not point to localhost or internal services"
	MessagePrivateAddress = "Webhook URL must not point to private or internal IP addresses"
)

// dnsTimeout bounds the lookup of a URL's host, forma2's DNS_TIMEOUT_MS.
const dnsTimeout = 3 * time.Second

// blockedHostnames are names that always mean this machine or the cloud metadata service.
var blockedHostnames = map[string]bool{
	"localhost":                true,
	"localhost.localdomain":    true,
	"ip6-localhost":            true,
	"ip6-loopback":             true,
	"metadata.google.internal": true,
}

// blockedPrefixes are the private, reserved and otherwise non-routable ranges forma2 refused, plus the Azure WireServer address, which lies outside link-local.
var blockedPrefixes = mustPrefixes(
	"0.0.0.0/8",      // "this" network
	"10.0.0.0/8",     // RFC 1918 private
	"100.64.0.0/10",  // shared address space, CGNAT
	"127.0.0.0/8",    // loopback
	"169.254.0.0/16", // link-local, cloud instance metadata
	"172.16.0.0/12",  // RFC 1918 private
	"192.0.0.0/24",   // IETF protocol assignments
	"192.0.2.0/24",   // TEST-NET-1
	"192.168.0.0/16", // RFC 1918 private
	"198.18.0.0/15",  // benchmarking
	"198.51.100.0/24",
	"203.0.113.0/24",
	"224.0.0.0/4", // multicast
	"240.0.0.0/4", // reserved, broadcast included
	"168.63.129.16/32",
	"::/96",           // IPv4-compatible, :: and ::1 included
	"::ffff:0:0:0/96", // IPv4-translated
	"64:ff9b::/96",    // NAT64
	"64:ff9b:1::/48",  // NAT64 local use
	"100::/64",        // discard only
	"2001::/32",       // Teredo
	"2001:db8::/32",   // documentation
	"2002::/16",       // 6to4
	"fc00::/7",        // unique local
	"fe80::/10",       // link-local
	"fec0::/10",       // site-local
	"ff00::/8",        // multicast
)

func mustPrefixes(cidrs ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(cidrs))
	for i, c := range cidrs {
		out[i] = netip.MustParsePrefix(c)
	}
	return out
}

// Blocked reports whether addr must never be a webhook target. An IPv4-mapped IPv6 address is judged as the IPv4 address it carries, and an invalid address is blocked.
func Blocked(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	addr = addr.WithZone("").Unmap()
	for _, p := range blockedPrefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// Resolver looks up a host's addresses; net.Resolver is the production one.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Guard validates user-supplied URLs and sends requests to them.
type Guard struct {
	allowInternal bool
	resolver      Resolver
	// dial connects to the pinned address; tests route it to a local listener.
	dial func(ctx context.Context, network, address string) (net.Conn, error)
}

func New(cfg config.Config) *Guard {
	g := &Guard{allowInternal: cfg.DangerouslyAllowWebhookInternalURLs, resolver: net.DefaultResolver}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if !g.allowInternal {
		dialer.Control = refuseBlocked
	}
	g.dial = dialer.DialContext
	return g
}

// WithResolver makes the guard look host names up with r: a test seam for the suites of the modules that validate URLs, which must not depend on the network's DNS.
func (g *Guard) WithResolver(r Resolver) *Guard {
	g.resolver = r
	return g
}

// refuseBlocked is the dial-time check: whatever address the connection is about to reach, a blocked one is refused.
func refuseBlocked(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil || Blocked(ap.Addr()) {
		return errors.New("webhook connection to a private or internal address refused")
	}
	return nil
}

// AllowsInternal reports whether DANGEROUSLY_ALLOW_WEBHOOK_INTERNAL_URLS is set.
func (g *Guard) AllowsInternal() bool { return g.allowInternal }

// Target is a validated URL and the address its connections are pinned to. Addr is invalid only for a local host name allowed by DANGEROUSLY_ALLOW_WEBHOOK_INTERNAL_URLS, which is resolved when connecting, as forma2 left it to /etc/hosts.
type Target struct {
	URL  *url.URL
	Addr netip.Addr
}

func invalid(message string) error { return apperr.BadRequest(message) }

// Validate is forma2's validateAndResolveWebhookUrl. Every failure is a 400 with forma2's message.
func (g *Guard) Validate(ctx context.Context, raw string) (*Target, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return nil, invalid(MessageInvalidURL)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return nil, invalid(MessageProtocol)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || u.Opaque != "" {
		return nil, invalid(MessageInvalidURL)
	}
	// The WHATWG URL parser forma2 ran reads a host that ends in a number as an IPv4 address in any of its legacy notations (0x7f000001, 2130706433, 0177.0.0.1, 127.1).
	if v4, isNumeric, ok := parseLegacyIPv4(host); isNumeric && !strings.Contains(host, ":") {
		if !ok {
			return nil, invalid(MessageInvalidURL)
		}
		host = v4.String()
	}
	localName := blockedHostnames[host]
	if localName && !g.allowInternal {
		return nil, invalid(MessageLocalHost)
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if !g.allowInternal && Blocked(addr) {
			return nil, invalid(MessagePrivateAddress)
		}
		return &Target{URL: u, Addr: addr.Unmap()}, nil
	}
	if localName {
		return &Target{URL: u}, nil
	}
	lookupCtx, cancel := context.WithTimeout(ctx, dnsTimeout)
	defer cancel()
	addrs, err := g.resolver.LookupNetIP(lookupCtx, "ip", host)
	if err != nil || len(addrs) == 0 {
		if errors.Is(lookupCtx.Err(), context.DeadlineExceeded) {
			return nil, invalid("DNS resolution timed out for webhook URL hostname: " + host)
		}
		return nil, invalid("Could not resolve webhook URL hostname: " + host)
	}
	if !g.allowInternal {
		for _, a := range addrs {
			if Blocked(a) {
				return nil, invalid(MessagePrivateAddress)
			}
		}
	}
	return &Target{URL: u, Addr: addrs[0].Unmap()}, nil
}

// ErrTimeout is a request that got no answer within its timeout.
var ErrTimeout = errors.New("request timed out")

// Post sends body to the target and returns the response status; the body of the answer is never read. A redirect is returned as its 3xx status, unless DANGEROUSLY_ALLOW_WEBHOOK_INTERNAL_URLS is set, which follows it. The request is cancelled after timeout, which is reported as ErrTimeout.
func (g *Guard) Post(ctx context.Context, t *Target, header http.Header, body []byte, timeout time.Duration) (int, error) {
	return g.Do(ctx, http.MethodPost, t, header, body, timeout)
}

// Do is Post with any method; a nil body sends none.
func (g *Guard) Do(ctx context.Context, method string, t *Target, header http.Header, body []byte, timeout time.Duration) (int, error) {
	status, _, err := g.Exchange(ctx, method, t, header, body, timeout, 0)
	return status, err
}

// Exchange is Do that also returns the first limit bytes of the answer's body, for a caller that records why a receiver refused a request; a limit of 0 reads none. The excerpt is what arrived before the timeout, cut anywhere, even inside a character.
func (g *Guard) Exchange(ctx context.Context, method string, t *Target, header http.Header, body []byte, timeout time.Duration, limit int64) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var reader io.Reader = http.NoBody
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, t.URL.String(), reader)
	if err != nil {
		return 0, nil, fmt.Errorf("build webhook request: %w", err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	transport := &http.Transport{
		// Never through a proxy from the environment: the proxy would resolve the host itself, outside the pin.
		Proxy:               nil,
		DialContext:         g.pinnedDial(t),
		TLSHandshakeTimeout: timeout,
		DisableKeepAlives:   true,
		ForceAttemptHTTP2:   true,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	if !g.allowInternal {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	res, err := client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return 0, nil, ErrTimeout
		}
		// The client's error names the URL, whose path and query may carry the receiver's token.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return 0, nil, urlErr.Err
		}
		return 0, nil, err
	}
	var excerpt []byte
	if limit > 0 {
		// A body cut short by the timeout or the connection still says what arrived.
		excerpt, _ = io.ReadAll(io.LimitReader(res.Body, limit))
	}
	_ = res.Body.Close()
	return res.StatusCode, excerpt, nil
}

// pinnedDial connects a request for the target's host to the address Validate checked, whatever the name resolves to now. Other hosts, reached only through a redirect the internal-URL switch allows, are dialled as named.
func (g *Guard) pinnedDial(t *Target) func(ctx context.Context, network, address string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if t.Addr.IsValid() && strings.EqualFold(host, t.URL.Hostname()) {
			address = net.JoinHostPort(t.Addr.String(), port)
		}
		return g.dial(ctx, network, address)
	}
}

// parseLegacyIPv4 is the WHATWG URL standard's IPv4 parser: a host whose last label is a number is an IPv4 address of up to four dot-separated parts, each decimal, octal with a leading 0 or hexadecimal with 0x, the last part filling the bytes the others leave. isNumeric reports whether the host ends in a number at all; ok whether it then parsed.
func parseLegacyIPv4(host string) (addr netip.Addr, isNumeric, ok bool) {
	parts := strings.Split(host, ".")
	if parts[len(parts)-1] == "" && len(parts) > 1 {
		parts = parts[:len(parts)-1]
	}
	last := parts[len(parts)-1]
	if _, numeric := parseIPv4Number(last); !numeric && !isDigits(last) {
		return netip.Addr{}, false, false
	}
	if len(parts) > 4 {
		return netip.Addr{}, true, false
	}
	numbers := make([]uint64, len(parts))
	for i, p := range parts {
		n, valid := parseIPv4Number(p)
		if !valid {
			return netip.Addr{}, true, false
		}
		numbers[i] = n
	}
	for _, n := range numbers[:len(numbers)-1] {
		if n > 255 {
			return netip.Addr{}, true, false
		}
	}
	if numbers[len(numbers)-1] >= 1<<(8*(5-len(numbers))) {
		return netip.Addr{}, true, false
	}
	v := numbers[len(numbers)-1]
	for i, n := range numbers[:len(numbers)-1] {
		v += n << (8 * (3 - i))
	}
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(v)) //nolint:gosec // v < 2^32: the last part was bounded above and the others are at most 255
	return netip.AddrFrom4(b), true, true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// parseIPv4Number is the WHATWG IPv4 number parser for one part.
func parseIPv4Number(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	base := 10
	switch {
	case len(s) >= 2 && (s[:2] == "0x" || s[:2] == "0X"):
		s, base = s[2:], 16
	case len(s) >= 2 && s[0] == '0':
		s, base = s[1:], 8
	}
	if s == "" {
		return 0, true
	}
	n, err := strconv.ParseUint(s, base, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
