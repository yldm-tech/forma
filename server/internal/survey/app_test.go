package survey

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/config"
)

func appPath(id any, rest string) string { return fmt.Sprintf("/api/app/surveys/%v%s", id, rest) }

// createLink creates a survey through v3 and returns its id.
func (e *env) createLink(user, ws uuid.UUID) string {
	e.t.Helper()
	var created resource
	e.ok(e.v3(user, http.MethodPost, surveysPath(""), linkSurvey(ws)), http.StatusCreated, &created)
	return created.Data["id"].(string)
}

// editorSurvey loads the survey as the editor holds it.
func (e *env) editorSurvey(user uuid.UUID, id string) map[string]any {
	e.t.Helper()
	var sv map[string]any
	e.ok(e.do(user, http.MethodGet, appPath(id, ""), nil), http.StatusOK, &sv)
	return sv
}

func TestEditorSave(t *testing.T) {
	e := newEnv(t)
	owner, reader := e.user(), e.user()
	org, ws := e.organization(owner)
	e.member(org, reader, "member")
	e.grant(org, ws, reader, "read")
	id := e.createLink(owner, ws)

	sv := e.editorSurvey(owner, id)
	if sv["workspaceId"] != ws.String() || len(sv["languages"].([]any)) != 2 || sv["segment"] != nil || len(sv["followUps"].([]any)) != 0 {
		t.Fatalf("editor survey %v", sv)
	}
	e.ok(e.do(reader, http.MethodGet, appPath(id, ""), nil), http.StatusOK, nil)
	e.fails(e.do(e.user(), http.MethodGet, appPath(id, ""), nil), http.StatusForbidden, apperr.CodeForbidden, "Not authorized")
	e.fails(e.do(owner, http.MethodGet, appPath(uuid.New(), ""), nil), http.StatusNotFound, apperr.CodeNotFound, "Survey not found")

	sv["name"] = "Edited"
	sv["pin"] = "1234"
	followUpID := uuid.NewString()
	sv["followUps"] = []any{map[string]any{"id": followUpID, "name": "Mail me", "trigger": map[string]any{"type": "response", "properties": nil},
		"action": map[string]any{"type": "send-email", "properties": map[string]any{"to": "boss@example.com", "from": "noreply@example.com", "replyTo": []any{"me@example.com"}, "subject": "New", "body": "<p>Hi</p>", "attachResponseData": false}}}}
	var saved map[string]any
	e.ok(e.do(owner, http.MethodPut, appPath(id, ""), sv), http.StatusOK, &saved)
	if saved["name"] != "Edited" || saved["pin"] != "1234" || len(saved["followUps"].([]any)) != 1 {
		t.Fatalf("saved %v", saved)
	}
	e.fails(e.do(reader, http.MethodPut, appPath(id, ""), sv), http.StatusForbidden, apperr.CodeForbidden, "")

	sv["pin"] = "12"
	body := e.fails(e.do(owner, http.MethodPut, appPath(id, ""), sv), http.StatusBadRequest, apperr.CodeValidation, "PIN must be a four digit number")
	if body.Details["issues"] == nil {
		t.Fatal("validation issues missing")
	}
	sv["pin"] = nil
	// A translation missing for an enabled language is a document rule.
	var blocks []map[string]any
	raw, _ := json.Marshal(sv["blocks"])
	_ = json.Unmarshal(raw, &blocks)
	headline := blocks[0]["elements"].([]any)[0].(map[string]any)["headline"].(map[string]any)
	delete(headline, "de-DE")
	sv["blocks"] = blocks
	e.fails(e.do(owner, http.MethodPut, appPath(id, ""), sv), http.StatusBadRequest, apperr.CodeValidation, "The headline in question 1 of block 1 is not present for the following languages: de-DE")

	// A draft save skips document rules, but only while the survey stays a draft.
	sv["updatedAt"] = saved["updatedAt"]
	e.ok(e.do(owner, http.MethodPut, appPath(id, "/draft"), sv), http.StatusOK, &saved)
	stale := sv
	stale["updatedAt"] = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	body = e.fails(e.do(owner, http.MethodPut, appPath(id, "/draft"), stale), http.StatusForbidden, apperr.CodeOperationNotAllowed, "")
	if !strings.HasPrefix(body.Message, "survey_modified_elsewhere:") {
		t.Fatalf("conflict %q", body.Message)
	}
	sv["updatedAt"] = saved["updatedAt"]
	sv["status"] = "inProgress"
	e.fails(e.do(owner, http.MethodPut, appPath(id, "/draft"), sv), http.StatusBadRequest, apperr.CodeValidation, `Invalid input: expected "draft"`)

	// Newly declared names are guarded on every save, a draft one included.
	sv["status"] = "draft"
	sv["hiddenFields"] = map[string]any{"enabled": true, "fieldIds": []any{"plan", "lang"}}
	body = e.fails(e.do(owner, http.MethodPut, appPath(id, "/draft"), sv), http.StatusBadRequest, apperr.CodeBadRequest, "")
	if !strings.Contains(body.Message, `Field name "lang" cannot be used`) {
		t.Fatalf("guard %q", body.Message)
	}

	sv["hiddenFields"] = map[string]any{"enabled": true, "fieldIds": []any{"plan"}}
	sv["recaptcha"] = map[string]any{"enabled": true, "threshold": 0.5}
	e.fails(e.do(owner, http.MethodPut, appPath(id, "/draft"), sv), http.StatusForbidden, apperr.CodeOperationNotAllowed, "Spam protection is not enabled for this organization")

	e.exec(`UPDATE surveys SET archived_at = now() WHERE id = ?`, id)
	sv["recaptcha"] = nil
	e.fails(e.do(owner, http.MethodPut, appPath(id, "/draft"), sv), http.StatusBadRequest, apperr.CodeBadRequest, "This survey is archived. Restore it before editing.")
}

func TestEditorSaveSchedulesAndAppSegment(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	trigger := e.actionClass(ws, "open")
	id := e.createLink(owner, ws)
	sv := e.editorSurvey(owner, id)

	tomorrow := time.Now().Add(24 * time.Hour)
	later := time.Now().Add(72 * time.Hour)
	sv["status"] = "paused"
	sv["publishOn"] = tomorrow.Format(time.RFC3339)
	sv["closeOn"] = later.Format(time.RFC3339)
	var saved Survey
	e.ok(e.do(owner, http.MethodPut, appPath(id, ""), sv), http.StatusOK, &saved)
	berlin, _ := time.LoadLocation("Europe/Berlin")
	if saved.PublishOn == nil || saved.PublishOn.In(berlin).Hour() != 0 || saved.PublishOn.In(berlin).Day() != tomorrow.In(berlin).Day() || saved.CloseOn == nil {
		t.Fatalf("schedule %v %v", saved.PublishOn, saved.CloseOn)
	}
	sv["closeOn"] = tomorrow.Format(time.RFC3339)
	e.fails(e.do(owner, http.MethodPut, appPath(id, ""), sv), http.StatusBadRequest, apperr.CodeValidation, "Close date must be after publish date")
	sv["status"], sv["publishOn"], sv["closeOn"] = "inProgress", tomorrow.Format(time.RFC3339), nil
	e.ok(e.do(owner, http.MethodPut, appPath(id, ""), sv), http.StatusOK, &saved)
	if saved.PublishOn != nil {
		t.Fatal("publishing by hand kept the publish date")
	}

	sv = e.editorSurvey(owner, id)
	sv["type"] = "app"
	e.fails(e.do(owner, http.MethodPut, appPath(id, ""), sv), http.StatusBadRequest, apperr.CodeBadRequest, appTriggerMessage)
	sv["triggers"] = []any{map[string]any{"actionClass": map[string]any{"id": trigger.String()}}}
	e.ok(e.do(owner, http.MethodPut, appPath(id, ""), sv), http.StatusOK, &saved)
	if saved.Segment == nil || !saved.Segment.IsPrivate || saved.Segment.Title != id || len(saved.Triggers) != 1 {
		t.Fatalf("app survey %+v", saved)
	}
	sv = e.editorSurvey(owner, id)
	sv["triggers"] = []any{map[string]any{"actionClass": map[string]any{"id": uuid.NewString()}}}
	e.fails(e.do(owner, http.MethodPut, appPath(id, ""), sv), http.StatusBadRequest, apperr.CodeBadRequest, "Invalid trigger id")

	public := e.insert(`INSERT INTO segments (workspace_id, title, is_private) VALUES (?, 'Pros', false) RETURNING id`, ws)
	e.ok(e.do(owner, http.MethodPut, appPath(id, "/segment"), map[string]any{"segmentId": public}), http.StatusOK, &saved)
	if saved.Segment == nil || saved.Segment.ID != public || e.count(`SELECT count(*) FROM segments WHERE title = ?`, id) != 0 {
		t.Fatal("loading a segment kept the private one")
	}
	other, _ := e.organization(e.user())
	foreignWS := e.workspace(other)
	foreign := e.insert(`INSERT INTO segments (workspace_id, title) VALUES (?, 'Theirs') RETURNING id`, foreignWS)
	e.fails(e.do(owner, http.MethodPut, appPath(id, "/segment"), map[string]any{"segmentId": foreign}), http.StatusBadRequest, CodeWorkspaceMismatch, "Segment and survey are not in the same workspace")
	e.fails(e.do(owner, http.MethodPut, appPath(id, "/segment"), map[string]any{"segmentId": uuid.NewString()}), http.StatusNotFound, apperr.CodeNotFound, "")
	another := e.insert(`INSERT INTO segments (workspace_id, title, is_private) VALUES (?, 'Others', false) RETURNING id`, ws)
	e.ok(e.do(owner, http.MethodPut, appPath(id, "/segment"), map[string]any{"segmentId": another}), http.StatusOK, &saved)
	if saved.Segment == nil || saved.Segment.ID != another || e.count(`SELECT count(*) FROM segments WHERE id = ?`, public) != 1 {
		t.Fatal("switching away from a shared segment deleted it")
	}

	sv = e.editorSurvey(owner, id)
	sv["type"] = "link"
	e.ok(e.do(owner, http.MethodPut, appPath(id, ""), sv), http.StatusOK, &saved)
	if saved.Segment != nil || e.count(`SELECT count(*) FROM segments WHERE id = ?`, public) != 1 {
		t.Fatal("a link survey kept its public segment, or deleted it")
	}
}

func TestCopyAndDuplicate(t *testing.T) {
	e := newEnv(t)
	owner, writer := e.user(), e.user()
	org, ws := e.organization(owner)
	target := e.workspace(org)
	e.member(org, writer, "member")
	e.grant(org, ws, writer, "readWrite")
	trigger := e.actionClass(ws, "checkout")
	e.exec(`INSERT INTO action_classes (workspace_id, name, type, key) VALUES (?, 'checkout', 'code', 'other-key')`, target)

	var created resource
	app := strings.Replace(linkSurvey(ws), `"name": " Product Feedback ",`, `"name": "Survey", "type": "app", "distribution": {"triggers": [{"actionClassId": "`+trigger.String()+`"}]},`, 1)
	e.ok(e.v3(owner, http.MethodPost, surveysPath(""), app), http.StatusCreated, &created)
	id := created.Data["id"].(string)
	e.exec(`UPDATE surveys SET pin = '1234', slug = 'my-survey' WHERE id = ?`, id)
	e.exec(`INSERT INTO survey_quotas (survey_id, name, "limit", action) VALUES (?, 'Q', 5, 'continueSurvey')`, id)

	var dup Survey
	e.ok(e.do(owner, http.MethodPost, appPath(id, "/copy"), map[string]any{"targetWorkspaceId": ws}), http.StatusCreated, &dup)
	if dup.Name != "Survey (copy)" || dup.Status != StatusDraft || dup.Pin != nil || dup.Slug != nil || dup.WorkspaceID != ws ||
		len(dup.Triggers) != 1 || dup.Triggers[0].ActionClass.ID != trigger || dup.Segment == nil || dup.Segment.Title != dup.ID.String() || len(dup.Languages) != 2 {
		t.Fatalf("duplicate %+v", dup)
	}
	if e.count(`SELECT count(*) FROM survey_quotas WHERE survey_id = ?`, dup.ID) != 1 || e.count(`SELECT count(*) FROM survey_embedded_data WHERE survey_id = ?`, dup.ID) != 2 {
		t.Fatal("quotas or embedded data not copied")
	}

	var copied Survey
	e.ok(e.do(owner, http.MethodPost, appPath(id, "/copy"), map[string]any{"targetWorkspaceId": target}), http.StatusCreated, &copied)
	if copied.WorkspaceID != target || len(copied.Triggers) != 1 || copied.Triggers[0].ActionClass.Name != "checkout (copy)" || copied.Triggers[0].ActionClass.WorkspaceID != target {
		t.Fatalf("copy %+v", copied)
	}
	if e.count(`SELECT count(*) FROM languages WHERE workspace_id = ? AND code = 'de-DE'`, target) != 1 {
		t.Fatal("languages not created in the target")
	}

	e.fails(e.do(writer, http.MethodPost, appPath(id, "/copy"), map[string]any{"targetWorkspaceId": target}), http.StatusForbidden, apperr.CodeForbidden, "")
	otherOrg, otherWS := e.organization(owner)
	_ = otherOrg
	e.fails(e.do(owner, http.MethodPost, appPath(id, "/copy"), map[string]any{"targetWorkspaceId": otherWS}), http.StatusForbidden, apperr.CodeOperationNotAllowed, "Source and target workspaces must be in the same organization")
}

func TestSlugSingleUseAndQuotas(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	id := e.createLink(owner, ws)
	other := e.createLink(owner, ws)

	var sv Survey
	e.ok(e.do(owner, http.MethodPut, appPath(id, "/slug"), map[string]any{"slug": "feedback-2026"}), http.StatusOK, &sv)
	if sv.Slug == nil || *sv.Slug != "feedback-2026" {
		t.Fatalf("slug %v", sv.Slug)
	}
	e.fails(e.do(owner, http.MethodPut, appPath(other, "/slug"), map[string]any{"slug": "feedback-2026"}), http.StatusBadRequest, apperr.CodeConflict, "A survey with this slug already exists")
	e.fails(e.do(owner, http.MethodPut, appPath(other, "/slug"), map[string]any{"slug": "Bad Slug"}), http.StatusBadRequest, apperr.CodeValidation, "Slug can only contain lowercase letters, numbers, and hyphens")
	e.ok(e.do(owner, http.MethodDelete, appPath(id, "/slug"), nil), http.StatusOK, &sv)
	if sv.Slug != nil {
		t.Fatal("slug not removed")
	}

	var links struct {
		Links []SingleUseLink `json:"links"`
	}
	e.ok(e.do(owner, http.MethodPost, appPath(id, "/single-use-links"), map[string]any{"isEncrypted": false, "count": 3}), http.StatusOK, &links)
	if len(links.Links) != 3 || links.Links[0].SuToken != SingleUseSignature(e.cfg.EncryptionKey, uuid.MustParse(id), links.Links[0].SuID) {
		t.Fatalf("plain links %+v", links)
	}
	links.Links = nil
	e.ok(e.do(owner, http.MethodPost, appPath(id, "/single-use-links"), map[string]any{"isEncrypted": true, "count": 2}), http.StatusOK, &links)
	if len(links.Links) != 2 || links.Links[0].SuToken != "" || links.Links[0].SuID == links.Links[1].SuID {
		t.Fatalf("encrypted links %+v", links)
	}
	links.Links = nil
	e.ok(e.do(owner, http.MethodPost, appPath(id, "/single-use-links"), map[string]any{"isEncrypted": false, "singleUseId": " custom "}), http.StatusOK, &links)
	if links.Links[0].SuID != "custom" {
		t.Fatalf("custom link %+v", links)
	}
	e.fails(e.do(owner, http.MethodPost, appPath(id, "/single-use-links"), map[string]any{"isEncrypted": true, "singleUseId": "custom"}), http.StatusBadRequest, apperr.CodeValidation, "Custom single-use IDs can only be generated one at a time without encryption")
	e.fails(e.do(owner, http.MethodPost, appPath(id, "/single-use-links"), map[string]any{"isEncrypted": false, "count": 5001}), http.StatusBadRequest, apperr.CodeValidation, "")

	quota := map[string]any{"name": "Pros", "limit": 10, "logic": map[string]any{"connector": "and", "conditions": []any{}}, "action": "endSurvey", "endingCardId": "endingone", "countPartialSubmissions": false}
	var q Quota
	e.ok(e.do(owner, http.MethodPost, appPath(id, "/quotas"), quota), http.StatusCreated, &q)
	e.fails(e.do(owner, http.MethodPost, appPath(id, "/quotas"), quota), http.StatusBadRequest, apperr.CodeBadRequest, "Quota with this name already exists")
	quota["endingCardId"] = nil
	e.fails(e.do(owner, http.MethodPost, appPath(id, "/quotas"), quota), http.StatusBadRequest, apperr.CodeValidation, "endingCardId is required when action is endSurvey")
	quota["action"], quota["limit"], quota["name"] = "continueSurvey", 20, "Renamed"
	e.ok(e.do(owner, http.MethodPatch, "/api/app/quotas/"+q.ID.String(), quota), http.StatusOK, &q)
	if q.Name != "Renamed" || q.Limit != 20 || q.EndingCardID != nil || q.SurveyID.String() != id {
		t.Fatalf("updated quota %+v", q)
	}
	var count struct {
		Count int `json:"count"`
	}
	e.ok(e.do(owner, http.MethodGet, "/api/app/quotas/"+q.ID.String()+"/response-count", nil), http.StatusOK, &count)
	var list struct {
		Quotas []Quota `json:"quotas"`
	}
	e.ok(e.do(owner, http.MethodGet, appPath(id, "/quotas"), nil), http.StatusOK, &list)
	if len(list.Quotas) != 1 || count.Count != 0 {
		t.Fatalf("quotas %+v", list)
	}
	e.fails(e.do(e.user(), http.MethodDelete, "/api/app/quotas/"+q.ID.String(), nil), http.StatusForbidden, apperr.CodeForbidden, "")
	rec := e.do(owner, http.MethodDelete, "/api/app/quotas/"+q.ID.String(), nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete quota %d", rec.Code)
	}
	e.fails(e.do(owner, http.MethodDelete, "/api/app/quotas/"+q.ID.String(), nil), http.StatusNotFound, apperr.CodeNotFound, "Quota not found")
}

func TestSlugsAreSelfHostedOnly(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.IsFormaCloud = true })
	owner := e.user()
	_, ws := e.organization(owner)
	id := e.createLink(owner, ws)
	e.fails(e.do(owner, http.MethodPut, appPath(id, "/slug"), map[string]any{"slug": "x"}), http.StatusForbidden, apperr.CodeOperationNotAllowed, "Pretty URLs are only available on self-hosted instances")
}

func TestTemplates(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	e.exec(`UPDATE users SET locale = 'ja-JP' WHERE id = ?`, owner)
	var out struct {
		Templates     []Template `json:"templates"`
		XMTemplateIDs []string   `json:"xmTemplateIds"`
	}
	e.ok(e.do(owner, http.MethodGet, "/api/app/workspaces/"+ws.String()+"/survey-templates", nil), http.StatusOK, &out)
	if len(out.Templates) != 53 || out.Templates[0].ID != CustomTemplateID || len(out.XMTemplateIDs) != 6 || strings.Contains(string(out.Templates[1].Preset), "{{") {
		t.Fatalf("templates %d %+v", len(out.Templates), out.Templates[0])
	}

	template := func(id, source string) map[string]any {
		return map[string]any{"workspaceId": ws, "templateId": id, "source": source, "surveyType": "link", "defaultLanguage": "de-DE"}
	}
	var created resource
	e.ok(e.v3(owner, http.MethodPost, surveysPath("/templates"), template("nps", "xm")), http.StatusCreated, &created)
	if created.Data["defaultLanguage"] != "de-DE" || created.Data["status"] != "draft" || len(created.Data["blocks"].([]any)) == 0 {
		t.Fatalf("template survey %v", created.Data)
	}
	e.ok(e.v3(owner, http.MethodPost, surveysPath("/templates"), template("custom", "custom")), http.StatusCreated, &created)
	p := e.problem(e.v3(owner, http.MethodPost, surveysPath("/templates"), template("cart-abandonment", "xm")), http.StatusBadRequest, "Template not found")
	if p.param(t, "templateId").Reason != "Unknown template for the requested source" {
		t.Fatalf("%+v", p)
	}
	e.problem(e.v3(owner, http.MethodPost, surveysPath("/templates"), template("custom", "catalog")), http.StatusBadRequest, "Template not found")
	// Every template of the catalog makes a valid survey, in every language the catalog is translated into.
	for i, tpl := range out.Templates {
		source := "catalog"
		switch {
		case tpl.ID == CustomTemplateID:
			source = "custom"
		case slices.Contains(XMTemplateIDs, tpl.ID):
			source = "xm"
		}
		body := template(tpl.ID, source)
		body["defaultLanguage"] = catalogLocales[i%len(catalogLocales)]
		rec := e.v3(owner, http.MethodPost, surveysPath("/templates"), body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("template %s: %d %s", tpl.ID, rec.Code, rec.Body.String())
		}
	}
}

// The survey links the list and the editor build start at PUBLIC_URL when it is set, else at WEBAPP_URL, as forma2's getPublicDomain did.
func TestPublicDomain(t *testing.T) {
	for name, tc := range map[string]struct {
		publicURL string
		want      string
	}{
		"web app":    {"", webApp},
		"public url": {"https://surveys.example.com/", "https://surveys.example.com"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, func(c *config.Config) {
				if tc.publicURL != "" {
					c.PublicURL, _ = url.Parse(tc.publicURL)
				}
			})
			var out struct {
				PublicDomain string `json:"publicDomain"`
			}
			e.ok(e.do(e.user(), http.MethodGet, "/api/app/public-domain", nil), http.StatusOK, &out)
			if out.PublicDomain != tc.want {
				t.Fatalf("public domain %q, want %q", out.PublicDomain, tc.want)
			}
			e.fails(e.request(http.MethodGet, "/api/app/public-domain", nil, nil), http.StatusUnauthorized, apperr.CodeNotAuthenticated, "")
		})
	}
}
