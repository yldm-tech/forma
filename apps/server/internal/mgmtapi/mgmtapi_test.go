package mgmtapi

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/legacyid"
)

// auditEvent is the part of an audit record the tests check.
type auditEvent struct {
	Actor          struct{ ID, Type string }
	Target         struct{ ID, Type string }
	Action         string
	Status         string
	OrganizationID string `json:"organizationId"`
	EventID        string `json:"eventId"`
}

func (e *env) audits() []auditEvent {
	e.t.Helper()
	var out []auditEvent
	for line := range strings.SplitSeq(e.logs.String(), "\n") {
		if !strings.Contains(line, `"msg":"audit"`) {
			continue
		}
		var ev auditEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, ev)
	}
	return out
}

func expectStatus(t *testing.T, name string, res result, status int) map[string]any {
	t.Helper()
	if res.Status != status {
		t.Fatalf("%s: %d %s, want %d", name, res.Status, res.Body, status)
	}
	if len(res.Body) == 0 {
		return nil
	}
	return res.json(t)
}

// forma2 audited a v1 mutation once the handler answered: the API key as the actor in its organization, a failure with the request id; a request refused before the handler (no key, rate limited) left no event.
func TestV1AuditsMutations(t *testing.T) {
	e := newEnv(t)
	s := e.seedRecording()
	ws := s.bound["<id:workspace>"]
	create := map[string]any{"workspaceId": ws, "name": "Checkout", "type": "code", "key": "checkout"}

	data := expectStatus(t, "create", e.do("POST", "/api/v1/management/action-classes", create, "x-api-key", s.bound["<api-key:manage>"]), 200)["data"].(map[string]any)
	expectStatus(t, "refused", e.do("POST", "/api/v1/management/action-classes", create, "x-api-key", s.bound["<api-key:read>"]), 401)
	expectStatus(t, "no key", e.do("POST", "/api/v1/management/action-classes", create), 401)
	s.fill("api:v1", s.keys["manage"], 100)
	expectStatus(t, "rate limited", e.do("POST", "/api/v1/management/action-classes", create, "x-api-key", s.bound["<api-key:manage>"]), 429)

	events := e.audits()
	if len(events) != 2 {
		t.Fatalf("audit events = %+v", events)
	}
	ok, refused := events[0], events[1]
	if ok.Action != "created" || ok.Target.Type != "actionClass" || ok.Target.ID != data["id"] || ok.Actor.Type != "api" || ok.Actor.ID != s.keys["manage"].String() ||
		ok.OrganizationID != s.bound["<id:organization>"] || ok.Status != "success" || ok.EventID != "" {
		t.Errorf("success event = %+v", ok)
	}
	if refused.Status != "failure" || refused.Actor.ID != s.keys["read"].String() || refused.EventID == "" {
		t.Errorf("failure event = %+v", refused)
	}
}

// The editor uploads public files (logos, backgrounds) through POST /api/v1/management/storage with its session. A key is used alone when the request carries one; a session must name the workspace by id, may write it, and, unlike forma2, passes the same CSRF check as every /api/app mutation.
func TestV1StorageAcceptsASession(t *testing.T) {
	e := newEnv(t)
	s := e.seedRecording()
	admin := s.bound["<id:user:admin>"]
	upload := map[string]any{"workspaceId": s.bound["<id:workspace>"], "fileName": "logo.png", "fileType": "image/png"}
	browser := []string{testUserHeader, admin, "Origin", webAppURL, "X-Requested-With", "XMLHttpRequest"}

	data := expectStatus(t, "session", e.do("POST", "/api/v1/management/storage", upload, browser...), 200)["data"].(map[string]any)
	if !strings.HasPrefix(data["fileUrl"].(string), "/storage/"+s.bound["<id:workspace>"]+"/public/logo--fid--") {
		t.Errorf("upload = %v", data)
	}
	body := expectStatus(t, "another site", e.do("POST", "/api/v1/management/storage", upload, testUserHeader, admin, "Origin", "https://evil.example", "X-Requested-With", "x"), 403)
	if body["code"] != "forbidden" {
		t.Errorf("cross-site = %v", body)
	}
	expectStatus(t, "no header", e.do("POST", "/api/v1/management/storage", upload, testUserHeader, admin, "Origin", webAppURL), 403)
	legacy := map[string]any{"environmentId": "clegacyenvaaaaaaaaaaaaaa1", "fileName": "logo.png", "fileType": "image/png"}
	if body := expectStatus(t, "legacy id", e.do("POST", "/api/v1/management/storage", legacy, browser...), 400); body["message"] != "workspaceId must be provided" {
		t.Errorf("a session names the workspace by id: %v", body)
	}
	if body := expectStatus(t, "invalid key", e.do("POST", "/api/v1/management/storage", upload, append(browser, "x-api-key", "fbk_nothing")...), 401); body["code"] != "not_authenticated" {
		t.Errorf("an invalid key never falls back to the session: %v", body)
	}
	// forma2 read `workspaceId ?? environmentId`: an empty workspaceId is not absent, so the legacy id is not consulted.
	empty := map[string]any{"workspaceId": "", "environmentId": "clegacyenvaaaaaaaaaaaaaa1", "fileName": "logo.png", "fileType": "image/png"}
	if body := expectStatus(t, "empty workspaceId", e.do("POST", "/api/v1/management/storage", empty, "x-api-key", s.bound["<api-key:manage>"]), 400); body["message"] != "workspaceId must be provided" {
		t.Errorf("empty workspaceId = %v", body)
	}

	outsider := e.user("Outsider", "outsider@example.com")
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'member', true)`, outsider, s.bound["<id:organization>"])
	e.deliver()
	if body := expectStatus(t, "no write access", e.do("POST", "/api/v1/management/storage", upload, testUserHeader, outsider.String(), "Origin", webAppURL, "X-Requested-With", "x"), 401); body["code"] != "unauthorized" {
		t.Errorf("a member without access = %v", body)
	}
}

// Without x-api-key, GET /api/v1/management/me is the signed-in user, unwrapped; Authorization is never read there.
// With GATEWAY_RATE_LIMITING=1 forma2 left an API key's requests on the routes its gateway policy covers to the gateway, so a spent api:v1 window does not refuse them here. The webhook collection is outside the policy's /api/v1/webhooks/ prefix and is still charged. The gateway counts a key only by its x-api-key header, so one sent as a Bearer token is charged here too.
func TestV1GatewayRateLimiting(t *testing.T) {
	e := newEnv(t, func(cfg *config.Config) { cfg.GatewayRateLimiting = true })
	s := e.seedRecording()
	key := s.bound["<api-key:manage>"]
	s.fill("api:v1", s.keys["manage"], 100)
	expectStatus(t, "management route", e.do("GET", "/api/v1/management/action-classes", nil, "x-api-key", key), 200)
	if res := e.do("GET", "/api/v1/management/me", nil, "x-api-key", key); res.Status == 429 {
		t.Fatalf("me: %d %s, want it left to the gateway", res.Status, res.Body)
	}
	expectStatus(t, "webhook collection", e.do("GET", "/api/v1/webhooks", nil, "x-api-key", key), 429)
	expectStatus(t, "management route, Bearer key", e.do("GET", "/api/v1/management/action-classes", nil, "Authorization", "Bearer "+key), 429)
	expectStatus(t, "webhook route, Bearer key", e.do("GET", "/api/v1/webhooks/"+uuid.NewString(), nil, "Authorization", "Bearer "+key), 429)
}

func TestV1MeSession(t *testing.T) {
	e := newEnv(t)
	s := e.seedRecording()
	res := e.do("GET", "/api/v1/management/me", nil, testUserHeader, s.bound["<id:user:admin>"])
	body := expectStatus(t, "session", res, 200)
	var keys []string
	for k := range body {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	want := []string{"createdAt", "email", "emailVerified", "id", "identityProvider", "isActive", "lastLoginAt", "locale", "name", "notificationSettings", "twoFactorEnabled", "updatedAt"}
	if !slices.Equal(keys, want) || body["email"] != "admin@example.com" || body["identityProvider"] != "email" || res.Header.Get("Cache-Control") != "" {
		t.Errorf("me = %v (%v)", body, res.Header)
	}
	expectStatus(t, "no session", e.do("GET", "/api/v1/management/me", nil, "Authorization", "Bearer "+s.bound["<api-key:manage>"]), 401)
}

// A key manages users and teams with its creator's role: a manager's key adds members but never owners and never touches an owner, and nobody demotes the last active owner.
func TestV2UsersFollowTheCreatorsRole(t *testing.T) {
	e := newEnv(t)
	s := e.seedRecording()
	org := s.bound["<id:organization>"]
	manager := e.user("Manager", "manager@example.com")
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'manager', true)`, manager, org)
	managerKey, _ := e.apiKey(uuid.MustParse(org), manager, "manager", true, true)
	e.deliver()
	users := "/api/v2/organizations/" + org + "/users"

	expectStatus(t, "manager adds a member", e.do("POST", users, map[string]any{"name": "New Member", "email": "new@example.com", "role": "member"}, "x-api-key", managerKey), 201)
	if body := expectStatus(t, "manager adds an owner", e.do("POST", users, map[string]any{"name": "New Owner", "email": "owner2@example.com", "role": "owner"}, "x-api-key", managerKey), 403); body["error"].(map[string]any)["details"] != nil {
		t.Errorf("a 403 carries no details: %v", body)
	}
	expectStatus(t, "manager renames the owner", e.do("PATCH", users, map[string]any{"email": "admin@example.com", "name": "Renamed"}, "x-api-key", managerKey), 403)
	body := expectStatus(t, "last owner", e.do("PATCH", users, map[string]any{"email": "admin@example.com", "role": "manager"}, "x-api-key", s.bound["<api-key:manage>"]), 409)
	if d := body["error"].(map[string]any)["details"].([]any)[0].(map[string]any); d["field"] != "role" || d["issue"] != "Cannot remove the last owner of the organization" {
		t.Errorf("last owner = %v", body)
	}
	expectStatus(t, "manager creates a team", e.do("POST", "/api/v2/organizations/"+org+"/teams", map[string]any{"name": "Team"}, "x-api-key", managerKey), 201)
	e.exec(`UPDATE memberships SET role = 'member' WHERE user_id = ?`, manager)
	e.deliver()
	expectStatus(t, "member's key creates a team", e.do("POST", "/api/v2/organizations/"+org+"/teams", map[string]any{"name": "Other team"}, "x-api-key", managerKey), 403)
}

// A bulk upload creates the keys it introduces, updates contacts it matches by email, and skips a contact whose userId another contact holds.
func TestV2BulkContacts(t *testing.T) {
	e := newEnv(t)
	s := e.seedRecording()
	ws := s.bound["<id:workspace>"]
	key := s.bound["<api-key:manage>"]
	attr := func(key, value string) map[string]any {
		return map[string]any{"attributeKey": map[string]any{"key": key, "name": strings.ToUpper(key[:1]) + key[1:]}, "value": value}
	}
	upload := func(contacts ...[]map[string]any) map[string]any {
		list := make([]any, len(contacts))
		for i, c := range contacts {
			list[i] = map[string]any{"attributes": c}
		}
		return map[string]any{"workspaceId": ws, "contacts": list}
	}
	expectStatus(t, "create", e.do("PUT", "/api/v2/management/contacts/bulk", upload(
		[]map[string]any{attr("email", "a@example.com"), attr("userId", "user-a"), attr("seats", "3")},
		[]map[string]any{attr("email", "b@example.com"), attr("region", "emea")},
	), "x-api-key", key), 200)
	expectStatus(t, "update", e.do("PUT", "/api/v2/management/contacts/bulk", upload(
		[]map[string]any{attr("email", "a@example.com"), attr("seats", "5")},
	), "x-api-key", key), 200)
	body := expectStatus(t, "conflict", e.do("PUT", "/api/v2/management/contacts/bulk", upload(
		[]map[string]any{attr("email", "c@example.com"), attr("userId", "user-a")},
	), "x-api-key", key), 207)
	if skipped := body["data"].(map[string]any)["meta"].(map[string]any)["skippedContacts"].([]any); len(skipped) != 1 {
		t.Errorf("skipped = %v", skipped)
	}
	type row struct {
		Key      string
		DataType string
		Value    string
	}
	var rows []row
	if err := e.db.Raw(`SELECT k.key, k.data_type, COALESCE(a.value_text, a.value_number::text) AS value FROM contact_attributes a JOIN contact_attribute_keys k ON k.id = a.attribute_key_id
		JOIN contacts c ON c.id = a.contact_id WHERE c.workspace_id = ? ORDER BY k.key, value`, ws).Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	want := []row{{"email", "string", "a@example.com"}, {"email", "string", "b@example.com"}, {"region", "string", "emea"}, {"seats", "number", "5"}, {"userId", "string", "user-a"}}
	if !slices.Equal(rows, want) {
		t.Errorf("stored = %+v, want %+v", rows, want)
	}
	body = expectStatus(t, "typed key", e.do("PUT", "/api/v2/management/contacts/bulk", upload(
		[]map[string]any{attr("email", "d@example.com"), attr("seats", "many")},
	), "x-api-key", key), 400)
	if d := body["error"].(map[string]any)["details"].([]any)[0].(map[string]any); d["issue"] != `Attribute "seats" is typed as "number" but received invalid values: many` {
		t.Errorf("typed key = %v", d)
	}
}

// Next.js answered OPTIONS on every route with the methods it exports.
func TestOptionsListTheRoutesMethods(t *testing.T) {
	e := newEnv(t)
	res := e.do(http.MethodOptions, "/api/v1/management/surveys/"+uuid.NewString(), nil)
	if res.Status != http.StatusNoContent || res.Header.Get("Allow") != "DELETE, GET, HEAD, OPTIONS, PUT" {
		t.Errorf("OPTIONS = %d %v", res.Status, res.Header)
	}
}

// forma2's next.config.mjs rewrote the Formbricks-era project-teams path onto workspace-teams and permanently redirected the short v1 paths to the management API; integrations still call both.
func TestForma2PathAliases(t *testing.T) {
	e := newEnv(t)
	s := e.seedRecording()
	org, key := s.bound["<id:organization>"], s.bound["<api-key:manage>"]
	canonical := e.do("GET", "/api/v2/organizations/"+org+"/workspace-teams", nil, "x-api-key", key)
	alias := e.do("GET", "/api/v2/organizations/"+org+"/project-teams", nil, "x-api-key", key)
	if canonical.Status != http.StatusOK || alias.Status != canonical.Status || !bytes.Equal(alias.Body, canonical.Body) {
		t.Errorf("project-teams = %d %s, workspace-teams = %d %s", alias.Status, alias.Body, canonical.Status, canonical.Body)
	}
	expectStatus(t, "project-teams without a key", e.do("POST", "/api/v2/organizations/"+org+"/project-teams", map[string]any{}), http.StatusUnauthorized)

	for _, tc := range []struct{ method, path, want string }{
		{"GET", "/api/v1/surveys", "/api/v1/management/surveys"},
		{"POST", "/api/v1/responses?surveyId=abc", "/api/v1/management/responses?surveyId=abc"},
		{"GET", "/api/v1/me", "/api/v1/management/me"},
	} {
		res := e.do(tc.method, tc.path, nil)
		if res.Status != http.StatusPermanentRedirect || res.Header.Get("Location") != tc.want || res.Header.Get("Refresh") != "0;url="+tc.want || string(res.Body) != tc.want {
			t.Errorf("%s %s = %d %v %q, want 308 to %s", tc.method, tc.path, res.Status, res.Header, res.Body, tc.want)
		}
	}
}

// zod's z.number() refuses Infinity and v1's .int() refuses an unsafe integer; an offset beyond int64 is an empty page, never a negative OFFSET.
func TestPaginationBounds(t *testing.T) {
	e := newEnv(t)
	s := e.seedRecording()
	key := s.bound["<api-key:manage>"]
	for _, skip := range []string{"Infinity", "-Infinity"} {
		body := expectStatus(t, "v2 skip "+skip, e.do("GET", "/api/v2/management/webhooks?skip="+skip, nil, "x-api-key", key), http.StatusUnprocessableEntity)
		if d := body["error"].(map[string]any)["details"].([]any)[0].(map[string]any); d["field"] != "skip" || d["issue"] != "Invalid input: expected number, received number" {
			t.Errorf("skip=%s details = %v", skip, d)
		}
	}
	body := expectStatus(t, "v2 skip 1e19", e.do("GET", "/api/v2/management/webhooks?skip=1e19", nil, "x-api-key", key), http.StatusOK)
	if meta := body["meta"].(map[string]any); meta["offset"] != float64(math.MaxInt64) || len(body["data"].([]any)) != 0 {
		t.Errorf("skip=1e19 = %v", body)
	}
	for _, offset := range []string{"1e19", "9007199254740992"} {
		expectStatus(t, "v1 offset "+offset, e.do("GET", "/api/v1/management/surveys?offset="+offset, nil, "x-api-key", key), http.StatusBadRequest)
	}
	expectStatus(t, "v1 largest safe offset", e.do("GET", "/api/v1/management/surveys?offset=9007199254740991", nil, "x-api-key", key), http.StatusOK)
}

func TestSaturatingInt(t *testing.T) {
	for _, tc := range []struct {
		in   float64
		want int
	}{{0, 0}, {12, 12}, {1e19, math.MaxInt64}, {math.MaxInt64, math.MaxInt64}, {9007199254740991, 9007199254740991}} {
		if got := saturatingInt(tc.in); got != tc.want {
			t.Errorf("saturatingInt(%g) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// forma2's ids keep filtering after the migration (contactId and a user's id resolve as surveyId does), and an empty filter is no filter, as forma2's `if (contactId)` and `if (params.id)` had it.
func TestV2ListFiltersReadForma2IDs(t *testing.T) {
	e := newEnv(t)
	s := e.seedRecording()
	key, org := s.bound["<api-key:manage>"], s.bound["<id:organization>"]
	sv := e.insert(`INSERT INTO surveys (workspace_id, name) VALUES (?, 'Survey') RETURNING id`, s.ws)
	contact := legacyid.ID("clcontactaaaaaaaaaaaaaaa1")
	e.exec(`INSERT INTO contacts (id, workspace_id) VALUES (?, ?)`, contact, s.ws)
	mine := e.insert(`INSERT INTO responses (survey_id, contact_id, data) VALUES (?, ?, '{}') RETURNING id`, sv, contact)
	e.insert(`INSERT INTO responses (survey_id, data) VALUES (?, '{}') RETURNING id`, sv)

	ids := func(body map[string]any) []string {
		var out []string
		for _, item := range body["data"].([]any) {
			out = append(out, item.(map[string]any)["id"].(string))
		}
		return out
	}
	for _, contactID := range []string{"clcontactaaaaaaaaaaaaaaa1", contact.String()} {
		body := expectStatus(t, "contactId "+contactID, e.do("GET", "/api/v2/management/responses?contactId="+contactID, nil, "x-api-key", key), http.StatusOK)
		if got := ids(body); len(got) != 1 || got[0] != mine.String() {
			t.Errorf("contactId=%s = %v, want [%s]", contactID, got, mine)
		}
	}
	if got := ids(expectStatus(t, "empty contactId", e.do("GET", "/api/v2/management/responses?contactId=", nil, "x-api-key", key), http.StatusOK)); len(got) != 2 {
		t.Errorf("contactId= = %v, want both responses", got)
	}

	user := legacyid.ID("cluseraaaaaaaaaaaaaaaaa1")
	e.exec(`INSERT INTO users (id, name, email, email_verified) VALUES (?, 'Migrated', 'migrated@example.com', true)`, user)
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'member', true)`, user, org)
	users := "/api/v2/organizations/" + org + "/users"
	if got := ids(expectStatus(t, "forma2 user id", e.do("GET", users+"?id=cluseraaaaaaaaaaaaaaaaa1", nil, "x-api-key", key), http.StatusOK)); len(got) != 1 || got[0] != user.String() {
		t.Errorf("id=<forma2 id> = %v, want [%s]", got, user)
	}
	if got := ids(expectStatus(t, "empty user id", e.do("GET", users+"?id=", nil, "x-api-key", key), http.StatusOK)); len(got) != 2 {
		t.Errorf("id= = %v, want both members", got)
	}
}
