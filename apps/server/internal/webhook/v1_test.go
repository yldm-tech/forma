package webhook

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type v1Error struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

type v1Data[T any] struct {
	Data T `json:"data"`
}

func expectV1Error(t *testing.T, name string, res *httptest.ResponseRecorder, rec v1Error, status int, code, message string) {
	t.Helper()
	if res.Code != status || rec.Code != code || rec.Message != message {
		t.Errorf("%s: %d %+v, want %d %s %q", name, res.Code, rec, status, code, message)
	}
}

// forma2's /api/v1/webhooks as Zapier, Make and n8n call it: API key only, the v1 envelope, 401 for a refused key, the secret in the create answer only, and environmentId on every webhook.
func TestV1Lifecycle(t *testing.T) {
	e := newEnv(t, false)
	owner := e.user()
	org, ws := e.organization(owner)
	other := e.workspace(org)
	sv := e.survey(ws)
	key, keyID := e.apiKey(org, ws, "manage", other, "read")

	// No key, and a session is never read.
	res := e.do(http.MethodGet, "/api/v1/webhooks", asUser(owner), nil)
	body := decode[v1Error](t, res)
	expectV1Error(t, "no key", res, body, http.StatusUnauthorized, "not_authenticated", "Not authenticated")
	if body.Details["x-Api-Key"] != "Header not provided or API Key invalid" || res.Header().Get("Cache-Control") != "private, no-store" {
		t.Errorf("not authenticated = %+v, %v", body, res.Header())
	}

	// environmentId is accepted in place of workspaceId, and every webhook carries its workspace's id as its environmentId.
	res = e.do(http.MethodPost, "/api/v1/webhooks", asKey(key), map[string]any{
		"environmentId": ws.String(), "url": "https://93.184.216.34/zap", "source": "zapier",
		"triggers": []string{"responseFinished"}, "surveyIds": []string{sv.String()}, "name": "Zap",
	})
	if res.Code != http.StatusOK {
		t.Fatalf("create = %d %s", res.Code, res.Body)
	}
	created := decode[v1Data[View]](t, res).Data
	if created.WorkspaceID != ws || created.Source != SourceZapier || created.Secret == nil || !validSecret(*created.Secret) || created.EnvironmentID == nil || *created.EnvironmentID != ws.String() {
		t.Fatalf("created = %+v", created)
	}
	for _, field := range []string{`"id":`, `"name":"Zap"`, `"createdAt":`, `"url":`, `"source":"zapier"`, `"workspaceId":`, `"triggers":["responseFinished"]`, `"surveyIds":["` + sv.String() + `"]`, `"secret":"whsec_`, `"environmentId":"` + ws.String() + `"`} {
		if !strings.Contains(res.Body.String(), field) {
			t.Errorf("create answer lacks %s: %s", field, res.Body)
		}
	}
	ev := e.lastAudit()
	if ev.Action != "created" || ev.Target.ID != created.ID.String() || ev.Actor.ID != keyID.String() || ev.Actor.Type != "api" || ev.OrganizationID != org.String() || ev.Status != "success" || ev.Changes["secret"] != "********" {
		t.Errorf("create audit = %+v", ev)
	}

	e.insert(`INSERT INTO webhooks (workspace_id, url, triggers) VALUES (?, 'https://example.com/other', '{responseCreated}') RETURNING id`, other)
	e.insert(`INSERT INTO webhooks (workspace_id, url, triggers) VALUES (?, 'https://example.com/none', '{responseCreated}') RETURNING id`, e.workspace(org))
	res = e.do(http.MethodGet, "/api/v1/webhooks", asKey(key), nil)
	list := decode[v1Data[[]View]](t, res).Data
	if res.Code != http.StatusOK || len(list) != 2 || strings.Contains(res.Body.String(), "secret") {
		t.Fatalf("list = %d %s", res.Code, res.Body)
	}
	for _, w := range list {
		if w.WorkspaceID == other && *w.EnvironmentID != other.String() {
			t.Errorf("a webhook's environmentId is its workspace's id: %+v", w)
		}
	}

	res = e.do(http.MethodGet, "/api/v1/webhooks/"+created.ID.String(), asKey(key), nil)
	got := decode[v1Data[View]](t, res).Data
	if res.Code != http.StatusOK || got.ID != created.ID || strings.Contains(res.Body.String(), "secret") || *got.EnvironmentID != ws.String() {
		t.Fatalf("get = %d %s", res.Code, res.Body)
	}

	readKey, _ := e.apiKey(org, ws, "write")
	res = e.do(http.MethodDelete, "/api/v1/webhooks/"+created.ID.String(), asKey(readKey), nil)
	expectV1Error(t, "delete without manage", res, decode[v1Error](t, res), http.StatusUnauthorized, "unauthorized", "You are not authorized to access this resource")
	if ev := e.lastAudit(); ev.Action != "deleted" || ev.Status != "failure" || ev.Target.ID != created.ID.String() {
		t.Errorf("refused delete audit = %+v", ev)
	}

	res = e.do(http.MethodDelete, "/api/v1/webhooks/"+created.ID.String(), asKey(key), nil)
	deleted := decode[v1Data[View]](t, res).Data
	if res.Code != http.StatusOK || deleted.ID != created.ID || strings.Contains(res.Body.String(), "secret") || deleted.EnvironmentID == nil {
		t.Fatalf("delete = %d %s", res.Code, res.Body)
	}
	if ev := e.lastAudit(); ev.Action != "deleted" || ev.Status != "success" || ev.Changes["url"] != "https://93.184.216.34/zap" {
		t.Errorf("delete audit = %+v", ev)
	}
	res = e.do(http.MethodGet, "/api/v1/webhooks/"+created.ID.String(), asKey(key), nil)
	notFound := decode[v1Error](t, res)
	expectV1Error(t, "deleted", res, notFound, http.StatusNotFound, "not_found", "Webhook not found")
	if notFound.Details["resource_id"] != created.ID.String() || notFound.Details["resource_type"] != "Webhook" {
		t.Errorf("not found details = %+v", notFound.Details)
	}
	res = e.do(http.MethodDelete, "/api/v1/webhooks/"+created.ID.String(), asKey(key), nil)
	expectV1Error(t, "second delete", res, decode[v1Error](t, res), http.StatusNotFound, "not_found", "Webhook not found")
}

func TestV1CreateRefusals(t *testing.T) {
	e := newEnv(t, false)
	owner := e.user()
	org, ws := e.organization(owner)
	readKey, _ := e.apiKey(org, ws, "read")
	writeKey, _ := e.apiKey(org, ws, "write")
	otherOwner := e.user()
	_, foreignWS := e.organization(otherOwner)
	foreignSurvey := e.survey(foreignWS)
	valid := func(extra map[string]any) map[string]any {
		body := map[string]any{"workspaceId": ws.String(), "url": "https://93.184.216.34/hook", "triggers": []string{"responseFinished"}}
		for k, v := range extra {
			body[k] = v
		}
		return body
	}
	cases := []struct {
		name    string
		key     string
		body    any
		status  int
		code    string
		message string
		details map[string]any
	}{
		{"malformed", writeKey, "{", http.StatusBadRequest, "bad_request", "Malformed JSON input, please check your request body", nil},
		{"no workspace", writeKey, map[string]any{"url": "https://93.184.216.34/"}, http.StatusBadRequest, "bad_request", "workspaceId must be provided", nil},
		{"numeric workspace", writeKey, map[string]any{"workspaceId": 5}, http.StatusBadRequest, "bad_request", "workspaceId must be a string", nil},
		{"unknown workspace", writeKey, valid(map[string]any{"workspaceId": "clnope"}), http.StatusNotFound, "not_found", "Workspace not found", map[string]any{"resource_id": "clnope", "resource_type": "Workspace"}},
		{"read key", readKey, valid(nil), http.StatusUnauthorized, "unauthorized", "You are not authorized to access this resource", nil},
		{"foreign workspace", writeKey, valid(map[string]any{"workspaceId": foreignWS.String()}), http.StatusUnauthorized, "unauthorized", "You are not authorized to access this resource", nil},
		{"invalid fields", writeKey, valid(map[string]any{"url": "nope", "triggers": []string{}, "source": "carrier-pigeon"}), http.StatusBadRequest, "bad_request", "Fields are missing or incorrectly formatted", map[string]any{
			"url": "Invalid URL", "triggers": "At least one trigger is required", "source": `Invalid option: expected one of "user"|"zapier"|"make"|"n8n"|"activepieces"`,
		}},
		{"missing fields", writeKey, map[string]any{"workspaceId": ws.String(), "triggers": []any{"responseFinished", "nope"}}, http.StatusBadRequest, "bad_request", "Fields are missing or incorrectly formatted", map[string]any{
			"url": "Invalid input: expected string, received undefined", "triggers.1": `Invalid option: expected one of "responseFinished"|"responseCreated"|"responseUpdated"`,
		}},
		{"private URL", writeKey, valid(map[string]any{"url": "http://10.0.0.1/hook"}), http.StatusBadRequest, "bad_request", "Webhook URL must not point to private or internal IP addresses", nil},
		{"foreign survey", writeKey, valid(map[string]any{"surveyIds": []string{foreignSurvey.String()}}), http.StatusBadRequest, "bad_request", "Every survey of a webhook must belong to its workspace", nil},
	}
	for _, tc := range cases {
		res := e.do(http.MethodPost, "/api/v1/webhooks", asKey(tc.key), tc.body)
		got := decode[v1Error](t, res)
		expectV1Error(t, tc.name, res, got, tc.status, tc.code, tc.message)
		for k, v := range tc.details {
			if got.Details[k] != v {
				t.Errorf("%s: details[%s] = %v, want %v", tc.name, k, got.Details[k], v)
			}
		}
	}
	big := `{"workspaceId":"` + ws.String() + `","pad":"` + strings.Repeat("x", 2*1024*1024) + `"}`
	res := e.do(http.MethodPost, "/api/v1/webhooks", asKey(writeKey), big)
	tooLarge := decode[v1Error](t, res)
	if res.Code != http.StatusRequestEntityTooLarge || tooLarge.Code != "payload_too_large" || tooLarge.Details["error"] != "Request body must not exceed 2097152 bytes" {
		t.Errorf("too large = %d %+v", res.Code, tooLarge)
	}
	if res := e.do(http.MethodGet, "/api/v1/webhooks/"+uuid.NewString(), asKey("not-a-key"), nil); res.Code != http.StatusUnauthorized {
		t.Errorf("invalid key = %d", res.Code)
	}
}
