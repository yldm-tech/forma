package contact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
)

// Stable codes of the segment failures.
const (
	CodeSegmentTitleTaken = "segment_title_taken"
	CodeWorkspaceMismatch = "workspace_mismatch"
	CodeRecursiveSegment  = "recursive_segment_filter"
	CodeSurveyNotFound    = "survey_not_in_workspace"
)

// surveyStatusInProgress is the status of a survey that is collecting responses; a segment lists those as active.
const surveyStatusInProgress = "inProgress"

// SurveyRef names a survey linked to a segment.
type SurveyRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// Segment is forma2's TSegmentWithSurveyRefs: the segment, the ids of the surveys that target it, and those surveys split by whether they are in progress.
type Segment struct {
	ID              uuid.UUID       `json:"id"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
	Title           string          `json:"title"`
	Description     *string         `json:"description"`
	IsPrivate       bool            `json:"isPrivate"`
	Filters         json.RawMessage `json:"filters"`
	WorkspaceID     uuid.UUID       `json:"workspaceId"`
	Surveys         []uuid.UUID     `json:"surveys"`
	ActiveSurveys   []SurveyRef     `json:"activeSurveys"`
	InactiveSurveys []SurveyRef     `json:"inactiveSurveys"`
}

// SegmentInput is forma2's ZSegmentCreateInput. SurveyID, when set, is a survey of the same workspace the segment is linked to.
type SegmentInput struct {
	Title       string
	Description *string
	IsPrivate   bool
	Filters     json.RawMessage
	SurveyID    *uuid.UUID
}

// SegmentUpdate is forma2's ZSegmentUpdateInput: every field is optional. Description may be set to null, so DescriptionSet says whether it was given. Surveys are linked to the segment; none is unlinked.
type SegmentUpdate struct {
	Title          *string
	Description    *string
	DescriptionSet bool
	IsPrivate      *bool
	Filters        json.RawMessage
	Surveys        []uuid.UUID
	SurveysSet     bool
}

// SurveyFilterRef is a survey offered by the segment editor's survey-interaction picker.
type SurveyFilterRef struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Status string    `json:"status"`
}

// MaxSurveyFilterRefs is forma2's SURVEY_FILTER_REF_LIMIT.
const MaxSurveyFilterRefs = 1000

// requiredFilters parses a create or update tree, which must hold at least one filter.
func (s *Service) requiredFilters(raw json.RawMessage) (Filters, error) {
	tree, err := ParseFilters(s.schemas, raw)
	if err != nil {
		return nil, err
	}
	if len(tree) == 0 {
		return nil, invalidFilters(atLeastOneFilterMessage)
	}
	return tree, nil
}

// Segments is forma2's getSegments: every segment of the workspace, private ones included; the segments page shows the public ones.
func (s *Service) Segments(ctx context.Context, userID, workspaceID uuid.UUID) ([]Segment, error) {
	if err := s.read(ctx, userID, workspaceID); err != nil {
		return nil, err
	}
	return s.repo.segments(ctx, workspaceID, nil)
}

// Segment reads one segment for a reader of its workspace.
func (s *Service) Segment(ctx context.Context, userID, segmentID uuid.UUID) (*Segment, error) {
	seg, err := s.repo.segment(ctx, segmentID)
	if err != nil {
		return nil, err
	}
	if seg == nil {
		return nil, apperr.NotFound("segment", segmentID.String())
	}
	if err := s.read(ctx, userID, seg.WorkspaceID); err != nil {
		return nil, err
	}
	return seg, nil
}

// SurveyFilterRefs is forma2's getSurveysForSegmentFilterAction: the workspace's surveys, most recently updated first, at most MaxSurveyFilterRefs.
func (s *Service) SurveyFilterRefs(ctx context.Context, userID, workspaceID uuid.UUID) ([]SurveyFilterRef, error) {
	if err := s.read(ctx, userID, workspaceID); err != nil {
		return nil, err
	}
	return s.repo.surveyRefs(ctx, workspaceID)
}

// CreateSegment is forma2's createSegmentAction. A private segment is upserted by title, which is how the survey editor saves its own targeting segment repeatedly; a public one is created and its title must be new in the workspace.
func (s *Service) CreateSegment(ctx context.Context, userID, workspaceID uuid.UUID, in SegmentInput) (seg *Segment, err error) {
	ev := audit.Event{Action: audit.ActionCreated, TargetType: audit.TargetSegment}
	defer func() { s.record(ctx, ev, err) }()

	tree, err := s.requiredFilters(in.Filters)
	if err != nil {
		return nil, err
	}
	org, err := s.organizationOf(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	ev.OrganizationID = org.String()
	if err := s.write(ctx, userID, workspaceID); err != nil {
		return nil, err
	}
	if in.SurveyID != nil {
		surveyWorkspace, err := s.surveyWorkspace(ctx, *in.SurveyID)
		if err != nil {
			return nil, err
		}
		if surveyWorkspace != workspaceID {
			return nil, apperr.InvalidInput(CodeWorkspaceMismatch, "Survey and segment are not in the same workspace")
		}
	}
	if err := s.assertInteractionSurveys(ctx, tree, workspaceID); err != nil {
		return nil, err
	}
	var id uuid.UUID
	err = s.repo.transaction(ctx, func(tx *Repository) error {
		var err error
		if in.IsPrivate {
			id, err = tx.upsertPrivateSegment(ctx, workspaceID, in.Title, in.Description, in.Filters)
		} else {
			id, err = tx.insertSegment(ctx, workspaceID, in.Title, in.Description, false, in.Filters)
		}
		if err != nil {
			return err
		}
		if in.SurveyID != nil {
			return tx.linkSurveys(ctx, id, workspaceID, []uuid.UUID{*in.SurveyID})
		}
		return nil
	})
	if errors.Is(err, errSegmentTitleTaken) {
		return nil, apperr.InvalidInput(CodeSegmentTitleTaken, "Segment with this title already exists")
	}
	if err != nil {
		return nil, err
	}
	seg, err = s.repo.segment(ctx, id)
	if err != nil {
		return nil, err
	}
	ev.TargetID, ev.NewObject = id.String(), seg
	return seg, nil
}

// segmentForWrite resolves a segment to its workspace and authorizes a change there.
func (s *Service) segmentForWrite(ctx context.Context, userID, segmentID uuid.UUID, ev *audit.Event) (*Segment, error) {
	seg, err := s.repo.segment(ctx, segmentID)
	if err != nil {
		return nil, err
	}
	if seg == nil {
		return nil, apperr.NotFound("segment", segmentID.String())
	}
	org, err := s.organizationOf(ctx, seg.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ev.OrganizationID = org.String()
	if err := s.write(ctx, userID, seg.WorkspaceID); err != nil {
		return nil, err
	}
	return seg, nil
}

// UpdateSegment is forma2's updateSegmentAction. Linked surveys must all be the segment workspace's; a tree may not reach the segment itself through the segments it references.
func (s *Service) UpdateSegment(ctx context.Context, userID, segmentID uuid.UUID, in SegmentUpdate) (seg *Segment, err error) {
	ev := audit.Event{Action: audit.ActionUpdated, TargetType: audit.TargetSegment, TargetID: segmentID.String()}
	defer func() { s.record(ctx, ev, err) }()

	var tree Filters
	if in.Filters != nil {
		if tree, err = s.requiredFilters(in.Filters); err != nil {
			return nil, err
		}
	}
	old, err := s.segmentForWrite(ctx, userID, segmentID, &ev)
	if err != nil {
		return nil, err
	}
	if in.SurveysSet && len(in.Surveys) > 0 {
		workspaces, err := s.repo.surveyWorkspaces(ctx, in.Surveys)
		if err != nil {
			return nil, err
		}
		for _, id := range in.Surveys {
			if workspaces[id] != old.WorkspaceID {
				return nil, apperr.InvalidInput(CodeWorkspaceMismatch, "Survey and segment are not in the same workspace")
			}
		}
	}
	if in.Filters != nil {
		if err := s.checkRecursion(ctx, tree, segmentID.String(), map[string]bool{}); err != nil {
			return nil, err
		}
		if err := s.assertInteractionSurveys(ctx, tree, old.WorkspaceID); err != nil {
			return nil, err
		}
	}
	err = s.repo.transaction(ctx, func(tx *Repository) error {
		if err := tx.updateSegment(ctx, segmentID, in); err != nil {
			return err
		}
		if in.SurveysSet {
			return tx.linkSurveys(ctx, segmentID, old.WorkspaceID, in.Surveys)
		}
		return nil
	})
	if errors.Is(err, errSegmentTitleTaken) {
		return nil, apperr.InvalidInput(CodeSegmentTitleTaken, "Segment with this title already exists")
	}
	if err != nil {
		return nil, err
	}
	seg, err = s.repo.segment(ctx, segmentID)
	if err != nil {
		return nil, err
	}
	ev.OldObject, ev.NewObject = old, seg
	return seg, nil
}

// checkRecursion is forma2's checkForRecursiveSegmentFilter: no referenced segment, however deep, may be the one being saved. A referenced segment that does not exist is not found. Unlike forma2, a cycle among the referenced segments ends the walk instead of recursing forever.
func (s *Service) checkRecursion(ctx context.Context, tree Filters, segmentID string, visited map[string]bool) error {
	var refs []string
	walkLeaves(tree, func(f *Filter) {
		if f.Root.Type == filterRootSegment {
			refs = append(refs, f.Root.SegmentID)
		}
	})
	for _, ref := range refs {
		if ref == segmentID {
			return apperr.InvalidInput(CodeRecursiveSegment, "Recursive segment filter is not allowed")
		}
		if visited[ref] {
			continue
		}
		visited[ref] = true
		nested, _, err := loadSegmentFilters(ctx, s.repo.db, ref)
		if err != nil {
			return notFound(err)
		}
		if err := s.checkRecursion(ctx, nested, segmentID, visited); err != nil {
			return err
		}
	}
	return nil
}

// assertInteractionSurveys is forma2's assertSurveyInteractionSurveyIds: every survey a survey-interaction filter names must be the workspace's, looked up in batches; the first missing one is reported.
func (s *Service) assertInteractionSurveys(ctx context.Context, tree Filters, workspaceID uuid.UUID) error {
	var ids []string
	for _, id := range interactionSurveyIDs(tree) {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	for start := 0; start < len(ids); start += surveyWorkspaceLookupBatchSize {
		batch := ids[start:min(start+surveyWorkspaceLookupBatchSize, len(ids))]
		found, err := s.repo.surveysInWorkspace(ctx, workspaceID, batch)
		if err != nil {
			return err
		}
		for _, id := range batch {
			if !found[id] {
				return apperr.InvalidInput(CodeSurveyNotFound, "Survey not found in workspace: "+id)
			}
		}
	}
	return nil
}

// DeleteSegment is forma2's deleteSegmentAction: a segment no survey targets.
func (s *Service) DeleteSegment(ctx context.Context, userID, segmentID uuid.UUID) (old *Segment, err error) {
	ev := audit.Event{Action: audit.ActionDeleted, TargetType: audit.TargetSegment, TargetID: segmentID.String()}
	defer func() { s.record(ctx, ev, err) }()

	old, err = s.segmentForWrite(ctx, userID, segmentID, &ev)
	if err != nil {
		return nil, err
	}
	ev.OldObject = old
	if len(old.Surveys) > 0 {
		return nil, apperr.OperationNotAllowed("Cannot delete a segment that is associated with a survey")
	}
	if err := s.repo.deleteSegment(ctx, segmentID); err != nil {
		return nil, err
	}
	return old, nil
}

// surveyWorkspace is a survey's workspace; a missing survey is forma2's ResourceNotFoundError("survey", id).
func (s *Service) surveyWorkspace(ctx context.Context, surveyID uuid.UUID) (uuid.UUID, error) {
	ws, err := s.repo.surveyWorkspaces(ctx, []uuid.UUID{surveyID})
	if err != nil {
		return uuid.Nil, err
	}
	id, ok := ws[surveyID]
	if !ok {
		return uuid.Nil, apperr.NotFound("survey", surveyID.String())
	}
	return id, nil
}

// sameWorkspace resolves a survey and a segment and requires them to share a workspace, which it returns.
func (s *Service) sameWorkspace(ctx context.Context, surveyID, segmentID uuid.UUID) (uuid.UUID, error) {
	surveyWorkspace, err := s.surveyWorkspace(ctx, surveyID)
	if err != nil {
		return uuid.Nil, err
	}
	seg, err := s.repo.segment(ctx, segmentID)
	if err != nil {
		return uuid.Nil, err
	}
	if seg == nil {
		return uuid.Nil, apperr.NotFound("segment", segmentID.String())
	}
	if seg.WorkspaceID != surveyWorkspace {
		return uuid.Nil, apperr.InvalidInput(CodeWorkspaceMismatch, "Segment and survey are not in the same workspace")
	}
	return surveyWorkspace, nil
}

var copySuffix = regexp.MustCompile(`\(([0-9]+)\)$`)

// CloneSegment is forma2's cloneSegmentAction: a copy titled "Copy of <title> (<n>)", n one more than the highest such copy, linked to the survey.
func (s *Service) CloneSegment(ctx context.Context, userID, segmentID, surveyID uuid.UUID) (seg *Segment, err error) {
	ev := audit.Event{Action: audit.ActionCreated, TargetType: audit.TargetSegment, TargetID: segmentID.String()}
	defer func() { s.record(ctx, ev, err) }()

	workspaceID, err := s.sameWorkspace(ctx, surveyID, segmentID)
	if err != nil {
		return nil, err
	}
	org, err := s.organizationOf(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	ev.OrganizationID = org.String()
	if err := s.write(ctx, userID, workspaceID); err != nil {
		return nil, err
	}
	source, err := s.repo.segment(ctx, segmentID)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, apperr.NotFound("segment", segmentID.String())
	}
	titles, err := s.repo.segmentTitles(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	prefix := "Copy of " + source.Title
	highest := 0
	for _, t := range titles {
		if !strings.HasPrefix(t, prefix) {
			continue
		}
		if m := copySuffix.FindStringSubmatch(t); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				highest = max(highest, n)
			}
		}
	}
	if _, err := ParseFilters(s.schemas, source.Filters); err != nil {
		return nil, apperr.InvalidInput(apperr.CodeValidation, invalidFiltersMessage)
	}
	var id uuid.UUID
	err = s.repo.transaction(ctx, func(tx *Repository) error {
		var err error
		id, err = tx.insertSegment(ctx, workspaceID, fmt.Sprintf("%s (%d)", prefix, highest+1), source.Description, source.IsPrivate, source.Filters)
		if err != nil {
			return err
		}
		return tx.linkSurveys(ctx, id, workspaceID, []uuid.UUID{surveyID})
	})
	if errors.Is(err, errSegmentTitleTaken) {
		return nil, apperr.InvalidInput(CodeSegmentTitleTaken, "Segment with this title already exists")
	}
	if err != nil {
		return nil, err
	}
	seg, err = s.repo.segment(ctx, id)
	if err != nil {
		return nil, err
	}
	ev.NewObject = seg
	return seg, nil
}

// ResetSurveySegment is forma2's resetSegmentFiltersAction: the survey's private segment, the one titled with the survey's id in its workspace, is linked to it with no filters, and created if it is missing.
func (s *Service) ResetSurveySegment(ctx context.Context, userID, surveyID uuid.UUID) (seg *Segment, err error) {
	ev := audit.Event{Action: audit.ActionUpdated, TargetType: audit.TargetSegment}
	defer func() { s.record(ctx, ev, err) }()

	workspaceID, err := s.surveyWorkspace(ctx, surveyID)
	if err != nil {
		return nil, err
	}
	org, err := s.organizationOf(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	ev.OrganizationID = org.String()
	if err := s.write(ctx, userID, workspaceID); err != nil {
		return nil, err
	}
	var id uuid.UUID
	err = s.repo.transaction(ctx, func(tx *Repository) error {
		existing, err := tx.privateSegmentID(ctx, workspaceID, surveyID.String())
		if err != nil {
			return err
		}
		if existing != nil {
			id = *existing
			if err := tx.clearFilters(ctx, id); err != nil {
				return err
			}
		} else if id, err = tx.insertSegment(ctx, workspaceID, surveyID.String(), nil, true, json.RawMessage(`[]`)); err != nil {
			return err
		}
		return tx.linkSurveys(ctx, id, workspaceID, []uuid.UUID{surveyID})
	})
	if err != nil {
		return nil, err
	}
	seg, err = s.repo.segment(ctx, id)
	if err != nil {
		return nil, err
	}
	ev.TargetID, ev.NewObject = id.String(), seg
	return seg, nil
}
