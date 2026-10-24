package survey

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/v3api"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// allowedLanguageCodes is forma2's getV3SurveyPatchAllowedLanguageCodes: the codes a patch may keep using as they are stored.
func allowedLanguageCodes(s *Survey) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range v3Languages(s.Languages) {
		if !seen[l.Code] {
			seen[l.Code] = true
			out = append(out, l.Code)
		}
	}
	for _, l := range s.Languages {
		if !seen[l.Language.Code] {
			seen[l.Language.Code] = true
			out = append(out, l.Language.Code)
		}
	}
	return out
}

// storedDocument is forma2's buildDocumentFromSurvey: the stored survey read back as a v3 document, the base a patch merges over.
func (s *Service) storedDocument(sv *Survey, allowed []string) (*v3Document, []v3api.InvalidParam) {
	defaultLanguage := v3DefaultLanguage(sv.Languages)
	o := newObject()
	o.set("name", sv.Name)
	o.set("status", sv.Status)
	meta := decoded(sv.Metadata)
	if meta == nil {
		meta = newObject()
	}
	o.set("metadata", meta)
	o.set("defaultLanguage", defaultLanguage)
	langs := []any{}
	for _, l := range v3Languages(sv.Languages) {
		lo := newObject()
		lo.set("code", l.Code)
		lo.set("default", l.Default)
		lo.set("enabled", l.Enabled)
		langs = append(langs, lo)
	}
	o.set("languages", langs)
	o.set("welcomeCard", decoded(sv.WelcomeCard))
	o.set("blocks", decoded(sv.Blocks))
	o.set("endings", decoded(sv.Endings))
	o.set("hiddenFields", decoded(sv.HiddenFields))
	o.set("variables", decoded(sv.Variables))
	if sv.Type == TypeApp {
		o.set("distribution", decoded(encodeValue(v3DistributionOf(sv))))
		o.set("targeting", decoded(encodeValue(v3TargetingOf(sv))))
	}
	normalizeDocument(o, defaultLanguage, true, true, false, allowed)
	p := &parser{docs: s.docs}
	d := p.parse(o, parseStored, allowed, sv.Type, defaultLanguage)
	if len(p.issues) > 0 {
		return nil, invalidParams(p.issues, "survey")
	}
	return d, nil
}

// parsePatch is createZV3PatchSurveyBodySchema: the strictness pre-check against the patchable keys, then the patch schema; its failures are semantic (422) because the body is checked against the stored survey.
func (s *Service) parsePatch(value any, sv *Survey, defaultLanguage string, allowed []string) (*v3Document, []v3api.InvalidParam) {
	if params := unsupportedFields(value, patchRootKeys, defaultLanguage, allowed); len(params) > 0 {
		return nil, params
	}
	o := asObject(value)
	if o == nil {
		return nil, []v3api.InvalidParam{{Name: "data", Reason: fmt.Sprintf("Invalid input: expected object, received %s", jsType(value))}}
	}
	o = o.clone()
	normalizeDocument(o, defaultLanguage, false, false, false, allowed)
	p := &parser{docs: s.docs}
	d := p.parse(o, parsePatch, allowed, sv.Type, defaultLanguage)
	if len(p.issues) > 0 {
		return nil, invalidParams(p.issues, "data")
	}
	return d, nil
}

// merge is forma2's mergeV3SurveyPatch: every key the patch carries replaces the stored one whole.
func merge(current, patch *v3Document) *v3Document {
	out := *current
	for key := range patch.present {
		switch key {
		case "name":
			out.Name = patch.Name
		case "status":
			out.Status = patch.Status
		case "metadata":
			out.Metadata = patch.Metadata
		case "languages":
			out.Languages = patch.Languages
		case "welcomeCard":
			out.WelcomeCard = patch.WelcomeCard
		case "blocks":
			out.Blocks = patch.Blocks
		case "endings":
			out.Endings = patch.Endings
		case "hiddenFields":
			out.HiddenFields = patch.HiddenFields
		case "variables":
			out.Variables = patch.Variables
		case "distribution":
			out.Distribution = patch.Distribution
		case "targeting":
			out.Targeting = patch.Targeting
		}
	}
	return &out
}

// immutableElementIDs is forma2's getImmutableElementIdIssues: once a survey is not a draft, an element that is not itself a draft keeps its id. The comparison is positional within the same block, so a deletion that shifts survivors is not a rename.
func immutableElementIDs(current, patched *surveydoc.Survey, currentStatus string) []v3api.InvalidParam {
	if currentStatus == StatusDraft {
		return nil
	}
	ids := func(s *surveydoc.Survey) map[string]bool {
		out := map[string]bool{}
		for _, b := range s.Blocks {
			for _, e := range b.Elements {
				out[e.ID] = true
			}
		}
		return out
	}
	patchedIDs, currentIDs := ids(patched), ids(current)
	var out []v3api.InvalidParam
	for _, cb := range current.Blocks {
		pb := -1
		for i, b := range patched.Blocks {
			if b.ID == cb.ID {
				pb = i
				break
			}
		}
		if pb < 0 {
			continue
		}
		for ei, ce := range cb.Elements {
			if (ce.IsDraft != nil && *ce.IsDraft) || patchedIDs[ce.ID] {
				continue
			}
			if ei >= len(patched.Blocks[pb].Elements) {
				continue
			}
			pe := patched.Blocks[pb].Elements[ei]
			if pe.ID == ce.ID || currentIDs[pe.ID] {
				continue
			}
			out = append(out, v3api.InvalidParam{
				Name: fmt.Sprintf("blocks.%d.elements.%d.id", pb, ei), Reason: fmt.Sprintf("Element id '%s' cannot be changed because the survey and element are no longer drafts", ce.ID),
				Code: v3api.CodeImmutableIdentifier, Identifier: ce.ID, ReferenceType: "element",
			})
		}
	}
	return out
}

// preparePatch is forma2's prepareV3SurveyPatchInput: the merged document, or the invalid params that refuse it.
func (s *Service) preparePatch(sv *Survey, body any) (*v3Document, *surveydoc.Survey, []v3api.InvalidParam, error) {
	allowed := allowedLanguageCodes(sv)
	current, params := s.storedDocument(sv, allowed)
	if len(params) > 0 {
		return nil, nil, params, nil
	}
	patch, params := s.parsePatch(body, sv, current.DefaultLanguage, allowed)
	if len(params) > 0 {
		return nil, nil, params, nil
	}
	merged := merge(current, patch)
	currentTyped, err := current.typed()
	if err != nil {
		return nil, nil, nil, err
	}
	mergedTyped, err := merged.typed()
	if err != nil {
		return nil, nil, nil, err
	}
	if params := immutableElementIDs(currentTyped, mergedTyped, current.Status); len(params) > 0 {
		return nil, nil, params, nil
	}
	params, typed, err := validateDocument(merged)
	if err != nil || len(params) > 0 {
		return nil, nil, params, err
	}
	return merged, typed, nil, nil
}

// PatchV3 is forma2's PATCH /api/v3/surveys/{id} behind workspace.write. An archived survey is read-only; everything the patch could be refused for is checked before the first write. The patch is merged into, checked against and written over the stored survey as it is under a row lock, so a concurrent change (an archive, a slug, another patch) is neither overwritten nor bypassed.
func (s *Service) PatchV3(ctx context.Context, caller Caller, id uuid.UUID, body json.RawMessage) (*v3Resource, error) {
	value, err := decodeValue(body)
	if err != nil {
		return nil, v3api.BadRequest("Invalid request body", v3api.InvalidParam{Name: "body", Reason: "Malformed JSON input, please check your request body"})
	}
	current, err := s.authorizedV3(ctx, caller, id, authz.WorkspaceWrite)
	if err != nil {
		return nil, err
	}
	if current.ArchivedAt != nil {
		return nil, errArchivedV3
	}
	before, updated, err := s.patchV3(ctx, current.ID, value)
	if err != nil {
		s.recordV3(ctx, caller, audit.ActionUpdated, id.String(), current.WorkspaceID, err, nil, nil)
		return nil, err
	}
	oldResource, _ := toV3Resource(before, nil)
	r, _ := toV3Resource(updated, nil)
	s.recordV3(ctx, caller, audit.ActionUpdated, id.String(), current.WorkspaceID, nil, oldResource, r)
	return r, nil
}

// errArchivedV3 refuses a change to an archived survey.
var errArchivedV3 = v3api.Unprocessable("Survey is archived", v3api.InvalidParam{Name: "archivedAt", Reason: "This survey is archived. Restore it before editing."})

// patchV3 applies the patch to the survey as it is stored under the row lock, returning that survey and the patched one.
func (s *Service) patchV3(ctx context.Context, id uuid.UUID, body any) (before, after *Survey, err error) {
	err = s.repo.transaction(ctx, func(tx *Repository) error {
		current, err := tx.lockForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if current == nil {
			return v3api.Forbidden("")
		}
		if current.ArchivedAt != nil {
			return errArchivedV3
		}
		before = current
		return s.applyPatchV3(ctx, tx, current, body)
	})
	if err != nil {
		return nil, nil, v3Problem(err)
	}
	sv, err := s.repo.byID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	changed, err := s.scheduler.reconcileIfDue(ctx, sv, s.now())
	if err != nil {
		return nil, nil, err
	}
	if changed {
		if sv, err = s.repo.byID(ctx, id); err != nil {
			return nil, nil, err
		}
	}
	return before, sv, nil
}

// applyPatchV3 checks the patch against the locked survey and writes it in tx.
func (s *Service) applyPatchV3(ctx context.Context, tx *Repository, current *Survey, body any) error {
	d, typed, params, err := s.preparePatch(current, body)
	if err != nil {
		return err
	}
	if len(params) > 0 {
		return v3api.Unprocessable(failedValidation, params...)
	}
	triggerCount := len(current.Triggers)
	if d.Distribution != nil {
		triggerCount = len(d.Distribution.Triggers)
	}
	if appSurveyMissingTriggers(current.Type, d.Status, triggerCount) {
		return v3api.Unprocessable(failedValidation, v3api.InvalidParam{Name: "triggers", Reason: appTriggerMessage})
	}
	existing, err := declared(current.Variables, current.HiddenFields)
	if err != nil {
		return err
	}
	if errs := surveydoc.ValidateNewDeclaredFieldNames(existing, surveydoc.DeclaredFields{Variables: &typed.Variables, HiddenFields: &typed.HiddenFields}); len(errs) > 0 {
		params := make([]v3api.InvalidParam, len(errs))
		for i, e := range errs {
			params[i] = v3api.InvalidParam{Name: e.Field, Reason: surveydoc.DescribeDeclaredFieldNameError(e), Code: v3api.CodeForbiddenIdentifier, Identifier: e.Field}
		}
		return v3api.Unprocessable(failedValidation, params...)
	}

	var triggerIDs []uuid.UUID
	var segmentFilters json.RawMessage
	if current.Type == TypeApp && d.Distribution != nil {
		ids, tps, err := s.resolveTriggers(ctx, current.WorkspaceID, d.Distribution.Triggers)
		if err != nil {
			return err
		}
		if len(tps) > 0 {
			return v3api.Unprocessable(failedValidation, tps...)
		}
		triggerIDs = ids
		next := json.RawMessage(`[]`)
		if d.Targeting != nil {
			next = d.Targeting.Filters
		}
		stored := json.RawMessage(`[]`)
		if current.Segment != nil {
			stored = current.Segment.Filters
		}
		if !sameJSON(stored, next) {
			if current.Segment == nil {
				return v3api.Unprocessable(failedValidation, v3api.InvalidParam{Name: "targeting.filters", Reason: "Cannot apply contact targeting: this app survey has no segment to store filters on."})
			}
			tps, err := s.targetingParams(ctx, current.WorkspaceID, next)
			if err != nil {
				return err
			}
			if len(tps) > 0 {
				return v3api.Unprocessable(failedValidation, tps...)
			}
			segmentFilters = next
		}
	}

	requests := languageRequests(d)
	codes := make([]string, len(requests))
	for i, r := range requests {
		codes[i] = r.Code
	}
	currentStatus := current.Status
	publishOn, closeOn, err := s.schedule.normalize(&currentStatus, d.Status, current.PublishOn, current.CloseOn)
	if err != nil {
		return err
	}
	languages, err := tx.ensureLanguages(ctx, current.WorkspaceID, codes)
	if err != nil {
		return err
	}
	links := make([]languageLink, 0, len(requests))
	for _, r := range requests {
		links = append(links, languageLink{LanguageID: languages[strings.ToLower(r.Code)].ID, Default: r.Default, Enabled: r.Enabled})
	}
	rec := recordOf(current)
	rec.Name, rec.Status = d.Name, d.Status
	rec.Metadata, rec.WelcomeCard, rec.Blocks = encodeValue(d.Metadata), encodeValue(d.WelcomeCard), stripIsDraft(encodeValue(d.Blocks))
	rec.Endings, rec.HiddenFields, rec.Variables = encodeValue(d.Endings), encodeValue(d.HiddenFields), encodeValue(d.Variables)
	rec.PublishOn, rec.CloseOn = publishOn, closeOn
	if current.Type == TypeApp && d.Distribution != nil {
		applyDistribution(&rec, d.Distribution)
	}
	if segmentFilters != nil {
		if err := tx.setSegmentFilters(ctx, current.Segment.ID, segmentFilters); err != nil {
			return err
		}
	}
	if err := tx.update(ctx, current.ID, rec); err != nil {
		return err
	}
	if err := tx.setLanguages(ctx, current.ID, links); err != nil {
		return err
	}
	if current.Type == TypeApp && d.Distribution != nil {
		if err := tx.setTriggers(ctx, current.ID, triggerIDs); err != nil {
			return err
		}
	}
	return tx.reconcileEmbeddedData(ctx, current.ID, current.WorkspaceID, embeddedPatch{Variables: &typed.Variables, HiddenFields: &typed.HiddenFields})
}

// DeleteV3, ArchiveV3 and RestoreV3 are the v3 forms of Delete, Archive and Restore: a missing survey is the same 403 as a denied one.
func (s *Service) DeleteV3(ctx context.Context, caller Caller, id uuid.UUID) error {
	return v3Problem(s.Delete(ctx, caller, id))
}

func (s *Service) ArchiveV3(ctx context.Context, caller Caller, id uuid.UUID) (*v3Lifecycle, error) {
	out, err := s.Archive(ctx, caller, id)
	if err != nil {
		return nil, v3Problem(err)
	}
	return toV3Lifecycle(out), nil
}

func (s *Service) RestoreV3(ctx context.Context, caller Caller, id uuid.UUID) (*v3Lifecycle, error) {
	out, err := s.Restore(ctx, caller, id)
	if err != nil {
		return nil, v3Problem(err)
	}
	return toV3Lifecycle(out), nil
}

// v3Lifecycle is the minimal acknowledgement of an archive or restore.
type v3Lifecycle struct {
	ID         uuid.UUID `json:"id"`
	Status     string    `json:"status"`
	ArchivedAt *string   `json:"archivedAt"`
}

func toV3Lifecycle(l *Lifecycle) *v3Lifecycle {
	return &v3Lifecycle{ID: l.ID, Status: l.Status, ArchivedAt: isoTimePtr(l.ArchivedAt)}
}

// ValidationResult is forma2's dry-run validation answer.
type ValidationResult struct {
	Valid         bool                 `json:"valid"`
	Operation     string               `json:"operation"`
	InvalidParams []v3api.InvalidParam `json:"invalid_params"`
	Languages     []languageRequest    `json:"languages,omitempty"`
}

func validationResult(operation string, d *v3Document, params []v3api.InvalidParam) *ValidationResult {
	if len(params) > 0 {
		return &ValidationResult{Valid: false, Operation: operation, InvalidParams: params}
	}
	requests := languageRequests(d)
	for i := range requests {
		requests[i].WriteBehavior = "connect_or_create"
	}
	return &ValidationResult{Valid: true, Operation: operation, InvalidParams: []v3api.InvalidParam{}, Languages: requests}
}

// ValidateV3 is forma2's POST /api/v3/surveys/validate: a dry run of a create or a patch that writes nothing, gated at read. A create names its workspace only optionally; without a parseable one no access check runs.
func (s *Service) ValidateV3(ctx context.Context, caller Caller, body json.RawMessage) (*ValidationResult, error) {
	value, err := decodeValue(body)
	if err != nil {
		return nil, v3api.BadRequest("Invalid request body", v3api.InvalidParam{Name: "body", Reason: "Malformed JSON input, please check your request body"})
	}
	o := asObject(value)
	operation, _ := o.get("operation")
	switch operation {
	case "create", "patch":
	default:
		return nil, v3api.BadRequest("Invalid request body", v3api.InvalidParam{Name: "operation", Reason: `Invalid input: expected "create" | "patch"`})
	}
	allowed := keySet("operation", "data")
	if operation == "patch" {
		allowed["surveyId"] = true
	}
	var unknown []string
	for _, k := range o.keys {
		if !allowed[k] {
			unknown = append(unknown, `"`+k+`"`)
		}
	}
	if len(unknown) > 0 {
		reason := "Unrecognized key: " + unknown[0]
		if len(unknown) > 1 {
			reason = "Unrecognized keys: " + strings.Join(unknown, ", ")
		}
		return nil, v3api.BadRequest("Invalid request body", v3api.InvalidParam{Name: "body", Reason: reason})
	}
	data, _ := o.get("data")
	if operation == "create" {
		if wid := asObject(data); wid != nil {
			if raw, ok := wid.values["workspaceId"].(string); ok {
				if workspaceID, err := uuid.Parse(raw); err == nil {
					if err := s.requireWorkspaceV3(ctx, caller, workspaceID, authz.WorkspaceRead); err != nil {
						return nil, err
					}
				}
			}
		}
		d, issues := s.parseCreateBody(data)
		if len(issues) > 0 {
			return validationResult("create", nil, invalidParams(issues, "data")), nil
		}
		params, _, err := validateDocument(d)
		if err != nil {
			return nil, err
		}
		return validationResult("create", d, params), nil
	}
	rawID, _ := o.get("surveyId")
	idText, _ := rawID.(string)
	id, err := uuid.Parse(idText)
	if err != nil {
		return nil, v3api.BadRequest("Invalid request body", v3api.InvalidParam{Name: "surveyId", Reason: "Invalid UUID"})
	}
	sv, err := s.authorizedV3(ctx, caller, id, authz.WorkspaceRead)
	if err != nil {
		return nil, err
	}
	d, _, params, err := s.preparePatch(sv, data)
	if err != nil {
		return nil, err
	}
	return validationResult("patch", d, params), nil
}
