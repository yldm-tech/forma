package usagereport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/valkey/valkeytest"
)

// licenseServer is the usage update endpoint, answering with status.
type licenseServer struct {
	mu       sync.Mutex
	status   int
	requests []*http.Request
	bodies   []string
}

func (s *licenseServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r)
	s.bodies = append(s.bodies, string(body))
	w.WriteHeader(s.status)
}

func (s *licenseServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

type env struct {
	t      *testing.T
	db     *gorm.DB
	cache  *redis.Client
	server *licenseServer
	url    string
	cfg    config.Config
	now    time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	server := &licenseServer{status: http.StatusOK}
	srv := httptest.NewServer(server)
	t.Cleanup(srv.Close)
	cfg := config.Config{SMTPHost: "smtp.example.com"}
	cfg.Telemetry.UsageReportEnabled = true
	cfg.Telemetry.PrometheusEnabled = true
	cfg.SSO.GitHubID = "github-app"
	return &env{t: t, db: pgtest.GORM(t), cache: valkeytest.Client(t), server: server, url: srv.URL, cfg: cfg, now: time.Date(2026, 10, 23, 2, 15, 0, 0, time.UTC)}
}

func (e *env) reporter() *Reporter {
	r := NewReporter(e.cfg, e.db, e.cache, slog.New(slog.DiscardHandler))
	r.endpoint = e.url + "/api/v1/instances/%s/usage-updates"
	r.now = func() time.Time { return e.now }
	return r
}

func (e *env) exec(query string, args ...any) string {
	e.t.Helper()
	var id string
	if err := e.db.Raw(query+" RETURNING id::text", args...).Row().Scan(&id); err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
	return id
}

func TestNothingIsSentUnlessTheOperatorOptedIn(t *testing.T) {
	e := newEnv(t)
	e.exec(`INSERT INTO organizations (name) VALUES ('Acme')`)
	e.cfg.Telemetry.UsageReportEnabled = false
	e.reporter().Send(t.Context())
	e.cfg.Telemetry.UsageReportEnabled = true
	e.cfg.E2ETesting = true
	e.reporter().Send(t.Context())
	e.cfg.E2ETesting = false
	e.cfg.Telemetry.NodeEnv = "development"
	e.reporter().Send(t.Context())
	if n := e.server.count(); n != 0 {
		t.Fatalf("%d usage updates sent, want none", n)
	}
}

func TestAnInstanceWithoutAnOrganizationDoesNotConsumeTheWindow(t *testing.T) {
	e := newEnv(t)
	r := e.reporter()
	r.Send(t.Context())
	if e.server.count() != 0 {
		t.Fatal("an update was sent without an organization")
	}
	if n, _ := e.cache.Exists(t.Context(), lastSentKey).Result(); n != 0 {
		t.Fatal("the 24-hour window was consumed")
	}
	// The next attempt waits an hour, not a day.
	e.exec(`INSERT INTO organizations (name) VALUES ('Acme')`)
	e.now = e.now.Add(59 * time.Minute)
	r.Send(t.Context())
	if e.server.count() != 0 {
		t.Fatal("sent within the hour")
	}
	e.now = e.now.Add(time.Minute)
	r.Send(t.Context())
	if e.server.count() != 1 {
		t.Fatalf("%d updates after the hour, want 1", e.server.count())
	}
}

// TestTheUpdateCountsUsageAndIsSentOncePerDay: the payload has forma2's shape and counts, no names or emails; it is sent under the hash of the oldest organization's id, recorded in Valkey, and neither this process nor another sends again within the day.
func TestTheUpdateCountsUsageAndIsSentOncePerDay(t *testing.T) {
	e := newEnv(t)
	oldest := e.exec(`INSERT INTO organizations (name, created_at) VALUES ('Acme Secret Name', '2025-01-02T03:04:05.678Z')`)
	org2 := e.exec(`INSERT INTO organizations (name) VALUES ('Second')`)
	user := e.exec(`INSERT INTO users (name, email) VALUES ('Jane Doe', 'jane@example.com')`)
	e.exec(`INSERT INTO user_identities (user_id, provider, subject) VALUES (?, 'google', 'g-1')`, user)
	ws := e.exec(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Website')`, oldest)
	e.exec(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'App')`, org2)
	survey := e.exec(`INSERT INTO surveys (workspace_id, name, status) VALUES (?, 'NPS', 'inProgress')`, ws)
	e.exec(`INSERT INTO surveys (workspace_id, name, status) VALUES (?, 'Done', 'completed')`, ws)
	e.exec(`INSERT INTO responses (survey_id, data, created_at) VALUES (?, '{"q1":"my private answer"}', '2026-10-20T10:00:00Z')`, survey)
	e.exec(`INSERT INTO displays (survey_id) VALUES (?)`, survey)
	e.exec(`INSERT INTO integrations (workspace_id, type) VALUES (?, 'slack')`, ws)
	definition := `{"trigger":{"triggerType":"response.completed"},"nodes":[{"type":"action","actionType":"send_email"},{"type":"condition"}]}`
	flow := e.exec(`INSERT INTO workflows (workspace_id, name, status, definition) VALUES (?, 'Flow', 'enabled', ?)`, ws, definition)
	e.exec(`INSERT INTO workflows (workspace_id, name, status, definition) VALUES (?, 'Old', 'archived', '{"trigger":{"triggerType":"archived.trigger"}}')`, ws)
	e.exec(`INSERT INTO workflow_runs (workflow_id, workspace_id, status, trigger_type, trigger_payload, finished_at) VALUES (?, ?, 'failed', 'response.completed', '{}', now())`, flow, ws)
	e.exec(`INSERT INTO workflow_runs (workflow_id, workspace_id, status, trigger_type, trigger_payload, is_dry_run) VALUES (?, ?, 'completed', 'response.completed', '{}', true)`, flow, ws)

	e.reporter().Send(t.Context())
	if e.server.count() != 1 {
		t.Fatalf("%d updates sent, want 1", e.server.count())
	}
	sum := sha256.Sum256([]byte(oldest))
	req := e.server.requests[0]
	if want := "/api/v1/instances/" + hex.EncodeToString(sum[:]) + "/usage-updates"; req.URL.Path != want || req.Method != http.MethodPost || req.Header.Get("Content-Type") != "application/json" {
		t.Errorf("request %s %s (%s), want POST %s", req.Method, req.URL.Path, req.Header.Get("Content-Type"), want)
	}
	body := e.server.bodies[0]
	for _, secret := range []string{"Acme Secret Name", "Jane Doe", "jane@example.com", "my private answer", "Website", oldest} {
		if strings.Contains(body, secret) {
			t.Errorf("the update carries %q: %s", secret, body)
		}
	}
	var p Payload
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	if p.SchemaVersion != 1 || p.OrganizationCount != 2 || p.UserCount != 1 || p.WorkspaceCount != 2 || p.SurveyCount != 2 || p.InProgressSurveyCount != 1 || p.CompletedSurveyCount != 1 ||
		p.ResponseCountAllTime != 1 || p.ResponseCountSinceLastUsageUpdate != 1 || p.DisplayCount != 1 {
		t.Errorf("counts = %+v", p)
	}
	w := p.Workflows
	if w.WorkflowCount != 1 || w.EnabledWorkflowCount != 1 || w.RunCountSinceLastUsageUpdate != 1 || w.FailedRunCountSinceLastUsageUpdate != 1 ||
		strings.Join(w.TriggerTypes, ",") != "response.completed" || strings.Join(w.ActionTypes, ",") != "condition,send_email" {
		t.Errorf("workflows = %+v", w)
	}
	if !p.Integrations.Slack || p.Integrations.Notion || !p.Infrastructure.SMTP || p.Infrastructure.S3 || !p.Infrastructure.Prometheus || p.Security.Recaptcha {
		t.Errorf("integrations %+v, infrastructure %+v, security %+v", p.Integrations, p.Infrastructure, p.Security)
	}
	if !p.SSO.GitHub || !p.SSO.Google || p.SSO.AzureAD || p.SSO.OIDC || p.SSO.SAML {
		t.Errorf("sso = %+v, want GitHub by configuration and Google by an identity", p.SSO)
	}
	if p.Meta.Version != "0.0.0" || p.Temporal.InstanceCreatedAt != "2025-01-02T03:04:05.678Z" || p.Temporal.NewestResponseAt == nil || *p.Temporal.NewestResponseAt != "2026-10-20T10:00:00.000Z" {
		t.Errorf("meta %+v, temporal %+v", p.Meta, p.Temporal)
	}

	raw, err := e.cache.Get(t.Context(), lastSentKey).Result()
	if err != nil || raw != `"`+strconv.FormatInt(e.now.UnixMilli(), 10)+`"` {
		t.Errorf("last sent = %q (%v), want forma2's JSON string of the milliseconds", raw, err)
	}
	if ttl, _ := e.cache.TTL(t.Context(), lastSentKey).Result(); ttl != -1 {
		t.Errorf("last sent TTL = %v, want none", ttl)
	}
	if n, _ := e.cache.Exists(t.Context(), lockKey).Result(); n != 0 {
		t.Error("the lock was not released")
	}

	// Another process, an hour later, sees the shared last-sent time.
	e.now = e.now.Add(time.Hour)
	e.reporter().Send(t.Context())
	e.now = e.now.Add(23 * time.Hour)
	e.reporter().Send(t.Context())
	if e.server.count() != 2 {
		t.Fatalf("%d updates after a day, want 2", e.server.count())
	}
	var second Payload
	_ = json.Unmarshal([]byte(e.server.bodies[1]), &second)
	if second.ResponseCountSinceLastUsageUpdate != 0 || second.ResponseCountAllTime != 1 {
		t.Errorf("second update counts %d responses since the first, want 0", second.ResponseCountSinceLastUsageUpdate)
	}
}

func TestARejectedUpdateIsNotRecordedAndWaitsAnHour(t *testing.T) {
	e := newEnv(t)
	e.exec(`INSERT INTO organizations (name) VALUES ('Acme')`)
	e.server.status = http.StatusServiceUnavailable
	r := e.reporter()
	r.Send(t.Context())
	if n, _ := e.cache.Exists(t.Context(), lastSentKey, lockKey).Result(); n != 0 {
		t.Fatal("a rejected update was recorded as sent, or its lock kept")
	}
	e.server.status = http.StatusOK
	e.now = e.now.Add(30 * time.Minute)
	r.Send(t.Context())
	if e.server.count() != 1 {
		t.Fatalf("%d attempts within the hour, want only the first", e.server.count())
	}
	e.now = e.now.Add(30 * time.Minute)
	r.Send(t.Context())
	if e.server.count() != 2 {
		t.Fatalf("%d attempts after the hour, want 2", e.server.count())
	}
}

func TestAnotherProcessHoldingTheLockSends(t *testing.T) {
	e := newEnv(t)
	e.exec(`INSERT INTO organizations (name) VALUES ('Acme')`)
	if err := e.cache.Set(t.Context(), lockKey, "locked", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	e.reporter().Send(t.Context())
	if e.server.count() != 0 {
		t.Fatal("sent while another process held the lock")
	}
	if n, _ := e.cache.Exists(t.Context(), lockKey).Result(); n != 1 {
		t.Fatal("released a lock it did not hold")
	}
}
