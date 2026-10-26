package survey

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
)

func templatePath(id any, rest string) string {
	return fmt.Sprintf("/api/app/survey-templates/%v%s", id, rest)
}

type savedTemplate struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organizationId"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Creator        *struct {
		Name string `json:"name"`
	} `json:"creator"`
	QuestionCount int `json:"questionCount"`
	Languages     []struct {
		Code    string `json:"code"`
		Default bool   `json:"default"`
		Enabled bool   `json:"enabled"`
	} `json:"languages"`
	Preset map[string]json.RawMessage `json:"preset"`
}

// Owners and managers save a survey as a template of its organization; whoever can write a workspace of the organization creates a draft from it there, with the document, translations and declared fields of the survey as it was saved.
func TestOrganizationTemplates(t *testing.T) {
	e := newEnv(t)
	owner, manager, writer, reader, outsider, biller := e.user(), e.user(), e.user(), e.user(), e.user(), e.user()
	org, ws := e.organization(owner)
	other := e.workspace(org)
	e.member(org, manager, "manager")
	e.member(org, writer, "member")
	e.member(org, reader, "member")
	e.member(org, biller, "billing")
	e.grant(org, ws, writer, "readWrite")
	e.grant(org, other, writer, "readWrite")
	e.grant(org, other, reader, "read")
	foreignOrg, foreignWS := e.organization(outsider)
	id := e.createLink(owner, ws)

	save := func(user uuid.UUID, body any) *savedTemplate {
		t.Helper()
		var out savedTemplate
		e.ok(e.do(user, http.MethodPost, appPath(id, "/save-as-template"), body), http.StatusCreated, &out)
		return &out
	}

	// Only an owner or a manager saves; writing the workspace is not enough.
	e.fails(e.do(writer, http.MethodPost, appPath(id, "/save-as-template"), map[string]any{"name": "NPS"}), http.StatusForbidden, apperr.CodeForbidden, "")
	e.fails(e.do(outsider, http.MethodPost, appPath(id, "/save-as-template"), map[string]any{"name": "NPS"}), http.StatusForbidden, apperr.CodeForbidden, "")
	e.fails(e.do(manager, http.MethodPost, appPath(uuid.New(), "/save-as-template"), map[string]any{"name": "NPS"}), http.StatusNotFound, apperr.CodeNotFound, "Survey not found")
	e.fails(e.do(manager, http.MethodPost, appPath(id, "/save-as-template"), map[string]any{"name": "   "}), http.StatusBadRequest, apperr.CodeValidation, "")
	e.fails(e.do(manager, http.MethodPost, appPath(id, "/save-as-template"), map[string]any{"name": strings.Repeat("n", maxTemplateNameLength+1)}), http.StatusBadRequest, apperr.CodeValidation, "")
	e.fails(e.do(manager, http.MethodPost, appPath(id, "/save-as-template"), map[string]any{"name": "NPS", "description": strings.Repeat("d", maxTemplateDescriptionLength+1)}), http.StatusBadRequest, apperr.CodeValidation, "")
	if n := e.count(`SELECT count(*) FROM survey_templates`); n != 0 {
		t.Fatalf("%d templates saved by refused requests", n)
	}

	tpl := save(manager, map[string]any{"name": "  Release feedback ", "description": " Every release "})
	if tpl.Name != "Release feedback" || tpl.Description != "Every release" || tpl.OrganizationID != org.String() || tpl.Creator == nil || tpl.Creator.Name != "Ada" || tpl.QuestionCount != 2 || tpl.Preset != nil {
		t.Fatalf("saved %+v", tpl)
	}
	if len(tpl.Languages) != 2 || tpl.Languages[0].Code != "en-US" || !tpl.Languages[0].Default || tpl.Languages[1].Code != "de-DE" || tpl.Languages[1].Default || !tpl.Languages[1].Enabled {
		t.Fatalf("languages %+v", tpl.Languages)
	}
	e.expectAudit(audit.ActionCreated, audit.StatusSuccess, audit.ActorUser, manager.String(), audit.TargetSurveyTemplate, tpl.ID)

	// The template is a copy: later edits of the survey do not reach it.
	e.exec(`UPDATE surveys SET blocks = '[]' WHERE id = ?`, id)

	// Every member of the organization except the billing role lists and reads them; only owners and managers manage them.
	var list OrganizationTemplates
	e.ok(e.do(reader, http.MethodGet, "/api/app/organizations/"+org.String()+"/survey-templates", nil), http.StatusOK, &list)
	if len(list.Templates) != 1 || list.Templates[0].ID.String() != tpl.ID || list.Templates[0].Preset != nil || list.CanManage {
		t.Fatalf("member's list %+v", list)
	}
	e.ok(e.do(manager, http.MethodGet, "/api/app/organizations/"+org.String()+"/survey-templates", nil), http.StatusOK, &list)
	if !list.CanManage {
		t.Fatal("a manager may not manage templates")
	}
	e.fails(e.do(outsider, http.MethodGet, "/api/app/organizations/"+org.String()+"/survey-templates", nil), http.StatusForbidden, apperr.CodeForbidden, "")
	e.fails(e.do(biller, http.MethodGet, "/api/app/organizations/"+org.String()+"/survey-templates", nil), http.StatusForbidden, apperr.CodeForbidden, "")
	var foreignList OrganizationTemplates
	e.ok(e.do(outsider, http.MethodGet, "/api/app/organizations/"+foreignOrg.String()+"/survey-templates", nil), http.StatusOK, &foreignList)
	if len(foreignList.Templates) != 0 || !foreignList.CanManage {
		t.Fatalf("another organization's list %+v", foreignList)
	}

	var full savedTemplate
	e.ok(e.do(reader, http.MethodGet, templatePath(tpl.ID, ""), nil), http.StatusOK, &full)
	var blocks []map[string]any
	if err := json.Unmarshal(full.Preset["blocks"], &blocks); err != nil || len(blocks) != 2 {
		t.Fatalf("preset blocks %s: %v", full.Preset["blocks"], err)
	}
	headline := blocks[0]["elements"].([]any)[0].(map[string]any)["headline"].(map[string]any)
	if headline["default"] != "What should we improve?" || headline["de-DE"] != "Was sollen wir verbessern?" {
		t.Fatalf("headline %v", headline)
	}
	e.fails(e.do(outsider, http.MethodGet, templatePath(tpl.ID, ""), nil), http.StatusForbidden, apperr.CodeForbidden, "")
	e.fails(e.do(biller, http.MethodGet, templatePath(tpl.ID, ""), nil), http.StatusForbidden, apperr.CodeForbidden, "")
	e.fails(e.do(reader, http.MethodGet, templatePath(uuid.New(), ""), nil), http.StatusNotFound, apperr.CodeNotFound, "Survey template not found")

	// Using it needs write access to the workspace the draft goes to, in the template's organization.
	use := map[string]any{"workspaceId": other, "surveyType": "app"}
	e.fails(e.do(reader, http.MethodPost, templatePath(tpl.ID, "/surveys"), use), http.StatusForbidden, apperr.CodeForbidden, "")
	e.fails(e.do(outsider, http.MethodPost, templatePath(tpl.ID, "/surveys"), map[string]any{"workspaceId": foreignWS, "surveyType": "link"}), http.StatusNotFound, apperr.CodeNotFound, "Survey template not found")
	e.fails(e.do(writer, http.MethodPost, templatePath(tpl.ID, "/surveys"), map[string]any{"workspaceId": other, "surveyType": "email"}), http.StatusBadRequest, apperr.CodeValidation, "")
	e.fails(e.do(writer, http.MethodPost, templatePath(uuid.New(), "/surveys"), use), http.StatusNotFound, apperr.CodeNotFound, "Survey template not found")

	var created map[string]any
	e.ok(e.do(writer, http.MethodPost, templatePath(tpl.ID, "/surveys"), use), http.StatusCreated, &created)
	if created["name"] != "Release feedback" || created["type"] != "app" || created["status"] != "draft" || created["workspaceId"] != other.String() || created["createdBy"] != writer.String() || created["segment"] == nil {
		t.Fatalf("created %v", created)
	}
	languages := created["languages"].([]any)
	if len(languages) != 2 || languages[0].(map[string]any)["language"].(map[string]any)["code"] != "en-US" || languages[0].(map[string]any)["language"].(map[string]any)["workspaceId"] != other.String() || languages[1].(map[string]any)["language"].(map[string]any)["code"] != "de-DE" {
		t.Fatalf("languages %v", languages)
	}
	if got := created["blocks"].([]any); len(got) != 2 {
		t.Fatalf("blocks %v", got)
	}
	assertEmbeddedFields(t, created["embeddedFields"], "score:computed", "plan:ingested")
	e.expectAudit(audit.ActionCreated, audit.StatusSuccess, audit.ActorUser, writer.String(), audit.TargetSurvey, created["id"].(string))
	// The created draft is an ordinary survey the editor saves.
	e.ok(e.do(writer, http.MethodPut, appPath(created["id"], "/draft"), e.editorSurvey(writer, created["id"].(string))), http.StatusOK, nil)

	// Renaming and deleting are for owners and managers; a deleted template leaves the surveys made from it.
	e.fails(e.do(writer, http.MethodPatch, templatePath(tpl.ID, ""), map[string]any{"name": "Mine"}), http.StatusForbidden, apperr.CodeForbidden, "")
	e.fails(e.do(owner, http.MethodPatch, templatePath(tpl.ID, ""), map[string]any{"name": ""}), http.StatusBadRequest, apperr.CodeValidation, "")
	var renamed savedTemplate
	e.ok(e.do(owner, http.MethodPatch, templatePath(tpl.ID, ""), map[string]any{"name": "Quarterly feedback", "description": ""}), http.StatusOK, &renamed)
	if renamed.Name != "Quarterly feedback" || renamed.Description != "" || renamed.Creator == nil || renamed.Preset != nil {
		t.Fatalf("renamed %+v", renamed)
	}
	e.expectAudit(audit.ActionUpdated, audit.StatusSuccess, audit.ActorUser, owner.String(), audit.TargetSurveyTemplate, tpl.ID)
	e.fails(e.do(writer, http.MethodDelete, templatePath(tpl.ID, ""), nil), http.StatusForbidden, apperr.CodeForbidden, "")
	if rec := e.do(manager, http.MethodDelete, templatePath(tpl.ID, ""), nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete %d %s", rec.Code, rec.Body.String())
	}
	e.expectAudit(audit.ActionDeleted, audit.StatusSuccess, audit.ActorUser, manager.String(), audit.TargetSurveyTemplate, tpl.ID)
	e.fails(e.do(manager, http.MethodDelete, templatePath(tpl.ID, ""), nil), http.StatusNotFound, apperr.CodeNotFound, "Survey template not found")
	e.fails(e.do(writer, http.MethodPost, templatePath(tpl.ID, "/surveys"), use), http.StatusNotFound, apperr.CodeNotFound, "Survey template not found")
	if e.count(`SELECT count(*) FROM surveys WHERE id = ?`, created["id"]) != 1 {
		t.Fatal("deleting the template deleted the survey made from it")
	}

	// A template keeps no account: deleting who saved it leaves it without a creator.
	again := save(manager, map[string]any{"name": "Again"})
	e.exec(`DELETE FROM users WHERE id = ?`, manager)
	e.ok(e.do(owner, http.MethodGet, templatePath(again.ID, ""), nil), http.StatusOK, &full)
	if full.Creator != nil {
		t.Fatalf("creator %+v", full.Creator)
	}
}

// An organization keeps a bounded number of templates.
func TestOrganizationTemplateLimit(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	org, ws := e.organization(owner)
	id := e.createLink(owner, ws)
	e.exec(`INSERT INTO survey_templates (organization_id, name, welcome_card, blocks, endings, hidden_fields, variables)
		SELECT ?, 'T' || n, '{"enabled": false}', '[]', '[]', '{"enabled": false}', '[]' FROM generate_series(1, ?) n`, org, maxOrganizationTemplates)
	e.fails(e.do(owner, http.MethodPost, appPath(id, "/save-as-template"), map[string]any{"name": "One more"}), http.StatusConflict, apperr.CodeConflict, "")
	e.exec(`DELETE FROM survey_templates WHERE name = 'T1'`)
	e.ok(e.do(owner, http.MethodPost, appPath(id, "/save-as-template"), map[string]any{"name": "One more"}), http.StatusCreated, nil)
}

// A template saved from a draft does not carry the editor's draft markers.
func TestSaveAsTemplateStripsDraftMarkers(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	id := e.createLink(owner, ws)
	e.exec(`UPDATE surveys SET blocks = jsonb_set(blocks, '{0,elements,0,isDraft}', 'true') WHERE id = ?`, id)
	var tpl savedTemplate
	e.ok(e.do(owner, http.MethodPost, appPath(id, "/save-as-template"), map[string]any{"name": "Draft"}), http.StatusCreated, &tpl)
	if n := e.count(`SELECT count(*) FROM survey_templates WHERE id = ? AND blocks::text LIKE '%isDraft%'`, tpl.ID); n != 0 {
		t.Fatal("the template kept isDraft")
	}
}

// A template keeps the settings that change how respondents move through the survey, and a draft made from it has them.
func TestOrganizationTemplateKeepsRespondentSettings(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	id := e.createLink(owner, ws)
	e.exec(`UPDATE surveys SET show_language_switch = true, is_back_button_hidden = true, is_auto_progressing_enabled = true WHERE id = ?`, id)
	var tpl savedTemplate
	e.ok(e.do(owner, http.MethodPost, appPath(id, "/save-as-template"), map[string]any{"name": "Settings"}), http.StatusCreated, &tpl)
	var full savedTemplate
	e.ok(e.do(owner, http.MethodGet, templatePath(tpl.ID, ""), nil), http.StatusOK, &full)
	for _, key := range []string{"showLanguageSwitch", "isBackButtonHidden", "isAutoProgressingEnabled"} {
		if string(full.Preset[key]) != "true" {
			t.Errorf("preset %s = %s", key, full.Preset[key])
		}
	}
	var created map[string]any
	e.ok(e.do(owner, http.MethodPost, templatePath(tpl.ID, "/surveys"), map[string]any{"workspaceId": ws, "surveyType": "link"}), http.StatusCreated, &created)
	if created["showLanguageSwitch"] != true || created["isBackButtonHidden"] != true || created["isAutoProgressingEnabled"] != true {
		t.Fatalf("created showLanguageSwitch=%v isBackButtonHidden=%v isAutoProgressingEnabled=%v", created["showLanguageSwitch"], created["isBackButtonHidden"], created["isAutoProgressingEnabled"])
	}
}

// A create from a template that fails leaves no language it would have added to the workspace.
func TestOrganizationTemplateFailedCreateLeavesNoLanguages(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	org, ws := e.organization(owner)
	// A variable and a hidden field share a storage key, which only the create's transaction finds.
	tpl := e.insert(`INSERT INTO survey_templates (organization_id, name, welcome_card, blocks, endings, hidden_fields, variables, languages)
		VALUES (?, 'Clash', '{"enabled": false}', '[]', '[]', '{"enabled": true, "fieldIds": ["dup"]}', '[{"id": "dup", "name": "dup", "type": "text", "value": ""}]',
		'[{"code": "fr-FR", "alias": null, "default": false, "enabled": true}]') RETURNING id`, org)
	e.fails(e.do(owner, http.MethodPost, templatePath(tpl, "/surveys"), map[string]any{"workspaceId": ws, "surveyType": "link"}), http.StatusBadRequest, apperr.CodeBadRequest, "")
	if n := e.count(`SELECT count(*) FROM languages WHERE workspace_id = ? AND code = 'fr-FR'`, ws); n != 0 {
		t.Fatalf("%d languages left by a failed create", n)
	}
}
