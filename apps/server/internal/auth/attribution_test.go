package auth

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics/analyticstest"
)

// forma2's getAttributionPropertiesFromCookies: whitelisted, non-blank string values, trimmed and capped at 256 characters; anything malformed is no attribution.
func TestParseAttribution(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", 300)
	for name, tc := range map[string]struct {
		raw  string
		want analytics.Properties
	}{
		"whitelisted keys only":  {`{"utm_source":"newsletter","page":"pricing","email":"ada@example.com","auth_provider":"x"}`, analytics.Properties{"utm_source": "newsletter", "page": "pricing"}},
		"trimmed, blank dropped": {`{"utm_medium":"  cpc ","utm_term":"   "}`, analytics.Properties{"utm_medium": "cpc"}},
		"non-strings dropped":    {`{"utm_campaign":3,"entry_page":["/"]}`, analytics.Properties{}},
		"capped":                 {`{"initial_source":"` + long + `"}`, analytics.Properties{"initial_source": long[:256]}},
		"capped in UTF-16 units": {`{"utm_content":"` + strings.Repeat("a", 255) + `😀"}`, analytics.Properties{"utm_content": strings.Repeat("a", 255)}},
		"not JSON":               {`utm_source=x`, analytics.Properties{}},
		"not an object":          {`["utm_source"]`, analytics.Properties{}},
		"empty":                  {``, analytics.Properties{}},
	} {
		got := parseAttribution(tc.raw)
		if len(got) != len(tc.want) {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
			continue
		}
		for k, v := range tc.want {
			if got[k] != v {
				t.Errorf("%s: %s = %v, want %v", name, k, got[k], v)
			}
		}
	}
}

// The first-touch cookie the sign-up page wrote rides on user_signed_up, under the server's own properties, and is cleared once read; a later sign-in reads the cookie it carries then and clears it too.
func TestAttributionOnSignUpAndSignIn(t *testing.T) {
	rec := analyticstest.Start(t)
	e := newEnv(t)
	b := e.browser()
	b.cookies[attributionCookieName] = &http.Cookie{Name: attributionCookieName, Value: url.QueryEscape(`{"utm_source":"newsletter","auth_provider":"spoofed","email":"x@y.z"}`)}
	r := b.post("/auth/signup", signupBody("ada@example.com"))
	expectStatus(t, r, http.StatusOK, "")
	if c := r.cookie(attributionCookieName); c == nil || c.MaxAge >= 0 {
		t.Errorf("sign-up did not clear the attribution cookie: %v", c)
	}
	signedUp := rec.Events(t, "user_signed_up", 1)[0].Properties
	if signedUp["utm_source"] != "newsletter" || signedUp["auth_provider"] != "credentials" || signedUp["email"] != nil {
		t.Errorf("user_signed_up = %v", signedUp)
	}

	e.exec(`UPDATE users SET email_verified = true WHERE email = 'ada@example.com'`)
	in := e.browser()
	in.cookies[attributionCookieName] = &http.Cookie{Name: attributionCookieName, Value: url.QueryEscape(`{"utm_campaign":"spring"}`)}
	r = in.post("/auth/sign-in", credentials("ada@example.com", strongPassword))
	expectStatus(t, r, http.StatusOK, "")
	if c := r.cookie(attributionCookieName); c == nil || c.MaxAge >= 0 {
		t.Errorf("sign-in did not clear the attribution cookie: %v", c)
	}
	if p := rec.Events(t, "user_signed_in", 1)[0].Properties; p["utm_campaign"] != "spring" || p["auth_provider"] != "password" {
		t.Errorf("user_signed_in = %v", p)
	}

	// A refused sign-in reads nothing, so the cookie stays for the attempt that succeeds.
	bad := e.browser()
	bad.cookies[attributionCookieName] = &http.Cookie{Name: attributionCookieName, Value: url.QueryEscape(`{"utm_campaign":"spring"}`)}
	r = bad.post("/auth/sign-in", credentials("ada@example.com", "Wrong-password-1"))
	if r.Code == http.StatusOK || r.cookie(attributionCookieName) != nil {
		t.Errorf("refused sign-in: %d, cookie %v", r.Code, r.cookie(attributionCookieName))
	}
}
