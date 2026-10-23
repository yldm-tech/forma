package org

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
)

// PrettyURLSurvey is a survey with a pretty URL, forma2's TSurveyWithSlug.
type PrettyURLSurvey struct {
	ID        uuid.UUID          `json:"id"`
	Name      string             `json:"name"`
	Slug      string             `json:"slug"`
	Status    string             `json:"status"`
	CreatedAt time.Time          `json:"createdAt"`
	Workspace PrettyURLWorkspace `json:"workspace"`
}

// PrettyURLWorkspace is the workspace a survey with a pretty URL belongs to.
type PrettyURLWorkspace struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	OrganizationID uuid.UUID `json:"organizationId"`
}

// PrettyURLs is the survey list of forma2's organization domain settings page (getSurveysWithSlugsByOrganizationId): every survey of the organization's workspaces that has a pretty URL. The page exists only self-hosted, so on Forma Cloud the organization reads as not found, and it is for members other than the billing role (organization.read_access, forma2's redirectBillingRoleFromRestrictedOrgSettings); anyone else gets a not-found.
func (s *Service) PrettyURLs(ctx context.Context, userID, organizationID uuid.UUID) ([]PrettyURLSurvey, error) {
	if s.cfg.IsFormaCloud {
		return nil, errOrganizationNotFound()
	}
	if _, err := s.memberWith(ctx, userID, organizationID, authz.OrganizationReadAccess); err != nil {
		return nil, err
	}
	return s.repo.prettyURLs(ctx, organizationID)
}

func (r *Repository) prettyURLs(ctx context.Context, organizationID uuid.UUID) ([]PrettyURLSurvey, error) {
	var rows []struct {
		ID            uuid.UUID
		Name          string
		Slug          string
		Status        string
		CreatedAt     time.Time
		WorkspaceID   uuid.UUID
		WorkspaceName string
	}
	if err := r.db.WithContext(ctx).Raw(
		`SELECT s.id, s.name, s.slug, s.status, s.created_at, w.id AS workspace_id, w.name AS workspace_name
		 FROM surveys s JOIN workspaces w ON w.id = s.workspace_id
		 WHERE w.organization_id = ? AND s.slug IS NOT NULL ORDER BY s.created_at, s.id`, organizationID,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list pretty urls: %w", err)
	}
	out := make([]PrettyURLSurvey, len(rows))
	for i, row := range rows {
		out[i] = PrettyURLSurvey{
			ID: row.ID, Name: row.Name, Slug: row.Slug, Status: row.Status, CreatedAt: row.CreatedAt,
			Workspace: PrettyURLWorkspace{ID: row.WorkspaceID, Name: row.WorkspaceName, OrganizationID: organizationID},
		}
	}
	return out, nil
}

type prettyURLsResponse struct {
	Surveys []PrettyURLSurvey `json:"surveys"`
}

func (h *Handler) prettyURLs(c *gin.Context) {
	id, ok := organizationID(c)
	if !ok {
		return
	}
	surveys, err := h.svc.PrettyURLs(c.Request.Context(), userID(c), id)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, prettyURLsResponse{Surveys: surveys})
}
