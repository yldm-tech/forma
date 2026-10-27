package ai

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Repository reads what the AI service decides on: an organization's AI switches, and the organization a workspace or survey belongs to.
type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// organizationSwitches are an organization's two AI switches: AI smart tools, and whether AI may read the organization's responses.
type organizationSwitches struct {
	IsAiSmartToolsEnabled      bool
	IsAiResponseReadingEnabled bool
}

// switches returns nil when the organization does not exist.
func (r *Repository) switches(ctx context.Context, organizationID uuid.UUID) (*organizationSwitches, error) {
	var rows []organizationSwitches
	if err := r.db.WithContext(ctx).Raw(`SELECT is_ai_smart_tools_enabled, is_ai_response_reading_enabled FROM organizations WHERE id = ?`, organizationID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("read organization AI switches: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// workspaceOrganization is the organization of a workspace; uuid.Nil when the workspace does not exist.
func (r *Repository) workspaceOrganization(ctx context.Context, workspaceID uuid.UUID) (uuid.UUID, error) {
	var rows []uuid.UUID
	if err := r.db.WithContext(ctx).Raw(`SELECT organization_id FROM workspaces WHERE id = ?`, workspaceID).Scan(&rows).Error; err != nil {
		return uuid.Nil, fmt.Errorf("read workspace organization: %w", err)
	}
	if len(rows) == 0 {
		return uuid.Nil, nil
	}
	return rows[0], nil
}

type surveyOwner struct {
	WorkspaceID    uuid.UUID
	OrganizationID uuid.UUID
}

// surveyOwner is the workspace and organization of a survey; nil when the survey does not exist.
func (r *Repository) surveyOwner(ctx context.Context, surveyID uuid.UUID) (*surveyOwner, error) {
	var rows []surveyOwner
	if err := r.db.WithContext(ctx).Raw(`SELECT s.workspace_id, w.organization_id FROM surveys s JOIN workspaces w ON w.id = s.workspace_id WHERE s.id = ?`, surveyID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("read survey owner: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}
