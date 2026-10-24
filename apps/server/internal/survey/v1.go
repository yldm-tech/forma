package survey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// The surveys of forma2's v1 management API (apps/web/app/api/v1/management/surveys): its survey object, which still carries the legacy `questions` beside `blocks`, and its create, update and delete. The API module authorizes the API key before it calls these, with v1's statuses; they re-check through authz and record the audit events themselves only where noted.

// V1Invalid is a v1 body that breaks forma2's input schema: 400 "Fields are missing or incorrectly formatted" with each issue's path and message as details.
type V1Invalid struct {
	Issues []surveydoc.Issue
}

func (e *V1Invalid) Error() string { return "invalid v1 survey input: " + e.Issues[0].Message }

// Load is a survey with its relations, nil when it does not exist. It authorizes nothing; the caller decides who may see it.
func (s *Service) Load(ctx context.Context, id uuid.UUID) (*Survey, error) {
	return s.repo.byID(ctx, id)
}

// ListV1 is the v1 API's getSurveys: the surveys of the workspaces, most recently updated first, limit of them after offset. It authorizes nothing: the API key's grants name the workspaces.
func (s *Service) ListV1(ctx context.Context, workspaceIDs []uuid.UUID, limit, offset int) ([]*Survey, error) {
	if len(workspaceIDs) == 0 {
		return []*Survey{}, nil
	}
	var ids []uuid.UUID
	if err := s.repo.db.WithContext(ctx).Raw(`SELECT id FROM surveys WHERE workspace_id = ANY(?::uuid[]) ORDER BY updated_at DESC, id DESC LIMIT ? OFFSET ?`,
		uuidArray(workspaceIDs), limit, offset).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("list surveys: %w", err)
	}
	out := make([]*Survey, 0, len(ids))
	for _, id := range ids {
		sv, err := s.repo.byID(ctx, id)
		if err != nil {
			return nil, err
		}
		if sv != nil {
			out = append(out, sv)
		}
	}
	return out, nil
}

// V1Object is forma2's v1 survey: the survey with `questions` derived from its blocks (withDerivedQuestions) and `projectOverwrites` mirroring `workspaceOverwrites`, without the fields v1 never described. environmentId is added by the API, which knows the workspace's legacy id.
func V1Object(sv *Survey) (map[string]any, error) {
	obj, err := toMap(sv)
	if err != nil {
		return nil, err
	}
	delete(obj, "inlineTriggers")
	// v1 never described the inlined embedded-data fields the admin app reads.
	delete(obj, "embeddedFields")
	questions, err := blocksToQuestions(sv.Blocks, sv.Endings)
	if err != nil {
		return nil, err
	}
	obj["questions"] = questions
	obj["projectOverwrites"] = obj["workspaceOverwrites"]
	return obj, nil
}

// V1DeletedObject is the survey a v1 delete answers with: forma2's raw row with its segment and triggers, before withDerivedQuestions, so `questions` is the stored legacy column, which the rewrite no longer keeps and is always empty.
func V1DeletedObject(sv *Survey) (map[string]any, error) {
	obj, err := toMap(sv)
	if err != nil {
		return nil, err
	}
	delete(obj, "languages")
	delete(obj, "followUps")
	delete(obj, "embeddedFields")
	obj["questions"] = []any{}
	var segmentID any
	if sv.Segment != nil {
		segmentID = sv.Segment.ID.String()
	}
	obj["segmentId"] = segmentID
	return obj, nil
}

func toMap(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode survey: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("decode survey: %w", err)
	}
	return out, nil
}

// ---- input ----

// v1Kind is what a top-level member of the v1 survey input must be, after zod's shape of ZSurveyBase.
type v1Kind int

const (
	kindString v1Kind = iota
	kindNumber
	kindBool
	kindObject
	kindArray
	kindDate
)

type v1Field struct {
	name     string
	kind     v1Kind
	nullable bool
	// options is an enum's values.
	options []string
}

// v1Fields are ZSurveyBase's members the v1 inputs read, in its declared order, which is the order zod reports them in. Every member is optional on create except name; the document parts are checked against their own schemas afterwards.
var v1Fields = []v1Field{
	{name: "name", kind: kindString},
	{name: "redirectUrl", kind: kindString, nullable: true},
	{name: "type", kind: kindString, options: []string{TypeLink, TypeApp}},
	{name: "status", kind: kindString, options: Statuses},
	{name: "displayOption", kind: kindString, options: DisplayOptions},
	{name: "autoClose", kind: kindNumber, nullable: true},
	{name: "triggers", kind: kindArray},
	{name: "recontactDays", kind: kindNumber, nullable: true},
	{name: "displayLimit", kind: kindNumber, nullable: true},
	{name: "welcomeCard", kind: kindObject},
	{name: "questions", kind: kindArray},
	{name: "blocks", kind: kindArray},
	{name: "endings", kind: kindArray},
	{name: "hiddenFields", kind: kindObject},
	{name: "variables", kind: kindArray},
	{name: "followUps", kind: kindArray},
	{name: "delay", kind: kindNumber},
	{name: "publishOn", kind: kindDate, nullable: true},
	{name: "closeOn", kind: kindDate, nullable: true},
	{name: "autoComplete", kind: kindNumber, nullable: true},
	{name: "workspaceOverwrites", kind: kindObject, nullable: true},
	{name: "styling", kind: kindObject, nullable: true},
	{name: "showLanguageSwitch", kind: kindBool, nullable: true},
	{name: "surveyClosedMessage", kind: kindObject, nullable: true},
	{name: "segment", kind: kindObject, nullable: true},
	{name: "singleUse", kind: kindObject, nullable: true},
	{name: "isVerifyEmailEnabled", kind: kindBool},
	{name: "recaptcha", kind: kindObject, nullable: true},
	{name: "isBackButtonHidden", kind: kindBool},
	{name: "isAutoProgressingEnabled", kind: kindBool},
	{name: "isCaptureIpEnabled", kind: kindBool},
	{name: "isAnonymizeResponsesEnabled", kind: kindBool},
	{name: "pin", kind: kindString, nullable: true},
	{name: "displayPercentage", kind: kindNumber, nullable: true},
	{name: "languages", kind: kindArray},
	{name: "metadata", kind: kindObject},
	{name: "slug", kind: kindString, nullable: true},
	{name: "customHeadScripts", kind: kindString, nullable: true},
	{name: "customHeadScriptsMode", kind: kindString, nullable: true, options: []string{"add", "replace"}},
}

// v1JSType is the type name zod's messages report for a JSON value.
func v1JSType(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case float64:
		if math.IsNaN(x) {
			return "NaN"
		}
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	}
	return "object"
}

func quoted(values []string) string {
	q := make([]string, len(values))
	for i, v := range values {
		q[i] = `"` + v + `"`
	}
	return strings.Join(q, "|")
}

// typeIssues checks the members of a v1 survey input against v1Fields, zod's messages for a member of the wrong type or outside its enum. A member that is absent is left to required.
func typeIssues(body map[string]any, required ...string) []surveydoc.Issue {
	var out []surveydoc.Issue
	add := func(name, message string) {
		out = append(out, surveydoc.Issue{Path: []any{name}, Message: message})
	}
	for _, f := range v1Fields {
		v, present := body[f.name]
		if !present {
			if contains(required, f.name) {
				add(f.name, "Invalid input: expected "+kindName(f.kind)+", received undefined")
			}
			continue
		}
		if v == nil {
			if !f.nullable {
				add(f.name, "Invalid input: expected "+kindName(f.kind)+", received null")
			}
			continue
		}
		if f.options != nil {
			if s, ok := v.(string); !ok || !contains(f.options, s) {
				add(f.name, "Invalid option: expected one of "+quoted(f.options))
			}
			continue
		}
		ok := true
		switch f.kind {
		case kindString:
			_, ok = v.(string)
		case kindNumber:
			_, ok = v.(float64)
		case kindBool:
			_, ok = v.(bool)
		case kindObject:
			_, ok = v.(map[string]any)
		case kindArray:
			_, ok = v.([]any)
		case kindDate:
			if s, isString := v.(string); isString {
				if _, err := time.Parse(time.RFC3339Nano, s); err != nil {
					add(f.name, "Invalid input: expected date, received Date")
				}
				continue
			}
			ok = false
		}
		if !ok {
			add(f.name, "Invalid input: expected "+kindName(f.kind)+", received "+v1JSType(v))
		}
	}
	return out
}

func kindName(k v1Kind) string {
	switch k {
	case kindNumber:
		return "number"
	case kindBool:
		return "boolean"
	case kindObject:
		return "object"
	case kindArray:
		return "array"
	case kindDate:
		return "date"
	}
	return "string"
}

func contains(values []string, v string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}

// nonEmptyArray reports whether a member is an array with at least one entry.
func nonEmptyArray(v any) bool {
	a, ok := v.([]any)
	return ok && len(a) > 0
}

// v1Defaults fills what zod's parse output carried for the document parts a v1 input writes: the prefaults of the welcome card, the elements (and the legacy questions they came from), validation and variables.
func v1Defaults(body map[string]any) {
	if wc, ok := body["welcomeCard"].(map[string]any); ok {
		setMissing(wc, "timeToFinish", true)
		setMissing(wc, "showResponseCount", false)
	}
	elementDefaults := func(el map[string]any) {
		switch el["type"] {
		case "openText":
			setMissing(el, "inputType", "text")
			setMissing(el, "insightsEnabled", false)
			setMissing(el, "charLimit", map[string]any{"enabled": false})
		case "nps", "rating", "csat", "ces":
			setMissing(el, "isColorCodingEnabled", false)
		case "cta":
			setMissing(el, "buttonExternal", false)
		case "pictureSelection":
			setMissing(el, "allowMulti", false)
		case "matrix":
			setMissing(el, "shuffleOption", "none")
		}
		if v, ok := el["validation"].(map[string]any); ok {
			setMissing(v, "logic", "and")
		}
	}
	if blocks, ok := body["blocks"].([]any); ok {
		for _, b := range blocks {
			block, _ := b.(map[string]any)
			elements, _ := block["elements"].([]any)
			for _, e := range elements {
				if el, ok := e.(map[string]any); ok {
					elementDefaults(el)
				}
			}
		}
	}
	if questions, ok := body["questions"].([]any); ok {
		for _, q := range questions {
			if el, ok := q.(map[string]any); ok {
				elementDefaults(el)
			}
		}
	}
	if vars, ok := body["variables"].([]any); ok {
		for _, v := range vars {
			vo, _ := v.(map[string]any)
			switch vo["type"] {
			case "number":
				setMissing(vo, "value", float64(0))
			case "text":
				setMissing(vo, "value", "")
			}
		}
	}
}

func setMissing(o map[string]any, key string, v any) {
	if o == nil {
		return
	}
	if _, ok := o[key]; !ok {
		o[key] = v
	}
}

func raw(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	return encodeValue(v)
}

func optString(v any) *string {
	if s, ok := v.(string); ok {
		return &s
	}
	return nil
}

func optInt(v any) *int {
	if f, ok := v.(float64); ok {
		n := int(f)
		return &n
	}
	return nil
}

func optFloat(v any) *float64 {
	if f, ok := v.(float64); ok {
		return &f
	}
	return nil
}

func optBool(v any) *bool {
	if b, ok := v.(bool); ok {
		return &b
	}
	return nil
}

func optTime(v any) *time.Time {
	if s, ok := v.(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return &t
		}
	}
	return nil
}

func docOf(body map[string]any) surveydoc.Document {
	return surveydoc.Document{
		WelcomeCard: raw(body["welcomeCard"]), Blocks: raw(body["blocks"]), Endings: raw(body["endings"]),
		HiddenFields: raw(body["hiddenFields"]), Variables: raw(body["variables"]), Metadata: raw(body["metadata"]),
		Styling: raw(body["styling"]), SurveyClosedMessage: raw(body["surveyClosedMessage"]), SingleUse: raw(body["singleUse"]),
		Recaptcha: raw(body["recaptcha"]), WorkspaceOverwrites: raw(body["workspaceOverwrites"]),
	}
}

// refIDs reads the ids of a list of {key: {id}} objects (triggers' action classes, languages' languages).
func refIDs(v any, list, key string) ([]uuid.UUID, []surveydoc.Issue) {
	entries, _ := v.([]any)
	var ids []uuid.UUID
	var issues []surveydoc.Issue
	for i, e := range entries {
		obj, _ := e.(map[string]any)
		ref, _ := obj[key].(map[string]any)
		s, _ := ref["id"].(string)
		id, err := uuid.Parse(s)
		if err != nil {
			issues = append(issues, surveydoc.Issue{Path: []any{list, i, key, "id"}, Message: "Invalid input: expected string, received " + v1JSType(ref["id"])})
			continue
		}
		ids = append(ids, id)
	}
	return ids, issues
}

func recaptchaEnabled(v any) bool {
	r, _ := v.(map[string]any)
	enabled, _ := r["enabled"].(bool)
	return enabled
}

// CreateV1 is forma2's POST /api/v1/management/surveys after the workspace was resolved and authorized: the body checked against ZSurveyCreateInputWithWorkspaceId (questions or blocks, not both), legacy questions converted into one block each, a reCAPTCHA setting refused when the instance has no keys, then createSurvey: the survey, its triggers, languages and follow-ups, and an app survey's private segment. The caller is the API key; the survey has no creator.
func (s *Service) CreateV1(ctx context.Context, caller Caller, workspaceID uuid.UUID, body map[string]any) (*Survey, error) {
	// The create schema omits these, so zod stripped them: a create sets neither overwrites nor the archive stamp.
	delete(body, "workspaceOverwrites")
	delete(body, "archivedAt")
	issues := typeIssues(body, "name")
	if len(issues) > 0 {
		return nil, &V1Invalid{Issues: issues}
	}
	v1Defaults(body)
	hasQuestions, hasBlocks := nonEmptyArray(body["questions"]), nonEmptyArray(body["blocks"])
	switch {
	case hasQuestions && hasBlocks:
		return nil, &V1Invalid{Issues: []surveydoc.Issue{
			{Path: []any{"blocks"}, Message: "Survey cannot have both questions and blocks. Use one model."},
			{Path: []any{"questions"}, Message: "Cannot provide both questions and blocks. Please provide only one of these fields."},
		}}
	case !hasQuestions && !hasBlocks:
		return nil, &V1Invalid{Issues: []surveydoc.Issue{
			{Path: []any{"questions"}, Message: "Survey must have either questions or blocks with elements"},
			{Path: []any{"questions"}, Message: "Must provide either questions or blocks. Both cannot be empty."},
		}}
	}
	if hasQuestions {
		body["blocks"] = QuestionsToBlocks(body["questions"].([]any), body["endings"])
	}
	def := defaultRecord()
	for key, v := range map[string]json.RawMessage{
		"welcomeCard": def.WelcomeCard, "endings": def.Endings, "hiddenFields": def.HiddenFields, "variables": def.Variables, "metadata": def.Metadata,
		"singleUse": def.SingleUse, "recaptcha": def.Recaptcha,
	} {
		if _, present := body[key]; !present {
			var decoded any
			_ = json.Unmarshal(v, &decoded)
			body[key] = decoded
		}
	}
	v1Defaults(body)

	langIDs, langIssues := refIDs(body["languages"], "languages", "language")
	triggerIDs, triggerIssues := refIDs(body["triggers"], "triggers", "actionClass")
	if len(langIssues)+len(triggerIssues) > 0 {
		return nil, &V1Invalid{Issues: append(langIssues, triggerIssues...)}
	}
	languages, err := s.checkLanguages(ctx, workspaceID, langIDs)
	if err != nil {
		return nil, err
	}
	docLangs := make([]surveydoc.Language, 0, len(langIDs))
	links := make([]languageLink, 0, len(langIDs))
	langList, _ := body["languages"].([]any)
	for _, e := range langList {
		obj := asMap(e)
		id, _ := uuid.Parse(fmt.Sprint(asMap(obj["language"])["id"]))
		isDefault, _ := obj["default"].(bool)
		enabled, _ := obj["enabled"].(bool)
		docLangs = append(docLangs, surveydoc.Language{Code: languages[id].Code, Default: isDefault, Enabled: enabled})
		links = append(links, languageLink{LanguageID: id, Default: isDefault, Enabled: enabled})
	}
	followUps, err := v1FollowUps(body["followUps"])
	if err != nil {
		return nil, err
	}
	docIssues, parsed, err := s.docs.Check(docOf(body), docLangs, followUps)
	if err != nil {
		return nil, err
	}
	surveyType, _ := body["type"].(string)
	if surveyType == "" {
		surveyType = TypeLink
	}
	status, _ := body["status"].(string)
	if status == "" {
		status = StatusDraft
	}
	displayOption, _ := body["displayOption"].(string)
	if displayOption == "" {
		displayOption = def.DisplayOption
	}
	delay := 0
	if d := optInt(body["delay"]); d != nil {
		delay = *d
	}
	docIssues = append(docIssues, scalarIssues(surveyType, status, displayOption, optInt(body["autoComplete"]), optInt(body["recontactDays"]), optInt(body["displayLimit"]), &delay,
		optFloat(body["displayPercentage"]), optString(body["pin"]), optString(body["customHeadScriptsMode"]))...)
	if len(docIssues) > 0 {
		return nil, &V1Invalid{Issues: docIssues}
	}
	if recaptchaEnabled(body["recaptcha"]) && !s.cfg.RecaptchaConfigured() {
		return nil, apperr.OperationNotAllowed("Spam protection is not enabled for this organization")
	}

	org, err := s.repo.organizationOf(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, apperr.NotFound("Organization", "")
	}
	incoming := surveydoc.DeclaredFields{Variables: &parsed.Variables, HiddenFields: &parsed.HiddenFields}
	if err := guardDeclaredFields(surveydoc.DeclaredFields{}, incoming); err != nil {
		return nil, err
	}
	if appSurveyMissingTriggers(surveyType, status, len(triggerIDs)) {
		return nil, apperr.InvalidInput(apperr.CodeBadRequest, appTriggerMessage)
	}
	if err := s.validateTriggers(ctx, workspaceID, triggerIDs); err != nil {
		return nil, err
	}
	if m := surveydoc.MediaIssue(parsed.Blocks); m != "" {
		return nil, apperr.InvalidInput(apperr.CodeBadRequest, m)
	}
	var segmentID *uuid.UUID
	if seg, ok := body["segment"].(map[string]any); ok && surveyType != TypeApp {
		id, _ := uuid.Parse(fmt.Sprint(seg["id"]))
		found, err := s.repo.segmentInWorkspace(ctx, id, workspaceID)
		if err != nil {
			return nil, err
		}
		if found == nil {
			return nil, apperr.NotFound("Segment", fmt.Sprint(seg["id"]))
		}
		segmentID = &id
	}
	writes, err := followUpWrites(followUps)
	if err != nil {
		return nil, err
	}

	rec := def
	rec.Name, rec.Type, rec.Status, rec.RedirectURL, rec.CreatedBy = body["name"].(string), surveyType, status, optString(body["redirectUrl"]), caller.userID()
	rec.WelcomeCard, rec.Blocks, rec.Endings = raw(body["welcomeCard"]), stripIsDraft(raw(body["blocks"])), raw(body["endings"])
	rec.HiddenFields, rec.Variables, rec.Metadata = raw(body["hiddenFields"]), raw(body["variables"]), raw(body["metadata"])
	rec.SurveyClosedMessage, rec.Styling, rec.SingleUse, rec.Recaptcha = raw(body["surveyClosedMessage"]), raw(body["styling"]), raw(body["singleUse"]), raw(body["recaptcha"])
	rec.DisplayOption, rec.AutoClose, rec.RecontactDays, rec.DisplayLimit = displayOption, optInt(body["autoClose"]), optInt(body["recontactDays"]), optInt(body["displayLimit"])
	rec.AutoComplete, rec.DisplayPercentage, rec.Delay = optInt(body["autoComplete"]), optFloat(body["displayPercentage"]), delay
	for _, f := range []struct {
		dst *bool
		key string
	}{
		{&rec.IsVerifyEmailEnabled, "isVerifyEmailEnabled"}, {&rec.IsBackButtonHidden, "isBackButtonHidden"}, {&rec.IsAutoProgressingEnabled, "isAutoProgressingEnabled"},
		{&rec.IsCaptureIPEnabled, "isCaptureIpEnabled"}, {&rec.IsAnonymizeResponsesEnabled, "isAnonymizeResponsesEnabled"},
	} {
		if b := optBool(body[f.key]); b != nil {
			*f.dst = *b
		}
	}
	rec.ShowLanguageSwitch, rec.Pin, rec.Slug, rec.CustomHeadScripts = optBool(body["showLanguageSwitch"]), optString(body["pin"]), optString(body["slug"]), optString(body["customHeadScripts"])
	if mode := optString(body["customHeadScriptsMode"]); mode != nil {
		rec.CustomHeadScriptsMode = mode
	}
	schedule, err := s.scheduleOf(ctx, s.repo, workspaceID)
	if err != nil {
		return nil, err
	}
	if rec.PublishOn, rec.CloseOn, err = normalizeDates(schedule, schedule, nil, rec.Status, optTime(body["publishOn"]), optTime(body["closeOn"])); err != nil {
		return nil, err
	}
	return s.insertSurvey(ctx, newSurvey{WorkspaceID: workspaceID, Record: rec, Languages: links, TriggerIDs: triggerIDs, FollowUps: writes, SegmentID: segmentID, PrivateSegmentFilters: json.RawMessage(`[]`), SnappedIn: &schedule}, *org)
}

// v1FollowUps reads the follow-ups of a v1 input; anything that is not a list of objects is no follow-ups, which the type check already reported.
func v1FollowUps(v any) ([]surveydoc.FollowUpInput, error) {
	list, _ := v.([]any)
	out := make([]surveydoc.FollowUpInput, 0, len(list))
	for _, e := range list {
		var f surveydoc.FollowUpInput
		if err := json.Unmarshal(encodeValue(e), &f); err != nil {
			return nil, &V1Invalid{Issues: []surveydoc.Issue{{Path: []any{"followUps"}, Message: "Invalid input"}}}
		}
		out = append(out, f)
	}
	return out, nil
}

// UpdateV1 is forma2's PUT /api/v1/management/surveys/{id} after the survey was found and authorized: the body may carry legacy questions or blocks, not both; questions become blocks; the stored survey with the body's members over it is checked against ZSurveyUpdateInput and saved as the editor saves it, with the reCAPTCHA check and the slug. Like forma2's updateSurvey it never changes the redirect URL. The API records the audit event.
func (s *Service) UpdateV1(ctx context.Context, caller Caller, current *Survey, body map[string]any) (*Survey, error) {
	if nonEmptyArray(body["questions"]) && nonEmptyArray(body["blocks"]) {
		return nil, apperr.InvalidInput(apperr.CodeBadRequest, "Cannot provide both questions and blocks. Please provide only one of these fields.")
	}
	merged, err := toMap(current)
	if err != nil {
		return nil, err
	}
	merged["questions"] = []any{}
	for k, v := range body {
		merged[k] = v
	}
	if questions, ok := body["questions"].([]any); ok && len(questions) > 0 {
		v1Defaults(merged)
		endings := merged["endings"]
		merged["blocks"] = QuestionsToBlocks(questions, endings)
		merged["questions"] = []any{}
	}
	issues := typeIssues(merged)
	if len(issues) > 0 {
		return nil, &V1Invalid{Issues: issues}
	}
	v1Defaults(merged)
	var in UpdateInput
	if err := json.Unmarshal(encodeValue(merged), &in); err != nil {
		return nil, &V1Invalid{Issues: []surveydoc.Issue{{Path: []any{}, Message: "Invalid input"}}}
	}
	in.ID = current.ID
	// The editor's own field; a v1 update's dates are read in the organization's zone, as forma2 read them in the instance's.
	in.SchedulingTimeZone = nil
	if recaptchaEnabled(merged["recaptcha"]) && !s.cfg.RecaptchaConfigured() {
		return nil, apperr.OperationNotAllowed("Spam protection is not enabled for this organization")
	}
	if err := s.authz.Require(ctx, caller.Actor, authz.SurveyWrite, authz.Survey(current.ID)); err != nil {
		return nil, err
	}
	updated, err := s.update(ctx, current, &in, false)
	if err == nil {
		updated, err = s.v1Slug(ctx, updated, merged)
	}
	if e, ok := apperr.As(err); ok && e.Code == apperr.CodeValidation {
		if list, ok := e.Details["issues"].([]surveydoc.Issue); ok {
			return nil, &V1Invalid{Issues: list}
		}
	}
	return updated, err
}

// v1Slug writes the slug a v1 update carried, which the editor's save leaves alone.
func (s *Service) v1Slug(ctx context.Context, sv *Survey, merged map[string]any) (*Survey, error) {
	slug, present := merged["slug"]
	if !present {
		return sv, nil
	}
	next := optString(slug)
	if (next == nil) == (sv.Slug == nil) && (next == nil || *next == *sv.Slug) {
		return sv, nil
	}
	if next != nil && !slugPattern.MatchString(*next) {
		return nil, &V1Invalid{Issues: []surveydoc.Issue{{Path: []any{"slug"}, Message: "Slug can only contain lowercase letters, numbers, and hyphens"}}}
	}
	if _, err := s.repo.setSlug(ctx, sv.ID, next); err != nil {
		if errors.Is(err, errSlugTaken) {
			return nil, apperr.InvalidInput(apperr.CodeConflict, "A survey with this slug already exists")
		}
		return nil, err
	}
	return s.repo.byID(ctx, sv.ID)
}

// DeleteV1 is forma2's v1 deleteSurvey: the survey and an app survey's private segment are deleted outright. It returns the survey as it was, nil when it was already gone. The caller authorized workspace.manage; the audit event is the API's.
func (s *Service) DeleteV1(ctx context.Context, id uuid.UUID) (*Survey, error) {
	return s.repo.deleteSurvey(ctx, id, nil)
}

// ---- legacy questions ----

// QuestionsToBlocks is forma2's transformQuestionsToBlocks: every legacy question becomes a block of its own named "Block <n>", the question's logic, fallback and button labels moving onto the block; CTA questions are migrated to the element shape, and logic that jumps to a question jumps to its block.
func QuestionsToBlocks(questions []any, endings any) []any {
	if len(questions) == 0 {
		return []any{}
	}
	copied := make([]map[string]any, 0, len(questions))
	for _, q := range questions {
		var c map[string]any
		_ = json.Unmarshal(encodeValue(q), &c)
		if c == nil {
			c = map[string]any{}
		}
		copied = append(copied, c)
	}
	processCTAQuestions(copied)
	endingIDs := map[string]bool{}
	if list, ok := endings.([]any); ok {
		for _, e := range list {
			if o, ok := e.(map[string]any); ok {
				if id, ok := o["id"].(string); ok {
					endingIDs[id] = true
				}
			}
		}
	}
	blockOf := map[string]string{}
	blocks := make([]map[string]any, 0, len(copied))
	for i, q := range copied {
		blockID := surveydoc.NewID()
		if id, ok := q["id"].(string); ok {
			blockOf[id] = blockID
		}
		block := map[string]any{"id": blockID, "name": fmt.Sprintf("Block %d", i+1)}
		for _, key := range []string{"logic", "logicFallback", "buttonLabel", "backButtonLabel"} {
			if v, ok := q[key]; ok {
				block[key] = v
				delete(q, key)
			}
		}
		block["elements"] = []any{q}
		blocks = append(blocks, block)
	}
	out := make([]any, len(blocks))
	for i, block := range blocks {
		if logic, ok := block["logic"].([]any); ok && len(logic) > 0 {
			for j, item := range logic {
				o, ok := item.(map[string]any)
				if !ok {
					continue
				}
				conditions := renameOperandTypes(o["conditions"], "question", "element")
				if !isConditionGroup(conditions) {
					continue
				}
				updated := cloneMap(o)
				updated["conditions"] = conditions
				updated["actions"] = retargetJumps(o["actions"], "jumpToQuestion", "jumpToBlock", blockOf)
				logic[j] = updated
			}
		}
		if fallback, ok := block["logicFallback"].(string); ok {
			switch {
			case blockOf[fallback] != "":
				block["logicFallback"] = blockOf[fallback]
			case endingIDs[fallback]:
			default:
				delete(block, "logicFallback")
			}
		}
		out[i] = block
	}
	return out
}

// blocksToQuestions is forma2's transformBlocksToQuestions: each block's elements in order, the block's logic, fallback and labels moved onto its last element, jumps to a block becoming jumps to its first element. A survey without blocks has no questions.
func blocksToQuestions(rawBlocks, rawEndings json.RawMessage) ([]any, error) {
	var blocks []map[string]any
	if len(rawBlocks) > 0 && string(rawBlocks) != "null" {
		if err := json.Unmarshal(rawBlocks, &blocks); err != nil {
			return nil, fmt.Errorf("decode survey blocks: %w", err)
		}
	}
	questions := []any{}
	if len(blocks) == 0 {
		return questions, nil
	}
	var endings []map[string]any
	_ = json.Unmarshal(rawEndings, &endings)
	endingIDs := map[string]bool{}
	for _, e := range endings {
		if id, ok := e["id"].(string); ok {
			endingIDs[id] = true
		}
	}
	firstElement := map[string]string{}
	for _, b := range blocks {
		elements, _ := b["elements"].([]any)
		if len(elements) == 0 {
			continue
		}
		if first, ok := elements[0].(map[string]any); ok {
			if id, ok := b["id"].(string); ok {
				firstElement[id], _ = first["id"].(string)
			}
		}
	}
	for _, b := range blocks {
		elements, _ := b["elements"].([]any)
		for i, e := range elements {
			el := cloneMap(asMap(e))
			if el["type"] == "cta" && truthy(el["ctaButtonLabel"]) {
				el["buttonLabel"] = el["ctaButtonLabel"]
			}
			if i == len(elements)-1 {
				if logic, ok := b["logic"].([]any); ok && len(logic) > 0 {
					converted := make([]any, len(logic))
					for j, item := range logic {
						o := asMap(item)
						conditions := renameOperandTypes(o["conditions"], "element", "question")
						if o == nil || !isConditionGroup(conditions) {
							converted[j] = item
							continue
						}
						updated := cloneMap(o)
						updated["conditions"] = conditions
						updated["actions"] = retargetJumps(o["actions"], "jumpToBlock", "jumpToQuestion", firstElement)
						converted[j] = updated
					}
					el["logic"] = converted
				}
				if fallback, ok := b["logicFallback"].(string); ok && fallback != "" {
					switch {
					case firstElement[fallback] != "":
						el["logicFallback"] = firstElement[fallback]
					case endingIDs[fallback]:
						el["logicFallback"] = fallback
					default:
						delete(el, "logicFallback")
					}
				}
				if truthy(b["buttonLabel"]) {
					el["buttonLabel"] = b["buttonLabel"]
				}
				if truthy(b["backButtonLabel"]) {
					el["backButtonLabel"] = b["backButtonLabel"]
				}
			}
			questions = append(questions, el)
		}
	}
	return questions, nil
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// truthy is JavaScript's Boolean(v) for a JSON value.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != ""
	}
	return true
}

func isConditionGroup(v any) bool {
	o := asMap(v)
	_, ok := o["conditions"].([]any)
	return ok && o["connector"] != nil
}

// renameOperandTypes copies a condition tree with every operand of type from retyped to.
func renameOperandTypes(v any, from, to string) any {
	o := asMap(v)
	if o == nil {
		return v
	}
	out := cloneMap(o)
	if list, ok := o["conditions"].([]any); ok {
		conditions := make([]any, len(list))
		for i, c := range list {
			conditions[i] = renameOperandTypes(c, from, to)
		}
		out["conditions"] = conditions
		return out
	}
	for _, side := range []string{"leftOperand", "rightOperand"} {
		if operand := asMap(o[side]); operand != nil {
			copied := cloneMap(operand)
			if copied["type"] == from {
				copied["type"] = to
			}
			out[side] = copied
		}
	}
	return out
}

// retargetJumps rewrites jump actions of objective from into objective to, a target found in targets replaced by what it maps to.
func retargetJumps(v any, from, to string, targets map[string]string) any {
	list, ok := v.([]any)
	if !ok {
		return v
	}
	out := make([]any, len(list))
	for i, a := range list {
		o := asMap(a)
		if o == nil || o["objective"] != from {
			out[i] = a
			continue
		}
		copied := cloneMap(o)
		copied["objective"] = to
		if target, ok := o["target"].(string); ok && targets[target] != "" {
			copied["target"] = targets[target]
		}
		out[i] = copied
	}
	return out
}

// processCTAQuestions is forma2's processCTAQuestions: a CTA question keeps an external button (relabelled ctaButtonLabel) only when it has a URL, loses its dismiss label, and logic on a CTA without an external button that tested clicks or skips is dropped, as is a test for a skip on one with.
func processCTAQuestions(questions []map[string]any) {
	ctas := map[string]bool{}
	for _, q := range questions {
		if q["type"] == "cta" {
			id, _ := q["id"].(string)
			ctas[id] = q["buttonExternal"] == true && truthy(q["buttonUrl"])
		}
	}
	if len(ctas) == 0 {
		return
	}
	for _, q := range questions {
		if q["type"] != "cta" {
			continue
		}
		if q["buttonExternal"] == true && truthy(q["buttonUrl"]) {
			if truthy(q["buttonLabel"]) {
				q["ctaButtonLabel"] = q["buttonLabel"]
			}
			q["buttonExternal"] = true
		} else {
			delete(q, "buttonExternal")
			delete(q, "buttonUrl")
		}
		delete(q, "buttonLabel")
		delete(q, "dismissButtonLabel")
	}
	for _, q := range questions {
		logic, ok := q["logic"].([]any)
		if !ok || len(logic) == 0 {
			continue
		}
		var kept []any
		for _, rule := range logic {
			o := asMap(rule)
			if o == nil {
				kept = append(kept, rule)
				continue
			}
			conditions := o["conditions"]
			keep := true
			for id, external := range ctas {
				operators := []string{"isSkipped"}
				if !external {
					operators = []string{"isClicked", "isSkipped"}
				}
				if !referencesCTA(conditions, id, operators) {
					continue
				}
				cleaned := removeCTAConditions(asMap(conditions), id, operators)
				if cleaned == nil {
					keep = false
					break
				}
				conditions = cleaned
			}
			if keep {
				copied := cloneMap(o)
				copied["conditions"] = conditions
				kept = append(kept, copied)
			}
		}
		if len(kept) == 0 {
			delete(q, "logic")
		} else {
			q["logic"] = kept
		}
	}
}

// referencesCTA is forma2's conditionReferencesCTA for any of operators (any operator when operators would include both a click and a skip test on a CTA without an external button, as forma2 asked without one).
func referencesCTA(v any, id string, operators []string) bool {
	o := asMap(v)
	if o == nil {
		return false
	}
	if list, ok := o["conditions"].([]any); ok {
		for _, c := range list {
			if referencesCTA(c, id, operators) {
				return true
			}
		}
		return false
	}
	left := asMap(o["leftOperand"])
	if left == nil || left["value"] != id {
		return false
	}
	if len(operators) > 1 {
		return true
	}
	op, _ := o["operator"].(string)
	return contains(operators, op)
}

// removeCTAConditions is forma2's removeCtaConditions: the group without the conditions that test the CTA with one of operators, nested groups emptied by it dropped; nil when nothing is left.
func removeCTAConditions(group map[string]any, id string, operators []string) map[string]any {
	list, _ := group["conditions"].([]any)
	var kept []any
	for _, c := range list {
		o := asMap(c)
		if o == nil {
			kept = append(kept, c)
			continue
		}
		if _, nested := o["conditions"].([]any); nested {
			cleaned := removeCTAConditions(o, id, operators)
			if cleaned == nil {
				continue
			}
			kept = append(kept, cleaned)
			continue
		}
		left := asMap(o["leftOperand"])
		op, _ := o["operator"].(string)
		if left != nil && left["value"] == id && contains(operators, op) {
			continue
		}
		kept = append(kept, c)
	}
	if len(kept) == 0 {
		return nil
	}
	out := cloneMap(group)
	out["conditions"] = kept
	return out
}
