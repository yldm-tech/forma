package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river/rivertype"

	"github.com/yldm-tech/forma/server/internal/followup"
	"github.com/yldm-tech/forma/server/internal/notification"
	"github.com/yldm-tech/forma/server/internal/pipeline"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/ssrf"
	"github.com/yldm-tech/forma/server/internal/response"
	"github.com/yldm-tech/forma/server/internal/webhook"
)

// enabled creates a workflow with the definition through the API and enables it.
func (e *env) enabled(f fixture, name string, def map[string]any) uuid.UUID {
	e.t.Helper()
	w := e.create(f.owner, f.ws, name, def)
	e.ok(e.lifecycle(f.owner, w.ID, "enable"), http.StatusOK, nil)
	return w.ID
}

// twoSteps is a definition that emails the respondent (the q1 answer) and then a literal recipient.
func twoSteps(survey uuid.UUID, literal string) map[string]any {
	d := definition(survey, "q1")
	second := emailNode("notify-owner")
	cfg := second["config"].(map[string]any)
	cfg["to"], cfg["from"], cfg["subject"], cfg["body"] = literal, "team@acme.example", "New\r\nBcc: x@evil.example response", "<p>Answer: #recall:q1/fallback:none#</p>"
	d["nodes"] = append(d["nodes"].([]any), second)
	d["edges"] = append(d["edges"].([]any), map[string]any{"id": "e2", "source": "send-email", "target": "notify-owner"})
	return d
}

func (e *env) event(f fixture, r response.Response) *pipeline.Event {
	return &pipeline.Event{Name: response.EventResponseFinished, JobKey: "key", WorkspaceID: f.ws, Response: r, OrganizationID: f.org, DisplayTimeZone: "UTC"}
}

func (e *env) runFor(workflowID, responseID uuid.UUID) uuid.UUID {
	e.t.Helper()
	var ids []uuid.UUID
	if err := e.db.Raw(`SELECT id FROM workflow_runs WHERE workflow_id = ? AND response_id = ?`, workflowID, responseID).Scan(&ids).Error; err != nil || len(ids) != 1 {
		e.t.Fatalf("runs of %s for %s: %v %v", workflowID, responseID, ids, err)
	}
	return ids[0]
}

func (e *env) jobsFor(run uuid.UUID) int {
	return e.count(`SELECT count(*) FROM river_job WHERE kind = 'workflow-run.process' AND args ->> 'workflowRunId' = ?`, run.String())
}

func TestEnqueueResponseCompleted(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	all := e.enabled(f, "All endings", definition(f.survey, "q1"))
	oneEnding := e.enabled(f, "One ending", definition(f.survey, "q1", testEndingID))
	unnarrowed := e.enabled(f, "Unnarrowed", definition(f.survey, "q1"))
	e.exec(`UPDATE workflows SET trigger_survey_id = NULL WHERE id = ?`, unnarrowed)
	disabled := e.enabled(f, "Disabled", definition(f.survey, "q1"))
	e.ok(e.lifecycle(f.owner, disabled, "disable"), http.StatusOK, nil)
	otherSurvey := e.insert(`INSERT INTO surveys (workspace_id, name, status, endings) VALUES (?, 'Other', 'inProgress', ?::jsonb) RETURNING id`, f.ws, `[{"id":"`+testEndingID+`","type":"endScreen"}]`)
	e.enabled(f, "Other survey", definition(otherSurvey, "q1"))
	unpublished := e.insert(`INSERT INTO workflows (workspace_id, name, status, definition) VALUES (?, 'Unpublished', 'enabled', ?::jsonb) RETURNING id`, f.ws, string(raw(t, definition(f.survey, "q1"))))

	ending := testEndingID
	r := e.respond(f.survey, true, `{"q1":"jane@example.com"}`, &ending)
	if err := e.enqueuer.EnqueueResponseCompleted(context.Background(), e.event(f, r)); err != nil {
		t.Fatal(err)
	}
	if e.count(`SELECT count(*) FROM workflow_runs`) != 3 {
		t.Fatalf("%d runs", e.count(`SELECT count(*) FROM workflow_runs`))
	}
	run := e.runFor(all, r.ID)
	e.runFor(oneEnding, r.ID)
	e.runFor(unnarrowed, r.ID)
	if !strings.Contains(e.logs.String(), "Enabled workflow has no published version") || !strings.Contains(e.logs.String(), unpublished.String()) {
		t.Fatal("the unpublished workflow was not reported")
	}
	var stored struct {
		Status, TriggerType, IdempotencyKey string
		Attempt                             int
		IsDryRun                            bool
		VersionMatches                      bool
		Dispatched                          bool
		TriggerPayload                      []byte
	}
	if err := e.db.Raw(`SELECT r.status, r.trigger_type, r.idempotency_key, r.attempt, r.is_dry_run, r.dispatched_at IS NOT NULL AS dispatched, r.trigger_payload,
		r.workflow_version_id = (SELECT id FROM workflow_versions v WHERE v.workflow_id = r.workflow_id) AS version_matches FROM workflow_runs r WHERE r.id = ?`, run).Scan(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != RunQueued || stored.TriggerType != TriggerResponseCompleted || stored.IdempotencyKey != r.ID.String() || stored.Attempt != 0 || stored.IsDryRun || !stored.VersionMatches || !stored.Dispatched {
		t.Fatalf("run %+v", stored)
	}
	var payload map[string]any
	if err := json.Unmarshal(stored.TriggerPayload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["type"] != TriggerResponseCompleted || payload["workspaceId"] != f.ws.String() || payload["surveyId"] != f.survey.String() || payload["responseId"] != r.ID.String() ||
		payload["endingCardId"] != testEndingID || payload["triggeredAt"] != jsvalue.ISOString(r.UpdatedAt) || payload["data"].(map[string]any)["q1"] != "jane@example.com" {
		t.Fatalf("payload %v", payload)
	}
	if e.jobsFor(run) != 1 {
		t.Fatalf("%d jobs for the run", e.jobsFor(run))
	}

	// A replayed event creates no run and no job.
	if err := e.enqueuer.EnqueueResponseCompleted(context.Background(), e.event(f, r)); err != nil {
		t.Fatal(err)
	}
	if e.count(`SELECT count(*) FROM workflow_runs`) != 3 || e.jobsFor(run) != 1 {
		t.Fatal("a replay duplicated work")
	}

	// No ending matches only the workflows that take every ending; an ending id no survey could have is left out of the payload.
	bogus := "../../not an id"
	for _, ending := range []*string{nil, &bogus} {
		r := e.respond(f.survey, true, `{}`, ending)
		if err := e.enqueuer.EnqueueResponseCompleted(context.Background(), e.event(f, r)); err != nil {
			t.Fatal(err)
		}
		if e.count(`SELECT count(*) FROM workflow_runs WHERE response_id = ?`, r.ID) != 2 || e.count(`SELECT count(*) FROM workflow_runs WHERE response_id = ? AND workflow_id = ?`, r.ID, oneEnding) != 0 {
			t.Fatalf("runs for ending %v", ending)
		}
		if e.count(`SELECT count(*) FROM workflow_runs WHERE response_id = ? AND trigger_payload -> 'endingCardId' IS NOT NULL`, r.ID) != 0 {
			t.Fatal("an invalid ending id reached the payload")
		}
	}
	if !strings.Contains(e.logs.String(), "Response ending id is not a valid ending card id") {
		t.Fatal("the invalid ending id was not reported")
	}

	unfinished := e.respond(f.survey, false, `{}`, nil)
	if err := e.enqueuer.EnqueueResponseCompleted(context.Background(), e.event(f, unfinished)); err != nil || e.count(`SELECT count(*) FROM workflow_runs WHERE response_id = ?`, unfinished.ID) != 0 {
		t.Fatalf("an unfinished response started runs: %v", err)
	}
}

func TestMatchingIsTotal(t *testing.T) {
	survey := uuid.New()
	ending := testEndingID
	for _, def := range []string{`null`, `{}`, `{"trigger":null}`, `{"trigger":{"triggerType":"other","config":{"surveyId":"` + survey.String() + `","endingCardIds":[]}}}`,
		`{"trigger":{"triggerType":"response.completed","config":{"surveyId":"` + survey.String() + `"}}}`, `{"trigger":{"triggerType":"response.completed","config":{"surveyId":1,"endingCardIds":[]}}}`,
		`{"trigger":{"triggerType":"response.completed","config":{"surveyId":"` + survey.String() + `","endingCardIds":[1]}}}`} {
		if readTriggerConfig(json.RawMessage(def)) != nil {
			t.Errorf("%s read as a trigger", def)
		}
	}
	cfg := readTriggerConfig(json.RawMessage(`{"trigger":{"triggerType":"response.completed","config":{"surveyId":"cm9zr4mps000008l8btfy1vtz","endingCardIds":["` + ending + `"]}}}`))
	legacy := uuid.NewSHA1(uuid.MustParse("027acbf8-3f2c-52c4-9593-7683c120c88b"), []byte("cm9zr4mps000008l8btfy1vtz"))
	if cfg == nil || !cfg.matches(legacy, &ending) || cfg.matches(legacy, nil) || cfg.matches(survey, &ending) {
		t.Fatal("a pre-migration survey id does not match its survey")
	}
}

func (e *env) mails(to string) []mailer.Message {
	var out []mailer.Message
	for _, m := range e.mail.Messages() {
		if m.To == to {
			out = append(out, m)
		}
	}
	return out
}

func TestRunSendsEveryStepOnce(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	wf := e.enabled(f, "Two steps", twoSteps(f.survey, "Owner@Example.com"))
	lang := "ja-JP"
	r := e.respond(f.survey, true, `{"q1":"jane@example.com"}`, nil)
	e.exec(`UPDATE responses SET language = ? WHERE id = ?`, lang, r.ID)
	if err := e.enqueuer.EnqueueResponseCompleted(context.Background(), e.event(f, r)); err != nil {
		t.Fatal(err)
	}
	run := e.runFor(wf, r.ID)
	if err := e.runner.Work(context.Background(), runJob(run, wf, f.ws, 1)); err != nil {
		t.Fatal(err)
	}
	var state struct {
		Status    string
		Attempt   int
		Data      []byte
		Started   bool
		Finished  bool
		ErrorText *string
	}
	e.db.Raw(`SELECT status, attempt, data, started_at IS NOT NULL AS started, finished_at IS NOT NULL AS finished, error AS error_text FROM workflow_runs WHERE id = ?`, run).Scan(&state)
	if state.Status != RunCompleted || state.Attempt != 1 || !state.Started || !state.Finished || state.ErrorText != nil {
		t.Fatalf("run %+v\n%s", state, e.errorLogs())
	}
	var data runDataDoc
	if err := json.Unmarshal(state.Data, &data); err != nil || len(data.Steps) != 2 || data.Steps[1].Status != StepSucceeded || !strings.Contains(string(data.Trigger), r.ID.String()) {
		t.Fatalf("data %s", state.Data)
	}
	var logs []RunLog
	e.db.Raw(`SELECT sequence, step_id, status, input, output FROM workflow_run_logs WHERE run_id = ? ORDER BY sequence`, run).Scan(&logs)
	if len(logs) != 2 || logs[0].Sequence != 1 || logs[0].StepID != "send-email" || logs[1].Sequence != 2 || logs[0].Status != StepSucceeded ||
		!strings.Contains(string(logs[0].Input), `"to": "q1"`) || !strings.Contains(string(logs[1].Output), `"provider": "smtp"`) {
		t.Fatalf("logs %+v", logs)
	}

	respondent := e.mails("jane@example.com")
	owner := e.mails("Owner@Example.com")
	if len(respondent) != 1 || len(owner) != 1 {
		t.Fatalf("sent %+v", e.mail.Messages())
	}
	m := respondent[0]
	if m.Subject != "Thanks for your response" || m.ReplyTo != "support@example.com, help@example.com" || m.From != `"Forma" <noreply@forma.ylam.ai>` ||
		m.MessageID != MessageID(run, "send-email", "noreply@example.com") || !strings.Contains(string(logs[0].Output), m.MessageID) {
		t.Fatalf("respondent mail %+v", m)
	}
	if !strings.Contains(m.HTML, "Hello jane@example.com") || strings.Contains(m.HTML, "<script>") || !strings.Contains(m.HTML, `lang="ja-JP"`) {
		t.Fatalf("respondent body %s", m.HTML)
	}
	if owner[0].Subject != "NewBcc: x@evil.example response" || owner[0].MessageID != MessageID(run, "notify-owner", "team@acme.example") || !strings.HasSuffix(owner[0].MessageID, "@acme.example>") {
		t.Fatalf("owner mail %+v", owner[0])
	}

	// Another delivery of the job finds the run terminal and sends nothing.
	if err := e.runner.Work(context.Background(), runJob(run, wf, f.ws, 1)); err != nil || len(e.mail.Messages()) != 2 {
		t.Fatalf("a redelivery sent again: %v", err)
	}
	// A job naming another workspace, or a run that does not exist, is dropped.
	if err := e.runner.Work(context.Background(), runJob(run, wf, uuid.New(), 1)); err != nil {
		t.Fatal(err)
	}
	if err := e.runner.Work(context.Background(), runJob(uuid.New(), wf, f.ws, 1)); err != nil || !strings.Contains(e.logs.String(), "Workflow run not found for workspace; dropping job") {
		t.Fatalf("missing run: %v", err)
	}
}

// An organization without a whitelabel logo, every new one, has a NULL logo URL; the run still sends.
func TestRunWithoutWhitelabelLogo(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	e.exec(`UPDATE organizations SET whitelabel = '{}' WHERE id = ?`, f.org)
	wf := e.enabled(f, "No logo", definition(f.survey, "owner@example.com"))
	r := e.respond(f.survey, true, `{"q1":"jane@example.com"}`, nil)
	if err := e.enqueuer.EnqueueResponseCompleted(context.Background(), e.event(f, r)); err != nil {
		t.Fatal(err)
	}
	run := e.runFor(wf, r.ID)
	if err := e.runner.Work(context.Background(), runJob(run, wf, f.ws, 1)); err != nil {
		t.Fatal(err)
	}
	var status string
	e.db.Raw(`SELECT status FROM workflow_runs WHERE id = ?`, run).Scan(&status)
	if status != RunCompleted || len(e.mails("owner@example.com")) != 1 {
		t.Fatalf("run %s, sent %+v\n%s", status, e.mail.Messages(), e.errorLogs())
	}
}

func TestRunRecipientAllowlistAndRetries(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	member := e.user("member@example.com")
	e.member(f.org, member, "manager")
	wf := e.enabled(f, "To a manager", twoSteps(f.survey, "member@example.com"))
	// The manager is demoted after enable: the runner's own check refuses the literal recipient.
	e.exec(`UPDATE memberships SET role = 'member' WHERE user_id = ?`, member)
	r := e.respond(f.survey, true, `{"q1":"jane@example.com"}`, nil)
	if err := e.enqueuer.EnqueueResponseCompleted(context.Background(), e.event(f, r)); err != nil {
		t.Fatal(err)
	}
	run := e.runFor(wf, r.ID)
	err := e.runner.Work(context.Background(), runJob(run, wf, f.ws, 1))
	if err == nil || err.Error() != "Recipient member@example.com cannot access this workspace" {
		t.Fatalf("first attempt: %v", err)
	}
	status := func() (string, string) {
		return e.text(`SELECT status FROM workflow_runs WHERE id = ?`, run), e.text(`SELECT error FROM workflow_runs WHERE id = ?`, run)
	}
	if s, msg := status(); s != RunRunning || msg != err.Error() || e.count(`SELECT count(*) FROM workflow_runs WHERE id = ? AND last_error_at IS NOT NULL AND finished_at IS NULL`, run) != 1 {
		t.Fatalf("after a non-final attempt: %s %s", s, msg)
	}
	if !strings.Contains(e.text(`SELECT data::text FROM workflow_runs WHERE id = ?`, run), `"status": "failed"`) {
		t.Fatal("the partial trace was not recorded")
	}
	// The retry resumes: the respondent's succeeded step is not sent again, the failed one is claimed again and fails again; the last attempt fails the run and swallows the error.
	if err := e.runner.Work(context.Background(), runJob(run, wf, f.ws, 3)); err != nil {
		t.Fatalf("final attempt: %v", err)
	}
	if s, _ := status(); s != RunFailed || e.count(`SELECT count(*) FROM workflow_runs WHERE id = ? AND finished_at IS NOT NULL`, run) != 1 {
		t.Fatalf("after the final attempt: %s", s)
	}
	if len(e.mails("jane@example.com")) != 1 || len(e.mails("member@example.com")) != 0 {
		t.Fatalf("sent %+v", e.mail.Messages())
	}
	if e.text(`SELECT status FROM workflow_run_logs WHERE run_id = ? AND step_id = 'notify-owner'`, run) != StepFailed {
		t.Fatal("the refused step is not failed")
	}
}

func TestRunStepOwnership(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	wf := e.enabled(f, "Two steps", twoSteps(f.survey, "owner@example.com"))
	r := e.respond(f.survey, true, `{"q1":"jane@example.com"}`, nil)
	if err := e.enqueuer.EnqueueResponseCompleted(context.Background(), e.event(f, r)); err != nil {
		t.Fatal(err)
	}
	run := e.runFor(wf, r.ID)
	// A step another delivery holds (running) is never sent twice, and the run is left for that delivery to finish.
	e.exec(`UPDATE workflow_runs SET status = 'running' WHERE id = ?`, run)
	e.exec(`INSERT INTO workflow_run_logs (run_id, sequence, step_id, step_type, status) VALUES (?, 1, 'send-email', 'send_email', 'running')`, run)
	if err := e.runner.Work(context.Background(), runJob(run, wf, f.ws, 2)); err != nil {
		t.Fatal(err)
	}
	if len(e.mail.Messages()) != 0 || e.text(`SELECT status FROM workflow_runs WHERE id = ?`, run) != RunRunning {
		t.Fatal("a held step was sent or the run finalized")
	}
	// A succeeded step is resumed from its log.
	e.exec(`UPDATE workflow_run_logs SET status = 'succeeded', output = '{"messageId":"<x@y>","provider":"smtp"}', finished_at = now() WHERE run_id = ?`, run)
	if err := e.runner.Work(context.Background(), runJob(run, wf, f.ws, 2)); err != nil {
		t.Fatal(err)
	}
	if len(e.mails("jane@example.com")) != 0 || len(e.mails("owner@example.com")) != 1 || e.text(`SELECT status FROM workflow_runs WHERE id = ?`, run) != RunCompleted {
		t.Fatalf("resume sent %+v", e.mail.Messages())
	}
	if !strings.Contains(e.text(`SELECT data::text FROM workflow_runs WHERE id = ?`, run), `<x@y>`) {
		t.Fatal("the resumed step's result is not in the run data")
	}
}

func TestRunFailures(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	wf := e.enabled(f, "Respondent", definition(f.survey, "q1"))

	run := func(data string) uuid.UUID {
		r := e.respond(f.survey, true, data, nil)
		if err := e.enqueuer.EnqueueResponseCompleted(context.Background(), e.event(f, r)); err != nil {
			t.Fatal(err)
		}
		return e.runFor(wf, r.ID)
	}
	final := func(id uuid.UUID, runner *Runner) string {
		t.Helper()
		if err := runner.Work(context.Background(), runJob(id, wf, f.ws, 3)); err != nil {
			t.Fatal(err)
		}
		if e.text(`SELECT status FROM workflow_runs WHERE id = ?`, id) != RunFailed {
			t.Fatalf("run %s not failed", id)
		}
		return e.text(`SELECT error FROM workflow_runs WHERE id = ?`, id)
	}
	if msg := final(run(`{}`), e.runner); msg != "Recipient value not found in response data" {
		t.Fatalf("no recipient: %s", msg)
	}
	if msg := final(run(`{"q1":"not an address"}`), e.runner); msg != "Resolved email recipient is not a valid address" {
		t.Fatalf("bad recipient: %s", msg)
	}
	e.mail.Err = errors.New("relay down")
	if msg := final(run(`{"q1":"jane@example.com"}`), e.runner); msg != "Failed to send email: relay down" {
		t.Fatalf("send failure: %s", msg)
	}
	unconfigured, err := mailer.NewWithTransport(e.cfg, nil, e.log)
	if err != nil {
		t.Fatal(err)
	}
	if msg := final(run(`{"q1":"jane@example.com"}`), NewRunner(e.cfg, e.db, unconfigured, e.log)); msg != smtpNotConfigured {
		t.Fatalf("no SMTP: %s", msg)
	}
	// A payload that names a response of another survey is refused before anything renders.
	other := run(`{"q1":"jane@example.com"}`)
	foreign := e.insert(`INSERT INTO surveys (workspace_id, name, status) VALUES (?, 'Other', 'inProgress') RETURNING id`, f.ws)
	e.exec(`UPDATE workflow_runs SET trigger_payload = jsonb_set(trigger_payload, '{surveyId}', to_jsonb(?::text)) WHERE id = ?`, foreign.String(), other)
	if msg := final(other, e.runner); !strings.Contains(msg, "does not belong to survey "+foreign.String()) {
		t.Fatalf("foreign survey: %s", msg)
	}
	// A run whose snapshot is not executable fails with forma2's message.
	broken := run(`{"q1":"jane@example.com"}`)
	e.exec(`UPDATE workflow_versions SET definition = '{"trigger":null}' WHERE workflow_id = ?`, wf)
	if msg := final(broken, e.runner); msg != definitionNotRunnable {
		t.Fatalf("not executable: %s", msg)
	}
}

func TestReconciler(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	wf := e.enabled(f, "Respondent", definition(f.survey, "q1"))
	now := time.Now()
	insertRun := func(status string, created time.Duration, dispatched, dryRun bool) uuid.UUID {
		r := e.respond(f.survey, true, `{}`, nil)
		var dispatchedAt *time.Time
		if dispatched {
			at := now.Add(-created)
			dispatchedAt = &at
		}
		return e.insert(`INSERT INTO workflow_runs (workflow_id, workspace_id, trigger_type, trigger_payload, response_id, idempotency_key, status, is_dry_run, dispatched_at, created_at, updated_at)
			VALUES (?, ?, 'response.completed', '{}', ?, ?, ?, ?, ?, ?, ?) RETURNING id`, wf, f.ws, r.ID, r.ID.String(), status, dryRun, dispatchedAt, now.Add(-created), now.Add(-created))
	}
	young := insertRun(RunQueued, time.Minute, false, false)
	orphan := insertRun(RunQueued, 5*time.Minute, false, false)
	lagging := insertRun(RunQueued, 5*time.Minute, true, false)
	dry := insertRun(RunQueued, 5*time.Minute, false, true)
	agedDispatched := insertRun(RunQueued, 25*time.Hour, true, false)
	agedNever := insertRun(RunQueued, 25*time.Hour, false, false)
	stuck := insertRun(RunRunning, 2*time.Hour, true, false)
	busy := insertRun(RunRunning, 10*time.Minute, true, false)
	e.exec(`INSERT INTO workflow_run_logs (run_id, sequence, step_id, step_type, status) VALUES (?, 1, 'send-email', 'send_email', 'running'), (?, 1, 'send-email', 'send_email', 'running')`, stuck, busy)

	queued, err := e.reconciler.Orphans(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if queued != (OrphanResult{Scanned: 4, Redispatched: 2, AgedOutFailed: 2, NeverDispatched: 1}) {
		t.Fatalf("orphans %+v", queued)
	}
	if e.jobsFor(orphan) != 1 || e.jobsFor(lagging) != 1 || e.jobsFor(young) != 0 || e.jobsFor(dry) != 0 {
		t.Fatal("dispatch went to the wrong runs")
	}
	if e.count(`SELECT count(*) FROM workflow_runs WHERE id = ? AND dispatched_at IS NOT NULL`, orphan) != 1 {
		t.Fatal("the orphan was not stamped")
	}
	if e.text(`SELECT error FROM workflow_runs WHERE id = ?`, agedDispatched) != agedOutDispatched || e.text(`SELECT error FROM workflow_runs WHERE id = ?`, agedNever) != agedOutUndispatched ||
		e.count(`SELECT count(*) FROM workflow_runs WHERE id IN (?, ?) AND status = 'failed' AND finished_at IS NOT NULL`, agedDispatched, agedNever) != 2 {
		t.Fatal("aged-out runs not failed")
	}
	// Dispatching again is idempotent.
	if _, err := e.reconciler.Orphans(context.Background(), now); err != nil || e.jobsFor(orphan) != 1 {
		t.Fatalf("second sweep: %v, %d jobs", err, e.jobsFor(orphan))
	}

	running, err := e.reconciler.Stuck(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if running != (StuckResult{Scanned: 1, Recovered: 1, StepsSkipped: 1}) {
		t.Fatalf("stuck %+v", running)
	}
	if e.text(`SELECT error FROM workflow_runs WHERE id = ?`, stuck) != abandonedRun || e.text(`SELECT status FROM workflow_run_logs WHERE run_id = ?`, stuck) != StepSkipped ||
		e.text(`SELECT error FROM workflow_run_logs WHERE run_id = ?`, stuck) != abandonedStep || e.text(`SELECT status FROM workflow_runs WHERE id = ?`, busy) != RunRunning {
		t.Fatal("stuck runs not recovered as forma2 did")
	}
	if err := e.reconciler.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestRiverEndToEnd runs real River workers from a finished response's pipeline job to a completed run with its email sent.
func TestRiverEndToEnd(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	wf := e.enabled(f, "Respondent", twoSteps(f.survey, "owner@example.com"))
	r := e.respond(f.survey, true, `{"q1":"jane@example.com"}`, nil)

	pool, cleanup, err := jobs.OpenPool(context.Background(), e.cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	pipelineWorker := pipeline.NewWorker(e.cfg, e.db, webhook.NewDeliverer(webhook.NewRepository(e.db), ssrf.New(e.cfg)),
		followup.NewSender(e.cfg, e.db, e.mailer, ratelimit.New(e.cfg, nil, e.log), e.log), notification.NewNotifier(e.cfg, e.db, e.mailer, e.log),
		audit.New(e.cfg, e.log), pipeline.Handoffs{Workflows: e.enqueuer}, e.log)
	reg := jobs.NewRegistry(slog.New(slog.NewTextHandler(io.Discard, nil)))
	pipeline.RegisterJobs(reg, pipelineWorker)
	RegisterJobs(reg, e.runner, e.reconciler)
	worker, err := jobs.NewWorker(e.cfg, pool, reg, e.log)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = worker.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	inserter, err := jobs.NewInserter(pool, e.log)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inserter.Insert(context.Background(), response.PipelineArgs{Event: response.EventResponseFinished, WorkspaceID: f.ws, SurveyID: f.survey, Response: r}, nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the workflow run to complete", 60*time.Second, func() bool {
		var status string
		_ = e.db.Raw(`SELECT status FROM workflow_runs WHERE workflow_id = ? AND response_id = ?`, wf, r.ID).Row().Scan(&status)
		return status == RunCompleted
	})
	run := e.runFor(wf, r.ID)
	waitFor(t, "the run's job to complete", 30*time.Second, func() bool {
		var state string
		_ = pool.QueryRow(context.Background(), `SELECT state FROM river_job WHERE kind = 'workflow-run.process' AND args ->> 'workflowRunId' = $1`, run.String()).Scan(&state)
		return state == string(rivertype.JobStateCompleted)
	})
	if len(e.mails("jane@example.com")) != 1 || len(e.mails("owner@example.com")) != 1 {
		t.Fatalf("sent %+v\n%s", e.mail.Messages(), e.errorLogs())
	}
	if e.count(`SELECT count(*) FROM workflow_run_logs WHERE run_id = ? AND status = 'succeeded'`, run) != 2 {
		t.Fatal("the run's steps were not recorded")
	}
}

// cancellingTransport is an SMTP server the job's context ends on before anything is sent, as River cancels a run at runTimeout or SHUTDOWN_TIMEOUT after the worker is asked to stop.
type cancellingTransport struct {
	cancel context.CancelFunc
}

func (c *cancellingTransport) Send(ctx context.Context, _ mailer.Message) error {
	c.cancel()
	return ctx.Err()
}

// A step whose send the job's cancellation cut short is recorded as failed, with the run's error, so the retry sends it again rather than finding it running and abandoning the run.
func TestCancelledSendIsRetried(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	wf := e.enabled(f, "One step", definition(f.survey, "q1"))
	r := e.respond(f.survey, true, `{"q1":"jane@example.com"}`, nil)
	if err := e.enqueuer.EnqueueResponseCompleted(context.Background(), e.event(f, r)); err != nil {
		t.Fatal(err)
	}
	run := e.runFor(wf, r.ID)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	m, err := mailer.NewWithTransport(e.cfg, &cancellingTransport{cancel: cancel}, e.log)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewRunner(e.cfg, e.db, m, e.log).Work(ctx, runJob(run, wf, f.ws, 1)); err == nil {
		t.Fatal("a cancelled attempt succeeded")
	}
	if s := e.text(`SELECT status FROM workflow_run_logs WHERE run_id = ?`, run); s != StepFailed {
		t.Fatalf("the cut-short step is %s", s)
	}
	if e.count(`SELECT count(*) FROM workflow_runs WHERE id = ? AND last_error_at IS NOT NULL`, run) != 1 {
		t.Fatal("the run's failure was not recorded")
	}
	if err := e.runner.Work(context.Background(), runJob(run, wf, f.ws, 2)); err != nil {
		t.Fatal(err)
	}
	if s := e.text(`SELECT status FROM workflow_runs WHERE id = ?`, run); s != RunCompleted || len(e.mails("jane@example.com")) != 1 {
		t.Fatalf("the retry left the run %s, sent %+v\n%s", s, e.mail.Messages(), e.errorLogs())
	}
}
