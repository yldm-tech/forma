// Package seed writes the contributor and CI seed data, forma2's packages/database/src/seed.ts: a demo organization with an owner, a manager and a member who can sign in, a workspace with forma2's four demo surveys (one of them carrying every element type) and their responses, demo workflows with run history, and on request an API key.
//
// Every seeded row has a fixed id, the uuidv5 legacyid derives from forma2's seed id (SEED_IDS), so a second seed rewrites the same rows instead of adding more, forma2's ids still resolve in URLs, and Clear can remove exactly what a seed wrote. Rows are written with SQL in one transaction, so the authorization triggers enqueue the SpiceDB projection of every organization, membership, workspace and API key exactly as the services' writes do; the worker (or `forma authz outbox drain`) delivers it. The services themselves are not used: they authorize an actor through SpiceDB, which knows nothing about data that does not exist yet.
package seed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/apikey"
	"github.com/yldm-tech/forma/apps/server/internal/jsonschema"
	"github.com/yldm-tech/forma/apps/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
	"github.com/yldm-tech/forma/apps/server/internal/workflow"
)

// Seeded ids, forma2's SEED_IDS through legacyid.
var (
	OrganizationID = legacyid.ID("clseedorg0000000000000")
	WorkspaceID    = legacyid.ID("clseedworkspace000000000")
	APIKeyID       = legacyid.ID("clseedapikey000000000")
	// KitchenSinkSurveyID is the survey with every element type, the trigger survey of the demo workflows.
	KitchenSinkSurveyID = legacyid.ID("clseedsurveykitchen00")
	// FollowUpWorkflowID is the enabled demo workflow, the one with run history.
	FollowUpWorkflowID = legacyid.ID("clseedwfresponsefup001")
)

// Password is the password of every seeded user, forma2's SEED_CREDENTIALS. The seed is development data; never run it against a database real people sign in to.
const Password = "Password#123"

// User is a seeded user who can sign in with Password.
type User struct {
	ID    uuid.UUID
	Name  string
	Email string
	Role  string
}

// Users are forma2's three seeded users, one per organization role it seeded.
var Users = []User{
	{ID: legacyid.ID("clseedadmin000000000000"), Name: "Admin User", Email: "admin@forma.yldm.ai", Role: "owner"},
	{ID: legacyid.ID("clseedmanager0000000000"), Name: "Manager User", Email: "manager@forma.yldm.ai", Role: "manager"},
	{ID: legacyid.ID("clseedmember00000000000"), Name: "Member User", Email: "member@forma.yldm.ai", Role: "member"},
}

// Options selects what a seed writes besides the base data.
type Options struct {
	// Clear removes everything an earlier seed wrote before seeding again: the seeded organization with everything in it, and the seeded users.
	Clear bool
	// APIKeySecret, when set, creates the seeded API key, sent as "fbk_" + APIKeySecret, with manage access to the seeded workspace (forma2's SEED_API_KEY). The secret must be base64url characters, as every key the API accepts is.
	APIKeySecret string
}

// Run seeds db.
func Run(ctx context.Context, db *gorm.DB, opts Options, log *slog.Logger) error {
	if opts.APIKeySecret != "" && !validKeySecret(opts.APIKeySecret) {
		return errors.New("SEED_API_KEY must be base64url characters (A-Z, a-z, 0-9, - and _), as `openssl rand -hex 32` prints")
	}
	schemas, err := jsonschema.New()
	if err != nil {
		return err
	}
	validator := surveydoc.NewValidator(schemas)
	// Hashed before the transaction opens: bcrypt at forma2's cost takes a moment each.
	passwordHash, err := secret.HashPassword(Password)
	if err != nil {
		return fmt.Errorf("hash seed password: %w", err)
	}
	var keyHash string
	if opts.APIKeySecret != "" {
		if keyHash, err = secret.HashPassword(opts.APIKeySecret); err != nil {
			return fmt.Errorf("hash seed api key: %w", err)
		}
	}

	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		s := &seeder{tx: tx, validator: validator, now: time.Now().UTC()}
		if opts.Clear {
			if err := clear(tx); err != nil {
				return err
			}
			log.InfoContext(ctx, "cleared the seeded data")
		}
		if err := s.base(passwordHash); err != nil {
			return err
		}
		if opts.APIKeySecret != "" {
			if err := s.apiKey(opts.APIKeySecret, keyHash); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	log.InfoContext(ctx, "seeded the demo organization", "organization", OrganizationID, "workspace", WorkspaceID, "api_key", opts.APIKeySecret != "")
	return nil
}

// Clear removes everything a seed wrote: the seeded organization, which takes its workspaces, surveys, responses, workflows and API keys with it, and the seeded users. Nothing else is touched.
func Clear(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(clear)
}

func clear(tx *gorm.DB) error {
	if err := tx.Exec(`DELETE FROM organizations WHERE id = ?`, OrganizationID).Error; err != nil {
		return fmt.Errorf("clear seeded organization: %w", err)
	}
	ids := make([]uuid.UUID, len(Users))
	for i, u := range Users {
		ids[i] = u.ID
	}
	if err := tx.Exec(`DELETE FROM users WHERE id IN ?`, ids).Error; err != nil {
		return fmt.Errorf("clear seeded users: %w", err)
	}
	return nil
}

func validKeySecret(s string) bool {
	return strings.TrimLeft(s, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_") == ""
}

type seeder struct {
	tx        *gorm.DB
	validator *surveydoc.Validator
	now       time.Time
}

func (s *seeder) exec(what, sql string, args ...any) error {
	if err := s.tx.Exec(sql, args...).Error; err != nil {
		return fmt.Errorf("seed %s: %w", what, err)
	}
	return nil
}

func jsonOf(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// base is forma2's seed.ts main: the organization and its billing row, the users and their memberships, the workspace with its language and default contact attribute keys, the surveys with their responses, and the demo workflows.
func (s *seeder) base(passwordHash string) error {
	if err := s.exec("organization", `INSERT INTO organizations (id, name) VALUES (?, 'Seed Organization') ON CONFLICT (id) DO NOTHING`, OrganizationID); err != nil {
		return err
	}
	// forma2's self-hosted defaults: three workspaces and 1500 responses a month.
	if err := s.exec("organization billing", `INSERT INTO organization_billing (organization_id, limits, usage_cycle_anchor) VALUES (?, ?::jsonb, now()) ON CONFLICT (organization_id) DO NOTHING`,
		OrganizationID, `{"workspaces": 3, "monthly": {"responses": 1500, "workflowRuns": 0}}`); err != nil {
		return err
	}
	for _, u := range Users {
		if err := s.exec("user", `INSERT INTO users (id, name, email, email_verified) VALUES (?, ?, ?, true) ON CONFLICT (id) DO NOTHING`, u.ID, u.Name, u.Email); err != nil {
			return err
		}
		// The credential identity is what email and password sign-in checks; a re-seed resets the password.
		if err := s.exec("credential identity", `INSERT INTO user_identities (user_id, provider, subject, password_hash) VALUES (?, 'credential', ?, ?)
			ON CONFLICT (provider, issuer, subject) DO UPDATE SET password_hash = excluded.password_hash, updated_at = now()`, u.ID, u.ID.String(), passwordHash); err != nil {
			return err
		}
		if err := s.exec("membership", `INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, ?, true)
			ON CONFLICT (user_id, organization_id) DO UPDATE SET role = excluded.role, accepted = true, updated_at = now()`, u.ID, OrganizationID, u.Role); err != nil {
			return err
		}
	}
	if err := s.exec("workspace", `INSERT INTO workspaces (id, organization_id, name, config) VALUES (?, ?, 'Seed Workspace', '{"channel": null, "industry": null}') ON CONFLICT (id) DO NOTHING`, WorkspaceID, OrganizationID); err != nil {
		return err
	}
	if err := s.exec("language", `INSERT INTO languages (workspace_id, code) VALUES (?, 'en-US') ON CONFLICT (workspace_id, code) DO NOTHING`, WorkspaceID); err != nil {
		return err
	}
	for _, k := range contactAttributeKeys {
		if err := s.exec("contact attribute key", `INSERT INTO contact_attribute_keys (workspace_id, key, name, description, type, is_unique) VALUES (?, ?, ?, ?, 'default', ?) ON CONFLICT (workspace_id, key) DO NOTHING`,
			WorkspaceID, k.key, k.name, k.description, k.unique); err != nil {
			return err
		}
	}
	for _, sv := range seedSurveys {
		id := legacyid.ID(sv.id)
		if err := s.survey(id, sv.id, sv.name, sv.status, sv.elements); err != nil {
			return err
		}
		if err := s.responses(id, sv.id, sv.elements, sv.responses); err != nil {
			return err
		}
	}
	return s.workflows()
}

// contactAttributeKeys are the keys every new workspace starts with (forma2's createWorkspace).
var contactAttributeKeys = []struct {
	key, name, description string
	unique                 bool
}{
	{"userId", "User Id", "The user id of a contact", true},
	{"email", "Email", "The email of a contact", true},
	{"firstName", "First Name", "Your contact's first name", false},
	{"lastName", "Last Name", "Your contact's last name", false},
	{"language", "Language", "The language preference of a contact", false},
}

// survey writes a link survey, rewriting its document on a re-seed. The document is checked as the editor's save checks it first, so a seed never writes a survey the product would refuse.
func (s *seeder) survey(id uuid.UUID, key, name, status string, elements []map[string]any) error {
	blocks, endings := blocksOf(key, elements), endingsOf(key)
	welcome, hidden := json.RawMessage(`{"enabled": false}`), json.RawMessage(`{"enabled": true, "fieldIds": []}`)
	issues, _, err := s.validator.Check(surveydoc.Document{WelcomeCard: welcome, Blocks: blocks, Endings: endings, HiddenFields: hidden, Variables: json.RawMessage(`[]`), Metadata: json.RawMessage(`{}`)}, nil, nil)
	if err != nil {
		return fmt.Errorf("validate seed survey %s: %w", name, err)
	}
	if len(issues) > 0 {
		return fmt.Errorf("seed survey %s is not a valid survey: %v", name, issues)
	}
	return s.exec("survey", `INSERT INTO surveys (id, workspace_id, created_by, name, type, status, welcome_card, blocks, endings, hidden_fields)
		VALUES (?, ?, ?, ?, 'link', ?, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb)
		ON CONFLICT (id) DO UPDATE SET name = excluded.name, type = 'link', status = excluded.status, welcome_card = excluded.welcome_card, blocks = excluded.blocks,
			endings = excluded.endings, hidden_fields = excluded.hidden_fields, archived_at = NULL, updated_at = now()`,
		id, WorkspaceID, Users[0].ID, name, status, string(welcome), string(blocks), string(endings), string(hidden))
}

// responses writes forma2's generated responses, each with the display it answered, plus 30% more displays nobody answered. A re-seed leaves rows that exist alone.
func (s *seeder) responses(surveyID uuid.UUID, key string, elements []map[string]any, count int) error {
	for i := range count {
		displayID := legacyid.ID(fmt.Sprintf("%sdisplay%03d", key, i))
		// Spread over the last weeks, newest first, so the summary's timeline has a shape.
		at := s.now.Add(-time.Duration(i) * 7 * time.Hour)
		if err := s.exec("display", `INSERT INTO displays (id, survey_id, created_at, updated_at) VALUES (?, ?, ?, ?) ON CONFLICT (id) DO NOTHING`, displayID, surveyID, at, at); err != nil {
			return err
		}
		if err := s.exec("response", `INSERT INTO responses (id, survey_id, display_id, finished, ending_id, data, language, created_at, updated_at)
			VALUES (?, ?, ?, true, ?, ?::jsonb, 'default', ?, ?) ON CONFLICT (id) DO NOTHING`,
			ResponseID(key, i), surveyID, displayID, key+"end", jsonOf(responseData(elements, i)), at, at); err != nil {
			return err
		}
	}
	for i := range count * 3 / 10 {
		at := s.now.Add(-time.Duration(i) * 11 * time.Hour)
		if err := s.exec("display", `INSERT INTO displays (id, survey_id, created_at, updated_at) VALUES (?, ?, ?, ?) ON CONFLICT (id) DO NOTHING`,
			legacyid.ID(fmt.Sprintf("%sextradisplay%03d", key, i)), surveyID, at, at); err != nil {
			return err
		}
	}
	return nil
}

// ResponseID is the id of the i-th seeded response of the survey with forma2 seed id key.
func ResponseID(key string, i int) uuid.UUID {
	return legacyid.ID(fmt.Sprintf("%sresponse%03d", key, i))
}

// apiKey is forma2's seedApiKey: a key with manage access to the seeded workspace and no organization-level grants. The row is keyed on its fixed id, so seeding with a different secret replaces the hashes rather than adding a key.
func (s *seeder) apiKey(keySecret, hashedKey string) error {
	sum := sha256.Sum256([]byte(keySecret))
	access := apikey.OrganizationAccess{}
	if err := s.exec("api key", `INSERT INTO api_keys (id, organization_id, created_by, label, hashed_key, lookup_hash, organization_access) VALUES (?, ?, ?, 'Seed API key', ?, ?, ?::jsonb)
		ON CONFLICT (id) DO UPDATE SET organization_id = excluded.organization_id, hashed_key = excluded.hashed_key, lookup_hash = excluded.lookup_hash, organization_access = excluded.organization_access`,
		APIKeyID, OrganizationID, Users[0].ID, hashedKey, hex.EncodeToString(sum[:]), jsonOf(access)); err != nil {
		return err
	}
	return s.exec("api key workspace", `INSERT INTO api_key_workspaces (api_key_id, workspace_id, permission) VALUES (?, ?, 'manage')
		ON CONFLICT (api_key_id, workspace_id) DO UPDATE SET permission = 'manage', updated_at = now()`, APIKeyID, WorkspaceID)
}

// workflowSpec is one of forma2's demo workflows (seedDemoWorkflows).
type workflowSpec struct {
	id, key, name, description, status string
	to, subject, body                  string
	runs                               []runSpec
}

// runSpec is one historical run of a demo workflow and the trace of its single send_email step (none for a run that never started).
type runSpec struct {
	suffix     string
	status     string
	minutesAgo int
	started    bool
	finished   bool
	durationMs int
	attempt    int
	err        string
	step       string
}

var demoWorkflows = []workflowSpec{
	{id: "clseedwfresponsefup001", key: "fup", name: "Response follow-up", description: "Email respondents after they complete the survey.", status: "enabled",
		to: "respondent@example.com", subject: "Thanks for your answers!", body: "Hi there, thanks for completing the survey.",
		runs: []runSpec{
			{suffix: "01", status: "completed", minutesAgo: 4, started: true, finished: true, durationMs: 820, step: "succeeded"},
			{suffix: "02", status: "completed", minutesAgo: 47, started: true, finished: true, durationMs: 610, step: "succeeded"},
			{suffix: "03", status: "running", minutesAgo: 1, started: true, step: "running"},
			{suffix: "04", status: "failed", minutesAgo: 133, started: true, finished: true, durationMs: 1480, attempt: 2, err: "SMTP connection timed out", step: "failed"},
			{suffix: "05", status: "queued"},
			{suffix: "06", status: "canceled", minutesAgo: 221, started: true, finished: true, durationMs: 240, step: "skipped"},
		}},
	{id: "clseedwfendingfup00001", key: "ending", name: "Ending card follow-up", description: "Notify the team when a specific ending card is reached.", status: "draft",
		to: "team@example.com", subject: "Respondent reached a key ending", body: "A respondent reached a tracked ending card."},
	{id: "clseedwfteamnotice0001", key: "team", name: "Team notification", description: "Send an internal notification for every completed response.", status: "disabled",
		to: "team@example.com", subject: "New survey response received", body: "A new response was completed. Check Forma for details.",
		runs: []runSpec{
			{suffix: "01", status: "completed", minutesAgo: 1440, started: true, finished: true, durationMs: 540, step: "succeeded"},
			{suffix: "02", status: "failed", minutesAgo: 1505, started: true, finished: true, durationMs: 910, attempt: 1, err: "Recipient mailbox full", step: "failed"},
		}},
}

// workflowDefinition is a one-step workflow: on a completed response to the survey, send one email.
func workflowDefinition(key string, surveyID uuid.UUID, to, subject, body string) json.RawMessage {
	trigger, action := key+"trigger", key+"action"
	raw, err := json.Marshal(map[string]any{
		"schemaVersion": 1,
		"entryNodeId":   trigger,
		"trigger": map[string]any{
			"id": trigger, "type": "trigger", "triggerType": "response.completed",
			"config": map[string]any{"surveyId": surveyID.String(), "endingCardIds": []string{}},
			"ui":     map[string]any{"position": map[string]any{"x": 220, "y": 80}},
		},
		"nodes": []any{map[string]any{
			"id": action, "type": "action", "actionType": "send_email", "label": "Send email",
			"config": map[string]any{"to": to, "from": "team@example.com", "replyTo": []string{}, "subject": subject, "body": body, "attachResponseData": false},
			"ui":     map[string]any{"position": map[string]any{"x": 220, "y": 200}},
		}},
		"edges": []any{map[string]any{"id": key + "edge", "source": trigger, "target": action}},
	})
	if err != nil {
		panic(err)
	}
	return raw
}

// workflow writes a workflow in the status given, rewriting it on a re-seed. A workflow that is or was live (enabled, disabled, archived) gets the published version enabling it would have written, so the runner and the API see what they would after a real enable.
func (s *seeder) workflow(id uuid.UUID, name, description, status string, definition json.RawMessage) error {
	if _, issues := workflow.ParseDefinition(definition); len(issues) > 0 {
		return fmt.Errorf("seed workflow %s is not a valid workflow: %v", name, issues)
	}
	var triggerSurvey *uuid.UUID
	if status != "draft" {
		triggerSurvey = &KitchenSinkSurveyID
	}
	if err := s.exec("workflow", `INSERT INTO workflows (id, workspace_id, created_by, name, description, status, definition, trigger_survey_id) VALUES (?, ?, ?, ?, ?, ?, ?::jsonb, ?)
		ON CONFLICT (id) DO UPDATE SET name = excluded.name, description = excluded.description, status = excluded.status, definition = excluded.definition,
			trigger_survey_id = excluded.trigger_survey_id, updated_at = now()`,
		id, WorkspaceID, Users[0].ID, name, description, status, string(definition), triggerSurvey); err != nil {
		return err
	}
	if status == "draft" {
		return nil
	}
	return s.exec("workflow version", `INSERT INTO workflow_versions (workflow_id, workspace_id, version, definition, published_by) VALUES (?, ?, 1, ?::jsonb, ?)
		ON CONFLICT (workflow_id, version) DO UPDATE SET definition = excluded.definition`, id, WorkspaceID, string(definition), Users[0].ID)
}

// workflows is forma2's seedDemoWorkflows and seedDemoWorkflowRuns: three demo workflows on the kitchen sink survey, and run history linked to its seeded responses so the runs list and the run detail have something to show.
func (s *seeder) workflows() error {
	cursor := 0
	for _, w := range demoWorkflows {
		id := legacyid.ID(w.id)
		if err := s.workflow(id, w.name, w.description, w.status, workflowDefinition(w.key, KitchenSinkSurveyID, w.to, w.subject, w.body)); err != nil {
			return err
		}
		for _, r := range w.runs {
			responseID := ResponseID("clseedsurveykitchen00", cursor)
			cursor++
			if err := s.run(id, w, r, responseID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *seeder) run(workflowID uuid.UUID, w workflowSpec, r runSpec, responseID uuid.UUID) error {
	runID := legacyid.ID("clseedwfrun" + w.key + r.suffix)
	created := s.now.Add(-time.Duration(r.minutesAgo) * time.Minute)
	var started, finished, lastErrorAt *time.Time
	if r.started {
		t := created.Add(2 * time.Second)
		started = &t
		if r.finished {
			f := t.Add(time.Duration(r.durationMs) * time.Millisecond)
			finished = &f
		}
	}
	if r.err != "" {
		lastErrorAt = finished
	}
	updated := created
	switch {
	case finished != nil:
		updated = *finished
	case started != nil:
		updated = *started
	}
	payload := map[string]any{"type": "response.completed", "workspaceId": WorkspaceID, "surveyId": KitchenSinkSurveyID, "responseId": responseID, "triggeredAt": created.Format(time.RFC3339Nano)}
	input := map[string]any{"to": w.to, "subject": w.subject}
	output := map[string]any{}
	if r.step == "succeeded" {
		output = map[string]any{"provider": "smtp", "messageId": "seed-" + runID.String(), "accepted": []string{w.to}}
	}
	steps := []any{}
	if r.step != "" {
		step := map[string]any{"stepId": w.key + "action", "stepType": "send_email", "status": r.step, "input": input, "output": output}
		if r.err != "" {
			step["error"] = r.err
		}
		if started != nil {
			step["startedAt"] = started.Format(time.RFC3339Nano)
		}
		if finished != nil {
			step["finishedAt"] = finished.Format(time.RFC3339Nano)
		}
		steps = append(steps, step)
	}
	var runErr *string
	if r.err != "" {
		runErr = &r.err
	}
	if err := s.exec("workflow run", `INSERT INTO workflow_runs (id, workflow_id, workspace_id, response_id, survey_id, status, trigger_type, attempt, last_error_at, trigger_payload, data, error, created_at, updated_at, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, 'response.completed', ?, ?, ?::jsonb, ?::jsonb, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET status = excluded.status, attempt = excluded.attempt, last_error_at = excluded.last_error_at, trigger_payload = excluded.trigger_payload,
			data = excluded.data, error = excluded.error, created_at = excluded.created_at, updated_at = excluded.updated_at, started_at = excluded.started_at, finished_at = excluded.finished_at`,
		runID, workflowID, WorkspaceID, responseID, KitchenSinkSurveyID, r.status, r.attempt, lastErrorAt, jsonOf(payload), jsonOf(map[string]any{"trigger": payload, "steps": steps}), runErr,
		created, updated, started, finished); err != nil {
		return err
	}
	// The trace is rewritten, so a re-seed matches the run's current shape.
	if err := s.exec("workflow run log", `DELETE FROM workflow_run_logs WHERE run_id = ?`, runID); err != nil {
		return err
	}
	if r.step == "" {
		return nil
	}
	// Sequence 1, as every row the runner writes.
	return s.exec("workflow run log", `INSERT INTO workflow_run_logs (id, run_id, sequence, step_id, step_type, status, input, output, error, started_at, finished_at)
		VALUES (?, ?, 1, ?, 'send_email', ?, ?::jsonb, ?::jsonb, ?, ?, ?)`,
		legacyid.ID("clseedwfrunlog"+w.key+r.suffix), runID, w.key+"action", r.step, jsonOf(input), jsonOf(output), runErr, started, finished)
}
