package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// UntrustedClientIP is the shared identity of every request whose address could not be established. All such requests share one rate-limit bucket, which is the price of never believing an address the proxy chain did not vouch for.
const UntrustedClientIP = "untrusted-client-ip"

// ipv6SubnetBits collapses an IPv6 client to its /64: one subscriber usually controls a whole /64, so rate limits keyed on the full address would be trivially sidestepped.
const ipv6SubnetBits = 64

const clientIPWarningInterval = 10 * time.Minute

type clientIPWarning string

const (
	warnHopCountZero       clientIPWarning = "hop-count-zero"
	warnMissingChain       clientIPWarning = "missing-chain"
	warnShortChain         clientIPWarning = "short-chain"
	warnInvalidSelectedHop clientIPWarning = "invalid-selected-hop"
	warnInvalidHeader      clientIPWarning = "invalid-client-ip-header"
	warnPrivatePeer        clientIPWarning = "private-peer-address"
	warnPrivateHop         clientIPWarning = "private-forwarded-address"
	warnPrivateHeader      clientIPWarning = "private-header-address"
)

// The messages never include the header values or the resolved address themselves: a forwarded value is caller-controlled and may be anything.
var clientIPWarningMessages = map[clientIPWarning]string{
	warnHopCountZero:       "Forwarding headers are present but TRUSTED_PROXY_HOP_COUNT is 0, so they are ignored and the TCP peer address identifies the client. If a proxy sits in front of the server, every request is now attributed to it; set TRUSTED_PROXY_HOP_COUNT to the number of proxies.",
	warnMissingChain:       "Client IP resolution failed because X-Forwarded-For is absent. Configure the proxy to append it; x-real-ip and cf-connecting-ip are not trusted client identity sources unless CLIENT_IP_HEADER names the header and the proxy's address is in TRUSTED_PROXY_CIDRS.",
	warnShortChain:         "Client IP resolution failed because X-Forwarded-For has fewer entries than TRUSTED_PROXY_HOP_COUNT. Verify that every configured proxy appends to the forwarding chain.",
	warnInvalidSelectedHop: "Client IP resolution failed because the selected trusted X-Forwarded-For hop is not a valid supported IP address. The request will use the shared untrusted identity.",
	warnInvalidHeader:      "Client IP resolution failed because the CLIENT_IP_HEADER header sent by a proxy in TRUSTED_PROXY_CIDRS is not exactly one valid IP address. The request will use the shared untrusted identity.",
	warnPrivatePeer:        "The client address is the TCP peer and it is a private network address, so it probably belongs to a proxy rather than the client, and every request through that proxy shares one rate-limit bucket. If a proxy sits in front of the server, set TRUSTED_PROXY_HOP_COUNT to the number of proxies that append to X-Forwarded-For, or CLIENT_IP_HEADER and TRUSTED_PROXY_CIDRS when the proxy passes the client address in a header such as CF-Connecting-IP.",
	warnPrivateHop:         "The client address selected from X-Forwarded-For is a private network address, so it probably belongs to a proxy rather than the client, and every request through that proxy shares one rate-limit bucket. Either more proxies than TRUSTED_PROXY_HOP_COUNT append to the chain, or the client address only arrives in a header such as CF-Connecting-IP: then set CLIENT_IP_HEADER and TRUSTED_PROXY_CIDRS.",
	warnPrivateHeader:      "The client address read from CLIENT_IP_HEADER is a private network address. Verify that the proxy in TRUSTED_PROXY_CIDRS sets that header to the address of the client rather than of another proxy.",
}

// clientIPSource is where Resolve found the client address.
type clientIPSource int

const (
	sourcePeer clientIPSource = iota
	sourceForwardedFor
	sourceHeader
)

var privateAddressWarnings = map[clientIPSource]clientIPWarning{
	sourcePeer:         warnPrivatePeer,
	sourceForwardedFor: warnPrivateHop,
	sourceHeader:       warnPrivateHeader,
}

var (
	validDecimalPort = regexp.MustCompile(`^[1-9][0-9]{0,4}$`)
	bracketedAddress = regexp.MustCompile(`^\[([^\[\]]+)\](?::([^:]+))?$`)
	ipv4Socket       = regexp.MustCompile(`^([^:]+):([0-9]+)$`)
)

// ClientIPResolver establishes the client address from the trusted end of the X-Forwarded-For chain, the way forma2's proxy did, and never falls back to a header the proxy chain does not control. Optionally (CLIENT_IP_HEADER with TRUSTED_PROXY_CIDRS) it reads a single-address header such as CF-Connecting-IP, but only from a TCP peer inside the trusted proxy networks.
type ClientIPResolver struct {
	hops int
	// header is the canonical name of CLIENT_IP_HEADER, empty when the trusted-header mode is off; proxies are the TCP peers it is read from.
	header  string
	proxies config.ProxyCIDRs
	log     *slog.Logger
	now     func() time.Time

	mu       sync.Mutex
	lastWarn map[clientIPWarning]time.Time
}

func NewClientIPResolver(cfg config.Config, log *slog.Logger) *ClientIPResolver {
	r := &ClientIPResolver{hops: cfg.TrustedProxyHopCount, log: log, now: time.Now, lastWarn: map[clientIPWarning]time.Time{}}
	if cfg.ClientIPHeader != "" && len(cfg.TrustedProxyCIDRs) > 0 {
		r.header, r.proxies = http.CanonicalHeaderKey(cfg.ClientIPHeader), cfg.TrustedProxyCIDRs
	}
	return r
}

// Resolve returns the canonical client address, or false when it cannot be trusted.
//
// In the trusted-header mode, a request whose TCP peer is inside the trusted proxy networks and that carries the header takes the client address from it: the header must hold exactly one valid address, or the request fails closed. Any other request, including one from an untrusted peer that sends the header, is resolved as below.
//
// With hops > 0 the client is the entry hops places from the right of X-Forwarded-For; entries further left are whatever the caller sent and are ignored. A missing, short or malformed chain fails closed rather than clamping to the leftmost entry or reading x-real-ip or cf-connecting-ip. With hops == 0 there is no proxy to vouch for anything, so the TCP peer is the client.
//
// A private network address as the result is logged (at most once per interval and source), because it usually means the address is a proxy's and every client behind that proxy shares one rate-limit bucket.
func (r *ClientIPResolver) Resolve(h http.Header, remoteAddr string) (string, bool) {
	ip, source, ok := r.resolve(h, remoteAddr)
	if ok && isPrivateAddress(ip) {
		r.warn(privateAddressWarnings[source])
	}
	return ip, ok
}

func (r *ClientIPResolver) resolve(h http.Header, remoteAddr string) (string, clientIPSource, bool) {
	if r.header != "" && r.fromTrustedProxy(remoteAddr) {
		if values := h.Values(r.header); len(values) > 0 {
			ip, ok := "", false
			if len(values) == 1 {
				ip, ok = CanonicalIP(strings.TrimSpace(values[0]))
			}
			if !ok {
				r.warn(warnInvalidHeader)
			}
			return ip, sourceHeader, ok
		}
	}

	if r.hops <= 0 {
		if hasForwardingHeader(h) {
			r.warn(warnHopCountZero)
		}
		host, _, err := net.SplitHostPort(remoteAddr)
		if err != nil {
			return "", sourcePeer, false
		}
		ip, ok := CanonicalIP(host)
		return ip, sourcePeer, ok
	}

	values := h.Values("X-Forwarded-For")
	if len(values) == 0 {
		if hasForwardingHeader(h) {
			r.warn(warnMissingChain)
		}
		return "", sourceForwardedFor, false
	}
	entries := strings.Split(strings.Join(values, ","), ",")
	if len(entries) < r.hops {
		r.warn(warnShortChain)
		return "", sourceForwardedFor, false
	}
	ip, ok := canonicalForwardedIP(strings.TrimSpace(entries[len(entries)-r.hops]))
	if !ok {
		r.warn(warnInvalidSelectedHop)
	}
	return ip, sourceForwardedFor, ok
}

// fromTrustedProxy reports whether the TCP peer is inside the trusted proxy networks.
func (r *ClientIPResolver) fromTrustedProxy(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return r.proxies.Contains(addr.WithZone(""))
}

// isPrivateAddress reports whether a canonical address from CanonicalIP is in a private network (RFC 1918, RFC 4193). Loopback is not counted: a development server is reached over it with no proxy at all.
func isPrivateAddress(ip string) bool {
	addr, err := netip.ParseAddr(ip)
	return err == nil && addr.IsPrivate()
}

// Middleware resolves the address once per request and stores it for ClientIP.
func (r *ClientIPResolver) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip, ok := r.Resolve(c.Request.Header, c.Request.RemoteAddr)
		if !ok {
			ip = UntrustedClientIP
		}
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), clientIPKey{}, ip))
		c.Next()
	}
}

type clientIPKey struct{}

// ClientIP returns the canonical client address established by ClientIPResolver.Middleware, or UntrustedClientIP.
func ClientIP(ctx context.Context) string {
	if ip, ok := ctx.Value(clientIPKey{}).(string); ok {
		return ip
	}
	return UntrustedClientIP
}

// warn logs each failure reason at most once per interval, so a misconfigured proxy does not log once per request.
func (r *ClientIPResolver) warn(reason clientIPWarning) {
	r.mu.Lock()
	now := r.now()
	last, seen := r.lastWarn[reason]
	if seen && now.Sub(last) < clientIPWarningInterval {
		r.mu.Unlock()
		return
	}
	r.lastWarn[reason] = now
	r.mu.Unlock()
	r.log.Warn(clientIPWarningMessages[reason], "reason", string(reason))
}

func hasForwardingHeader(h http.Header) bool {
	for _, name := range []string{"X-Forwarded-For", "X-Real-Ip", "Cf-Connecting-Ip"} {
		if _, ok := h[name]; ok {
			return true
		}
	}
	return false
}

// CanonicalIP validates a bare address and returns its canonical form: IPv4 in dotted quad (an IPv4-mapped IPv6 address included), IPv6 masked to its /64 and written out in full ("2001:0db8:abcd:0012:0000:0000:0000:0000"). Zoned addresses are rejected.
func CanonicalIP(s string) (string, bool) {
	addr, err := netip.ParseAddr(s)
	if err != nil || addr.Zone() != "" {
		return "", false
	}
	addr = addr.Unmap()
	if addr.Is4() {
		return addr.String(), true
	}
	prefix, err := addr.Prefix(ipv6SubnetBits)
	if err != nil {
		return "", false
	}
	b := prefix.Addr().As16()
	groups := make([]string, 8)
	for i := range groups {
		groups[i] = fmt.Sprintf("%02x%02x", b[2*i], b[2*i+1])
	}
	return strings.Join(groups, ":"), true
}

// canonicalForwardedIP parses one X-Forwarded-For entry without guessing at ambiguous socket forms. A gateway may append an IPv4 port ("203.0.113.7:443"); IPv6 with a port must be bracketed ("[2001:db8::1]:443"); a valid unbracketed IPv6 value is always an address, never address-plus-port.
func canonicalForwardedIP(value string) (string, bool) {
	if ip, ok := CanonicalIP(value); ok {
		return ip, true
	}
	if m := bracketedAddress.FindStringSubmatch(value); m != nil {
		address, port := m[1], m[2]
		if !strings.Contains(address, ":") || (port != "" && !validPort(port)) {
			return "", false
		}
		return CanonicalIP(address)
	}
	if m := ipv4Socket.FindStringSubmatch(value); m != nil {
		if !validPort(m[2]) {
			return "", false
		}
		ip, ok := CanonicalIP(m[1])
		if !ok || strings.Contains(ip, ":") {
			return "", false
		}
		return ip, true
	}
	return "", false
}

func validPort(port string) bool {
	if !validDecimalPort.MatchString(port) {
		return false
	}
	n, err := strconv.Atoi(port)
	return err == nil && n <= 65535
}
