package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/apps/server/internal/platform/rowid"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ssrf"
	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/responsemail"
)

// Failure messages a run records, forma2's.
const (
	smtpNotConfigured     = "SMTP is not configured; workflow email was not sent"
	definitionNotRunnable = "Workflow run definition is not executable"
	stepFailedFallback    = "Workflow step failed"
	// continuedAfterFailure starts the error of a run that went on past a failed step set to continue on failure.
	continuedAfterFailure = "Continued after a failed step: "
	messageIDDomain       = "forma.yldm.ai"
	emailProvider         = "smtp"
)

// Runner executes workflow-run.process jobs, forma2's processWorkflowRunJob: a run moves queued → running → completed or failed, and each step is claimed in its run log row before it acts (an email goes out, a request is sent, a tag or attribute is written), so a step acts at most once however often its job is delivered.
type Runner struct {
	cfg     config.Config
	db      *gorm.DB
	mailer  *mailer.Mailer
	actions *actions
	log     *slog.Logger
}

func NewRunner(cfg config.Config, db *gorm.DB, m *mailer.Mailer, guard *ssrf.Guard, cipher *secret.Cipher, log *slog.Logger) *Runner {
	return &Runner{cfg: cfg, db: db, mailer: m, actions: newActions(db, guard, cipher), log: log}
}

// claimedRun is what executing a run reads of it.
type claimedRun struct {
	ID                 uuid.UUID
	Status             string
	TriggerType        string
	TriggerPayload     json.RawMessage
	VersionDefinition  json.RawMessage
	WorkflowDefinition json.RawMessage
}

// notExecutableError fails a run that cannot be executed at all.
type notExecutableError struct{ message string }

func (e *notExecutableError) Error() string { return e.message }

// stepFailedError fails a run whose step failed, carrying the steps executed so far. permanent is a failure no retry can change, which fails the run without the attempts left.
type stepFailedError struct {
	message   string
	data      json.RawMessage
	permanent bool
}

func (e *stepFailedError) Error() string { return e.message }

// StepResult is one step of a run's data, forma2's ZWorkflowStepResult.
type StepResult struct {
	StepID     string          `json:"stepId"`
	StepType   string          `json:"stepType"`
	Status     string          `json:"status"`
	Input      json.RawMessage `json:"input,omitempty"`
	Output     json.RawMessage `json:"output,omitempty"`
	Error      string          `json:"error,omitempty"`
	StartedAt  string          `json:"startedAt,omitempty"`
	FinishedAt string          `json:"finishedAt,omitempty"`
	// permanent marks a failure no retry can change; the step log records it as permanent_failure, so a retry resumes the step as failed instead of performing it again.
	permanent bool
	// continued marks a failure the run went on past, its step being set to continue on failure; the step's input records the setting.
	continued bool
}

// runDataDoc is the data a run records: its trigger payload and its steps.
type runDataDoc struct {
	Trigger json.RawMessage `json:"trigger"`
	Steps   []StepResult    `json:"steps"`
}

func isTerminal(status string) bool {
	return status == RunCompleted || status == RunFailed || status == RunCanceled
}

// Work runs one delivery of a run's job. A run that is missing or belongs to another workspace is dropped, a terminal one is left alone. A failure is recorded on the run and retried while attempts remain; the last attempt, or a step failure no retry can change, marks the run failed.
func (r *Runner) Work(ctx context.Context, job *river.Job[RunArgs]) error {
	args := job.Args
	log := r.log.With("attempt", job.Attempt, "max_attempts", job.MaxAttempts, "job_id", job.ID, "workflow_id", args.WorkflowID, "workflow_run_id", args.WorkflowRunID, "workspace_id", args.WorkspaceID)
	run, err := r.loadRun(ctx, args.WorkflowRunID, args.WorkspaceID)
	if err != nil {
		return err
	}
	if run == nil {
		log.ErrorContext(ctx, "Workflow run not found for workspace; dropping job")
		return nil
	}
	if isTerminal(run.Status) {
		log.InfoContext(ctx, "Workflow run already terminal; skipping", "status", run.Status)
		return nil
	}
	err = r.execute(ctx, job, run, log)
	if err == nil {
		return nil
	}
	final := job.Attempt >= job.MaxAttempts
	var data json.RawMessage
	var stepErr *stepFailedError
	if errors.As(err, &stepErr) {
		data = stepErr.data
		final = final || stepErr.permanent
	}
	recorded := r.recordFailure(ctx, run.ID, args.WorkspaceID, err, data, final, log)
	if !final {
		return err
	}
	log.ErrorContext(ctx, "Workflow run job failed after final attempt", "err", err)
	// Only the delivery whose terminal write landed reports the failure, so a run is reported once and never over another delivery's verdict.
	if recorded {
		captureRunFailed(ctx, r.db, log, runFailure{
			runID: run.ID, workflowID: args.WorkflowID, workspaceID: args.WorkspaceID, triggerType: run.TriggerType,
			failedStepType: failedStepType(data), errorKind: classifyRunError(err), attempt: job.Attempt,
		})
	}
	return nil
}

func (r *Runner) loadRun(ctx context.Context, id, workspaceID uuid.UUID) (*claimedRun, error) {
	var rows []claimedRun
	if err := r.db.WithContext(ctx).Raw(`SELECT r.id, r.status, r.trigger_type, r.trigger_payload, v.definition AS version_definition, w.definition AS workflow_definition
		FROM workflow_runs r JOIN workflows w ON w.id = r.workflow_id LEFT JOIN workflow_versions v ON v.id = r.workflow_version_id
		WHERE r.id = ? AND r.workspace_id = ?`, id, workspaceID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load workflow run: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// parsePayload reads the stored trigger payload, whose survey and response ids are UUIDs.
func parsePayload(raw json.RawMessage) (*TriggerPayload, uuid.UUID, uuid.UUID, error) {
	var p TriggerPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, uuid.Nil, uuid.Nil, fmt.Errorf("workflow run trigger payload: %w", err)
	}
	surveyID, sok := rowid.Parse(p.SurveyID)
	responseID, rok := rowid.Parse(p.ResponseID)
	if p.Type != TriggerResponseCompleted || !sok || !rok || p.TriggeredAt == "" {
		return nil, uuid.Nil, uuid.Nil, errors.New("workflow run trigger payload is not a response.completed payload")
	}
	return &p, surveyID, responseID, nil
}

func (r *Runner) execute(ctx context.Context, job *river.Job[RunArgs], run *claimedRun, log *slog.Logger) error {
	payload, surveyID, responseID, err := parsePayload(run.TriggerPayload)
	if err != nil {
		return err
	}
	// The claim takes a queued run; a run already running is this job's own retry and carries on, resuming its succeeded steps.
	res := r.db.WithContext(ctx).Exec(`UPDATE workflow_runs SET status = 'running', started_at = now(), attempt = ?, updated_at = now() WHERE id = ? AND workspace_id = ? AND status = 'queued'`,
		job.Attempt, run.ID, job.Args.WorkspaceID)
	if res.Error != nil {
		return fmt.Errorf("claim workflow run: %w", res.Error)
	}
	if res.RowsAffected == 0 && run.Status == RunQueued {
		log.InfoContext(ctx, "Workflow run already claimed by another delivery; skipping")
		return nil
	}
	if res.RowsAffected == 0 {
		// A retry finds its run already running; the run still records which delivery is working on it, never going back to an earlier one.
		if err := r.db.WithContext(ctx).Exec(`UPDATE workflow_runs SET attempt = ?, updated_at = now() WHERE id = ? AND workspace_id = ? AND status = 'running' AND attempt < ?`,
			job.Attempt, run.ID, job.Args.WorkspaceID, job.Attempt).Error; err != nil {
			return fmt.Errorf("record workflow run attempt: %w", err)
		}
	}
	raw := run.VersionDefinition
	if len(raw) == 0 {
		raw = run.WorkflowDefinition
	}
	definition, issues := ParseExecutable(raw)
	if len(issues) > 0 {
		return &notExecutableError{message: definitionNotRunnable}
	}
	steps, err := Plan(definition)
	if err != nil {
		return err
	}
	ec, err := r.loadContext(ctx, job.Args.WorkspaceID, surveyID, responseID, steps)
	if err != nil {
		return err
	}
	ec.template.Fixed["workflow.id"] = job.Args.WorkflowID.String()
	ec.template.Fixed["run.id"] = run.ID.String()
	results, failure, bail, err := r.runSteps(ctx, run.ID, steps, ec, log)
	if err != nil {
		return err
	}
	if bail {
		log.InfoContext(ctx, "Workflow run owned by another delivery; leaving it running without finalizing")
		return nil
	}
	data := mustJSON(runDataDoc{Trigger: mustJSON(payload), Steps: results})
	if failure != "" {
		permanent := len(results) > 0 && results[len(results)-1].permanent
		return &stepFailedError{message: failure, data: data, permanent: permanent}
	}
	// A failed step set to continue on failure did not stop the steps after it, but it still gets the run's retries: while attempts remain and a retry could change it, the run is retried, resuming its succeeded steps without acting again; after that the run completes and records the failure.
	continued := continuedFailures(results)
	if len(continued) > 0 && job.Attempt < job.MaxAttempts && slices.ContainsFunc(continued, func(s StepResult) bool { return !s.permanent }) {
		return &stepFailedError{message: continuedMessage(continued), data: data}
	}
	// A run whose steps all succeeded in the end drops the error an earlier attempt recorded for continued failures, which would say a step failed when none did. Any other error an earlier attempt recorded stays, as it did before continue on failure.
	completion := `UPDATE workflow_runs SET status = 'completed', finished_at = now(), data = ?::jsonb,
		error = CASE WHEN starts_with(error, ?) THEN NULL ELSE error END, last_error_at = CASE WHEN starts_with(error, ?) THEN NULL ELSE last_error_at END,
		updated_at = now() WHERE id = ? AND workspace_id = ? AND status = 'running'`
	args := []any{string(data), continuedAfterFailure, continuedAfterFailure, run.ID, job.Args.WorkspaceID}
	if len(continued) > 0 {
		completion = `UPDATE workflow_runs SET status = 'completed', finished_at = now(), data = ?::jsonb, error = ?, last_error_at = now(), updated_at = now() WHERE id = ? AND workspace_id = ? AND status = 'running'`
		args = []any{string(data), continuedMessage(continued), run.ID, job.Args.WorkspaceID}
	}
	done := r.db.WithContext(ctx).Exec(completion, args...)
	if done.Error != nil {
		return fmt.Errorf("complete workflow run: %w", done.Error)
	}
	if done.RowsAffected == 0 {
		log.InfoContext(ctx, "Workflow run already finalized by another delivery; skipping completion write")
	}
	return nil
}

// recordTimeout bounds the writes that record an outcome after the job's context may have ended.
const recordTimeout = 10 * time.Second

// detached is ctx without its cancellation, bounded by recordTimeout. River cancels a run's context at runTimeout and SHUTDOWN_TIMEOUT after the worker is asked to stop, usually while a send is waiting on it; the step's outcome and the run's failure must still be written, or the retry finds the step running, takes it for another delivery's and abandons a send that never happened.
func detached(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
}

// recordFailure writes the failure onto a run that is not yet terminal: the error and, on the last attempt, the failed status. A run another delivery finalized keeps its verdict. It reports whether the write landed.
func (r *Runner) recordFailure(ctx context.Context, runID, workspaceID uuid.UUID, cause error, data json.RawMessage, final bool, log *slog.Logger) bool {
	set := map[string]any{"error": cause.Error(), "last_error_at": gorm.Expr("now()"), "updated_at": gorm.Expr("now()")}
	if final {
		set["status"] = RunFailed
		set["finished_at"] = gorm.Expr("now()")
	}
	if data != nil {
		set["data"] = gorm.Expr("?::jsonb", string(data))
	}
	rctx, cancel := detached(ctx)
	defer cancel()
	res := r.db.WithContext(rctx).Table("workflow_runs").Where("id = ? AND workspace_id = ? AND status NOT IN ('completed', 'failed', 'canceled')", runID, workspaceID).Updates(set)
	switch {
	case res.Error != nil:
		log.ErrorContext(ctx, "Failed to persist workflow run failure state", "err", res.Error)
	case res.RowsAffected == 0:
		log.InfoContext(ctx, "Workflow run already finalized by another delivery; skipping failure write")
	}
	return res.Error == nil && res.RowsAffected > 0
}

// emailContext is what every step of a run renders against, loaded once.
type emailContext struct {
	workspaceID uuid.UUID
	response    *response.Response
	survey      *responsemail.Survey
	answers     *responsemail.Answers
	logo        string
	locale      string
	// allowed is the workspace's recipient allowlist, loaded only when a step has a literal recipient; empty, it refuses every literal recipient.
	allowed map[string]bool
	// template is what the steps after send_email render their templates against.
	template *Template
}

// loadContext is forma2's loadRunEmailContext: the response, its survey, the organization's email logo and, when a step needs it, the recipient allowlist. The survey must belong to the run's workspace and the response to the payload's survey.
func (r *Runner) loadContext(ctx context.Context, workspaceID, surveyID, responseID uuid.UUID, steps []Step) (*emailContext, error) {
	resp, err := response.Load(ctx, r.db, responseID)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, &notExecutableError{message: fmt.Sprintf("Response %s not found", responseID)}
	}
	sv, err := responsemail.LoadSurvey(ctx, r.db, surveyID)
	if err != nil {
		return nil, err
	}
	if sv == nil {
		return nil, &notExecutableError{message: fmt.Sprintf("Survey %s not found", surveyID)}
	}
	if sv.WorkspaceID != workspaceID {
		return nil, &notExecutableError{message: fmt.Sprintf("Survey %s does not belong to workspace %s", surveyID, workspaceID)}
	}
	if resp.SurveyID != surveyID {
		return nil, &notExecutableError{message: fmt.Sprintf("Response %s does not belong to survey %s", responseID, surveyID)}
	}
	answers, err := responsemail.Decode(resp)
	if err != nil {
		return nil, err
	}
	ec := &emailContext{workspaceID: workspaceID, response: resp, survey: sv, answers: answers, allowed: map[string]bool{}, template: responseTemplate(resp, sv, answers)}
	// Scanned into a struct: GORM cannot scan a NULL column into a []*string, and an organization without a whitelabel logo has one.
	var orgs []struct{ LogoURL *string }
	if err := r.db.WithContext(ctx).Raw(`SELECT o.whitelabel ->> 'logoUrl' AS logo_url FROM organizations o JOIN workspaces w ON w.organization_id = o.id WHERE w.id = ?`, workspaceID).Scan(&orgs).Error; err != nil {
		return nil, fmt.Errorf("load workflow organization: %w", err)
	}
	if len(orgs) > 0 && orgs[0].LogoURL != nil {
		ec.logo = *orgs[0].LogoURL
	}
	if resp.Language != nil && slices.Contains(mailer.Locales, *resp.Language) {
		ec.locale = *resp.Language
	}
	if slices.ContainsFunc(steps, func(s Step) bool { return IsLiteralRecipient(s.Config.To) }) {
		if ec.allowed, err = MemberEmails(ctx, r.db, workspaceID); err != nil {
			return nil, err
		}
	}
	return ec, nil
}

// stepLog is a step's run log row, as the claim reads it.
type stepLog struct {
	StepID     string
	StepType   string
	Status     string
	Input      json.RawMessage
	Output     json.RawMessage
	Error      *string
	StartedAt  *time.Time
	FinishedAt *time.Time
	// PermanentFailure marks a failed step no retry can change, which a retry resumes as failed without performing it again.
	PermanentFailure bool
}

// runSteps runs the steps in order. bail means another delivery owns a step, and the run is left for it to finish; failure is the error of the first failed step that is not set to continue on failure, which stops the run there. A failed step set to continue is marked continued and the next step runs.
func (r *Runner) runSteps(ctx context.Context, runID uuid.UUID, steps []Step, ec *emailContext, log *slog.Logger) (results []StepResult, failure string, bail bool, err error) {
	results = []StepResult{}
	for i, step := range steps {
		// The sequence is 1-based: it is the step number the run detail shows.
		res, owned, err := r.runStep(ctx, runID, i+1, step, ec)
		if err != nil {
			return nil, "", false, err
		}
		if !owned {
			log.WarnContext(ctx, "Workflow step owned by another delivery; bailing", "step_id", step.ID)
			return nil, "", true, nil
		}
		if res.Status == StepFailed && step.Node.ContinueOnFailure {
			res.continued = true
			log.InfoContext(ctx, "Workflow step failed; continuing with the next step as the step is set to", "step_id", step.ID, "step_type", step.Type)
		}
		results = append(results, *res)
		if res.Status == StepFailed && !res.continued {
			return results, stepError(*res), false, nil
		}
	}
	return results, "", false, nil
}

// stepError is a failed step's error, or a generic one when it recorded none.
func stepError(s StepResult) string {
	if s.Error == "" {
		return stepFailedFallback
	}
	return s.Error
}

// continuedFailures are the failed steps a run went on past.
func continuedFailures(results []StepResult) []StepResult {
	var out []StepResult
	for _, s := range results {
		if s.continued {
			out = append(out, s)
		}
	}
	return out
}

// continuedMessage is the error a run records for the failed steps it went on past: the first one's error, and how many more failed.
func continuedMessage(failed []StepResult) string {
	msg := continuedAfterFailure + stepError(failed[0])
	if len(failed) > 1 {
		msg += fmt.Sprintf(" (and %d more)", len(failed)-1)
	}
	return msg
}

func isoOrNow(t *time.Time) string {
	if t == nil {
		return jsvalue.ISOString(time.Now())
	}
	return jsvalue.ISOString(*t)
}

// runStep is forma2's runStep: a succeeded or skipped step, or one that failed for good, is resumed without sending, a running one belongs to another delivery, and anything else is claimed (a new row, or a failed or pending one taken over) and sent. owned is false when the step is not this delivery's to send.
func (r *Runner) runStep(ctx context.Context, runID uuid.UUID, sequence int, step Step, ec *emailContext) (*StepResult, bool, error) {
	var existing []stepLog
	if err := r.db.WithContext(ctx).Raw(`SELECT step_id, step_type, status, input, output, error, started_at, finished_at, permanent_failure FROM workflow_run_logs WHERE run_id = ? AND step_id = ?`, runID, step.ID).Scan(&existing).Error; err != nil {
		return nil, false, fmt.Errorf("load workflow step: %w", err)
	}
	if len(existing) > 0 {
		l := existing[0]
		// A skipped step (a contact attribute with nothing to write) is done too; only the reconciler skips steps otherwise, on runs it fails. A step that failed for good is done as well: a run goes on past one only when it is set to continue on failure, and a retry of that run, for another step that failed transiently, must not perform it again.
		if l.Status == StepSucceeded || l.Status == StepSkipped || (l.Status == StepFailed && l.PermanentFailure) {
			res := &StepResult{StepID: l.StepID, StepType: l.StepType, Status: l.Status, Input: jsonObject(l.Input), Output: jsonObject(l.Output), StartedAt: isoOrNow(l.StartedAt), FinishedAt: isoOrNow(l.FinishedAt), permanent: l.PermanentFailure}
			if l.Error != nil {
				res.Error = *l.Error
			}
			return res, true, nil
		}
		if l.Status == StepRunning {
			return nil, false, nil
		}
	}
	input := mustJSON(stepInput(step))
	startedAt := time.Now()
	var claim *gorm.DB
	if len(existing) == 0 {
		claim = r.db.WithContext(ctx).Exec(`INSERT INTO workflow_run_logs (run_id, sequence, step_id, step_type, status, input, started_at) VALUES (?, ?, ?, ?, 'running', ?::jsonb, ?)
			ON CONFLICT (run_id, step_id) DO NOTHING`, runID, sequence, step.ID, step.Type, string(input), startedAt)
	} else {
		claim = r.db.WithContext(ctx).Exec(`UPDATE workflow_run_logs SET status = 'running', started_at = ?, output = '{}', error = NULL, finished_at = NULL
			WHERE run_id = ? AND step_id = ? AND status IN ('failed', 'pending')`, startedAt, runID, step.ID)
	}
	if claim.Error != nil {
		return nil, false, fmt.Errorf("claim workflow step: %w", claim.Error)
	}
	if claim.RowsAffected == 0 {
		return nil, false, nil
	}
	status, output, errMsg, permanent := r.perform(ctx, runID, step, ec)
	finishedAt := time.Now()
	var stored *string
	if errMsg != "" {
		stored = &errMsg
	}
	rctx, cancel := detached(ctx)
	defer cancel()
	if err := r.db.WithContext(rctx).Exec(`UPDATE workflow_run_logs SET status = ?, output = ?::jsonb, error = ?, finished_at = ?, permanent_failure = ? WHERE run_id = ? AND step_id = ?`,
		status, string(output), stored, finishedAt, status == StepFailed && permanent, runID, step.ID).Error; err != nil {
		return nil, false, fmt.Errorf("record workflow step: %w", err)
	}
	return &StepResult{StepID: step.ID, StepType: step.Type, Status: status, Input: input, Output: output, Error: errMsg,
		StartedAt: jsvalue.ISOString(startedAt), FinishedAt: jsvalue.ISOString(finishedAt), permanent: permanent}, true, nil
}

// perform runs a claimed step: send_email sends its email, every other action goes through actions.perform with the run's template, its response and contact, and a message id the same on every delivery of the step.
func (r *Runner) perform(ctx context.Context, runID uuid.UUID, step Step, ec *emailContext) (status string, output json.RawMessage, errMsg string, permanent bool) {
	if step.Type == ActionSendEmail {
		status, output, errMsg = r.send(ctx, runID, step, ec)
		return status, output, errMsg, false
	}
	env := actionEnv{workspaceID: ec.workspaceID, responseID: ec.response.ID, template: ec.template, messageID: stepMessageID(runID, step.ID), retry: true}
	if ec.response.Contact != nil {
		id := ec.response.Contact.ID
		env.contactID = &id
	}
	o := r.actions.perform(ctx, env, step.Node)
	if o.output == nil {
		o.output = map[string]any{}
	}
	return o.status, mustJSON(o.output), o.err, o.permanent
}

// controlChars are what an SMTP header injection smuggles into a subject: control characters, line breaks among them, and the Unicode line and paragraph separators.
var controlChars = regexp.MustCompile("[\x00-\x1f\x7f  ]")

// MessageID is forma2's buildMessageId: <the first 32 hex digits of sha256("<run id>:<step id>")@the domain of the action's from>, the same on every delivery of the step.
func MessageID(runID uuid.UUID, stepID, from string) string {
	sum := sha256.Sum256([]byte(runID.String() + ":" + stepID))
	domain := messageIDDomain
	if _, after, ok := strings.Cut(from, "@"); ok {
		if d := strings.TrimSpace(strings.SplitN(after, "@", 2)[0]); d != "" {
			domain = d
		}
	}
	return "<" + hex.EncodeToString(sum[:])[:32] + "@" + domain + ">"
}

// send is forma2's sendResolvedEmail: the recipient resolved from `to` (a literal recipient must be in the workspace's allowlist), the follow-up email built from the author's subject, its placeholders filled on one line (Template.RenderSubject), and sanitized body, and sent from the instance's MAIL_FROM with a stable Message-ID.
func (r *Runner) send(ctx context.Context, runID uuid.UUID, step Step, ec *emailContext) (status string, output json.RawMessage, errMsg string) {
	failed := func(msg string) (string, json.RawMessage, string) { return StepFailed, json.RawMessage("{}"), msg }
	cfg := step.Config
	to, reason := ec.answers.ResolveRecipient(cfg.To)
	if reason != "" {
		return failed(reason)
	}
	if IsLiteralRecipient(cfg.To) && !ec.allowed[normalizeEmail(to)] {
		return failed(fmt.Sprintf("Recipient %s cannot access this workspace", to))
	}
	messageID := MessageID(runID, step.ID, cfg.From)
	content, err := ec.answers.FollowUpContent(ec.survey, responsemail.EmailOptions{
		Subject: ec.template.RenderSubject(cfg.Subject), Body: cfg.Body, AttachResponseData: cfg.AttachResponseData,
		IncludeVariables: cfg.IncludeVariables != nil && *cfg.IncludeVariables, IncludeHiddenFields: cfg.IncludeHiddenFields != nil && *cfg.IncludeHiddenFields,
		LogoURL: ec.logo, Locale: ec.locale, PublicBase: responsemail.PublicBase(r.cfg),
	})
	if err != nil {
		return failed(err.Error())
	}
	locale := ec.locale
	if locale == "" {
		locale = mailer.DefaultLocale
	}
	subject, html, err := r.mailer.Render(locale, content)
	if err != nil {
		return failed(err.Error())
	}
	sent, err := r.mailer.Send(ctx, mailer.Message{To: to, ReplyTo: strings.Join(cfg.ReplyTo, ", "), Subject: subject, HTML: html, MessageID: messageID})
	if err != nil {
		// forma2 persisted the sender's "Failed to send email: <reason>".
		if cause := errors.Unwrap(err); cause != nil {
			return failed("Failed to send email: " + cause.Error())
		}
		return failed(err.Error())
	}
	if !sent {
		return failed(smtpNotConfigured)
	}
	return StepSucceeded, mustJSON(map[string]string{"messageId": messageID, "provider": emailProvider}), ""
}
