package contact

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/apikey"
	"github.com/yldm-tech/forma/server/internal/apiv3"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
)

// Ported from forma2's api/v3/contact-attribute-keys/lib/operations.test.ts and lib/auth.ts, against docs/api-v3-reference/src/paths/api_v3_contact-attribute-keys.yml.

type v3List struct {
	Data []map[string]any `json:"data"`
	Meta struct {
		Limit      int     `json:"limit"`
		NextCursor *string `json:"nextCursor"`
	} `json:"meta"`
}

func (e *env) v3(user *uuid.UUID, query string, headers map[string]string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.request(user, http.MethodGet, "/api/v3/contact-attribute-keys"+query, nil, "", headers)
}

func (e *env) problem(rec *httptest.ResponseRecorder, status int, code string) apiv3.Problem {
	e.t.Helper()
	var p apiv3.Problem
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != status || p.Code != code || rec.Header().Get("Content-Type") != "application/problem+json" || rec.Header().Get("Cache-Control") != "private, no-store" || p.RequestID == "" || rec.Header().Get("X-Request-Id") != p.RequestID {
		e.t.Fatalf("got %d %v %s, want %d %s", rec.Code, rec.Header(), rec.Body.String(), status, code)
	}
	return p
}

func TestV3ListContactAttributeKeys(t *testing.T) {
	e := newEnv(t)
	owner, reader, outsider := e.user(), e.user(), e.user()
	org, ws := e.organization(owner)
	e.grant(org, ws, reader, "read")
	_, other := e.organization(outsider)
	plan := e.key(ws, "plan", TypeNumber)
	e.exec(`UPDATE contact_attribute_keys SET description = 'Billing plan' WHERE id = ?`, plan)

	rec := e.v3(&reader, "?workspaceId="+ws.String(), nil)
	var list v3List
	e.ok(rec, http.StatusOK, &list)
	if rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("X-Request-Id") == "" {
		t.Fatalf("headers %v", rec.Header())
	}
	if len(list.Data) != 6 || list.Meta.Limit != 50 || list.Meta.NextCursor != nil {
		t.Fatalf("list %+v", list)
	}
	for i := 1; i < len(list.Data); i++ {
		if list.Data[i-1]["id"].(string) >= list.Data[i]["id"].(string) {
			t.Fatalf("not in id order: %+v", list.Data)
		}
	}
	for _, item := range list.Data {
		if len(item) != 6 {
			t.Fatalf("an item carries exactly id, key, name, description, type and dataType: %v", item)
		}
		if item["id"] == plan.String() && (item["key"] != "plan" || item["name"] != "Plan" || item["description"] != "Billing plan" || item["type"] != "custom" || item["dataType"] != "number") {
			t.Fatalf("plan %v", item)
		}
	}

	// Paging walks every key once.
	var seen []string
	cursor := ""
	for {
		rec := e.v3(&reader, "?workspaceId="+ws.String()+"&limit=4"+cursor, nil)
		var page v3List
		e.ok(rec, http.StatusOK, &page)
		for _, item := range page.Data {
			seen = append(seen, item["id"].(string))
		}
		if page.Meta.NextCursor == nil {
			break
		}
		cursor = "&cursor=" + *page.Meta.NextCursor
	}
	if len(seen) != 6 {
		t.Fatalf("paged %v", seen)
	}

	p := e.problem(e.v3(&reader, "?workspaceId="+ws.String()+"&cursor=%25%25%25%25", nil), http.StatusBadRequest, "bad_request")
	if p.Detail != "Invalid pagination cursor" || len(p.InvalidParams) != 1 || p.InvalidParams[0].Name != "cursor" {
		t.Fatalf("cursor problem %+v", p)
	}
	p = e.problem(e.v3(&reader, "?workspaceId=nope&limit=251&extra=1", nil), http.StatusBadRequest, "bad_request")
	if p.Detail != "Invalid query parameters" || len(p.InvalidParams) != 3 || p.InvalidParams[0].Name != "workspaceId" || p.InvalidParams[1].Reason != "Too big: expected number to be <=250" || p.InvalidParams[2].Reason != `Unrecognized key: "extra"` {
		t.Fatalf("query problem %+v", p)
	}
	e.problem(e.v3(&reader, "?limit=0", nil), http.StatusBadRequest, "bad_request")

	// A workspace the caller may not read and one that does not exist are refused alike.
	for _, id := range []uuid.UUID{other, uuid.New()} {
		p = e.problem(e.v3(&reader, "?workspaceId="+id.String(), nil), http.StatusForbidden, "forbidden")
		if p.Detail != "You are not authorized to access this resource" {
			t.Fatalf("forbidden %+v", p)
		}
	}
	p = e.problem(e.v3(nil, "?workspaceId="+ws.String(), nil), http.StatusUnauthorized, "not_authenticated")
	if p.Detail != "Not authenticated" || p.Title != "Unauthorized" || p.Instance != "/api/v3/contact-attribute-keys" {
		t.Fatalf("unauthenticated %+v", p)
	}
}

func TestV3ListContactAttributeKeysWithAPIKey(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	org, ws := e.organization(owner)
	unlisted := e.workspace(org)
	e.authz.Deliver(t)
	created, err := e.apiKeys.Create(t.Context(), httpapi.Actor{UserID: owner}, org, apikey.CreateInput{Label: "agent", WorkspacePermissions: []apikey.WorkspacePermissionInput{{WorkspaceID: ws, Permission: "read"}}})
	if err != nil {
		t.Fatal(err)
	}

	var list v3List
	e.ok(e.v3(nil, "?workspaceId="+ws.String(), map[string]string{"x-api-key": created.Key}), http.StatusOK, &list)
	if len(list.Data) != 5 {
		t.Fatalf("list %+v", list)
	}
	e.problem(e.v3(nil, "?workspaceId="+unlisted.String(), map[string]string{"x-api-key": created.Key}), http.StatusForbidden, "forbidden")
	// A key is authenticated by itself: a session next to a bad key does not rescue it.
	e.problem(e.v3(&owner, "?workspaceId="+ws.String(), map[string]string{"x-api-key": "fbk_" + strings.Repeat("x", 40)}), http.StatusUnauthorized, "not_authenticated")
}
