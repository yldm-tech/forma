package ai

import (
	"context"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

// Availability is whether a surface may offer AI, and if not why, forma2's TSurveyAIAvailability and checkAITranslationAvailableAction's answer. It drives the SPA's AI unavailable alert.
type Availability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// OrganizationAvailability is an organization's AI configuration with its availability, for the organization settings' AI toggle.
type OrganizationAvailability struct {
	OrganizationConfig
	Availability
}

// availability is what the SPA's AI surfaces are told. Unlike forma2's reason order, which generation and the v3 API keep (UnavailableReason), an instance without a provider is reported ahead of the organization's toggle: turning the toggle on cannot fix it, so naming the toggle sent users to a setting that does nothing.
func availability(cfg *OrganizationConfig) Availability {
	reason := cfg.UnavailableReason()
	if reason == ReasonNotEnabled && !cfg.IsInstanceConfigured {
		reason = ReasonInstanceNotConfigured
	}
	return Availability{Available: reason == "", Reason: reason}
}

// OrganizationAvailability answers any member of the organization (organization.read).
func (s *Service) OrganizationAvailability(ctx context.Context, userID, organizationID uuid.UUID) (*OrganizationAvailability, error) {
	if err := s.authz.Require(ctx, authz.User(userID), authz.OrganizationRead, authz.Organization(organizationID)); err != nil {
		return nil, err
	}
	cfg, err := s.OrganizationConfig(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	return &OrganizationAvailability{OrganizationConfig: *cfg, Availability: availability(cfg)}, nil
}

// Availability is what the SPA's AI surfaces are told about an organization, for a caller that has already authorized the surface it serves (the analysis page's text summaries authorize reading the survey's responses).
func (s *Service) Availability(ctx context.Context, organizationID uuid.UUID) (*Availability, error) {
	cfg, err := s.OrganizationConfig(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	a := availability(cfg)
	return &a, nil
}

// WorkspaceAvailability is forma2's getSurveyAIAvailability for a workspace's survey list: a caller who can read the workspace but not write to it is told read_only before anything else.
func (s *Service) WorkspaceAvailability(ctx context.Context, userID, workspaceID uuid.UUID) (*Availability, error) {
	organizationID, err := s.repo.workspaceOrganization(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if organizationID == uuid.Nil {
		return nil, apperr.NotFound("Workspace", "")
	}
	actor, resource := authz.User(userID), authz.Workspace(workspaceID)
	if err := s.authz.Require(ctx, actor, authz.WorkspaceRead, resource); err != nil {
		return nil, err
	}
	canWrite, err := s.authz.Can(ctx, actor, authz.WorkspaceWrite, resource)
	if err != nil {
		return nil, err
	}
	if !canWrite {
		return &Availability{Reason: ReasonReadOnly}, nil
	}
	cfg, err := s.OrganizationConfig(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	a := availability(cfg)
	return &a, nil
}

// SurveyTranslationAvailability is forma2's checkAITranslationAvailableAction: workspace.read on the survey's workspace, then the organization's availability.
func (s *Service) SurveyTranslationAvailability(ctx context.Context, userID, surveyID uuid.UUID) (*Availability, error) {
	owner, err := s.repo.surveyOwner(ctx, surveyID)
	if err != nil {
		return nil, err
	}
	if owner == nil {
		return nil, apperr.NotFound("Survey", surveyID.String())
	}
	if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceRead, authz.Workspace(owner.WorkspaceID)); err != nil {
		return nil, err
	}
	cfg, err := s.OrganizationConfig(ctx, owner.OrganizationID)
	if err != nil {
		return nil, err
	}
	a := availability(cfg)
	return &a, nil
}

// TranslateSurveyFieldsInput is forma2's ZTranslateSurveyFieldsAction. The languages are the names the editor shows (English, Deutsch, ...), which is what the model is told to translate between.
type TranslateSurveyFieldsInput struct {
	Fields         []TranslationField `json:"fields" binding:"required,min=1,dive"`
	SourceLanguage string             `json:"sourceLanguage" binding:"required,min=1"`
	TargetLanguage string             `json:"targetLanguage" binding:"required,min=1"`
}

// TranslateSurveyFields is forma2's translateSurveyFieldsAction: workspace.write, then the organization's AI, then the translation, partial by contract.
func (s *Service) TranslateSurveyFields(ctx context.Context, userID, workspaceID uuid.UUID, in TranslateSurveyFieldsInput) (*TranslationResult, error) {
	organizationID, err := s.repo.workspaceOrganization(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if organizationID == uuid.Nil {
		return nil, apperr.NotFound("Workspace", "")
	}
	if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceWrite, authz.Workspace(workspaceID)); err != nil {
		return nil, err
	}
	if _, err := s.Assert(ctx, organizationID); err != nil {
		return nil, err
	}
	return s.TranslateFields(ctx, organizationID, in.Fields, in.SourceLanguage, in.TargetLanguage)
}
