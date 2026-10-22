package linksurvey

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
	"github.com/yldm-tech/forma/server/internal/survey"
)

// forma2's renderSurvey gates and metadata 404s: a survey the page cannot serve is the not-found page.
func TestSurveyPageNotFound(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	draft := e.survey(ws, "status", "draft")
	app := e.survey(ws, "type", "app")
	archived := e.survey(ws, "status", "paused", "archived_at", time.Now())
	for name, target := range map[string]string{
		"unknown uuid":      "/s/" + uuid.NewString(),
		"unknown forma2 id": "/s/clunknownsurveyaaaaaaaaa1",
		"garbage":           "/s/%3Cscript%3E",
		"draft":             "/s/" + draft.String(),
		"app survey":        "/s/" + app.String(),
		"archived":          "/s/" + archived.String(),
	} {
		t.Run(name, func(t *testing.T) {
			p := e.page(target)
			if p.Status != http.StatusNotFound || p.view() != viewNotFound || p.Title != "Forma" || p.meta("robots") != "noindex" {
				t.Fatalf("status %d view %q title %q robots %q", p.Status, p.view(), p.Title, p.meta("robots"))
			}
			if strings.Contains(p.Body, "Link Feedback Survey") {
				t.Fatal("the survey's name reached a not-found page")
			}
		})
	}
}

func TestSurveyPageInactive(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace("link_survey_branding", "false")
	paused := e.survey(ws, "status", "paused")
	scheduled := e.survey(ws, "status", "paused", "publish_on", time.Now().Add(24*time.Hour))
	completed := e.survey(ws, "status", "completed", "survey_closed_message", `{"heading":"Thanks","subheading":"We are done"}`)
	cases := []struct {
		id          uuid.UUID
		status      string
		isScheduled bool
		closed      any
	}{
		{paused, "paused", false, nil},
		{scheduled, "paused", true, nil},
		{completed, "completed", false, map[string]any{"heading": "Thanks", "subheading": "We are done"}},
	}
	for _, tc := range cases {
		p := e.page("/s/" + tc.id.String())
		in := p.sub("inactive")
		if p.Status != http.StatusOK || p.view() != viewInactive || in["status"] != tc.status || in["isScheduled"] != tc.isScheduled {
			t.Fatalf("%s: status %d view %q inactive %v", tc.id, p.Status, p.view(), in)
		}
		if closed, _ := json.Marshal(in["surveyClosedMessage"]); string(closed) != mustJSON(tc.closed) {
			t.Fatalf("closed message %s", closed)
		}
		if ws := p.sub("inactive", "workspace"); ws["linkSurveyBranding"] != false {
			t.Fatalf("workspace %v", ws)
		}
		if p.State["survey"] != nil || strings.Contains(p.Body, "What do you think?") {
			t.Fatal("an inactive page carries the survey")
		}
		// An inactive survey still has its full metadata.
		if p.Title != "Link Feedback Survey | Forma" || p.link("canonical") != "http://localhost:3999/s/"+tc.id.String() {
			t.Fatalf("head %q %q", p.Title, p.link("canonical"))
		}
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestSurveyPageRendersTheSurvey(t *testing.T) {
	e := newEnv(t, func(c *config.Config) {
		c.RecaptchaSiteKey, c.RecaptchaSecretKey = "site-key", "secret-key"
		c.ImprintURL, c.PrivacyURL, c.TermsURL = "https://example.com/imprint", "https://example.com/privacy", "https://example.com/terms"
	})
	ws := e.workspace("custom_head_scripts", "<script>window.ws=1</script>", "logo", `{"url":"/storage/ws/public/logo.png"}`)
	segment := uuid.New()
	e.exec(`INSERT INTO segments (id, workspace_id, title, filters) VALUES (?, ?, 'secret targeting', '[{"id":"f1","connector":null,"resource":{"id":"r","root":{"type":"attribute","contactAttributeKey":"email"},"value":"ceo@example.com","qualifier":{"operator":"equals"}}}]')`, segment, ws)
	id := e.survey(ws, "pin", nil, "segment_id", segment, "recaptcha", `{"enabled":true,"threshold":0.5}`,
		"custom_head_scripts", "<script>window.survey=1</script>", "custom_head_scripts_mode", "replace",
		"welcome_card", `{"enabled":true,"showResponseCount":true,"headline":{"default":"Welcome"}}`)
	e.exec(`INSERT INTO survey_follow_ups (survey_id, name, trigger, action) VALUES (?, 'f', '{"type":"response","properties":null}', '{"type":"send-email","properties":{"to":"boss@example.com","subject":"s","body":"b","from":"noreply@example.com","replyTo":[]}}')`, id)
	e.response(id, true)
	e.response(id, false)

	p := e.page("/s/"+id.String()+"?embed=true&preview=true&lang=nope", "Accept-Language", "ja-JP")
	if p.Status != http.StatusOK || p.view() != viewSurvey {
		t.Fatalf("status %d view %q", p.Status, p.view())
	}
	s := p.sub("survey")
	if s["isEmbed"] != true || s["isPreview"] != true || s["languageCode"] != "default" || s["locale"] != "ja-JP" || p.Lang != "ja-JP" {
		t.Fatalf("flags %v", s)
	}
	if s["recaptchaSiteKey"] != "site-key" || s["isSpamProtectionEnabled"] != true || s["isFormaCloud"] != false {
		t.Fatalf("recaptcha %v", s)
	}
	if s["imprintUrl"] != "https://example.com/imprint" || s["privacyUrl"] != "https://example.com/privacy" || s["termsUrl"] != "https://example.com/terms" || s["publicDomain"] != "http://localhost:3999" {
		t.Fatalf("legal %v", s)
	}
	if s["responseCount"] != float64(2) {
		t.Fatalf("response count %v", s["responseCount"])
	}
	w := p.sub("survey", "workspace")
	if w["customHeadScripts"] != "<script>window.ws=1</script>" || w["linkSurveyBranding"] != true || p.sub("survey", "workspace", "logo")["url"] != "/storage/ws/public/logo.png" {
		t.Fatalf("workspace %v", w)
	}
	sv := p.sub("survey", "survey")
	if sv["id"] != id.String() || sv["customHeadScripts"] != "<script>window.survey=1</script>" || sv["customHeadScriptsMode"] != "replace" {
		t.Fatalf("survey %v", sv)
	}
	// What the public page must never carry.
	if sv["pin"] != nil || len(sv["followUps"].([]any)) != 0 || strings.Contains(p.Body, "boss@example.com") || strings.Contains(p.Body, "ceo@example.com") || strings.Contains(p.Body, "secret targeting") {
		t.Fatalf("secrets reached the page: %s", p.Body)
	}
	if seg := p.sub("survey", "survey", "segment"); seg["id"] != segment.String() || seg["hasFilters"] != false || len(seg) != 2 {
		t.Fatalf("segment %v", seg)
	}
	if fields, ok := sv["embeddedFields"].([]any); !ok || fields == nil {
		t.Fatalf("embedded fields %v", sv["embeddedFields"])
	}
	// The runtime's script is in the document, so it loads alongside the page bundle.
	if !strings.Contains(p.Body, `<script async src="http://localhost:3999/js/surveys.umd.js"></script>`) {
		t.Fatal("the survey runtime is not in the document")
	}
}

// The welcome card's response count is fetched only when the card shows it, and then served from the cache for a minute.
func TestSurveyPageResponseCount(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	hidden := e.survey(ws, "welcome_card", `{"enabled":true,"showResponseCount":false}`)
	disabled := e.survey(ws, "welcome_card", `{"enabled":false,"showResponseCount":true}`)
	shown := e.survey(ws, "welcome_card", `{"enabled":true,"showResponseCount":true}`)
	e.response(shown, true)
	for _, id := range []uuid.UUID{hidden, disabled} {
		if c := e.page("/s/" + id.String()).sub("survey")["responseCount"]; c != nil {
			t.Fatalf("count %v fetched for a card that does not show it", c)
		}
	}
	if c := e.page("/s/" + shown.String()).sub("survey")["responseCount"]; c != float64(1) {
		t.Fatalf("count %v", c)
	}
	e.response(shown, false)
	if c := e.page("/s/" + shown.String()).sub("survey")["responseCount"]; c != float64(1) {
		t.Fatalf("count %v was not served from the cache", c)
	}
	if err := e.cache.Del(t.Context(), "fb:response:"+shown.String()+":count").Err(); err != nil {
		t.Fatal(err)
	}
	if c := e.page("/s/" + shown.String()).sub("survey")["responseCount"]; c != float64(2) {
		t.Fatalf("count %v after the cache expired", c)
	}
}

// forma2's computeStyling and getMetadataBrandColor: the survey's styling only when the workspace allows overrides and the survey overrides the theme.
func TestSurveyPageStylingAndBrandColor(t *testing.T) {
	e := newEnv(t)
	allow := e.workspace("styling", `{"allowStyleOverwrite":true,"brandColor":{"light":"#ff0000"}}`)
	deny := e.workspace("styling", `{"allowStyleOverwrite":false,"brandColor":{"light":"#ff0000"}}`)
	surveyStyling := `{"overwriteThemeStyling":true,"brandColor":{"light":"#0000ff"}}`
	cases := []struct {
		ws      uuid.UUID
		styling any
		color   string
	}{
		{allow, surveyStyling, "#0000ff"},
		{allow, `{"overwriteThemeStyling":false,"brandColor":{"light":"#0000ff"}}`, "#ff0000"},
		{deny, surveyStyling, "#ff0000"},
		{allow, nil, "#ff0000"},
	}
	for _, tc := range cases {
		id := e.survey(tc.ws, "styling", tc.styling)
		p := e.page("/s/" + id.String())
		if got := p.sub("survey", "styling", "brandColor")["light"]; got != tc.color {
			t.Errorf("styling brand colour %v, want %s", got, tc.color)
		}
		if want := "http://localhost:3999/api/v1/client/og?brandColor=" + url.QueryEscape(tc.color) + "&name=Link%20Feedback%20Survey"; p.meta("og:image") != want {
			t.Errorf("og:image %q, want %q", p.meta("og:image"), want)
		}
	}
}

// forma2's getBasicSurveyMetadata, getSurveyOpenGraphMetadata and the page's own additions.
func TestSurveyPageMetadata(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	e.exec(`UPDATE organizations SET whitelabel = '{"faviconUrl":"/storage/x/public/favicon.ico"}' WHERE id = ?`, e.organizationOf(ws))
	custom := e.survey(ws, "metadata", `{"title":{"default":"Custom title","de-DE":"Eigener Titel"},"description":{"default":"Custom description"},"ogImage":"https://cdn.example.com/og.png"}`)
	e.language(ws, custom, "en-US", "", true, true)
	e.language(ws, custom, "de-DE", "deutsch", false, true)
	welcome := e.survey(ws, "welcome_card", `{"enabled":true,"headline":{"default":"<p>Hello <b>#recall:feedback/fallback:x# and #recall:utm_source/fallback:y# and #recall:utmSource/fallback:z#</b></p>"}}`,
		"hidden_fields", `{"enabled":true,"fieldIds":["utm_source"]}`)

	p := e.page("/s/" + custom.String())
	if p.Title != "Custom title | Forma" || p.meta("description") != "Custom description" || p.meta("og:title") != "Custom title" || p.meta("twitter:description") != "Custom description" {
		t.Fatalf("head %q %v", p.Title, p.Metas)
	}
	if p.meta("og:image") != "https://cdn.example.com/og.png" || p.meta("twitter:image") != "https://cdn.example.com/og.png" {
		t.Fatalf("custom OG image %q", p.meta("og:image"))
	}
	if p.link("icon") != "http://localhost:3999/storage/x/public/favicon.ico" {
		t.Fatalf("favicon %q", p.link("icon"))
	}
	if p.meta("robots") != "noindex, follow" || p.meta("googlebot") != "noindex, follow, noimageindex" || p.meta("viewport") != linkViewport {
		t.Fatalf("robots %q googlebot %q viewport %q", p.meta("robots"), p.meta("googlebot"), p.meta("viewport"))
	}
	for lang, title := range map[string]string{"de-DE": "Eigener Titel", "deutsch": "Eigener Titel", "DE-de": "Eigener Titel", "de": "Custom title"} {
		if got := e.page("/s/" + custom.String() + "?lang=" + lang).Title; got != title+" | Forma" {
			t.Errorf("lang=%s: title %q", lang, got)
		}
	}
	// The welcome card's headline as text, recall tokens shown as their labels.
	if got := e.page("/s/" + welcome.String()).Title; got != "Hello @What do you think? and @utm_source and @Utm Source | Forma" {
		t.Fatalf("welcome title %q", got)
	}
}

func TestSurveyPageFormaCloudTitle(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.IsFormaCloud = true })
	ws := e.workspace()
	derived := e.survey(ws)
	authored := e.survey(ws, "metadata", `{"title":{"default":"Authored"}}`)
	if got := e.page("/s/" + derived.String()).Title; got != "Link Feedback Survey | Forma | Forma" {
		t.Fatalf("derived title %q", got)
	}
	if got := e.page("/s/" + authored.String()).Title; got != "Authored | Forma" {
		t.Fatalf("authored title %q", got)
	}
}

// A migrated survey keeps its forma2 links: the forma2 id resolves to it.
func TestSurveyPageByForma2ID(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	e.surveyWithID(legacyid.ID(forma2Survey), ws)
	p := e.page("/s/" + forma2Survey)
	if p.Status != http.StatusOK || p.view() != viewSurvey || p.sub("survey", "survey")["id"] != legacyid.ID(forma2Survey).String() {
		t.Fatalf("status %d view %q", p.Status, p.view())
	}
}

func TestSurveyPageLanguage(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	id := e.survey(ws, "is_verify_email_enabled", true)
	e.language(ws, id, "en", "", true, true)
	e.language(ws, id, "de", "", false, true)
	e.language(ws, id, "he", "", false, true)
	e.language(ws, id, "fr", "", false, false)
	cases := []struct {
		lang, accept, languageCode, locale string
	}{
		{"de-DE", "ja-JP", "de", "de-DE"},
		{"", "ja-JP", "default", "ja-JP"},
		{"he", "ja-JP", "he", "ja-JP"},
		{"it", "ja-JP", "default", "en-US"},
		// A disabled language renders the default one.
		{"fr", "ja-JP", "default", "en-US"},
	}
	for _, tc := range cases {
		p := e.page("/s/"+id.String()+"?lang="+tc.lang, "Accept-Language", tc.accept)
		v := p.sub("verifyEmail")
		if v["languageCode"] != tc.languageCode || v["locale"] != tc.locale || p.State["locale"] != "ja-JP" {
			t.Errorf("lang=%q: languageCode %v gate locale %v page locale %v", tc.lang, v["languageCode"], v["locale"], p.State["locale"])
		}
	}
}

// forma2's checkAndValidateSingleUseId and the single-use response lookup.
func TestSurveyPageSingleUse(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace("link_survey_branding", "false")
	plain := e.survey(ws, "single_use", `{"enabled":true,"isEncrypted":false,"heading":"Already done","subheading":"Once only"}`)
	encrypted := e.survey(ws, "single_use", `{"enabled":true,"isEncrypted":true}`)
	key := string(e.cfg.EncryptionKeyText)
	token := survey.SingleUseSignature(key, plain.String(), "su-1")

	invalid := []string{
		"/s/" + plain.String(),
		"/s/" + plain.String() + "?suId=su-1",
		"/s/" + plain.String() + "?suId=su-1&suToken=" + strings.Repeat("0", 64),
		"/s/" + plain.String() + "?suId=%20&suToken=" + token,
		"/s/" + encrypted.String() + "?suId=not-sealed",
	}
	for _, target := range invalid {
		p := e.page(target)
		if p.Status != http.StatusOK || p.view() != viewInactive || p.sub("inactive")["status"] != inactiveLinkInvalid || p.sub("inactive", "workspace")["linkSurveyBranding"] != false {
			t.Fatalf("%s: status %d view %q inactive %v", target, p.Status, p.view(), p.sub("inactive"))
		}
	}

	p := e.page("/s/" + plain.String() + "?suId=su-1&suToken=" + token)
	if p.view() != viewSurvey || p.sub("survey")["singleUseId"] != "su-1" || p.sub("survey")["singleUseResponseId"] != nil {
		t.Fatalf("valid link: view %q survey %v", p.view(), p.sub("survey"))
	}
	partial := e.response(plain, false, "single_use_id", "su-1")
	p = e.page("/s/" + plain.String() + "?suId=su-1&suToken=" + token)
	if p.view() != viewSurvey || p.sub("survey")["singleUseResponseId"] != partial.String() {
		t.Fatalf("unfinished response: view %q survey %v", p.view(), p.sub("survey"))
	}
	e.exec(`UPDATE responses SET finished = true WHERE id = ?`, partial)
	p = e.page("/s/" + plain.String() + "?suId=su-1&suToken=" + token)
	if p.view() != viewCompleted || p.sub("completed", "singleUse")["heading"] != "Already done" || p.sub("completed", "workspace")["linkSurveyBranding"] != false {
		t.Fatalf("finished response: view %q completed %v", p.view(), p.sub("completed"))
	}

	parts := strings.Split(forma2EncryptedSingleUseID(t), ":")
	p = e.page("/s/" + encrypted.String() + "?suId=" + url.QueryEscape(strings.Join(parts, ":")))
	if p.view() != viewSurvey || p.sub("survey")["singleUseId"] != "clsingleuseaaaaaaaaaaaaa1" {
		t.Fatalf("forma2 encrypted id: view %q survey %v", p.view(), p.sub("survey"))
	}
}

func forma2EncryptedSingleUseID(t *testing.T) string {
	sealed, err := secret.EncryptForma2("clsingleuseaaaaaaaaaaaaa1", bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

// forma2's verify-email gate: no token asks for the address, a bad one is fishy, a good one passes with the address as a hidden field, and preview skips it.
func TestSurveyPageVerifyEmail(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	id := e.survey(ws, "is_verify_email_enabled", true)
	token, err := newEmailToken(e.cfg, id, "user1@example.com", e.now)
	if err != nil {
		t.Fatal(err)
	}
	p := e.page("/s/" + id.String() + "?suId=su-1&suToken=tok")
	v := p.sub("verifyEmail")
	if p.view() != viewVerifyEmail || v["fishy"] != false || v["singleUseId"] != "su-1" || v["singleUseToken"] != "tok" {
		t.Fatalf("no token: view %q %v", p.view(), v)
	}
	if blocks := p.sub("verifyEmail", "survey")["blocks"].([]any); len(blocks) != 1 {
		t.Fatalf("the curious preview lost its questions: %v", blocks)
	}
	p = e.page("/s/" + id.String() + "?verify=not-a-token")
	if p.view() != viewVerifyEmail || p.sub("verifyEmail")["fishy"] != true {
		t.Fatalf("bad token: view %q %v", p.view(), p.sub("verifyEmail"))
	}
	other, _ := newEmailToken(e.cfg, uuid.New(), "user1@example.com", e.now)
	if p = e.page("/s/" + id.String() + "?verify=" + other); p.sub("verifyEmail")["fishy"] != true {
		t.Fatal("another survey's token passed")
	}
	p = e.page("/s/" + id.String() + "?verify=" + token)
	if p.view() != viewSurvey || p.sub("survey")["verifiedEmail"] != "user1@example.com" {
		t.Fatalf("good token: view %q %v", p.view(), p.sub("survey"))
	}
	if p = e.page("/s/" + id.String() + "?preview=true"); p.view() != viewSurvey || p.sub("survey")["verifiedEmail"] != nil {
		t.Fatalf("preview: view %q", p.view())
	}
	if p = e.page("/s/" + id.String() + "?preview=TRUE"); p.view() != viewVerifyEmail {
		t.Fatalf("preview is exactly \"true\": view %q", p.view())
	}
}

// The PIN never leaves the server, and neither does the survey behind it.
func TestSurveyPagePin(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	id := e.survey(ws, "pin", "1234")
	p := e.page("/s/"+id.String()+"?lang=de", "Accept-Language", "ja")
	s := p.sub("survey")
	if p.view() != viewPin || s["survey"] != nil || s["surveyId"] != id.String() || s["locale"] != "ja-JP" {
		t.Fatalf("view %q survey %v", p.view(), s)
	}
	// The PIN would appear as a JSON string; ids may contain its digits.
	if strings.Contains(p.Body, `"1234"`) || strings.Contains(p.Body, `"pin":`) || strings.Contains(p.Body, "What do you think?") || strings.Contains(p.Body, "surveys.umd.js") {
		t.Fatalf("the PIN gate leaks: %s", p.Body)
	}
	verified := e.survey(ws, "pin", "1234", "is_verify_email_enabled", true)
	p = e.page("/s/" + verified.String())
	if p.view() != viewVerifyEmail || len(p.sub("verifyEmail", "survey")["blocks"].([]any)) != 0 || strings.Contains(p.Body, "What do you think?") {
		t.Fatalf("verify gate in front of a PIN shows the questions: view %q", p.view())
	}
}

// forma2's canReadUserIdFromUrl: a non-empty userId parameter, in any case, on a page without a contact.
func TestSurveyPageUserIDFromURL(t *testing.T) {
	e := newEnv(t)
	id := e.survey(e.workspace())
	for query, want := range map[string]bool{"": false, "?userId=u1": true, "?USERID=u1": true, "?userid=": false, "?userId=&userid=u2": false, "?other=1&UserId=u3": true} {
		if got := e.page("/s/" + id.String() + query).sub("survey")["canReadUserIdFromUrl"]; got != want {
			t.Errorf("%q: %v, want %v", query, got, want)
		}
	}
}

func contactToken(t *testing.T, cfg config.Config, contactID, surveyID string, expiry *time.Time) string {
	t.Helper()
	c, err := secret.EncryptForma2(contactID, cfg.EncryptionKey)
	if err != nil {
		t.Fatal(err)
	}
	s, err := secret.EncryptForma2(surveyID, cfg.EncryptionKey)
	if err != nil {
		t.Fatal(err)
	}
	type claims struct {
		contactClaims
		jwt.Claims
	}
	cl := claims{contactClaims: contactClaims{ContactID: c, SurveyID: s}}
	if expiry != nil {
		cl.Expiry = jwt.NewNumericDate(*expiry)
	}
	token, err := sign(string(cfg.EncryptionKeyText), cl)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// forma2's ContactSurveyPage.
func TestContactPage(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace("link_survey_branding", "false")
	id := e.survey(ws)
	contact := e.contact(ws)
	token := contactToken(t, e.cfg, contact.String(), id.String(), nil)

	p := e.page("/c/"+token, "Accept-Language", "zh-TW")
	if p.Status != http.StatusOK || p.view() != viewSurvey || p.sub("survey")["contactId"] != contact.String() || p.State["locale"] != "zh-Hant-TW" {
		t.Fatalf("status %d view %q survey %v", p.Status, p.view(), p.sub("survey"))
	}
	// The personal page has the survey's metadata without the canonical link or robots directives, and Next's default viewport.
	if p.Title != "Link Feedback Survey | Forma" || p.link("canonical") != "" || p.meta("robots") != "" || p.meta("viewport") != rootViewport {
		t.Fatalf("head %q %v %v", p.Title, p.Metas, p.Links)
	}
	// The contact comes from the token, never the URL.
	if p = e.page("/c/" + token + "?userId=someone"); p.sub("survey")["canReadUserIdFromUrl"] != false {
		t.Fatal("a personal link read the user id from the URL")
	}

	past := e.now.Add(-time.Hour)
	expired := contactToken(t, e.cfg, contact.String(), id.String(), &past)
	p = e.page("/c/" + expired)
	if p.view() != viewInactive || p.sub("inactive")["status"] != inactiveLinkExpired || p.sub("inactive")["workspace"] != nil || p.Title != "Survey | Forma" {
		t.Fatalf("expired: view %q inactive %v title %q", p.view(), p.sub("inactive"), p.Title)
	}
	p = e.page("/c/not-a-token")
	if p.view() != viewInactive || p.sub("inactive")["status"] != inactiveLinkInvalid || p.sub("inactive")["workspace"] != nil || p.meta("description") != defaultDescription {
		t.Fatalf("invalid: view %q inactive %v", p.view(), p.sub("inactive"))
	}

	e.response(id, false, "contact_id", contact)
	p = e.page("/c/" + token)
	if p.view() != viewInactive || p.sub("inactive")["status"] != inactiveResponseSubmitted || p.sub("inactive", "workspace")["linkSurveyBranding"] != false {
		t.Fatalf("answered: view %q inactive %v", p.view(), p.sub("inactive"))
	}

	draft := e.survey(ws, "status", "draft")
	if p = e.page("/c/" + contactToken(t, e.cfg, contact.String(), draft.String(), nil)); p.Status != http.StatusNotFound || p.meta("viewport") != rootViewport {
		t.Fatalf("draft: %d", p.Status)
	}
	if p = e.page("/c/" + contactToken(t, e.cfg, contact.String(), uuid.NewString(), nil)); p.Status != http.StatusNotFound {
		t.Fatalf("missing survey: %d", p.Status)
	}
	// A personal link forma2 issued names the contact and survey by their forma2 ids.
	e.surveyWithID(legacyid.ID(forma2Survey), ws)
	e.exec(`INSERT INTO contacts (id, workspace_id) VALUES (?, ?)`, legacyid.ID(forma2Contact), ws)
	if p = e.page("/c/" + forma2ContactToken); p.view() != viewSurvey || p.sub("survey")["contactId"] != legacyid.ID(forma2Contact).String() {
		t.Fatalf("forma2 link: view %q survey %v", p.view(), p.sub("survey"))
	}
}

func TestPrettyURL(t *testing.T) {
	e := newEnv(t)
	id := e.survey(e.workspace(), "slug", "feedback")
	cases := map[string]string{
		"/p/feedback":                            "/s/" + id.String(),
		"/p/feedback?lang=de&suId=a%20b":         "/s/" + id.String() + "?lang=de&suId=a+b",
		"/p/feedback?tag=a&tag=b&preview=true":   "/s/" + id.String() + "?tag=a%2Cb&preview=true",
		"/p/feedback?q=%E2%9C%93&x=%2F%3F%26%3D": "/s/" + id.String() + "?q=%E2%9C%93&x=%2F%3F%26%3D",
	}
	for target, want := range cases {
		rec := e.request(http.MethodGet, target, nil)
		if rec.Code != http.StatusTemporaryRedirect || rec.Header().Get("Location") != want {
			t.Errorf("%s: %d to %q, want %q", target, rec.Code, rec.Header().Get("Location"), want)
		}
	}
	p := e.page("/p/nothing")
	if p.Status != http.StatusNotFound || p.view() != viewNotFound || p.Header.Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Fatalf("unknown slug: %d %q", p.Status, p.view())
	}
}

// Pretty URLs are a self-hosted feature.
func TestPrettyURLOnFormaCloud(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.IsFormaCloud = true })
	e.survey(e.workspace(), "slug", "feedback")
	if p := e.page("/p/feedback"); p.Status != http.StatusNotFound {
		t.Fatalf("status %d", p.Status)
	}
}

func decodeBody(t *testing.T, body []byte, dst any) {
	t.Helper()
	if err := json.Unmarshal(body, dst); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
}

// forma2's validateSurveyPinAction, limited to the surveys the page puts behind a PIN.
func TestValidatePin(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	id := e.survey(ws, "pin", "1234")
	path := "/s/" + id.String() + "/pin"

	rec := e.request(http.MethodPost, path, map[string]any{"pin": "1235"})
	var fail struct{ Code, Message string }
	decodeBody(t, rec.Body.Bytes(), &fail)
	if rec.Code != http.StatusBadRequest || fail.Code != "INVALID_PIN" {
		t.Fatalf("wrong PIN: %d %s", rec.Code, rec.Body)
	}
	rec = e.request(http.MethodPost, path, map[string]any{"pin": "1234"})
	var ok pinResult
	decodeBody(t, rec.Body.Bytes(), &ok)
	if rec.Code != http.StatusOK || !VerifyPinToken(e.cfg, ok.PinAuthToken, id, e.now) {
		t.Fatalf("right PIN: %d %s", rec.Code, rec.Body)
	}
	var sv map[string]any
	decodeBody(t, ok.Survey, &sv)
	if sv["id"] != id.String() || sv["pin"] != nil || len(sv["blocks"].([]any)) != 1 {
		t.Fatalf("survey %v", sv)
	}
	if rec = e.request(http.MethodPost, path, map[string]any{}); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing PIN: %d", rec.Code)
	}

	// Only a live, PIN-protected link survey answers: never a draft, an app survey or a survey without a PIN.
	for _, other := range []uuid.UUID{e.survey(ws), e.survey(ws, "pin", "1234", "status", "draft"), e.survey(ws, "pin", "1234", "type", "app"), e.survey(ws, "pin", "1234", "status", "paused"), uuid.New()} {
		if rec = e.request(http.MethodPost, "/s/"+other.String()+"/pin", map[string]any{"pin": "1234"}); rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "What do you think") {
			t.Fatalf("%s: %d %s", other, rec.Code, rec.Body)
		}
	}

	// A cross-site post is refused.
	req := e.request(http.MethodPost, path, map[string]any{"pin": "1234"}, "Origin", "https://evil.example")
	if req.Code != http.StatusForbidden {
		t.Fatalf("cross-site: %d", req.Code)
	}
}

// forma2's action:validate-survey-pin limit: 10 attempts a minute per client address.
func TestValidatePinRateLimit(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.RateLimitingDisabled = false })
	id := e.survey(e.workspace(), "pin", "1234")
	for i := range 10 {
		if rec := e.request(http.MethodPost, "/s/"+id.String()+"/pin", map[string]any{"pin": "0000"}); rec.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d: %d", i+1, rec.Code)
		}
	}
	if rec := e.request(http.MethodPost, "/s/"+id.String()+"/pin", map[string]any{"pin": "1234"}); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("eleventh attempt: %d", rec.Code)
	}
}

// jaSubject is emails.verified_link_survey_email_subject in ja-JP.
const jaSubject = "フォームへの回答準備ができました。"

// forma2's sendLinkSurveyEmailAction and buildVerifiedLinkSurveyUrl.
func TestSendVerificationEmail(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	e.exec(`UPDATE organizations SET whitelabel = '{"logoUrl":"/storage/x/public/logo.png"}' WHERE id = ?`, e.organizationOf(ws))
	id := e.survey(ws, "is_verify_email_enabled", true, "name", "Customer <b>Feedback</b>")
	e.language(ws, id, "en-US", "", true, true)
	e.language(ws, id, "de-DE", "", false, true)
	path := "/s/" + id.String() + "/verify-email"

	rec := e.request(http.MethodPost, path, map[string]any{"email": "user1@example.com", "suId": "su-1", "suToken": "tok", "locale": "ja-JP", "surveyLanguageCode": "de", "surveyName": "Injected"})
	if rec.Code != http.StatusOK {
		t.Fatalf("send: %d %s", rec.Code, rec.Body)
	}
	msgs := e.mail.Messages()
	if len(msgs) != 1 || msgs[0].To != "user1@example.com" || msgs[0].Subject != jaSubject || !strings.Contains(msgs[0].HTML, `<html lang="ja-JP">`) {
		t.Fatalf("mail %+v", msgs)
	}
	html := msgs[0].HTML
	if strings.Contains(html, "Injected") || !strings.Contains(html, "Customer &lt;b&gt;Feedback&lt;/b&gt;") || !strings.Contains(html, `src="http://localhost:3999/storage/x/public/logo.png"`) {
		t.Fatalf("mail body:\n%s", html)
	}
	start := strings.Index(html, `href="http://localhost:3999/s/`)
	link, err := url.Parse(strings.ReplaceAll(html[start+6:start+6+strings.Index(html[start+6:], `"`)], "&amp;", "&"))
	if err != nil {
		t.Fatal(err)
	}
	q := link.Query()
	if link.Path != "/s/"+id.String() || q.Get("suId") != "su-1" || q.Get("suToken") != "tok" || q.Get("lang") != "de-DE" {
		t.Fatalf("link %s", link)
	}
	if got := VerifyEmailToken(e.cfg, q.Get("verify"), id, id.String(), e.now); got != "user1@example.com" {
		t.Fatalf("the link's token proves %q", got)
	}
	if p := e.page(link.RequestURI()); p.view() != viewSurvey || p.sub("survey")["verifiedEmail"] != "user1@example.com" {
		t.Fatalf("following the link: view %q", p.view())
	}

	// Without a language choice, or with one the survey does not offer, the link stays on the default language; suToken travels only with suId.
	e.request(http.MethodPost, path, map[string]any{"email": "user2@example.com", "suToken": "tok", "locale": "en-US", "surveyLanguageCode": "it"})
	last := e.mail.Messages()[1].HTML
	if strings.Contains(last, "&amp;lang=") || strings.Contains(last, "suToken=") || !strings.Contains(last, "?verify=") {
		t.Fatalf("link carries more than it should:\n%s", last)
	}

	bad := []map[string]any{
		{"email": "a@example.com, b@example.com", "locale": "en-US"},
		{"email": "<a@example.com>", "locale": "en-US"},
		{"email": "not-an-email", "locale": "en-US"},
		{"email": "a@example.com", "locale": "xx-XX"},
		{"email": "a@example.com"},
	}
	for _, body := range bad {
		if rec := e.request(http.MethodPost, path, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: %d", body, rec.Code)
		}
	}
	disabled := e.survey(ws)
	rec = e.request(http.MethodPost, "/s/"+disabled.String()+"/verify-email", map[string]any{"email": "a@example.com", "locale": "en-US"})
	var fail struct{ Code string }
	decodeBody(t, rec.Body.Bytes(), &fail)
	if rec.Code != http.StatusBadRequest || fail.Code != "EMAIL_VERIFICATION_NOT_ENABLED" {
		t.Fatalf("not enabled: %d %s", rec.Code, rec.Body)
	}
	if rec = e.request(http.MethodPost, "/s/"+uuid.NewString()+"/verify-email", map[string]any{"email": "a@example.com", "locale": "en-US"}); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown survey: %d", rec.Code)
	}
	if len(e.mail.Messages()) != 2 {
		t.Fatalf("%d mails sent", len(e.mail.Messages()))
	}
}

// forma2's action:send-link-survey-email limit: 10 an hour per client address.
func TestSendVerificationEmailRateLimit(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.RateLimitingDisabled = false })
	id := e.survey(e.workspace(), "is_verify_email_enabled", true)
	for i := range 10 {
		if rec := e.request(http.MethodPost, "/s/"+id.String()+"/verify-email", map[string]any{"email": "a@example.com", "locale": "en-US"}); rec.Code != http.StatusOK {
			t.Fatalf("mail %d: %d", i+1, rec.Code)
		}
	}
	if rec := e.request(http.MethodPost, "/s/"+id.String()+"/verify-email", map[string]any{"email": "a@example.com", "locale": "en-US"}); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("eleventh mail: %d", rec.Code)
	}
}

// The OG route shares the client API's per-address budget (api:client, 100 a minute) and answers forma2's 429 body.
func TestOgImageRateLimit(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.RateLimitingDisabled = false })
	for i := range 100 {
		if rec := e.request(http.MethodGet, "/api/v1/client/og?name=x", nil); rec.Code != http.StatusOK {
			t.Fatalf("request %d: %d", i+1, rec.Code)
		}
	}
	rec := e.request(http.MethodGet, "/api/v1/client/og?name=x", nil)
	if rec.Code != http.StatusTooManyRequests || rec.Body.String() != `{"code":"too_many_requests","message":"Maximum number of requests reached. Please try again later.","details":{}}` {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("Access-Control-Allow-Origin") != "*" || rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Retry-After") != "" {
		t.Fatalf("headers %v", rec.Header())
	}
}

// The state is JSON inside a script element; no value can close it.
func TestPageStateCannotBreakOutOfItsScript(t *testing.T) {
	e := newEnv(t)
	id := e.survey(e.workspace(), "name", "</script><script>alert(1)</script>")
	p := e.page("/s/" + id.String())
	if strings.Contains(p.Body, "<script>alert(1)") {
		t.Fatalf("injected markup:\n%s", p.Body)
	}
	if p.sub("survey", "survey")["name"] != "</script><script>alert(1)</script>" || p.Title != "</script><script>alert(1)</script> | Forma" {
		t.Fatalf("name %v title %q", p.sub("survey", "survey")["name"], p.Title)
	}
}
