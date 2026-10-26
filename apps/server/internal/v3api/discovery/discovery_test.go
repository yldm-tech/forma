package discovery

import (
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/v3api"
	"github.com/yldm-tech/forma/apps/server/internal/v3api/openapi/openapitest"
	"github.com/yldm-tech/forma/apps/server/internal/v3api/v3apitest"
)

func newEnv(t *testing.T) (*v3apitest.Env, *openapitest.Document) {
	e := v3apitest.New(t)
	h, err := NewHandler(e.Config, e.DB, e.Authz.Authorizer, e.V3)
	if err != nil {
		t.Fatal(err)
	}
	h.RegisterV3(e.API)
	return e, openapitest.Parse(t, h.document)
}

type meBody struct {
	Data Me `json:"data"`
}

func TestMeWithAnAPIKey(t *testing.T) {
	e, doc := newEnv(t)
	owner := e.User()
	org, ws := e.Organization(owner)
	second := e.Workspace(org, "Analytics")
	unlisted := e.Workspace(org, "Unlisted")
	e.Organization(e.User())
	key := e.APIKey(org, map[uuid.UUID]string{ws: "manage", second: "read"})
	e.Exec(`UPDATE api_keys SET label = 'CI', organization_access = '{"accessControl":{"read":true,"write":false}}' WHERE organization_id = ?`, org)

	rec := e.Do(v3apitest.Request{Method: http.MethodGet, Path: "/api/v3/me", APIKey: key})
	var body meBody
	e.OK(rec, http.StatusOK, &body)
	doc.Check(t, http.MethodGet, "/api/v3/me", rec)
	me := body.Data
	if me.Type != "apiKey" || me.User != nil || me.APIKey == nil || me.APIKey.Label != "CI" || !me.APIKey.OrganizationAccess.AccessControl.Read || me.APIKey.OrganizationAccess.AccessControl.Write || me.APIKey.ExpiresAt != nil {
		t.Fatalf("me %+v", me)
	}
	if len(me.Organizations) != 1 || me.Organizations[0].ID != org || me.Organizations[0].Name != "Acme" || me.Organizations[0].Role != nil {
		t.Fatalf("organizations %+v", me.Organizations)
	}
	want := []Workspace{
		{ID: second, Name: "Analytics", OrganizationID: org, Permission: "read"},
		{ID: ws, Name: "My workspace", OrganizationID: org, Permission: "manage"},
	}
	if !slices.Equal(me.Workspaces, want) {
		t.Fatalf("workspaces %+v, want %+v (not %s)", me.Workspaces, want, unlisted)
	}
	if rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("headers %v", rec.Header())
	}

	// A key with only organization access (no workspace grant), which the API Keys page creates for the members and teams routes, is answered too: its organization, its access and no workspaces.
	orgOnly := e.APIKey(org, nil)
	// It expires, which /me tells its holder.
	expires := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Microsecond)
	e.Exec(`UPDATE api_keys SET label = 'Directory', organization_access = '{"accessControl":{"read":true,"write":false}}', expires_at = ? WHERE label = 'test' AND organization_id = ?`, expires, org)
	rec = e.Do(v3apitest.Request{Method: http.MethodGet, Path: "/api/v3/me", APIKey: orgOnly})
	body = meBody{}
	e.OK(rec, http.StatusOK, &body)
	doc.Check(t, http.MethodGet, "/api/v3/me", rec)
	if me := body.Data; me.Type != "apiKey" || me.APIKey == nil || me.APIKey.Label != "Directory" || !me.APIKey.OrganizationAccess.AccessControl.Read || me.APIKey.ExpiresAt == nil || !me.APIKey.ExpiresAt.Equal(expires) || len(me.Organizations) != 1 || me.Organizations[0].ID != org || me.Workspaces == nil || len(me.Workspaces) != 0 {
		t.Fatalf("an organization-only key %+v", me)
	}
	if !strings.Contains(rec.Body.String(), `"workspaces":[]`) {
		t.Fatalf("an organization-only key's workspaces are not an empty list: %s", rec.Body.String())
	}
	// Every other v3 route, mounted with AuthBoth, still refuses it.
	e.API.GET("/probe", e.V3.Auth(v3api.AuthBoth), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	e.Problem(e.Do(v3apitest.Request{Method: http.MethodGet, Path: "/api/v3/probe", APIKey: orgOnly}), http.StatusUnauthorized, "not_authenticated", "Not authenticated")
	if rec := e.Do(v3apitest.Request{Method: http.MethodGet, Path: "/api/v3/probe", APIKey: key}); rec.Code != http.StatusNoContent {
		t.Fatalf("a key with a workspace grant on an AuthBoth route = %d", rec.Code)
	}

	// No credentials, or a key that matches nothing, is a 401 problem.
	for _, r := range []v3apitest.Request{{Method: http.MethodGet, Path: "/api/v3/me"}, {Method: http.MethodGet, Path: "/api/v3/me", APIKey: "fbk_unknown"}} {
		rec := e.Do(r)
		e.Problem(rec, http.StatusUnauthorized, "not_authenticated", "Not authenticated")
		doc.Check(t, http.MethodGet, "/api/v3/me", rec)
	}
}

func TestMeWithASession(t *testing.T) {
	e, doc := newEnv(t)
	user := e.User()
	// An owner manages every workspace of their organization.
	owned, ownedWS := e.Organization(user)
	// A member reaches only the workspaces their teams grant, at the level granted.
	team := e.Insert(`INSERT INTO organizations (name) VALUES ('Beta') RETURNING id`)
	e.Member(team, user, "member")
	readWS, writeWS, manageWS := e.Workspace(team, "Read"), e.Workspace(team, "Write"), e.Workspace(team, "Manage")
	e.Workspace(team, "Hidden")
	e.Grant(team, readWS, user, "read")
	e.Grant(team, writeWS, user, "readWrite")
	e.Grant(team, manageWS, user, "manage")
	// A billing member reaches no workspace.
	billing := e.Insert(`INSERT INTO organizations (name) VALUES ('Accounts') RETURNING id`)
	e.Member(billing, user, "billing")
	e.Workspace(billing, "Ledger")

	rec := e.Do(v3apitest.Request{Method: http.MethodGet, Path: "/api/v3/me", User: user})
	var body meBody
	e.OK(rec, http.StatusOK, &body)
	doc.Check(t, http.MethodGet, "/api/v3/me", rec)
	me := body.Data
	if me.Type != "user" || me.APIKey != nil || me.User == nil || me.User.ID != user || me.User.Name != "Ada" || !strings.HasSuffix(me.User.Email, "@example.com") {
		t.Fatalf("me %+v", me)
	}
	var orgs []string
	for _, o := range me.Organizations {
		orgs = append(orgs, o.Name+":"+*o.Role)
	}
	if !slices.Equal(orgs, []string{"Accounts:billing", "Acme:owner", "Beta:member"}) {
		t.Fatalf("organizations %v", orgs)
	}
	want := []Workspace{
		{ID: manageWS, Name: "Manage", OrganizationID: team, Permission: "manage"},
		{ID: ownedWS, Name: "My workspace", OrganizationID: owned, Permission: "manage"},
		{ID: readWS, Name: "Read", OrganizationID: team, Permission: "read"},
		{ID: writeWS, Name: "Write", OrganizationID: team, Permission: "write"},
	}
	if !slices.Equal(me.Workspaces, want) {
		t.Fatalf("workspaces %+v, want %+v", me.Workspaces, want)
	}

	// A user with no organization is still answered.
	rec = e.Do(v3apitest.Request{Method: http.MethodGet, Path: "/api/v3/me", User: e.User()})
	e.OK(rec, http.StatusOK, &body)
	doc.Check(t, http.MethodGet, "/api/v3/me", rec)
	if len(body.Data.Organizations) != 0 || len(body.Data.Workspaces) != 0 {
		t.Fatalf("a user without organizations %+v", body.Data)
	}
}

func TestOpenAPIDocumentIsPublic(t *testing.T) {
	e, _ := newEnv(t)
	rec := e.Do(v3apitest.Request{Method: http.MethodGet, Path: "/api/v3/openapi.json", CrossSite: true})
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json; charset=utf-8" || rec.Header().Get("Cache-Control") != documentCacheControl || rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("GET /api/v3/openapi.json = %d %v", rec.Code, rec.Header())
	}
	var doc struct {
		OpenAPI string           `json:"openapi"`
		Servers []map[string]any `json:"servers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil || doc.OpenAPI != "3.1.0" || len(doc.Servers) != 1 || doc.Servers[0]["url"] != v3apitest.WebApp {
		t.Fatalf("document %s: %v", rec.Body.String()[:200], err)
	}
}

var pathParam = regexp.MustCompile(`\{([^}]+)\}`)

// The document is well formed: unique operation ids, every reference names a schema it defines, and every path parameter is declared.
func TestDocument(t *testing.T) {
	raw, err := Document("https://forma.example.com")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
			Parameters  []struct {
				Name string `json:"name"`
				In   string `json:"in"`
			} `json:"parameters"`
		} `json:"paths"`
		Components struct {
			Schemas   map[string]json.RawMessage `json:"schemas"`
			Responses map[string]json.RawMessage `json:"responses"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for path, item := range doc.Paths {
		for method, op := range item {
			if op.OperationID == "" || ids[op.OperationID] {
				t.Errorf("%s %s: operation id %q is empty or taken", method, path, op.OperationID)
			}
			ids[op.OperationID] = true
			var declared []string
			for _, p := range op.Parameters {
				if p.In == "path" {
					declared = append(declared, p.Name)
				}
			}
			var inPath []string
			for _, m := range pathParam.FindAllStringSubmatch(path, -1) {
				inPath = append(inPath, m[1])
			}
			if !slices.Equal(declared, inPath) {
				t.Errorf("%s %s declares path parameters %q, the path has %q", method, path, declared, inPath)
			}
		}
	}
	for _, m := range regexp.MustCompile(`"\$ref":"#/components/(schemas|responses)/([^"]+)"`).FindAllSubmatch(compact(t, raw), -1) {
		defined := doc.Components.Schemas
		if string(m[1]) == "responses" {
			defined = doc.Components.Responses
		}
		if _, ok := defined[string(m[2])]; !ok {
			t.Errorf("reference to undefined %s %s", m[1], m[2])
		}
	}
	if len(ids) < 25 {
		t.Fatalf("only %d operations", len(ids))
	}
}

func compact(t *testing.T, raw []byte) []byte {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
