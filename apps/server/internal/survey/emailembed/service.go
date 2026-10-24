package emailembed

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/apps/server/internal/responsemail"
	"github.com/yldm-tech/forma/apps/server/internal/survey"
)

// Service holds the share modal's email routes. Every method authorizes the acting user on the survey's workspace.
type Service struct {
	db        *gorm.DB
	surveys   *survey.Service
	authz     *authz.Authorizer
	mailer    *mailer.Mailer
	generator *Generator
}

func NewService(cfg config.Config, db *gorm.DB, surveys *survey.Service, authorizer *authz.Authorizer, m *mailer.Mailer) (*Service, error) {
	webAppURL := ""
	if cfg.WebAppURL != nil {
		webAppURL = cfg.WebAppURL.String()
	}
	g, err := NewGenerator(webAppURL, surveys.PublicDomain())
	if err != nil {
		return nil, err
	}
	return &Service{db: db, surveys: surveys, authz: authorizer, mailer: m, generator: g}, nil
}

// surveyContext is what the routes read besides the survey: its workspace's styling and organization logo, and the acting user's address and locale.
type surveyContext struct {
	WorkspaceID      uuid.UUID
	WorkspaceStyling json.RawMessage
	LogoURL          *string
}

// surveyWorkspace is forma2's getWorkspaceIdFromSurveyId with the workspace's styling and its organization's email logo; a missing survey is ResourceNotFoundError("survey").
func (s *Service) surveyWorkspace(ctx context.Context, surveyID uuid.UUID) (*surveyContext, error) {
	var rows []surveyContext
	if err := s.db.WithContext(ctx).Raw(`SELECT s.workspace_id, w.styling AS workspace_styling, o.whitelabel ->> 'logoUrl' AS logo_url
		FROM surveys s JOIN workspaces w ON w.id = s.workspace_id JOIN organizations o ON o.id = w.organization_id WHERE s.id = ?`, surveyID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("find survey workspace: %w", err)
	}
	if len(rows) == 0 {
		return nil, apperr.NotFound("survey", surveyID.String())
	}
	return &rows[0], nil
}

type account struct {
	Email  string
	Locale string
}

func (s *Service) account(ctx context.Context, userID uuid.UUID) (*account, error) {
	var rows []account
	if err := s.db.WithContext(ctx).Raw(`SELECT email, locale FROM users WHERE id = ?`, userID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	if len(rows) == 0 {
		return nil, apperr.Forbidden("User not found")
	}
	return &rows[0], nil
}

// render is forma2's getEmailTemplateHtml: the survey's preview in the styling it resolves to, linking to its public page, with its copy in locale.
func (s *Service) render(ctx context.Context, surveyID uuid.UUID, sc *surveyContext, locale string) (*survey.Survey, string, error) {
	sv, err := s.surveys.Load(ctx, surveyID)
	if err != nil {
		return nil, "", err
	}
	if sv == nil {
		return nil, "", apperr.NotFound("Survey", surveyID.String())
	}
	out, err := s.generator.HTML(sv.Blocks, ResolveStyling(sc.WorkspaceStyling, sv.Styling), s.surveys.PublicDomain()+"/s/"+sv.ID.String(), locale)
	if err != nil {
		return nil, "", err
	}
	return sv, out, nil
}

// HTML is forma2's getEmailHtmlAction: a writer of the survey's workspace gets its email embed, in their locale, its links still marked as previews.
func (s *Service) HTML(ctx context.Context, userID, surveyID uuid.UUID) (string, error) {
	sc, err := s.surveyWorkspace(ctx, surveyID)
	if err != nil {
		return "", err
	}
	if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceWrite, authz.Workspace(sc.WorkspaceID)); err != nil {
		return "", err
	}
	acc, err := s.account(ctx, userID)
	if err != nil {
		return "", err
	}
	_, out, err := s.render(ctx, surveyID, sc, acc.Locale)
	return out, err
}

// SendPreview is forma2's sendEmbedSurveyPreviewEmailAction: a reader of the survey's workspace is mailed its email embed, the preview marks stripped from its links, under the organization's logo. It reports whether the mail was sent; it is not when no mail server is configured.
func (s *Service) SendPreview(ctx context.Context, userID, surveyID uuid.UUID) (bool, error) {
	sc, err := s.surveyWorkspace(ctx, surveyID)
	if err != nil {
		return false, err
	}
	if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceRead, authz.Workspace(sc.WorkspaceID)); err != nil {
		return false, err
	}
	acc, err := s.account(ctx, userID)
	if err != nil {
		return false, err
	}
	sv, out, err := s.render(ctx, surveyID, sc, acc.Locale)
	if err != nil {
		return false, err
	}
	content := mailer.EmbedSurveyPreview{HTML: template.HTML(StripPreview(out)), WorkspaceID: sv.WorkspaceID.String()} //nolint:gosec // markup this package generated around the survey author's own rich text, mailed to a member of the survey's workspace as forma2 did
	if sc.LogoURL != nil && *sc.LogoURL != "" {
		content.LogoURL = responsemail.ResolveStorageURL(s.generator.publicURL, *sc.LogoURL)
	}
	return s.mailer.SendTemplate(ctx, acc.Email, acc.Locale, content)
}
