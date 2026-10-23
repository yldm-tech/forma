package auth

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics"
)

// The marketing attribution the sign-in and sign-up pages record, forma2's modules/auth/lib/attribution.ts: the SPA writes the first-touch fb_attribution cookie from the page's whitelisted query parameters, and the server reads it into user_signed_up and user_signed_in, then clears it so it cannot bleed onto a later user's events on a shared browser.
const attributionCookieName = "fb_attribution"

// attributionKeys are forma2's ATTRIBUTION_PARAM_KEYS, the only keys ever read from the cookie.
var attributionKeys = []string{"page", "utm_source", "utm_medium", "utm_campaign", "utm_term", "utm_content", "initial_source", "entry_page"}

// maxAttributionValueLength is forma2's MAX_ATTRIBUTION_VALUE_LENGTH, counted in UTF-16 code units as JavaScript's slice does.
const maxAttributionValueLength = 256

// parseAttribution is forma2's getAttributionPropertiesFromCookies on the cookie's decoded value: a JSON object whose whitelisted, non-blank string values are kept, trimmed and capped. Anything malformed yields no properties.
func parseAttribution(raw string) analytics.Properties {
	props := analytics.Properties{}
	var record map[string]any
	if raw == "" || json.Unmarshal([]byte(raw), &record) != nil {
		return props
	}
	for _, key := range attributionKeys {
		value, ok := record[key].(string)
		if !ok {
			continue
		}
		if value = strings.TrimSpace(value); value != "" {
			props[key] = truncateUTF16(value, maxAttributionValueLength)
		}
	}
	return props
}

// truncateUTF16 keeps the first n UTF-16 code units of s. A surrogate pair split by the cut is dropped whole rather than left as half a character.
func truncateUTF16(s string, n int) string {
	units := 0
	for i, r := range s {
		width := 1
		if r > 0xFFFF {
			width = 2
		}
		if units+width > n {
			return s[:i]
		}
		units += width
	}
	return s
}

// attribution is one request's view of the cookie: what it carried, and whether a sign-up or sign-in consumed it, after which the response clears it.
type attribution struct {
	present bool
	props   analytics.Properties
	used    bool
}

type attributionKey struct{}

// readAttribution reads the request's attribution cookie into its context, where the service finds it when it reports a sign-up or a sign-in. The returned value tells the handler whether to clear the cookie (clearAttribution).
func readAttribution(c *gin.Context) *attribution {
	a := &attribution{props: analytics.Properties{}}
	// gin percent-decodes the value, undoing the encodeURIComponent the page wrote it with.
	if raw, err := c.Cookie(attributionCookieName); err == nil {
		a.present = true
		a.props = parseAttribution(raw)
	}
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), attributionKey{}, a))
	return a
}

// attributionProperties returns a copy of the request's attribution properties without consuming them, for the SSO sign-up event, whose sign-in in the same request consumes them.
func attributionProperties(ctx context.Context) analytics.Properties {
	props := analytics.Properties{}
	if a, ok := ctx.Value(attributionKey{}).(*attribution); ok {
		for k, v := range a.props {
			props[k] = v
		}
	}
	return props
}

// consumeAttribution returns the request's attribution properties and marks the cookie to be cleared, as forma2 deleted it once a sign-up or sign-in had read it.
func consumeAttribution(ctx context.Context) analytics.Properties {
	if a, ok := ctx.Value(attributionKey{}).(*attribution); ok {
		a.used = true
	}
	return attributionProperties(ctx)
}

// clearAttribution deletes the cookie when the request carried one, even an empty or malformed one, and consumed it. It must run before the response is written.
func (s cookieSettings) clearAttribution(c *gin.Context, a *attribution) {
	if a.present && a.used {
		s.clear(c, attributionCookieName)
	}
}

// withAttribution is props with the attribution properties spread in first, so the server's own properties win on a clash, as forma2 spread them.
func withAttribution(attr, props analytics.Properties) analytics.Properties {
	out := analytics.Properties{}
	for k, v := range attr {
		out[k] = v
	}
	for k, v := range props {
		out[k] = v
	}
	return out
}
