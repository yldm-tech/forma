package clientapi

import (
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func (e *env) identify(t *testing.T, ws uuid.UUID, body any) result {
	t.Helper()
	e.clock.nextWindow()
	return e.do("POST", "/api/v2/client/"+ws.String()+"/user", body)
}

// stateData is the identify answer's state.data.
func stateData(t *testing.T, r result) map[string]any {
	t.Helper()
	if r.Status != 200 {
		t.Fatalf("identify %d %s", r.Status, r.Body)
	}
	return r.data(t)["state"].(map[string]any)["data"].(map[string]any)
}

func (e *env) attribute(contactID any, key string) string {
	e.t.Helper()
	var v *string
	if err := e.db.Raw(`SELECT COALESCE(ca.value_text, ca.value_number::text) FROM contact_attributes ca JOIN contact_attribute_keys k ON k.id = ca.attribute_key_id WHERE ca.contact_id = ? AND k.key = ?`, contactID, key).Row().Scan(&v); err != nil || v == nil {
		return ""
	}
	return *v
}

// update-user.test.ts "should create a new contact if not found", and an identify of a known user finds it again.
func TestIdentifyCreatesTheContactOnce(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	first := stateData(t, e.identify(t, ws, map[string]any{"userId": "user-1"}))
	again := stateData(t, e.identify(t, ws, map[string]any{"userId": "user-1"}))
	if first["contactId"] != again["contactId"] || first["userId"] != "user-1" {
		t.Fatalf("first %v, again %v", first, again)
	}
	if e.attribute(first["contactId"], "userId") != "user-1" {
		t.Fatal("the contact does not carry its userId")
	}
	if _, ok := first["language"]; ok {
		t.Fatal("a contact without a language reports one")
	}
	if first["lastDisplayAt"] != nil || len(first["displays"].([]any)) != 0 || len(first["responses"].([]any)) != 0 {
		t.Fatalf("a new contact's history %v", first)
	}
}

// "reuses the contact a concurrent identify created instead of creating a second one".
func TestConcurrentFirstIdentifiesCreateOneContact(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if r := e.do("POST", "/api/v2/client/"+ws.String()+"/user", map[string]any{"userId": "racer"}); r.Status != 200 {
				t.Errorf("identify %d %s", r.Status, r.Body)
			}
		})
	}
	wg.Wait()
	if n := e.count(`SELECT count(*) FROM contacts WHERE workspace_id = ?`, ws); n != 1 {
		t.Fatalf("%d contacts for one userId", n)
	}
}

// The language attribute: canonicalized on write and echoed in its legacy form, a configured alias kept as configured, anything else dropped with a message unless it was blank.
func TestIdentifyLanguage(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	e.language(ws, "pt-BR", ptr("Brasileiro"))

	d := stateData(t, e.identify(t, ws, map[string]any{"userId": "u1", "attributes": map[string]any{"language": "de", "plan": "pro"}}))
	if d["language"] != "de" || e.attribute(d["contactId"], "language") != "de-DE" || e.attribute(d["contactId"], "plan") != "pro" {
		t.Fatalf("legacy code: echoed %v, stored %q", d["language"], e.attribute(d["contactId"], "language"))
	}
	d = stateData(t, e.identify(t, ws, map[string]any{"userId": "u1", "attributes": map[string]any{"language": "fr-FR"}}))
	if d["language"] != "fr" || e.attribute(d["contactId"], "language") != "fr-FR" {
		t.Fatalf("canonical code: echoed %v", d["language"])
	}
	d = stateData(t, e.identify(t, ws, map[string]any{"userId": "u1", "attributes": map[string]any{"language": "BRASILEIRO"}}))
	if d["language"] != "Brasileiro" || e.attribute(d["contactId"], "language") != "Brasileiro" {
		t.Fatalf("alias: echoed %v, stored %q", d["language"], e.attribute(d["contactId"], "language"))
	}

	r := e.identify(t, ws, map[string]any{"userId": "u1", "attributes": map[string]any{"language": "klingonese", "plan": "free"}})
	d = stateData(t, r)
	msgs := r.data(t)["messages"].([]any)
	if len(msgs) != 1 || msgs[0] != "Ignored invalid language code 'klingonese'. The existing value was preserved." {
		t.Fatalf("messages %v", msgs)
	}
	if e.attribute(d["contactId"], "language") != "Brasileiro" || e.attribute(d["contactId"], "plan") != "free" {
		t.Fatal("an invalid language changed the stored one, or the other attributes were not written")
	}
	r = e.identify(t, ws, map[string]any{"userId": "u1", "attributes": map[string]any{"language": 0}})
	if msgs := r.data(t)["messages"].([]any); len(msgs) != 1 || msgs[0] != "Ignored invalid language code '0'. The existing value was preserved." {
		t.Fatalf("a falsy language: %v", msgs)
	}
	r = e.identify(t, ws, map[string]any{"userId": "u1", "attributes": map[string]any{"language": "   "}})
	if _, ok := r.data(t)["messages"]; ok {
		t.Fatalf("a blank language is dropped silently: %s", r.Body)
	}
}

// "should not update attributes if they are the same": nothing is written when every value equals the stored text.
func TestIdentifyWritesOnlyChanges(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	d := stateData(t, e.identify(t, ws, map[string]any{"userId": "u1", "attributes": map[string]any{"plan": "pro"}}))
	var before string
	_ = e.db.Raw(`SELECT updated_at::text FROM contacts WHERE id = ?`, d["contactId"]).Row().Scan(&before)
	stateData(t, e.identify(t, ws, map[string]any{"userId": "u1", "attributes": map[string]any{"plan": "pro"}}))
	var after string
	_ = e.db.Raw(`SELECT updated_at::text FROM contacts WHERE id = ?`, d["contactId"]).Row().Scan(&after)
	if before != after {
		t.Fatal("an unchanged identify wrote the contact")
	}
}

// The attribute messages: existence-revealing ones are withheld, errors are returned, new keys are reported.
func TestIdentifyMessages(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	stateData(t, e.identify(t, ws, map[string]any{"userId": "owner", "attributes": map[string]any{"email": "taken@example.com"}}))
	r := e.identify(t, ws, map[string]any{"userId": "other", "attributes": map[string]any{"email": "taken@example.com", "seats": 3, "Bad Key": "x", "vip": true}})
	d := stateData(t, r)
	if e.attribute(d["contactId"], "email") != "" {
		t.Fatal("an email another contact holds was written")
	}
	body := r.data(t)
	msgs, _ := body["messages"].([]any)
	want := []any{"Created new attribute 'seats' with type 'number'", "Created new attribute 'vip' with type 'string'"}
	if len(msgs) != len(want) || msgs[0] != want[0] || msgs[1] != want[1] {
		t.Fatalf("messages %v, want %v", msgs, want)
	}
	errs, _ := body["errors"].([]any)
	if len(errs) != 1 || errs[0] != "Skipped creating attribute(s) with invalid key(s): Bad Key. Keys must only contain lowercase letters, numbers, and underscores, and must start with a letter." {
		t.Fatalf("errors %v", errs)
	}
	if e.attribute(d["contactId"], "vip") != "true" || e.attribute(d["contactId"], "seats") != "3" {
		t.Fatal("new attributes were not stored")
	}
}

func TestIdentifyRefusals(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	e.identify(t, ws, map[string]any{"userId": 5}).expectError(t, 400, "bad_request", "userId is required and must be a string")
	e.identify(t, ws, []any{"u1"}).expectError(t, 400, "bad_request", "userId is required and must be a string")
	e.identify(t, ws, map[string]any{"userId": "u1", "attributes": map[string]any{"email": 5}}).expectError(t, 400, "bad_request", "Invalid email format")
	e.identify(t, ws, map[string]any{"userId": "u1", "attributes": map[string]any{"email": "a@"}}).expectError(t, 400, "bad_request", "Invalid email format")
	// It passes the route's check but not ZUserEmail.
	e.identify(t, ws, map[string]any{"userId": "u1", "attributes": map[string]any{"email": "a@b"}}).expectError(t, 400, "bad_request", "Validation failed: Invalid email")
	e.identify(t, ws, map[string]any{"userId": "u1", "attributes": map[string]any{"plan": nil}}).expectError(t, 400, "bad_request", "Validation failed: planInvalid input")
	// userId and id inside the attributes are ignored.
	d := stateData(t, e.identify(t, ws, map[string]any{"userId": "u2", "attributes": map[string]any{"userId": "spoofed", "id": "x"}}))
	if e.attribute(d["contactId"], "userId") != "u2" {
		t.Fatal("attributes overwrote the userId")
	}
}

// The state lists the contact's segments, its displays newest first and the surveys it responded to.
func TestIdentifyState(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	all := e.segment(ws, "Everyone", `[]`)
	d := stateData(t, e.identify(t, ws, map[string]any{"userId": "u1"}))
	contactID := d["contactId"]
	older, newer := e.survey(ws, "type", "app"), e.survey(ws, "type", "app")
	e.exec(`INSERT INTO displays (survey_id, contact_id, created_at) VALUES (?, ?, now() - interval '1 day'), (?, ?, now())`, older, contactID, newer, contactID)
	e.exec(`INSERT INTO responses (survey_id, contact_id) VALUES (?, ?)`, older, contactID)

	d = stateData(t, e.identify(t, ws, map[string]any{"userId": "u1"}))
	if segments := d["segments"].([]any); len(segments) != 1 || segments[0] != all.String() {
		t.Fatalf("segments %v", segments)
	}
	displays := d["displays"].([]any)
	if len(displays) != 2 || displays[0].(map[string]any)["surveyId"] != newer.String() || d["lastDisplayAt"] != displays[0].(map[string]any)["createdAt"] {
		t.Fatalf("displays %v, last %v", displays, d["lastDisplayAt"])
	}
	if responses := d["responses"].([]any); len(responses) != 1 || responses[0] != older.String() {
		t.Fatalf("responses %v", responses)
	}
}

// Device filters are evaluated for the device the identify comes from, read from its User-Agent: a phone for a mobile one, a desktop for anything else, tablets included. forma2 evaluated every identify as a phone.
func TestIdentifySegmentsByDevice(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	all := e.segment(ws, "Everyone", `[]`)
	phone := e.segment(ws, "Phones", `[{"id":"f1","connector":null,"resource":{"id":"r1","root":{"type":"device","deviceType":"phone"},"value":"phone","qualifier":{"operator":"equals"}}}]`)
	desktop := e.segment(ws, "Desktops", `[{"id":"f2","connector":null,"resource":{"id":"r2","root":{"type":"device","deviceType":"desktop"},"value":"desktop","qualifier":{"operator":"equals"}}}]`)
	for _, tc := range []struct {
		name, userAgent string
		want            uuid.UUID
	}{
		{"desktop Chrome", browserUA, desktop},
		{"iPhone", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1", phone},
		{"Android phone", "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Mobile Safari/537.36", phone},
		{"iPad counts as a desktop", "Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1", desktop},
		{"no User-Agent", "", desktop},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e.clock.nextWindow()
			d := stateData(t, e.do("POST", "/api/v2/client/"+ws.String()+"/user", map[string]any{"userId": "u1"}, "User-Agent", tc.userAgent))
			got := map[any]bool{}
			for _, s := range d["segments"].([]any) {
				got[s] = true
			}
			other := phone
			if tc.want == phone {
				other = desktop
			}
			if len(got) != 2 || !got[all.String()] || !got[tc.want.String()] || got[other.String()] {
				t.Fatalf("segments %v, want everyone and %s", d["segments"], tc.want)
			}
		})
	}
}

// A value of the wrong type for a typed key is not written and is reported under errors, which the SDK always logs, not under messages, which it logs only in debug mode.
func TestIdentifyTypeMismatchIsAnError(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	d := stateData(t, e.identify(t, ws, map[string]any{"userId": "u1", "attributes": map[string]any{"seats": 25}}))
	r := e.identify(t, ws, map[string]any{"userId": "u1", "attributes": map[string]any{"seats": "30"}})
	stateData(t, r)
	body := r.data(t)
	if msgs, _ := body["messages"].([]any); len(msgs) != 0 {
		t.Fatalf("messages %v", msgs)
	}
	errs, _ := body["errors"].([]any)
	if len(errs) != 1 || !strings.HasPrefix(errs[0].(string), "Attribute 'seats' expects a number") || !strings.HasSuffix(errs[0].(string), "(attribute 'seats' has dataType: number)") {
		t.Fatalf("errors %v", errs)
	}
	if got := e.attribute(d["contactId"], "seats"); got != "25" {
		t.Fatalf("seats %q, want the stored 25", got)
	}
}
