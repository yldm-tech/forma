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
	expectDetail(t, expectStatus(t, "manager adds an owner", e.do("POST", users, map[string]any{"name": "New Owner", "email": "owner2@example.com", "role": "owner"}, "x-api-key", managerKey), 403), "apiKey", "Key creator's current role cannot assign this role")
	expectDetail(t, expectStatus(t, "manager renames the owner", e.do("PATCH", users, map[string]any{"email": "admin@example.com", "name": "Renamed"}, "x-api-key", managerKey), 403), "apiKey", "Key creator's current role cannot change an owner")
	body := expectStatus(t, "last owner", e.do("PATCH", users, map[string]any{"email": "admin@example.com", "role": "manager"}, "x-api-key", s.bound["<api-key:manage>"]), 409)
	if d := body["error"].(map[string]any)["details"].([]any)[0].(map[string]any); d["field"] != "role" || d["issue"] != "Cannot remove the last owner of the organization" {
		t.Errorf("last owner = %v", body)
	}
	expectStatus(t, "manager creates a team", e.do("POST", "/api/v2/organizations/"+org+"/teams", map[string]any{"name": "Team"}, "x-api-key", managerKey), 201)
	e.exec(`UPDATE memberships SET role = 'member' WHERE user_id = ?`, manager)
	e.deliver()
	expectDetail(t, expectStatus(t, "member's key creates a team", e.do("POST", "/api/v2/organizations/"+org+"/teams", map[string]any{"name": "Other team"}, "x-api-key", managerKey), 403), "apiKey", "Key creator's current role cannot manage teams")
	expectDetail(t, expectStatus(t, "member's key adds a user", e.do("POST", users, map[string]any{"name": "Another", "email": "another@example.com", "role": "member"}, "x-api-key", managerKey), 403), "apiKey", "Key creator's current role cannot manage users")
	e.exec(`DELETE FROM memberships WHERE user_id = ?`, manager)
	e.deliver()
	expectDetail(t, expectStatus(t, "removed creator's key creates a team", e.do("POST", "/api/v2/organizations/"+org+"/teams", map[string]any{"name": "Other team"}, "x-api-key", managerKey), 403), "apiKey", "Key creator is no longer a member of the organization")
}

// expectDetail checks that an error body carries exactly the one detail given.
func expectDetail(t *testing.T, body map[string]any, field, issue string) {
	t.Helper()
	details, _ := body["error"].(map[string]any)["details"].([]any)
	if len(details) != 1 {
		t.Fatalf("details = %v, want [{%s %s}]", body, field, issue)
	}
	if d := details[0].(map[string]any); d["field"] != field || d["issue"] != issue {
		t.Errorf("detail = %v, want {%s %s}", d, field, issue)
	}
}

// A team's workspace grants decide which workspaces its members reach, so changing them takes the same creator role as every other team change: a key whose creator was demoted to member, or left, grants nothing.
func TestV2WorkspaceTeamsFollowTheCreatorsRole(t *testing.T) {
	e := newEnv(t)
	s := e.seedRecording()
	org := s.bound["<id:organization>"]
	ws := s.bound["<id:workspace>"]
	manager := e.user("Manager", "manager@example.com")
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'manager', true)`, manager, org)
	managerKey, _ := e.apiKey(uuid.MustParse(org), manager, "manager", true, true)
	team := e.insert(`INSERT INTO teams (organization_id, name) VALUES (?, 'Team') RETURNING id`, org)
	e.deliver()
	grants := "/api/v2/organizations/" + org + "/workspace-teams"
	grant := map[string]any{"teamId": team.String(), "workspaceId": ws, "permission": "read"}

	expectStatus(t, "manager grants", e.do("POST", grants, grant, "x-api-key", managerKey), 200)
	e.exec(`UPDATE memberships SET role = 'member' WHERE user_id = ?`, manager)
	e.deliver()
	manage := map[string]any{"teamId": team.String(), "workspaceId": ws, "permission": "manage"}
	expectDetail(t, expectStatus(t, "member's key raises the grant", e.do("PUT", grants, manage, "x-api-key", managerKey), 403), "apiKey", "Key creator's current role cannot manage teams")
	expectDetail(t, expectStatus(t, "member's key deletes the grant", e.do("DELETE", grants+"?teamId="+team.String()+"&workspaceId="+ws, nil, "x-api-key", managerKey), 403), "apiKey", "Key creator's current role cannot manage teams")
	e.exec(`DELETE FROM workspace_teams WHERE team_id = ?`, team)
	expectDetail(t, expectStatus(t, "member's key grants", e.do("POST", grants, manage, "x-api-key", managerKey), 403), "apiKey", "Key creator's current role cannot manage teams")
	var n int64
	if err := e.db.Raw(`SELECT count(*) FROM workspace_teams WHERE team_id = ?`, team).Scan(&n).Error; err != nil || n != 0 {
		t.Fatalf("grants after the refused requests = %d (%v)", n, err)
	}
	expectStatus(t, "owner's key grants", e.do("POST", grants, manage, "x-api-key", s.bound["<api-key:manage>"]), 200)
}

// A user's name lives on their account, which every organization they belong to shares: one organization's key renames only a user who belongs to it alone. isActive is the membership's (D-AO01): an organization's key suspends and restores its own member's access there, and the user keeps their account and their other organizations. Their role and teams in the organization stay its own to change.
func TestV2UsersSuspendOnlyTheirMembership(t *testing.T) {
	e := newEnv(t)
	s := e.seedRecording()
	org, key := s.bound["<id:organization>"], s.bound["<api-key:manage>"]
	users := "/api/v2/organizations/" + org + "/users"
	other := e.insert(`INSERT INTO organizations (name) VALUES ('Their own organization') RETURNING id`)
	shared := e.user("Shared User", "shared@example.com")
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'owner', true)`, shared, other)
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'member', true)`, shared, org)
	e.deliver()

	type state struct {
		Name           string
		IsActive       bool
		Role           string
		SuspendedHere  bool
		SuspendedOther bool
	}
	read := func() state {
		t.Helper()
		var st state
		if err := e.db.Raw(`SELECT u.name, u.is_active, m.role, m.suspended_at IS NOT NULL AS suspended_here,
			(SELECT o.suspended_at IS NOT NULL FROM memberships o WHERE o.user_id = u.id AND o.organization_id = ?) AS suspended_other
			FROM users u JOIN memberships m ON m.user_id = u.id AND m.organization_id = ? WHERE u.id = ?`, other, org, shared).Scan(&st).Error; err != nil {
			t.Fatal(err)
		}
		return st
	}

	issue := "The user also belongs to another organization, so this organization cannot change it"
	expectDetail(t, expectStatus(t, "rename", e.do("PATCH", users, map[string]any{"email": "shared@example.com", "name": "Renamed by another org"}, "x-api-key", key), 422), "name", issue)
	expectDetail(t, expectStatus(t, "rename and suspend", e.do("PATCH", users, map[string]any{"email": "shared@example.com", "name": "Renamed", "isActive": false, "role": "manager"}, "x-api-key", key), 422), "name", issue)
	if st := read(); st != (state{Name: "Shared User", IsActive: true, Role: "member"}) {
		t.Fatalf("after the refused changes: %+v", st)
	}

	data := expectStatus(t, "suspend", e.do("PATCH", users, map[string]any{"email": "shared@example.com", "isActive": false}, "x-api-key", key), 200)["data"].(map[string]any)
	if data["isActive"] != false {
		t.Errorf("suspend = %v", data)
	}
	if st := read(); st != (state{Name: "Shared User", IsActive: true, Role: "member", SuspendedHere: true}) {
		t.Fatalf("after suspending: %+v; only this organization's membership is suspended", st)
	}
	list := expectStatus(t, "list", e.do("GET", users+"?email=shared", nil, "x-api-key", key), 200)["data"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["isActive"] != false {
		t.Errorf("list = %v", list)
	}

	data = expectStatus(t, "unchanged name, restored and promoted", e.do("PATCH", users, map[string]any{"email": "shared@example.com", "name": "Shared User", "isActive": true, "role": "manager"}, "x-api-key", key), 200)["data"].(map[string]any)
	if data["role"] != "manager" || data["name"] != "Shared User" || data["isActive"] != true {
		t.Errorf("restore = %v", data)
	}
	if st := read(); st != (state{Name: "Shared User", IsActive: true, Role: "manager"}) {
		t.Fatalf("after restoring: %+v", st)
	}

	e.exec(`DELETE FROM memberships WHERE user_id = ? AND organization_id = ?`, shared, other)
	data = expectStatus(t, "only here", e.do("PATCH", users, map[string]any{"email": "shared@example.com", "name": "Renamed", "isActive": false}, "x-api-key", key), 200)["data"].(map[string]any)
	if data["name"] != "Renamed" || data["isActive"] != false {
		t.Errorf("change of a user of this organization only = %v", data)
	}

	// A member whose account the instance deactivated reads as inactive, yet false still suspends their membership, so turning the account back on does not give them this organization again; true only lifts the suspension.
	off := e.user("Deactivated Account", "off@example.com")
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'member', true)`, off, org)
	e.exec(`UPDATE users SET is_active = false WHERE id = ?`, off)
	suspendedAt := func() bool {
		t.Helper()
		var v bool
		if err := e.db.Raw(`SELECT suspended_at IS NOT NULL FROM memberships WHERE user_id = ? AND organization_id = ?`, off, org).Scan(&v).Error; err != nil {
			t.Fatal(err)
		}
		return v
	}
	data = expectStatus(t, "suspend a deactivated account", e.do("PATCH", users, map[string]any{"email": "off@example.com", "isActive": false}, "x-api-key", key), 200)["data"].(map[string]any)
	if data["isActive"] != false || !suspendedAt() {
		t.Fatalf("suspend a deactivated account = %v, suspended %v", data, suspendedAt())
	}
	data = expectStatus(t, "restore a deactivated account", e.do("PATCH", users, map[string]any{"email": "off@example.com", "isActive": true}, "x-api-key", key), 200)["data"].(map[string]any)
	if data["isActive"] != false || suspendedAt() {
		t.Fatalf("restore a deactivated account = %v, suspended %v; the membership is restored, the account stays off", data, suspendedAt())
	}
}

// Deactivating an owner is refused when it would leave the organization without an active owner, as demoting one is.
func TestV2UsersKeepAnActiveOwner(t *testing.T) {
	e := newEnv(t)
	s := e.seedRecording()
	org, key := s.bound["<id:organization>"], s.bound["<api-key:manage>"]
	users := "/api/v2/organizations/" + org + "/users"
	second := e.user("Second Owner", "owner2@example.com")
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'owner', true)`, second, org)
	e.deliver()

	expectStatus(t, "deactivate one of two owners", e.do("PATCH", users, map[string]any{"email": "owner2@example.com", "isActive": false}, "x-api-key", key), 200)
	expectDetail(t, expectStatus(t, "deactivate the last active owner", e.do("PATCH", users, map[string]any{"email": "admin@example.com", "isActive": false}, "x-api-key", key), 409), "isActive", "Cannot deactivate the last active owner of the organization")
	expectDetail(t, expectStatus(t, "demote the last active owner", e.do("PATCH", users, map[string]any{"email": "admin@example.com", "role": "manager", "isActive": false}, "x-api-key", key), 409), "role", "Cannot remove the last owner of the organization")
	var active bool
	if err := e.db.Raw(`SELECT is_active FROM users WHERE email = 'admin@example.com'`).Scan(&active).Error; err != nil || !active {
		t.Fatalf("the last active owner is_active = %v (%v)", active, err)
	}
	var suspended bool
	if err := e.db.Raw(`SELECT u.is_active AND m.suspended_at IS NOT NULL FROM users u JOIN memberships m ON m.user_id = u.id WHERE u.email = 'owner2@example.com'`).Scan(&suspended).Error; err != nil || !suspended {
		t.Fatalf("the deactivated owner's account active and membership suspended = %v (%v)", suspended, err)
	}
	expectStatus(t, "inactive owner demoted", e.do("PATCH", users, map[string]any{"email": "owner2@example.com", "role": "member"}, "x-api-key", key), 200)
}

// An email that already has an account gets the same 409 body whether its user is a member of the organization or only has an account elsewhere on the instance. This makes the text uniform only: 409 against 201 still shows that the email has an account (D-AO01).
func TestV2CreateUserDuplicateEmailIsNeutral(t *testing.T) {
	e := newEnv(t)
	s := e.seedRecording()
	org, key := s.bound["<id:organization>"], s.bound["<api-key:manage>"]
	users := "/api/v2/organizations/" + org + "/users"
	elsewhere := e.insert(`INSERT INTO organizations (name) VALUES ('Elsewhere') RETURNING id`)
	outsider := e.user("Outsider", "outsider@example.com")
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'owner', true)`, outsider, elsewhere)
	e.deliver()

	member := e.do("POST", users, map[string]any{"name": "Dup", "email": "admin@example.com", "role": "member"}, "x-api-key", key)
	outside := e.do("POST", users, map[string]any{"name": "Dup", "email": "outsider@example.com", "role": "member"}, "x-api-key", key)
	expectDetail(t, expectStatus(t, "member's email", member, 409), "email", "A new user cannot be created with this email")
	expectStatus(t, "outsider's email", outside, 409)
	if !bytes.Equal(member.Body, outside.Body) {
		t.Errorf("outsider's email = %s, member's email = %s", outside.Body, member.Body)
	}
}

// Demoting an owner or manager below manager through the users API takes back the team admin seats the role carried, in this organization only.
func TestV2UsersDemotionEndsTeamAdmin(t *testing.T) {
	e := newEnv(t)
	s := e.seedRecording()
	org, key := s.bound["<id:organization>"], s.bound["<api-key:manage>"]
	users := "/api/v2/organizations/" + org + "/users"
	manager := e.user("Manager", "manager@example.com")
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'manager', true)`, manager, org)
	team := e.insert(`INSERT INTO teams (organization_id, name) VALUES (?, 'Team') RETURNING id`, org)
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'admin')`, team, manager)
	elsewhere := e.insert(`INSERT INTO organizations (name) VALUES ('Elsewhere') RETURNING id`)
	foreign := e.insert(`INSERT INTO teams (organization_id, name) VALUES (?, 'Foreign team') RETURNING id`, elsewhere)
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'admin')`, foreign, manager)
	plain := e.user("Plain", "plain@example.com")
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'member', true)`, plain, org)
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'admin')`, team, plain)
	e.deliver()

	expectStatus(t, "demote the manager", e.do("PATCH", users, map[string]any{"email": "manager@example.com", "role": "member"}, "x-api-key", key), 200)
	expectStatus(t, "member's role sent unchanged", e.do("PATCH", users, map[string]any{"email": "plain@example.com", "role": "member"}, "x-api-key", key), 200)
	teamRole := func(team, user uuid.UUID) string {
		var role string
		if err := e.db.Raw(`SELECT role FROM team_members WHERE team_id = ? AND user_id = ?`, team, user).Scan(&role).Error; err != nil {
			t.Fatal(err)
		}
		return role
	}
	if got := teamRole(team, manager); got != "contributor" {
		t.Errorf("demoted manager's team role = %s, want contributor", got)
	}
	if got := teamRole(foreign, manager); got != "admin" {
		t.Errorf("another organization's team role = %s, want admin", got)
	}
	if got := teamRole(team, plain); got != "admin" {
		t.Errorf("plain member's team admin seat = %s, want admin", got)
	}
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

// The list filters name rows by their ids, and an empty filter is no filter.
func TestV2ListFilters(t *testing.T) {
	e := newEnv(t)
	s := e.seedRecording()
	key, org := s.bound["<api-key:manage>"], s.bound["<id:organization>"]
	sv := e.insert(`INSERT INTO surveys (workspace_id, name) VALUES (?, 'Survey') RETURNING id`, s.ws)
	contact := e.insert(`INSERT INTO contacts (workspace_id) VALUES (?) RETURNING id`, s.ws)
	mine := e.insert(`INSERT INTO responses (survey_id, contact_id, data) VALUES (?, ?, '{}') RETURNING id`, sv, contact)
	e.insert(`INSERT INTO responses (survey_id, data) VALUES (?, '{}') RETURNING id`, sv)

	ids := func(body map[string]any) []string {
		var out []string
		for _, item := range body["data"].([]any) {
			out = append(out, item.(map[string]any)["id"].(string))
		}
		return out
	}
	body := expectStatus(t, "contactId", e.do("GET", "/api/v2/management/responses?contactId="+contact.String(), nil, "x-api-key", key), http.StatusOK)
	if got := ids(body); len(got) != 1 || got[0] != mine.String() {
		t.Errorf("contactId=%s = %v, want [%s]", contact, got, mine)
	}
	if got := ids(expectStatus(t, "cuid2 contactId", e.do("GET", "/api/v2/management/responses?contactId=clcontactaaaaaaaaaaaaaaa1", nil, "x-api-key", key), http.StatusOK)); len(got) != 0 {
		t.Errorf("contactId=<cuid2> = %v, want none", got)
	}
	if got := ids(expectStatus(t, "empty contactId", e.do("GET", "/api/v2/management/responses?contactId=", nil, "x-api-key", key), http.StatusOK)); len(got) != 2 {
		t.Errorf("contactId= = %v, want both responses", got)
	}

	user := e.insert(`INSERT INTO users (name, email, email_verified) VALUES ('Member', 'member@example.com', true) RETURNING id`)
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'member', true)`, user, org)
	users := "/api/v2/organizations/" + org + "/users"
	if got := ids(expectStatus(t, "user id", e.do("GET", users+"?id="+user.String(), nil, "x-api-key", key), http.StatusOK)); len(got) != 1 || got[0] != user.String() {
		t.Errorf("id=%s = %v, want [%s]", user, got, user)
	}
	if got := ids(expectStatus(t, "empty user id", e.do("GET", users+"?id=", nil, "x-api-key", key), http.StatusOK)); len(got) != 2 {
		t.Errorf("id= = %v, want both members", got)
	}
}

// Owner decision D-AO02, through the users API: a role below manager disables the API keys the user created in the organization, with an audit event each attributed to the key that made the change, and a disabled key is refused from then on. Promotion and keys of other organizations are untouched.
func TestV2UsersDemotionDisablesTheCreatorsKeys(t *testing.T) {
	e := newEnv(t)
	s := e.seedRecording()
	org, ownerKey := s.bound["<id:organization>"], s.bound["<api-key:manage>"]
	users := "/api/v2/organizations/" + org + "/users"
	manager := e.user("Manager", "manager@example.com")
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'manager', true)`, manager, org)
	managerKey, managerKeyID := e.apiKey(uuid.MustParse(org), manager, "manager", true, true)
	elsewhere := e.insert(`INSERT INTO organizations (name) VALUES ('Elsewhere') RETURNING id`)
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'manager', true)`, manager, elsewhere)
	_, foreignKeyID := e.apiKey(elsewhere, manager, "elsewhere", true, false)
	e.deliver()
	reason := func(id uuid.UUID) string {
		var r *string
		if err := e.db.Raw(`SELECT disabled_reason FROM api_keys WHERE id = ?`, id).Row().Scan(&r); err != nil {
			t.Fatal(err)
		}
		if r == nil {
			return ""
		}
		return *r
	}

	expectStatus(t, "promote the manager", e.do("PATCH", users, map[string]any{"email": "manager@example.com", "role": "owner"}, "x-api-key", ownerKey), 200)
	if got := reason(managerKeyID); got != "" {
		t.Fatalf("a promotion disabled the key (%s)", got)
	}
	expectStatus(t, "the key works", e.do("GET", "/api/v2/organizations/"+org+"/teams", nil, "x-api-key", managerKey), 200)

	expectStatus(t, "demote to member", e.do("PATCH", users, map[string]any{"email": "manager@example.com", "role": "member"}, "x-api-key", ownerKey), 200)
	if got := reason(managerKeyID); got != "creator_demoted" {
		t.Fatalf("disabled_reason = %q, want creator_demoted", got)
	}
	if got := reason(foreignKeyID); got != "" {
		t.Fatalf("the key of another organization was disabled (%s)", got)
	}
	var disabled []auditEvent
	for _, ev := range e.audits() {
		if ev.Action == "disabled" {
			disabled = append(disabled, ev)
		}
	}
	if len(disabled) != 1 || disabled[0].Target.ID != managerKeyID.String() || disabled[0].Target.Type != "apiKey" || disabled[0].Actor.Type != "api" || disabled[0].Actor.ID != s.keys["manage"].String() || disabled[0].OrganizationID != org || disabled[0].Status != "success" {
		t.Fatalf("disabled events = %+v", disabled)
	}
	expectStatus(t, "the disabled key", e.do("GET", "/api/v2/organizations/"+org+"/teams", nil, "x-api-key", managerKey), 401)
}

// A full user record PATCHed again, as a directory sync does on every run, demotes nobody: the key of a creator who was already a member stays enabled and no disabled event is recorded.
func TestV2UsersUnchangedRoleKeepsTheCreatorsKeys(t *testing.T) {
	e := newEnv(t)
	s := e.seedRecording()
	org, ownerKey := s.bound["<id:organization>"], s.bound["<api-key:manage>"]
	member := e.user("Member", "member@example.com")
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'member', true)`, member, org)
	_, keyID := e.apiKey(uuid.MustParse(org), member, "legacy", true, false)
	e.deliver()

	expectStatus(t, "patch the unchanged role", e.do("PATCH", "/api/v2/organizations/"+org+"/users", map[string]any{"email": "member@example.com", "role": "member", "name": "Member"}, "x-api-key", ownerKey), 200)
	var reason *string
	if err := e.db.Raw(`SELECT disabled_reason FROM api_keys WHERE id = ?`, keyID).Row().Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if reason != nil {
		t.Fatalf("an unchanged role disabled the key (%s)", *reason)
	}
	for _, ev := range e.audits() {
		if ev.Action == "disabled" {
			t.Fatalf("an unchanged role recorded %+v", ev)
		}
	}
}

// The v2 team, user and workspace-team writes reach SpiceDB before they answer, with no worker running, so /api/v3, MCP and the app do not wait for the worker to see them and no revocation waits on a worker that may be minutes behind.
func TestV2AccessWritesAreProjectedBeforeTheyAnswer(t *testing.T) {
	e := newEnv(t)
	s := e.seedRecording()
	org, ws, key := s.bound["<id:organization>"], s.bound["<id:workspace>"], s.bound["<api-key:manage>"]
	pending := func(step string) {
		t.Helper()
		var n int64
		if err := e.db.Raw(`SELECT count(*) FROM authz_projection_outbox WHERE processed_at IS NULL`).Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%s: %d events left for the worker", step, n)
		}
	}
	base := "/api/v2/organizations/" + org

	body := expectStatus(t, "create a team", e.do("POST", base+"/teams", map[string]any{"name": "Team"}, "x-api-key", key), 201)
	team := body["data"].(map[string]any)["id"].(string)
	pending("create a team")
	expectStatus(t, "create a user", e.do("POST", base+"/users", map[string]any{"name": "New", "email": "new@example.com", "role": "member", "teams": []string{"Team"}}, "x-api-key", key), 201)
	pending("create a user")
	expectStatus(t, "grant the team", e.do("POST", base+"/workspace-teams", map[string]any{"teamId": team, "workspaceId": ws, "permission": "read"}, "x-api-key", key), 200)
	pending("grant the team")
	expectStatus(t, "raise the grant", e.do("PUT", base+"/workspace-teams", map[string]any{"teamId": team, "workspaceId": ws, "permission": "manage"}, "x-api-key", key), 200)
	pending("raise the grant")
	expectStatus(t, "remove the user's teams", e.do("PATCH", base+"/users", map[string]any{"email": "new@example.com", "teams": []string{}}, "x-api-key", key), 200)
	pending("remove the user's teams")
	expectStatus(t, "revoke the grant", e.do("DELETE", base+"/workspace-teams?teamId="+team+"&workspaceId="+ws, nil, "x-api-key", key), 200)
	pending("revoke the grant")
	expectStatus(t, "delete the team", e.do("DELETE", base+"/teams/"+team, nil, "x-api-key", key), 200)
	pending("delete the team")
}
