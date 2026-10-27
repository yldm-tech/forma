package linksurvey

import (
	"bytes"
	"encoding/json"
	"html"
	"regexp"
)

// The survey page bundle's HTML (apps/web/survey.html, built into the embedded web UI) marks where the server writes the head, the first paint (firstpaint.go) and the page state.
const (
	headMarker       = "<!--forma:head-->"
	firstPaintMarker = "<!--forma:first-paint-->"
	stateMarker      = "<!--forma:state-->"
	// StateElementID is the id of the script element that carries the page state; the bundle reads it.
	stateElementID = "forma-survey-page"
)

// fallbackShell stands in when the binary was built without the web UI: the page still answers with its status, head and state, only nothing renders it.
const fallbackShell = `<!doctype html><html lang="en" translate="no"><head><meta charset="UTF-8">` + headMarker + `</head><body>` + firstPaintMarker + `<div id="root"></div>` + stateMarker + `</body></html>`

var htmlLang = regexp.MustCompile(`<html lang="[^"]*"`)

// stylesheetLink is a stylesheet link as Vite writes it into the built survey.html.
var stylesheetLink = regexp.MustCompile(`<link rel="stylesheet"[^>]*>`)

type shell struct {
	doc []byte
}

func newShell(doc []byte) shell {
	if !bytes.Contains(doc, []byte(headMarker)) || !bytes.Contains(doc, []byte(firstPaintMarker)) || !bytes.Contains(doc, []byte(stateMarker)) {
		doc = []byte(fallbackShell)
	}
	return shell{doc: stylesheetsFirst(doc)}
}

// stylesheetsFirst moves the head's stylesheet links in front of the head marker, so the browser asks for them first. Vite writes the page bundle's stylesheet last, after its module preloads, and the server writes the runtime's high-priority preload at the marker; but the stylesheet blocks rendering, so on a first visit the first paint (firstpaint.go), and every other screen, would wait while it shared the connection with several hundred kilobytes of script.
func stylesheetsFirst(doc []byte) []byte {
	head, body, ok := bytes.Cut(doc, []byte("</head>"))
	if !ok {
		return doc
	}
	links := stylesheetLink.FindAll(head, -1)
	if len(links) == 0 {
		return doc
	}
	head = stylesheetLink.ReplaceAllLiteral(head, nil)
	moved := append(bytes.Join(links, nil), headMarker...)
	head = bytes.Replace(head, []byte(headMarker), moved, 1)
	out := make([]byte, 0, len(doc))
	out = append(out, head...)
	out = append(out, "</head>"...)
	return append(out, body...)
}

// render fills the shell; firstPaintHTML may be empty. The state is JSON inside a script element; encoding/json escapes <, > and & in strings, so no value can close the element.
func (s shell) render(locale, headHTML, firstPaintHTML string, state pageState) ([]byte, error) {
	b, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	doc := htmlLang.ReplaceAllLiteral(s.doc, []byte(`<html lang="`+html.EscapeString(locale)+`"`))
	doc = bytes.Replace(doc, []byte(headMarker), []byte(headHTML), 1)
	doc = bytes.Replace(doc, []byte(firstPaintMarker), []byte(firstPaintHTML), 1)
	script := append([]byte(`<script id="`+stateElementID+`" type="application/json">`), b...)
	script = append(script, "</script>"...)
	return bytes.Replace(doc, []byte(stateMarker), script, 1), nil
}
