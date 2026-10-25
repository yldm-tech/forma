package actionclass

import (
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/v3api"
	"github.com/yldm-tech/forma/apps/server/internal/v3api/openapi/openapitest"
	"github.com/yldm-tech/forma/apps/server/internal/v3api/v3apitest"
)

func newEnv(t *testing.T) *v3apitest.Env {
	e := v3apitest.New(t)
	h := NewHandler(NewService(NewRepository(e.DB), e.Authz.Authorizer, e.Limiter, e.Audit), v3apitest.Sessions{}, e.V3)
	h.Register(e.App)
	h.RegisterV3(e.API)
	return e
}

func wsPath(ws uuid.UUID) string { return "/api/app/workspaces/" + ws.String() + "/action-classes" }

func acPath(id uuid.UUID, rest string) string { return "/api/app/action-classes/" + id.String() + rest }

func TestCreateAndListActionClasses(t *testing.T) {
	e := newEnv(t)
	owner, writer, reader, outsider := e.User(), e.User(), e.User(), e.User()
	org, ws := e.Organization(owner)
	e.Member(org, writer, "member")
	e.Member(org, reader, "member")
	e.Grant(org, ws, writer, "readWrite")
	e.Grant(org, ws, reader, "read")

	var code ActionClass
	e.OK(e.Do(v3apitest.Request{Method: http.MethodPost, Path: wsPath(ws), User: writer, Body: map[string]any{
		"name": " Checkout ", "type": "code", "key": " checkout ", "description": "Paid", "workspaceId": uuid.NewString(),
	}}), http.StatusCreated, &code)
	if code.Name != "Checkout" || *code.Key != "checkout" || *code.Description != "Paid" || code.WorkspaceID != ws || string(code.NoCodeConfig) != "null" {
		t.Fatalf("code action %+v", code)
	}
	var noCode ActionClass
	e.OK(e.Do(v3apitest.Request{Method: http.MethodPost, Path: wsPath(ws), User: owner, Body: map[string]any{
		"name": "Pricing", "type": "noCode", "key": "ignored", "noCodeConfig": map[string]any{"type": "pageView", "urlFilters": []any{map[string]any{"value": "/pricing", "rule": "startsWith"}}},
	}}), http.StatusCreated, &noCode)
	if noCode.Key != nil || noCode.Description != nil || string(noCode.NoCodeConfig) != `{"type":"pageView","urlFilters":[{"rule":"startsWith","value":"/pricing"}]}` {
		t.Fatalf("no-code action %+v %s", noCode, noCode.NoCodeConfig)
	}

	// Names and code keys are unique within the workspace.
	e.Fails(e.Do(v3apitest.Request{Method: http.MethodPost, Path: wsPath(ws), User: owner, Body: map[string]any{"name": "Checkout", "type": "code", "key": "other"}}),
		http.StatusConflict, apperr.CodeConflict, "Action with name Checkout already exists")
	e.Fails(e.Do(v3apitest.Request{Method: http.MethodPost, Path: wsPath(ws), User: owner, Body: map[string]any{"name": "Other", "type": "code", "key": "checkout"}}),
		http.StatusConflict, apperr.CodeConflict, "Action with key checkout already exists")
	// Another workspace may reuse both.
	other := e.Workspace(org, "Second")
	e.OK(e.Do(v3apitest.Request{Method: http.MethodPost, Path: wsPath(other), User: owner, Body: map[string]any{"name": "Checkout", "type": "code", "key": "checkout"}}), http.StatusCreated, nil)

	e.Fails(e.Do(v3apitest.Request{Method: http.MethodPost, Path: wsPath(ws), User: owner, Body: map[string]any{"name": "Bad", "type": "noCode", "noCodeConfig": map[string]any{"type": "click", "urlFilters": []any{}}}}),
		http.StatusBadRequest, apperr.CodeValidation, "")
	for _, actor := range []uuid.UUID{reader, outsider} {
		e.Fails(e.Do(v3apitest.Request{Method: http.MethodPost, Path: wsPath(ws), User: actor, Body: map[string]any{"name": "Mine", "type": "code"}}), http.StatusForbidden, apperr.CodeForbidden, "Not authorized")
	}
	e.Fails(e.Do(v3apitest.Request{Method: http.MethodPost, Path: wsPath(uuid.New()), User: owner, Body: map[string]any{"name": "Ghost", "type": "code"}}), http.StatusForbidden, apperr.CodeForbidden, "")

	var list listResponse
	e.OK(e.Do(v3apitest.Request{Method: http.MethodGet, Path: wsPath(ws), User: reader}), http.StatusOK, &list)
	if len(list.ActionClasses) != 2 || list.ActionClasses[0].ID != code.ID || list.ActionClasses[1].ID != noCode.ID {
		t.Fatalf("list %+v", list.ActionClasses)
	}
	e.Fails(e.Do(v3apitest.Request{Method: http.MethodGet, Path: wsPath(ws), User: outsider}), http.StatusForbidden, apperr.CodeForbidden, "")

	audits := e.Logs.Records(t, "audit")
	if len(audits) == 0 || audits[0]["action"] != "created" || audits[0]["status"] != "success" || audits[0]["organizationId"] != org.String() ||
		audits[0]["target"].(map[string]any)["type"] != "actionClass" || audits[0]["target"].(map[string]any)["id"] != code.ID.String() {
		t.Fatalf("audit %v", audits)
	}
}

func TestUpdateAndDeleteActionClass(t *testing.T) {
	e := newEnv(t)
	owner, reader := e.User(), e.User()
	org, ws := e.Organization(owner)
	e.Member(org, reader, "member")
	e.Grant(org, ws, reader, "read")

	var ac ActionClass
	e.OK(e.Do(v3apitest.Request{Method: http.MethodPost, Path: wsPath(ws), User: owner, Body: map[string]any{"name": "Checkout", "type": "code", "key": "checkout", "description": "Paid"}}), http.StatusCreated, &ac)

	// Absent description is kept; switching to no-code clears the key so it can be reused.
	var updated ActionClass
	e.OK(e.Do(v3apitest.Request{Method: http.MethodPatch, Path: acPath(ac.ID, ""), User: owner, Body: map[string]any{
		"name": "Checkout page", "type": "noCode", "noCodeConfig": map[string]any{"type": "exitIntent", "urlFilters": []any{}},
	}}), http.StatusOK, &updated)
	if updated.Name != "Checkout page" || updated.Key != nil || *updated.Description != "Paid" || updated.Type != TypeNoCode || !updated.UpdatedAt.After(ac.UpdatedAt) {
		t.Fatalf("updated %+v", updated)
	}
	// A no-code update without a configuration keeps the stored one; an explicit null description clears it.
	e.OK(e.Do(v3apitest.Request{Method: http.MethodPatch, Path: acPath(ac.ID, ""), User: owner, Body: map[string]any{"name": "Checkout page", "type": "noCode", "noCodeConfig": nil, "description": nil}}), http.StatusOK, &updated)
	if updated.Description != nil || string(updated.NoCodeConfig) != `{"type":"exitIntent","urlFilters":[]}` {
		t.Fatalf("kept config %+v %s", updated, updated.NoCodeConfig)
	}
	e.OK(e.Do(v3apitest.Request{Method: http.MethodPost, Path: wsPath(ws), User: owner, Body: map[string]any{"name": "Reuse", "type": "code", "key": "checkout"}}), http.StatusCreated, nil)
	e.Fails(e.Do(v3apitest.Request{Method: http.MethodPatch, Path: acPath(ac.ID, ""), User: owner, Body: map[string]any{"name": "Reuse", "type": "code"}}), http.StatusConflict, apperr.CodeConflict, "Action with name Reuse already exists")

	e.Fails(e.Do(v3apitest.Request{Method: http.MethodPatch, Path: acPath(ac.ID, ""), User: reader, Body: map[string]any{"name": "X", "type": "code"}}), http.StatusForbidden, apperr.CodeForbidden, "")
	e.Fails(e.Do(v3apitest.Request{Method: http.MethodPatch, Path: acPath(uuid.New(), ""), User: owner, Body: map[string]any{"name": "X", "type": "code"}}), http.StatusNotFound, apperr.CodeNotFound, "")

	// Which surveys an action triggers, split on whether they run.
	running := e.Insert(`INSERT INTO surveys (workspace_id, name, status) VALUES (?, 'Running', 'inProgress') RETURNING id`, ws)
	draft := e.Insert(`INSERT INTO surveys (workspace_id, name) VALUES (?, 'Draft') RETURNING id`, ws)
	e.Exec(`INSERT INTO survey_triggers (survey_id, action_class_id) VALUES (?, ?), (?, ?)`, running, ac.ID, draft, ac.ID)
	var usage SurveyUsage
	e.OK(e.Do(v3apitest.Request{Method: http.MethodGet, Path: acPath(ac.ID, "/surveys"), User: reader}), http.StatusOK, &usage)
	if len(usage.ActiveSurveys) != 1 || usage.ActiveSurveys[0] != "Running" || len(usage.InactiveSurveys) != 1 || usage.InactiveSurveys[0] != "Draft" {
		t.Fatalf("usage %+v", usage)
	}
	e.OK(e.Do(v3apitest.Request{Method: http.MethodGet, Path: acPath(ac.ID, "/surveys?excludeSurveyId="+running.String()), User: reader}), http.StatusOK, &usage)
	if len(usage.ActiveSurveys) != 0 || len(usage.InactiveSurveys) != 1 {
		t.Fatalf("usage excluding %+v", usage)
	}
	e.Fails(e.Do(v3apitest.Request{Method: http.MethodGet, Path: acPath(ac.ID, "/surveys"), User: e.User()}), http.StatusForbidden, apperr.CodeForbidden, "")

	e.Fails(e.Do(v3apitest.Request{Method: http.MethodDelete, Path: acPath(ac.ID, ""), User: reader}), http.StatusForbidden, apperr.CodeForbidden, "")
	e.OK(e.Do(v3apitest.Request{Method: http.MethodDelete, Path: acPath(ac.ID, ""), User: owner}), http.StatusOK, nil)
	if e.Count(`SELECT count(*) FROM action_classes WHERE id = ?`, ac.ID) != 0 || e.Count(`SELECT count(*) FROM survey_triggers WHERE action_class_id = ?`, ac.ID) != 0 {
		t.Fatal("action class or its triggers survived")
	}
	e.Fails(e.Do(v3apitest.Request{Method: http.MethodDelete, Path: acPath(ac.ID, ""), User: owner}), http.StatusNotFound, apperr.CodeNotFound, "")
}

type v3List struct {
	Data []map[string]any `json:"data"`
	Meta struct {
		Limit      int     `json:"limit"`
		NextCursor *string `json:"nextCursor"`
	} `json:"meta"`
}

func TestListActionClassesV3(t *testing.T) {
	e := newEnv(t)
	owner, outsider := e.User(), e.User()
	org, ws := e.Organization(owner)
	other := e.Workspace(org, "Second")
	for _, name := range []string{"A", "B", "C"} {
		e.Exec(`INSERT INTO action_classes (workspace_id, name, type, key, no_code_config) VALUES (?, ?, 'code', ?, NULL)`, ws, name, name)
	}
	e.Exec(`INSERT INTO action_classes (workspace_id, name, type, no_code_config) VALUES (?, 'Pricing', 'noCode', '{"type":"pageView","urlFilters":[]}')`, ws)
	path := "/api/v3/action-classes?workspaceId=" + ws.String()

	var page v3List
	rec := e.Do(v3apitest.Request{Method: http.MethodGet, Path: path, User: owner})
	e.OK(rec, http.StatusOK, &page)
	// The answers are the ones the published OpenAPI document describes.
	doc := openapitest.New(t, V3OpenAPI())
	doc.Check(t, http.MethodGet, "/api/v3/action-classes", rec)
	if len(page.Data) != 4 || page.Meta.Limit != 50 || page.Meta.NextCursor != nil || rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("page %+v", page)
	}
	for _, item := range page.Data {
		if len(item) != 5 || item["id"] == nil || item["type"] == nil {
			t.Fatalf("v3 shape %v", item)
		}
	}
	for i := 1; i < len(page.Data); i++ {
		if page.Data[i-1]["id"].(string) >= page.Data[i]["id"].(string) {
			t.Fatal("not ordered by id")
		}
	}

	// An API key with a grant on the workspace reads it too, page by page.
	key := e.APIKey(org, map[uuid.UUID]string{ws: "read"})
	var seen []string
	cursor := ""
	for {
		var p v3List
		e.OK(e.Do(v3apitest.Request{Method: http.MethodGet, Path: path + "&limit=3" + cursor, APIKey: key}), http.StatusOK, &p)
		for _, item := range p.Data {
			seen = append(seen, item["id"].(string))
		}
		if p.Meta.NextCursor == nil {
			break
		}
		cursor = "&cursor=" + *p.Meta.NextCursor
	}
	if len(seen) != 4 {
		t.Fatalf("paged %v", seen)
	}

	// An API key wins over a session, and a key without a grant on the workspace is refused.
	otherKey := e.APIKey(org, map[uuid.UUID]string{other: "manage"})
	e.Problem(e.Do(v3apitest.Request{Method: http.MethodGet, Path: path, User: owner, APIKey: otherKey}), http.StatusForbidden, "forbidden", "You are not authorized to access this resource")
	e.Problem(e.Do(v3apitest.Request{Method: http.MethodGet, Path: path, APIKey: "unknown"}), http.StatusUnauthorized, "not_authenticated", "Not authenticated")
	e.Problem(e.Do(v3apitest.Request{Method: http.MethodGet, Path: path}), http.StatusUnauthorized, "not_authenticated", "Not authenticated")
	e.Problem(e.Do(v3apitest.Request{Method: http.MethodGet, Path: path, User: outsider}), http.StatusForbidden, "forbidden", "You are not authorized to access this resource")
	e.Problem(e.Do(v3apitest.Request{Method: http.MethodGet, Path: "/api/v3/action-classes?workspaceId=" + uuid.NewString(), User: owner}), http.StatusForbidden, "forbidden", "You are not authorized to access this resource")

	doc.Check(t, http.MethodGet, "/api/v3/action-classes", e.Do(v3apitest.Request{Method: http.MethodGet, Path: path}))
	doc.Check(t, http.MethodGet, "/api/v3/action-classes", e.Do(v3apitest.Request{Method: http.MethodGet, Path: path, User: outsider}))
	p := e.Problem(e.Do(v3apitest.Request{Method: http.MethodGet, Path: path + "&cursor=%25%25", User: owner}), http.StatusBadRequest, "bad_request", "Invalid pagination cursor")
	if len(p.InvalidParams) != 1 || p.InvalidParams[0].Name != "cursor" {
		t.Fatalf("cursor problem %+v", p)
	}
	p = e.Problem(e.Do(v3apitest.Request{Method: http.MethodGet, Path: "/api/v3/action-classes?limit=500", User: owner}), http.StatusBadRequest, "bad_request", "Invalid query parameters")
	want := []v3api.InvalidParam{{Name: "workspaceId", Reason: "Invalid input: expected string, received undefined"}, {Name: "limit", Reason: "Too big: expected number to be <=250"}}
	if !slices.Equal(p.InvalidParams, want) {
		t.Fatalf("query problem %+v", p.InvalidParams)
	}
}
