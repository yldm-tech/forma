package apikey

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
)

func createBody(label string, access OrganizationAccess, grants ...map[string]string) map[string]any {
	perms := []map[string]string{}
	perms = append(perms, grants...)
	return map[string]any{"label": label, "organizationAccess": access, "workspacePermissions": perms}
}

func grant(ws uuid.UUID, permission string) map[string]string {
	return map[string]string{"workspaceId": ws.String(), "permission": permission}
}

func TestKeyLifecycle(t *testing.T) {
	e := newAppEnv(t)
	org, owner := e.org(), e.user()
	e.member(org, owner, "owner")
	growth, ops, unused := e.workspace(org, "Growth"), e.workspace(org, "Ops"), e.workspace(org, "Unused")
	e.drain()
	token := e.session(owner)

	res := e.do(http.MethodPost, keysPath(org), token, createBody("CI", readAccess, grant(growth, PermissionWrite), grant(ops, PermissionManage)))
	if res.status != http.StatusCreated {
		t.Fatalf("create = %d %s", res.status, res.body)
	}
	if res.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store on the response carrying the plaintext", res.header.Get("Cache-Control"))
	}
	var created Created
	res.decode(t, &created)
	for _, leaked := range []string{"hashedKey", "lookupHash", "hashed_key", "lookup_hash", "createdBy", "lastUsedAt"} {
		if strings.Contains(string(res.body), leaked) {
			t.Fatalf("create response exposes %s: %s", leaked, res.body)
		}
	}
	if !strings.HasPrefix(created.Key, Prefix) || len(created.Key) != 47 || created.Label != "CI" || created.OrganizationAccess != readAccess {
		t.Fatalf("created = %+v", created)
	}
	wantGrants := []WorkspaceGrant{{growth, "Growth", PermissionWrite}, {ops, "Ops", PermissionManage}}
	if len(created.WorkspacePermissions) != 2 || created.WorkspacePermissions[0] != wantGrants[0] || created.WorkspacePermissions[1] != wantGrants[1] {
		t.Fatalf("created grants = %+v, want %+v", created.WorkspacePermissions, wantGrants)
	}

	// Stored as forma2 stores it: the creator, the SHA-256 lookup of the secret and its bcrypt at cost 12, never the plaintext.
	var row struct {
		CreatedBy          uuid.UUID
		HashedKey          string
		LookupHash         string
		OrganizationAccess string
	}
	if err := e.db.Raw(`SELECT created_by, hashed_key, lookup_hash, organization_access::text FROM api_keys WHERE id = ?`, created.ID).Scan(&row).Error; err != nil {
		t.Fatal(err)
	}
	s := created.Key[len(Prefix):]
	if row.CreatedBy != owner || row.LookupHash != sha256Hex(s) || bcrypt.CompareHashAndPassword([]byte(row.HashedKey), []byte(s)) != nil {
		t.Fatalf("stored row = %+v", row)
	}
	if cost, _ := bcrypt.Cost([]byte(row.HashedKey)); cost != 12 {
		t.Fatalf("bcrypt cost = %d", cost)
	}
	if row.OrganizationAccess != `{"accessControl": {"read": true, "write": false}}` {
		t.Fatalf("stored organization access = %s", row.OrganizationAccess)
	}

	// The triggers project the key; the key can then act as the authorization subject it authenticates as.
	e.drain()
	p := e.authenticate(map[string]string{"X-Api-Key": created.Key}, Options{})
	if p == nil {
		t.Fatal("the created key does not authenticate")
	}
	key := p.Actor()
	for _, tc := range []struct {
		action   authz.Action
		resource authz.Resource
		want     bool
	}{
		{authz.WorkspaceWrite, authz.Workspace(growth), true},
		{authz.WorkspaceManage, authz.Workspace(growth), false},
		{authz.WorkspaceManage, authz.Workspace(ops), true},
		{authz.WorkspaceRead, authz.Workspace(unused), false},
		{authz.OrganizationReadAccess, authz.Organization(org), true},
		{authz.OrganizationManageAccess, authz.Organization(org), false},
	} {
		if got := e.can(key, tc.action, tc.resource); got != tc.want {
			t.Errorf("key %s on %s = %v, want %v", tc.action, tc.resource.ID, got, tc.want)
		}
	}

	res = e.do(http.MethodGet, keysPath(org), token, nil)
	var listed []View
	res.decode(t, &listed)
	if res.status != http.StatusOK || len(listed) != 1 || listed[0].ID != created.ID || listed[0].Label != "CI" || len(listed[0].WorkspacePermissions) != 2 {
		t.Fatalf("list = %d %s", res.status, res.body)
	}
	if strings.Contains(string(res.body), s) || strings.Contains(string(res.body), `"key"`) {
		t.Fatalf("list exposes the key: %s", res.body)
	}

	res = e.do(http.MethodPatch, keyPath(org, created.ID), token, map[string]string{"label": "Deploy"})
	var updated View
	res.decode(t, &updated)
	if res.status != http.StatusOK || updated.Label != "Deploy" || len(updated.WorkspacePermissions) != 2 || updated.OrganizationAccess != readAccess {
		t.Fatalf("update = %d %s", res.status, res.body)
	}

	res = e.do(http.MethodDelete, keyPath(org, created.ID), token, nil)
	if res.status != http.StatusNoContent {
		t.Fatalf("delete = %d %s", res.status, res.body)
	}
	if n := e.count(`SELECT count(*) FROM api_key_workspaces WHERE api_key_id = ?`, created.ID); n != 0 {
		t.Fatalf("%d grants survive the key", n)
	}
	if p := e.authenticate(map[string]string{"X-Api-Key": created.Key}, Options{AllowOrganizationOnly: true}); p != nil {
		t.Fatal("a deleted key authenticates")
	}
	e.drain()
	if e.can(key, authz.WorkspaceRead, authz.Workspace(growth)) {
		t.Fatal("a deleted key keeps its access")
	}
	res = e.do(http.MethodGet, keysPath(org), token, nil)
	if res.status != http.StatusOK || strings.TrimSpace(string(res.body)) != "[]" {
		t.Fatalf("list after delete = %d %s", res.status, res.body)
	}
	if res = e.do(http.MethodDelete, keyPath(org, created.ID), token, nil); res.status != http.StatusNotFound || res.code(t) != apperr.CodeNotFound {
		t.Fatalf("second delete = %d %s", res.status, res.body)
	}

	// Audit: who did what to which key, with the input that created it and never a secret or hash.
	events := e.logs.records(t, "audit")
	if len(events) != 4 {
		t.Fatalf("%d audit events, want create, update, delete and the failed delete", len(events))
	}
	created0, deleted := events[0], events[2]
	if created0["action"] != "created" || created0["status"] != "success" || created0["organizationId"] != org.String() {
		t.Fatalf("create event = %+v", created0)
	}
	if target, _ := created0["target"].(map[string]any); target["id"] != created.ID.String() || target["type"] != "apiKey" {
		t.Fatalf("create event target = %+v", created0["target"])
	}
	if actor, _ := created0["actor"].(map[string]any); actor["id"] != owner.String() || actor["type"] != "user" {
		t.Fatalf("create event actor = %+v", created0["actor"])
	}
	if deleted["action"] != "deleted" || events[3]["status"] != "failure" {
		t.Fatalf("delete events = %+v / %+v", deleted, events[3])
	}
	logs := e.logs.String()
	for _, secretValue := range []string{s, row.LookupHash, row.HashedKey} {
		if strings.Contains(logs, secretValue) {
			t.Fatal("a key, lookup hash or bcrypt hash reached the log")
		}
	}
}

func TestOnlyOwnersAndManagersManageKeys(t *testing.T) {
	e := newAppEnv(t)
	org, other := e.org(), e.org()
	ws := e.workspace(org, "W")
	users := map[string]uuid.UUID{}
	for _, role := range []string{"owner", "manager", "member", "billing"} {
		users[role] = e.user()
		e.member(org, users[role], role)
	}
	outsider := e.user()
	e.member(other, outsider, "owner")
	e.drain()
	existing, _ := e.v2Key(org, readAccess, grantInput{WorkspaceID: ws, Permission: PermissionRead})

	for _, role := range []string{"member", "billing", "outsider"} {
		t.Run(role+" is refused", func(t *testing.T) {
			user := outsider
			if role != "outsider" {
				user = users[role]
			}
			token := e.session(user)
			for _, req := range []struct{ method, path string }{
				{http.MethodGet, keysPath(org)},
				{http.MethodPost, keysPath(org)},
				{http.MethodPatch, keyPath(org, existing)},
				{http.MethodDelete, keyPath(org, existing)},
				// A key id that does not exist is refused the same way, so a refused caller cannot probe for ids.
				{http.MethodDelete, keyPath(org, uuid.New())},
			} {
				var body any
				switch req.method {
				case http.MethodPost:
					body = createBody("Sneaky", readAccess, grant(ws, PermissionManage))
				case http.MethodPatch:
					body = map[string]string{"label": "Sneaky"}
				}
				res := e.do(req.method, req.path, token, body)
				if res.status != http.StatusForbidden || res.code(t) != apperr.CodeForbidden {
					t.Errorf("%s %s = %d %s, want 403", req.method, req.path, res.status, res.body)
				}
			}
		})
	}
	if n := e.count(`SELECT count(*) FROM api_keys WHERE organization_id = ?`, org); n != 1 {
		t.Fatalf("%d keys after refused requests, want the 1 that existed", n)
	}
	if n := e.count(`SELECT count(*) FROM api_keys WHERE id = ? AND label = 'key'`, existing); n != 1 {
		t.Fatal("a refused update changed the key")
	}

	t.Run("manager is allowed", func(t *testing.T) {
		token := e.session(users["manager"])
		res := e.do(http.MethodPost, keysPath(org), token, createBody("Manager", OrganizationAccess{AccessControl: AccessFlags{Read: true, Write: true}}))
		if res.status != http.StatusCreated {
			t.Fatalf("create = %d %s", res.status, res.body)
		}
		if res := e.do(http.MethodGet, keysPath(org), token, nil); res.status != http.StatusOK {
			t.Fatalf("list = %d %s", res.status, res.body)
		}
	})

	t.Run("without a session", func(t *testing.T) {
		if res := e.do(http.MethodGet, keysPath(org), "", nil); res.status != http.StatusUnauthorized || res.code(t) != apperr.CodeNotAuthenticated {
			t.Fatalf("list = %d %s", res.status, res.body)
		}
	})

	t.Run("a cross-site mutation is refused before it reaches the key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, keyPath(org, existing), nil)
		req.Header.Set("Origin", "https://evil.example")
		req.Header.Set(httpapi.CSRFHeader, "XMLHttpRequest")
		req.AddCookie(&http.Cookie{Name: "forma.session_token", Value: e.session(users["owner"])})
		w := httptest.NewRecorder()
		e.engine.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), httpapi.CodeCSRF) {
			t.Fatalf("cross-site delete = %d %s", w.Code, w.Body)
		}
		if n := e.count(`SELECT count(*) FROM api_keys WHERE id = ?`, existing); n != 1 {
			t.Fatal("a cross-site request deleted the key")
		}
	})
}

func TestCreateRefusesWorkspacesOutsideTheOrganization(t *testing.T) {
	e := newAppEnv(t)
	org, other := e.org(), e.org()
	owner := e.user()
	e.member(org, owner, "owner")
	own, foreign := e.workspace(org, "Own"), e.workspace(other, "Foreign")
	e.drain()
	token := e.session(owner)

	for name, ws := range map[string]uuid.UUID{"another organization's workspace": foreign, "a workspace that does not exist": uuid.New()} {
		t.Run(name, func(t *testing.T) {
			res := e.do(http.MethodPost, keysPath(org), token, createBody("Cross", readAccess, grant(own, PermissionRead), grant(ws, PermissionManage)))
			var env httpapi.Envelope
			res.decode(t, &env)
			want := "Workspace " + ws.String() + " does not belong to organization " + org.String()
			if res.status != http.StatusForbidden || env.Code != apperr.CodeOperationNotAllowed || env.Message != want {
				t.Fatalf("create = %d %s", res.status, res.body)
			}
		})
	}
	if n := e.count(`SELECT count(*) FROM api_keys`); n != 0 {
		t.Fatalf("%d keys created", n)
	}
}

func TestUpdateAndDeleteStayInsideThePathsOrganization(t *testing.T) {
	e := newAppEnv(t)
	orgA, orgB := e.org(), e.org()
	owner := e.user()
	e.member(orgA, owner, "owner")
	e.member(orgB, owner, "owner")
	wsB := e.workspace(orgB, "B")
	e.drain()
	keyB, _ := e.v2Key(orgB, readAccess, grantInput{WorkspaceID: wsB, Permission: PermissionRead})
	token := e.session(owner)

	// The caller may manage keys in both organizations, but a key is only reachable under its own.
	if res := e.do(http.MethodPatch, keyPath(orgA, keyB), token, map[string]string{"label": "moved"}); res.status != http.StatusNotFound {
		t.Fatalf("update through another organization = %d %s", res.status, res.body)
	}
	if res := e.do(http.MethodDelete, keyPath(orgA, keyB), token, nil); res.status != http.StatusNotFound {
		t.Fatalf("delete through another organization = %d %s", res.status, res.body)
	}
	if n := e.count(`SELECT count(*) FROM api_keys WHERE id = ? AND label = 'key'`, keyB); n != 1 {
		t.Fatal("the key was changed through another organization")
	}
	if res := e.do(http.MethodGet, keysPath(orgA), token, nil); strings.Contains(string(res.body), keyB.String()) {
		t.Fatalf("another organization's key is listed: %s", res.body)
	}

	if res := e.do(http.MethodPatch, keyPath(orgB, keyB), token, map[string]string{"label": "  "}); res.status != http.StatusBadRequest || res.code(t) != apperr.CodeValidation {
		t.Fatalf("blank label = %d %s", res.status, res.body)
	}
	if res := e.do(http.MethodPatch, keyPath(orgB, uuid.New()), token, map[string]string{"label": "x"}); res.status != http.StatusNotFound {
		t.Fatalf("update of a missing key = %d %s", res.status, res.body)
	}
	if res := e.do(http.MethodDelete, keysPath(orgB)+"/not-a-uuid", token, nil); res.status != http.StatusBadRequest {
		t.Fatalf("malformed id = %d %s", res.status, res.body)
	}
}
