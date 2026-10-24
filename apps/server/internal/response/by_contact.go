package response

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ByContact is forma2's getResponsesByContactId: every response of a contact of the workspace, newest first, each with its tags and the quotas it is screened into. A contact of another workspace has none. It authorizes nothing: the caller checked that the workspace's contacts may be read.
func ByContact(ctx context.Context, db *gorm.DB, contactID, workspaceID uuid.UUID) ([]WithQuotas, error) {
	var rows []responseRow
	err := db.WithContext(ctx).Raw(responseSelect+`WHERE r.contact_id = ? AND EXISTS (SELECT 1 FROM contacts c WHERE c.id = r.contact_id AND c.workspace_id = ?)
		ORDER BY r.created_at DESC, r.id DESC`, contactID, workspaceID).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list contact responses: %w", err)
	}
	return withQuotas(ctx, db, rows)
}

// ContactDisplay is one display of a contact, forma2's Pick<TDisplay, "id" | "createdAt" | "surveyId">.
type ContactDisplay struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	SurveyID  uuid.UUID `json:"surveyId"`
}

// DisplaysByContact is forma2's getDisplaysByContactId: every display of a contact of the workspace, newest first. It authorizes nothing.
func DisplaysByContact(ctx context.Context, db *gorm.DB, contactID, workspaceID uuid.UUID) ([]ContactDisplay, error) {
	out := []ContactDisplay{}
	err := db.WithContext(ctx).Raw(`SELECT d.id, d.created_at, d.survey_id FROM displays d
		WHERE d.contact_id = ? AND EXISTS (SELECT 1 FROM contacts c WHERE c.id = d.contact_id AND c.workspace_id = ?)
		ORDER BY d.created_at DESC, d.id DESC`, contactID, workspaceID).Scan(&out).Error
	if err != nil {
		return nil, fmt.Errorf("list contact displays: %w", err)
	}
	return out, nil
}
