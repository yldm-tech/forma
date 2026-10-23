package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
)

// Kinds of notification setting, named as forma2's notification page and the unsubscribe links in its emails named them (the ?type= of /account/settings/notifications).
const (
	notificationAlert                 = "alert"
	notificationOrganizationAutoAlert = "unsubscribedOrganizationIds"
)

// NotificationOrganization is one organization on the notifications page: whether the user is subscribed to new surveys' alerts by default, and every survey they can see in it with its per-response alert.
type NotificationOrganization struct {
	ID            uuid.UUID            `json:"id"`
	Name          string               `json:"name"`
	AutoSubscribe bool                 `json:"autoSubscribe"`
	Surveys       []NotificationSurvey `json:"surveys"`
}

// NotificationSurvey is a survey row of the notifications page.
type NotificationSurvey struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	WorkspaceID   uuid.UUID `json:"workspaceId"`
	WorkspaceName string    `json:"workspaceName"`
	Alert         bool      `json:"alert"`
}

// notificationSettings is users.notification_settings, forma2's ZUserNotificationSettings: per-survey "every response" alerts, and the organizations whose new surveys the user is not subscribed to. Other keys a document carries are kept on write.
type notificationSettings struct {
	Alert        map[string]bool
	Unsubscribed []string
	rest         map[string]json.RawMessage
}

func parseNotificationSettings(raw []byte) (*notificationSettings, error) {
	doc := map[string]json.RawMessage{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("decode notification settings: %w", err)
		}
	}
	s := &notificationSettings{Alert: map[string]bool{}, Unsubscribed: []string{}, rest: doc}
	if v, ok := doc[notificationAlert]; ok && string(v) != "null" {
		if err := json.Unmarshal(v, &s.Alert); err != nil {
			return nil, fmt.Errorf("decode notification alerts: %w", err)
		}
	}
	if v, ok := doc[notificationOrganizationAutoAlert]; ok && string(v) != "null" {
		if err := json.Unmarshal(v, &s.Unsubscribed); err != nil {
			return nil, fmt.Errorf("decode unsubscribed organizations: %w", err)
		}
	}
	return s, nil
}

func (s *notificationSettings) marshal() ([]byte, error) {
	doc := make(map[string]json.RawMessage, len(s.rest)+2)
	for k, v := range s.rest {
		doc[k] = v
	}
	alert, err := json.Marshal(s.Alert)
	if err != nil {
		return nil, err
	}
	unsubscribed, err := json.Marshal(s.Unsubscribed)
	if err != nil {
		return nil, err
	}
	doc[notificationAlert], doc[notificationOrganizationAutoAlert] = alert, unsubscribed
	return json.Marshal(doc)
}

// notifications is forma2's notifications page: the organizations the user belongs to in a role other than billing, each with the surveys of the workspaces the user can open there (all of them for an owner or manager, those granted to one of their teams otherwise). A member who can open no workspace in an organization does not see it, as forma2's query left it out.
func (s *Service) notifications(ctx context.Context, actor httpapi.Actor) ([]NotificationOrganization, error) {
	rows, err := s.repo.notificationRows(ctx, actor.UserID)
	if err != nil {
		return nil, err
	}
	raw, err := s.repo.notificationSettings(ctx, actor.UserID)
	if err != nil {
		return nil, err
	}
	settings, err := parseNotificationSettings(raw)
	if err != nil {
		return nil, err
	}
	out := []NotificationOrganization{}
	for _, r := range rows {
		if len(out) == 0 || out[len(out)-1].ID != r.OrganizationID {
			if r.WorkspaceID == nil && r.Role == "member" {
				continue
			}
			out = append(out, NotificationOrganization{
				ID: r.OrganizationID, Name: r.OrganizationName, Surveys: []NotificationSurvey{},
				AutoSubscribe: !slices.Contains(settings.Unsubscribed, r.OrganizationID.String()),
			})
		}
		if r.SurveyID == nil {
			continue
		}
		org := &out[len(out)-1]
		org.Surveys = append(org.Surveys, NotificationSurvey{
			ID: *r.SurveyID, Name: *r.SurveyName, WorkspaceID: *r.WorkspaceID, WorkspaceName: *r.WorkspaceName,
			Alert: settings.Alert[r.SurveyID.String()],
		})
	}
	return out, nil
}

// setNotification switches one setting: a survey's per-response alert, or whether new surveys of an organization alert the user. Unlike forma2, which stored whatever document the browser sent, the survey or organization must be one the page shows this user.
func (s *Service) setNotification(ctx context.Context, actor httpapi.Actor, kind string, id uuid.UUID, enabled bool) error {
	err := s.changeNotification(ctx, actor, kind, id, enabled)
	if err != nil {
		s.logFailure(ctx, audit.ActionUpdated, actor.UserID.String())
	}
	return err
}

func (s *Service) changeNotification(ctx context.Context, actor httpapi.Actor, kind string, id uuid.UUID, enabled bool) error {
	rows, err := s.repo.notificationRows(ctx, actor.UserID)
	if err != nil {
		return err
	}
	visible := slices.ContainsFunc(rows, func(r notificationRow) bool {
		if kind == notificationAlert {
			return r.SurveyID != nil && *r.SurveyID == id
		}
		return r.OrganizationID == id && (r.WorkspaceID != nil || r.Role != "member")
	})
	if !visible {
		if kind == notificationAlert {
			return apperr.NotFound("Survey", "")
		}
		return apperr.NotFound("Organization", "")
	}
	var oldDoc, newDoc []byte
	err = s.repo.transaction(ctx, func(tx *Repository) error {
		if oldDoc, err = tx.lockNotificationSettings(ctx, actor.UserID); err != nil {
			return err
		}
		settings, err := parseNotificationSettings(oldDoc)
		if err != nil {
			return err
		}
		key := id.String()
		if kind == notificationAlert {
			settings.Alert[key] = enabled
		} else {
			settings.Unsubscribed = slices.DeleteFunc(settings.Unsubscribed, func(v string) bool { return v == key })
			if !enabled {
				settings.Unsubscribed = append(settings.Unsubscribed, key)
			}
		}
		if newDoc, err = settings.marshal(); err != nil {
			return err
		}
		return tx.writeNotificationSettings(ctx, actor.UserID, newDoc)
	})
	if err != nil {
		return err
	}
	s.logEvent(ctx, audit.Event{
		Action: audit.ActionUpdated, TargetType: audit.TargetUser, TargetID: actor.UserID.String(), Status: audit.StatusSuccess,
		OldObject: map[string]any{"notificationSettings": json.RawMessage(oldDoc)}, NewObject: map[string]any{"notificationSettings": json.RawMessage(newDoc)},
	})
	return nil
}

// notificationRow is one survey the user can see, or an organization or workspace with none; the nullable columns are empty for those.
type notificationRow struct {
	OrganizationID   uuid.UUID
	OrganizationName string
	Role             string
	WorkspaceID      *uuid.UUID
	WorkspaceName    *string
	SurveyID         *uuid.UUID
	SurveyName       *string
}

// notificationRows lists the user's non-billing organizations, the workspaces they can open in each (every workspace for an owner or manager, else those granted to one of their teams), and those workspaces' surveys, in organization, workspace and survey creation order.
func (r *Repository) notificationRows(ctx context.Context, userID uuid.UUID) ([]notificationRow, error) {
	var rows []notificationRow
	if err := r.db.WithContext(ctx).Raw(`
		SELECT o.id AS organization_id, o.name AS organization_name, m.role,
		       w.id AS workspace_id, w.name AS workspace_name, s.id AS survey_id, s.name AS survey_name
		FROM memberships m
		JOIN organizations o ON o.id = m.organization_id
		LEFT JOIN workspaces w ON w.organization_id = o.id AND (
		     m.role IN ('owner', 'manager')
		     OR EXISTS (SELECT 1 FROM workspace_teams wt JOIN team_members tm ON tm.team_id = wt.team_id
		                WHERE wt.workspace_id = w.id AND tm.user_id = m.user_id))
		LEFT JOIN surveys s ON s.workspace_id = w.id
		WHERE m.user_id = ? AND m.role <> 'billing'
		ORDER BY o.created_at, o.id, w.created_at, w.id, s.created_at, s.id`, userID,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list notification surveys: %w", err)
	}
	return rows, nil
}

func (r *Repository) notificationSettings(ctx context.Context, userID uuid.UUID) ([]byte, error) {
	var raw []byte
	if err := r.db.WithContext(ctx).Raw(`SELECT notification_settings FROM users WHERE id = ?`, userID).Row().Scan(&raw); err != nil {
		return nil, fmt.Errorf("read notification settings: %w", err)
	}
	return raw, nil
}

func (r *Repository) lockNotificationSettings(ctx context.Context, userID uuid.UUID) ([]byte, error) {
	var raw []byte
	if err := r.db.WithContext(ctx).Raw(`SELECT notification_settings FROM users WHERE id = ? FOR NO KEY UPDATE`, userID).Row().Scan(&raw); err != nil {
		return nil, fmt.Errorf("lock notification settings: %w", err)
	}
	return raw, nil
}

func (r *Repository) writeNotificationSettings(ctx context.Context, userID uuid.UUID, doc []byte) error {
	if err := r.db.WithContext(ctx).Exec(
		`UPDATE users SET notification_settings = ?::jsonb, updated_at = now() WHERE id = ?`, string(doc), userID,
	).Error; err != nil {
		return fmt.Errorf("write notification settings: %w", err)
	}
	return nil
}
