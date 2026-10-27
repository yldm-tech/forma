package linksurvey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/repotest"
)

// The survey page bundle's HTML carries the markers the server fills; without them every page would fall back to a shell that renders nothing.
func TestWebSurveyHTMLHasTheMarkers(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repotest.Root(t), "apps/web/survey.html"))
	if err != nil {
		t.Fatal(err)
	}
	if s := newShell(b); string(s.doc) != string(b) {
		t.Fatal("apps/web/survey.html lacks the head, first paint or state marker")
	}
	if !strings.Contains(string(b), `<html lang="`) {
		t.Fatal("apps/web/survey.html has no lang attribute to set")
	}
}

func TestShellRender(t *testing.T) {
	s := newShell([]byte(testShell))
	doc, err := s.render(`ja-JP"><script>`, "<title>T</title>", `<div id="paint"></div>`, pageState{View: viewNotFound, Locale: "</script><script>alert(1)</script>"})
	if err != nil {
		t.Fatal(err)
	}
	out := string(doc)
	if !strings.Contains(out, `<html lang="ja-JP&#34;&gt;&lt;script&gt;"`) || !strings.Contains(out, "<title>T</title>") || !strings.Contains(out, `<body><div id="paint"></div><div id="root">`) {
		t.Fatalf("shell:\n%s", out)
	}
	if strings.Contains(out, "<script>alert(1)") || !strings.Contains(out, `</script>`) {
		t.Fatalf("the state escaped its script element:\n%s", out)
	}
	if strings.Contains(out, headMarker) || strings.Contains(out, firstPaintMarker) || strings.Contains(out, stateMarker) {
		t.Fatal("a marker was left in the page")
	}
	// A build without the web UI still answers with the head and state.
	if doc, _ := newShell(nil).render("en-US", "<title>T</title>", "", pageState{View: viewNotFound}); !strings.Contains(string(doc), `<script id="forma-survey-page" type="application/json">{"view":"notFound"`) {
		t.Fatalf("fallback shell:\n%s", doc)
	}
}

// The page bundle's stylesheet blocks rendering, so the first paint waits for it: the shell asks for it before anything else, ahead of the runtime the head preloads and the bundle's module preloads, which Vite writes before it.
func TestShellPutsTheStylesheetFirst(t *testing.T) {
	built := `<!doctype html><html lang="en" translate="no"><head><meta charset="UTF-8"><!--forma:head--><script type="module" crossorigin src="/app/assets/survey-a.js"></script><link rel="modulepreload" crossorigin href="/app/assets/chunk-b.js"><link rel="stylesheet" crossorigin href="/app/assets/survey-c.css"></head><body><!--forma:first-paint--><div id="root"></div><!--forma:state--></body></html>`
	doc, err := newShell([]byte(built)).render("en-US", `<title>T</title><link rel="preload" href="/js/surveys.umd.js" as="script" fetchpriority="high">`, "", pageState{View: viewNotFound})
	if err != nil {
		t.Fatal(err)
	}
	const stylesheet = `<link rel="stylesheet" crossorigin href="/app/assets/survey-c.css">`
	out := string(doc)
	if !strings.Contains(out, `<head><meta charset="UTF-8">`+stylesheet+`<title>T</title><link rel="preload" href="/js/surveys.umd.js"`) || strings.Count(out, stylesheet) != 1 {
		t.Fatalf("shell:\n%s", out)
	}
	if !strings.Contains(out, `<script type="module" crossorigin src="/app/assets/survey-a.js"></script><link rel="modulepreload" crossorigin href="/app/assets/chunk-b.js"></head>`) {
		t.Fatalf("the bundle's other tags moved:\n%s", out)
	}
}
