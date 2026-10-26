package response

import (
	"context"
	"fmt"
	"strconv"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
)

// MaxBulkTagResponseIDs bounds an explicit selection in one bulk tag change. The response table selects loaded rows and sends them in chunks of this size; a whole filtered result goes as the filter itself, without ids.
const MaxBulkTagResponseIDs = 1000

// BulkTagResult is the outcome of a bulk tag change: how many of the survey's responses matched the filter at the time of the change, and how many of them gained (or lost) the tag. A matching response that already carried the tag (or never did) is matched but not changed.
type BulkTagResult struct {
	TagID   uuid.UUID `json:"tagId"`
	Matched int64     `json:"matched"`
	Changed int64     `json:"changed"`
}

// BulkTagChange is the audit object of a bulk tag change. It names the survey and how many responses changed, not the filter, which can hold answer text.
type BulkTagChange struct {
	SurveyID      uuid.UUID `json:"surveyId"`
	TagID         uuid.UUID `json:"tagId"`
	ResponseCount int64     `json:"responseCount"`
}

// writableSurvey authorizes a change to a survey's responses (response.write is survey.write, workspace write) and loads the survey with its organization; an unknown survey is the same 403 as a foreign one.
func (s *Service) writableSurvey(ctx context.Context, userID, surveyID uuid.UUID) (*Survey, *uuid.UUID, error) {
	if err := s.authz.Require(ctx, authz.User(userID), authz.SurveyWrite, authz.Survey(surveyID)); err != nil {
		return nil, nil, err
	}
	sv, err := LoadSurvey(ctx, s.db, surveyID)
	if err != nil {
		return nil, nil, err
	}
	if sv == nil {
		return nil, nil, apperr.Forbidden("Not authorized")
	}
	var orgs []uuid.UUID
	if err := s.db.WithContext(ctx).Raw(`SELECT organization_id FROM workspaces WHERE id = ?`, sv.WorkspaceID).Scan(&orgs).Error; err != nil {
		return nil, nil, fmt.Errorf("load survey organization: %w", err)
	}
	if len(orgs) == 0 {
		return nil, nil, apperr.Forbidden("Not authorized")
	}
	return sv, &orgs[0], nil
}

// BulkTag adds the tag to, or with remove takes it off, every response of the survey that matches the filter: an explicit selection is the filter's responseIds, "all matching" is the response view's current filter. It is one statement, so the set is the one matching when it runs, and responses already in the wanted state are left alone. The tag must exist (404) and belong to the survey's workspace (400, as the single tag change answers).
func (s *Service) BulkTag(ctx context.Context, userID, surveyID, tagID uuid.UUID, filter *Criteria, remove bool) (result *BulkTagResult, err error) {
	sv, organizationID, err := s.writableSurvey(ctx, userID, surveyID)
	defer func() {
		e := audit.Event{Action: audit.ActionAddedToResponse, TargetType: audit.TargetTag, TargetID: tagID.String()}
		var change any
		if result != nil {
			change = BulkTagChange{SurveyID: surveyID, TagID: tagID, ResponseCount: result.Changed}
		}
		if remove {
			e.Action, e.OldObject = audit.ActionRemovedFromResponse, change
		} else {
			e.NewObject = change
		}
		s.record(ctx, e, organizationID, err)
	}()
	if err != nil {
		return nil, err
	}
	if filter != nil && len(filter.ResponseIDs) > MaxBulkTagResponseIDs {
		return nil, apperr.Validation([]apperr.FieldError{{Field: "filterCriteria.responseIds", Rule: "max", Param: strconv.Itoa(MaxBulkTagResponseIDs)}})
	}
	var tagWorkspaces []uuid.UUID
	if err := s.db.WithContext(ctx).Raw(`SELECT workspace_id FROM tags WHERE id = ?`, tagID).Scan(&tagWorkspaces).Error; err != nil {
		return nil, fmt.Errorf("load tag: %w", err)
	}
	if len(tagWorkspaces) == 0 {
		return nil, apperr.NotFound("Tag", tagID.String())
	}
	if tagWorkspaces[0] != sv.WorkspaceID {
		return nil, apperr.BadRequest("Response and tag are not in the same workspace")
	}
	w, err := where(ctx, s.segments, sv, filter)
	if err != nil {
		return nil, err
	}
	if err := s.limiter.Apply(ctx, ratelimit.ActionStateMutation, sv.WorkspaceID.String()); err != nil {
		return nil, err
	}
	step := `changed AS (INSERT INTO response_tags (response_id, tag_id) SELECT id, ? FROM matched ON CONFLICT DO NOTHING RETURNING 1)`
	if remove {
		step = `changed AS (DELETE FROM response_tags d USING matched m WHERE d.response_id = m.id AND d.tag_id = ? RETURNING 1)`
	}
	args := append([]any{surveyID}, w.args...)
	args = append(args, tagID)
	var counts struct {
		Matched int64
		Changed int64
	}
	if err := s.db.WithContext(ctx).Raw(`WITH matched AS (SELECT r.id FROM responses r WHERE r.survey_id = ? AND `+w.and()+`), `+step+`
		SELECT (SELECT count(*) FROM matched) AS matched, (SELECT count(*) FROM changed) AS changed`, args...).Scan(&counts).Error; err != nil {
		return nil, fmt.Errorf("bulk tag responses: %w", err)
	}
	return &BulkTagResult{TagID: tagID, Matched: counts.Matched, Changed: counts.Changed}, nil
}
