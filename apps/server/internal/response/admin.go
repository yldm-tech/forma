package response

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
)

// DefaultPageSize is forma2's RESPONSES_PER_PAGE, the page a list returns without a limit.
const DefaultPageSize = 10

// Cursor is a position in the list order (newest first, then by id descending), forma2's TResponseCursor: the list continues strictly after it.
type Cursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        uuid.UUID `json:"id"`
}

// ListInput is forma2's getResponses arguments.
type ListInput struct {
	Limit  *int
	Offset *int
	Filter *Criteria
	Cursor *Cursor
}

// readableSurvey authorizes reading a survey's responses (forma2's workspace.read on the survey's workspace) and loads it; an unknown survey is the same 403 as a foreign one.
func (s *Service) readableSurvey(ctx context.Context, userID, surveyID uuid.UUID) (*Survey, error) {
	if err := s.authz.Require(ctx, authz.User(userID), authz.SurveyResponseRead, authz.Survey(surveyID)); err != nil {
		return nil, err
	}
	sv, err := LoadSurvey(ctx, s.db, surveyID)
	if err != nil {
		return nil, err
	}
	if sv == nil {
		return nil, apperr.Forbidden("Not authorized")
	}
	return sv, nil
}

// List is forma2's getResponsesAction: a page of the survey's responses matching the filter, newest first, each with the quotas it is screened into.
func (s *Service) List(ctx context.Context, userID, surveyID uuid.UUID, in ListInput) ([]WithQuotas, error) {
	sv, err := s.readableSurvey(ctx, userID, surveyID)
	if err != nil {
		return nil, err
	}
	filter, err := Where(sv, in.Filter)
	if err != nil {
		return nil, err
	}
	limit := DefaultPageSize
	if in.Limit != nil {
		limit = *in.Limit
	}
	offset := 0
	if in.Offset != nil {
		offset = *in.Offset
	}
	if limit < 0 || offset < 0 {
		return nil, apperr.Validation([]apperr.FieldError{{Field: "limit", Rule: "min", Param: "0"}})
	}
	return Page(ctx, s.db, surveyID, filter, in.Cursor, limit, offset)
}

// Clause is a filter compiled to a SQL condition on the responses row aliased `r`, with its arguments in placeholder order.
type Clause struct {
	SQL  string
	Args []any
}

// Where is forma2's buildWhereClause for one survey as a SQL condition; a malformed condition is a validation error.
func Where(sv *Survey, f *Criteria) (Clause, error) {
	w, err := where(sv, f)
	if err != nil {
		return Clause{}, err
	}
	return Clause{SQL: w.and(), Args: w.args}, nil
}

// Page reads the survey's responses matching filter in the list order (newest first, then by id descending), strictly after cursor when there is one, each with its tags and the quotas it is screened into. It authorizes nothing: callers have checked that the survey's responses may be read.
func Page(ctx context.Context, db *gorm.DB, surveyID uuid.UUID, filter Clause, cursor *Cursor, limit, offset int) ([]WithQuotas, error) {
	w := &clause{sql: []string{"r.survey_id = ?", filter.SQL}, args: append([]any{surveyID}, filter.Args...)}
	if cursor != nil {
		w.add(q("(r.created_at < ", arg{cursor.CreatedAt}, " OR (r.created_at = ", arg{cursor.CreatedAt}, " AND r.id < ", arg{cursor.ID}, "))"))
	}
	var rows []responseRow
	args := append(slices.Clone(w.args), limit, offset)
	if err := db.WithContext(ctx).Raw(responseSelect+"WHERE "+w.and()+" ORDER BY r.created_at DESC, r.id DESC LIMIT ? OFFSET ?", args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list responses: %w", err)
	}
	return withQuotas(ctx, db, rows)
}

// Count is forma2's getResponseCountAction: how many of the survey's responses match the filter.
func (s *Service) Count(ctx context.Context, userID, surveyID uuid.UUID, filter *Criteria) (int64, error) {
	sv, err := s.readableSurvey(ctx, userID, surveyID)
	if err != nil {
		return 0, err
	}
	w, err := where(sv, filter)
	if err != nil {
		return 0, err
	}
	var n int64
	args := append([]any{surveyID}, w.args...)
	if err := s.db.WithContext(ctx).Raw("SELECT count(*) FROM responses r WHERE r.survey_id = ? AND "+w.and(), args...).Scan(&n).Error; err != nil {
		return 0, fmt.Errorf("count responses: %w", err)
	}
	return n, nil
}

func withQuotas(ctx context.Context, db *gorm.DB, rows []responseRow) ([]WithQuotas, error) {
	responses := make([]Response, len(rows))
	for i, r := range rows {
		responses[i] = r.response()
	}
	if err := loadTags(ctx, db, responses); err != nil {
		return nil, err
	}
	out := make([]WithQuotas, len(responses))
	index := map[uuid.UUID]int{}
	ids := make([]uuid.UUID, len(responses))
	for i := range responses {
		out[i] = WithQuotas{Response: responses[i], Quotas: []QuotaRef{}}
		index[responses[i].ID] = i
		ids[i] = responses[i].ID
	}
	if len(ids) == 0 {
		return out, nil
	}
	var links []struct {
		ResponseID uuid.UUID
		ID         uuid.UUID
		Name       string
	}
	if err := db.WithContext(ctx).Raw(`SELECT l.response_id, q.id, q.name FROM response_quota_links l JOIN survey_quotas q ON q.id = l.quota_id
		WHERE l.response_id IN ? AND l.status = ? ORDER BY q.created_at, q.id`, ids, statusScreenedIn).Scan(&links).Error; err != nil {
		return nil, fmt.Errorf("load response quotas: %w", err)
	}
	for _, l := range links {
		i := index[l.ResponseID]
		out[i].Quotas = append(out[i].Quotas, QuotaRef{ID: l.ID, Name: l.Name})
	}
	return out, nil
}

// Get is forma2's getResponseAction: one response with the quotas it is screened into, for a reader of its workspace. An unknown response is the same 403 as a foreign one.
func (s *Service) Get(ctx context.Context, userID, responseID uuid.UUID) (*WithQuotas, error) {
	if err := s.authz.Require(ctx, authz.User(userID), authz.ResponseRead, authz.Response(responseID)); err != nil {
		return nil, err
	}
	var rows []responseRow
	if err := s.db.WithContext(ctx).Raw(responseSelect+"WHERE r.id = ?", responseID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load response: %w", err)
	}
	if len(rows) == 0 {
		return nil, apperr.Forbidden("Not authorized")
	}
	out, err := withQuotas(ctx, s.db, rows)
	if err != nil {
		return nil, err
	}
	return &out[0], nil
}

// responseWorkspace is the workspace and organization a response belongs to; nil when the response does not exist.
func responseWorkspace(ctx context.Context, db *gorm.DB, responseID uuid.UUID) (workspaceID, organizationID *uuid.UUID, err error) {
	var rows []struct {
		WorkspaceID    uuid.UUID
		OrganizationID uuid.UUID
	}
	if err := db.WithContext(ctx).Raw(`SELECT s.workspace_id, w.organization_id FROM responses r JOIN surveys s ON s.id = r.survey_id
		JOIN workspaces w ON w.id = s.workspace_id WHERE r.id = ?`, responseID).Scan(&rows).Error; err != nil {
		return nil, nil, fmt.Errorf("load response workspace: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil, nil
	}
	return &rows[0].WorkspaceID, &rows[0].OrganizationID, nil
}

func (s *Service) record(ctx context.Context, e audit.Event, organizationID *uuid.UUID, err error) {
	e.ActorType, e.Status = audit.ActorUser, audit.StatusSuccess
	if organizationID != nil {
		e.OrganizationID = organizationID.String()
	}
	if err != nil {
		e.Status, e.OldObject, e.NewObject = audit.StatusFailure, nil, nil
	}
	s.audit.Log(ctx, audit.FromRequest(ctx, e))
}

// writableResponse authorizes a change to a response (forma2's workspace.write on its workspace) and applies the state-mutation limit of that workspace.
func (s *Service) writableResponse(ctx context.Context, userID, responseID uuid.UUID) (organizationID *uuid.UUID, err error) {
	if err := s.authz.Require(ctx, authz.User(userID), authz.ResponseWrite, authz.Response(responseID)); err != nil {
		return nil, err
	}
	workspaceID, organizationID, err := responseWorkspace(ctx, s.db, responseID)
	if err != nil {
		return nil, err
	}
	if workspaceID == nil {
		return nil, apperr.Forbidden("Not authorized")
	}
	if err := s.limiter.Apply(ctx, ratelimit.ActionStateMutation, workspaceID.String()); err != nil {
		return organizationID, err
	}
	return organizationID, nil
}

// Delete is forma2's deleteResponseAction: the response goes with its display; with decrementQuotas, every quota it is screened into gives back one place (down to a limit of 1). The storage objects its file upload answers point at are deleted by a job enqueued in the same transaction.
func (s *Service) Delete(ctx context.Context, userID, responseID uuid.UUID, decrementQuotas bool) (deleted *Response, err error) {
	organizationID, err := s.writableResponse(ctx, userID, responseID)
	defer func() {
		var old any
		if deleted != nil {
			old = deleted
		}
		s.record(ctx, audit.Event{Action: audit.ActionDeleted, TargetType: audit.TargetResponse, TargetID: responseID.String(), OldObject: old}, organizationID, err)
	}()
	if err != nil {
		return nil, err
	}
	err = s.jobs.Transaction(ctx, s.db, func(tx *jobs.Tx) error {
		var locked []uuid.UUID
		if err := tx.WithContext(ctx).Raw(`SELECT id FROM responses WHERE id = ? FOR UPDATE`, responseID).Scan(&locked).Error; err != nil {
			return fmt.Errorf("lock response: %w", err)
		}
		r, _, err := loadResponse(ctx, tx.DB, responseID)
		if err != nil {
			return err
		}
		if len(locked) == 0 || r == nil {
			return apperr.NotFound("Response", responseID.String())
		}
		var quotaIDs []uuid.UUID
		if err := tx.WithContext(ctx).Raw(`SELECT quota_id FROM response_quota_links WHERE response_id = ? AND status = ?`, responseID, statusScreenedIn).Scan(&quotaIDs).Error; err != nil {
			return fmt.Errorf("load response quotas: %w", err)
		}
		if err := tx.WithContext(ctx).Exec(`DELETE FROM responses WHERE id = ?`, responseID).Error; err != nil {
			return fmt.Errorf("delete response: %w", err)
		}
		if r.DisplayID != nil {
			if err := tx.WithContext(ctx).Exec(`DELETE FROM displays WHERE id = ?`, *r.DisplayID).Error; err != nil {
				return fmt.Errorf("delete display: %w", err)
			}
		}
		if decrementQuotas && len(quotaIDs) > 0 {
			if err := tx.WithContext(ctx).Exec(`UPDATE survey_quotas SET "limit" = "limit" - 1, updated_at = ? WHERE id IN ? AND "limit" > 1`, time.Now().UTC(), quotaIDs).Error; err != nil {
				return fmt.Errorf("reduce quota limits: %w", err)
			}
		}
		if err := s.enqueueFileCleanup(ctx, tx, r); err != nil {
			return err
		}
		deleted = r
		return nil
	})
	if err != nil {
		return nil, err
	}
	return deleted, nil
}

// enqueueFileCleanup is forma2's findAndDeleteUploadedFilesInResponse: the storage URLs held under the survey's file upload elements, which are always arrays of strings, go to a DeleteFilesArgs job.
func (s *Service) enqueueFileCleanup(ctx context.Context, tx *jobs.Tx, r *Response) error {
	sv, err := LoadSurvey(ctx, tx.DB, r.SurveyID)
	if err != nil || sv == nil {
		return err
	}
	urls, err := FileURLs(r.Data, sv.FileUploadElementIDs())
	if err != nil || len(urls) == 0 {
		return err
	}
	return tx.Enqueue(ctx, DeleteFilesArgs{WorkspaceID: sv.WorkspaceID, FileURLs: urls}, nil)
}

// FileURLs is forma2's collectResponseFileUrls: the strings of every array answer under a file upload element. Anything else under such a key is skipped rather than cast, so malformed data cannot produce a delete target.
func FileURLs(data json.RawMessage, fileUploadIDs map[string]bool) ([]string, error) {
	if len(fileUploadIDs) == 0 {
		return nil, nil
	}
	answers, err := decodeObject(data)
	if err != nil {
		return nil, err
	}
	var urls []string
	for id := range fileUploadIDs {
		list, ok := answers[id].([]any)
		if !ok {
			continue
		}
		for _, v := range list {
			if s, ok := v.(string); ok {
				urls = append(urls, s)
			}
		}
	}
	return urls, nil
}

// TagChange is the audit object of a tag added to or removed from a response, forma2's TTagsOnResponses.
type TagChange struct {
	ResponseID uuid.UUID `json:"responseId"`
	TagID      uuid.UUID `json:"tagId"`
}

// tagTarget checks a tag change: the response and the tag must exist (404 "Workspace not found" otherwise, as forma2 answered), the caller may write the response's workspace, and the tag must be that workspace's.
func (s *Service) tagTarget(ctx context.Context, userID, responseID, tagID uuid.UUID) (*uuid.UUID, error) {
	workspaceID, _, err := responseWorkspace(ctx, s.db, responseID)
	if err != nil {
		return nil, err
	}
	var tagWorkspaces []uuid.UUID
	if err := s.db.WithContext(ctx).Raw(`SELECT workspace_id FROM tags WHERE id = ?`, tagID).Scan(&tagWorkspaces).Error; err != nil {
		return nil, fmt.Errorf("load tag: %w", err)
	}
	if workspaceID == nil || len(tagWorkspaces) == 0 {
		return nil, apperr.NotFound("Workspace", "")
	}
	organizationID, err := s.writableResponse(ctx, userID, responseID)
	if err != nil {
		return organizationID, err
	}
	if tagWorkspaces[0] != *workspaceID {
		return organizationID, apperr.BadRequest("Response and tag are not in the same workspace")
	}
	return organizationID, nil
}

// AddTag is forma2's createTagToResponseAction. Adding a tag the response already carries succeeds.
func (s *Service) AddTag(ctx context.Context, userID, responseID, tagID uuid.UUID) (change *TagChange, err error) {
	organizationID, err := s.tagTarget(ctx, userID, responseID, tagID)
	defer func() {
		var created any
		if change != nil {
			created = change
		}
		s.record(ctx, audit.Event{Action: audit.ActionAddedToResponse, TargetType: audit.TargetTag, TargetID: tagID.String(), NewObject: created}, organizationID, err)
	}()
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Exec(`INSERT INTO response_tags (response_id, tag_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, responseID, tagID).Error; err != nil {
		return nil, fmt.Errorf("tag response: %w", err)
	}
	return &TagChange{ResponseID: responseID, TagID: tagID}, nil
}

// RemoveTag is forma2's deleteTagOnResponseAction. A tag the response does not carry is 404.
func (s *Service) RemoveTag(ctx context.Context, userID, responseID, tagID uuid.UUID) (change *TagChange, err error) {
	organizationID, err := s.tagTarget(ctx, userID, responseID, tagID)
	defer func() {
		var removed any
		if change != nil {
			removed = change
		}
		s.record(ctx, audit.Event{Action: audit.ActionRemovedFromResponse, TargetType: audit.TargetTag, TargetID: tagID.String(), OldObject: removed}, organizationID, err)
	}()
	if err != nil {
		return nil, err
	}
	res := s.db.WithContext(ctx).Exec(`DELETE FROM response_tags WHERE response_id = ? AND tag_id = ?`, responseID, tagID)
	if res.Error != nil {
		return nil, fmt.Errorf("untag response: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, apperr.NotFound("Tag", tagID.String())
	}
	return &TagChange{ResponseID: responseID, TagID: tagID}, nil
}
