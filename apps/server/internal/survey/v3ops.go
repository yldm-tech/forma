package survey

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/v3api"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// This file is forma2's apps/web/app/api/v3/surveys: the operations behind /api/v3/surveys, each answering with a result or a *v3api.Problem.

const failedValidation = "Survey document failed validation"

// requireWorkspaceV3 is requireV3WorkspaceAccess for a parsed workspace id.
func (s *Service) requireWorkspaceV3(ctx context.Context, caller Caller, workspaceID uuid.UUID, action authz.Action) error {
	return v3api.RequireWorkspace(ctx, s.authz, caller.Actor, action, workspaceID)
}

// authorizedV3 is forma2's getAuthorizedV3Survey: a missing survey and a denied one are the same 403.
func (s *Service) authorizedV3(ctx context.Context, caller Caller, id uuid.UUID, action authz.Action) (*Survey, error) {
	sv, err := s.repo.byID(ctx, id)
	if err != nil {
		return nil, err
	}
	if sv == nil {
		return nil, v3api.Forbidden("")
	}
	if err := s.requireWorkspaceV3(ctx, caller, sv.WorkspaceID, action); err != nil {
		return nil, err
	}
	return sv, nil
}

// v3Problem maps a service error onto v3's contract: not found and forbidden are the same 403, an invalid input is 400 on the body, anything else is returned for a 500.
func v3Problem(err error) error {
	e, ok := apperr.As(err)
	if !ok {
		return err
	}
	switch e.Status {
	case 404, 403:
		if e.Code == apperr.CodeOperationNotAllowed {
			return v3api.Forbidden(e.Message)
		}
		return v3api.Forbidden("")
	case 400:
		return v3api.BadRequest(e.Message, v3api.InvalidParam{Name: "body", Reason: e.Message})
	case 429:
		return v3api.TooManyRequests(e.Message, e.RetryAfter)
	}
	return err
}

// ListQuery is a parsed GET /api/v3/surveys query.
type ListQuery struct {
	WorkspaceID       uuid.UUID
	Limit             int
	Cursor            *Cursor
	IncludeTotalCount bool
	SortBy            string
	Filter            Filter
}

const (
	filterNameParam   = "filter[name][contains]"
	filterStatusParam = "filter[status][in]"
	filterTypeParam   = "filter[type][in]"
)

var listQueryParams = []string{"workspaceId", "limit", "cursor", "includeTotalCount", filterNameParam, filterStatusParam, filterTypeParam, "sortBy"}

// multiValue is forma2's collectMultiValueQueryParam: repeated keys and comma-separated values, trimmed and deduplicated.
func multiValue(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			t := strings.TrimSpace(part)
			if t != "" && !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

func enumReason(options []string) string {
	quoted := make([]string, len(options))
	for i, o := range options {
		quoted[i] = `"` + o + `"`
	}
	return "Invalid option: expected one of " + strings.Join(quoted, "|")
}

// ParseListQuery is forma2's parseV3SurveysListQuery.
func ParseListQuery(q map[string][]string) (*ListQuery, *v3api.Problem) {
	fail := func(params ...v3api.InvalidParam) (*ListQuery, *v3api.Problem) {
		return nil, v3api.BadRequest("Invalid query parameters", params...)
	}
	var unsupported []v3api.InvalidParam
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		known := false
		for _, p := range listQueryParams {
			if p == k {
				known = true
			}
		}
		if !known {
			unsupported = append(unsupported, v3api.InvalidParam{Name: k, Reason: "Unsupported query parameter. Use only " + strings.Join(listQueryParams, ", ") + "."})
		}
	}
	if len(unsupported) > 0 {
		return fail(unsupported...)
	}
	get := func(k string) (string, bool) {
		if v, ok := q[k]; ok && len(v) > 0 {
			return v[0], true
		}
		return "", false
	}
	out := &ListQuery{Limit: 20, IncludeTotalCount: true, SortBy: SortUpdatedAt}
	var params []v3api.InvalidParam
	if raw, ok := get("workspaceId"); !ok {
		params = append(params, v3api.InvalidParam{Name: "workspaceId", Reason: "Invalid input: expected string, received null"})
	} else if id, err := uuid.Parse(raw); err != nil {
		params = append(params, v3api.InvalidParam{Name: "workspaceId", Reason: "Invalid UUID"})
	} else {
		out.WorkspaceID = id
	}
	if raw, ok := get("limit"); ok {
		n, err := strconv.Atoi(strings.TrimSpace(raw))
		switch {
		case err != nil:
			params = append(params, v3api.InvalidParam{Name: "limit", Reason: "Invalid input: expected int, received number"})
		case n < 1:
			params = append(params, v3api.InvalidParam{Name: "limit", Reason: "Too small: expected number to be >=1"})
		case n > 250:
			params = append(params, v3api.InvalidParam{Name: "limit", Reason: "Too big: expected number to be <=250"})
		default:
			out.Limit = n
		}
	}
	if raw, ok := get("includeTotalCount"); ok && strings.TrimSpace(raw) != "" {
		switch strings.TrimSpace(raw) {
		case "true":
		case "false":
			out.IncludeTotalCount = false
		default:
			params = append(params, v3api.InvalidParam{Name: "includeTotalCount", Reason: enumReason([]string{"true", "false"})})
		}
	}
	if raw, ok := get(filterNameParam); ok {
		if len([]rune(raw)) > 512 {
			params = append(params, v3api.InvalidParam{Name: filterNameParam, Reason: "Too big: expected string to have <=512 characters"})
		} else {
			out.Filter.Name = strings.TrimSpace(raw)
		}
	}
	statusOptions := append(append([]string{}, Statuses...), "archived")
	for i, st := range multiValue(q[filterStatusParam]) {
		switch {
		case st == "archived":
			out.Filter.IncludeArchived = true
		case StatusIn(st):
			out.Filter.Statuses = append(out.Filter.Statuses, st)
		default:
			params = append(params, v3api.InvalidParam{Name: fmt.Sprintf("%s.%d", filterStatusParam, i), Reason: enumReason(statusOptions)})
		}
	}
	for i, t := range multiValue(q[filterTypeParam]) {
		if t != TypeLink && t != TypeApp {
			params = append(params, v3api.InvalidParam{Name: fmt.Sprintf("%s.%d", filterTypeParam, i), Reason: enumReason([]string{TypeLink, TypeApp})})
			continue
		}
		out.Filter.Types = append(out.Filter.Types, t)
	}
	if raw, ok := get("sortBy"); ok && strings.TrimSpace(raw) != "" {
		switch s := strings.TrimSpace(raw); s {
		case SortCreatedAt, SortUpdatedAt, SortName, SortRelevance:
			out.SortBy = s
		default:
			params = append(params, v3api.InvalidParam{Name: "sortBy", Reason: enumReason([]string{SortCreatedAt, SortUpdatedAt, SortName, SortRelevance})})
		}
	}
	if len(params) > 0 {
		return fail(params...)
	}
	if raw, ok := get("cursor"); ok && strings.TrimSpace(raw) != "" {
		c, err := DecodeCursor(strings.TrimSpace(raw), out.SortBy)
		if err != nil {
			e, _ := apperr.As(err)
			return fail(v3api.InvalidParam{Name: "cursor", Reason: e.Message})
		}
		out.Cursor = c
	}
	return out, nil
}

// ListResult is the data and meta of GET /api/v3/surveys.
type ListResult struct {
	Data []v3ListItem `json:"data"`
	Meta struct {
		Limit                int     `json:"limit"`
		NextCursor           *string `json:"nextCursor"`
		TotalCount           *int    `json:"totalCount"`
		WorkspaceSurveyCount *int    `json:"workspaceSurveyCount"`
	} `json:"meta"`
}

// ListV3 is forma2's listV3Surveys behind workspace.read.
func (s *Service) ListV3(ctx context.Context, caller Caller, q *ListQuery) (*ListResult, error) {
	if err := s.requireWorkspaceV3(ctx, caller, q.WorkspaceID, authz.WorkspaceRead); err != nil {
		return nil, err
	}
	page, err := s.repo.page(ctx, q.WorkspaceID, q.Filter, q.SortBy, q.Cursor, q.Limit)
	if err != nil {
		return nil, err
	}
	out := &ListResult{Data: v3ListItems(page.Surveys)}
	out.Meta.Limit, out.Meta.NextCursor = q.Limit, page.NextCursor
	if q.IncludeTotalCount {
		total, err := s.repo.count(ctx, q.WorkspaceID, q.Filter)
		if err != nil {
			return nil, err
		}
		all, err := s.repo.workspaceCount(ctx, q.WorkspaceID)
		if err != nil {
			return nil, err
		}
		out.Meta.TotalCount, out.Meta.WorkspaceSurveyCount = &total, &all
	}
	return out, nil
}

// GetV3 is forma2's getV3Survey behind workspace.read; lang narrows the translations.
func (s *Service) GetV3(ctx context.Context, caller Caller, id uuid.UUID, lang []string) (*v3Resource, error) {
	sv, err := s.authorizedV3(ctx, caller, id, authz.WorkspaceRead)
	if err != nil {
		return nil, err
	}
	r, langErr := toV3Resource(sv, lang)
	if langErr != nil {
		return nil, v3api.BadRequest(langErr.Message, v3api.InvalidParam{Name: "lang", Reason: langErr.Message, Identifier: langErr.Code})
	}
	return r, nil
}

// parseCreateBody is ZV3CreateSurveyBody: the strictness pre-check, then the normalized document schema. Its issues are reported as fallback-named invalid params.
func (s *Service) parseCreateBody(value any) (*v3Document, []surveydoc.Issue) {
	if params := unsupportedFields(value, createRootKeys, defaultV3Language, nil); len(params) > 0 {
		issues := make([]surveydoc.Issue, len(params))
		for i, p := range params {
			issues[i] = surveydoc.Issue{Path: splitName(p.Name), Message: p.Reason, Code: p.Code}
		}
		return nil, issues
	}
	o := asObject(value)
	if o == nil {
		return nil, []surveydoc.Issue{{Message: fmt.Sprintf("Invalid input: expected object, received %s", jsType(value))}}
	}
	o = o.clone()
	normalizeDocument(o, defaultV3Language, true, false, true, nil)
	p := &parser{docs: s.docs}
	d := p.parse(o, parseCreate, nil, "", "")
	if _, err := uuid.Parse(d.WorkspaceID); err != nil && d.WorkspaceID != "" {
		p.add([]any{"workspaceId"}, "Invalid UUID", "")
	}
	if len(p.issues) > 0 {
		return nil, p.issues
	}
	return d, nil
}

func splitName(name string) []any {
	if name == "" {
		return nil
	}
	parts := strings.Split(name, ".")
	out := make([]any, len(parts))
	for i, p := range parts {
		out[i] = p
	}
	return out
}

// languageRequest is forma2's TV3SurveyLanguageRequest.
type languageRequest struct {
	Code          string `json:"code"`
	Default       bool   `json:"default"`
	Enabled       bool   `json:"enabled"`
	WriteBehavior string `json:"writeBehavior,omitempty"`
}

// languageRequests is forma2's deriveV3SurveyLanguageRequests: the default language first, enabled, then the declared languages by code.
func languageRequests(d *v3Document) []languageRequest {
	byCode := map[string]languageRequest{}
	var order []string
	add := func(code string, enabled bool) {
		isDefault := strings.EqualFold(code, d.DefaultLanguage)
		if _, ok := byCode[code]; !ok {
			order = append(order, code)
		}
		byCode[code] = languageRequest{Code: code, Default: isDefault, Enabled: isDefault || enabled}
	}
	add(d.DefaultLanguage, true)
	for _, l := range d.Languages {
		add(l.Code, l.Enabled)
	}
	out := make([]languageRequest, 0, len(order))
	for _, c := range order {
		out = append(out, byCode[c])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Default != out[j].Default {
			return out[i].Default
		}
		return strings.ToLower(out[i].Code) < strings.ToLower(out[j].Code)
	})
	return out
}

// resolveTriggers is forma2's resolveV3SurveyTriggers: every id must name one of the workspace's action classes, once.
func (s *Service) resolveTriggers(ctx context.Context, workspaceID uuid.UUID, triggers []v3Trigger) ([]uuid.UUID, []v3api.InvalidParam, error) {
	if len(triggers) == 0 {
		return []uuid.UUID{}, nil, nil
	}
	classes, err := s.repo.actionClasses(ctx, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	known := map[string]uuid.UUID{}
	for _, c := range classes {
		known[c.ID.String()] = c.ID
	}
	var params []v3api.InvalidParam
	seen := map[string]bool{}
	var ids []uuid.UUID
	for i, t := range triggers {
		name := fmt.Sprintf("distribution.triggers.%d.actionClassId", i)
		if seen[t.ActionClassID] {
			params = append(params, v3api.InvalidParam{Name: name, Reason: fmt.Sprintf("Duplicate trigger action class id '%s'", t.ActionClassID), Code: v3api.CodeDuplicateIdentifier, Identifier: t.ActionClassID})
			continue
		}
		seen[t.ActionClassID] = true
		id, ok := known[strings.ToLower(t.ActionClassID)]
		if !ok {
			params = append(params, v3api.InvalidParam{Name: name, Reason: fmt.Sprintf("Action class '%s' was not found in this workspace", t.ActionClassID), Code: v3api.CodeInvalidReference, Identifier: t.ActionClassID})
			continue
		}
		ids = append(ids, id)
	}
	return ids, params, nil
}

// targetingParams is forma2's assertV3SurveyTargetingFilterReferences: attribute keys (and the userId person identifier), segments and surveys a filter names must exist in the workspace, and device filters must name a known device.
func (s *Service) targetingParams(ctx context.Context, workspaceID uuid.UUID, filters json.RawMessage) ([]v3api.InvalidParam, error) {
	type ref struct{ kind, value, path string }
	var refs []ref
	var walk func(nodes []any, base string)
	walk = func(nodes []any, base string) {
		for i, n := range nodes {
			rp := fmt.Sprintf("%s.%d.resource", base, i)
			node := asObject(n)
			resource, _ := node.get("resource")
			if nested, ok := resource.([]any); ok {
				walk(nested, rp)
				continue
			}
			res := asObject(resource)
			rootValue, _ := res.get("root")
			root := asObject(rootValue)
			kind, _ := root.get("type")
			str := func(o *object, key string) string {
				v, _ := o.get(key)
				s, _ := v.(string)
				return s
			}
			switch kind {
			case "attribute":
				refs = append(refs, ref{"attributeKey", str(root, "contactAttributeKey"), rp + ".root.contactAttributeKey"})
			case "person":
				id := str(root, "personIdentifier")
				k := "unsupportedPersonIdentifier"
				if id == "userId" {
					k = "attributeKey"
				}
				refs = append(refs, ref{k, id, rp + ".root.personIdentifier"})
			case "segment":
				refs = append(refs, ref{"segment", str(root, "segmentId"), rp + ".root.segmentId"})
			case "device":
				value, _ := res.get("value")
				if value != "phone" && value != "desktop" {
					text, isString := value.(string)
					if !isString {
						text = string(encodeValue(value))
					}
					refs = append(refs, ref{"invalidDeviceValue", text, rp + ".value"})
				}
				if d := str(root, "deviceType"); d != "phone" && d != "desktop" {
					refs = append(refs, ref{"invalidDeviceValue", d, rp + ".root.deviceType"})
				}
			case "surveyInteraction":
				valueRaw, _ := res.get("value")
				value := asObject(valueRaw)
				if scope, _ := value.get("surveyScope"); scope == "specific" {
					idsRaw, _ := value.get("surveyIds")
					ids, _ := idsRaw.([]any)
					for j, id := range ids {
						text, _ := id.(string)
						refs = append(refs, ref{"survey", text, fmt.Sprintf("%s.value.surveyIds.%d", rp, j)})
					}
				}
			}
		}
	}
	parsed, err := decodeValue(filters)
	if err != nil {
		return nil, fmt.Errorf("decode targeting filters: %w", err)
	}
	nodes, _ := parsed.([]any)
	walk(nodes, "targeting.filters")
	if len(refs) == 0 {
		return nil, nil
	}
	var params []v3api.InvalidParam
	invalid := func(r ref, reason string) {
		params = append(params, v3api.InvalidParam{Name: r.path, Reason: reason, Code: v3api.CodeInvalidReference, Identifier: r.value})
	}
	for _, r := range refs {
		if r.kind == "unsupportedPersonIdentifier" {
			invalid(r, fmt.Sprintf("Unsupported person identifier '%s'; only 'userId' is supported", r.value))
		}
	}
	for _, r := range refs {
		if r.kind == "invalidDeviceValue" {
			invalid(r, fmt.Sprintf("Unsupported device '%s'; expected one of: phone, desktop", r.value))
		}
	}
	lookup := func(kind, query string) (map[string]bool, error) {
		var values []string
		for _, r := range refs {
			if r.kind == kind {
				values = append(values, r.value)
			}
		}
		if len(values) == 0 {
			return nil, nil
		}
		var found []string
		if err := s.repo.db.WithContext(ctx).Raw(query, workspaceID, textArray(values)).Scan(&found).Error; err != nil {
			return nil, fmt.Errorf("resolve targeting references: %w", err)
		}
		out := map[string]bool{}
		for _, f := range found {
			out[f] = true
		}
		return out, nil
	}
	keys, err := lookup("attributeKey", `SELECT key FROM contact_attribute_keys WHERE workspace_id = ? AND key = ANY(?::text[])`)
	if err != nil {
		return nil, err
	}
	segments, err := lookup("segment", `SELECT id::text FROM segments WHERE workspace_id = ? AND id::text = ANY(?::text[])`)
	if err != nil {
		return nil, err
	}
	surveys, err := lookup("survey", `SELECT id::text FROM surveys WHERE workspace_id = ? AND id::text = ANY(?::text[])`)
	if err != nil {
		return nil, err
	}
	for _, check := range []struct {
		kind, format string
		found        map[string]bool
	}{
		{"attributeKey", "Contact attribute key '%s' was not found in this workspace", keys},
		{"segment", "Segment '%s' was not found in this workspace", segments},
		{"survey", "Survey '%s' was not found in this workspace", surveys},
	} {
		for _, r := range refs {
			if r.kind == check.kind && !check.found[r.value] {
				invalid(r, fmt.Sprintf(check.format, r.value))
			}
		}
	}
	return params, nil
}

// recordV3 emits the audit event of a v3 mutation.
func (s *Service) recordV3(ctx context.Context, caller Caller, action audit.Action, targetID string, workspaceID uuid.UUID, err error, oldObject, newObject any) {
	s.record(ctx, caller, action, audit.TargetSurvey, targetID, s.organizationOf(ctx, workspaceID), err, oldObject, newObject)
}

// CreateV3 is forma2's POST /api/v3/surveys behind workspace.write: the body is parsed first (400), then the document is checked against its own references and the workspace (422), then forma2's createSurvey runs.
func (s *Service) CreateV3(ctx context.Context, caller Caller, body json.RawMessage, createdFrom string) (*v3Resource, error) {
	value, err := decodeValue(body)
	if err != nil {
		return nil, v3api.BadRequest("Invalid request body", v3api.InvalidParam{Name: "body", Reason: "Malformed JSON input, please check your request body"})
	}
	d, issues := s.parseCreateBody(value)
	if len(issues) > 0 {
		return nil, v3api.BadRequest("Invalid request body", formatIssues(issues, "body")...)
	}
	workspaceID := uuid.MustParse(d.WorkspaceID)
	if err := s.requireWorkspaceV3(ctx, caller, workspaceID, authz.WorkspaceWrite); err != nil {
		return nil, err
	}
	return s.createFromDocument(ctx, caller, workspaceID, d, createdFrom)
}

// formatIssues is the API wrapper's formatZodIssues: the path or the fallback, the message as it is, and the v3 code when the issue carries one.
func formatIssues(issues []surveydoc.Issue, fallback string) []v3api.InvalidParam {
	out := make([]v3api.InvalidParam, len(issues))
	for i, is := range issues {
		name := is.Name()
		if name == "" {
			name = fallback
		}
		out[i] = v3api.InvalidParam{Name: name, Reason: is.Message, Code: is.Code}
	}
	return out
}

// createFromDocument creates the survey; createdFrom is the editor's blank, template, xm-template or ai, empty for an API caller.
func (s *Service) createFromDocument(ctx context.Context, caller Caller, workspaceID uuid.UUID, d *v3Document, createdFrom string) (*v3Resource, error) {
	sv, err := s.createV3(ctx, caller, workspaceID, d)
	if err != nil {
		s.recordV3(ctx, caller, audit.ActionCreated, "", workspaceID, err, nil, nil)
		return nil, err
	}
	r, _ := toV3Resource(sv, nil)
	s.recordV3(ctx, caller, audit.ActionCreated, sv.ID.String(), workspaceID, nil, nil, r)
	s.captureCreated(ctx, caller, sv, createdFrom)
	return r, nil
}

func (s *Service) createV3(ctx context.Context, caller Caller, workspaceID uuid.UUID, d *v3Document) (*Survey, error) {
	params, typed, err := validateDocument(d)
	if err != nil {
		return nil, err
	}
	if len(params) > 0 {
		return nil, v3api.Unprocessable(failedValidation, params...)
	}
	var triggerIDs []uuid.UUID
	if d.Type == TypeApp {
		var tps []v3api.InvalidParam
		triggers := []v3Trigger{}
		if d.Distribution != nil {
			triggers = d.Distribution.Triggers
		}
		if triggerIDs, tps, err = s.resolveTriggers(ctx, workspaceID, triggers); err != nil {
			return nil, err
		}
		if len(tps) > 0 {
			return nil, v3api.Unprocessable(failedValidation, tps...)
		}
		if d.Targeting != nil {
			tps, err := s.targetingParams(ctx, workspaceID, d.Targeting.Filters)
			if err != nil {
				return nil, err
			}
			if len(tps) > 0 {
				return nil, v3api.Unprocessable(failedValidation, tps...)
			}
		}
	}
	requests := languageRequests(d)
	codes := make([]string, len(requests))
	docLangs := make([]surveydoc.Language, len(requests))
	for i, r := range requests {
		codes[i] = r.Code
		docLangs[i] = surveydoc.Language{Code: r.Code, Default: r.Default, Enabled: r.Enabled}
	}
	// The survey service's own write schema, forma2's ZSurveyCreateInput: the cross-field refinement over the stored form.
	typed.Languages = docLangs
	if issues := surveydoc.Refine(typed); len(issues) > 0 {
		return nil, v3api.Unprocessable(failedValidation, invalidParams(issues, "body")...)
	}
	existing := surveydoc.DeclaredFields{}
	incoming := surveydoc.DeclaredFields{Variables: &typed.Variables, HiddenFields: &typed.HiddenFields}
	if err := guardDeclaredFields(existing, incoming); err != nil {
		return nil, v3Problem(err)
	}
	if appSurveyMissingTriggers(d.Type, d.Status, len(triggerIDs)) {
		return nil, v3api.BadRequest(appTriggerMessage, v3api.InvalidParam{Name: "body", Reason: appTriggerMessage})
	}
	org, err := s.repo.organizationOf(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, v3api.Forbidden("")
	}
	languages, err := s.repo.ensureLanguages(ctx, workspaceID, codes)
	if err != nil {
		return nil, err
	}
	links := make([]languageLink, 0, len(requests))
	for _, r := range requests {
		links = append(links, languageLink{LanguageID: languages[strings.ToLower(r.Code)].ID, Default: r.Default, Enabled: r.Enabled})
	}
	rec := defaultRecord()
	rec.Name, rec.Type, rec.Status, rec.CreatedBy = d.Name, d.Type, d.Status, caller.userID()
	rec.Metadata, rec.WelcomeCard, rec.Blocks = encodeValue(d.Metadata), encodeValue(d.WelcomeCard), stripIsDraft(encodeValue(d.Blocks))
	rec.Endings, rec.HiddenFields, rec.Variables = encodeValue(d.Endings), encodeValue(d.HiddenFields), encodeValue(d.Variables)
	if d.Type == TypeApp && d.Distribution != nil {
		applyDistribution(&rec, d.Distribution)
	}
	// A v3 create carries no dates, so the organization's zone does not matter here.
	if rec.PublishOn, rec.CloseOn, err = normalizeDates(s.schedule, s.schedule, nil, rec.Status, nil, nil); err != nil {
		return nil, err
	}
	filters := json.RawMessage(`[]`)
	if d.Targeting != nil {
		filters = d.Targeting.Filters
	}
	sv, err := s.insertSurvey(ctx, newSurvey{WorkspaceID: workspaceID, Record: rec, Languages: links, TriggerIDs: triggerIDs, PrivateSegmentFilters: filters}, *org)
	if err != nil {
		return nil, v3Problem(err)
	}
	return sv, nil
}

// applyDistribution writes an app survey's display settings, forma2's v3DistributionToScalars.
func applyDistribution(rec *record, d *v3Distribution) {
	rec.DisplayOption, rec.DisplayPercentage, rec.DisplayLimit = d.DisplayOption, d.DisplayPercentage, d.DisplayLimit
	rec.RecontactDays, rec.AutoClose, rec.AutoComplete, rec.Delay = d.RecontactDays, d.AutoClose, d.AutoComplete, d.Delay
}
