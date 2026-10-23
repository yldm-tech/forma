// Package usagereport is forma2's anonymous usage update (apps/web/lib/telemetry): at most once a day, an instance whose operator opted in with TELEMETRY_ENABLED=1 posts counts of what it holds (organizations, surveys, responses, workflows, which integrations and sign-in providers are in use) to Forma's license server under an id derived from its oldest organization. It never sends names, emails, answers or any other content.
package usagereport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/riverqueue/river"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/platform/buildinfo"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
)

// forma2's timing and Valkey keys.
const (
	interval        = 24 * time.Hour
	failureCooldown = time.Hour
	lockTTL         = time.Minute
	sendTimeout     = 10 * time.Second
	lockKey         = "fb:analytics:telemetry_lock"
	lastSentKey     = "fb:analytics:telemetry_last_sent_ts"
	endpoint        = "https://ee.forma.ylam.ai/api/v1/instances/%s/usage-updates"
)

// Reporter sends the usage update. Every caller goes through the same checks, so the daily job and the response pipeline can both call Send without a duplicate report: a process-local next-check time, the opt-in, a shared last-sent time in Valkey and a lock held while one process sends.
type Reporter struct {
	cfg      config.Config
	db       *gorm.DB
	cache    *redis.Client
	client   *http.Client
	endpoint string
	log      *slog.Logger
	now      func() time.Time

	mu        sync.Mutex
	nextCheck time.Time
}

func NewReporter(cfg config.Config, db *gorm.DB, cache *redis.Client, log *slog.Logger) *Reporter {
	return &Reporter{cfg: cfg, db: db, cache: cache, client: &http.Client{Timeout: sendTimeout}, endpoint: endpoint, log: log, now: time.Now}
}

// Send is forma2's sendTelemetryEvents. It never fails its caller: a failed update is logged and the next attempt waits an hour.
func (r *Reporter) Send(ctx context.Context) {
	// Development and end-to-end runs never report, whatever their configuration.
	if r.cfg.E2ETesting || r.cfg.Telemetry.NodeEnv == "development" {
		return
	}
	now := r.now()
	r.mu.Lock()
	early := now.Before(r.nextCheck)
	r.mu.Unlock()
	if early || !r.cfg.Telemetry.UsageReportEnabled {
		return
	}

	lastSent, err := r.lastSent(ctx)
	if err != nil {
		// Valkey is unavailable: try again in an hour rather than on every response.
		r.wait(now.Add(failureCooldown))
		return
	}
	if now.Sub(lastSent) < interval {
		r.wait(lastSent.Add(interval))
		return
	}
	locked, err := r.cache.SetNX(ctx, lockKey, "locked", lockTTL).Result()
	if err != nil || !locked {
		// Another process is sending.
		return
	}
	defer func() {
		if err := r.cache.Del(context.WithoutCancel(ctx), lockKey).Err(); err != nil {
			r.log.WarnContext(ctx, "Failed to release the usage update lock", "err", err)
		}
	}()

	sent, err := r.send(ctx, lastSent)
	switch {
	case err != nil:
		r.log.WarnContext(ctx, "Failed to send telemetry - applying 1h cooldown", "err", err, "lastSent", lastSent.UnixMilli(), "now", now.UnixMilli())
		r.wait(now.Add(failureCooldown))
	case !sent:
		// No organization yet: the 24-hour window is not consumed, so the first report is not delayed a day.
		r.log.InfoContext(ctx, "Telemetry skipped - no organization to report on yet, not consuming the 24h window")
		r.wait(now.Add(failureCooldown))
	default:
		// Kept without expiry, so a quiet instance still knows when it last reported.
		value, _ := json.Marshal(strconv.FormatInt(now.UnixMilli(), 10))
		if err := r.cache.Set(ctx, lastSentKey, value, 0).Err(); err != nil {
			r.log.WarnContext(ctx, "Failed to record the usage update", "err", err)
		}
		r.wait(now.Add(interval))
	}
}

func (r *Reporter) wait(until time.Time) {
	r.mu.Lock()
	r.nextCheck = until
	r.mu.Unlock()
}

// lastSent reads when any process last reported; zero when none has. forma2 stored the milliseconds as a JSON string.
func (r *Reporter) lastSent(ctx context.Context) (time.Time, error) {
	raw, err := r.cache.Get(ctx, lastSentKey).Result()
	if errors.Is(err, redis.Nil) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	var text string
	if json.Unmarshal([]byte(raw), &text) != nil {
		text = raw
	}
	ms, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return time.Time{}, nil
	}
	return time.UnixMilli(ms), nil
}

// send posts the update; false means there is no organization to report on yet. A rejected update is an error, so it is not recorded as sent.
func (r *Reporter) send(ctx context.Context, lastSent time.Time) (bool, error) {
	payload, instanceID, err := r.collect(ctx, lastSent)
	if err != nil || payload == nil {
		return false, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return false, fmt.Errorf("encode usage update: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf(r.endpoint, instanceID), bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("build usage update request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := r.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("send usage update: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return false, fmt.Errorf("usage update endpoint responded with status %d", res.StatusCode)
	}
	return true, nil
}

// Payload is forma2's usage update, schema version 1.
type Payload struct {
	SchemaVersion                     int            `json:"schemaVersion"`
	OrganizationCount                 int64          `json:"organizationCount"`
	UserCount                         int64          `json:"userCount"`
	TeamCount                         int64          `json:"teamCount"`
	WorkspaceCount                    int64          `json:"workspaceCount"`
	SurveyCount                       int64          `json:"surveyCount"`
	InProgressSurveyCount             int64          `json:"inProgressSurveyCount"`
	CompletedSurveyCount              int64          `json:"completedSurveyCount"`
	ResponseCountAllTime              int64          `json:"responseCountAllTime"`
	ResponseCountSinceLastUsageUpdate int64          `json:"responseCountSinceLastUsageUpdate"`
	DisplayCount                      int64          `json:"displayCount"`
	ContactCount                      int64          `json:"contactCount"`
	SegmentCount                      int64          `json:"segmentCount"`
	Workflows                         Workflows      `json:"workflows"`
	Integrations                      Integrations   `json:"integrations"`
	Infrastructure                    Infrastructure `json:"infrastructure"`
	Security                          Security       `json:"security"`
	SSO                               SSO            `json:"sso"`
	Meta                              Meta           `json:"meta"`
	Temporal                          Temporal       `json:"temporal"`
}

type Workflows struct {
	WorkflowCount                      int64    `json:"workflowCount"`
	EnabledWorkflowCount               int64    `json:"enabledWorkflowCount"`
	RunCountSinceLastUsageUpdate       int64    `json:"runCountSinceLastUsageUpdate"`
	FailedRunCountSinceLastUsageUpdate int64    `json:"failedRunCountSinceLastUsageUpdate"`
	TriggerTypes                       []string `json:"triggerTypes"`
	ActionTypes                        []string `json:"actionTypes"`
}

type Integrations struct {
	Notion       bool `json:"notion"`
	GoogleSheets bool `json:"googleSheets"`
	Airtable     bool `json:"airtable"`
	Slack        bool `json:"slack"`
}

type Infrastructure struct {
	SMTP       bool `json:"smtp"`
	S3         bool `json:"s3"`
	Prometheus bool `json:"prometheus"`
}

type Security struct {
	Recaptcha bool `json:"recaptcha"`
}

type SSO struct {
	GitHub  bool `json:"github"`
	Google  bool `json:"google"`
	AzureAD bool `json:"azureAd"`
	OIDC    bool `json:"oidc"`
	SAML    bool `json:"saml"`
}

type Meta struct {
	Version string `json:"version"`
}

type Temporal struct {
	InstanceCreatedAt string  `json:"instanceCreatedAt"`
	NewestResponseAt  *string `json:"newestResponseAt"`
}

type counts struct {
	OrganizationCount, UserCount, TeamCount, WorkspaceCount                        int64
	SurveyCount, InProgressSurveyCount, CompletedSurveyCount                         int64
	ResponseCountAllTime, ResponseCountSinceLastUpdate                               int64
	DisplayCount, ContactCount, SegmentCount                                         int64
	NewestResponseAt                                                                 *time.Time
	WorkflowCount, EnabledWorkflowCount                                              int64
	WorkflowRunCountSinceLastUpdate, WorkflowRunFailedCountSinceLastUpdate           int64
}

// collect reads the update; a nil payload means there is no organization yet. The instance id is the SHA-256 of the oldest organization's id, so it never changes once an instance has one.
func (r *Reporter) collect(ctx context.Context, lastSent time.Time) (*Payload, string, error) {
	db := r.db.WithContext(ctx)
	var oldest struct {
		ID        string
		CreatedAt time.Time
	}
	res := db.Raw(`SELECT id::text AS id, created_at FROM organizations ORDER BY created_at, id LIMIT 1`).Scan(&oldest)
	if res.Error != nil {
		return nil, "", fmt.Errorf("read the oldest organization: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, "", nil
	}
	sum := sha256.Sum256([]byte(oldest.ID))

	var c counts
	err := db.Raw(`SELECT
		(SELECT count(*) FROM organizations) AS organization_count,
		(SELECT count(*) FROM users) AS user_count,
		(SELECT count(*) FROM teams) AS team_count,
		(SELECT count(*) FROM workspaces) AS workspace_count,
		(SELECT count(*) FROM surveys) AS survey_count,
		(SELECT count(*) FROM surveys WHERE status = 'inProgress') AS in_progress_survey_count,
		(SELECT count(*) FROM surveys WHERE status = 'completed') AS completed_survey_count,
		(SELECT count(*) FROM responses) AS response_count_all_time,
		(SELECT count(*) FROM responses WHERE created_at > @since) AS response_count_since_last_update,
		(SELECT count(*) FROM displays) AS display_count,
		(SELECT count(*) FROM contacts) AS contact_count,
		(SELECT count(*) FROM segments) AS segment_count,
		(SELECT max(created_at) FROM responses) AS newest_response_at,
		(SELECT count(*) FROM workflows WHERE status <> 'archived') AS workflow_count,
		(SELECT count(*) FROM workflows WHERE status = 'enabled') AS enabled_workflow_count,
		(SELECT count(*) FROM workflow_runs WHERE NOT is_dry_run AND created_at > @since) AS workflow_run_count_since_last_update,
		-- Failures are windowed on finished_at: a run created in this window can fail after this read, and every terminal write sets finished_at, so each failure counts once, in the window it settled in.
		(SELECT count(*) FROM workflow_runs WHERE NOT is_dry_run AND status = 'failed' AND finished_at > @since) AS workflow_run_failed_count_since_last_update`,
		map[string]any{"since": lastSent}).Scan(&c).Error
	if err != nil {
		return nil, "", fmt.Errorf("count usage: %w", err)
	}
	var integrationTypes, providers []string
	if err := db.Raw(`SELECT DISTINCT type FROM integrations`).Scan(&integrationTypes).Error; err != nil {
		return nil, "", fmt.Errorf("read integration types: %w", err)
	}
	if err := db.Raw(`SELECT DISTINCT provider FROM user_identities`).Scan(&providers).Error; err != nil {
		return nil, "", fmt.Errorf("read sign-in providers: %w", err)
	}
	triggers, actions := r.workflowNodeTypes(ctx)

	sso := r.cfg.SSO
	p := &Payload{
		SchemaVersion:     1,
		OrganizationCount: c.OrganizationCount, UserCount: c.UserCount, TeamCount: c.TeamCount, WorkspaceCount: c.WorkspaceCount,
		SurveyCount: c.SurveyCount, InProgressSurveyCount: c.InProgressSurveyCount, CompletedSurveyCount: c.CompletedSurveyCount,
		ResponseCountAllTime: c.ResponseCountAllTime, ResponseCountSinceLastUsageUpdate: c.ResponseCountSinceLastUpdate,
		DisplayCount: c.DisplayCount, ContactCount: c.ContactCount, SegmentCount: c.SegmentCount,
		Workflows: Workflows{
			WorkflowCount: c.WorkflowCount, EnabledWorkflowCount: c.EnabledWorkflowCount,
			RunCountSinceLastUsageUpdate: c.WorkflowRunCountSinceLastUpdate, FailedRunCountSinceLastUsageUpdate: c.WorkflowRunFailedCountSinceLastUpdate,
			TriggerTypes: triggers, ActionTypes: actions,
		},
		Integrations: Integrations{
			Notion: slices.Contains(integrationTypes, "notion"), GoogleSheets: slices.Contains(integrationTypes, "googleSheets"),
			Airtable: slices.Contains(integrationTypes, "airtable"), Slack: slices.Contains(integrationTypes, "slack"),
		},
		Infrastructure: Infrastructure{SMTP: r.cfg.SMTPHost != "", S3: r.cfg.S3.Configured(), Prometheus: r.cfg.Telemetry.PrometheusEnabled},
		Security:       Security{Recaptcha: r.cfg.RecaptchaConfigured()},
		SSO: SSO{
			GitHub: sso.GitHubID != "" || slices.Contains(providers, "github"), Google: sso.GoogleClientID != "" || slices.Contains(providers, "google"),
			AzureAD: sso.AzureADClientID != "" || slices.Contains(providers, "azuread"), OIDC: sso.OIDCClientID != "" || slices.Contains(providers, "openid"),
			SAML: slices.Contains(providers, "saml"),
		},
		Meta:     Meta{Version: buildinfo.Version},
		Temporal: Temporal{InstanceCreatedAt: isoTime(oldest.CreatedAt)},
	}
	if c.NewestResponseAt != nil {
		newest := isoTime(*c.NewestResponseAt)
		p.Temporal.NewestResponseAt = &newest
	}
	return p, hex.EncodeToString(sum[:]), nil
}

// workflowNodeTypes are the distinct trigger and step types of the live workflows, read from their definitions so a new node kind reports itself. A malformed definition must not cost the whole report, so a failure is logged and reported as none.
func (r *Reporter) workflowNodeTypes(ctx context.Context) ([]string, []string) {
	var triggerJSON, actionJSON []byte
	err := r.db.WithContext(ctx).Raw(`SELECT
		COALESCE((SELECT jsonb_agg(DISTINCT w.definition->'trigger'->>'triggerType') FROM workflows w
			WHERE w.status <> 'archived' AND w.definition->'trigger'->>'triggerType' IS NOT NULL), '[]'),
		COALESCE((SELECT jsonb_agg(DISTINCT COALESCE(n->>'actionType', n->>'type'))
			FROM (SELECT definition FROM workflows WHERE status <> 'archived' AND jsonb_typeof(definition->'nodes') = 'array') w,
				jsonb_array_elements(w.definition->'nodes') n
			WHERE COALESCE(n->>'actionType', n->>'type') IS NOT NULL), '[]')`).Row().Scan(&triggerJSON, &actionJSON)
	triggers, actions := []string{}, []string{}
	if err == nil {
		err = errors.Join(json.Unmarshal(triggerJSON, &triggers), json.Unmarshal(actionJSON, &actions))
	}
	if err != nil {
		r.log.WarnContext(ctx, "Failed to read workflow node types for the usage update", "err", err)
		return []string{}, []string{}
	}
	slices.Sort(triggers)
	slices.Sort(actions)
	return triggers, actions
}

// isoTime is JavaScript's Date.toISOString.
func isoTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// Args is forma2's usage-telemetry.process job.
type Args struct{}

func (Args) Kind() string { return "usage-telemetry.process" }

type worker struct {
	river.WorkerDefaults[Args]
	reporter *Reporter
	log      *slog.Logger
}

func (w *worker) Work(ctx context.Context, job *river.Job[Args]) error {
	logContext := []any{"attempt", job.Attempt, "jobId", job.ID, "jobName", job.Kind, "maxAttempts", job.MaxAttempts, "queueName", job.Queue}
	w.log.InfoContext(ctx, "Usage telemetry job started", logContext...)
	w.reporter.Send(ctx)
	w.log.InfoContext(ctx, "Usage telemetry job completed", logContext...)
	return nil
}

// RegisterJobs adds the daily report at 02:15 UTC, off the survey sweeps. It also runs when the worker starts, as forma2's first registration did, so an instance that is not up at 02:15 still reports; the shared last-sent time makes that a single Valkey read when a report already went out.
func RegisterJobs(reg *jobs.Registry, reporter *Reporter, log *slog.Logger) {
	jobs.Add(reg, &worker{reporter: reporter, log: log})
	reg.AddPeriodic(river.NewPeriodicJob(jobs.DailyUTC{Hour: 2, Minute: 15}, func() (river.JobArgs, *river.InsertOpts) {
		return Args{}, nil
	}, &river.PeriodicJobOpts{ID: "usage-telemetry", RunOnStart: true}))
}
