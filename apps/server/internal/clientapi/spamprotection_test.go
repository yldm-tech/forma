package clientapi

import (
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// With Turnstile configured, a survey with spam protection on takes the first write of a response only with a turnstileToken Cloudflare accepts for the survey widget's action, verified with the address the proxy chain vouched for; the updates that carry the rest of the response need no token; Cloudflare not answering refuses the response with a 503 instead of taking it unchecked; a survey with spam protection off needs no token at all. While reCAPTCHA keys are configured too, a create from a page that still uses reCAPTCHA is checked by reCAPTCHA.
func TestCreateResponseTurnstile(t *testing.T) {
	e := newEnv(t, func(c *config.Config) {
		c.TurnstileSiteKey, c.TurnstileSecretKey = "1x00000000000000000000AA", "turnstile-secret"
		// Configured too, but Turnstile is the provider: reCAPTCHA only checks a create that carries no Turnstile token.
		c.RecaptchaSiteKey, c.RecaptchaSecretKey = "recaptcha-site", "recaptcha-secret"
	})
	ws := e.workspace()
	sv := e.survey(ws, "recaptcha", `{"enabled":true,"threshold":0.9}`)
	body := func(token any) map[string]any {
		b := map[string]any{"surveyId": sv.String(), "finished": false, "data": map[string]any{}}
		if token != nil {
			b["turnstileToken"] = token
		}
		return b
	}
	const clientIP = "203.0.113.9"
	post := func(t *testing.T, version string, b map[string]any) result {
		t.Helper()
		e.clock.nextWindow()
		return e.do("POST", respond(ws, version), b, "X-Forwarded-For", clientIP)
	}

	t.Run("missing", func(t *testing.T) {
		for _, v := range []string{"v1", "v2"} {
			for _, b := range []map[string]any{body(nil), body(""), {"surveyId": sv.String(), "finished": true, "data": map[string]any{}, "recaptchaToken": ""}} {
				details := post(t, v, b).expectError(t, 400, "bad_request", "Missing turnstile token")
				if details["code"] != codeRecaptchaFailed {
					t.Fatalf("details %v", details)
				}
			}
		}
	})

	t.Run("invalid", func(t *testing.T) {
		for _, v := range []string{"v1", "v2"} {
			details := post(t, v, body("forged-token")).expectError(t, 400, "bad_request", "Turnstile verification failed")
			if details["code"] != codeRecaptchaFailed {
				t.Fatalf("details %v", details)
			}
		}
	})

	t.Run("minted for sign-up", func(t *testing.T) {
		for _, v := range []string{"v1", "v2"} {
			post(t, v, body(turnstileSignUpToken)).expectError(t, 400, "bad_request", "Turnstile verification failed")
		}
	})

	t.Run("reCAPTCHA token Google refuses", func(t *testing.T) {
		e.recaptchaAnswers(200, `{"success":true,"score":0.5}`)
		for _, v := range []string{"v1", "v2"} {
			b := map[string]any{"surveyId": sv.String(), "finished": false, "data": map[string]any{}, "recaptchaToken": "a-recaptcha-token"}
			post(t, v, b).expectError(t, 400, "bad_request", "reCAPTCHA verification failed")
		}
	})

	t.Run("provider down", func(t *testing.T) {
		for _, v := range []string{"v1", "v2"} {
			details := post(t, v, body(turnstileDownToken)).expectError(t, 503, "service_unavailable", "Spam protection is temporarily unavailable. Please try again.")
			if details["code"] != "spam_protection_unavailable" {
				t.Fatalf("details %v", details)
			}
		}
	})

	if n := e.count(`SELECT count(*) FROM responses WHERE survey_id = ?`, sv); n != 0 {
		t.Fatalf("%d responses written without an accepted token", n)
	}

	t.Run("valid, then updates without a token", func(t *testing.T) {
		for _, v := range []string{"v1", "v2"} {
			r := post(t, v, body(turnstilePassToken))
			if r.Status != 200 {
				t.Fatalf("%s create: %d %s", v, r.Status, r.Body)
			}
			id, _ := r.data(t)["id"].(string)
			put := e.do("PUT", "/api/"+v+"/client/"+ws.String()+"/responses/"+id, map[string]any{"finished": true, "data": map[string]any{}})
			if put.Status != 200 {
				t.Fatalf("%s update without a token: %d %s", v, put.Status, put.Body)
			}
		}
		requests := e.turnstileRequests()
		last := requests[len(requests)-1]
		if last["secret"] != "turnstile-secret" || last["response"] != turnstilePassToken || last["remoteip"] != clientIP {
			t.Fatalf("siteverify request %v", last)
		}
	})

	t.Run("reCAPTCHA token from a page from before the switch", func(t *testing.T) {
		e.recaptchaAnswers(200, `{"success":true,"score":0.95}`)
		before := len(e.turnstileRequests())
		for _, v := range []string{"v1", "v2"} {
			b := map[string]any{"surveyId": sv.String(), "finished": true, "data": map[string]any{}, "recaptchaToken": "a-recaptcha-token"}
			if r := post(t, v, b); r.Status != 200 {
				t.Fatalf("%s: %d %s", v, r.Status, r.Body)
			}
		}
		if after := len(e.turnstileRequests()); after != before {
			t.Fatalf("%d siteverify requests for reCAPTCHA tokens", after-before)
		}
	})

	t.Run("spam protection off", func(t *testing.T) {
		before := len(e.turnstileRequests())
		off := e.survey(ws, "recaptcha", `{"enabled":false,"threshold":0.1}`)
		for _, v := range []string{"v1", "v2"} {
			if r := post(t, v, map[string]any{"surveyId": off.String(), "finished": true, "data": map[string]any{}}); r.Status != 200 {
				t.Fatalf("%s: %d %s", v, r.Status, r.Body)
			}
		}
		if after := len(e.turnstileRequests()); after != before {
			t.Fatalf("%d siteverify requests for a survey without spam protection", after-before)
		}
	})

	t.Run("reCAPTCHA token on an instance without reCAPTCHA keys", func(t *testing.T) {
		e := newEnv(t, func(c *config.Config) { c.TurnstileSiteKey, c.TurnstileSecretKey = "site", "secret" })
		ws := e.workspace()
		sv := e.survey(ws, "recaptcha", `{"enabled":true,"threshold":0.1}`)
		e.recaptchaAnswers(200, `{"success":true,"score":1}`)
		e.post(t, respond(ws, "v2"), map[string]any{"surveyId": sv.String(), "finished": true, "data": map[string]any{}, "recaptchaToken": "a-recaptcha-token"}).
			expectError(t, 400, "bad_request", "Missing turnstile token")
	})

	t.Run("organization without billing record", func(t *testing.T) {
		e := newEnv(t, func(c *config.Config) { c.TurnstileSiteKey, c.TurnstileSecretKey = "site", "secret" })
		ws := e.workspace()
		sv := e.survey(ws, "recaptcha", `{"enabled":true,"threshold":0.1}`)
		e.exec(`DELETE FROM organization_billing`)
		e.post(t, respond(ws, "v2"), map[string]any{"surveyId": sv.String(), "finished": true, "data": map[string]any{}, "turnstileToken": turnstilePassToken}).
			expectError(t, 404, "not_found", "Organization not found")
	})
}

// The environment state names the site key of the instance's one spam protection service: Turnstile's when it is configured (even beside reCAPTCHA keys), reCAPTCHA's otherwise.
func TestEnvironmentSpamProtectionSiteKey(t *testing.T) {
	cases := []struct {
		name              string
		configure         func(*config.Config)
		turnstile, recapt any
	}{
		{"turnstile", func(c *config.Config) { c.TurnstileSiteKey, c.TurnstileSecretKey = "ts", "tk" }, "ts", nil},
		{"both", func(c *config.Config) {
			c.TurnstileSiteKey, c.TurnstileSecretKey = "ts", "tk"
			c.RecaptchaSiteKey, c.RecaptchaSecretKey = "rs", "rk"
		}, "ts", nil},
		{"recaptcha", func(c *config.Config) { c.RecaptchaSiteKey, c.RecaptchaSecretKey = "rs", "rk" }, nil, "rs"},
		{"none", func(*config.Config) {}, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, tc.configure)
			ws := e.workspace()
			state := e.state(t, ws.String())
			if state["turnstileSiteKey"] != tc.turnstile || state["recaptchaSiteKey"] != tc.recapt {
				t.Fatalf("turnstileSiteKey %v, recaptchaSiteKey %v", state["turnstileSiteKey"], state["recaptchaSiteKey"])
			}
		})
	}
}
