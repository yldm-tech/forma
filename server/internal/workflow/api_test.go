package workflow

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type envelope[T any] struct {
	Data T `json:"data"`
	Meta struct {
		Limit      int     `json:"limit"`
		NextCursor *string `json:"nextCursor"`
	} `json:"meta"`
}

type resource struct {
	ID          uuid.UUID       `json:"id"`
	WorkspaceID uuid.UUID       `json:"workspaceId"`
	Name        string          `json:"name"`
	Description *string         `json:"description"`
	Status      string          `json:"status"`
	TriggerType *string         `json:"triggerType"`
	SurveyID    *string         `json:"surveyId"`
	CreatedBy   *uuid.UUID      `json:"createdBy"`
	Creator     *Creator        `json:"creator"`
	RunCount    int             `json:"runCount"`
	LastRun     *RunSummary     `json:"lastRun"`
	Definition  json.RawMessage `json:"definition"`
}

func (e *env) create(user uuid.UUID, ws uuid.UUID, name string, def any) resource {
	e.t.Helper()
	var out envelope[resource]
	e.ok(e.v3(user, http.MethodPost, "/api/v3/workflows", map[string]any{"workspaceId": ws, "name": name, "definition": def}), http.StatusCreated, &out)
	return out.Data
}

func (e *env) lifecycle(user, id uuid.UUID, action string) *httptest.ResponseRecorder {
	return e.v3(user, http.MethodPost, "/api/v3/workflows/"+id.String()+"/"+action, nil)
}

func TestCreateGetAndList(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	rec := e.v3(f.owner, http.MethodPost, "/api/v3/workflows", map[string]any{"workspaceId": f.ws, "name": "Notify team", "description": "Tell us", "status": "draft", "definition": definition(f.survey, "owner@example.com")})
	var created envelope[resource]
	e.ok(rec, http.StatusCreated, &created)
	w := created.Data
	if rec.Header().Get("Location") != "/api/v3/workflows/"+w.ID.String() || rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("X-Request-Id") == "" {
		t.Fatalf("headers %v", rec.Header())
	}
	if w.Status != StatusDraft || *w.TriggerType != TriggerResponseCompleted || *w.SurveyID != f.survey.String() || *w.CreatedBy != f.owner || w.Creator.Name != "Ada" || w.LastRun != nil || w.RunCount != 0 {
		t.Fatalf("created %+v", w)
	}
	var got envelope[resource]
	e.ok(e.v3(f.owner, http.MethodGet, "/api/v3/workflows/"+w.ID.String(), nil), http.StatusOK, &got)
	if string(got.Data.Definition) != string(w.Definition) || !strings.Contains(string(w.Definition), `"includeHiddenFields":false`) {
		t.Fatalf("get %s", got.Data.Definition)
	}

	// Archived workflows are left out unless asked for; name and sort filters apply; pages follow the cursor.
	for i := range 3 {
		e.create(f.owner, f.ws, fmt.Sprintf("Batch %d", i), map[string]any{"trigger": nil, "entryNodeId": nil})
	}
	archived := e.create(f.owner, f.ws, "Old", map[string]any{"trigger": nil, "entryNodeId": nil})
	e.ok(e.lifecycle(f.owner, archived.ID, "archive"), http.StatusOK, nil)
	var page envelope[[]resource]
	e.ok(e.v3(f.owner, http.MethodGet, "/api/v3/workflows?workspaceId="+f.ws.String(), nil), http.StatusOK, &page)
	if len(page.Data) != 4 || page.Meta.Limit != 20 || page.Meta.NextCursor != nil || page.Data[0].Name != "Batch 2" {
		t.Fatalf("default list %+v", page)
	}
	if page.Data[0].Definition != nil {
		t.Fatal("list items carry the definition")
	}
	e.ok(e.v3(f.owner, http.MethodGet, "/api/v3/workflows?workspaceId="+f.ws.String()+"&filter[status][in]=archived,draft&filter[name][contains]=OL&ignored=1", nil), http.StatusOK, &page)
	if len(page.Data) != 1 || page.Data[0].ID != archived.ID {
		t.Fatalf("filtered %+v", page.Data)
	}
	seen := map[uuid.UUID]bool{}
	cursor := ""
	for range 3 {
		e.ok(e.v3(f.owner, http.MethodGet, "/api/v3/workflows?workspaceId="+f.ws.String()+"&limit=2&sortBy=name"+cursor, nil), http.StatusOK, &page)
		for _, item := range page.Data {
			seen[item.ID] = true
		}
		if page.Meta.NextCursor == nil {
			break
		}
		cursor = "&cursor=" + *page.Meta.NextCursor
	}
	if len(seen) != 4 {
		t.Fatalf("paged through %d workflows", len(seen))
	}
	p := e.problem(e.v3(f.owner, http.MethodGet, "/api/v3/workflows?workspaceId="+f.ws.String()+"&sortBy=createdAt"+cursor, nil), http.StatusBadRequest, "bad_request", "The cursor does not match the requested sort order.")
	if p.InvalidParams[0].Name != "cursor" {
		t.Fatalf("%+v", p)
	}
	p = e.problem(e.v3(f.owner, http.MethodGet, "/api/v3/workflows?workspaceId="+f.ws.String()+"&limit=101&filter[status][in]=live", nil), http.StatusBadRequest, "bad_request", "The request payload is invalid.")
	if len(p.InvalidParams) != 2 || p.InvalidParams[0].Name != "limit" || p.InvalidParams[1].Name != "statusIn.0" {
		t.Fatalf("%+v", p)
	}
	e.problem(e.v3(f.owner, http.MethodGet, "/api/v3/workflows", nil), http.StatusBadRequest, "bad_request", "The request payload is invalid.")
}

func TestAccessIsRefusedWithoutLeakingExistence(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	w := e.create(f.owner, f.ws, "Notify", definition(f.survey, "owner@example.com"))
	outsider := e.user("outsider@example.com")
	otherOrg := e.insert(`INSERT INTO organizations (name) VALUES ('Other') RETURNING id`)
	e.member(otherOrg, outsider, "owner")
	for _, path := range []string{"/api/v3/workflows/" + w.ID.String(), "/api/v3/workflows/" + uuid.NewString()} {
		e.problem(e.v3(outsider, http.MethodGet, path, nil), http.StatusForbidden, "forbidden", "You are not authorized to access this resource")
	}
	e.problem(e.v3(f.owner, http.MethodGet, "/api/v3/workflows/"+uuid.NewString(), nil), http.StatusForbidden, "forbidden", "")
	e.problem(e.v3(f.owner, http.MethodGet, "/api/v3/workflows/runs/"+uuid.NewString(), nil), http.StatusForbidden, "forbidden", "")
	p := e.problem(e.v3(f.owner, http.MethodGet, "/api/v3/workflows/not-an-id", nil), http.StatusBadRequest, "bad_request", "Invalid route parameters")
	if p.InvalidParams[0].Name != "workflowId" {
		t.Fatalf("%+v", p)
	}
	e.problem(e.v3(outsider, http.MethodPost, "/api/v3/workflows", map[string]any{"workspaceId": f.ws, "name": "x", "definition": definition(f.survey, "q1")}), http.StatusForbidden, "forbidden", "")
	e.problem(e.request(http.MethodGet, "/api/v3/workflows?workspaceId="+f.ws.String(), nil, nil), http.StatusUnauthorized, "not_authenticated", "")

	// A read-only team member can read and dry-run, but not write; a read API key likewise.
	reader := e.user("reader@example.com")
	e.member(f.org, reader, "member")
	e.grant(f.org, f.ws, reader, "read")
	e.ok(e.v3(reader, http.MethodGet, "/api/v3/workflows/"+w.ID.String(), nil), http.StatusOK, nil)
	e.ok(e.lifecycle(reader, w.ID, "test"), http.StatusOK, nil)
	e.problem(e.v3(reader, http.MethodPatch, "/api/v3/workflows/"+w.ID.String(), map[string]any{"name": "x"}), http.StatusForbidden, "forbidden", "")
	key := e.apiKey(f.org, f.ws, "read")
	e.ok(e.request(http.MethodGet, "/api/v3/workflows?workspaceId="+f.ws.String(), nil, map[string]string{"x-api-key": key}), http.StatusOK, nil)
	e.problem(e.request(http.MethodPost, "/api/v3/workflows/"+w.ID.String()+"/enable", nil, map[string]string{"x-api-key": key}), http.StatusForbidden, "forbidden", "")
	writer := e.apiKey(f.org, f.ws, "write")
	var created envelope[resource]
	e.ok(e.request(http.MethodPost, "/api/v3/workflows", map[string]any{"workspaceId": f.ws, "name": "By key", "definition": definition(f.survey, "q1")}, map[string]string{"x-api-key": writer}), http.StatusCreated, &created)
	if created.Data.CreatedBy != nil || created.Data.Creator != nil {
		t.Fatalf("a key's workflow names a creator: %+v", created.Data)
	}
}

func TestRequestBodies(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	req := httptest.NewRequest(http.MethodPost, "/api/v3/workflows", strings.NewReader("{}"))
	req.ContentLength = maxBodyBytes + 1
	req.Header.Set(testUserHeader, f.owner.String())
	req.Header.Set("Origin", webApp)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	rec := httptest.NewRecorder()
	e.authz.Deliver(t)
	e.engine.ServeHTTP(rec, req)
	e.problem(rec, http.StatusBadRequest, "bad_request", "Request body is too large.")
	e.problem(e.v3(f.owner, http.MethodPost, "/api/v3/workflows", `{"name":"`+strings.Repeat("a", maxBodyBytes)+`"}`), http.StatusBadRequest, "bad_request", "Request body is too large.")
	e.problem(e.v3(f.owner, http.MethodPost, "/api/v3/workflows", "{"), http.StatusBadRequest, "bad_request", "Malformed JSON body.")
	e.problem(e.v3(f.owner, http.MethodPost, "/api/v3/workflows", " "), http.StatusBadRequest, "bad_request", "Malformed JSON body.")

	broken := definition(f.survey, "q1")
	broken["edges"] = []any{map[string]any{"id": "e", "source": "trigger", "target": "nowhere"}}
	p := e.problem(e.v3(f.owner, http.MethodPost, "/api/v3/workflows", map[string]any{"workspaceId": f.ws, "name": "Broken", "definition": broken, "extra": true}), http.StatusBadRequest, "bad_request", "The request payload is invalid.")
	names := map[string]string{}
	for _, ip := range p.InvalidParams {
		names[ip.Name] = ip.Reason
	}
	if names["definition.edges.0.target"] != "Edge target does not reference a workflow node: nowhere" || names[""] != `Unrecognized key: "extra"` {
		t.Fatalf("%+v", p.InvalidParams)
	}
	p = e.problem(e.v3(f.owner, http.MethodPost, "/api/v3/workflows", map[string]any{"workspaceId": f.ws, "name": strings.Repeat("n", 121), "status": "enabled", "definition": definition(f.survey, "q1")}), http.StatusBadRequest, "bad_request", "")
	if len(p.InvalidParams) != 2 || p.InvalidParams[0].Name != "name" || p.InvalidParams[1].Name != "status" {
		t.Fatalf("%+v", p.InvalidParams)
	}
	if e.count(`SELECT count(*) FROM workflows`) != 0 {
		t.Fatal("a refused body was stored")
	}
}

func TestPatchDuplicateAndDelete(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	w := e.create(f.owner, f.ws, "Notify", definition(f.survey, "owner@example.com"))
	path := "/api/v3/workflows/" + w.ID.String()
	var patched envelope[resource]
	e.ok(e.v3(f.owner, http.MethodPatch, path, map[string]any{"name": "Renamed", "description": "Now described"}), http.StatusOK, &patched)
	if patched.Data.Name != "Renamed" || *patched.Data.Description != "Now described" {
		t.Fatalf("%+v", patched.Data)
	}
	e.ok(e.v3(f.owner, http.MethodPatch, path, map[string]any{"description": nil}), http.StatusOK, &patched)
	if patched.Data.Description != nil || patched.Data.Name != "Renamed" {
		t.Fatalf("description not cleared: %+v", patched.Data)
	}
	e.problem(e.v3(f.owner, http.MethodPatch, path, map[string]any{}), http.StatusBadRequest, "bad_request", "The request payload is invalid.")
	e.problem(e.v3(f.owner, http.MethodPatch, path, map[string]any{"status": "enabled"}), http.StatusBadRequest, "bad_request", "")

	// An enabled workflow keeps its definition; its name can still change.
	e.ok(e.lifecycle(f.owner, w.ID, "enable"), http.StatusOK, nil)
	e.problem(e.v3(f.owner, http.MethodPatch, path, map[string]any{"definition": definition(f.survey, "q1")}), http.StatusUnprocessableEntity, "invalid_workflow_state", definitionLocked)
	e.ok(e.v3(f.owner, http.MethodPatch, path, map[string]any{"name": "Live"}), http.StatusOK, nil)
	// Archived workflows are read-only, refused before the body is read.
	e.ok(e.lifecycle(f.owner, w.ID, "archive"), http.StatusOK, nil)
	e.problem(e.v3(f.owner, http.MethodPatch, path, "{"), http.StatusUnprocessableEntity, "invalid_workflow_state", archivedReadOnly)

	var copied envelope[resource]
	rec := e.v3(f.owner, http.MethodPost, path+"/duplicate", nil)
	e.ok(rec, http.StatusCreated, &copied)
	if copied.Data.Name != "Live (copy)" || copied.Data.Status != StatusDraft || copied.Data.ID == w.ID || string(copied.Data.Definition) != string(w.Definition) || rec.Header().Get("Location") != "/api/v3/workflows/"+copied.Data.ID.String() {
		t.Fatalf("duplicate %+v", copied.Data)
	}
	e.ok(e.v3(f.owner, http.MethodPost, path+"/duplicate", map[string]any{"name": "Chosen"}), http.StatusCreated, &copied)
	if copied.Data.Name != "Chosen" || e.count(`SELECT count(*) FROM workflow_versions WHERE workflow_id = ?`, copied.Data.ID) != 0 {
		t.Fatalf("named duplicate %+v", copied.Data)
	}
	e.problem(e.v3(f.owner, http.MethodPost, path+"/duplicate", map[string]any{"name": ""}), http.StatusBadRequest, "bad_request", "")

	// Delete is a hard delete of every status, with versions and runs.
	r := e.respond(f.survey, true, `{}`, nil)
	e.exec(`INSERT INTO workflow_runs (workflow_id, workspace_id, trigger_type, trigger_payload, response_id, idempotency_key) VALUES (?, ?, 'response.completed', '{}', ?, ?)`, w.ID, f.ws, r.ID, r.ID.String())
	rec = e.v3(f.owner, http.MethodDelete, path, nil)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 || rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("delete %d %s", rec.Code, rec.Body.String())
	}
	if e.count(`SELECT count(*) FROM workflows WHERE id = ?`, w.ID)+e.count(`SELECT count(*) FROM workflow_versions WHERE workflow_id = ?`, w.ID)+e.count(`SELECT count(*) FROM workflow_runs WHERE workflow_id = ?`, w.ID) != 0 {
		t.Fatal("delete left rows behind")
	}
	e.problem(e.v3(f.owner, http.MethodDelete, path, nil), http.StatusForbidden, "forbidden", "")
}

func TestLifecycle(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	w := e.create(f.owner, f.ws, "Notify", definition(f.survey, "owner@example.com", testEndingID))
	var out envelope[resource]
	e.ok(e.lifecycle(f.owner, w.ID, "enable"), http.StatusOK, &out)
	if out.Data.Status != StatusEnabled || e.text(`SELECT trigger_survey_id::text FROM workflows WHERE id = ?`, w.ID) != f.survey.String() {
		t.Fatalf("enable %+v", out.Data)
	}
	if e.count(`SELECT version FROM workflow_versions WHERE workflow_id = ? AND published_by = ?`, w.ID, f.owner) != 1 {
		t.Fatal("no version 1")
	}
	e.problem(e.lifecycle(f.owner, w.ID, "enable"), http.StatusUnprocessableEntity, "invalid_workflow_state", onlyDraftOrDisabled)
	e.problem(e.lifecycle(f.owner, w.ID, "unarchive"), http.StatusUnprocessableEntity, "invalid_workflow_state", onlyArchived)
	e.ok(e.lifecycle(f.owner, w.ID, "disable"), http.StatusOK, &out)
	if out.Data.Status != StatusDisabled || e.text(`SELECT trigger_survey_id::text FROM workflows WHERE id = ?`, w.ID) != f.survey.String() {
		t.Fatalf("disable %+v", out.Data)
	}
	e.problem(e.lifecycle(f.owner, w.ID, "disable"), http.StatusUnprocessableEntity, "invalid_workflow_state", onlyEnabled)
	// Edits after a disable publish as the next version on re-enable.
	e.ok(e.v3(f.owner, http.MethodPatch, "/api/v3/workflows/"+w.ID.String(), map[string]any{"definition": definition(f.survey, "q1")}), http.StatusOK, nil)
	e.ok(e.lifecycle(f.owner, w.ID, "enable"), http.StatusOK, nil)
	if e.count(`SELECT max(version) FROM workflow_versions WHERE workflow_id = ?`, w.ID) != 2 || !strings.Contains(e.text(`SELECT definition::text FROM workflow_versions WHERE workflow_id = ? AND version = 1`, w.ID), "owner@example.com") {
		t.Fatal("versions are not immutable snapshots")
	}
	e.ok(e.lifecycle(f.owner, w.ID, "archive"), http.StatusOK, &out)
	if out.Data.Status != StatusArchived {
		t.Fatalf("archive %+v", out.Data)
	}
	e.problem(e.lifecycle(f.owner, w.ID, "archive"), http.StatusUnprocessableEntity, "invalid_workflow_state", alreadyArchived)
	e.problem(e.lifecycle(f.owner, w.ID, "test"), http.StatusUnprocessableEntity, "invalid_workflow_state", archivedNotTestable)
	e.ok(e.lifecycle(f.owner, w.ID, "unarchive"), http.StatusOK, &out)
	if out.Data.Status != StatusDraft {
		t.Fatalf("unarchive returns to draft, got %s", out.Data.Status)
	}
}

func TestEnableAndTestRefusals(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	notExec := definition(f.survey, "owner@example.com")
	notExec["nodes"].([]any)[0].(map[string]any)["config"].(map[string]any)["subject"] = ""
	w := e.create(f.owner, f.ws, "Incomplete", notExec)
	p := e.problem(e.lifecycle(f.owner, w.ID, "enable"), http.StatusUnprocessableEntity, "workflow_not_executable", notExecutableDetail)
	if len(p.InvalidParams) != 1 || p.InvalidParams[0].Name != "nodes.0.config.subject" || p.InvalidParams[0].Reason != "send_email node send-email is missing subject" {
		t.Fatalf("%+v", p.InvalidParams)
	}
	var tested envelope[TestResult]
	e.ok(e.lifecycle(f.owner, w.ID, "test"), http.StatusOK, &tested)
	if tested.Data.OK || len(tested.Data.Problems) != 1 || tested.Data.Problems[0].Code != problemNotExecutable || tested.Data.Problems[0].Field != "nodes.0.config.subject" {
		t.Fatalf("test %+v", tested.Data)
	}

	otherWorkspace := e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Other') RETURNING id`, f.org)
	foreign := e.insert(`INSERT INTO surveys (workspace_id, name, status) VALUES (?, 'Elsewhere', 'inProgress') RETURNING id`, otherWorkspace)
	w = e.create(f.owner, f.ws, "Foreign survey", definition(foreign, "owner@example.com", testEndingID))
	p = e.problem(e.lifecycle(f.owner, w.ID, "enable"), http.StatusUnprocessableEntity, "workflow_not_executable", notExecutableDetail)
	if len(p.InvalidParams) != 1 || p.InvalidParams[0].Name != surveyField || p.InvalidParams[0].Reason != surveyMissingReason {
		t.Fatalf("%+v", p.InvalidParams)
	}

	w = e.create(f.owner, f.ws, "Gone ending", definition(f.survey, "stranger@example.org", "cm9zr4q7i000108l84gozfggz"))
	p = e.problem(e.lifecycle(f.owner, w.ID, "enable"), http.StatusUnprocessableEntity, "workflow_not_executable", notExecutableDetail)
	if len(p.InvalidParams) != 1 || p.InvalidParams[0].Name != endingField || p.InvalidParams[0].Reason != "Ending card cm9zr4q7i000108l84gozfggz does not exist on the survey." {
		t.Fatalf("%+v", p.InvalidParams)
	}
	e.ok(e.lifecycle(f.owner, w.ID, "test"), http.StatusOK, &tested)
	if tested.Data.OK || len(tested.Data.Problems) != 2 || tested.Data.Problems[0].Code != problemEndingNotFound || tested.Data.Problems[1].Code != problemRecipientBlocked ||
		tested.Data.Problems[1].Message != recipientReason("stranger@example.org") || tested.Data.WorkflowID != w.ID {
		t.Fatalf("test %+v", tested.Data)
	}

	// Literal recipients must be able to access the workspace: an owner can, a member only through a team on this workspace, a deactivated user never.
	member := e.user("Member@Example.com")
	e.member(f.org, member, "member")
	w = e.create(f.owner, f.ws, "Member", definition(f.survey, "member@example.com"))
	p = e.problem(e.lifecycle(f.owner, w.ID, "enable"), http.StatusUnprocessableEntity, "workflow_not_executable", recipientDetail)
	if p.InvalidParams[0].Name != recipientField || p.InvalidParams[0].Reason != recipientReason("member@example.com") {
		t.Fatalf("%+v", p.InvalidParams)
	}
	e.grant(f.org, otherWorkspace, member, "manage")
	e.problem(e.lifecycle(f.owner, w.ID, "enable"), http.StatusUnprocessableEntity, "workflow_not_executable", recipientDetail)
	e.grant(f.org, f.ws, member, "read")
	e.ok(e.lifecycle(f.owner, w.ID, "test"), http.StatusOK, &tested)
	if !tested.Data.OK || len(tested.Data.Problems) != 0 {
		t.Fatalf("test %+v", tested.Data)
	}
	e.ok(e.lifecycle(f.owner, w.ID, "enable"), http.StatusOK, nil)
	e.exec(`UPDATE users SET is_active = false WHERE id = ?`, member)
	w = e.create(f.owner, f.ws, "Deactivated", definition(f.survey, "member@example.com"))
	e.problem(e.lifecycle(f.owner, w.ID, "enable"), http.StatusUnprocessableEntity, "workflow_not_executable", recipientDetail)
	// The dry run creates no run.
	if e.count(`SELECT count(*) FROM workflow_runs`) != 0 {
		t.Fatal("a dry run created a run")
	}
}

func TestRuns(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	w := e.create(f.owner, f.ws, "Notify", definition(f.survey, "owner@example.com"))
	var ids []uuid.UUID
	for i := range 3 {
		r := e.respond(f.survey, true, `{}`, nil)
		ids = append(ids, e.insert(`INSERT INTO workflow_runs (workflow_id, workspace_id, trigger_type, trigger_payload, data, response_id, survey_id, idempotency_key, is_dry_run, status, created_at)
			VALUES (?, ?, 'response.completed', '{"type":"response.completed"}', '{"trigger":{"type":"response.completed"},"steps":[]}', ?, ?, ?, ?, ?, now() - make_interval(mins => ?)) RETURNING id`,
			w.ID, f.ws, r.ID, f.survey, r.ID.String(), i == 2, []string{RunCompleted, RunFailed, RunQueued}[i], 10-i))
	}
	e.exec(`INSERT INTO workflow_run_logs (run_id, sequence, step_id, step_type, status, error) VALUES (?, 1, 'send-email', 'send_email', 'failed', 'boom')`, ids[1])

	var got envelope[resource]
	e.ok(e.v3(f.owner, http.MethodGet, "/api/v3/workflows/"+w.ID.String(), nil), http.StatusOK, &got)
	if got.Data.RunCount != 3 || got.Data.LastRun == nil || got.Data.LastRun.ID != ids[2] || !got.Data.LastRun.IsDryRun {
		t.Fatalf("last run %+v", got.Data)
	}
	var page envelope[[]RunListItem]
	base := "/api/v3/workflows/runs?workspaceId=" + f.ws.String()
	e.ok(e.v3(f.owner, http.MethodGet, base+"&limit=2", nil), http.StatusOK, &page)
	if len(page.Data) != 2 || page.Data[0].ID != ids[2] || page.Data[0].WorkflowName != "Notify" || page.Meta.NextCursor == nil {
		t.Fatalf("runs page %+v", page)
	}
	e.ok(e.v3(f.owner, http.MethodGet, base+"&limit=2&cursor="+*page.Meta.NextCursor, nil), http.StatusOK, &page)
	if len(page.Data) != 1 || page.Data[0].ID != ids[0] || page.Meta.NextCursor != nil {
		t.Fatalf("second page %+v", page)
	}
	e.ok(e.v3(f.owner, http.MethodGet, base+"&filter[isDryRun][eq]=false&filter[status][in]=failed&filter[status][in]=queued&workflowId="+w.ID.String(), nil), http.StatusOK, &page)
	if len(page.Data) != 1 || page.Data[0].ID != ids[1] {
		t.Fatalf("filtered %+v", page.Data)
	}
	if strings.Contains(e.v3(f.owner, http.MethodGet, base, nil).Body.String(), "triggerPayload") {
		t.Fatal("the run list carries payloads")
	}
	p := e.problem(e.v3(f.owner, http.MethodGet, base+"&filter[isDryRun][eq]=yes", nil), http.StatusBadRequest, "bad_request", "")
	if p.InvalidParams[0].Name != "isDryRun" {
		t.Fatalf("%+v", p)
	}
	e.problem(e.v3(f.owner, http.MethodGet, base+"&cursor=nope", nil), http.StatusBadRequest, "bad_request", invalidCursor)

	var detail envelope[RunResource]
	e.ok(e.v3(f.owner, http.MethodGet, "/api/v3/workflows/runs/"+ids[1].String(), nil), http.StatusOK, &detail)
	if detail.Data.ID != ids[1] || len(detail.Data.Logs) != 1 || *detail.Data.Logs[0].Error != "boom" || string(detail.Data.Data) != `{"steps":[]}` ||
		string(detail.Data.TriggerPayload) != `{"type":"response.completed"}` || *detail.Data.IdempotencyKey == "" {
		t.Fatalf("run detail %+v", detail.Data)
	}
	outsider := e.user("outsider@example.com")
	e.problem(e.v3(outsider, http.MethodGet, "/api/v3/workflows/runs/"+ids[1].String(), nil), http.StatusForbidden, "forbidden", "")
}

func TestAuditEvents(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	w := e.create(f.owner, f.ws, "Notify", definition(f.survey, "owner@example.com"))
	e.ok(e.v3(f.owner, http.MethodPatch, "/api/v3/workflows/"+w.ID.String(), map[string]any{"name": "After"}), http.StatusOK, nil)
	e.ok(e.lifecycle(f.owner, w.ID, "enable"), http.StatusOK, nil)
	e.problem(e.lifecycle(f.owner, w.ID, "enable"), http.StatusUnprocessableEntity, "invalid_workflow_state", "")
	var events []map[string]any
	for _, line := range strings.Split(e.logs.String(), "\n") {
		if strings.Contains(line, `"msg":"audit"`) {
			var ev map[string]any
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				t.Fatal(err)
			}
			events = append(events, ev)
		}
	}
	if len(events) != 4 {
		t.Fatalf("%d audit events: %v", len(events), events)
	}
	for i, want := range []struct{ action, status string }{{"created", "success"}, {"updated", "success"}, {"updated", "success"}, {"updated", "failure"}} {
		ev := events[i]
		if ev["action"] != want.action || ev["status"] != want.status || ev["target"].(map[string]any)["type"] != "workflow" || ev["actor"].(map[string]any)["id"] != f.owner.String() {
			t.Fatalf("event %d: %v", i, ev)
		}
		if want.status == "success" && (ev["target"].(map[string]any)["id"] != w.ID.String() || ev["organizationId"] != f.org.String() || !strings.HasPrefix(ev["apiUrl"].(string), webApp+"/api/v3/workflows")) {
			t.Fatalf("event %d: %v", i, ev)
		}
	}
	if events[3]["eventId"] == nil {
		t.Fatalf("a failure carries no event id: %v", events[3])
	}
	// The audit log masks names itself; the diff still shows that the name changed and nothing else in the document did.
	changes := events[1]["changes"].(map[string]any)
	if _, renamed := changes["name"]; !renamed || changes["definition"] != nil {
		t.Fatalf("the update's changes: %v", changes)
	}
	for _, ev := range events {
		if strings.Contains(string(mustJSON(ev["changes"])), "owner@example.com") || strings.Contains(string(mustJSON(ev["changes"])), "support@example.com") {
			t.Fatalf("an address reached the audit log: %v", ev)
		}
	}
	if !strings.Contains(string(mustJSON(events[0]["changes"])), "[redacted:") {
		t.Fatalf("the creation's definition is not redacted: %v", events[0])
	}
}

func TestAuthoringContext(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	teamMember := e.user("team@example.com")
	e.member(f.org, teamMember, "member")
	e.grant(f.org, f.ws, teamMember, "read")
	other := e.user("other@example.com")
	e.member(f.org, other, "member")
	path := "/api/app/workspaces/" + f.ws.String() + "/workflows/authoring-context"
	var ctx AuthoringContext
	e.ok(e.v3(teamMember, http.MethodGet, path, nil), http.StatusOK, &ctx)
	if ctx.MailFrom != "noreply@forma.ylam.ai" || len(ctx.TeamMemberDetails) != 2 {
		t.Fatalf("%+v", ctx)
	}
	if rec := e.v3(other, http.MethodGet, path, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("a member without access got %d", rec.Code)
	}
}
