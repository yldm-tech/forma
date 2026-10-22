package contact

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Ported from forma2's contact-attribute-keys.test.ts and attributes/actions.ts.

func keyPath(id uuid.UUID, rest string) string {
	return "/api/app/contact-attribute-keys/" + id.String() + rest
}

func TestCreateKey(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)

	var k Key
	e.ok(e.do(owner, http.MethodPost, wsPath(ws, "/contact-attribute-keys"), map[string]any{"key": "job_description"}), http.StatusCreated, &k)
	if k.Key != "job_description" || k.Name == nil || *k.Name != "Job Description" || k.Description != nil || k.Type != KeyTypeCustom || k.DataType != TypeString || k.WorkspaceID != ws {
		t.Fatalf("created %+v", k)
	}

	e.ok(e.do(owner, http.MethodPost, wsPath(ws, "/contact-attribute-keys"), map[string]any{"key": "signup", "name": "Signed up", "description": "", "dataType": "date"}), http.StatusCreated, &k)
	if *k.Name != "Signed up" || k.Description == nil || *k.Description != "" || k.DataType != TypeDate {
		t.Fatalf("explicit fields: %+v", k)
	}

	e.fails(e.do(owner, http.MethodPost, wsPath(ws, "/contact-attribute-keys"), map[string]any{"key": "signup"}), http.StatusBadRequest, CodeKeyExists, "Attribute key already exists")
	e.fails(e.do(owner, http.MethodPost, wsPath(ws, "/contact-attribute-keys"), map[string]any{"key": "email"}), http.StatusBadRequest, CodeKeyExists, "Attribute key already exists")
	e.fails(e.do(owner, http.MethodPost, wsPath(ws, "/contact-attribute-keys"), map[string]any{"key": "Plan"}), http.StatusBadRequest, CodeKeyUnsafe, "Key must be a safe identifier: only lowercase letters, numbers, and underscores, and must start with a letter")
	e.fails(e.do(owner, http.MethodPost, wsPath(ws, "/contact-attribute-keys"), map[string]any{"key": "1plan"}), http.StatusBadRequest, CodeKeyUnsafe, "")
	e.fails(e.do(owner, http.MethodPost, wsPath(ws, "/contact-attribute-keys"), map[string]any{"key": "user_id"}), http.StatusBadRequest, CodeKeyReserved, "Key is reserved for the v5.1 safe-identifier default attribute migration. Reserved keys: user_id, first_name, last_name.")
	e.fails(e.do(owner, http.MethodPost, wsPath(ws, "/contact-attribute-keys"), map[string]any{"key": "plan", "dataType": "boolean"}), http.StatusBadRequest, "validation_error", "")

	if !strings.Contains(e.logs.String(), `"action":"created"`) || !strings.Contains(e.logs.String(), `"type":"contactAttributeKey"`) {
		t.Fatalf("no audit event: %s", e.logs.String())
	}
}

func TestKeyAccess(t *testing.T) {
	e := newEnv(t)
	owner, reader, outsider := e.user(), e.user(), e.user()
	org, ws := e.organization(owner)
	e.grant(org, ws, reader)
	_, other := e.organization(outsider)

	var keys []Key
	e.ok(e.do(reader, http.MethodGet, wsPath(ws, "/contact-attribute-keys"), nil), http.StatusOK, &keys)
	if len(keys) != 5 || keys[0].Key != "userId" {
		t.Fatalf("keys %+v", keys)
	}
	e.fails(e.do(reader, http.MethodPost, wsPath(ws, "/contact-attribute-keys"), map[string]any{"key": "plan"}), http.StatusForbidden, "forbidden", "Not authorized")
	e.fails(e.do(outsider, http.MethodGet, wsPath(ws, "/contact-attribute-keys"), nil), http.StatusForbidden, "forbidden", "")

	foreign := e.key(other, "plan", TypeString)
	e.fails(e.do(owner, http.MethodPatch, keyPath(foreign, ""), map[string]any{"name": "Mine"}), http.StatusForbidden, "forbidden", "")
	e.fails(e.do(owner, http.MethodDelete, keyPath(uuid.New(), ""), nil), http.StatusNotFound, "not_found", "")
}

func TestUpdateAndDeleteKey(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	plan := e.key(ws, "plan", TypeString)

	var k Key
	e.ok(e.do(owner, http.MethodPatch, keyPath(plan, ""), map[string]any{"name": "Plan name"}), http.StatusOK, &k)
	if *k.Name != "Plan name" || k.Description != nil || k.Key != "plan" {
		t.Fatalf("updated %+v", k)
	}
	e.ok(e.do(owner, http.MethodPatch, keyPath(plan, ""), map[string]any{"description": "Billing plan", "key": "renamed", "dataType": "number"}), http.StatusOK, &k)
	if *k.Name != "Plan name" || *k.Description != "Billing plan" || k.Key != "plan" || k.DataType != TypeString {
		t.Fatalf("key and dataType must not change: %+v", k)
	}

	email := e.keyID(ws, "email")
	e.fails(e.do(owner, http.MethodPatch, keyPath(email, ""), map[string]any{"name": "Mail"}), http.StatusForbidden, "operation_not_allowed", "Cannot update default contact attribute key")
	e.fails(e.do(owner, http.MethodDelete, keyPath(email, ""), nil), http.StatusForbidden, "operation_not_allowed", "Cannot delete default contact attribute key")

	c := e.contact(ws, map[string]string{"email": "a@example.com", "plan": "pro"})
	e.ok(e.do(owner, http.MethodDelete, keyPath(plan, ""), nil), http.StatusOK, nil)
	if got := e.values(c); got["plan"] != "" || got["email"] != "a@example.com" {
		t.Fatalf("deleting a key deletes its values only: %v", got)
	}
	e.fails(e.do(owner, http.MethodDelete, keyPath(plan, ""), nil), http.StatusNotFound, "not_found", "contactAttributeKey with ID "+plan.String()+" not found")
}

func TestDistinctValues(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	e.contact(ws, map[string]string{"plan": "pro"})
	e.contact(ws, map[string]string{"plan": "free"})
	e.contact(ws, map[string]string{"plan": "pro"})
	e.contact(ws, map[string]string{"plan": ""})

	var values []string
	e.ok(e.do(owner, http.MethodGet, keyPath(e.keyID(ws, "plan"), "/values"), nil), http.StatusOK, &values)
	if strings.Join(values, ",") != "free,pro" {
		t.Fatalf("values %v", values)
	}
}
