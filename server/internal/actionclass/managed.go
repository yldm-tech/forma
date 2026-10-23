package actionclass

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// The reads and writes of forma2's /api/v1/management/action-classes. The management API module validates the input and authorizes the API key with v1's rules before it calls these; they authorize nothing.

// ListManaged is the v1 API's getActionClasses: every action class of the workspaces, oldest first.
func (s *Service) ListManaged(ctx context.Context, workspaceIDs []uuid.UUID) ([]*ActionClass, error) {
	ids := make([]string, len(workspaceIDs))
	for i, id := range workspaceIDs {
		ids[i] = id.String()
	}
	var rows []row
	if err := s.repo.db.WithContext(ctx).Raw(`SELECT `+columns+` FROM action_classes WHERE workspace_id = ANY(?::uuid[]) ORDER BY created_at, id`, "{"+strings.Join(ids, ",")+"}").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list action classes: %w", err)
	}
	out := make([]*ActionClass, len(rows))
	for i, r := range rows {
		out[i] = r.actionClass()
	}
	return out, nil
}

// ByID is the action class with id, nil when there is none.
func (s *Service) ByID(ctx context.Context, id uuid.UUID) (*ActionClass, error) {
	return s.repo.byID(ctx, id)
}

// CreateManaged is forma2's createActionClass: a name or code key the workspace already uses is a 409.
func (s *Service) CreateManaged(ctx context.Context, workspaceID uuid.UUID, in Input) (*ActionClass, error) {
	return s.repo.create(ctx, workspaceID, in)
}

// UpdateManaged is forma2's updateActionClass: the action class stays in its own workspace whatever the input names.
func (s *Service) UpdateManaged(ctx context.Context, current *ActionClass, in Input) (*ActionClass, error) {
	return s.repo.update(ctx, current.ID, current.WorkspaceID, in)
}

// DeleteManaged is forma2's deleteActionClass; the surveys it triggered lose the trigger. It reports whether there was one to delete.
func (s *Service) DeleteManaged(ctx context.Context, current *ActionClass) (bool, error) {
	return s.repo.delete(ctx, current.ID, current.WorkspaceID)
}

// SurveyTrigger is one survey an action class triggers, as forma2's v1 update answer lists it.
type SurveyTrigger struct {
	SurveyID uuid.UUID `json:"surveyId"`
}

// SurveyTriggers lists the surveys an action class triggers.
func (s *Service) SurveyTriggers(ctx context.Context, id uuid.UUID) ([]SurveyTrigger, error) {
	out := []SurveyTrigger{}
	if err := s.repo.db.WithContext(ctx).Raw(`SELECT survey_id FROM survey_triggers WHERE action_class_id = ? ORDER BY created_at, survey_id`, id).Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("list action class triggers: %w", err)
	}
	return out, nil
}
