package linksurvey

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/net/html"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/server/internal/platform/mailer/mailertest"
	"github.com/yldm-tech/forma/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
	"github.com/yldm-tech/forma/server/internal/platform/valkey/valkeytest"
	"github.com/yldm-tech/forma/server/internal/survey"
	"github.com/yldm-tech/forma/server/internal/webui"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

// testShell is a survey page shell with the markers the web build's survey.html carries.
const testShell = `<!doctype html><html lang="en" translate="no"><head><meta charset="UTF-8"><!--forma:head--><link rel="stylesheet" href="/assets/survey.css"><script type="module" src="/assets/survey.js"></script></head><body><div id="root"></div><!--forma:state--></body></html>`

// env is the link survey module on a fresh database and Valkey, mounted the way the server mounts it.
type env struct {
	t      *testing.T
	cfg    config.Config
	db     *gorm.DB
	cache  *redis.Client
	mail   *mailertest.Transport
	svc    *Service
	engine *gin.Engine
	now    time.Time
}

func newEnv(t *testing.T, configure ...func(*config.Config)) *env {
	t.Helper()
	t.Parallel()
	u, _ := url.Parse("http://localhost:3999")
	cfg := config.Config{
		WebAppURL: u, RateLimitingDisabled: true, TrustedProxyHopCount: 1, MailFrom: "noreply@forma.ylam.ai", MailFromName: "Forma",
		EncryptionKey: bytes.Repeat([]byte{7}, 32), EncryptionKeyText: config.Secret(strings.Repeat("07", 32)), EncryptionKeyVersion: 1,
		NextAuthSecret: testNextAuthSecret, BetterAuthSecret: testBetterAuthSecret, SurveySchedulingTimeZone: "Europe/Berlin",
	}
	for _, f := range configure {
		f(&cfg)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db := pgtest.GORM(t)
	cache := valkeytest.Client(t)
	cipher, err := secret.NewCipher(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Windows are aligned to the wall clock. A clock frozen at the start of a window keeps a rate-limit scenario inside one window, and its counter alive for the whole minute, on a slow run.
	frozen := time.Now().Truncate(time.Minute)
	limiter := ratelimit.New(cfg, cache, log).WithClock(func() time.Time { return frozen })
	surveyRepo := survey.NewRepository(db)
	surveys := survey.NewService(cfg, surveyRepo, nil, limiter, audit.New(cfg, log), nil, cipher, nil, nil, log)
	mail := &mailertest.Transport{}
	m, err := mailer.NewWithTransport(cfg, mail, log)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, cfg: cfg, db: db, cache: cache, mail: mail, now: frozen}
	e.svc = NewService(cfg, NewRepository(db, surveyRepo, cache, log), surveys, m, log)
	e.svc.now = func() time.Time { return e.now }
	web, err := webui.NewFromFS(fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}, "survey.html": {Data: []byte(testShell)}})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(cfg, e.svc, limiter, web, log)
	e.engine = httpserver.NewEngine(cfg, log)
	e.engine.Use(httpapi.NewClientIPResolver(cfg, log).Middleware())
	h.Register(e.engine)
	return e
}

func (e *env) exec(query string, args ...any) {
	e.t.Helper()
	if err := e.db.Exec(query, args...).Error; err != nil {
		e.t.Fatalf("exec %q: %v", query, err)
	}
}

// workspace creates an organization with one workspace and returns the workspace.
func (e *env) workspace(columns ...string) uuid.UUID {
	e.t.Helper()
	org, ws := uuid.New(), uuid.New()
	e.exec(`INSERT INTO organizations (id, name) VALUES (?, ?)`, org, "Org "+org.String()[:8])
	e.exec(`INSERT INTO workspaces (id, organization_id, name) VALUES (?, ?, ?)`, ws, org, "Workspace "+ws.String()[:8])
	for i := 0; i+1 < len(columns); i += 2 {
		e.exec(fmt.Sprintf(`UPDATE workspaces SET %s = ? WHERE id = ?`, columns[i]), columns[i+1], ws)
	}
	return ws
}

func (e *env) organizationOf(ws uuid.UUID) uuid.UUID {
	var rows []struct{ OrganizationID uuid.UUID }
	if err := e.db.Raw(`SELECT organization_id FROM workspaces WHERE id = ?`, ws).Scan(&rows).Error; err != nil || len(rows) != 1 {
		e.t.Fatalf("organization of %s: %v", ws, err)
	}
	return rows[0].OrganizationID
}

// linkBlocks is a one-block survey with a required open text and a rating, like the recorded link survey.
const linkBlocks = `[{"id":"blockaaaaaaaaaaaaaaaaaa1","name":"Block 1","elements":[{"id":"feedback","type":"openText","headline":{"default":"What do you think?","de-DE":"Was denkst du?"},"required":true,"inputType":"text","charLimit":{"enabled":false}},{"id":"score","type":"rating","headline":{"default":"Rate us"},"required":false,"scale":"number","range":5}]}]`

// survey creates a link survey in progress; columns override its defaults as name/value pairs.
func (e *env) survey(ws uuid.UUID, columns ...any) uuid.UUID {
	e.t.Helper()
	return e.surveyWithID(uuid.New(), ws, columns...)
}

func (e *env) surveyWithID(id, ws uuid.UUID, columns ...any) uuid.UUID {
	e.t.Helper()
	values := map[string]any{"name": "Link Feedback Survey", "type": "link", "status": "inProgress", "blocks": linkBlocks}
	order := []string{"name", "type", "status", "blocks"}
	for i := 0; i+1 < len(columns); i += 2 {
		name := columns[i].(string)
		if _, ok := values[name]; !ok {
			order = append(order, name)
		}
		values[name] = columns[i+1]
	}
	cols, marks, args := []string{"id", "workspace_id"}, []string{"?", "?"}, []any{id, ws}
	for _, name := range order {
		cols, marks, args = append(cols, name), append(marks, "?"), append(args, values[name])
	}
	e.exec(fmt.Sprintf(`INSERT INTO surveys (%s) VALUES (%s)`, strings.Join(cols, ", "), strings.Join(marks, ", ")), args...)
	return id
}

// language adds a workspace language to a survey.
func (e *env) language(ws, surveyID uuid.UUID, code, alias string, isDefault, enabled bool) {
	e.t.Helper()
	var rows []struct{ ID uuid.UUID }
	if err := e.db.Raw(`SELECT id FROM languages WHERE workspace_id = ? AND code = ?`, ws, code).Scan(&rows).Error; err != nil {
		e.t.Fatal(err)
	}
	var id uuid.UUID
	if len(rows) == 1 {
		id = rows[0].ID
	} else {
		id = uuid.New()
		var a any
		if alias != "" {
			a = alias
		}
		e.exec(`INSERT INTO languages (id, workspace_id, code, alias) VALUES (?, ?, ?, ?)`, id, ws, code, a)
	}
	e.exec(`INSERT INTO survey_languages (survey_id, language_id, is_default, enabled) VALUES (?, ?, ?, ?)`, surveyID, id, isDefault, enabled)
}

func (e *env) contact(ws uuid.UUID) uuid.UUID {
	id := uuid.New()
	e.exec(`INSERT INTO contacts (id, workspace_id) VALUES (?, ?)`, id, ws)
	return id
}

func (e *env) response(surveyID uuid.UUID, finished bool, columns ...any) uuid.UUID {
	e.t.Helper()
	id := uuid.New()
	e.exec(`INSERT INTO responses (id, survey_id, finished) VALUES (?, ?, ?)`, id, surveyID, finished)
	for i := 0; i+1 < len(columns); i += 2 {
		e.exec(fmt.Sprintf(`UPDATE responses SET %s = ? WHERE id = ?`, columns[i]), columns[i+1], id)
	}
	return id
}

func (e *env) request(method, target string, body any, header ...string) *httptest.ResponseRecorder {
	e.t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			e.t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, target, r)
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://localhost:3999")
		req.Header.Set(httpapi.CSRFHeader, "XMLHttpRequest")
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec
}

// renderedPage is a survey page as the browser receives it: its status, the head the server wrote and the state the bundle reads.
type renderedPage struct {
	Status int
	Header http.Header
	Title  string
	Metas  []map[string]string
	Links  []map[string]string
	Lang   string
	State  map[string]any
	Body   string
}

func (e *env) page(target string, header ...string) renderedPage {
	e.t.Helper()
	rec := e.request(http.MethodGet, target, nil, header...)
	return parsePage(e.t, rec)
}

// parsePage reads a page the way the recorded fixtures were made: the title, every meta tag but the charset, every link that is not a stylesheet or an asset hint, and the page state.
func parsePage(t *testing.T, rec *httptest.ResponseRecorder) renderedPage {
	t.Helper()
	p := renderedPage{Status: rec.Code, Header: rec.Header(), Body: rec.Body.String()}
	doc, err := html.Parse(strings.NewReader(rec.Body.String()))
	if err != nil {
		t.Fatal(err)
	}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			attrs := map[string]string{}
			for _, a := range n.Attr {
				attrs[a.Key] = a.Val
			}
			switch n.Data {
			case "html":
				p.Lang = attrs["lang"]
			case "title":
				if n.FirstChild != nil {
					p.Title = n.FirstChild.Data
				}
			case "meta":
				if _, charset := attrs["charset"]; !charset {
					p.Metas = append(p.Metas, attrs)
				}
			case "link":
				switch attrs["rel"] {
				case "stylesheet", "preload", "modulepreload", "preconnect", "dns-prefetch":
				default:
					p.Links = append(p.Links, attrs)
				}
			case "script":
				if attrs["id"] == stateElementID && n.FirstChild != nil {
					if err := json.Unmarshal([]byte(n.FirstChild.Data), &p.State); err != nil {
						t.Fatalf("page state: %v", err)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return p
}

func (p renderedPage) meta(key string) string {
	for _, m := range p.Metas {
		if m["name"] == key || m["property"] == key {
			return m["content"]
		}
	}
	return ""
}

func (p renderedPage) link(rel string) string {
	for _, l := range p.Links {
		if l["rel"] == rel {
			return l["href"]
		}
	}
	return ""
}

func (p renderedPage) view() string {
	v, _ := p.State["view"].(string)
	return v
}

// sub reads a nested object of the page state.
func (p renderedPage) sub(keys ...string) map[string]any {
	cur := p.State
	for _, k := range keys {
		next, _ := cur[k].(map[string]any)
		cur = next
	}
	return cur
}
