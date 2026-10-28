package linksurvey

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// The policy every response gets, which a page running custom head scripts keeps: inline scripts and any https host.
const permissivePolicy = "default-src 'self'; script-src 'self' 'unsafe-inline' https:; style-src 'self' 'unsafe-inline' https:; img-src 'self' blob: data: https:; font-src 'self' data: https:; connect-src 'self' https: wss:; frame-src 'self' https://app.cal.com https:; media-src 'self' https:; object-src 'self' data: https:; base-uri 'self'; form-action 'self'; frame-ancestors *"

// pagePolicyWith is the survey pages' own policy (DC-03) with the given script-src.
func pagePolicyWith(scriptSrc string) string {
	return "default-src 'self'; script-src " + scriptSrc + "; style-src 'self' 'unsafe-inline' https:; img-src 'self' blob: data: https:; font-src 'self' data: https:; connect-src 'self' https: wss:; frame-src 'self' https://app.cal.com https:; media-src 'self' https:; object-src 'none'; base-uri 'self'; form-action 'self'; worker-src 'self' blob:; frame-ancestors *"
}

// A survey page runs its bundle, the respondent runtime from the public domain, Cal.com's embed and, where configured, reCAPTCHA, and no inline script or other host; only a page that runs custom head scripts, which are arbitrary HTML by design, keeps the permissive policy, and only where the page runs them: not on Forma Cloud, not in a preview, not for blank scripts, not on an instance whose survey pages share the admin origin.
func TestSurveyPagePolicy(t *testing.T) {
	recaptcha := func(c *config.Config) { c.RecaptchaSiteKey, c.RecaptchaSecretKey = "site-key", "secret-key" }
	strict := pagePolicyWith("'self' http://surveys.localhost:3999 https://cal.com/embed.js https://www.google.com/recaptcha/ https://www.gstatic.com/recaptcha/")
	e := newEnv(t, recaptcha, withSurveyDomain)
	plain := e.workspace()
	scripted := e.workspace("custom_head_scripts", "<script>window.ws=1</script>")
	blank := e.workspace("custom_head_scripts", "  \n")
	cases := []struct {
		name string
		path string
		want string
	}{
		{"no custom scripts", "/s/" + e.survey(plain).String(), strict},
		{"blank workspace scripts", "/s/" + e.survey(blank).String(), strict},
		{"workspace scripts", "/s/" + e.survey(scripted).String(), permissivePolicy},
		{"workspace scripts in a preview", "/s/" + e.survey(scripted).String() + "?preview=true", strict},
		{"survey scripts behind a PIN", "/s/" + e.survey(plain, "pin", "1234", "custom_head_scripts", "<script src=\"https://cdn.example/a.js\"></script>").String(), permissivePolicy},
		{"an unknown survey", "/s/" + uuid.NewString(), strict},
		{"a paused survey with workspace scripts", "/s/" + e.survey(scripted, "status", "paused").String(), strict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := e.page(tc.path)
			if p.Status != http.StatusOK && p.Status != http.StatusNotFound {
				t.Fatalf("status %d", p.Status)
			}
			if got := p.Header.Get("Content-Security-Policy"); got != tc.want {
				t.Errorf("Content-Security-Policy =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

// Without reCAPTCHA keys no survey can turn it on, so the page does not name Google.
func TestSurveyPagePolicyWithoutRecaptcha(t *testing.T) {
	if got, want := newEnv(t).page("/s/"+uuid.NewString()).Header.Get("Content-Security-Policy"), pagePolicyWith("'self' http://localhost:3999 https://cal.com/embed.js"); got != want {
		t.Errorf("without reCAPTCHA: Content-Security-Policy =\n%q\nwant\n%q", got, want)
	}
}

// With Turnstile as the spam protection provider the page loads Turnstile's script from challenges.cloudflare.com, its challenge frame is within frame-src's https: sources, and Google is not named even when reCAPTCHA keys are configured too.
func TestSurveyPagePolicyWithTurnstile(t *testing.T) {
	e := newEnv(t, func(c *config.Config) {
		c.TurnstileSiteKey, c.TurnstileSecretKey = "site-key", "secret-key"
		c.RecaptchaSiteKey, c.RecaptchaSecretKey = "recaptcha-site", "recaptcha-secret"
	})
	got := e.page("/s/" + uuid.NewString()).Header.Get("Content-Security-Policy")
	if want := pagePolicyWith("'self' http://localhost:3999 https://cal.com/embed.js https://challenges.cloudflare.com"); got != want {
		t.Errorf("with Turnstile: Content-Security-Policy =\n%q\nwant\n%q", got, want)
	}
	if !strings.Contains(got, "frame-src 'self' https://app.cal.com https:;") {
		t.Errorf("frame-src does not allow Turnstile's challenge frame: %q", got)
	}
}

// Forma Cloud never runs custom head scripts, and a separate public domain is named as the runtime's origin.
func TestSurveyPagePolicyOnFormaCloud(t *testing.T) {
	cloud := newEnv(t, func(c *config.Config) {
		c.IsFormaCloud = true
		c.PublicURL = &url.URL{Scheme: "https", Host: "surveys.example.com"}
	})
	id := cloud.survey(cloud.workspace("custom_head_scripts", "<script>window.ws=1</script>"))
	if got, want := cloud.page("/s/"+id.String()).Header.Get("Content-Security-Policy"), pagePolicyWith("'self' https://surveys.example.com https://cal.com/embed.js"); got != want {
		t.Errorf("Forma Cloud: Content-Security-Policy =\n%q\nwant\n%q", got, want)
	}
}

// Where the survey pages share the admin origin the instance runs no custom head scripts (config.CustomScripts), so a page whose workspace stores some keeps the survey pages' own policy.
func TestSurveyPagePolicyWithoutASurveyDomain(t *testing.T) {
	e := newEnv(t)
	id := e.survey(e.workspace("custom_head_scripts", "<script>window.ws=1</script>"))
	if got, want := e.page("/s/"+id.String()).Header.Get("Content-Security-Policy"), pagePolicyWith("'self' http://localhost:3999 https://cal.com/embed.js"); got != want {
		t.Errorf("without a survey domain: Content-Security-Policy =\n%q\nwant\n%q", got, want)
	}
}
