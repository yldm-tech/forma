package webhook

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

// The writes and reads of forma2's /api/v2/management/webhooks. The management API module validates the input and authorizes the API key with v2's rules before it calls these; they authorize nothing more, except ReplaceManaged, which alone knows whether a replacement retargets the webhook.

// View is the webhook as the v2 API serializes it, without its secret.
func (w *Webhook) View() View { return w.view() }

// ViewWithSecret is the webhook with its secret, the v2 create answer.
func (w *Webhook) ViewWithSecret() View { return w.viewWithSecret() }

// AuditObject is the webhook as an audit event records it; the emitter masks the secret.
func (w *Webhook) AuditObject() map[string]any { return w.auditObject() }

// ByID is the webhook with id, or nil.
func (s *Service) ByID(ctx context.Context, id uuid.UUID) (*Webhook, error) {
	return s.repo.ByID(ctx, id)
}

// CreateManaged is forma2's v2 createWebhook in a workspace the caller may manage: the input's rules, the URL's SSRF check (400 with forma2's message) and a new secret.
func (s *Service) CreateManaged(ctx context.Context, workspaceID uuid.UUID, in Input) (*Webhook, error) {
	return s.createV1(ctx, workspaceID, in)
}

// ReplaceManaged is forma2's v2 updateWebhook by a caller who may write to the webhook's workspace: every field of the webhook replaced, its source included, after the URL's SSRF check. A replacement that changes its URL, its triggers or its surveys also needs workspace.manage, as on the settings page (403 forbidden otherwise), and so does one that changes its source: a third-party source locks the webhook against secret regeneration and against editing on the settings page, which pins the source. A webhook that disappeared meanwhile is 404 webhook.
func (s *Service) ReplaceManaged(ctx context.Context, actor authz.Actor, id uuid.UUID, in Input) (*Webhook, error) {
	current, err := s.repo.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, apperr.NotFound("webhook", id.String())
	}
	f, err := s.fields(ctx, current.WorkspaceID, in, current.SurveyIDs)
	if err != nil {
		return nil, err
	}
	if retargets(current, f) || f.Source != current.Source {
		allowed, err := s.authz.Can(ctx, actor, authz.WorkspaceManage, authz.Workspace(current.WorkspaceID))
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, apperr.Forbidden(managersOnly)
		}
	}
	if _, err := s.guard.Validate(ctx, f.URL); err != nil {
		return nil, err
	}
	if err := s.repo.replace(ctx, id, f); err != nil {
		return nil, err
	}
	return s.repo.ByID(ctx, id)
}

// DeleteManaged deletes a webhook and returns it as it was, nil when there was none.
func (s *Service) DeleteManaged(ctx context.Context, id uuid.UUID) (*Webhook, error) {
	w, err := s.repo.ByID(ctx, id)
	if err != nil || w == nil {
		return nil, err
	}
	deleted, err := s.repo.delete(ctx, id)
	if err != nil || !deleted {
		return nil, err
	}
	return w, nil
}

// ManagedQuery selects the webhooks of a v2 list: those of WorkspaceIDs, created (or updated, by DateField) within Start and End, ordered by OrderBy ("created_at" or "updated_at", newest first unless Ascending), Limit of them after Offset (no bound when zero).
type ManagedQuery struct {
	WorkspaceIDs []uuid.UUID
	DateField    string
	Start        *time.Time
	End          *time.Time
	OrderBy      string
	Ascending    bool
	Limit        int
	Offset       int
}

func (q ManagedQuery) where() (string, []any) {
	where := `WHERE w.workspace_id = ANY(?::uuid[])`
	args := []any{uuidArray(q.WorkspaceIDs)}
	field := "w.created_at"
	if q.DateField == "updated_at" {
		field = "w.updated_at"
	}
	if q.Start != nil {
		where += ` AND ` + field + ` >= ?`
		args = append(args, *q.Start)
	}
	if q.End != nil {
		where += ` AND ` + field + ` <= ?`
		args = append(args, *q.End)
	}
	return where, args
}

// ListManaged reads a page of the webhooks q selects and how many it selects in all.
func (s *Service) ListManaged(ctx context.Context, q ManagedQuery) ([]*Webhook, int64, error) {
	if len(q.WorkspaceIDs) == 0 {
		return []*Webhook{}, 0, nil
	}
	where, args := q.where()
	var total int64
	if err := s.repo.db.WithContext(ctx).Raw(`SELECT count(*) FROM webhooks w `+where, args...).Scan(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count webhooks: %w", err)
	}
	order := "w.created_at"
	if q.OrderBy == "updated_at" {
		order = "w.updated_at"
	}
	direction := " DESC"
	if q.Ascending {
		direction = " ASC"
	}
	query := where + ` ORDER BY ` + order + direction + `, w.id` + direction
	if q.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, q.Limit)
	}
	if q.Offset > 0 {
		query += ` OFFSET ?`
		args = append(args, q.Offset)
	}
	found, err := s.repo.find(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	return found, total, nil
}
