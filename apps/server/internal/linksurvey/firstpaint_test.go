package linksurvey

import (
	"strings"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// paint is the first paint a page carries, or "" when it has none; style reports whether its stylesheet is in the head.
func (p renderedPage) paint() (paint string, style bool) {
	head, body, _ := strings.Cut(p.Body, "</head>")
	style = strings.Contains(head, firstPaintStyle)
	start := strings.Index(body, `<div id="`+firstPaintElementID+`">`)
	if start < 0 {
		return "", style
	}
	end := strings.Index(body[start:], `<div id="root">`)
	return body[start : start+end], style
}

// A survey view paints the survey's title before either bundle runs, in the language the survey opens in, escaped like every other value the page carries.
func TestSurveyPagePaintsTheTitleFirst(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	named := e.survey(ws, "name", `Q4 <NPS> & "more"`)
	titled := e.survey(ws, "metadata", `{"title":{"default":"Custom title","de-DE":"Eigener Titel"}}`)
	e.language(ws, titled, "en-US", "", true, true)
	e.language(ws, titled, "de-DE", "", false, true)
	welcome := e.survey(ws, "welcome_card", `{"enabled":true,"headline":{"default":"<p>Welcome to our <b>survey</b></p>"}}`)

	for target, want := range map[string]string{
		"/s/" + named.String():                  "Q4 &lt;NPS&gt; &amp; &#34;more&#34;",
		"/s/" + titled.String():                 "Custom title",
		"/s/" + titled.String() + "?lang=de-DE": "Eigener Titel",
		"/s/" + welcome.String():                "Welcome to our survey",
	} {
		p := e.page(target)
		if p.view() != viewSurvey {
			t.Fatalf("%s: view %q", target, p.view())
		}
		paint, style := p.paint()
		if paint != `<div id="forma-first-paint"><p dir="auto">`+want+`</p>`+firstPaintSpinner+`</div>` || !style {
			t.Errorf("%s: first paint %q, style %v", target, paint, style)
		}
	}
	// Negotiated from the browser's languages when the link names none, as the title is.
	if paint, _ := e.page("/s/"+titled.String(), "Accept-Language", "de-DE").paint(); !strings.Contains(paint, ">Eigener Titel<") {
		t.Errorf("Accept-Language de-DE: first paint %q", paint)
	}
}

// Forma Cloud's " | Forma" belongs to the title tag, not to the title painted on the page.
func TestSurveyPageFirstPaintOnFormaCloud(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.IsFormaCloud = true })
	id := e.survey(e.workspace())
	if paint, _ := e.page("/s/" + id.String()).paint(); !strings.Contains(paint, `<p dir="auto">Link Feedback Survey</p>`) {
		t.Fatalf("first paint %q", paint)
	}
}

// Only a survey view has a survey to paint: a gate, a closed survey or a missing one renders its own screen, which the title would cover until the bundle removed it.
func TestSurveyPageFirstPaintOnlyOnTheSurvey(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	pin := e.survey(ws, "pin", "1234")
	verify := e.survey(ws, "is_verify_email_enabled", true)
	paused := e.survey(ws, "status", "paused")
	for _, target := range []string{"/s/" + pin.String(), "/s/" + verify.String(), "/s/" + paused.String(), "/s/" + e.survey(ws, "status", "draft").String(), "/c/not-a-token"} {
		p := e.page(target)
		if paint, style := p.paint(); paint != "" || style || strings.Contains(p.Body, firstPaintMarker) {
			t.Errorf("%s (view %q): first paint %q, style %v", target, p.view(), paint, style)
		}
	}
}

// A personal link paints its survey's title too.
func TestContactPagePaintsTheTitleFirst(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	id := e.survey(ws)
	token := contactToken(t, e.cfg, e.contact(ws).String(), id.String(), nil)
	p := e.page("/c/" + token)
	if paint, style := p.paint(); p.view() != viewSurvey || !strings.Contains(paint, `<p dir="auto">Link Feedback Survey</p>`) || !style {
		t.Fatalf("view %q first paint %q style %v", p.view(), paint, style)
	}
}
