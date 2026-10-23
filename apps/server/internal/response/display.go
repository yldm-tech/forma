package response

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/survey"
)

// Display is forma2's display create answer: `{id, contactId, surveyId}`.
type Display struct {
	ID        uuid.UUID  `json:"id"`
	ContactID *uuid.UUID `json:"contactId"`
	SurveyID  uuid.UUID  `json:"surveyId"`
}

// DisplayInput is a display as the client API validated it (ZDisplayCreateInput, or v2's).
type DisplayInput struct {
	WorkspaceID uuid.UUID
	// SurveyID is the survey as the client sent it.
	SurveyID string
	// UserID is v1's userId: the display is shown to the workspace contact with that userId attribute, created on the spot when there is none.
	UserID *string
	// ContactID is v2's contactId: the display is attached to that contact when it exists in the workspace. No contact is created.
	ContactID *uuid.UUID
}

// CreateDisplay is forma2's client createDisplay, v1 and v2: the survey must be the workspace's (404 Survey naming the id sent) and in progress (403 "Survey is not accepting submissions" with that id), checked before any contact is written; then the contact is resolved (v1 creating it, v2 only attaching an existing one) and the display inserted. Neither displayOption, displayLimit nor recontactDays is enforced here: the SDK filters on the displays and responses the user state lists.
func (s *Service) CreateDisplay(ctx context.Context, in DisplayInput) (*Display, error) {
	surveyID, err := ResolveID(in.SurveyID)
	if err != nil {
		return nil, err
	}
	var statuses []string
	if err := s.db.WithContext(ctx).Raw(`SELECT status FROM surveys WHERE id = ? AND workspace_id = ?`, surveyID, in.WorkspaceID).Scan(&statuses).Error; err != nil {
		return nil, fmt.Errorf("load survey: %w", err)
	}
	if len(statuses) == 0 {
		return nil, NotFound("Survey", &in.SurveyID)
	}
	if statuses[0] != survey.StatusInProgress {
		return nil, forbidden("Survey is not accepting submissions", details("surveyId", in.SurveyID))
	}

	var contactID *uuid.UUID
	switch {
	case in.UserID != nil && *in.UserID != "":
		// Not in a transaction, as in forma2: a contact created here outlives a display insert that fails.
		if contactID, err = contactIDByUserID(ctx, s.db, in.WorkspaceID, *in.UserID); err != nil {
			return nil, err
		}
		if contactID == nil {
			if contactID, err = createContactWithUserID(ctx, s.db, in.WorkspaceID, *in.UserID); err != nil {
				return nil, err
			}
		}
	case in.ContactID != nil:
		exists, err := contactInWorkspace(ctx, s.db, in.WorkspaceID, *in.ContactID)
		if err != nil {
			return nil, err
		}
		if exists {
			contactID = in.ContactID
		}
	}

	d := Display{ContactID: contactID, SurveyID: surveyID}
	if err := s.db.WithContext(ctx).Raw(`INSERT INTO displays (survey_id, contact_id) VALUES (?, ?) RETURNING id`, surveyID, contactID).Row().Scan(&d.ID); err != nil {
		return nil, fmt.Errorf("create display: %w", err)
	}
	return &d, nil
}

// createContactWithUserID creates a contact of the workspace holding userID under the workspace's userId attribute key. A workspace without that key fails, as forma2's connect on the missing key did.
func createContactWithUserID(ctx context.Context, db *gorm.DB, workspaceID uuid.UUID, userID string) (*uuid.UUID, error) {
	var id uuid.UUID
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var keys []uuid.UUID
		if err := tx.Raw(`SELECT id FROM contact_attribute_keys WHERE workspace_id = ? AND key = 'userId'`, workspaceID).Scan(&keys).Error; err != nil {
			return fmt.Errorf("load userId attribute key: %w", err)
		}
		if len(keys) == 0 {
			return fmt.Errorf("workspace %s has no userId attribute key", workspaceID)
		}
		if err := tx.Raw(`INSERT INTO contacts (workspace_id) VALUES (?) RETURNING id`, workspaceID).Row().Scan(&id); err != nil {
			return fmt.Errorf("create contact: %w", err)
		}
		if err := tx.Exec(`INSERT INTO contact_attributes (contact_id, attribute_key_id, value_text) VALUES (?, ?, ?)`, id, keys[0], userID).Error; err != nil {
			return fmt.Errorf("create contact attribute: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// DisplayResponseID is forma2's getResponseIdByDisplayId, what the runtime recovers a response id with after an offline restore: the response that answers the display, nil when none does yet; 404 Display naming the id sent when the display is not one of the workspace's.
func (s *Service) DisplayResponseID(ctx context.Context, workspaceID uuid.UUID, rawDisplayID string) (*uuid.UUID, error) {
	id, err := ResolveID(rawDisplayID)
	if err != nil {
		return nil, err
	}
	var rows []struct{ ResponseID *uuid.UUID }
	err = s.db.WithContext(ctx).Raw(`SELECT r.id AS response_id FROM displays d JOIN surveys s ON s.id = d.survey_id
		LEFT JOIN responses r ON r.display_id = d.id WHERE d.id = ? AND s.workspace_id = ?`, id, workspaceID).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load display: %w", err)
	}
	if len(rows) == 0 {
		return nil, NotFound("Display", &rawDisplayID)
	}
	return rows[0].ResponseID, nil
}

// DisplayRef is one display of a contact, as the SDK's user state lists it.
type DisplayRef struct {
	SurveyID  uuid.UUID `json:"surveyId"`
	CreatedAt time.Time `json:"createdAt"`
}

// Interactions is the recontact and display-limit bookkeeping of one contact, forma2's user state fields: its displays newest first, the surveys it responded to, and when it was last shown a survey. The server keeps no counters; the SDK's filterSurveys decides displayOption, displayLimit and recontactDays from these.
type Interactions struct {
	Displays      []DisplayRef `json:"displays"`
	Responses     []uuid.UUID  `json:"responses"`
	LastDisplayAt *time.Time   `json:"lastDisplayAt"`
}

// ContactInteractions reads a contact's Interactions.
func (s *Service) ContactInteractions(ctx context.Context, contactID uuid.UUID) (*Interactions, error) {
	out := &Interactions{Displays: []DisplayRef{}, Responses: []uuid.UUID{}}
	if err := s.db.WithContext(ctx).Raw(`SELECT survey_id, created_at FROM displays WHERE contact_id = ? ORDER BY created_at DESC, id DESC`, contactID).Scan(&out.Displays).Error; err != nil {
		return nil, fmt.Errorf("load contact displays: %w", err)
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT survey_id FROM responses WHERE contact_id = ? ORDER BY created_at, id`, contactID).Scan(&out.Responses).Error; err != nil {
		return nil, fmt.Errorf("load contact responses: %w", err)
	}
	if len(out.Displays) > 0 {
		out.LastDisplayAt = &out.Displays[0].CreatedAt
	}
	return out, nil
}
