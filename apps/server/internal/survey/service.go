package survey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/ai"
	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
	"github.com/yldm-tech/forma/apps/server/internal/survey/scheduling"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// errSlugTaken is a slug another survey already holds.
var errSlugTaken = errors.New("survey slug taken")

// Caller is who acts: a user (the editor, or a v3 call with a session) or an API key.
type Caller struct {
	Actor authz.Actor
}

// UserCaller and APIKeyCaller name a caller.
func UserCaller(id uuid.UUID) Caller   { return Caller{Actor: authz.User(id)} }
func APIKeyCaller(id uuid.UUID) Caller { return Caller{Actor: authz.APIKey(id)} }

// userID is the acting user, or nil for an API key, forma2's createdBy.
func (c Caller) userID() *uuid.UUID {
	if c.Actor.Type == authz.ActorUser {
		id := c.Actor.ID
		return &id
	}
	return nil
}

func (c Caller) auditActor() (audit.ActorType, string) {
	if c.Actor.Type == authz.ActorAPIKey {
		return audit.ActorAPI, c.Actor.ID.String()
	}
	return audit.ActorUser, c.Actor.ID.String()
}

// Service holds the survey rules. Every exported method authorizes the caller through authz before it reads or writes anything the caller could not already see.
type Service struct {
	cfg       config.Config
	repo      *Repository
	authz     *authz.Authorizer
	limiter   *ratelimit.Limiter
	audit     *audit.Emitter
	docs      *surveydoc.Validator
	scheduler *Scheduler
	schedule  scheduling.Schedule
	cipher    *secret.Cipher
	catalog   *Catalog
	ai        *ai.Service
	log       *slog.Logger
	now       func() time.Time
}

func NewService(cfg config.Config, repo *Repository, authorizer *authz.Authorizer, limiter *ratelimit.Limiter, emitter *audit.Emitter, docs *surveydoc.Validator, cipher *secret.Cipher, catalog *Catalog, aiService *ai.Service, log *slog.Logger) *Service {
	return &Service{
		cfg: cfg, repo: repo, authz: authorizer, limiter: limiter, audit: emitter, docs: docs, cipher: cipher, catalog: catalog, ai: aiService, log: log, now: time.Now,
		scheduler: NewScheduler(repo, emitter, log), schedule: scheduling.Instance(cfg),
	}
}

// PublicDomain is forma2's getPublicDomain: the origin survey links are built on, PUBLIC_URL when it is set and WEBAPP_URL otherwise, without a trailing slash so a path can be appended to it.
func (s *Service) PublicDomain() string {
	u := s.cfg.WebAppURL
	if s.cfg.PublicURL != nil {
		u = s.cfg.PublicURL
	}
	return strings.TrimRight(u.String(), "/")
}

// record emits forma2's audit event for a survey mutation; a failure is recorded without objects.
func (s *Service) record(ctx context.Context, caller Caller, action audit.Action, target audit.Target, targetID string, organizationID *uuid.UUID, err error, oldObject, newObject any) {
	actorType, actorID := caller.auditActor()
	e := audit.Event{Action: action, TargetType: target, TargetID: targetID, ActorType: actorType, ActorID: actorID, Status: audit.StatusSuccess, OldObject: oldObject, NewObject: newObject}
	if organizationID != nil {
		e.OrganizationID = organizationID.String()
	}
	if err != nil {
		e.Status, e.OldObject, e.NewObject = audit.StatusFailure, nil, nil
	}
	s.audit.Log(ctx, audit.FromRequest(ctx, e))
}

// organizationOf is the survey's organization, for audit events.
func (s *Service) organizationOf(ctx context.Context, workspaceID uuid.UUID) *uuid.UUID {
	org, err := s.repo.organizationOf(ctx, workspaceID)
	if err != nil {
		s.log.ErrorContext(ctx, "resolve survey organization for audit", "err", err)
		return nil
	}
	return org
}

// authorized loads a survey and checks the caller may take a survey action on it, which authz evaluates on the survey's workspace. A missing survey is forma2's ResourceNotFoundError("Survey"), a denial its AuthorizationError.
func (s *Service) authorized(ctx context.Context, caller Caller, id uuid.UUID, action authz.Action) (*Survey, error) {
	sv, err := s.repo.byID(ctx, id)
	if err != nil {
		return nil, err
	}
	if sv == nil {
		return nil, apperr.NotFound("Survey", "")
	}
	if err := s.authz.Require(ctx, caller.Actor, action, authz.Survey(sv.ID)); err != nil {
		return nil, err
	}
	return sv, nil
}

// stateMutation is forma2's actions.stateMutation limit, per workspace.
func (s *Service) stateMutation(ctx context.Context, workspaceID uuid.UUID) error {
	return s.limiter.Apply(ctx, ratelimit.ActionStateMutation, workspaceID.String())
}

// Get is the survey for the editor; reading needs workspace.read.
func (s *Service) Get(ctx context.Context, caller Caller, id uuid.UUID) (*Survey, error) {
	return s.authorized(ctx, caller, id, authz.SurveyRead)
}

// ValidationError is a document that breaks forma2's rules: 400 with every issue in details.issues, the first one as the message.
func ValidationError(issues []surveydoc.Issue) *apperr.Error {
	return &apperr.Error{Status: http.StatusBadRequest, Code: apperr.CodeValidation, Message: issues[0].Message, Details: map[string]any{"issues": issues}}
}

// declared reads the declared-field carriers of a document part pair; an absent part is "not declared".
func declared(variables, hiddenFields json.RawMessage) (surveydoc.DeclaredFields, error) {
	var d surveydoc.DeclaredFields
	if !isNullJSON(variables) {
		var vs []surveydoc.Variable
		if err := json.Unmarshal(variables, &vs); err != nil {
			return d, apperr.InvalidInput(apperr.CodeValidation, "Invalid variables")
		}
		d.Variables = &vs
	}
	if !isNullJSON(hiddenFields) {
		var h surveydoc.HiddenFields
		if err := json.Unmarshal(hiddenFields, &h); err != nil {
			return d, apperr.InvalidInput(apperr.CodeValidation, "Invalid hidden fields")
		}
		d.HiddenFields = &h
	}
	return d, nil
}

// guardDeclaredFields is forma2's declared-field guard on save: newly declared variable and hidden-field names must pass the strict naming rule and must not clash.
func guardDeclaredFields(existing, incoming surveydoc.DeclaredFields) error {
	if errs := surveydoc.ValidateNewDeclaredFields(existing, incoming); len(errs) > 0 {
		return apperr.InvalidInput(apperr.CodeBadRequest, surveydoc.DescribeDeclaredFieldNameErrors(errs))
	}
	return nil
}

// appTriggerMessage is forma2's APP_SURVEY_TRIGGER_REQUIRED_MESSAGE.
const appTriggerMessage = "An app survey must have at least one trigger to be set to a non-draft status."

func appSurveyMissingTriggers(surveyType, status string, triggers int) bool {
	return surveyType == TypeApp && status != StatusDraft && triggers == 0
}

// validateTriggers is forma2's handleTriggerUpdates check: every id must be one of the workspace's action classes, and none may repeat.
func (s *Service) validateTriggers(ctx context.Context, workspaceID uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	classes, err := s.repo.actionClasses(ctx, workspaceID)
	if err != nil {
		return err
	}
	known := map[uuid.UUID]bool{}
	for _, c := range classes {
		known[c.ID] = true
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if !known[id] {
			return apperr.InvalidInput(apperr.CodeBadRequest, "Invalid trigger id")
		}
		if seen[id] {
			return apperr.InvalidInput(apperr.CodeBadRequest, "Duplicate trigger id")
		}
		seen[id] = true
	}
	return nil
}

// checkLanguages is forma2's tenant guard on survey languages: every language must be the workspace's own.
func (s *Service) checkLanguages(ctx context.Context, workspaceID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]Language, error) {
	found, err := s.repo.workspaceLanguagesByID(ctx, workspaceID, ids)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, ok := found[id]; !ok {
			return nil, apperr.NotFound("Language", id.String())
		}
	}
	return found, nil
}

// subscribeCreator is forma2's subscribeOrganizationMembersToSurveyResponses: the creator gets response alerts for the new survey unless they unsubscribed from the organization.
func (s *Service) subscribeCreator(ctx context.Context, surveyID, userID, organizationID uuid.UUID) error {
	_, err := s.repo.exec(ctx, `UPDATE users SET notification_settings = jsonb_set(
			CASE WHEN jsonb_typeof(notification_settings->'alert') = 'object' THEN notification_settings ELSE notification_settings || '{"alert": {}}'::jsonb END,
			ARRAY['alert', ?::text], 'true'::jsonb), updated_at = now()
		WHERE id = ? AND NOT COALESCE(notification_settings->'unsubscribedOrganizationIds' @> to_jsonb(?::text), false)`,
		surveyID.String(), userID, organizationID.String())
	if err != nil {
		return fmt.Errorf("subscribe survey creator: %w", err)
	}
	return nil
}

// newSurvey is everything a create writes, already validated.
type newSurvey struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	Record      record
	Languages   []languageLink
	TriggerIDs  []uuid.UUID
	FollowUps   []followUpWrite
	// SegmentID connects an existing segment; an app survey without one gets a private segment with PrivateSegmentFilters.
	SegmentID             *uuid.UUID
	PrivateSegmentFilters json.RawMessage
}

// insertSurvey is the write half of forma2's createSurvey: one transaction creates the survey, its private segment for an app survey, its triggers, languages, follow-ups and embedded data; afterwards a schedule already due is applied and the creator subscribed to response alerts.
func (s *Service) insertSurvey(ctx context.Context, n newSurvey, organizationID uuid.UUID) (*Survey, error) {
	if n.ID == uuid.Nil {
		n.ID = uuid.Must(uuid.NewV7())
	}
	doc, err := declared(n.Record.Variables, n.Record.HiddenFields)
	if err != nil {
		return nil, err
	}
	err = s.repo.transaction(ctx, func(tx *Repository) error {
		if err := tx.insert(ctx, n.ID, n.WorkspaceID, n.Record); err != nil {
			return err
		}
		segmentID := n.SegmentID
		if n.Record.Type == TypeApp && segmentID == nil {
			id, err := tx.createSegment(ctx, n.WorkspaceID, n.ID.String(), true, n.PrivateSegmentFilters)
			if err != nil {
				return err
			}
			segmentID = &id
		}
		if segmentID != nil {
			if err := tx.setSurveySegment(ctx, n.ID, segmentID); err != nil {
				return err
			}
		}
		if err := tx.setTriggers(ctx, n.ID, n.TriggerIDs); err != nil {
			return err
		}
		if err := tx.setLanguages(ctx, n.ID, n.Languages); err != nil {
			return err
		}
		if err := tx.applyFollowUps(ctx, n.ID, n.FollowUps); err != nil {
			return err
		}
		return tx.reconcileEmbeddedData(ctx, n.ID, n.WorkspaceID, embeddedPatch{Variables: doc.Variables, HiddenFields: doc.HiddenFields})
	})
	if err != nil {
		if errors.Is(err, errSlugTaken) {
			return nil, apperr.InvalidInput(apperr.CodeConflict, "A survey with this slug already exists")
		}
		return nil, err
	}
	sv, err := s.repo.byID(ctx, n.ID)
	if err != nil {
		return nil, err
	}
	if changed, err := s.scheduler.reconcileIfDue(ctx, sv, s.now()); err != nil {
		return nil, err
	} else if changed {
		if sv, err = s.repo.byID(ctx, n.ID); err != nil {
			return nil, err
		}
	}
	if n.Record.CreatedBy != nil {
		if err := s.subscribeCreator(ctx, n.ID, *n.Record.CreatedBy, organizationID); err != nil {
			return nil, err
		}
	}
	return sv, nil
}

// Archive is forma2's archiveSurvey behind workspace.write: archiving is idempotent, pauses a running survey and keeps its publish date.
func (s *Service) Archive(ctx context.Context, caller Caller, id uuid.UUID) (*Lifecycle, error) {
	sv, err := s.authorized(ctx, caller, id, authz.SurveyWrite)
	if err != nil {
		return nil, err
	}
	out, err := s.repo.archive(ctx, id, s.now())
	if err == nil && out == nil {
		err = apperr.NotFound("Survey", id.String())
	}
	s.record(ctx, caller, audit.ActionArchived, audit.TargetSurvey, id.String(), s.organizationOf(ctx, sv.WorkspaceID), err, sv, out)
	return out, err
}

// Restore is forma2's restoreSurvey behind workspace.write: the status is left as it is.
func (s *Service) Restore(ctx context.Context, caller Caller, id uuid.UUID) (*Lifecycle, error) {
	sv, err := s.authorized(ctx, caller, id, authz.SurveyWrite)
	if err != nil {
		return nil, err
	}
	out, err := s.repo.restore(ctx, id)
	if err == nil && out == nil {
		err = apperr.NotFound("Survey", id.String())
	}
	s.record(ctx, caller, audit.ActionRestored, audit.TargetSurvey, id.String(), s.organizationOf(ctx, sv.WorkspaceID), err, sv, out)
	return out, err
}

// Delete is forma2's deleteSurvey behind workspace.write (not manage: the v3 route gates it there).
func (s *Service) Delete(ctx context.Context, caller Caller, id uuid.UUID) error {
	sv, err := s.authorized(ctx, caller, id, authz.SurveyDelete)
	if err != nil {
		return err
	}
	deleted, err := s.repo.deleteSurvey(ctx, id, nil)
	if err == nil && deleted == nil {
		err = apperr.NotFound("Survey", id.String())
	}
	s.record(ctx, caller, audit.ActionDeleted, audit.TargetSurvey, id.String(), s.organizationOf(ctx, sv.WorkspaceID), err, sv, nil)
	return err
}

// SetSlug is forma2's updateSurveySlugAction: pretty URLs are a self-hosted feature, slugs are unique across the instance.
func (s *Service) SetSlug(ctx context.Context, caller Caller, id uuid.UUID, slug *string) (*Survey, error) {
	if s.cfg.IsFormaCloud {
		return nil, apperr.OperationNotAllowed("Pretty URLs are only available on self-hosted instances")
	}
	if slug != nil && !slugPattern.MatchString(*slug) {
		return nil, apperr.InvalidInput(apperr.CodeValidation, "Slug can only contain lowercase letters, numbers, and hyphens")
	}
	sv, err := s.authorized(ctx, caller, id, authz.SurveyWrite)
	if err != nil {
		return nil, err
	}
	if err := s.stateMutation(ctx, sv.WorkspaceID); err != nil {
		return nil, err
	}
	found, err := s.repo.setSlug(ctx, id, slug)
	if errors.Is(err, errSlugTaken) {
		return nil, apperr.InvalidInput(apperr.CodeConflict, "A survey with this slug already exists")
	}
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, apperr.NotFound("Survey", id.String())
	}
	return s.repo.byID(ctx, id)
}

// CodeWorkspaceMismatch is the failure of targeting a segment of another workspace.
const CodeWorkspaceMismatch = "workspace_mismatch"

// AttachSegment is forma2's loadNewSegmentAction over loadNewSegmentInSurvey: the survey moves to another segment of its workspace, a private segment it leaves behind is deleted, and the change is audited as an update of the survey.
func (s *Service) AttachSegment(ctx context.Context, caller Caller, id, segmentID uuid.UUID) (out *Survey, err error) {
	sv, err := s.authorized(ctx, caller, id, authz.SurveyWrite)
	if err != nil {
		return nil, err
	}
	defer func() {
		s.record(ctx, caller, audit.ActionUpdated, audit.TargetSurvey, id.String(), s.organizationOf(ctx, sv.WorkspaceID), err, sv, out)
	}()
	seg, err := s.repo.segment(ctx, segmentID)
	if err != nil {
		return nil, err
	}
	if seg == nil {
		return nil, apperr.NotFound("Segment", segmentID.String())
	}
	if seg.WorkspaceID != sv.WorkspaceID {
		return nil, apperr.InvalidInput(CodeWorkspaceMismatch, "Segment and survey are not in the same workspace")
	}
	if err := s.stateMutation(ctx, sv.WorkspaceID); err != nil {
		return nil, err
	}
	err = s.repo.transaction(ctx, func(tx *Repository) error {
		if err := tx.setSurveySegment(ctx, id, &segmentID); err != nil {
			return err
		}
		if prev := sv.Segment; prev != nil && prev.ID != segmentID && prev.IsPrivate && prev.Title == id.String() {
			return tx.deleteSegment(ctx, prev.ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.repo.byID(ctx, id)
}

// segmentSurveyIDsLimit is forma2's MAX_SEGMENT_SURVEYS.
const segmentSurveyIDsLimit = 500

// StatusIn reports whether a status is one of the survey statuses.
func StatusIn(status string) bool { return slices.Contains(Statuses, status) }
