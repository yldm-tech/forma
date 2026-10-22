package survey

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/v3api"
)

// linkSurvey is a multilingual v3 create document in the shape of the spec's productFeedback example.
func linkSurvey(ws uuid.UUID) string {
	return fmt.Sprintf(`{
	  "workspaceId": %q,
	  "name": " Product Feedback ",
	  "defaultLanguage": "en-US",
	  "metadata": {"cx_operation": "onboarding", "title": {"en-US": "Product Feedback", "de-DE": "Produktfeedback"}},
	  "languages": [{"code": "de-DE", "enabled": true}],
	  "welcomeCard": {"enabled": true, "headline": {"en-US": "Welcome", "de-DE": "Willkommen"}},
	  "blocks": [
	    {"name": "Main", "elements": [
	      {"id": "satisfaction", "type": "openText", "headline": {"en-US": "What should we improve?", "de-DE": "Was sollen wir verbessern?"}, "required": true}
	    ]},
	    {"name": "Rate", "elements": [
	      {"id": "rating", "type": "rating", "headline": {"en-US": "Rate us", "de-DE": "Bewerte uns"}, "required": false, "scale": "star", "range": 5}
	    ]}
	  ],
	  "endings": [{"id": "endingone", "type": "endScreen", "headline": {"en-US": "Thanks", "de-DE": "Danke"}}],
	  "hiddenFields": {"enabled": true, "fieldIds": ["plan"]},
	  "variables": [{"name": "score", "type": "number", "value": 0}]
	}`, ws.String())
}

func TestV3CreateGetListPatchLifecycle(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)

	rec := e.v3(owner, http.MethodPost, surveysPath(""), linkSurvey(ws))
	var created resource
	e.ok(rec, http.StatusCreated, &created)
	id := created.Data["id"].(string)
	if rec.Header().Get("Location") != "/api/v3/surveys/"+id || rec.Header().Get("Cache-Control") != v3api.CacheNoStore {
		t.Fatalf("headers %v", rec.Header())
	}
	d := created.Data
	if d["name"] != "Product Feedback" || d["status"] != "draft" || d["type"] != "link" || d["defaultLanguage"] != "en-US" || d["archivedAt"] != nil {
		t.Fatalf("created %v", d)
	}
	if _, ok := d["distribution"]; ok {
		t.Fatal("a link survey carries distribution")
	}
	langs := d["languages"].([]any)
	if len(langs) != 2 || langs[0].(map[string]any)["code"] != "en-US" || langs[0].(map[string]any)["default"] != true || langs[1].(map[string]any)["code"] != "de-DE" {
		t.Fatalf("languages %v", langs)
	}
	blocks := d["blocks"].([]any)
	first := blocks[0].(map[string]any)
	if id, _ := first["id"].(string); len(id) != 24 {
		t.Fatalf("generated block id %v", first["id"])
	}
	el := first["elements"].([]any)[0].(map[string]any)
	if h := el["headline"].(map[string]any); h["en-US"] != "What should we improve?" || h["de-DE"] != "Was sollen wir verbessern?" || len(h) != 2 {
		t.Fatalf("headline %v", h)
	}
	if el["inputType"] != "text" {
		t.Fatalf("open text defaults were not stored: %v", el)
	}
	// Stored internally under "default", the default language's text.
	if !strings.Contains(e.text(`SELECT blocks::text FROM surveys WHERE id = ?`, id), `"default": "What should we improve?"`) {
		t.Fatal("blocks not stored in the internal form")
	}
	if e.count(`SELECT count(*) FROM survey_languages sl JOIN languages l ON l.id = sl.language_id WHERE sl.survey_id = ? AND l.code = 'de-DE' AND NOT sl.is_default`, id) != 1 {
		t.Fatal("de-DE was not connected-or-created")
	}
	if e.count(`SELECT count(*) FROM survey_embedded_data WHERE survey_id = ?`, id) != 2 {
		t.Fatal("embedded data not reconciled")
	}
	if e.count(`SELECT count(*) FROM users WHERE id = ? AND notification_settings->'alert'->>? = 'true'`, owner, id) != 1 {
		t.Fatal("the creator was not subscribed to response alerts")
	}

	var got resource
	e.ok(e.v3(owner, http.MethodGet, surveysPath("/"+id+"?lang=de"), nil), http.StatusOK, &got)
	gotHeadline := got.Data["welcomeCard"].(map[string]any)["headline"].(map[string]any)
	if len(gotHeadline) != 1 || gotHeadline["de-DE"] != "Willkommen" {
		t.Fatalf("lang selector %v", gotHeadline)
	}
	p := e.problem(e.v3(owner, http.MethodGet, surveysPath("/"+id+"?lang=fr-FR"), nil), http.StatusBadRequest, "Language 'fr-FR' is not configured for this survey")
	if p.param(t, "lang").Identifier != "fr-FR" {
		t.Fatalf("lang problem %+v", p)
	}
	e.problem(e.v3(owner, http.MethodGet, surveysPath("/"+id+"?workspaceId=x"), nil), http.StatusBadRequest, "Invalid query parameters")

	var list struct {
		Data []map[string]any `json:"data"`
		Meta map[string]any   `json:"meta"`
	}
	e.ok(e.v3(owner, http.MethodGet, surveysPath("?workspaceId="+ws.String()), nil), http.StatusOK, &list)
	if len(list.Data) != 1 || list.Meta["totalCount"] != float64(1) || list.Meta["workspaceSurveyCount"] != float64(1) || list.Meta["nextCursor"] != nil || list.Meta["limit"] != float64(20) {
		t.Fatalf("list %+v", list)
	}
	if list.Data[0]["responseCount"] != float64(0) || list.Data[0]["creator"].(map[string]any)["name"] != "Ada" {
		t.Fatalf("list item %v", list.Data[0])
	}

	var patched resource
	e.ok(e.v3(owner, http.MethodPatch, surveysPath("/"+id), `{"name": "Renamed", "status": "inProgress"}`), http.StatusOK, &patched)
	if patched.Data["name"] != "Renamed" || patched.Data["status"] != "inProgress" || len(patched.Data["blocks"].([]any)) != 2 {
		t.Fatalf("patched %v", patched.Data)
	}
	p = e.problem(e.v3(owner, http.MethodPatch, surveysPath("/"+id), `{}`), http.StatusUnprocessableEntity, failedValidation)
	if p.param(t, "data").Reason != "Request body must include at least one updatable field" {
		t.Fatalf("empty patch %+v", p)
	}
	p = e.problem(e.v3(owner, http.MethodPatch, surveysPath("/"+id), `{"type": "app"}`), http.StatusUnprocessableEntity, failedValidation)
	if ip := p.param(t, "type"); ip.Code != v3api.CodeUnsupportedField {
		t.Fatalf("type patch %+v", ip)
	}

	var ack struct {
		Data map[string]any `json:"data"`
	}
	e.ok(e.v3(owner, http.MethodPost, surveysPath("/"+id+"/archive"), nil), http.StatusOK, &ack)
	if ack.Data["status"] != "paused" || ack.Data["archivedAt"] == nil || len(ack.Data) != 3 {
		t.Fatalf("archive ack %v", ack.Data)
	}
	p = e.problem(e.v3(owner, http.MethodPatch, surveysPath("/"+id), `{"name": "x"}`), http.StatusUnprocessableEntity, "Survey is archived")
	if p.param(t, "archivedAt").Reason != "This survey is archived. Restore it before editing." {
		t.Fatalf("archived patch %+v", p)
	}
	e.ok(e.v3(owner, http.MethodGet, surveysPath("?workspaceId="+ws.String()), nil), http.StatusOK, &list)
	if len(list.Data) != 0 || list.Meta["workspaceSurveyCount"] != float64(1) {
		t.Fatalf("archived surveys are hidden by default: %+v", list)
	}
	e.ok(e.v3(owner, http.MethodGet, surveysPath("?workspaceId="+ws.String()+"&filter[status][in]=archived"), nil), http.StatusOK, &list)
	if len(list.Data) != 1 {
		t.Fatalf("the archived filter %+v", list)
	}
	e.ok(e.v3(owner, http.MethodPost, surveysPath("/"+id+"/restore"), nil), http.StatusOK, &ack)
	if ack.Data["status"] != "paused" || ack.Data["archivedAt"] != nil {
		t.Fatalf("restore ack %v", ack.Data)
	}

	rec = e.v3(owner, http.MethodDelete, surveysPath("/"+id), nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete %d %s", rec.Code, rec.Body.String())
	}
	e.problem(e.v3(owner, http.MethodGet, surveysPath("/"+id), nil), http.StatusForbidden, v3api.ForbiddenMessage)
	if e.count(`SELECT count(*) FROM embedded_data WHERE survey_id = ?`, id) != 0 {
		t.Fatal("embedded data survived the delete")
	}
}

func TestV3CreateValidation(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	doc := func(mutate func(m map[string]any)) map[string]any {
		var m map[string]any
		if err := json.Unmarshal([]byte(linkSurvey(ws)), &m); err != nil {
			t.Fatal(err)
		}
		mutate(m)
		return m
	}

	p := e.problem(e.v3(owner, http.MethodPost, surveysPath(""), `{"broken"`), http.StatusBadRequest, "Invalid request body")
	if p.param(t, "body").Reason != "Malformed JSON input, please check your request body" {
		t.Fatalf("%+v", p)
	}
	p = e.problem(e.v3(owner, http.MethodPost, surveysPath(""), doc(func(m map[string]any) { m["questions"] = []any{} })), http.StatusBadRequest, "Invalid request body")
	if ip := p.param(t, "questions"); ip.Code != v3api.CodeUnsupportedField || !strings.HasPrefix(ip.Reason, "Unsupported field 'questions' for the survey document. Allowed fields: blocks, defaultLanguage, distribution") {
		t.Fatalf("%+v", ip)
	}
	p = e.problem(e.v3(owner, http.MethodPost, surveysPath(""), doc(func(m map[string]any) {
		m["distribution"] = map[string]any{"displayOption": "displayOnce"}
	})), http.StatusBadRequest, "Invalid request body")
	if ip := p.param(t, "distribution"); ip.Code != v3api.CodeUnsupportedField || ip.Reason != "'distribution' is only supported for app surveys (type: 'app')" {
		t.Fatalf("%+v", ip)
	}
	p = e.problem(e.v3(owner, http.MethodPost, surveysPath(""), doc(func(m map[string]any) {
		m["welcomeCard"] = map[string]any{"enabled": true, "headline": map[string]any{"default": "x"}}
	})), http.StatusBadRequest, "Invalid request body")
	p.param(t, "welcomeCard.headline.default")
	p = e.problem(e.v3(owner, http.MethodPost, surveysPath(""), doc(func(m map[string]any) { m["blocks"] = []any{} })), http.StatusBadRequest, "Invalid request body")
	if p.param(t, "blocks").Reason != "At least one block is required" {
		t.Fatalf("%+v", p)
	}
	p = e.problem(e.v3(owner, http.MethodPost, surveysPath(""), doc(func(m map[string]any) { m["name"] = "  " })), http.StatusBadRequest, "Invalid request body")
	if p.param(t, "name").Reason != "Survey name is required" {
		t.Fatalf("%+v", p)
	}

	// Semantic failures are 422.
	p = e.problem(e.v3(owner, http.MethodPost, surveysPath(""), doc(func(m map[string]any) {
		m["languages"] = []any{map[string]any{"code": "de-DE"}, map[string]any{"code": "fr-FR"}}
	})), http.StatusUnprocessableEntity, failedValidation)
	if ip := p.InvalidParams[0]; ip.Code != v3api.CodeMissingTranslation || ip.Identifier != "fr-FR" {
		t.Fatalf("%+v", p)
	}
	p = e.problem(e.v3(owner, http.MethodPost, surveysPath(""), doc(func(m map[string]any) {
		blocks := m["blocks"].([]any)
		blocks[0].(map[string]any)["logic"] = []any{map[string]any{"id": "logicone", "conditions": map[string]any{"id": "groupone", "connector": "and", "conditions": []any{}}, "actions": []any{map[string]any{"id": "actone", "objective": "jumpToBlock", "target": "nowhere"}}}}
	})), http.StatusUnprocessableEntity, failedValidation)
	if ip := p.param(t, "blocks.0.logic.0.actions.0.target"); ip.Code != v3api.CodeDanglingReference || ip.MissingID != "nowhere" {
		t.Fatalf("%+v", ip)
	}
	p = e.problem(e.v3(owner, http.MethodPost, surveysPath(""), doc(func(m map[string]any) {
		m["welcomeCard"] = map[string]any{"enabled": true, "headline": map[string]any{"en-US": "Hi #recall:ghost/fallback:x#", "de-DE": "Hallo"}}
	})), http.StatusUnprocessableEntity, failedValidation)
	// References are checked on the normalized document, where the default language's text sits under "default".
	if ip := p.param(t, "welcomeCard.headline.default"); ip.Code != v3api.CodeDanglingReference || ip.ReferenceType != "recall" {
		t.Fatalf("%+v", ip)
	}
	p = e.problem(e.v3(owner, http.MethodPost, surveysPath(""), doc(func(m map[string]any) {
		m["hiddenFields"] = map[string]any{"enabled": true, "fieldIds": []any{"satisfaction"}}
	})), http.StatusUnprocessableEntity, failedValidation)
	if ip := p.param(t, "hiddenFields.fieldIds.0"); ip.Code != v3api.CodeDuplicateIdentifier || ip.ConflictsWith != "blocks.0.elements.0.id" {
		t.Fatalf("%+v", ip)
	}
	// The survey service's stricter write rules: a CTA to plain http is refused only there.
	p = e.problem(e.v3(owner, http.MethodPost, surveysPath(""), doc(func(m map[string]any) {
		blocks := m["blocks"].([]any)
		blocks[1].(map[string]any)["elements"] = []any{map[string]any{"id": "cta", "type": "cta", "headline": map[string]any{"en-US": "Go", "de-DE": "Los"}, "required": false, "buttonExternal": true, "buttonUrl": "http://example.com", "ctaButtonLabel": map[string]any{"en-US": "Open", "de-DE": "Öffnen"}}}
	})), http.StatusUnprocessableEntity, failedValidation)
	if ip := p.param(t, "blocks.1.elements.0.buttonUrl"); ip.Reason != "Element 1 in block 2: URL must start with https:// or mailto:" {
		t.Fatalf("%+v", ip)
	}
	p = e.problem(e.v3(owner, http.MethodPost, surveysPath(""), doc(func(m map[string]any) {
		m["hiddenFields"] = map[string]any{"enabled": true, "fieldIds": []any{"country"}}
	})), http.StatusBadRequest, "")
	if !strings.HasPrefix(p.param(t, "body").Reason, `Field name "country" cannot be used`) {
		t.Fatalf("%+v", p)
	}
	if e.count(`SELECT count(*) FROM surveys`) != 0 || e.count(`SELECT count(*) FROM languages WHERE code = 'de-DE'`) != 0 {
		t.Fatal("a refused create wrote rows")
	}
}

func TestV3AppSurveyDistributionAndTargeting(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	trigger := e.actionClass(ws, "signup")
	e.exec(`INSERT INTO contact_attribute_keys (workspace_id, key) VALUES (?, 'plan')`, ws)
	body := func(triggers, filters string) string {
		return fmt.Sprintf(`{"workspaceId": %q, "name": "App", "type": "app", "status": "inProgress",
			"blocks": [{"name": "B", "elements": [{"id": "q", "type": "openText", "headline": {"en-US": "Q"}, "required": false}]}],
			"distribution": {"displayOption": "displaySome", "displayLimit": 2, "triggers": %s},
			"targeting": {"filters": %s}}`, ws.String(), triggers, filters)
	}
	planFilter := `[{"id": "filterone", "connector": null, "resource": {"id": "leafone", "root": {"type": "attribute", "contactAttributeKey": "plan"}, "qualifier": {"operator": "equals"}, "value": "pro"}}]`

	p := e.problem(e.v3(owner, http.MethodPost, surveysPath(""), body(`[{"actionClassId": "`+uuid.NewString()+`"}]`, `[]`)), http.StatusUnprocessableEntity, failedValidation)
	if ip := p.param(t, "distribution.triggers.0.actionClassId"); ip.Code != v3api.CodeInvalidReference {
		t.Fatalf("%+v", ip)
	}
	p = e.problem(e.v3(owner, http.MethodPost, surveysPath(""), body(`[{"actionClassId": "`+trigger.String()+`"}]`, strings.Replace(planFilter, `"plan"`, `"tier"`, 1))), http.StatusUnprocessableEntity, failedValidation)
	if ip := p.param(t, "targeting.filters.0.resource.root.contactAttributeKey"); ip.Reason != "Contact attribute key 'tier' was not found in this workspace" {
		t.Fatalf("%+v", ip)
	}
	e.problem(e.v3(owner, http.MethodPost, surveysPath(""), body(`[]`, `[]`)), http.StatusBadRequest, appTriggerMessage)

	var created resource
	e.ok(e.v3(owner, http.MethodPost, surveysPath(""), body(`[{"actionClassId": "`+trigger.String()+`"}]`, planFilter)), http.StatusCreated, &created)
	id := created.Data["id"].(string)
	dist := created.Data["distribution"].(map[string]any)
	if dist["displayOption"] != "displaySome" || dist["displayLimit"] != float64(2) || dist["delay"] != float64(0) || len(dist["triggers"].([]any)) != 1 {
		t.Fatalf("distribution %v", dist)
	}
	if filters := created.Data["targeting"].(map[string]any)["filters"].([]any); len(filters) != 1 {
		t.Fatalf("targeting %v", created.Data["targeting"])
	}
	if e.count(`SELECT count(*) FROM segments WHERE title = ? AND is_private AND jsonb_array_length(filters) = 1`, id) != 1 {
		t.Fatal("the private segment was not created with the filters")
	}

	// Distribution replaces whole: dropping the triggers of a running app survey is refused.
	p = e.problem(e.v3(owner, http.MethodPatch, surveysPath("/"+id), `{"distribution": {"displayOption": "displayOnce"}}`), http.StatusUnprocessableEntity, failedValidation)
	if p.param(t, "triggers").Reason != appTriggerMessage {
		t.Fatalf("%+v", p)
	}
	var patched resource
	e.ok(e.v3(owner, http.MethodPatch, surveysPath("/"+id), `{"targeting": {"filters": []}}`), http.StatusOK, &patched)
	if e.count(`SELECT count(*) FROM segments WHERE title = ? AND jsonb_array_length(filters) = 0`, id) != 1 {
		t.Fatal("the targeting change did not reach the segment")
	}
	if dist := patched.Data["distribution"].(map[string]any); len(dist["triggers"].([]any)) != 1 || dist["displayLimit"] != float64(2) {
		t.Fatalf("an omitted distribution was not kept: %v", dist)
	}

	rec := e.v3(owner, http.MethodDelete, surveysPath("/"+id), nil)
	if rec.Code != http.StatusNoContent || e.count(`SELECT count(*) FROM segments WHERE title = ?`, id) != 0 {
		t.Fatalf("deleting an app survey keeps its private segment: %d", rec.Code)
	}
}

func TestV3Authorization(t *testing.T) {
	e := newEnv(t)
	owner, reader, outsider := e.user(), e.user(), e.user()
	org, ws := e.organization(owner)
	e.member(org, reader, "member")
	e.grant(org, ws, reader, "read")
	var created resource
	e.ok(e.v3(owner, http.MethodPost, surveysPath(""), linkSurvey(ws)), http.StatusCreated, &created)
	id := created.Data["id"].(string)

	e.problem(e.request(http.MethodGet, surveysPath("?workspaceId="+ws.String()), nil, nil), http.StatusUnauthorized, "Not authenticated")
	e.problem(e.request(http.MethodPost, surveysPath("/templates"), `{}`, map[string]string{"x-api-key": e.apiKey(org, ws, "write")}), http.StatusUnauthorized, "Session required")

	e.ok(e.v3(reader, http.MethodGet, surveysPath("/"+id), nil), http.StatusOK, nil)
	e.problem(e.v3(reader, http.MethodPatch, surveysPath("/"+id), `{"name": "x"}`), http.StatusForbidden, v3api.ForbiddenMessage)
	e.problem(e.v3(reader, http.MethodPost, surveysPath(""), linkSurvey(ws)), http.StatusForbidden, v3api.ForbiddenMessage)
	e.problem(e.v3(reader, http.MethodDelete, surveysPath("/"+id), nil), http.StatusForbidden, v3api.ForbiddenMessage)
	e.problem(e.v3(outsider, http.MethodGet, surveysPath("/"+id), nil), http.StatusForbidden, v3api.ForbiddenMessage)
	e.problem(e.v3(outsider, http.MethodGet, surveysPath("?workspaceId="+ws.String()), nil), http.StatusForbidden, v3api.ForbiddenMessage)
	e.problem(e.v3(owner, http.MethodGet, surveysPath("/"+uuid.NewString()), nil), http.StatusForbidden, v3api.ForbiddenMessage)
	e.problem(e.v3(owner, http.MethodGet, surveysPath("/not-an-id"), nil), http.StatusBadRequest, "Invalid route parameters")

	readKey, writeKey := e.apiKey(org, ws, "read"), e.apiKey(org, ws, "write")
	e.ok(e.v3Key(readKey, http.MethodGet, surveysPath("/"+id), nil), http.StatusOK, nil)
	e.problem(e.v3Key(readKey, http.MethodPatch, surveysPath("/"+id), `{"name": "x"}`), http.StatusForbidden, v3api.ForbiddenMessage)
	var patched resource
	e.ok(e.v3Key(writeKey, http.MethodPatch, surveysPath("/"+id), `{"name": "By key"}`), http.StatusOK, &patched)
	var keyCreated resource
	e.ok(e.v3Key(writeKey, http.MethodPost, surveysPath(""), linkSurvey(ws)), http.StatusCreated, &keyCreated)
	if e.count(`SELECT count(*) FROM surveys WHERE id = ? AND created_by IS NULL`, keyCreated.Data["id"]) != 1 {
		t.Fatal("a survey created by an API key has a creator")
	}
	otherOrg, otherWS := e.organization(e.user())
	foreignKey := e.apiKey(otherOrg, otherWS, "manage")
	e.problem(e.v3Key(foreignKey, http.MethodGet, surveysPath("/"+id), nil), http.StatusForbidden, v3api.ForbiddenMessage)
	e.problem(e.v3Key("nonsense", http.MethodGet, surveysPath("/"+id), nil), http.StatusUnauthorized, "Not authenticated")
}

func TestV3Validate(t *testing.T) {
	e := newEnv(t)
	owner, outsider := e.user(), e.user()
	_, ws := e.organization(owner)
	var out struct {
		Data ValidationResult `json:"data"`
	}
	e.ok(e.v3(owner, http.MethodPost, surveysPath("/validate"), `{"operation": "create", "data": `+linkSurvey(ws)+`}`), http.StatusOK, &out)
	if !out.Data.Valid || len(out.Data.InvalidParams) != 0 || len(out.Data.Languages) != 2 || out.Data.Languages[0].Code != "en-US" || out.Data.Languages[1].WriteBehavior != "connect_or_create" {
		t.Fatalf("valid create %+v", out.Data)
	}
	out = struct {
		Data ValidationResult `json:"data"`
	}{}
	e.ok(e.v3(owner, http.MethodPost, surveysPath("/validate"), `{"operation": "create", "data": {"name": "x"}}`), http.StatusOK, &out)
	if out.Data.Valid || len(out.Data.InvalidParams) == 0 || out.Data.Languages != nil {
		t.Fatalf("invalid create %+v", out.Data)
	}
	e.problem(e.v3(outsider, http.MethodPost, surveysPath("/validate"), `{"operation": "create", "data": `+linkSurvey(ws)+`}`), http.StatusForbidden, v3api.ForbiddenMessage)
	e.problem(e.v3(owner, http.MethodPost, surveysPath("/validate"), `{"operation": "delete", "data": {}}`), http.StatusBadRequest, "Invalid request body")

	var created resource
	e.ok(e.v3(owner, http.MethodPost, surveysPath(""), linkSurvey(ws)), http.StatusCreated, &created)
	e.ok(e.v3(owner, http.MethodPost, surveysPath("/validate"), fmt.Sprintf(`{"operation": "patch", "surveyId": %q, "data": {"blocks": []}}`, created.Data["id"])), http.StatusOK, &out)
	if out.Data.Valid || out.Data.InvalidParams[0].Name != "blocks" {
		t.Fatalf("invalid patch %+v", out.Data)
	}
	if e.count(`SELECT count(*) FROM surveys WHERE name = 'Product Feedback'`) != 1 {
		t.Fatal("validation wrote")
	}
}

func TestV3ImmutableElementIDsOnceLive(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	var created resource
	e.ok(e.v3(owner, http.MethodPost, surveysPath(""), linkSurvey(ws)), http.StatusCreated, &created)
	id := created.Data["id"].(string)
	blocks, _ := json.Marshal(created.Data["blocks"])
	renamed := strings.Replace(string(blocks), `"id":"satisfaction"`, `"id":"renamed"`, 1)
	e.ok(e.v3(owner, http.MethodPatch, surveysPath("/"+id), `{"blocks": `+renamed+`}`), http.StatusOK, nil)
	e.ok(e.v3(owner, http.MethodPatch, surveysPath("/"+id), `{"status": "inProgress"}`), http.StatusOK, nil)
	again := strings.Replace(renamed, `"id":"renamed"`, `"id":"again"`, 1)
	p := e.problem(e.v3(owner, http.MethodPatch, surveysPath("/"+id), `{"blocks": `+again+`}`), http.StatusUnprocessableEntity, failedValidation)
	if ip := p.param(t, "blocks.0.elements.0.id"); ip.Code != v3api.CodeImmutableIdentifier || ip.Identifier != "renamed" {
		t.Fatalf("%+v", ip)
	}
}

// A v3 patch is merged into and written over the survey as it is under the row lock: one archived while the patch waited stays archived and paused instead of going live.
func TestV3PatchRespectsAConcurrentArchive(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	var created resource
	e.ok(e.v3(owner, http.MethodPost, surveysPath(""), linkSurvey(ws)), http.StatusCreated, &created)
	id := created.Data["id"].(string)

	release := e.holdSurveyLock(id)
	patched := e.async(http.MethodPatch, surveysPath("/"+id), map[string]any{"status": "inProgress"}, map[string]string{testUserHeader: owner.String()})
	e.waitForLockWaiter()
	release(`UPDATE surveys SET archived_at = now(), status = 'paused' WHERE id = ?`)
	p := e.problem(<-patched, http.StatusUnprocessableEntity, "Survey is archived")
	p.param(t, "archivedAt")
	if status := e.text(`SELECT status FROM surveys WHERE id = ?`, id); status != "paused" {
		t.Fatalf("archived survey patched to %q", status)
	}

	// Two patches of different fields both land: the second is merged into what the first wrote.
	e.exec(`UPDATE surveys SET archived_at = NULL WHERE id = ?`, id)
	release = e.holdSurveyLock(id)
	renamed := e.async(http.MethodPatch, surveysPath("/"+id), map[string]any{"name": "Renamed"}, map[string]string{testUserHeader: owner.String()})
	e.waitForLockWaiter()
	release(`UPDATE surveys SET welcome_card = '{"enabled": false}' WHERE id = ?`)
	e.ok(<-renamed, http.StatusOK, nil)
	if name, wc := e.text(`SELECT name FROM surveys WHERE id = ?`, id), e.text(`SELECT welcome_card::text FROM surveys WHERE id = ?`, id); name != "Renamed" || !strings.Contains(wc, `"enabled": false`) {
		t.Fatalf("name %q welcome card %s", name, wc)
	}
}

// A session is an ambient credential: every session-authenticated v3 mutation must come from the app's origin with the X-Requested-With header, as on /api/app and the other v3 routes. An API key is exempt.
func TestV3SessionMutationsPassTheCSRFRule(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	org, ws := e.organization(owner)
	var created resource
	e.ok(e.v3(owner, http.MethodPost, surveysPath(""), linkSurvey(ws)), http.StatusCreated, &created)
	id := created.Data["id"].(string)

	crossSite := map[string]string{testUserHeader: owner.String(), "Origin": "https://evil.example.com"}
	noHeader := map[string]string{testUserHeader: owner.String(), httpapi.CSRFHeader: ""}
	for _, r := range []struct{ method, path, body string }{
		{http.MethodPost, surveysPath(""), linkSurvey(ws)},
		{http.MethodPost, surveysPath("/validate"), linkSurvey(ws)},
		{http.MethodPatch, surveysPath("/" + id), `{"name": "Hijacked"}`},
		{http.MethodPost, surveysPath("/" + id + "/archive"), ""},
		{http.MethodPost, surveysPath("/" + id + "/restore"), ""},
		{http.MethodDelete, surveysPath("/" + id), ""},
		{http.MethodPost, surveysPath("/templates"), `{}`},
	} {
		var body any
		if r.body != "" {
			body = r.body
		}
		e.problem(e.request(r.method, r.path, body, crossSite), http.StatusForbidden, "Cross-origin request rejected")
		e.problem(e.request(r.method, r.path, body, noHeader), http.StatusForbidden, "Missing X-Requested-With header")
	}
	if e.count(`SELECT count(*) FROM surveys WHERE id = ? AND name = 'Product Feedback' AND archived_at IS NULL`, id) != 1 || e.count(`SELECT count(*) FROM surveys WHERE workspace_id = ?`, ws) != 1 {
		t.Fatal("a cross-site request changed a survey")
	}
	// A read needs no origin.
	e.ok(e.request(http.MethodGet, surveysPath("/"+id), nil, crossSite), http.StatusOK, nil)
	// An API key is not sent by a browser on its own.
	e.ok(e.request(http.MethodPatch, surveysPath("/"+id), `{"name": "By key"}`, map[string]string{"x-api-key": e.apiKey(org, ws, "write"), "Origin": "", httpapi.CSRFHeader: ""}), http.StatusOK, nil)
}

// forma2's POST /api/v3/surveys/generate up to the AI provider call, on an instance without an AI provider: the body, the workspace and the prompt are checked first, then the organization's AI smart tools toggle, then the instance's configuration.
func TestV3Generate(t *testing.T) {
	e := newEnv(t)
	owner, reader, outsider := e.user(), e.user(), e.user()
	org, ws := e.organization(owner)
	e.member(org, reader, "member")
	e.grant(org, ws, reader, "read")
	prompt := "Understand why new users stop during onboarding and what would help them finish setup."
	body := func(extra string) string {
		return fmt.Sprintf(`{"workspaceId": %q, "prompt": %q%s}`, ws, prompt, extra)
	}

	e.problem(e.request(http.MethodPost, surveysPath("/generate"), body(""), nil), http.StatusUnauthorized, "Not authenticated")
	cases := []struct {
		name, body, param, reason string
	}{
		{"no prompt", fmt.Sprintf(`{"workspaceId": %q}`, ws), "prompt", "Invalid input: expected string, received undefined"},
		{"a short prompt", fmt.Sprintf(`{"workspaceId": %q, "prompt": " ab "}`, ws), "prompt", "Prompt must be at least 4 characters"},
		{"a workspace that is not an id", fmt.Sprintf(`{"workspaceId": "ws", "prompt": %q}`, prompt), "workspaceId", "Invalid UUID"},
		{"an unknown type", body(`, "type": "email"`), "type", `Invalid option: expected one of "link"|"app"`},
		{"an unsupported language", body(`, "languages": ["en-US", "xx"]`), "languages.1", "Language 'xx' is not supported for AI survey creation"},
		{"a repeated language", body(`, "languages": ["de-DE", "de_de"]`), "languages", "languages must not repeat a locale"},
		{"an unknown key", body(`, "model": "big"`), "body", `Unrecognized key: "model"`},
	}
	for _, tc := range cases {
		p := e.problem(e.v3(owner, http.MethodPost, surveysPath("/generate"), tc.body), http.StatusBadRequest, "Invalid request body")
		if got := p.param(t, tc.param); got.Reason != tc.reason {
			t.Errorf("%s: %q", tc.name, got.Reason)
		}
	}
	e.problem(e.v3(reader, http.MethodPost, surveysPath("/generate"), body("")), http.StatusForbidden, v3api.ForbiddenMessage)
	e.problem(e.v3(outsider, http.MethodPost, surveysPath("/generate"), body("")), http.StatusForbidden, v3api.ForbiddenMessage)
	e.problem(e.v3Key(e.apiKey(org, ws, "read"), http.MethodPost, surveysPath("/generate"), body("")), http.StatusForbidden, v3api.ForbiddenMessage)

	vague := fmt.Sprintf(`{"workspaceId": %q, "prompt": "a survey please"}`, ws)
	p := e.problem(e.v3(owner, http.MethodPost, surveysPath("/generate"), vague), http.StatusBadRequest, "Prompt needs more detail")
	p.param(t, "prompt")

	p = e.problem(e.v3(owner, http.MethodPost, surveysPath("/generate"), body("")), http.StatusForbidden, "AI smart tools are disabled for this organization.")
	if p.Title != "AI Unavailable" || p.Code != "ai_smart_tools_disabled" {
		t.Fatalf("disabled %+v", p)
	}
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	for _, rec := range []*httptest.ResponseRecorder{
		e.v3(owner, http.MethodPost, surveysPath("/generate"), body(`, "type": "app", "languages": ["de-DE", "en-US"]`)),
		e.v3Key(e.apiKey(org, ws, "write"), http.MethodPost, surveysPath("/generate"), body("")),
	} {
		p := e.problem(rec, http.StatusServiceUnavailable, "AI is not configured for this Forma instance.")
		if p.Title != "AI Unavailable" || p.Code != "ai_instance_not_configured" {
			t.Fatalf("unavailable %+v", p)
		}
	}
	if e.count(`SELECT count(*) FROM surveys WHERE workspace_id = ?`, ws) != 0 {
		t.Fatal("generate created a survey")
	}
}

// translatedDocument is the translatable content of the survey created from linkSurvey, in the given languages, as members of a patch body.
func translatedDocument(created resource, languages ...string) string {
	tr := func(text string) string {
		parts := make([]string, len(languages))
		for i, l := range languages {
			parts[i] = fmt.Sprintf("%q: %q", l, text+" "+l)
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	blocks := created.Data["blocks"].([]any)
	blockID := func(i int) string { return blocks[i].(map[string]any)["id"].(string) }
	return fmt.Sprintf(`"metadata": {"title": %s}, "welcomeCard": {"enabled": true, "headline": %s},
	  "blocks": [
	    {"id": %q, "name": "Main", "elements": [{"id": "satisfaction", "type": "openText", "headline": %s, "required": true}]},
	    {"id": %q, "name": "Rate", "elements": [{"id": "rating", "type": "rating", "headline": %s, "required": false, "scale": "star", "range": 5}]}
	  ],
	  "endings": [{"id": "endingone", "type": "endScreen", "headline": %s}]`, tr("Feedback"), tr("Welcome"), blockID(0), tr("Improve?"), blockID(1), tr("Rate us"), tr("Thanks"))
}

// forma2's patch.test.ts "reserved names for newly declared fields": a name the survey does not already declare may not be reserved, and a refused patch writes nothing, not even a workspace language it asked for.
func TestV3PatchRefusesNewReservedNames(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	var created resource
	e.ok(e.v3(owner, http.MethodPost, surveysPath(""), linkSurvey(ws)), http.StatusCreated, &created)
	id := created.Data["id"].(string)
	path := surveysPath("/" + id)
	hidden := func() string { return e.text(`SELECT hidden_fields::text FROM surveys WHERE id = ?`, id) }
	before := hidden()

	p := e.problem(e.v3(owner, http.MethodPatch, path, `{"hiddenFields": {"enabled": true, "fieldIds": ["plan", "country"]}, "languages": [{"code": "de-DE", "enabled": true}, {"code": "fr-FR", "enabled": true}], `+translatedDocument(created, "en-US", "de-DE", "fr-FR")+`}`), http.StatusUnprocessableEntity, "")
	if got := p.param(t, "country"); got.Code != v3api.CodeForbiddenIdentifier || got.Identifier != "country" || !strings.Contains(got.Reason, `Field name "country" cannot be used`) {
		t.Fatalf("param %+v", got)
	}
	if hidden() != before || e.count(`SELECT count(*) FROM languages WHERE workspace_id = ? AND code = 'fr-FR'`, ws) != 0 {
		t.Fatal("a refused patch wrote")
	}
	p = e.problem(e.v3(owner, http.MethodPatch, path, `{"variables": [{"id": "varbrowser", "name": "browser", "type": "text", "value": ""}]}`), http.StatusUnprocessableEntity, "")
	if got := p.param(t, "browser"); got.Code != v3api.CodeForbiddenIdentifier {
		t.Fatalf("param %+v", got)
	}
	e.ok(e.v3(owner, http.MethodPatch, path, `{"hiddenFields": {"enabled": true, "fieldIds": ["plan", "team_size"]}}`), http.StatusOK, nil)

	// A survey that already declares a reserved name keeps it, but may not add another.
	e.exec(`UPDATE surveys SET hidden_fields = '{"enabled": true, "fieldIds": ["country"]}' WHERE id = ?`, id)
	e.ok(e.v3(owner, http.MethodPatch, path, `{"name": "Renamed"}`), http.StatusOK, nil)
	if !strings.Contains(hidden(), `"country"`) {
		t.Fatalf("grandfathered field dropped: %s", hidden())
	}
	p = e.problem(e.v3(owner, http.MethodPatch, path, `{"hiddenFields": {"enabled": true, "fieldIds": ["country", "url"]}}`), http.StatusUnprocessableEntity, "")
	p.param(t, "url")
}

// A patch's languages replace the survey's: a language it leaves out is unlinked, one it adds is linked (and created in the workspace when new), and a patch refused for another reason creates no workspace language.
func TestV3PatchReplacesLanguages(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	var created resource
	e.ok(e.v3(owner, http.MethodPost, surveysPath(""), linkSurvey(ws)), http.StatusCreated, &created)
	id := created.Data["id"].(string)
	path := surveysPath("/" + id)
	linked := func() []string {
		var codes []string
		if err := e.db.Raw(`SELECT l.code FROM survey_languages sl JOIN languages l ON l.id = sl.language_id WHERE sl.survey_id = ? ORDER BY l.code`, id).Scan(&codes).Error; err != nil {
			t.Fatal(err)
		}
		return codes
	}
	if got := strings.Join(linked(), ","); got != "de-DE,en-US" {
		t.Fatalf("languages %s", got)
	}

	var patched resource
	e.ok(e.v3(owner, http.MethodPatch, path, `{"languages": [{"code": "fr-FR", "enabled": true}], `+translatedDocument(created, "en-US", "fr-FR")+`}`), http.StatusOK, &patched)
	if got := strings.Join(linked(), ","); got != "en-US,fr-FR" {
		t.Fatalf("languages after replacing %s", got)
	}
	if e.count(`SELECT count(*) FROM languages WHERE workspace_id = ? AND code = 'fr-FR'`, ws) != 1 || e.count(`SELECT count(*) FROM languages WHERE workspace_id = ? AND code = 'de-DE'`, ws) != 1 {
		t.Fatal("workspace languages")
	}

	app := strings.Replace(linkSurvey(ws), `"name": " Product Feedback ",`, `"name": "App", "type": "app",`, 1)
	e.ok(e.v3(owner, http.MethodPost, surveysPath(""), app), http.StatusCreated, &created)
	p := e.problem(e.v3(owner, http.MethodPatch, surveysPath("/"+created.Data["id"].(string)), `{"languages": [{"code": "de-DE", "enabled": true}, {"code": "it-IT", "enabled": true}], `+translatedDocument(created, "en-US", "de-DE", "it-IT")+`, "distribution": {"triggers": [{"actionClassId": "`+uuid.NewString()+`"}]}}`), http.StatusUnprocessableEntity, "")
	if len(p.InvalidParams) == 0 || e.count(`SELECT count(*) FROM languages WHERE workspace_id = ? AND code = 'it-IT'`, ws) != 0 {
		t.Fatalf("a refused patch left a language: %+v", p)
	}
}
