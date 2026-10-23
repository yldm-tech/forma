package actionclass

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/v3api"
)

// Service holds the action class rules. Every method authorizes through authz before it reads or writes anything.
type Service struct {
	repo    *Repository
	authz   *authz.Authorizer
	limiter *ratelimit.Limiter
	audit   *audit.Emitter
}

func NewService(repo *Repository, authorizer *authz.Authorizer, limiter *ratelimit.Limiter, emitter *audit.Emitter) *Service {
	return &Service{repo: repo, authz: authorizer, limiter: limiter, audit: emitter}
}

// record emits forma2's audit event for an action class mutation, a failure included.
func (s *Service) record(ctx context.Context, action audit.Action, id uuid.UUID, organizationID *uuid.UUID, err error, oldObject, newObject any) {
	e := audit.Event{Action: action, TargetType: audit.TargetActionClass, ActorType: audit.ActorUser, Status: audit.StatusSuccess, OldObject: oldObject, NewObject: newObject}
	if id != uuid.Nil {
		e.TargetID = id.String()
	}
	if organizationID != nil {
		e.OrganizationID = organizationID.String()
	}
	if err != nil {
		e.Status, e.OldObject, e.NewObject = audit.StatusFailure, nil, nil
	}
	s.audit.Log(ctx, audit.FromRequest(ctx, e))
}

// List is the user-actions settings page's read: workspace.read, oldest first.
func (s *Service) List(ctx context.Context, userID, workspaceID uuid.UUID) ([]*ActionClass, error) {
	if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceRead, authz.Workspace(workspaceID)); err != nil {
		return nil, err
	}
	return s.repo.ofWorkspace(ctx, workspaceID)
}

// ListV3 is GET /api/v3/action-classes: a session or API key with read access to the workspace; a missing workspace is refused like a foreign one.
func (s *Service) ListV3(ctx context.Context, p *v3api.Principal, workspaceID uuid.UUID) ([]*ActionClass, error) {
	if err := v3api.RequireWorkspace(ctx, s.authz, p, authz.WorkspaceRead, workspaceID); err != nil {
		return nil, err
	}
	return s.repo.ofWorkspace(ctx, workspaceID)
}

// Create is forma2's createActionClassAction: workspace.write, then the state-mutation limit per workspace. A name or code key already used in the workspace is a 409.
func (s *Service) Create(ctx context.Context, userID, workspaceID uuid.UUID, in Input) (*ActionClass, error) {
	var org *uuid.UUID
	ac, err := func() (*ActionClass, error) {
		if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceWrite, authz.Workspace(workspaceID)); err != nil {
			return nil, err
		}
		var err error
		if org, err = s.repo.organizationOf(ctx, workspaceID); err != nil {
			return nil, err
		}
		if err := s.limiter.Apply(ctx, ratelimit.ActionStateMutation, workspaceID.String()); err != nil {
			return nil, err
		}
		return s.repo.create(ctx, workspaceID, in)
	}()
	id := uuid.Nil
	if ac != nil {
		id = ac.ID
	}
	s.record(ctx, audit.ActionCreated, id, org, err, nil, ac)
	if err == nil {
		captureCreated(userID, org, workspaceID, in)
	}
	return ac, err
}

// captureCreated is forma2's action_class_created: its type and how it triggers (codeAction, or the no-code trigger type).
func captureCreated(userID uuid.UUID, organizationID *uuid.UUID, workspaceID uuid.UUID, in Input) {
	trigger := "codeAction"
	if in.Type != "code" {
		var config struct {
			Type string `json:"type"`
		}
		trigger = "noCode"
		if json.Unmarshal(in.NoCodeConfig, &config) == nil && config.Type != "" {
			trigger = config.Type
		}
	}
	org := ""
	if organizationID != nil {
		org = organizationID.String()
	}
	analytics.Capture(userID.String(), "action_class_created", analytics.Properties{
		"organization_id": org,
		"workspace_id":    workspaceID.String(),
		"type":            in.Type,
		"trigger_type":    trigger,
	}, analytics.Groups{OrganizationID: org, WorkspaceID: workspaceID.String()})
}

// target loads an action class and authorizes action on its own workspace. A missing action class is forma2's ResourceNotFoundError("ActionClass").
func (s *Service) target(ctx context.Context, userID, id uuid.UUID, action authz.Action) (*ActionClass, *uuid.UUID, error) {
	ac, err := s.repo.byID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if ac == nil {
		return nil, nil, apperr.NotFound("ActionClass", id.String())
	}
	if err := s.authz.Require(ctx, authz.User(userID), action, authz.Workspace(ac.WorkspaceID)); err != nil {
		return nil, nil, err
	}
	org, err := s.repo.organizationOf(ctx, ac.WorkspaceID)
	return ac, org, err
}

// Update is forma2's updateActionClassAction: workspace.write on the action class's own workspace, so the input can never move it elsewhere. Unlike forma2 it also applies the state-mutation limit, as every other settings write does.
func (s *Service) Update(ctx context.Context, userID, id uuid.UUID, in Input) (*ActionClass, error) {
	old, org, err := s.target(ctx, userID, id, authz.WorkspaceWrite)
	var updated *ActionClass
	if err == nil {
		err = s.limiter.Apply(ctx, ratelimit.ActionStateMutation, old.WorkspaceID.String())
	}
	if err == nil {
		updated, err = s.repo.update(ctx, id, old.WorkspaceID, in)
	}
	s.record(ctx, audit.ActionUpdated, id, org, err, old, updated)
	return updated, err
}

// Delete is forma2's deleteActionClassAction; the surveys it triggered lose the trigger. Unlike forma2 it also applies the state-mutation limit.
func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) (*ActionClass, error) {
	old, org, err := s.target(ctx, userID, id, authz.WorkspaceWrite)
	if err == nil {
		err = s.limiter.Apply(ctx, ratelimit.ActionStateMutation, old.WorkspaceID.String())
	}
	if err == nil {
		var deleted bool
		if deleted, err = s.repo.delete(ctx, id, old.WorkspaceID); err == nil && !deleted {
			err = apperr.NotFound("Action", id.String())
		}
	}
	s.record(ctx, audit.ActionDeleted, id, org, err, old, nil)
	if err != nil {
		return nil, err
	}
	return old, nil
}

// SurveyUsage is forma2's getActiveInactiveSurveysAction result: the names of the surveys an action class triggers, split on whether they are in progress.
type SurveyUsage struct {
	ActiveSurveys   []string `json:"activeSurveys"`
	InactiveSurveys []string `json:"inactiveSurveys"`
}

// SurveysUsing needs workspace.read on the action class's workspace. exclude leaves out the survey being edited.
func (s *Service) SurveysUsing(ctx context.Context, userID, id uuid.UUID, exclude *uuid.UUID) (*SurveyUsage, error) {
	ac, _, err := s.target(ctx, userID, id, authz.WorkspaceRead)
	if err != nil {
		return nil, err
	}
	surveys, err := s.repo.surveysTriggeredBy(ctx, id, ac.WorkspaceID, exclude)
	if err != nil {
		return nil, err
	}
	out := &SurveyUsage{ActiveSurveys: []string{}, InactiveSurveys: []string{}}
	for _, sv := range surveys {
		if sv.Status == "inProgress" {
			out.ActiveSurveys = append(out.ActiveSurveys, sv.Name)
		} else {
			out.InactiveSurveys = append(out.InactiveSurveys, sv.Name)
		}
	}
	return out, nil
}
