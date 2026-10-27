package survey

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/v3api"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
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

// assertEmbeddedFields checks a survey payload's embeddedFields as "name:source" pairs in order; a hidden field is keyed by its name.
func assertEmbeddedFields(t *testing.T, raw any, want ...string) {
	t.Helper()
	list, ok := raw.([]any)
	if !ok {
		t.Fatalf("embeddedFields %#v is not a list", raw)
	}
	got := make([]string, 0, len(list))
	for _, item := range list {
		f := item.(map[string]any)
		def, link := f["field"].(map[string]any), f["link"].(map[string]any)
		got = append(got, fmt.Sprintf("%v:%v", def["name"], def["source"]))
		if def["source"] == "ingested" && def["name"] != link["storageKey"] {
			t.Fatalf("hidden field %v", f)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("embeddedFields %v, want %v", got, want)
	}
}

// followUpTo is a follow-up of the editor's shape sending to `to`.
func followUpTo(id, to string) map[string]any {
	return map[string]any{"id": id, "name": "Mail", "trigger": map[string]any{"type": "response", "properties": nil},
		"action": map[string]any{"type": "send-email", "properties": map[string]any{"to": to, "from": "noreply@example.com", "replyTo": []any{}, "subject": "New", "body": "<p>Hi</p>", "attachResponseData": true}}}
}

// A follow-up mails each finished response to its recipient, so one addressed by literal address may only name someone with access to the workspace, as a workflow's send_email may, whoever saves it: an editor or an API key cannot route responses to an outside account. A recipient a save does not change is not checked again, so a survey holding one forma2 accepted can still be saved.
func TestFollowUpRecipients(t *testing.T) {
	e := newEnv(t)
	owner, contributor := e.user(), e.user()
	org, ws := e.organization(owner)
	e.member(org, contributor, "member")
	e.grant(org, ws, contributor, "readWrite")
	colleague := e.insert(`INSERT INTO users (name, email) VALUES ('Colleague', 'colleague@example.com') RETURNING id`)
	e.member(org, colleague, "member")
	e.grant(org, ws, colleague, "read")
	e.member(org, e.insert(`INSERT INTO users (name, email) VALUES ('Elsewhere', 'elsewhere@example.com') RETURNING id`), "member")
	id := e.createLink(owner, ws)
	outside := "The action in follow up 1 sends to %s, who cannot access this workspace. A follow-up may only address someone with access to this workspace or a respondent field."

	for _, user := range []uuid.UUID{contributor, owner} {
		sv := e.editorSurvey(user, id)
		sv["followUps"] = []any{followUpTo(uuid.NewString(), "outsider@elsewhere.example")}
		e.fails(e.do(user, http.MethodPut, appPath(id, ""), sv), http.StatusBadRequest, apperr.CodeValidation, fmt.Sprintf(outside, "outsider@elsewhere.example"))
		sv["updatedAt"] = nil
		e.fails(e.do(user, http.MethodPut, appPath(id, "/draft"), sv), http.StatusBadRequest, apperr.CodeValidation, fmt.Sprintf(outside, "outsider@elsewhere.example"))
		// An organization member without access to the workspace is outside it too.
		sv["followUps"] = []any{followUpTo(uuid.NewString(), "elsewhere@example.com")}
		e.fails(e.do(user, http.MethodPut, appPath(id, ""), sv), http.StatusBadRequest, apperr.CodeValidation, fmt.Sprintf(outside, "elsewhere@example.com"))
	}
	if n := e.count(`SELECT count(*) FROM survey_follow_ups WHERE survey_id = ?`, id); n != 0 {
		t.Fatalf("%d follow-ups saved", n)
	}

	sv := e.editorSurvey(contributor, id)
	sv["followUps"] = []any{followUpTo(uuid.NewString(), "Colleague@Example.com"), followUpTo(uuid.NewString(), "plan")}
	e.ok(e.do(contributor, http.MethodPut, appPath(id, ""), sv), http.StatusOK, nil)

	legacy := uuid.NewString()
	e.exec(`INSERT INTO survey_follow_ups (id, survey_id, name, trigger, action) VALUES (?, ?, 'Legacy', '{"type":"response","properties":null}', ?::jsonb)`, legacy, id,
		`{"type":"send-email","properties":{"to":"legacy@elsewhere.example","from":"noreply@example.com","replyTo":[],"subject":"Old","body":"<p>Old</p>","attachResponseData":true}}`)
	sv = e.editorSurvey(contributor, id)
	sv["name"] = "Renamed"
	e.ok(e.do(contributor, http.MethodPut, appPath(id, ""), sv), http.StatusOK, nil)
	for _, f := range sv["followUps"].([]any) {
		if f := f.(map[string]any); f["id"] == legacy {
			f["action"].(map[string]any)["properties"].(map[string]any)["to"] = "another@elsewhere.example"
		}
	}
	body := e.fails(e.do(contributor, http.MethodPut, appPath(id, ""), sv), http.StatusBadRequest, apperr.CodeValidation, "")
	if !strings.Contains(body.Message, "sends to another@elsewhere.example, who cannot access this workspace") {
		t.Fatalf("changed legacy recipient %q", body.Message)
	}

	// The v1 API's create and update hold a write key to the same rule.
	e.apiKey(org, ws, "write")
	key := APIKeyCaller(e.insert(`SELECT id FROM api_keys WHERE organization_id = ?`, org))
	e.authz.Deliver(t)
	// CreateV1 rewrites the body it is given, so each call gets its own.
	create := func(to string) map[string]any {
		return map[string]any{"name": "Via API", "questions": []any{map[string]any{"id": "q1", "type": "openText", "headline": map[string]any{"default": "Q"}, "required": false, "inputType": "text", "charLimit": map[string]any{"enabled": false}}},
			"followUps": []any{followUpTo(uuid.NewString(), to)}}
	}
	var invalid *V1Invalid
	if _, err := e.svc.CreateV1(t.Context(), key, ws, create("outsider@elsewhere.example")); !errors.As(err, &invalid) || invalid.Issues[0].Message != fmt.Sprintf(outside, "outsider@elsewhere.example") {
		t.Fatalf("v1 create to an outsider = %v", err)
	}
	created, err := e.svc.CreateV1(t.Context(), key, ws, create("colleague@example.com"))
	if err != nil {
		t.Fatalf("v1 create to a member = %v", err)
	}
	stored, err := e.svc.Load(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.UpdateV1(t.Context(), key, stored, map[string]any{"followUps": []any{followUpTo(uuid.NewString(), "outsider@elsewhere.example")}}); !errors.As(err, &invalid) || invalid.Issues[0].Message != fmt.Sprintf(outside, "outsider@elsewhere.example") {
		t.Fatalf("v1 update to an outsider = %v", err)
	}
}

func TestEditorSave(t *testing.T) {
	e := newEnv(t)
	owner, reader := e.user(), e.user()
	org, ws := e.organization(owner)
	e.member(org, reader, "member")
	e.grant(org, ws, reader, "read")
	// A follow-up's literal recipient must be able to access the workspace.
	e.member(org, e.insert(`INSERT INTO users (name, email) VALUES ('Boss', 'boss@example.com') RETURNING id`), "manager")
	id := e.createLink(owner, ws)

	sv := e.editorSurvey(owner, id)
	if sv["workspaceId"] != ws.String() || len(sv["languages"].([]any)) != 2 || sv["segment"] != nil || len(sv["followUps"].([]any)) != 0 {
		t.Fatalf("editor survey %v", sv)
	}
	// The response table, card and filter read hidden fields and variables only from embeddedFields.
	assertEmbeddedFields(t, sv["embeddedFields"], "score:computed", "plan:ingested")
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
	assertEmbeddedFields(t, saved["embeddedFields"], "score:computed", "plan:ingested")
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

// Progress saving is on for a new survey; the editor turns it off and on, and a save that does not carry it keeps what is stored.
func TestEditorSaveProgressSaving(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	id := e.createLink(owner, ws)

	sv := e.editorSurvey(owner, id)
	if sv["isProgressSavingEnabled"] != true {
		t.Fatalf("new survey isProgressSavingEnabled %v", sv["isProgressSavingEnabled"])
	}
	var saved map[string]any
	sv["isProgressSavingEnabled"] = false
	e.ok(e.do(owner, http.MethodPut, appPath(id, "/draft"), sv), http.StatusOK, &saved)
	if saved["isProgressSavingEnabled"] != false {
		t.Fatalf("turned off: %v", saved["isProgressSavingEnabled"])
	}
	delete(saved, "isProgressSavingEnabled")
	e.ok(e.do(owner, http.MethodPut, appPath(id, "/draft"), saved), http.StatusOK, &saved)
	if saved["isProgressSavingEnabled"] != false {
		t.Fatalf("a save without the field changed it: %v", saved["isProgressSavingEnabled"])
	}
	saved["isProgressSavingEnabled"] = true
	e.ok(e.do(owner, http.MethodPut, appPath(id, "/draft"), saved), http.StatusOK, &saved)
	if saved["isProgressSavingEnabled"] != true {
		t.Fatalf("turned on: %v", saved["isProgressSavingEnabled"])
	}
}

// The duplicate-submission limits (D-FG03) are off for a new survey; the editor turns each on and off, and a save that does not carry them keeps what is stored. The v1 management API neither shows nor changes them.
func TestEditorSaveSingleResponseLimits(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	id := e.createLink(owner, ws)
	keys := []string{"isSingleResponsePerBrowserEnabled", "isSingleResponsePerEmailEnabled"}

	sv := e.editorSurvey(owner, id)
	for _, k := range keys {
		if sv[k] != false {
			t.Fatalf("new survey %s %v", k, sv[k])
		}
	}
	var saved map[string]any
	for _, k := range keys {
		sv[k] = true
	}
	e.ok(e.do(owner, http.MethodPut, appPath(id, "/draft"), sv), http.StatusOK, &saved)
	for _, k := range keys {
		if saved[k] != true {
			t.Fatalf("turned on: %s %v", k, saved[k])
		}
		delete(saved, k)
	}
	e.ok(e.do(owner, http.MethodPut, appPath(id, "/draft"), saved), http.StatusOK, &saved)
	for _, k := range keys {
		if saved[k] != true {
			t.Fatalf("a save without %s changed it: %v", k, saved[k])
		}
	}
	saved["isSingleResponsePerEmailEnabled"] = false
	e.ok(e.do(owner, http.MethodPut, appPath(id, "/draft"), saved), http.StatusOK, &saved)
	if saved["isSingleResponsePerEmailEnabled"] != false || saved["isSingleResponsePerBrowserEnabled"] != true {
		t.Fatalf("turned one off: %v %v", saved["isSingleResponsePerEmailEnabled"], saved["isSingleResponsePerBrowserEnabled"])
	}

	stored, err := e.repo.ByID(t.Context(), uuid.MustParse(id))
	if err != nil || stored == nil {
		t.Fatalf("load: %v", err)
	}
	obj, err := V1Object(stored)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if _, ok := obj[k]; ok {
			t.Fatalf("v1 shows %s", k)
		}
	}
}

// Every editor save needs a name, a full save (Publish, Save on a live survey) as much as a draft one, and a name of only spaces is no name. The stored survey keeps its name.
func TestEditorSaveRequiresAName(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	id := e.createLink(owner, ws)
	sv := e.editorSurvey(owner, id)
	stored := sv["name"]

	for _, name := range []string{"", "   ", "\t\n"} {
		sv["name"] = name
		for _, rest := range []string{"", "/draft"} {
			body := e.fails(e.do(owner, http.MethodPut, appPath(id, rest), sv), http.StatusBadRequest, apperr.CodeValidation, "Survey name is required")
			issues, _ := body.Details["issues"].([]any)
			if len(issues) == 0 || fmt.Sprint(issues[0].(map[string]any)["path"]) != "[name]" {
				t.Fatalf("save %q to %q: issues %v", rest, name, body.Details["issues"])
			}
			// Refused like any other invalid save, so audited as a failed update.
			e.expectAudit(audit.ActionUpdated, audit.StatusFailure, audit.ActorUser, owner.String(), audit.TargetSurvey, id)
		}
	}
	if got := e.editorSurvey(owner, id)["name"]; got != stored {
		t.Fatalf("stored name %v, want %v", got, stored)
	}

	sv["name"] = "  Renamed  "
	e.ok(e.do(owner, http.MethodPut, appPath(id, ""), sv), http.StatusOK, nil)
}

// The editor holds updatedAt as a JavaScript Date, which keeps milliseconds, while updated_at has microseconds: a draft save compares the two, and names the stored version, at millisecond precision, as forma2's DateTime(3) did.
func TestEditorDraftSaveComparesVersionsToTheMillisecond(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	id := e.createLink(owner, ws)
	sv := e.editorSurvey(owner, id)

	e.exec(`UPDATE surveys SET updated_at = '2026-10-01 10:00:00.123456+00' WHERE id = ?`, id)
	sv["updatedAt"] = "2026-10-01T10:00:00.123Z"
	e.ok(e.do(owner, http.MethodPut, appPath(id, "/draft"), sv), http.StatusOK, nil)

	e.exec(`UPDATE surveys SET updated_at = '2026-10-01 10:00:01.123456+00' WHERE id = ?`, id)
	sv["updatedAt"] = "2026-10-01T10:00:00.999Z"
	e.fails(e.do(owner, http.MethodPut, appPath(id, "/draft"), sv), http.StatusForbidden, apperr.CodeOperationNotAllowed, "survey_modified_elsewhere:2026-10-01T10:00:01.123Z")
	// Adopting the version the conflict named is a save based on the stored survey.
	sv["updatedAt"] = "2026-10-01T10:00:01.123Z"
	e.ok(e.do(owner, http.MethodPut, appPath(id, "/draft"), sv), http.StatusOK, nil)
}

// An editor save writes the survey as it is under the row lock: a slug set while the save waited is kept, not reverted to the snapshot the save started from.
func TestEditorSaveKeepsAConcurrentChange(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	id := e.createLink(owner, ws)
	sv := e.editorSurvey(owner, id)
	sv["name"] = "Saved while locked"

	release := e.holdSurveyLock(id)
	saved := e.async(http.MethodPut, appPath(id, ""), sv, map[string]string{testUserHeader: owner.String()})
	e.waitForLockWaiter()
	release(`UPDATE surveys SET slug = 'set-meanwhile' WHERE id = ?`)
	e.ok(<-saved, http.StatusOK, nil)
	if slug, name := e.text(`SELECT slug FROM surveys WHERE id = ?`, id), e.text(`SELECT name FROM surveys WHERE id = ?`, id); slug != "set-meanwhile" || name != "Saved while locked" {
		t.Fatalf("slug %q name %q", slug, name)
	}

	// A survey archived while the save waited is not written.
	sv["name"] = "Never saved"
	release = e.holdSurveyLock(id)
	saved = e.async(http.MethodPut, appPath(id, ""), sv, map[string]string{testUserHeader: owner.String()})
	e.waitForLockWaiter()
	release(`UPDATE surveys SET archived_at = now() WHERE id = ?`)
	e.fails(<-saved, http.StatusBadRequest, apperr.CodeBadRequest, "This survey is archived. Restore it before editing.")
	if name := e.text(`SELECT name FROM surveys WHERE id = ?`, id); name != "Saved while locked" {
		t.Fatalf("archived survey written: %q", name)
	}
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

// The editor's publish of a survey switched to "Website & App" with no targeting, as forma2's editor sent it: its placeholder segment never became a segment (forma2's createSegmentAction refused the empty tree), so the survey arrives without one and the save gives it an empty private segment titled with its id.
func TestEditorPublishesAnAppSurveyWithoutTargeting(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	trigger := e.actionClass(ws, "open")

	for _, path := range []string{"/draft", ""} {
		id := e.createLink(owner, ws)
		sv := e.editorSurvey(owner, id)
		sv["type"] = "app"
		sv["segment"] = nil
		sv["triggers"] = []any{map[string]any{"actionClass": map[string]any{"id": trigger.String()}}}
		if path == "" {
			sv["status"] = "inProgress"
		}
		var saved Survey
		e.ok(e.do(owner, http.MethodPut, appPath(id, path), sv), http.StatusOK, &saved)
		if saved.Segment == nil || !saved.Segment.IsPrivate || saved.Segment.Title != id || string(saved.Segment.Filters) != "[]" {
			t.Fatalf("save %q: segment %+v", path, saved.Segment)
		}
		if path == "" && saved.Status != "inProgress" {
			t.Fatalf("publish left the survey %s", saved.Status)
		}
		if n := e.count(`SELECT count(*) FROM segments WHERE workspace_id = ? AND title = ?`, ws, id); n != 1 {
			t.Fatalf("save %q: %d private segments", path, n)
		}
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
	if len(dup.EmbeddedFields) != 2 || dup.EmbeddedFields[1].Field.Name != "plan" || dup.EmbeddedFields[1].Link.StorageKey != "plan" {
		t.Fatalf("duplicate embedded fields %+v", dup.EmbeddedFields)
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
	if len(links.Links) != 3 || links.Links[0].SuToken != SingleUseSignature(string(e.cfg.EncryptionKeyText), id, links.Links[0].SuID) {
		t.Fatalf("plain links %+v", links)
	}
	links.Links = nil
	e.ok(e.do(owner, http.MethodPost, appPath(id, "/single-use-links"), map[string]any{"isEncrypted": true, "count": 2}), http.StatusOK, &links)
	if len(links.Links) != 2 || links.Links[0].SuToken != "" || links.Links[0].SuID == links.Links[1].SuID {
		t.Fatalf("encrypted links %+v", links)
	}
	// An encrypted id opens under the survey it was made for, to a new id, and under no other survey.
	for _, link := range links.Links {
		sealed, err := base64.RawURLEncoding.DecodeString(link.SuID)
		if err != nil {
			t.Fatal(err)
		}
		opened, err := e.svc.cipher.Open(sealed, singleUseContext(uuid.MustParse(id)))
		if err != nil || !surveydoc.IsCUID2(string(opened)) {
			t.Fatalf("open %q: %q %v", link.SuID, opened, err)
		}
		if _, err := e.svc.cipher.Open(sealed, singleUseContext(uuid.New())); err == nil {
			t.Fatal("an encrypted id opened under another survey")
		}
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
	if len(out.Templates) != 68 || out.Templates[0].ID != CustomTemplateID || len(out.XMTemplateIDs) != 6 || strings.Contains(string(out.Templates[1].Preset), "{{") {
		t.Fatalf("templates %d %+v", len(out.Templates), out.Templates[0])
	}
	// Asked in a survey's default language, the catalog renders as the survey created from it will, whatever the dashboard's language: a dashboard locale in itself, anything else (a bare runtime code too) in en-US.
	for language, want := range map[string]string{"zh-Hans-CN": "从零开始", "en-US": "Start from scratch", "ja": "Start from scratch", "de-DE": "Start from scratch"} {
		var inLanguage struct {
			Templates []Template `json:"templates"`
		}
		e.ok(e.do(owner, http.MethodGet, "/api/app/workspaces/"+ws.String()+"/survey-templates?language="+language, nil), http.StatusOK, &inLanguage)
		if len(inLanguage.Templates) != len(out.Templates) || inLanguage.Templates[0].Name != want {
			t.Fatalf("templates in %s: %d, %q", language, len(inLanguage.Templates), inLanguage.Templates[0].Name)
		}
	}
	for _, language := range []string{"", "xx-YY"} {
		e.fails(e.do(owner, http.MethodGet, "/api/app/workspaces/"+ws.String()+"/survey-templates?language="+language, nil), http.StatusBadRequest, apperr.CodeBadRequest, "Unsupported survey default language")
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
	// The picker's card keeps its label, but a survey started from scratch gets a name of its own rather than the label (de-DE renders the catalog in en-US).
	if out.Templates[0].Name != "最初から始める" || created.Data["name"] != "Untitled survey" {
		t.Fatalf("custom template card %q, survey name %v", out.Templates[0].Name, created.Data["name"])
	}
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

// forma2's audit events of the survey module: the action, its outcome, who acted (a user, or an API key as "api") and on what.
func TestSurveyAuditEvents(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	org, ws := e.organization(owner)
	target := e.workspace(org)
	user := audit.ActorUser
	id := e.createLink(owner, ws)
	e.expectAudit(audit.ActionCreated, audit.StatusSuccess, user, owner.String(), audit.TargetSurvey, id)

	var copied Survey
	e.ok(e.do(owner, http.MethodPost, appPath(id, "/copy"), map[string]any{"targetWorkspaceId": target}), http.StatusCreated, &copied)
	// The copy is audited on the survey it was made from.
	e.expectAudit(audit.ActionCopiedToOtherWorkspace, audit.StatusSuccess, user, owner.String(), audit.TargetSurvey, id)

	sv := e.editorSurvey(owner, id)
	sv["name"] = "Audited"
	e.ok(e.do(owner, http.MethodPut, appPath(id, ""), sv), http.StatusOK, nil)
	e.expectAudit(audit.ActionUpdated, audit.StatusSuccess, user, owner.String(), audit.TargetSurvey, id)

	key := e.apiKey(org, ws, "write")
	e.ok(e.v3Key(key, http.MethodPatch, surveysPath("/"+id), `{"name": "By key"}`), http.StatusOK, nil)
	e.expectAudit(audit.ActionUpdated, audit.StatusSuccess, audit.ActorAPI, "", audit.TargetSurvey, id)
	e.problem(e.v3Key(key, http.MethodPatch, surveysPath("/"+id), `{"blocks": []}`), http.StatusUnprocessableEntity, "")
	e.expectAudit(audit.ActionUpdated, audit.StatusFailure, audit.ActorAPI, "", audit.TargetSurvey, id)

	quota := map[string]any{"name": "Pros", "limit": 10, "logic": map[string]any{"connector": "and", "conditions": []any{}}, "action": "continueSurvey", "countPartialSubmissions": false}
	var q Quota
	e.ok(e.do(owner, http.MethodPost, appPath(id, "/quotas"), quota), http.StatusCreated, &q)
	e.expectAudit(audit.ActionCreated, audit.StatusSuccess, user, owner.String(), audit.TargetQuota, q.ID.String())
	quota["limit"] = 20
	e.ok(e.do(owner, http.MethodPatch, "/api/app/quotas/"+q.ID.String(), quota), http.StatusOK, nil)
	e.expectAudit(audit.ActionUpdated, audit.StatusSuccess, user, owner.String(), audit.TargetQuota, q.ID.String())
	e.ok(e.do(owner, http.MethodDelete, "/api/app/quotas/"+q.ID.String(), nil), http.StatusNoContent, nil)
	e.expectAudit(audit.ActionDeleted, audit.StatusSuccess, user, owner.String(), audit.TargetQuota, q.ID.String())

	e.ok(e.v3(owner, http.MethodPost, surveysPath("/"+id+"/archive"), nil), http.StatusOK, nil)
	e.expectAudit(audit.ActionArchived, audit.StatusSuccess, user, owner.String(), audit.TargetSurvey, id)
	e.ok(e.v3(owner, http.MethodPost, surveysPath("/"+id+"/restore"), nil), http.StatusOK, nil)
	e.expectAudit(audit.ActionRestored, audit.StatusSuccess, user, owner.String(), audit.TargetSurvey, id)
	e.ok(e.v3Key(key, http.MethodDelete, surveysPath("/"+id), nil), http.StatusNoContent, nil)
	e.expectAudit(audit.ActionDeleted, audit.StatusSuccess, audit.ActorAPI, "", audit.TargetSurvey, id)
}

// An editor save may only link the languages, segment and segment surveys of the survey's own workspace, within forma2's segment bounds.
func TestEditorSaveTenantGuards(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	trigger := e.actionClass(ws, "open")
	id := e.createLink(owner, ws)
	sv := e.editorSurvey(owner, id)
	sv["type"] = "app"
	sv["triggers"] = []any{map[string]any{"actionClass": map[string]any{"id": trigger.String()}}}
	e.ok(e.do(owner, http.MethodPut, appPath(id, "/draft"), sv), http.StatusOK, nil)

	other, _ := e.organization(e.user())
	foreignWS := e.workspace(other)
	foreignLanguage := e.insert(`INSERT INTO languages (workspace_id, code) VALUES (?, 'fr-FR') RETURNING id`, foreignWS)
	foreignSegment := e.insert(`INSERT INTO segments (workspace_id, title) VALUES (?, 'Theirs') RETURNING id`, foreignWS)
	foreignSurvey := e.insert(`INSERT INTO surveys (workspace_id, name) VALUES (?, 'Theirs') RETURNING id`, foreignWS)

	withForeignLanguage := func() map[string]any {
		sv := e.editorSurvey(owner, id)
		sv["languages"] = append(sv["languages"].([]any), map[string]any{"language": map[string]any{"id": foreignLanguage}, "default": false, "enabled": false})
		return sv
	}
	e.fails(e.do(owner, http.MethodPut, appPath(id, "/draft"), withForeignLanguage()), http.StatusNotFound, apperr.CodeNotFound, "")
	if rec := e.do(owner, http.MethodPut, appPath(id, ""), withForeignLanguage()); rec.Code == http.StatusOK {
		t.Fatal("a full save linked another workspace's language")
	}
	if e.count(`SELECT count(*) FROM survey_languages WHERE language_id = ?`, foreignLanguage) != 0 {
		t.Fatal("another workspace's language was linked")
	}

	withSegment := func(mutate func(seg map[string]any)) map[string]any {
		sv := e.editorSurvey(owner, id)
		seg := sv["segment"].(map[string]any)
		mutate(seg)
		return sv
	}
	e.fails(e.do(owner, http.MethodPut, appPath(id, "/draft"), withSegment(func(seg map[string]any) { seg["id"] = foreignSegment })), http.StatusNotFound, apperr.CodeNotFound, "")
	e.fails(e.do(owner, http.MethodPut, appPath(id, "/draft"), withSegment(func(seg map[string]any) { seg["surveys"] = []any{foreignSurvey} })), http.StatusBadRequest, apperr.CodeBadRequest, "Survey and segment are not in the same workspace")
	tooMany := make([]any, segmentSurveyIDsLimit+1)
	for i := range tooMany {
		tooMany[i] = uuid.NewString()
	}
	e.fails(e.do(owner, http.MethodPut, appPath(id, "/draft"), withSegment(func(seg map[string]any) { seg["surveys"] = tooMany })), http.StatusBadRequest, apperr.CodeBadRequest, fmt.Sprintf("Invalid segment surveys: at most %d valid survey ids are allowed", segmentSurveyIDsLimit))
	deep := []any{}
	for range maxSegmentFilterDepth + 1 {
		deep = []any{map[string]any{"id": uuid.NewString(), "connector": nil, "resource": deep}}
	}
	e.fails(e.do(owner, http.MethodPut, appPath(id, "/draft"), withSegment(func(seg map[string]any) { seg["filters"] = deep })), http.StatusBadRequest, apperr.CodeBadRequest, fmt.Sprintf("Segment filters are nested too deeply: at most %d levels are supported", maxSegmentFilterDepth))
	if e.count(`SELECT count(*) FROM segments WHERE workspace_id = ? AND title = 'Theirs'`, foreignWS) != 1 || e.count(`SELECT count(*) FROM surveys WHERE id = ? AND segment_id IS NOT NULL`, foreignSurvey) != 0 {
		t.Fatal("another workspace's segment or survey changed")
	}
}

// A known answer of forma2's generateSurveySingleUseSignature, computed with its code (createHmac("sha256", ENCRYPTION_KEY).update(payload).digest("hex")) for the hex key of 32 bytes of 7: the plain links P3 verifies must carry exactly this token.
func TestSingleUseSignatureMatchesForma2(t *testing.T) {
	got := SingleUseSignature(hex.EncodeToString(bytes.Repeat([]byte{7}, 32)), "01a16211-77de-7d59-ada1-7e2ea6ddb527", "su-1")
	if got != "c38448563887c9d696d03df027af932d52e591114eb6b936d4ac92dd11069a1d" {
		t.Fatalf("signature %s", got)
	}
}

// forma2 asserted workspace.write on both ends of a copy and for a template survey, and read for the template list.
func TestTemplateAndCopyAuthorization(t *testing.T) {
	e := newEnv(t)
	owner, reader, outsider := e.user(), e.user(), e.user()
	org, ws := e.organization(owner)
	target := e.workspace(org)
	e.member(org, reader, "member")
	e.grant(org, ws, reader, "read")
	e.grant(org, target, reader, "readWrite")
	id := e.createLink(owner, ws)

	e.ok(e.do(reader, http.MethodGet, "/api/app/workspaces/"+ws.String()+"/survey-templates", nil), http.StatusOK, nil)
	e.fails(e.do(outsider, http.MethodGet, "/api/app/workspaces/"+ws.String()+"/survey-templates", nil), http.StatusForbidden, apperr.CodeForbidden, "")

	template := map[string]any{"workspaceId": ws, "templateId": "nps", "source": "xm", "surveyType": "link", "defaultLanguage": "en-US"}
	e.problem(e.v3(reader, http.MethodPost, surveysPath("/templates"), template), http.StatusForbidden, v3api.ForbiddenMessage)
	e.problem(e.v3(outsider, http.MethodPost, surveysPath("/templates"), template), http.StatusForbidden, v3api.ForbiddenMessage)
	template["workspaceId"] = target
	e.ok(e.v3(reader, http.MethodPost, surveysPath("/templates"), template), http.StatusCreated, nil)

	// Reading the source is not enough to copy it, even into a workspace the caller may write.
	before := e.count(`SELECT count(*) FROM surveys WHERE workspace_id = ?`, target)
	e.fails(e.do(reader, http.MethodPost, appPath(id, "/copy"), map[string]any{"targetWorkspaceId": target}), http.StatusForbidden, apperr.CodeForbidden, "")
	e.fails(e.do(outsider, http.MethodPost, appPath(id, "/copy"), map[string]any{"targetWorkspaceId": target}), http.StatusForbidden, apperr.CodeForbidden, "")
	if e.count(`SELECT count(*) FROM surveys WHERE workspace_id = ?`, target) != before {
		t.Fatal("a refused copy was made")
	}
}
