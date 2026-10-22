package contact

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Ported from forma2's contacts.test.ts, attributes.test.ts and update-contact-attributes.test.ts.

func contactPath(id uuid.UUID, rest string) string {
	return "/api/app/contacts/" + id.String() + rest
}

func TestListContacts(t *testing.T) {
	e := newEnv(t)
	owner, reader, outsider := e.user(), e.user(), e.user()
	org, ws := e.organization(owner)
	e.grant(org, ws, reader, "read")
	_, other := e.organization(outsider)
	e.contact(other, map[string]string{"email": "alice@other.example"})

	var ids []uuid.UUID
	for i := range 32 {
		ids = append(ids, e.contact(ws, map[string]string{"email": fmt.Sprintf("user%02d@example.com", i)}))
	}
	alice := e.contact(ws, map[string]string{"email": "Alice@Example.com", "firstName": "Alice"})
	e.setNumber(ws, alice, "age", 42)

	var page []Contact
	e.ok(e.do(reader, http.MethodGet, wsPath(ws, "/contacts"), nil), http.StatusOK, &page)
	if len(page) != ItemsPerPage || page[0].ID != alice || page[0].Attributes["age"] != "42" || page[0].Attributes["firstName"] != "Alice" {
		t.Fatalf("first page: %d contacts, first %+v", len(page), page[0])
	}
	e.ok(e.do(reader, http.MethodGet, wsPath(ws, "/contacts?offset=30"), nil), http.StatusOK, &page)
	if len(page) != 3 || page[2].ID != ids[0] {
		t.Fatalf("second page: %+v", page)
	}

	e.ok(e.do(reader, http.MethodGet, wsPath(ws, "/contacts?search=alice"), nil), http.StatusOK, &page)
	if len(page) != 1 || page[0].ID != alice {
		t.Fatalf("search is case-insensitive and stays in the workspace: %+v", page)
	}
	e.ok(e.do(reader, http.MethodGet, wsPath(ws, "/contacts?search=%20%2042%20"), nil), http.StatusOK, &page)
	if len(page) != 1 || page[0].ID != alice {
		t.Fatalf("search matches typed values as text, trimmed: %+v", page)
	}
	e.ok(e.do(reader, http.MethodGet, wsPath(ws, "/contacts?search="+ids[5].String()), nil), http.StatusOK, &page)
	if len(page) != 1 || page[0].ID != ids[5] {
		t.Fatalf("search by exact id: %+v", page)
	}
	e.ok(e.do(reader, http.MethodGet, wsPath(ws, "/contacts?search="+ids[5].String()[:8]), nil), http.StatusOK, &page)
	if len(page) != 0 {
		t.Fatalf("ids match exactly, not as substrings: %+v", page)
	}
	e.ok(e.do(reader, http.MethodGet, wsPath(ws, "/contacts?search=%20%20"), nil), http.StatusOK, &page)
	if len(page) != ItemsPerPage {
		t.Fatalf("whitespace is no search: %d", len(page))
	}
	e.ok(e.do(reader, http.MethodGet, wsPath(ws, "/contacts?search=user%5F0"), nil), http.StatusOK, &page)
	if len(page) != 0 {
		t.Fatalf("LIKE wildcards in a search are literal: %+v", page)
	}

	e.fails(e.do(owner, http.MethodGet, wsPath(ws, "/contacts?offset=-1"), nil), http.StatusBadRequest, "validation_error", "")
	e.fails(e.do(outsider, http.MethodGet, wsPath(ws, "/contacts"), nil), http.StatusForbidden, "forbidden", "")
}

func TestContactDetail(t *testing.T) {
	e := newEnv(t)
	owner, outsider := e.user(), e.user()
	_, ws := e.organization(owner)
	_, other := e.organization(outsider)
	signup := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	c := e.contact(ws, map[string]string{"email": "a@example.com", "plan": "pro"})
	e.setDate(ws, c, "signup", signup)
	foreign := e.contact(other, map[string]string{"email": "b@example.com"})

	var d Detail
	e.ok(e.do(owner, http.MethodGet, wsPath(ws, "/contacts/"+c.String()), nil), http.StatusOK, &d)
	if d.Contact.ID != c || d.Contact.Attributes["signup"] != "2024-06-15T10:30:00.000Z" || len(d.Attributes) != 3 {
		t.Fatalf("detail %+v", d)
	}
	byKey := map[string]AttributeDetail{}
	for _, a := range d.Attributes {
		byKey[a.Key] = a
	}
	if byKey["email"].Type != KeyTypeDefault || byKey["signup"].DataType != TypeDate || byKey["plan"].Type != KeyTypeCustom || *byKey["plan"].Name != "Plan" {
		t.Fatalf("attribute details %+v", d.Attributes)
	}
	// A contact of another workspace, reached through a workspace the caller may read, does not exist.
	e.fails(e.do(owner, http.MethodGet, wsPath(ws, "/contacts/"+foreign.String()), nil), http.StatusNotFound, "not_found", "Contact not found")
	e.fails(e.do(owner, http.MethodGet, wsPath(ws, "/contacts/"+uuid.NewString()), nil), http.StatusNotFound, "not_found", "Contact not found")
	e.fails(e.do(owner, http.MethodGet, wsPath(other, "/contacts/"+foreign.String()), nil), http.StatusForbidden, "forbidden", "")
}

func TestDeleteContact(t *testing.T) {
	e := newEnv(t)
	owner, reader := e.user(), e.user()
	org, ws := e.organization(owner)
	e.grant(org, ws, reader, "read")
	c := e.contact(ws, map[string]string{"email": "a@example.com"})

	e.fails(e.do(reader, http.MethodDelete, contactPath(c, ""), nil), http.StatusForbidden, "forbidden", "")
	var old Contact
	e.ok(e.do(owner, http.MethodDelete, contactPath(c, ""), nil), http.StatusOK, &old)
	if old.ID != c || old.Attributes["email"] != "a@example.com" {
		t.Fatalf("deleted %+v", old)
	}
	if e.count(`SELECT count(*) FROM contact_attributes WHERE contact_id = ?`, c) != 0 {
		t.Fatal("values outlived their contact")
	}
	e.fails(e.do(owner, http.MethodDelete, contactPath(c, ""), nil), http.StatusNotFound, "not_found", "contact with ID "+c.String()+" not found")
	if !strings.Contains(e.logs.String(), `"action":"deleted"`) {
		t.Fatal("no audit event")
	}
}

// updateAttrs submits the contact editor's form; the body keeps the key order of the literal.
func (e *env) updateAttrs(user, contact uuid.UUID, body string) UpdateResult {
	e.t.Helper()
	var res UpdateResult
	e.ok(e.request(&user, http.MethodPut, contactPath(contact, "/attributes"), strings.NewReader(`{"attributes":`+body+`}`), "application/json", nil), http.StatusOK, &res)
	return res
}

func codes(messages []Message) string {
	var out []string
	for _, m := range messages {
		out = append(out, m.Code)
	}
	return strings.Join(out, ",")
}

func TestUpdateAttributes(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	e.key(ws, "age", TypeNumber)
	e.key(ws, "renewal", TypeDate)
	c := e.contact(ws, map[string]string{"email": "a@example.com", "userId": "u1", "firstName": "Ada", "plan": "pro", "language": "en"})

	t.Run("updates, types and deletes what the form left out", func(t *testing.T) {
		res := e.updateAttrs(owner, c, `{"email":"a@example.com","userId":"u1","age":42,"renewal":"2025-01-15","vip":true}`)
		got := e.values(c)
		want := map[string]string{"email": "a@example.com", "userId": "u1", "firstName": "Ada", "age": "42", "renewal": "2025-01-15T00:00:00.000Z", "vip": "true"}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("values %v, want %v (plan and language are deleted, firstName is protected)", got, want)
		}
		if codes(res.Messages) != "new_attribute_created" || res.Messages[0].Params["key"] != "vip" || res.Messages[0].Params["dataType"] != TypeString {
			t.Fatalf("messages %+v", res.Messages)
		}
		if len(res.UpdatedAttributeKeys) != 1 || res.UpdatedAttributeKeys[0].Key != "vip" || *res.UpdatedAttributeKeys[0].Name != "Vip" || res.UpdatedAttributeKeys[0].Type != KeyTypeCustom {
			t.Fatalf("new keys %+v", res.UpdatedAttributeKeys)
		}
		if fmt.Sprint(res.UpdatedAttributes) != fmt.Sprint(want) {
			t.Fatalf("updated attributes %v", res.UpdatedAttributes)
		}
	})

	t.Run("strict types for existing keys skip only the bad value", func(t *testing.T) {
		res := e.updateAttrs(owner, c, `{"email":"a@example.com","age":"43","renewal":"15-01-2025","firstName":"Grace"}`)
		if codes(res.Messages) != "attribute_type_validation_error,attribute_type_validation_error" {
			t.Fatalf("messages %+v", res.Messages)
		}
		if res.Messages[0].Params["key"] != "age" || res.Messages[0].Params["dataType"] != TypeNumber || !strings.HasPrefix(res.Messages[0].Params["error"], "Attribute 'age' expects a number") {
			t.Fatalf("type message %+v", res.Messages[0])
		}
		got := e.values(c)
		if got["age"] != "42" || got["firstName"] != "Grace" {
			t.Fatalf("values %v", got)
		}
	})

	t.Run("new SDK-typed keys", func(t *testing.T) {
		e.updateAttrs(owner, c, `{"email":"a@example.com","score":7.5,"seen":"2024-01-15T10:30:00.000Z","code":"123"}`)
		var keys []Key
		e.ok(e.do(owner, http.MethodGet, wsPath(ws, "/contact-attribute-keys"), nil), http.StatusOK, &keys)
		types := map[string]string{}
		for _, k := range keys {
			types[k.Key] = k.DataType
		}
		if types["score"] != TypeNumber || types["seen"] != TypeDate || types["code"] != TypeString {
			t.Fatalf("types %v", types)
		}
	})

	t.Run("unsafe and reserved new keys are refused", func(t *testing.T) {
		res := e.updateAttrs(owner, c, `{"email":"a@example.com","Bad Key":"x","first_name":"y","ok_key":"z"}`)
		if codes(res.Errors) != "invalid_attribute_keys,reserved_attribute_keys" || res.Errors[0].Params["keys"] != "Bad Key" {
			t.Fatalf("errors %+v", res.Errors)
		}
		if !strings.Contains(res.Errors[1].Params["issue"], "Reserved attribute key(s): first_name.") {
			t.Fatalf("reserved issue %+v", res.Errors[1])
		}
		if got := e.values(c); got["ok_key"] != "z" {
			t.Fatalf("a valid new key next to refused ones is still created: %v", got)
		}
	})

	t.Run("an invalid email fails the update", func(t *testing.T) {
		rec := e.request(&owner, http.MethodPut, contactPath(c, "/attributes"), strings.NewReader(`{"attributes":{"email":"not an email"}}`), "application/json", nil)
		e.fails(rec, http.StatusBadRequest, "validation_error", "Validation failed: Invalid email")
	})

	t.Run("values must be strings, numbers or booleans", func(t *testing.T) {
		rec := e.request(&owner, http.MethodPut, contactPath(c, "/attributes"), strings.NewReader(`{"attributes":{"email":null}}`), "application/json", nil)
		e.fails(rec, http.StatusBadRequest, "validation_error", "")
	})
}

func TestUpdateAttributesKeepsEmailAndUserIdUnique(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	e.contact(ws, map[string]string{"email": "taken@example.com", "userId": "taken"})
	c := e.contact(ws, map[string]string{"email": "mine@example.com", "userId": "mine"})

	res := e.updateAttrs(owner, c, `{"email":"taken@example.com","userId":"taken","firstName":"Ada"}`)
	if codes(res.Messages) != "email_already_exists,userid_already_exists" {
		t.Fatalf("messages %+v", res.Messages)
	}
	if got := e.values(c); got["email"] != "mine@example.com" || got["userId"] != "mine" || got["firstName"] != "Ada" {
		t.Fatalf("values %v", got)
	}

	// Clearing userId is allowed while an email remains.
	e.updateAttrs(owner, c, `{"email":"mine@example.com","userId":""}`)
	if got := e.values(c); got["userId"] != "" || got["email"] != "mine@example.com" {
		t.Fatalf("values %v", got)
	}

	// Clearing both keeps the current values.
	res = e.updateAttrs(owner, c, `{"email":"","userId":""}`)
	if codes(res.Messages) != "email_or_userid_required" {
		t.Fatalf("messages %+v", res.Messages)
	}
	if got := e.values(c); got["email"] != "mine@example.com" {
		t.Fatalf("values %v", got)
	}
}

func TestUpdateAttributesAttributeKeyLimit(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	for i := range MaxAttributeKeys - 5 - 1 {
		e.key(ws, fmt.Sprintf("k%03d", i), TypeString)
	}
	c := e.contact(ws, map[string]string{"email": "a@example.com"})

	res := e.updateAttrs(owner, c, `{"email":"a@example.com","one":"1","two":"2"}`)
	if codes(res.Messages) != "attribute_limit_exceeded" || res.Messages[0].Params["count"] != "2" || res.Messages[0].Params["limit"] != "150" {
		t.Fatalf("messages %+v", res.Messages)
	}
	if got := e.values(c); got["one"] != "" {
		t.Fatalf("no new key is created past the limit: %v", got)
	}
	res = e.updateAttrs(owner, c, `{"email":"a@example.com","one":"1"}`)
	if codes(res.Messages) != "new_attribute_created" {
		t.Fatalf("one more key fits: %+v", res.Messages)
	}
}
