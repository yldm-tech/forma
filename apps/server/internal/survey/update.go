package survey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// SegmentInput is the segment an editor save carries for an app survey.
type SegmentInput struct {
	ID          uuid.UUID       `json:"id"`
	Title       string          `json:"title"`
	Description *string         `json:"description"`
	IsPrivate   bool            `json:"isPrivate"`
	Filters     json.RawMessage `json:"filters"`
	Surveys     []uuid.UUID     `json:"surveys"`
}

// UpdateInput is an editor save: the whole survey as the editor holds it, forma2's TSurvey. Fields the editor cannot change (id, workspace, archive stamp, creator, timestamps) are ignored.
type UpdateInput struct {
	ID        uuid.UUID  `json:"id"`
	UpdatedAt *time.Time `json:"updatedAt"`
	Name      string     `json:"name"`
	Type      string     `json:"type"`
	Status    string     `json:"status"`

	WelcomeCard         json.RawMessage `json:"welcomeCard"`
	Blocks              json.RawMessage `json:"blocks"`
	Endings             json.RawMessage `json:"endings"`
	HiddenFields        json.RawMessage `json:"hiddenFields"`
	Variables           json.RawMessage `json:"variables"`
	Metadata            json.RawMessage `json:"metadata"`
	InlineTriggers      json.RawMessage `json:"inlineTriggers"`
	SurveyClosedMessage json.RawMessage `json:"surveyClosedMessage"`
	WorkspaceOverwrites json.RawMessage `json:"workspaceOverwrites"`
	Styling             json.RawMessage `json:"styling"`
	SingleUse           json.RawMessage `json:"singleUse"`
	Recaptcha           json.RawMessage `json:"recaptcha"`

	DisplayOption     string   `json:"displayOption"`
	AutoClose         *int     `json:"autoClose"`
	RecontactDays     *int     `json:"recontactDays"`
	DisplayLimit      *int     `json:"displayLimit"`
	AutoComplete      *int     `json:"autoComplete"`
	DisplayPercentage *float64 `json:"displayPercentage"`
	Delay             int      `json:"delay"`

	PublishOn *time.Time `json:"publishOn"`
	CloseOn   *time.Time `json:"closeOn"`
	// SchedulingTimeZone is the zone the editor showed the publish and close dates in, which is not part of forma2's TSurvey. The dates keep their calendar day in it when the organization's zone has changed since the editor loaded it; without it they are read in the organization's zone.
	SchedulingTimeZone *string `json:"schedulingTimeZone"`
	// SchedulingExactTimes says the publish and close dates carry a time of day the editor picked, on the scheduler's quarter-hour grid, which is kept; without it each new date is a calendar day, snapped to the schedule's local time as forma2 did. Not part of forma2's TSurvey either.
	SchedulingExactTimes bool `json:"schedulingExactTimes"`

	IsVerifyEmailEnabled        bool    `json:"isVerifyEmailEnabled"`
	IsBackButtonHidden          bool    `json:"isBackButtonHidden"`
	IsAutoProgressingEnabled    bool    `json:"isAutoProgressingEnabled"`
	IsCaptureIPEnabled          bool    `json:"isCaptureIpEnabled"`
	IsAnonymizeResponsesEnabled bool    `json:"isAnonymizeResponsesEnabled"`
	IsProgressSavingEnabled     *bool   `json:"isProgressSavingEnabled"`
	ShowLanguageSwitch          *bool   `json:"showLanguageSwitch"`
	Pin                         *string `json:"pin"`
	CustomHeadScripts           *string `json:"customHeadScripts"`
	CustomHeadScriptsMode       *string `json:"customHeadScriptsMode"`
	// The duplicate-submission limits (D-FG03), optional like isProgressSavingEnabled.
	IsSingleResponsePerBrowserEnabled *bool `json:"isSingleResponsePerBrowserEnabled"`
	IsSingleResponsePerEmailEnabled   *bool `json:"isSingleResponsePerEmailEnabled"`

	Triggers []struct {
		ActionClass struct {
			ID uuid.UUID `json:"id"`
		} `json:"actionClass"`
	} `json:"triggers"`
	Languages []struct {
		Language struct {
			ID uuid.UUID `json:"id"`
		} `json:"language"`
		Default bool `json:"default"`
		Enabled bool `json:"enabled"`
	} `json:"languages"`
	Segment   *SegmentInput             `json:"segment"`
	FollowUps []surveydoc.FollowUpInput `json:"followUps"`
}

func (in *UpdateInput) document() surveydoc.Document {
	return surveydoc.Document{
		WelcomeCard: in.WelcomeCard, Blocks: in.Blocks, Endings: in.Endings, HiddenFields: in.HiddenFields, Variables: in.Variables, Metadata: in.Metadata,
		Styling: in.Styling, SurveyClosedMessage: in.SurveyClosedMessage, SingleUse: in.SingleUse, Recaptcha: in.Recaptcha,
		WorkspaceOverwrites: in.WorkspaceOverwrites, InlineTriggers: in.InlineTriggers,
	}
}

func issue(path string, message string) surveydoc.Issue {
	return surveydoc.Issue{Path: []any{path}, Message: message}
}

// scalarIssues are ZSurveyBase's rules on the survey's own columns.
func scalarIssues(surveyType, status, displayOption string, autoComplete, recontactDays, displayLimit, delay *int, displayPercentage *float64, pin, customHeadScriptsMode *string) []surveydoc.Issue {
	var out []surveydoc.Issue
	if surveyType != TypeLink && surveyType != TypeApp {
		out = append(out, issue("type", `Invalid option: expected one of "link"|"app"`))
	}
	if !StatusIn(status) {
		out = append(out, issue("status", `Invalid option: expected one of "draft"|"inProgress"|"paused"|"completed"`))
	}
	if !slices.Contains(DisplayOptions, displayOption) {
		out = append(out, issue("displayOption", `Invalid option: expected one of "displayOnce"|"displayMultiple"|"respondMultiple"|"displaySome"`))
	}
	if autoComplete != nil && *autoComplete < 1 {
		out = append(out, issue("autoComplete", "Response limit must be greater than 0"))
	}
	for _, n := range []struct {
		name  string
		value *int
	}{{"recontactDays", recontactDays}, {"displayLimit", displayLimit}, {"delay", delay}} {
		if n.value != nil && *n.value < 0 {
			out = append(out, issue(n.name, "Too small: expected number to be >=0"))
		}
	}
	if displayPercentage != nil && (*displayPercentage < 0.01 || *displayPercentage > 100) {
		out = append(out, issue("displayPercentage", "Invalid input: display percentage must be between 0.01 and 100"))
	}
	if pin != nil && len([]rune(*pin)) != 4 {
		out = append(out, issue("pin", "PIN must be a four digit number"))
	}
	if customHeadScriptsMode != nil && *customHeadScriptsMode != "add" && *customHeadScriptsMode != "replace" {
		out = append(out, issue("customHeadScriptsMode", `Invalid option: expected one of "add"|"replace"`))
	}
	return out
}

// editorNameIssue is the editor's rule that a survey has a name: forma2's ZSurveyDraft required one for a draft save only, which let a full save (Publish, Save on a live survey) store an empty name. The editor saves both ways under this rule, and a name of only spaces counts as empty. The v1 management API keeps forma2's rule, so this is not in scalarIssues.
func editorNameIssue(name string) *surveydoc.Issue {
	if strings.TrimSpace(name) != "" {
		return nil
	}
	nameIssue := issue("name", "Survey name is required")
	return &nameIssue
}

// draftShapeIssues are what a draft save must still satisfy to be storable: forma2's ZSurveyDraft (a name, the draft status) and the columns' own constraints.
func draftShapeIssues(in *UpdateInput) []surveydoc.Issue {
	var out []surveydoc.Issue
	if nameIssue := editorNameIssue(in.Name); nameIssue != nil {
		out = append(out, *nameIssue)
	}
	if in.Status != StatusDraft {
		out = append(out, issue("status", `Invalid input: expected "draft"`))
	}
	if in.Type != TypeLink && in.Type != TypeApp {
		out = append(out, issue("type", `Invalid option: expected one of "link"|"app"`))
	}
	for _, part := range []struct {
		name  string
		raw   json.RawMessage
		array bool
	}{
		{"welcomeCard", in.WelcomeCard, false}, {"blocks", in.Blocks, true}, {"endings", in.Endings, true},
		{"hiddenFields", in.HiddenFields, false}, {"variables", in.Variables, true}, {"metadata", in.Metadata, false},
	} {
		t := strings.TrimSpace(string(part.raw))
		if part.array && !strings.HasPrefix(t, "[") || !part.array && !strings.HasPrefix(t, "{") {
			out = append(out, issue(part.name, "Invalid input"))
		}
	}
	out = append(out, scalarIssues(in.Type, in.Status, in.DisplayOption, nil, in.RecontactDays, in.DisplayLimit, &in.Delay, in.DisplayPercentage, nil, in.CustomHeadScriptsMode)...)
	return out
}

// Update is an editor save behind workspace.write and the state-mutation limit: forma2's updateSurveyAction, or with draft its updateSurveyDraftAction, which skips document validation for a draft that stays a draft and refuses a save based on a version older than the stored one.
func (s *Service) Update(ctx context.Context, caller Caller, in UpdateInput, draft bool) (*Survey, error) {
	current, err := s.authorized(ctx, caller, in.ID, authz.SurveyWrite)
	if err != nil {
		return nil, err
	}
	if err := s.stateMutation(ctx, current.WorkspaceID); err != nil {
		return nil, err
	}
	var recaptcha struct {
		Enabled bool `json:"enabled"`
	}
	if !isNullJSON(in.Recaptcha) && json.Unmarshal(in.Recaptcha, &recaptcha) == nil && recaptcha.Enabled && !s.cfg.RecaptchaConfigured() {
		return nil, apperr.OperationNotAllowed("Spam protection is not enabled for this organization")
	}
	if err := draftVersionConflict(current, &in, draft); err != nil {
		return nil, err
	}
	// A draft save reports a missing name with the rest of its shape issues; a full save is refused here, and audited as a failed update like every other refused save. The check stays out of update(), which the v1 API shares under forma2's rule.
	var updated *Survey
	if nameIssue := editorNameIssue(in.Name); !draft && nameIssue != nil {
		err = ValidationError([]surveydoc.Issue{*nameIssue})
	} else {
		updated, err = s.update(ctx, current, &in, draft)
	}
	org := s.organizationOf(ctx, current.WorkspaceID)
	s.record(ctx, caller, audit.ActionUpdated, audit.TargetSurvey, in.ID.String(), org, err, current, updated)
	if err == nil {
		captureEditorSave(caller, org, current, updated)
	}
	return updated, err
}

// draftVersionConflict refuses a draft save whose version is older than the stored survey's. Versions are compared to the millisecond: forma2 stored them at that precision, and the editor, holding them as JavaScript Dates, sends back no finer one, while updated_at carries microseconds. The stored version is named at the same precision, so the editor can adopt it.
func draftVersionConflict(stored *Survey, in *UpdateInput, draft bool) error {
	if !draft || in.UpdatedAt == nil {
		return nil
	}
	version := stored.UpdatedAt.UTC().Truncate(time.Millisecond)
	if version.After(in.UpdatedAt.Truncate(time.Millisecond)) {
		return apperr.OperationNotAllowed("survey_modified_elsewhere:" + version.Format("2006-01-02T15:04:05.000Z07:00"))
	}
	return nil
}

// update is forma2's updateSurveyInternal for one stored survey. Everything the save depends on in the stored row (its archive stamp, version, status, declared fields, segment, slug and every column the save does not carry) is read again under a row lock in the transaction that writes it, so a concurrent change is neither overwritten nor bypassed; current is the unlocked snapshot the request was authorized against.
func (s *Service) update(ctx context.Context, current *Survey, in *UpdateInput, skipValidation bool) (*Survey, error) {
	langIDs := make([]uuid.UUID, len(in.Languages))
	for i, l := range in.Languages {
		langIDs[i] = l.Language.ID
	}
	triggerIDs := make([]uuid.UUID, len(in.Triggers))
	for i, t := range in.Triggers {
		triggerIDs[i] = t.ActionClass.ID
	}
	var parsed *surveydoc.Survey
	if skipValidation {
		if issues := draftShapeIssues(in); len(issues) > 0 {
			return nil, ValidationError(issues)
		}
	} else {
		issues := scalarIssues(in.Type, in.Status, in.DisplayOption, in.AutoComplete, in.RecontactDays, in.DisplayLimit, &in.Delay, in.DisplayPercentage, in.Pin, in.CustomHeadScriptsMode)
		langs := make([]surveydoc.Language, 0, len(in.Languages))
		if len(in.Languages) > 0 {
			found, err := s.repo.workspaceLanguagesByID(ctx, current.WorkspaceID, langIDs)
			if err != nil {
				return nil, err
			}
			for _, l := range in.Languages {
				langs = append(langs, surveydoc.Language{Code: found[l.Language.ID].Code, Default: l.Default, Enabled: l.Enabled})
			}
		}
		docIssues, doc, err := s.docs.Check(in.document(), langs, in.FollowUps)
		if err != nil {
			return nil, err
		}
		issues = append(issues, docIssues...)
		if len(issues) > 0 {
			return nil, ValidationError(issues)
		}
		parsed = doc
	}

	incoming, err := declared(in.Variables, in.HiddenFields)
	if err != nil {
		return nil, err
	}
	// The stored survey's state allows the save: it is not archived, its declared names are not renamed into reserved ones, and a save without validation keeps a draft a draft.
	storedAllows := func(stored *Survey) error {
		if stored.ArchivedAt != nil {
			return apperr.InvalidInput(apperr.CodeBadRequest, "This survey is archived. Restore it before editing.")
		}
		existing, err := declared(stored.Variables, stored.HiddenFields)
		if err != nil {
			return err
		}
		if err := guardDeclaredFields(existing, incoming); err != nil {
			return err
		}
		if skipValidation && (stored.Status != StatusDraft || in.Status != StatusDraft) {
			return apperr.OperationNotAllowed("Only draft surveys can be updated without validation")
		}
		return nil
	}
	if err := storedAllows(current); err != nil {
		return nil, err
	}
	if in.Segment != nil {
		seg, err := s.repo.segmentInWorkspace(ctx, in.Segment.ID, current.WorkspaceID)
		if err != nil {
			return nil, err
		}
		if seg == nil {
			return nil, apperr.NotFound("Segment", in.Segment.ID.String())
		}
	}
	languages, err := s.checkLanguages(ctx, current.WorkspaceID, langIDs)
	if err != nil {
		return nil, err
	}
	if !skipValidation {
		if appSurveyMissingTriggers(in.Type, in.Status, len(in.Triggers)) {
			return nil, apperr.InvalidInput(apperr.CodeBadRequest, appTriggerMessage)
		}
		if m := surveydoc.MediaIssue(parsed.Blocks); m != "" {
			return nil, apperr.InvalidInput(apperr.CodeBadRequest, m)
		}
	}
	if err := s.validateTriggers(ctx, current.WorkspaceID, triggerIDs); err != nil {
		return nil, err
	}
	links := make([]languageLink, 0, len(in.Languages))
	for _, l := range in.Languages {
		links = append(links, languageLink{LanguageID: languages[l.Language.ID].ID, Default: l.Default, Enabled: l.Enabled})
	}
	followUps, err := followUpWrites(in.FollowUps)
	if err != nil {
		return nil, err
	}
	if in.Type == TypeApp && in.Segment != nil {
		if err := s.checkSegmentSave(ctx, current, in.Segment, skipValidation); err != nil {
			return nil, err
		}
	}

	err = s.repo.transaction(ctx, func(tx *Repository) error {
		// Before the survey's lock, in the order a zone change locks (lockedScheduleOf). A survey never changes workspace, so the unlocked snapshot names the right organization.
		schedule, err := s.lockedScheduleOf(ctx, tx, current.WorkspaceID)
		if err != nil {
			return err
		}
		stored, err := tx.lockForUpdate(ctx, current.ID)
		if err != nil {
			return err
		}
		if stored == nil {
			return apperr.NotFound("Survey", current.ID.String())
		}
		if err := draftVersionConflict(stored, in, skipValidation); err != nil {
			return err
		}
		if err := storedAllows(stored); err != nil {
			return err
		}
		rec := recordOf(stored)
		rec.Name, rec.Type, rec.Status = in.Name, in.Type, in.Status
		rec.WelcomeCard, rec.Blocks, rec.Endings, rec.HiddenFields, rec.Variables = in.WelcomeCard, stripIsDraft(in.Blocks), in.Endings, in.HiddenFields, in.Variables
		if !isNullJSON(in.Metadata) {
			rec.Metadata = in.Metadata
		}
		rec.InlineTriggers, rec.SurveyClosedMessage, rec.WorkspaceOverwrites, rec.Styling = in.InlineTriggers, in.SurveyClosedMessage, in.WorkspaceOverwrites, in.Styling
		rec.SingleUse, rec.Recaptcha = in.SingleUse, in.Recaptcha
		rec.DisplayOption, rec.AutoClose, rec.RecontactDays, rec.DisplayLimit, rec.AutoComplete = in.DisplayOption, in.AutoClose, in.RecontactDays, in.DisplayLimit, in.AutoComplete
		rec.DisplayPercentage, rec.Delay = in.DisplayPercentage, in.Delay
		rec.IsVerifyEmailEnabled, rec.IsBackButtonHidden, rec.IsAutoProgressingEnabled = in.IsVerifyEmailEnabled, in.IsBackButtonHidden, in.IsAutoProgressingEnabled
		rec.IsCaptureIPEnabled, rec.IsAnonymizeResponsesEnabled, rec.ShowLanguageSwitch = in.IsCaptureIPEnabled, in.IsAnonymizeResponsesEnabled, in.ShowLanguageSwitch
		// Optional, unlike the switches above: a save that does not carry it (an editor loaded before it existed) keeps the stored value instead of turning progress saving off.
		if in.IsProgressSavingEnabled != nil {
			rec.IsProgressSavingEnabled = *in.IsProgressSavingEnabled
		}
		// The duplicate-submission limits are optional for the same reason: an editor loaded before they existed must not turn them off.
		if in.IsSingleResponsePerBrowserEnabled != nil {
			rec.IsSingleResponsePerBrowserEnabled = *in.IsSingleResponsePerBrowserEnabled
		}
		if in.IsSingleResponsePerEmailEnabled != nil {
			rec.IsSingleResponsePerEmailEnabled = *in.IsSingleResponsePerEmailEnabled
		}
		rec.Pin, rec.CustomHeadScripts, rec.CustomHeadScriptsMode = in.Pin, in.CustomHeadScripts, in.CustomHeadScriptsMode
		storedStatus := stored.Status
		if rec.PublishOn, rec.CloseOn, err = normalizeDates(schedule, pickedIn(schedule, in.SchedulingTimeZone), &storedStatus, in.Status, scheduledDates{in.PublishOn, in.CloseOn}, scheduledDates{stored.PublishOn, stored.CloseOn}, in.SchedulingExactTimes); err != nil {
			return err
		}

		if err := tx.setLanguages(ctx, stored.ID, links); err != nil {
			return err
		}
		if err := tx.setTriggers(ctx, stored.ID, triggerIDs); err != nil {
			return err
		}
		if err := tx.saveSegment(ctx, stored, in.Type, in.Segment); err != nil {
			return err
		}
		if err := tx.applyFollowUps(ctx, stored.ID, followUps); err != nil {
			return err
		}
		if err := tx.update(ctx, stored.ID, rec); err != nil {
			return err
		}
		return tx.reconcileEmbeddedData(ctx, stored.ID, stored.WorkspaceID, embeddedPatch{Variables: incoming.Variables, HiddenFields: incoming.HiddenFields})
	})
	if err != nil {
		if errors.Is(err, errSlugTaken) {
			return nil, apperr.InvalidInput(apperr.CodeConflict, "A survey with this slug already exists")
		}
		return nil, err
	}
	sv, err := s.repo.byID(ctx, current.ID)
	if err != nil {
		return nil, err
	}
	if changed, err := s.scheduler.reconcileIfDue(ctx, sv, s.now()); err != nil {
		return nil, err
	} else if changed {
		return s.repo.byID(ctx, current.ID)
	}
	return sv, nil
}

// checkSegmentSave is forma2's guard on an app survey's segment: the filter tree within bounds even on a draft, valid filters when validating, and the surveys it connects in the survey's workspace.
func (s *Service) checkSegmentSave(ctx context.Context, current *Survey, seg *SegmentInput, skipValidation bool) error {
	if m := segmentFilterBoundsViolation(seg.Filters); m != "" {
		return apperr.InvalidInput(apperr.CodeBadRequest, m)
	}
	if !skipValidation {
		violations, err := s.segmentFilterIssues(seg.Filters)
		if err != nil {
			return err
		}
		if len(violations) > 0 {
			return apperr.InvalidInput(apperr.CodeBadRequest, "Invalid user segment filters")
		}
	}
	if len(seg.Surveys) > segmentSurveyIDsLimit {
		return apperr.InvalidInput(apperr.CodeBadRequest, fmt.Sprintf("Invalid segment surveys: at most %d valid survey ids are allowed", segmentSurveyIDsLimit))
	}
	if len(seg.Surveys) > 0 {
		workspaces, err := s.repo.workspaceOfSurveys(ctx, seg.Surveys)
		if err != nil {
			return err
		}
		for _, id := range seg.Surveys {
			if workspaces[id] != current.WorkspaceID {
				return apperr.InvalidInput(apperr.CodeBadRequest, "Survey and segment are not in the same workspace")
			}
		}
	}
	return nil
}

// saveSegment is forma2's segment handling on save: an app survey writes its segment's own fields (and connects the listed surveys), or gets a private segment when it has none; a survey that is not an app survey drops its segment, deleting it when private.
func (r *Repository) saveSegment(ctx context.Context, current *Survey, surveyType string, seg *SegmentInput) error {
	switch {
	case seg != nil && surveyType == TypeApp:
		filters := seg.Filters
		if isNullJSON(filters) {
			filters = json.RawMessage(`[]`)
		}
		if err := r.updateSegment(ctx, seg.ID, seg.Title, seg.Description, seg.IsPrivate, filters); err != nil {
			return err
		}
		if len(seg.Surveys) > 0 {
			return r.connectSegmentSurveys(ctx, seg.ID, seg.Surveys)
		}
		return nil
	case seg != nil:
		if err := r.setSurveySegment(ctx, current.ID, nil); err != nil {
			return err
		}
		if seg.IsPrivate {
			return r.deleteSegment(ctx, seg.ID)
		}
		return nil
	case surveyType == TypeApp && current.Segment == nil:
		var existing []uuid.UUID
		if err := r.db.WithContext(ctx).Raw(`SELECT id FROM segments WHERE workspace_id = ? AND title = ?`, current.WorkspaceID, current.ID.String()).Scan(&existing).Error; err != nil {
			return fmt.Errorf("find private segment: %w", err)
		}
		var id uuid.UUID
		if len(existing) > 0 {
			id = existing[0]
		} else {
			created, err := r.createSegment(ctx, current.WorkspaceID, current.ID.String(), true, nil)
			if err != nil {
				return err
			}
			id = created
		}
		return r.setSurveySegment(ctx, current.ID, &id)
	}
	return nil
}

// followUpWrites reads the follow-ups of a save; their ids are the client's choice and must be uuids.
func followUpWrites(in []surveydoc.FollowUpInput) ([]followUpWrite, error) {
	out := make([]followUpWrite, 0, len(in))
	for i, f := range in {
		id, err := uuid.Parse(f.ID)
		if err != nil {
			return nil, apperr.Validation([]apperr.FieldError{{Field: fmt.Sprintf("followUps.%d.id", i), Rule: "uuid"}})
		}
		out = append(out, followUpWrite{ID: id, Name: f.Name, Trigger: f.Trigger, Action: f.Action, Deleted: f.Deleted})
	}
	return out, nil
}

// stripIsDraft is forma2's stripIsDraftFromBlocks: the editor's draft marker on elements is never stored.
func stripIsDraft(blocks json.RawMessage) json.RawMessage {
	var parsed []map[string]json.RawMessage
	if json.Unmarshal(blocks, &parsed) != nil {
		return blocks
	}
	changed := false
	for _, block := range parsed {
		var elements []map[string]json.RawMessage
		if json.Unmarshal(block["elements"], &elements) != nil {
			continue
		}
		for _, el := range elements {
			if _, ok := el["isDraft"]; ok {
				delete(el, "isDraft")
				changed = true
			}
		}
		block["elements"], _ = json.Marshal(elements)
	}
	if !changed {
		return blocks
	}
	out, err := json.Marshal(parsed)
	if err != nil {
		return blocks
	}
	return out
}
