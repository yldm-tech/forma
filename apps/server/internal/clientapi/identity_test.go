package clientapi

import (
	"strings"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/sdkidentity"
)

// errorsOf is an identify answer's errors.
func errorsOf(t *testing.T, r result) []string {
	t.Helper()
	var out []string
	list, _ := r.data(t)["errors"].([]any)
	for _, v := range list {
		out = append(out, v.(string))
	}
	return out
}

// Without identity verification the SDK cannot write email: anyone holding the public workspace id could otherwise point another contact's follow-ups at their own address. The rest of the attributes are written, and the refusal is reported in errors, which the SDK always logs.
func TestIdentifyWithoutVerificationSkipsEmail(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	r := e.identify(t, ws, map[string]any{"userId": "u1", "attributes": map[string]any{"email": "attacker@evil.example", "plan": "enterprise"}})
	d := stateData(t, r)
	contactID := e.contactOf(t, ws, d)
	if e.attribute(contactID, "email") != "" || e.attribute(contactID, "plan") != "enterprise" {
		t.Fatalf("email %q, plan %q", e.attribute(contactID, "email"), e.attribute(contactID, "plan"))
	}
	if errs := errorsOf(t, r); len(errs) != 1 || errs[0] != msgEmailNotVerified {
		t.Fatalf("errors %v", errs)
	}
	// A userHash sent to a workspace without a secret is reported, not trusted.
	r = e.identify(t, ws, map[string]any{"userId": "u1", "userHash": "00", "attributes": map[string]any{"email": "user@example.com"}})
	if errs := errorsOf(t, r); len(errs) != 2 || errs[0] != msgUserHashNoSecret || errs[1] != msgEmailNotVerified {
		t.Fatalf("errors %v", errs)
	}
	e.identify(t, ws, map[string]any{"userId": "u1", "userHash": 5}).expectError(t, 400, "bad_request", msgUserHashNotString)
}

// With a secret, a verified identify writes email; one whose userHash does not verify is identified anyway, unverified, and says so.
func TestIdentifyVerifiedWritesEmail(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	e.identitySettings(ws, false)
	hash := sdkidentity.UserHash(testIdentitySecret, "u1")

	r := e.identify(t, ws, map[string]any{"userId": "u1", "userHash": strings.ToUpper(hash), "attributes": map[string]any{"email": "user@example.com"}})
	d := stateData(t, r)
	if e.attribute(e.contactOf(t, ws, d), "email") != "user@example.com" || len(errorsOf(t, r)) != 0 {
		t.Fatalf("a verified identify: email %q, errors %v", e.attribute(e.contactOf(t, ws, d), "email"), errorsOf(t, r))
	}
	r = e.identify(t, ws, map[string]any{"userId": "u1", "userHash": sdkidentity.UserHash(testIdentitySecret, "u2"), "attributes": map[string]any{"email": "other@example.com"}})
	if e.attribute(e.contactOf(t, ws, stateData(t, r)), "email") != "user@example.com" {
		t.Fatal("an identify with another user's hash wrote the email")
	}
	if errs := errorsOf(t, r); len(errs) != 2 || errs[0] != msgUserHashMismatch || errs[1] != msgEmailNotVerified {
		t.Fatalf("errors %v", errs)
	}
}

// Protected attributes are never written through the client API, verified or not.
func TestIdentifySkipsProtectedAttributes(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	e.exec(`INSERT INTO contact_attribute_keys (workspace_id, key, name, type, data_type) VALUES (?, 'plan', 'Plan', 'custom', 'string'), (?, 'tier', 'Tier', 'custom', 'string')`, ws, ws)
	e.identitySettings(ws, false, "plan", "tier")
	r := e.identify(t, ws, map[string]any{"userId": "u1", "userHash": sdkidentity.UserHash(testIdentitySecret, "u1"),
		"attributes": map[string]any{"plan": "enterprise", "tier": "gold", "firstName": "Ada", "email": "ada@example.com"}})
	contactID := e.contactOf(t, ws, stateData(t, r))
	if e.attribute(contactID, "plan") != "" || e.attribute(contactID, "tier") != "" {
		t.Fatal("a protected attribute was written")
	}
	if e.attribute(contactID, "firstName") != "Ada" || e.attribute(contactID, "email") != "ada@example.com" {
		t.Fatal("the other attributes were not written")
	}
	if errs := errorsOf(t, r); len(errs) != 1 || errs[0] != "Skipped protected attribute(s): plan, tier. Protected attributes are set only through the Management API, a CSV import or the contact page, never by the SDK." {
		t.Fatalf("errors %v", errs)
	}
}

// A workspace that requires verified identities refuses an identify without a valid userHash before it finds or creates the contact.
func TestIdentifyRequiresVerification(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	e.identitySettings(ws, true)
	for _, body := range []map[string]any{
		{"userId": "u1"},
		{"userId": "u1", "userHash": ""},
		{"userId": "u1", "userHash": sdkidentity.UserHash(testIdentitySecret, "u2")},
		{"userId": "u1", "userHash": sdkidentity.UserHash("another-secret", "u1")},
	} {
		e.identify(t, ws, body).expectError(t, 403, "forbidden", msgIdentityNotVerified)
	}
	if n := e.count(`SELECT count(*) FROM contacts WHERE workspace_id = ?`, ws); n != 0 {
		t.Fatalf("a refused identify created %d contacts", n)
	}
	d := stateData(t, e.identify(t, ws, map[string]any{"userId": "u1", "userHash": sdkidentity.UserHash(testIdentitySecret, "u1")}))
	if d["userId"] != "u1" {
		t.Fatalf("a verified identify: %v", d)
	}
}

// Under required verification the v1 routes, which name the respondent by userId, attach it only with a userHash that verifies it. Without one the display or response is still recorded, without a contact, so no answer is lost and no contact is created.
func TestV1RoutesRequireVerifiedUserID(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	e.identitySettings(ws, true)
	sv := e.survey(ws, "type", "app")

	d := e.post(t, "/api/v1/client/"+ws.String()+"/displays", map[string]any{"surveyId": sv.String(), "userId": "u1"}).data(t)
	if d["contactId"] != nil {
		t.Fatalf("an unverified userId was attached: %v", d)
	}
	r := e.post(t, respond(ws, "v1"), map[string]any{"surveyId": sv.String(), "finished": false, "data": map[string]any{}, "userId": "u1", "userHash": "bad"})
	var contact *string
	_ = e.db.Raw(`SELECT contact_id::text FROM responses WHERE id = ?`, r.data(t)["id"]).Row().Scan(&contact)
	if contact != nil {
		t.Fatal("a response with an unverified userId was attached to a contact")
	}
	if n := e.count(`SELECT count(*) FROM contacts WHERE workspace_id = ?`, ws); n != 0 {
		t.Fatalf("an unverified userId created %d contacts", n)
	}

	hash := sdkidentity.UserHash(testIdentitySecret, "u1")
	d = e.post(t, "/api/v1/client/"+ws.String()+"/displays", map[string]any{"surveyId": sv.String(), "userId": "u1", "userHash": hash}).data(t)
	created := e.contactOf(t, ws, d)
	r = e.post(t, respond(ws, "v1"), map[string]any{"surveyId": sv.String(), "finished": false, "data": map[string]any{}, "userId": "u1", "userHash": hash, "displayId": d["id"]})
	_ = e.db.Raw(`SELECT contact_id::text FROM responses WHERE id = ?`, r.data(t)["id"]).Row().Scan(&contact)
	if contact == nil || *contact != created.String() {
		t.Fatalf("a verified response's contact %v, want %s", contact, created)
	}
}
