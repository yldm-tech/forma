package linksurvey

import (
	"bytes"
	"encoding/json"
	"html"
	"regexp"
)

// The survey page bundle's HTML (apps/web/survey.html, built into the embedded web UI) marks where the server writes the head and the page state.
const (
	headMarker  = "<!--forma:head-->"
	stateMarker = "<!--forma:state-->"
	// StateElementID is the id of the script element that carries the page state; the bundle reads it.
	stateElementID = "forma-survey-page"
)

// fallbackShell stands in when the binary was built without the web UI: the page still answers with its status, head and state, only nothing renders it.
const fallbackShell = `<!doctype html><html lang="en" translate="no"><head><meta charset="UTF-8">` + headMarker + `</head><body><div id="root"></div>` + stateMarker + `</body></html>`

var htmlLang = regexp.MustCompile(`<html lang="[^"]*"`)

type shell struct {
	doc []byte
}

func newShell(doc []byte) shell {
	if !bytes.Contains(doc, []byte(headMarker)) || !bytes.Contains(doc, []byte(stateMarker)) {
		doc = []byte(fallbackShell)
	}
	return shell{doc: doc}
}

// render fills the shell. The state is JSON inside a script element; encoding/json escapes <, > and & in strings, so no value can close the element.
func (s shell) render(locale, headHTML string, state pageState) ([]byte, error) {
	b, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	doc := htmlLang.ReplaceAllLiteral(s.doc, []byte(`<html lang="`+html.EscapeString(locale)+`"`))
	doc = bytes.Replace(doc, []byte(headMarker), []byte(headHTML), 1)
	script := append([]byte(`<script id="`+stateElementID+`" type="application/json">`), b...)
	script = append(script, "</script>"...)
	return bytes.Replace(doc, []byte(stateMarker), script, 1), nil
}
