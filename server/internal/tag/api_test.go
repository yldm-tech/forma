package tag

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/v3api/v3apitest"
)

func newEnv(t *testing.T) *v3apitest.Env {
	e := v3apitest.New(t)
	h := NewHandler(NewService(NewRepository(e.DB), e.Authz.Authorizer, e.Limiter, e.Audit), v3apitest.Sessions{}, e.V3, e.Auditor)
	h.Register(e.App)
	h.RegisterV3(e.API)
	return e
}

type fixture struct {
	e                     *v3apitest.Env
	org, ws               uuid.UUID
	owner, writer, reader uuid.UUID
	survey                uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	e := newEnv(t)
	f := &fixture{e: e, owner: e.User(), writer: e.User(), reader: e.User()}
	f.org, f.ws = e.Organization(f.owner)
	e.Member(f.org, f.writer, "member")
	e.Member(f.org, f.reader, "member")
	e.Grant(f.org, f.ws, f.writer, "readWrite")
	e.Grant(f.org, f.ws, f.reader, "read")
	f.survey = e.Insert(`INSERT INTO surveys (workspace_id, name) VALUES (?, 'Survey') RETURNING id`, f.ws)
	return f
}

func (f *fixture) tag(ws uuid.UUID, name string) uuid.UUID {
	return f.e.Insert(`INSERT INTO tags (workspace_id, name) VALUES (?, ?) RETURNING id`, ws, name)
}

// responses creates n responses carrying every one of tags.
func (f *fixture) responses(n int, tags ...uuid.UUID) []uuid.UUID {
	var ids []uuid.UUID
	for range n {
		r := f.e.Insert(`INSERT INTO responses (survey_id) VALUES (?) RETURNING id`, f.survey)
		for _, t := range tags {
			f.e.Exec(`INSERT INTO response_tags (response_id, tag_id) VALUES (?, ?)`, r, t)
		}
		ids = append(ids, r)
	}
	return ids
}

func tagPath(id uuid.UUID, rest string) string { return "/api/v3/tags/" + id.String() + rest }

func TestCreateTag(t *testing.T) {
	f := newFixture(t)
	e := f.e
	path := "/api/app/workspaces/" + f.ws.String() + "/tags"

	var created Tag
	e.OK(e.Do(v3apitest.Request{Method: http.MethodPost, Path: path, User: f.writer, Body: map[string]any{"name": "  VIP "}}), http.StatusCreated, &created)
	if created.Name != "VIP" || created.WorkspaceID != f.ws {
		t.Fatalf("created %+v", created)
	}
	e.Fails(e.Do(v3apitest.Request{Method: http.MethodPost, Path: path, User: f.owner, Body: map[string]any{"name": "VIP"}}), http.StatusBadRequest, CodeNameTaken, "Tag with this name already exists")
	for _, name := range []string{"   ", strings.Repeat("x", 256)} {
		e.Fails(e.Do(v3apitest.Request{Method: http.MethodPost, Path: path, User: f.owner, Body: map[string]any{"name": name}}), http.StatusBadRequest, apperr.CodeValidation, "")
	}
	e.OK(e.Do(v3apitest.Request{Method: http.MethodPost, Path: path, User: f.owner, Body: map[string]any{"name": strings.Repeat("é", 255)}}), http.StatusCreated, nil)
	e.Fails(e.Do(v3apitest.Request{Method: http.MethodPost, Path: path, User: f.reader, Body: map[string]any{"name": "Mine"}}), http.StatusForbidden, apperr.CodeForbidden, "")
	e.Fails(e.Do(v3apitest.Request{Method: http.MethodPost, Path: path, User: f.owner, Body: map[string]any{"name": "X"}, CrossSite: true}), http.StatusForbidden, "csrf_rejected", "")
}

type v3Body[T any] struct {
	Data T `json:"data"`
}

func TestListTagsV3(t *testing.T) {
	f := newFixture(t)
	e := f.e
	used, unused := f.tag(f.ws, "Used"), f.tag(f.ws, "Unused")
	f.responses(2, used)
	other := e.Workspace(f.org, "Second")
	f.tag(other, "Elsewhere")

	var list v3Body[[]map[string]any]
	rec := e.Do(v3apitest.Request{Method: http.MethodGet, Path: "/api/v3/tags?workspaceId=" + f.ws.String(), User: f.reader})
	e.OK(rec, http.StatusOK, &list)
	if len(list.Data) != 2 || rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("list %s", rec.Body.String())
	}
	if list.Data[0]["id"] != used.String() || list.Data[0]["count"] != float64(2) || list.Data[1]["id"] != unused.String() || list.Data[1]["count"] != float64(0) ||
		list.Data[0]["workspaceId"] != f.ws.String() || len(list.Data[0]) != 6 || !strings.HasSuffix(list.Data[0]["createdAt"].(string), "Z") || len(list.Data[0]["createdAt"].(string)) != len("2026-01-01T00:00:00.000Z") {
		t.Fatalf("list %s", rec.Body.String())
	}

	e.Problem(e.Do(v3apitest.Request{Method: http.MethodGet, Path: "/api/v3/tags?workspaceId=" + f.ws.String(), User: e.User()}), http.StatusForbidden, "forbidden", "You are not authorized to access this resource")
	e.Problem(e.Do(v3apitest.Request{Method: http.MethodGet, Path: "/api/v3/tags?workspaceId=" + uuid.NewString(), User: f.owner}), http.StatusForbidden, "forbidden", "")
	e.Problem(e.Do(v3apitest.Request{Method: http.MethodGet, Path: "/api/v3/tags?workspaceId=" + f.ws.String() + "&limit=5", User: f.owner}), http.StatusBadRequest, "bad_request", "Invalid query parameters")
	// Session only: an API key, even one with access, is not a session.
	key := e.APIKey(f.org, map[uuid.UUID]string{f.ws: "manage"})
	e.Problem(e.Do(v3apitest.Request{Method: http.MethodGet, Path: "/api/v3/tags?workspaceId=" + f.ws.String(), APIKey: key}), http.StatusUnauthorized, "not_authenticated", "Session required")
}

func TestRenameTagV3(t *testing.T) {
	f := newFixture(t)
	e := f.e
	vip, gold := f.tag(f.ws, "VIP"), f.tag(f.ws, "Gold")
	f.responses(3, vip)

	var renamed v3Body[map[string]any]
	e.OK(e.Do(v3apitest.Request{Method: http.MethodPatch, Path: tagPath(vip, ""), User: f.writer, Body: map[string]any{"name": "  Premium "}}), http.StatusOK, &renamed)
	if renamed.Data["name"] != "Premium" || renamed.Data["count"] != float64(3) || renamed.Data["id"] != vip.String() {
		t.Fatalf("renamed %v", renamed.Data)
	}
	e.OK(e.Do(v3apitest.Request{Method: http.MethodPatch, Path: tagPath(gold, ""), User: f.writer, Body: map[string]any{"name": "Platinum"}}), http.StatusOK, &renamed)
	if renamed.Data["count"] != float64(0) {
		t.Fatalf("unused tag count %v", renamed.Data)
	}

	p := e.Problem(e.Do(v3apitest.Request{Method: http.MethodPatch, Path: tagPath(gold, ""), User: f.writer, Body: map[string]any{"name": "Premium"}}), http.StatusUnprocessableEntity, "unprocessable_content", "Unable to update tag")
	if len(p.InvalidParams) != 1 || p.InvalidParams[0].Name != "name" || p.InvalidParams[0].Reason != CodeNameTaken {
		t.Fatalf("duplicate %+v", p)
	}
	// forma2's wrapper answered 400 for a body that fails its schema: blank, too long, unknown keys, missing.
	for _, body := range []any{map[string]any{"name": "   "}, map[string]any{"name": strings.Repeat("x", 256)}, map[string]any{"name": "A", "workspaceId": f.ws}, map[string]any{}, "{"} {
		e.Problem(e.Do(v3apitest.Request{Method: http.MethodPatch, Path: tagPath(gold, ""), User: f.writer, Body: body}), http.StatusBadRequest, "bad_request", "Invalid request body")
	}
	e.Problem(e.Do(v3apitest.Request{Method: http.MethodPatch, Path: "/api/v3/tags/nope", User: f.writer, Body: map[string]any{"name": "A"}}), http.StatusBadRequest, "bad_request", "Invalid route parameters")

	// The workspace is the tag's own: a reader of it, an outsider and an unknown tag all get the same 403.
	for _, req := range []v3apitest.Request{
		{Method: http.MethodPatch, Path: tagPath(gold, ""), User: f.reader, Body: map[string]any{"name": "A"}},
		{Method: http.MethodPatch, Path: tagPath(gold, ""), User: e.User(), Body: map[string]any{"name": "A"}},
		{Method: http.MethodPatch, Path: tagPath(uuid.New(), ""), User: f.owner, Body: map[string]any{"name": "A"}},
	} {
		e.Problem(e.Do(req), http.StatusForbidden, "forbidden", "You are not authorized to access this resource")
	}
	// A session-authenticated mutation must come from the app.
	e.Problem(e.Do(v3apitest.Request{Method: http.MethodPatch, Path: tagPath(gold, ""), User: f.owner, Body: map[string]any{"name": "A"}, CrossSite: true}), http.StatusForbidden, "forbidden", "Cross-origin request rejected")
	if e.Count(`SELECT count(*) FROM tags WHERE name = 'A'`) != 0 {
		t.Fatal("a refused rename was written")
	}

	audits := e.Logs.Records(t, "audit")
	first := audits[0]
	if first["action"] != "updated" || first["status"] != "success" || first["organizationId"] != f.org.String() || first["apiUrl"] != v3apitest.WebApp+"/api/v3/tags/"+vip.String() ||
		first["target"].(map[string]any)["id"] != vip.String() || first["target"].(map[string]any)["type"] != "tag" || first["actor"].(map[string]any)["id"] != f.writer.String() {
		t.Fatalf("audit %v", first)
	}
	failed := audits[2]
	if failed["status"] != "failure" || failed["eventId"] == nil {
		t.Fatalf("failure audit %v", failed)
	}
}

func TestDeleteTagV3(t *testing.T) {
	f := newFixture(t)
	e := f.e
	vip := f.tag(f.ws, "VIP")
	f.responses(2, vip)

	e.Problem(e.Do(v3apitest.Request{Method: http.MethodDelete, Path: tagPath(vip, ""), User: f.reader}), http.StatusForbidden, "forbidden", "")
	var deleted v3Body[map[string]any]
	e.OK(e.Do(v3apitest.Request{Method: http.MethodDelete, Path: tagPath(vip, ""), User: f.writer}), http.StatusOK, &deleted)
	if deleted.Data["id"] != vip.String() || e.Count(`SELECT count(*) FROM tags WHERE id = ?`, vip) != 0 || e.Count(`SELECT count(*) FROM response_tags WHERE tag_id = ?`, vip) != 0 {
		t.Fatalf("deleted %v", deleted.Data)
	}
	if e.Count(`SELECT count(*) FROM responses WHERE survey_id = ?`, f.survey) != 2 {
		t.Fatal("deleting a tag deleted responses")
	}
	// Gone now, so the same 403 as any unknown tag.
	e.Problem(e.Do(v3apitest.Request{Method: http.MethodDelete, Path: tagPath(vip, ""), User: f.writer}), http.StatusForbidden, "forbidden", "")
}

func TestMergeTagsV3(t *testing.T) {
	f := newFixture(t)
	e := f.e
	source, target := f.tag(f.ws, "Customer"), f.tag(f.ws, "Client")
	both := f.responses(2, source, target)
	onlySource := f.responses(3, source)
	f.responses(1, target)

	merge := func(user, id uuid.UUID, body any) v3apitest.Request {
		return v3apitest.Request{Method: http.MethodPost, Path: tagPath(id, "/merge"), User: user, Body: body}
	}
	p := e.Problem(e.Do(merge(f.writer, source, map[string]any{"newTagId": source})), http.StatusBadRequest, "bad_request", "A tag cannot be merged into itself")
	if len(p.InvalidParams) != 1 || p.InvalidParams[0].Name != "newTagId" {
		t.Fatalf("self merge %+v", p)
	}
	other := e.Workspace(f.org, "Second")
	foreign := f.tag(other, "Elsewhere")
	e.Problem(e.Do(merge(f.owner, source, map[string]any{"newTagId": foreign})), http.StatusForbidden, "forbidden", "Tags must be in the same workspace")
	e.Problem(e.Do(merge(f.owner, source, map[string]any{"newTagId": uuid.New()})), http.StatusForbidden, "forbidden", "You are not authorized to access this resource")
	e.Problem(e.Do(merge(f.reader, source, map[string]any{"newTagId": target})), http.StatusForbidden, "forbidden", "You are not authorized to access this resource")
	e.Problem(e.Do(merge(f.writer, source, map[string]any{"newTagId": "nope"})), http.StatusBadRequest, "bad_request", "Invalid request body")
	e.Problem(e.Do(merge(f.writer, source, map[string]any{})), http.StatusBadRequest, "bad_request", "Invalid request body")

	var merged v3Body[map[string]any]
	e.OK(e.Do(merge(f.writer, source, map[string]any{"newTagId": target})), http.StatusOK, &merged)
	if merged.Data["id"] != target.String() {
		t.Fatalf("merged %v", merged.Data)
	}
	if e.Count(`SELECT count(*) FROM tags WHERE id = ?`, source) != 0 || e.Count(`SELECT count(*) FROM response_tags WHERE tag_id = ?`, target) != 6 {
		t.Fatal("merge did not move every response onto the target")
	}
	for _, r := range append(both, onlySource...) {
		if e.Count(`SELECT count(*) FROM response_tags WHERE response_id = ?`, r) != 1 {
			t.Fatalf("response %s does not carry exactly the target", r)
		}
	}

	var audit map[string]any
	for _, rec := range e.Logs.Records(t, "audit") {
		if rec["action"] == "merged" && rec["status"] == "success" {
			audit = rec
		}
	}
	if audit == nil || audit["target"].(map[string]any)["id"] != source.String()+"-"+target.String() {
		t.Fatalf("merge audit %v", audit)
	}
}
