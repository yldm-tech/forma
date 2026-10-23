package response

import (
	"context"
	"net/http"

	"github.com/yldm-tech/forma/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
)

// ClientMeta is the meta a client sends with a new response (ZResponseMeta, validated by the client API). Only the keys a browser can observe are read from it: the auto-captured page context, plus source, url and action. A client's country, userAgent or ipAddress is never kept; the server derives those from the request.
type ClientMeta struct {
	Source         *string  `json:"source"`
	URL            *string  `json:"url"`
	Action         *string  `json:"action"`
	PagePath       *string  `json:"pagePath"`
	PageReferrer   *string  `json:"pageReferrer"`
	UtmSource      *string  `json:"utmSource"`
	UtmMedium      *string  `json:"utmMedium"`
	UtmCampaign    *string  `json:"utmCampaign"`
	UtmTerm        *string  `json:"utmTerm"`
	UtmContent     *string  `json:"utmContent"`
	ScreenWidth    *float64 `json:"screenWidth"`
	ScreenHeight   *float64 `json:"screenHeight"`
	ViewportWidth  *float64 `json:"viewportWidth"`
	ViewportHeight *float64 `json:"viewportHeight"`
	Timezone       *string  `json:"timezone"`
	Locale         *string  `json:"locale"`
}

// UserAgent is what ua-parser-js reads off the request's User-Agent header: the browser name, the operating system name and the device type, each "" when not recognised.
type UserAgent struct {
	Browser string
	OS      string
	Device  string
}

// RequestInfo is what a new response's meta takes from the request rather than from the client's payload.
type RequestInfo struct {
	UserAgent UserAgent
	// Country is the geo-IP country the CDN put in front of the request, "" when there is none (see CountryFrom).
	Country string
}

// CountryFrom is the country forma2's routes read: Cloudflare's CF-IPCountry, else CloudFront-Viewer-Country.
func CountryFrom(h http.Header) string {
	if c := h.Get("CF-IPCountry"); c != "" {
		return c
	}
	return h.Get("CloudFront-Viewer-Country")
}

// captureMeta is the meta a new response stores, rebuilt from a whitelist as forma2's client routes rebuilt it: the auto-captured keys, source, url and action from the client; the user agent and country from the request; the client address only when the survey captures it and does not anonymize; then the anonymize policy.
func captureMeta(ctx context.Context, sv *Survey, cm ClientMeta, req RequestInfo) map[string]any {
	meta := map[string]any{}
	putString := func(key string, v *string) {
		if v != nil {
			meta[key] = *v
		}
	}
	putNumber := func(key string, v *float64) {
		if v != nil {
			meta[key] = *v
		}
	}
	putString("pagePath", cm.PagePath)
	putString("pageReferrer", cm.PageReferrer)
	putString("utmSource", cm.UtmSource)
	putString("utmMedium", cm.UtmMedium)
	putString("utmCampaign", cm.UtmCampaign)
	putString("utmTerm", cm.UtmTerm)
	putString("utmContent", cm.UtmContent)
	putNumber("screenWidth", cm.ScreenWidth)
	putNumber("screenHeight", cm.ScreenHeight)
	putNumber("viewportWidth", cm.ViewportWidth)
	putNumber("viewportHeight", cm.ViewportHeight)
	putString("timezone", cm.Timezone)
	putString("locale", cm.Locale)
	putString("source", cm.Source)
	putString("url", cm.URL)
	putString("action", cm.Action)

	userAgent := map[string]any{"device": "desktop"}
	if req.UserAgent.Browser != "" {
		userAgent["browser"] = req.UserAgent.Browser
	}
	if req.UserAgent.OS != "" {
		userAgent["os"] = req.UserAgent.OS
	}
	if req.UserAgent.Device != "" {
		userAgent["device"] = req.UserAgent.Device
	}
	meta["userAgent"] = userAgent
	if req.Country != "" {
		meta["country"] = req.Country
	}

	// The address comes only from the trusted proxy chain; an untrusted request stores the shared "untrusted-client-ip" marker.
	if sv.IsCaptureIPEnabled && !sv.IsAnonymizeResponsesEnabled {
		meta["ipAddress"] = httpapi.ClientIP(ctx)
	}
	return anonymize(meta, sv.IsAnonymizeResponsesEnabled)
}

// compositeMetaKeys maps the sub-keys of a composite meta key to the reserved fields they are.
var compositeMetaKeys = map[string]map[string]string{
	"userAgent": {"browser": "browser", "os": "os", "device": "deviceType"},
}

// anonymize is forma2's applyAnonymizePolicy: with the survey's toggle on, every meta key whose reserved field is "drop" goes (country, ipAddress and the user agent's parts, the userAgent object with them once nothing in it survives), and a "redactQuery" one keeps only its origin and path. Capture time only: nothing is filtered on read.
func anonymize(meta map[string]any, enabled bool) map[string]any {
	if !enabled {
		return meta
	}
	out := map[string]any{}
	for key, value := range meta {
		if sub, composite := compositeMetaKeys[key]; composite {
			obj, isObj := value.(map[string]any)
			if !isObj {
				continue
			}
			survivors := map[string]any{}
			for subKey, subValue := range obj {
				if v, keep := applyPrivacy(sub[subKey], subValue); keep {
					survivors[subKey] = v
				}
			}
			if len(survivors) > 0 {
				out[key] = survivors
			}
			continue
		}
		if v, keep := applyPrivacy(key, value); keep {
			out[key] = v
		}
	}
	return out
}

func applyPrivacy(field string, value any) (any, bool) {
	switch embeddeddata.ReservedPrivacy(field) {
	case embeddeddata.PrivacyDrop:
		return nil, false
	case embeddeddata.PrivacyRedactQuery:
		if s, ok := value.(string); ok {
			return embeddeddata.RedactURLQuery(s), true
		}
	}
	return value, true
}
