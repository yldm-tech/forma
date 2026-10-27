package pipeline

import (
	"cmp"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/yldm-tech/forma/apps/server/internal/followup"
	"github.com/yldm-tech/forma/apps/server/internal/notification"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer/mailertest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ssrf"
	"github.com/yldm-tech/forma/apps/server/internal/platform/valkey/valkeytest"
	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/webhook"
)

// syncBuffer collects the log, audit events included, from concurrent work.
type syncBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

// env is one isolated pipeline: a fresh database, a Valkey database for the follow-up limit, a mail transport that records, and webhooks allowed to reach the test's local receivers (DANGEROUSLY_ALLOW_WEBHOOK_INTERNAL_URLS).
type env struct {
	t      *testing.T
	cfg    config.Config
	dsn    string
	db     *gorm.DB
	mail   *mailertest.Transport
	logs   *syncBuffer
	worker *Worker
	hooks  *webhook.Deliverer
	log    *slog.Logger
}

func newEnv(t *testing.T, handoffs Handoffs) *env {
	t.Helper()
	t.Parallel()
	app, _ := url.Parse("https://app.example.com")
	public, _ := url.Parse("https://surveys.example.com")
	dsn := pgtest.DSN(t)
	cfg := config.Config{
		WebAppURL: app, PublicURL: public, DatabaseURL: dsn, DatabaseMaxOpenConns: 5, ShutdownTimeout: 5 * time.Second, WorkerConcurrency: 2,
		AuditLogEnabled: true, MailFrom: "noreply@forma.yldm.ai", MailFromName: "Forma", DangerouslyAllowWebhookInternalURLs: true,
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	e := &env{t: t, cfg: cfg, dsn: dsn, db: db, mail: &mailertest.Transport{}, logs: &syncBuffer{}}
	e.log = slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	m, err := mailer.NewWithTransport(cfg, e.mail, e.log)
	if err != nil {
		t.Fatal(err)
	}
	frozen := time.Now()
	limiter := ratelimit.New(cfg, valkeytest.Client(t), e.log).WithClock(func() time.Time { return frozen })
	enqueuer, err := jobs.NewEnqueuer(e.log)
	if err != nil {
		t.Fatal(err)
	}
	e.hooks = webhook.NewDeliverer(webhook.NewRepository(db), ssrf.New(cfg))
	e.worker = NewWorker(cfg, db, e.hooks, enqueuer, followup.NewSender(cfg, db, m, limiter, e.log),
		notification.NewNotifier(cfg, db, m, e.log), audit.New(cfg, e.log), handoffs, e.log)
	return e
}

func (e *env) exec(query string, args ...any) {
	e.t.Helper()
	if err := e.db.Exec(query, args...).Error; err != nil {
		e.t.Fatalf("exec %q: %v", query, err)
	}
}

func (e *env) insert(query string, args ...any) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.db.Raw(query, args...).Row().Scan(&id); err != nil {
		e.t.Fatalf("insert %q: %v", query, err)
	}
	return id
}

// user creates a user whose alert for each of surveys is on.
func (e *env) user(email string, surveys ...uuid.UUID) uuid.UUID {
	alerts := map[string]bool{}
	for _, s := range surveys {
		alerts[s.String()] = true
	}
	settings, _ := json.Marshal(map[string]any{"alert": alerts, "unsubscribedOrganizationIds": []string{}})
	return e.insert(`INSERT INTO users (name, email, locale, notification_settings) VALUES ('U', ?, 'en-US', ?::jsonb) RETURNING id`, email, string(settings))
}

// fixture is an organization with one workspace and a survey in it.
type fixture struct {
	org, ws, survey uuid.UUID
}

const blocks = `[{"id":"b1","name":"Block","elements":[
	{"id":"q1","type":"openText","headline":{"default":"Your email?"},"required":false,"inputType":"email"},
	{"id":"q2","type":"fileUpload","headline":{"default":"Upload"},"required":false}
]}]`

func (e *env) fixture() fixture {
	e.t.Helper()
	org := e.insert(`INSERT INTO organizations (name, whitelabel) VALUES ('Acme', '{"logoUrl":"/storage/ws/public/logo.png"}') RETURNING id`)
	ws := e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Main') RETURNING id`, org)
	sv := e.insert(`INSERT INTO surveys (workspace_id, name, status, blocks, endings) VALUES (?, 'Feedback', 'inProgress', ?::jsonb, '[{"id":"end1","type":"endScreen"},{"id":"end2","type":"endScreen"}]'::jsonb) RETURNING id`, ws, blocks)
	return fixture{org: org, ws: ws, survey: sv}
}

func (e *env) member(org, user uuid.UUID, role string) {
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, ?, true)`, user, org, role)
}

func (e *env) teamAccess(org, ws, user uuid.UUID) {
	team := e.insert(`INSERT INTO teams (organization_id, name) VALUES (?, ?) RETURNING id`, org, "Team "+uuid.NewString())
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'contributor')`, team, user)
	e.exec(`INSERT INTO workspace_teams (workspace_id, team_id, permission) VALUES (?, ?, 'read')`, ws, team)
}

// respond stores a response to the fixture's survey and returns it as the pipeline payload carries it.
func (e *env) respond(f fixture, finished bool, data string) response.Response {
	e.t.Helper()
	id := e.insert(`INSERT INTO responses (survey_id, finished, data, contact_attributes, meta) VALUES (?, ?, ?::jsonb, '{"email":"respondent@example.com"}'::jsonb, '{"url":"https://example.com/?a=1"}'::jsonb) RETURNING id`,
		f.survey, finished, data)
	r, err := response.Load(context.Background(), e.db, id)
	if err != nil || r == nil {
		e.t.Fatalf("load response: %v", err)
	}
	return *r
}

// job is a pipeline job on its given attempt, as River hands it to the worker.
func job(event string, f fixture, r response.Response, attempt int) *river.Job[response.PipelineArgs] {
	return &river.Job[response.PipelineArgs]{
		JobRow: &rivertype.JobRow{ID: 1, Attempt: attempt, MaxAttempts: 3, Kind: response.PipelineArgs{}.Kind()},
		Args:   response.PipelineArgs{Event: event, WorkspaceID: f.ws, SurveyID: f.survey, Response: r},
	}
}

func (e *env) work(j *river.Job[response.PipelineArgs]) error {
	return e.worker.Work(context.Background(), j)
}

// webhook stores a webhook of ws on url for triggers; surveys empty scopes it to every survey.
func (e *env) webhook(ws uuid.UUID, url, secret string, triggers string, surveys ...uuid.UUID) uuid.UUID {
	e.t.Helper()
	id := e.insert(`INSERT INTO webhooks (workspace_id, url, triggers, secret, all_surveys) VALUES (?, ?, ?::text[], NULLIF(?, ''), ?) RETURNING id`, ws, url, triggers, secret, len(surveys) == 0)
	for _, s := range surveys {
		e.exec(`INSERT INTO webhook_surveys (webhook_id, survey_id) VALUES (?, ?)`, id, s)
	}
	return id
}

// receiver is a webhook endpoint that records the requests it gets; fail makes the next n requests answer failStatus, 500 unless failWith set another.
type receiver struct {
	*httptest.Server
	mu         sync.Mutex
	requests   []received
	fail       int
	failStatus int
}

type received struct {
	Header http.Header
	Body   []byte
}

func newReceiver(t *testing.T) *receiver {
	t.Helper()
	r := &receiver{}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.requests = append(r.requests, received{Header: req.Header.Clone(), Body: body})
		fail, status := r.fail > 0, cmp.Or(r.failStatus, http.StatusInternalServerError)
		if fail {
			r.fail--
		}
		r.mu.Unlock()
		if fail {
			w.WriteHeader(status)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(r.Close)
	return r
}

// failOnce makes the next request fail.
func (r *receiver) failOnce() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fail = 1
}

// failWith makes the next n requests answer status.
func (r *receiver) failWith(status, n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fail, r.failStatus = n, status
}

func (r *receiver) received() []received {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]received(nil), r.requests...)
}

// waitFor polls cond until it holds, failing the test after the deadline.
func waitFor(t *testing.T, what string, deadline time.Duration, cond func() bool) {
	t.Helper()
	end := time.Now().Add(deadline)
	for !cond() {
		if time.Now().After(end) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// deliver works the queued webhook deliveries on River, as `forma worker` does, until none is due: a delivery that failed is left retryable until its next retry.
func (e *env) deliver() {
	e.t.Helper()
	pool, cleanup, err := jobs.OpenPool(context.Background(), e.cfg)
	if err != nil {
		e.t.Fatal(err)
	}
	defer cleanup()
	reg := jobs.NewRegistry(e.log)
	webhook.RegisterJobs(reg, e.hooks, jobs.Concurrency(e.cfg), e.log)
	worker, err := jobs.NewWorker(e.cfg, pool, reg, e.log)
	if err != nil {
		e.t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	waitFor(e.t, "the queued webhook deliveries", 30*time.Second, func() bool {
		return e.count(`SELECT count(*) FROM river_job WHERE kind = 'webhook.deliver' AND state IN ('available', 'running') AND scheduled_at <= now()`) == 0
	})
	cancel()
	if err := <-done; err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.Raw(query, args...).Row().Scan(&n); err != nil {
		e.t.Fatalf("count %q: %v", query, err)
	}
	return n
}

// deliveryJob is the state of the webhook.deliver job of hook.
type deliveryJob struct {
	ID          int64
	State       string
	Attempt     int
	MaxAttempts int
	RetryIn     time.Duration
}

func (e *env) deliveryJob(hook uuid.UUID) deliveryJob {
	e.t.Helper()
	var j deliveryJob
	var retryIn float64
	err := e.db.Raw(`SELECT id, state, attempt, max_attempts, COALESCE(EXTRACT(EPOCH FROM scheduled_at - attempted_at), 0)::float8 FROM river_job WHERE kind = 'webhook.deliver' AND args->>'webhookId' = ?`, hook.String()).
		Row().Scan(&j.ID, &j.State, &j.Attempt, &j.MaxAttempts, &retryIn)
	if err != nil {
		e.t.Fatalf("delivery job of webhook %s: %v", hook, err)
	}
	j.RetryIn = time.Duration(retryIn * float64(time.Second))
	return j
}
