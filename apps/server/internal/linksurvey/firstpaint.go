package linksurvey

import (
	"html"

	"github.com/yldm-tech/forma/apps/server/internal/survey"
)

// The survey's title, painted by the server over a survey page until the page bundle and the respondent runtime have rendered the first card. Without it the page stays blank until both bundles have downloaded and run, seconds on a slow phone; with it the first paint is in the HTML itself. The bundle fades it out once the card is ready, or removes it at once when it shows any other screen (apps/web/src/survey-page/lib/first-paint.ts).
const (
	// firstPaintElementID names the element for the bundle.
	firstPaintElementID = "forma-first-paint"
	// firstPaintStyle is self-contained, so the paint needs nothing but the HTML and the page's stylesheet, which the head loads first anyway. It looks like the bundle's own loading overlay (survey-loading-animation.tsx), which it covers: a white page with a spinner, here under the survey's title. The fade matches the overlay's duration-300; the bundle adds the class.
	firstPaintStyle = `<style>#forma-first-paint{position:fixed;inset:0;z-index:5001;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:1rem;padding:1.5rem;background:#fff;color:#0f172a;transition:opacity .3s}#forma-first-paint.forma-first-paint-done{opacity:0}#forma-first-paint p{margin:0;max-width:36rem;font-size:1.5rem;line-height:2rem;font-weight:600;text-align:center;overflow-wrap:anywhere}#forma-first-paint svg{width:1.5rem;height:1.5rem;color:#334155;animation:forma-first-paint-spin 1s linear infinite}@keyframes forma-first-paint-spin{to{transform:rotate(360deg)}}@media (prefers-reduced-motion:reduce){#forma-first-paint{transition:none}#forma-first-paint svg{animation-duration:3s}}</style>`
	// firstPaintSpinner is the bundle's LoadingSpinner.
	firstPaintSpinner = `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" aria-hidden="true"><circle opacity=".25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path opacity=".75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg>`
)

// firstPaintTitle is the title a survey page paints first: the survey's title as the page's <title> shows it (the link metadata's title, else the welcome card's headline, else the survey's name), in the language the survey opens in, without the " | Forma" the title tag adds.
func firstPaintTitle(sv *survey.Survey, lang string) string {
	return basicSurveyMetadata(sv, lang, false).Title
}

// firstPaintHTML is the element painted over the page; empty without a title, where the page keeps the bundle's own overlay. The title is the respondent-facing text an author wrote, so it is escaped like every other value written into the page.
func firstPaintHTML(title string) string {
	if title == "" {
		return ""
	}
	return `<div id="` + firstPaintElementID + `"><p dir="auto">` + html.EscapeString(title) + `</p>` + firstPaintSpinner + `</div>`
}
