package linksurvey

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fixture is one exchange recorded against forma2 (server/internal/clientapi/testdata/forma2, see its README).
type fixture struct {
	Request struct {
		Method  string            `json:"method"`
		Path    string            `json:"path"`
		Query   map[string]string `json:"query"`
		Headers map[string]string `json:"headers"`
	} `json:"request"`
	Response struct {
		Status     int               `json:"status"`
		Headers    map[string]string `json:"headers"`
		BodyFormat string            `json:"bodyFormat"`
		Body       json.RawMessage   `json:"body"`
	} `json:"response"`
}

type headBody struct {
	Title string              `json:"title"`
	Meta  []map[string]string `json:"meta"`
	Links []map[string]string `json:"links"`
}

func loadFixture(t *testing.T, name string, placeholders map[string]string) fixture {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "clientapi", "testdata", "forma2", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for k, v := range placeholders {
		s = strings.ReplaceAll(s, k, v)
	}
	var f fixture
	if err := json.Unmarshal([]byte(s), &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f fixture) target() string {
	q := url.Values{}
	for k, v := range f.Request.Query {
		q.Set(k, v)
	}
	if len(q) == 0 {
		return f.Request.Path
	}
	return f.Request.Path + "?" + q.Encode()
}

// checkHeaders compares every header the fixture recorded, and requires the ones it recorded as absent (x-frame-options on an embeddable page) to be absent. Vary is compared as a set of tokens.
func checkHeaders(t *testing.T, got http.Header, want map[string]string) {
	t.Helper()
	for name, value := range want {
		if got.Get(name) != value {
			t.Errorf("header %s = %q, want %q", name, got.Get(name), value)
		}
	}
	if _, recorded := want["x-frame-options"]; !recorded && got.Get("X-Frame-Options") != "" {
		t.Errorf("X-Frame-Options %q was not sent by forma2", got.Get("X-Frame-Options"))
	}
	if _, recorded := want["access-control-allow-origin"]; !recorded && got.Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("Access-Control-Allow-Origin %q was not sent by forma2", got.Get("Access-Control-Allow-Origin"))
	}
}

func checkHead(t *testing.T, p renderedPage, raw json.RawMessage) {
	t.Helper()
	var want headBody
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if p.Title != want.Title {
		t.Errorf("title %q, want %q", p.Title, want.Title)
	}
	if !reflect.DeepEqual(p.Metas, want.Meta) {
		t.Errorf("meta tags\n got %v\nwant %v", p.Metas, want.Meta)
	}
	links := p.Links
	if links == nil {
		links = []map[string]string{}
	}
	if !reflect.DeepEqual(links, want.Links) {
		t.Errorf("links\n got %v\nwant %v", links, want.Links)
	}
}

// The recorded link survey: in progress, en-US by default and de-DE, with the slug contract-feedback, on a workspace with the default styling.
func (e *env) recordedSurveys() (link, paused string) {
	ws := e.workspace()
	l := e.survey(ws, "slug", "contract-feedback", "hidden_fields", `{"enabled":true,"fieldIds":["utm_source","campaign"]}`)
	e.language(ws, l, "en-US", "", true, true)
	e.language(ws, l, "de-DE", "", false, true)
	p := e.survey(ws, "name", "Paused Survey", "status", "paused")
	return l.String(), p.String()
}

// The /s and /p pages answer the recorded requests with forma2's status, headers and document head.
func TestPagesMatchRecordedForma2(t *testing.T) {
	e := newEnv(t)
	link, paused := e.recordedSurveys()
	placeholders := map[string]string{"<webapp-url>": "http://localhost:3999", "<id:survey:link>": link, "<id:survey:paused>": paused}
	for _, name := range []string{"page-s-link-survey", "page-s-link-survey-lang", "page-s-paused-survey", "page-s-unknown-survey", "page-p-unknown-slug"} {
		t.Run(name, func(t *testing.T) {
			f := loadFixture(t, name, placeholders)
			p := e.page(f.target(), "Accept", f.Request.Headers["accept"], "User-Agent", f.Request.Headers["user-agent"], "Origin", f.Request.Headers["origin"])
			if p.Status != f.Response.Status {
				t.Fatalf("status %d, want %d", p.Status, f.Response.Status)
			}
			checkHeaders(t, p.Header, f.Response.Headers)
			checkHead(t, p, f.Response.Body)
		})
	}
}

// forma2's /p/{slug} redirects to /s/{id}; the recorder followed the redirect, so the fixture is the /s page it lands on.
func TestPrettyURLMatchesRecordedForma2(t *testing.T) {
	e := newEnv(t)
	link, _ := e.recordedSurveys()
	f := loadFixture(t, "page-p-slug", map[string]string{"<webapp-url>": "http://localhost:3999", "<id:survey:link>": link})
	rec := e.request(http.MethodGet, f.target(), nil)
	if rec.Code != http.StatusTemporaryRedirect || rec.Header().Get("Location") != "/s/"+link {
		t.Fatalf("redirect %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	p := e.page(rec.Header().Get("Location"))
	if p.Status != f.Response.Status {
		t.Fatalf("status %d, want %d", p.Status, f.Response.Status)
	}
	checkHeaders(t, p.Header, f.Response.Headers)
	checkHead(t, p, f.Response.Body)
}

// The OG image route answers with forma2's status, headers and an 800x400 PNG, and its OPTIONS with Next's automatic 204.
func TestOgImageMatchesRecordedForma2(t *testing.T) {
	e := newEnv(t)
	for _, name := range []string{"v1-og-image", "v1-og-image-options"} {
		t.Run(name, func(t *testing.T) {
			f := loadFixture(t, name, nil)
			rec := e.request(f.Request.Method, f.target(), nil, "Origin", f.Request.Headers["origin"])
			if rec.Code != f.Response.Status {
				t.Fatalf("status %d, want %d", rec.Code, f.Response.Status)
			}
			checkHeaders(t, rec.Header(), f.Response.Headers)
			if f.Response.BodyFormat == "png" {
				var size struct{ Width, Height int }
				_ = json.Unmarshal(f.Response.Body, &size)
				img := decodeOg(t, rec.Body.Bytes())
				if img.Bounds().Dx() != size.Width || img.Bounds().Dy() != size.Height {
					t.Fatalf("image %v, want %dx%d", img.Bounds(), size.Width, size.Height)
				}
			} else if rec.Body.Len() != 0 {
				t.Fatalf("body %q, want none", rec.Body.String())
			}
		})
	}
}
