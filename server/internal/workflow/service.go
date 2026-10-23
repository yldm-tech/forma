package workflow

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/v3api"
)

// The lifecycle refusals, forma2's WorkflowInvalidStateError messages.
const (
	archivedReadOnly      = "Archived workflows are read-only; unarchive before editing."
	definitionLocked      = "A workflow's definition can only be updated while it is draft or disabled."
	alreadyArchived       = "Workflow is already archived."
	onlyArchived          = "Only archived workflows can be unarchived."
	onlyDraftOrDisabled   = "Only draft or disabled workflows can be enabled."
	onlyEnabled           = "Only enabled workflows can be disabled."
	archivedNotTestable   = "Archived workflows cannot be tested."
	notExecutableDetail   = "The workflow definition is not executable."
	recipientDetail       = "A send_email recipient cannot access this workspace."
	recipientField        = "definition.nodes.config.to"
	surveyField           = "definition.trigger.config.surveyId"
	endingField           = "definition.trigger.config.endingCardIds"
	surveyMissingReason   = "The referenced survey does not exist in this workspace."
	conflictingWriteRetry = "A conflicting workflow write happened at the same time. Please retry."
)

// Dry-run problem codes, forma2's ZWorkflowTestProblemCode.
const (
	problemNotExecutable    = "definition_not_executable"
	problemSurveyNotFound   = "survey_not_found"
	problemEndingNotFound   = "ending_card_not_found"
	problemRecipientBlocked = "recipient_not_allowed"
)

func invalidState(detail string) *v3api.Problem {
	return &v3api.Problem{Status: http.StatusUnprocessableEntity, Title: "Unprocessable Content", Detail: detail, Code: "invalid_workflow_state"}
}

func notExecutable(detail string, params []v3api.InvalidParam) *v3api.Problem {
	return &v3api.Problem{Status: http.StatusUnprocessableEntity, Title: "Unprocessable Content", Detail: detail, Code: "workflow_not_executable", InvalidParams: params}
}

func conflict(detail string) *v3api.Problem {
	return &v3api.Problem{Status: http.StatusConflict, Title: "Conflict", Detail: detail, Code: "conflict"}
}

func endingReason(id string) string {
	return fmt.Sprintf("Ending card %s does not exist on the survey.", id)
}

func recipientReason(email string) string {
	return fmt.Sprintf("Recipient %s cannot access this workspace. A send_email action may only address someone with access to this workspace or a respondent field.", email)
}

// Service is the workflow management API behind /api/v3/workflows. Every operation authorizes the caller on the workspace the workflow belongs to; an unknown id is refused exactly like one the caller may not see.
type Service struct {
	repo  *Repository
	db    *gorm.DB
	authz *authz.Authorizer
	log   *slog.Logger
}

func NewService(repo *Repository, db *gorm.DB, authorizer *authz.Authorizer, log *slog.Logger) *Service {
	return &Service{repo: repo, db: db, authz: authorizer, log: log}
}

// userOf is the acting user, forma2's createdBy and publishedBy; nil for an API key.
func userOf(actor authz.Actor) *uuid.UUID {
	if actor.Type == authz.ActorUser {
		id := actor.ID
		return &id
	}
	return nil
}

// load is forma2's loadAndAuthorize: the workflow, then the caller's access to its workspace.
func (s *Service) load(ctx context.Context, actor authz.Actor, id uuid.UUID, action authz.Action) (*Workflow, error) {
	w, err := s.repo.byID(ctx, id)
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, v3api.Forbidden("")
	}
	if err := v3api.RequireWorkspace(ctx, s.authz, actor, action, w.WorkspaceID); err != nil {
		return nil, err
	}
	return w, nil
}

// List is a page of the workspace's workflows and the cursor of the next one.
func (s *Service) List(ctx context.Context, actor authz.Actor, q listQuery, cursor string) ([]Workflow, *string, error) {
	if err := v3api.RequireWorkspace(ctx, s.authz, actor, authz.WorkspaceRead, q.WorkspaceID); err != nil {
		return nil, nil, err
	}
	if cursor != "" {
		c, err := decodeListCursor(cursor, q.SortBy)
		if err != nil {
			return nil, nil, err
		}
		q.Cursor = c
	}
	rows, more, err := s.repo.list(ctx, q)
	if err != nil {
		return nil, nil, err
	}
	if !more {
		return rows, nil, nil
	}
	next := nextListCursor(rows[len(rows)-1], q.SortBy)
	return rows, &next, nil
}

// Create stores a new draft.
func (s *Service) Create(ctx context.Context, actor authz.Actor, readBody func() (any, error)) (*Workflow, error) {
	body, err := readBody()
	if err != nil {
		return nil, err
	}
	in, issues := parseCreate(body)
	if len(issues) > 0 {
		return nil, payloadProblem(issues)
	}
	if err := v3api.RequireWorkspace(ctx, s.authz, actor, authz.WorkspaceWrite, in.WorkspaceID); err != nil {
		return nil, err
	}
	created, err := s.repo.create(ctx, in.WorkspaceID, in.Name, in.Description, mustJSON(in.Definition), userOf(actor))
	if err == nil {
		s.capture(ctx, actor, lifecycleEvent{operation: "created", row: created})
	}
	return created, err
}

// Get is one workflow.
func (s *Service) Get(ctx context.Context, actor authz.Actor, id uuid.UUID) (*Workflow, error) {
	return s.load(ctx, actor, id, authz.WorkspaceRead)
}

// Patch edits a workflow's name, description or draft definition. An archived workflow is read-only, and an enabled one's definition is its published version's until it is disabled.
func (s *Service) Patch(ctx context.Context, actor authz.Actor, id uuid.UUID, readBody func() (any, error)) (before, after *Workflow, err error) {
	w, err := s.load(ctx, actor, id, authz.WorkspaceWrite)
	if err != nil {
		return nil, nil, err
	}
	if w.Status == StatusArchived {
		return nil, nil, invalidState(archivedReadOnly)
	}
	body, err := readBody()
	if err != nil {
		return nil, nil, err
	}
	in, issues := parsePatch(body)
	if len(issues) > 0 {
		return nil, nil, payloadProblem(issues)
	}
	if in.Definition != nil && w.Status == StatusEnabled {
		return nil, nil, invalidState(definitionLocked)
	}
	set := map[string]any{}
	if in.Name != nil {
		set["name"] = *in.Name
	}
	if in.DescriptionSet {
		set["description"] = in.Description
	}
	if in.Definition != nil {
		set["definition"] = gorm.Expr("?::jsonb", string(mustJSON(in.Definition)))
	}
	updated, err := s.repo.update(ctx, w.ID, w.WorkspaceID, set)
	return w, updated, err
}

// Duplicate copies a workflow's description and definition into a new draft with no versions or runs, named as given or "<name> (copy)". Archived workflows can be copied.
func (s *Service) Duplicate(ctx context.Context, actor authz.Actor, id uuid.UUID, readBody func() (any, error)) (source, created *Workflow, err error) {
	w, err := s.load(ctx, actor, id, authz.WorkspaceWrite)
	if err != nil {
		return nil, nil, err
	}
	body, err := readBody()
	if err != nil {
		return nil, nil, err
	}
	name, issues := parseDuplicate(body)
	if len(issues) > 0 {
		return nil, nil, payloadProblem(issues)
	}
	copyName := w.Name + " (copy)"
	if name != nil {
		copyName = *name
	}
	created, err = s.repo.create(ctx, w.WorkspaceID, copyName, w.Description, w.Definition, userOf(actor))
	if err == nil {
		s.capture(ctx, actor, lifecycleEvent{operation: "duplicated", row: created, sourceWorkflowID: &w.ID})
	}
	return w, created, err
}

// Delete removes a workflow with its versions, runs and their logs, whatever its status.
func (s *Service) Delete(ctx context.Context, actor authz.Actor, id uuid.UUID) (*Workflow, error) {
	w, err := s.load(ctx, actor, id, authz.WorkspaceWrite)
	if err != nil {
		return nil, err
	}
	if err := s.repo.delete(ctx, w.ID, w.WorkspaceID); err != nil {
		return w, err
	}
	s.capture(ctx, actor, lifecycleEvent{operation: "deleted", row: w})
	return w, nil
}

// Archive takes a workflow of any other status out of use; its trigger survey is kept.
func (s *Service) Archive(ctx context.Context, actor authz.Actor, id uuid.UUID) (before, after *Workflow, err error) {
	w, err := s.load(ctx, actor, id, authz.WorkspaceWrite)
	if err != nil {
		return nil, nil, err
	}
	if w.Status == StatusArchived {
		return nil, nil, invalidState(alreadyArchived)
	}
	after, err = s.repo.update(ctx, w.ID, w.WorkspaceID, map[string]any{"status": StatusArchived})
	s.captureTransition(ctx, actor, "archived", w, after, err)
	return w, after, err
}

// Unarchive returns an archived workflow to draft, never to the status it had.
func (s *Service) Unarchive(ctx context.Context, actor authz.Actor, id uuid.UUID) (before, after *Workflow, err error) {
	w, err := s.load(ctx, actor, id, authz.WorkspaceWrite)
	if err != nil {
		return nil, nil, err
	}
	if w.Status != StatusArchived {
		return nil, nil, invalidState(onlyArchived)
	}
	after, err = s.repo.update(ctx, w.ID, w.WorkspaceID, map[string]any{"status": StatusDraft})
	s.captureTransition(ctx, actor, "unarchived", w, after, err)
	return w, after, err
}

// Disable stops an enabled workflow from starting runs.
func (s *Service) Disable(ctx context.Context, actor authz.Actor, id uuid.UUID) (before, after *Workflow, err error) {
	w, err := s.load(ctx, actor, id, authz.WorkspaceWrite)
	if err != nil {
		return nil, nil, err
	}
	if w.Status != StatusEnabled {
		return nil, nil, invalidState(onlyEnabled)
	}
	after, err = s.repo.update(ctx, w.ID, w.WorkspaceID, map[string]any{"status": StatusDisabled})
	s.captureTransition(ctx, actor, "disabled", w, after, err)
	return w, after, err
}

// readiness is what enable and the dry run check beyond the document: the trigger's survey and endings, and the literal recipients nobody in the workspace owns.
type readiness struct {
	surveyFound       bool
	missingEndings    []string
	blockedRecipients []string
}

func (s *Service) readiness(ctx context.Context, workspaceID uuid.UUID, d *Definition) (*readiness, error) {
	out := &readiness{}
	surveyID, err := uuid.Parse(d.Trigger.Config.SurveyID)
	if err != nil {
		return nil, fmt.Errorf("trigger survey id: %w", err)
	}
	endings, found, err := s.repo.surveyEndings(ctx, surveyID, workspaceID)
	if err != nil {
		return nil, err
	}
	out.surveyFound = found
	if found {
		for _, id := range d.Trigger.Config.EndingCardIDs {
			if !slices.Contains(endings, id) {
				out.missingEndings = append(out.missingEndings, id)
			}
		}
	}
	literal := LiteralRecipients(d)
	if len(literal) == 0 {
		return out, nil
	}
	members, err := MemberEmails(ctx, s.db, workspaceID)
	if err != nil {
		return nil, err
	}
	for _, email := range literal {
		if !members[normalizeEmail(email)] {
			out.blockedRecipients = append(out.blockedRecipients, email)
		}
	}
	return out, nil
}

func issueParams(issues []Issue) []v3api.InvalidParam {
	params := make([]v3api.InvalidParam, len(issues))
	for i, is := range issues {
		params[i] = v3api.InvalidParam{Name: is.Name(), Reason: is.Message}
	}
	return params
}

// Enable publishes the workflow's definition as its next immutable version and starts matching finished responses against it. The definition must be executable, its trigger's survey and endings must exist in the workspace, and every literal recipient must be able to access the workspace.
func (s *Service) Enable(ctx context.Context, actor authz.Actor, id uuid.UUID) (before, after *Workflow, err error) {
	w, err := s.load(ctx, actor, id, authz.WorkspaceWrite)
	if err != nil {
		return nil, nil, err
	}
	if w.Status != StatusDraft && w.Status != StatusDisabled {
		return nil, nil, invalidState(onlyDraftOrDisabled)
	}
	d, issues := ParseExecutable(w.Definition)
	if len(issues) > 0 {
		return nil, nil, notExecutable(notExecutableDetail, issueParams(issues))
	}
	ready, err := s.readiness(ctx, w.WorkspaceID, d)
	if err != nil {
		return nil, nil, err
	}
	if !ready.surveyFound || len(ready.missingEndings) > 0 {
		var params []v3api.InvalidParam
		if !ready.surveyFound {
			params = append(params, v3api.InvalidParam{Name: surveyField, Reason: surveyMissingReason})
		}
		for _, id := range ready.missingEndings {
			params = append(params, v3api.InvalidParam{Name: endingField, Reason: endingReason(id)})
		}
		return nil, nil, notExecutable(notExecutableDetail, params)
	}
	if len(ready.blockedRecipients) > 0 {
		params := make([]v3api.InvalidParam, len(ready.blockedRecipients))
		for i, email := range ready.blockedRecipients {
			params[i] = v3api.InvalidParam{Name: recipientField, Reason: recipientReason(email)}
		}
		return nil, nil, notExecutable(recipientDetail, params)
	}
	surveyID := uuid.MustParse(d.Trigger.Config.SurveyID)
	after, err = s.repo.enable(ctx, w.ID, w.WorkspaceID, mustJSON(d), surveyID, userOf(actor))
	switch {
	case errors.Is(err, errNotEnableable):
		return nil, nil, conflict("The workflow is no longer in a state that can be enabled.")
	case errors.Is(err, errVersionRace):
		return nil, nil, conflict("The workflow was just enabled by another request. Please retry.")
	case errors.Is(err, errWorkflowMissed):
		return nil, nil, conflict("The workflow no longer exists.")
	case err != nil:
		return nil, nil, err
	}
	s.captureTransition(ctx, actor, "enabled", w, after, nil)
	return w, after, nil
}

// Test is the dry run: every problem enable would refuse the workflow for, collected rather than stopped at, with no run created and nothing sent. Reading the workspace is enough.
func (s *Service) Test(ctx context.Context, actor authz.Actor, id uuid.UUID) (*TestResult, error) {
	w, err := s.load(ctx, actor, id, authz.WorkspaceRead)
	if err != nil {
		return nil, err
	}
	if w.Status == StatusArchived {
		return nil, invalidState(archivedNotTestable)
	}
	problems := []TestProblem{}
	d, issues := ParseExecutable(w.Definition)
	for _, is := range issues {
		field := is.Name()
		if field == "" {
			field = "definition"
		}
		problems = append(problems, TestProblem{Code: problemNotExecutable, Field: field, Message: is.Message})
	}
	if d != nil {
		ready, err := s.readiness(ctx, w.WorkspaceID, d)
		if err != nil {
			return nil, err
		}
		if !ready.surveyFound {
			problems = append(problems, TestProblem{Code: problemSurveyNotFound, Field: surveyField, Message: surveyMissingReason})
		}
		for _, id := range ready.missingEndings {
			problems = append(problems, TestProblem{Code: problemEndingNotFound, Field: endingField, Message: endingReason(id)})
		}
		for _, email := range ready.blockedRecipients {
			problems = append(problems, TestProblem{Code: problemRecipientBlocked, Field: recipientField, Message: recipientReason(email)})
		}
	}
	ok := len(problems) == 0
	s.capture(ctx, actor, lifecycleEvent{operation: "tested", row: w, testOK: &ok})
	return &TestResult{WorkflowID: w.ID, OK: ok, Problems: problems}, nil
}

// ListRuns is a page of the workspace's runs, newest first.
func (s *Service) ListRuns(ctx context.Context, actor authz.Actor, q runQuery, cursor string) ([]Run, *string, error) {
	if err := v3api.RequireWorkspace(ctx, s.authz, actor, authz.WorkspaceRead, q.WorkspaceID); err != nil {
		return nil, nil, err
	}
	if cursor != "" {
		c, err := decodeRunCursor(cursor)
		if err != nil {
			return nil, nil, err
		}
		q.Cursor = c
	}
	rows, more, err := s.repo.listRuns(ctx, q)
	if err != nil {
		return nil, nil, err
	}
	if !more {
		return rows, nil, nil
	}
	next := nextRunCursor(rows[len(rows)-1])
	return rows, &next, nil
}

// GetRun is one run with its steps, authorized on the run's own workspace.
func (s *Service) GetRun(ctx context.Context, actor authz.Actor, id uuid.UUID) (*Run, error) {
	r, err := s.repo.run(ctx, id)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, v3api.Forbidden("")
	}
	if err := v3api.RequireWorkspace(ctx, s.authz, actor, authz.WorkspaceRead, r.WorkspaceID); err != nil {
		return nil, err
	}
	return r, nil
}

// AuthoringContext is what the send_email inspector authors against: who can be addressed, from the same source enable and the runner check, and the address emails are sent from.
type AuthoringContext struct {
	TeamMemberDetails []Member `json:"teamMemberDetails"`
	MailFrom          string   `json:"mailFrom"`
}

// Members is the workspace's recipient allowlist with names, for a user who can read the workspace.
func (s *Service) Members(ctx context.Context, userID, workspaceID uuid.UUID) ([]Member, error) {
	if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceRead, authz.Workspace(workspaceID)); err != nil {
		return nil, err
	}
	members, err := Members(ctx, s.db, workspaceID)
	if members == nil && err == nil {
		members = []Member{}
	}
	return members, err
}

// organizationOf is the workspace's organization id for audit events; empty when it cannot be read.
func (s *Service) organizationOf(ctx context.Context, workspaceID uuid.UUID) string {
	id, err := s.repo.organizationOf(ctx, workspaceID)
	if err != nil {
		s.log.ErrorContext(ctx, "resolve workflow organization for audit", "err", err)
		return ""
	}
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}
