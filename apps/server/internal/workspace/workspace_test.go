package workspace

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/apps/server/internal/survey/scheduling"
)

func TestCreateWorkspace(t *testing.T) {
	e := newEnv(t)
	owner, manager, member, billing := e.user(), e.user(), e.user(), e.user()
	org, _ := e.organization(owner)
	e.member(org, manager, "manager")
	e.member(org, member, "member")
	e.member(org, billing, "billing")
	team := e.insert(`INSERT INTO teams (organization_id, name) VALUES (?, 'Team') RETURNING id`, org)

	var ws Workspace
	e.ok(e.do(manager, http.MethodPost, orgWorkspaces(org, ""), map[string]any{"name": " Second ", "teamIds": []uuid.UUID{team}, "config": map[string]any{"channel": "link"}}), http.StatusCreated, &ws)
	if ws.Name != "Second" || ws.OrganizationID != org || ws.RecontactDays != 7 || ws.Placement != "bottomRight" || ws.Overlay != "none" ||
		!ws.LinkSurveyBranding || !ws.InAppSurveyBranding || !ws.ClickOutsideClose || ws.AppSetupCompleted || string(ws.Logo) != "null" || ws.CustomHeadScripts != nil {
		t.Fatalf("created workspace %+v", ws)
	}
	if string(ws.Styling) != `{"allowStyleOverwrite":true}` || string(ws.Config) != `{"channel":"link","industry":null}` {
		t.Fatalf("styling %s config %s", ws.Styling, ws.Config)
	}
	if len(ws.Languages) != 1 || ws.Languages[0].Code != "en-US" || ws.Languages[0].Alias != nil {
		t.Fatalf("languages %+v", ws.Languages)
	}
	if e.count(`SELECT count(*) FROM contact_attribute_keys WHERE workspace_id = ? AND type = 'default'`, ws.ID) != 5 {
		t.Fatal("contact attribute keys not seeded")
	}
	if e.count(`SELECT count(*) FROM workspace_teams WHERE workspace_id = ? AND team_id = ? AND permission = 'read'`, ws.ID, team) != 1 {
		t.Fatal("team not linked with read access")
	}

	e.fails(e.do(owner, http.MethodPost, orgWorkspaces(org, ""), map[string]any{"name": "Second"}), http.StatusBadRequest, CodeNameTaken, "A workspace with this name already exists in your organization")
	e.fails(e.do(owner, http.MethodPost, orgWorkspaces(org, ""), map[string]any{"name": "  "}), http.StatusBadRequest, apperr.CodeValidation, "")
	e.fails(e.do(owner, http.MethodPost, orgWorkspaces(org, ""), map[string]any{"name": "X", "config": map[string]any{"industry": "retail"}}), http.StatusBadRequest, apperr.CodeValidation, "")
	for _, actor := range []uuid.UUID{member, billing, e.user()} {
		e.fails(e.do(actor, http.MethodPost, orgWorkspaces(org, ""), map[string]any{"name": "Mine"}), http.StatusForbidden, apperr.CodeForbidden, "Not authorized")
	}
	e.fails(e.do(owner, http.MethodPost, orgWorkspaces(uuid.New(), ""), map[string]any{"name": "Ghost"}), http.StatusForbidden, apperr.CodeForbidden, "")
}

func TestCreateWorkspaceRejectsForeignAndDuplicateTeams(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	org, _ := e.organization(owner)
	other, _ := e.organization(e.user())
	mine := e.insert(`INSERT INTO teams (organization_id, name) VALUES (?, 'Mine') RETURNING id`, org)
	foreign := e.insert(`INSERT INTO teams (organization_id, name) VALUES (?, 'Theirs') RETURNING id`, other)

	// A foreign team and a missing one fail alike, and nothing is created.
	for _, teams := range [][]uuid.UUID{{mine, foreign}, {uuid.New()}} {
		e.fails(e.do(owner, http.MethodPost, orgWorkspaces(org, ""), map[string]any{"name": "Linked", "teamIds": teams}), http.StatusBadRequest, apperr.CodeValidation, "teamIds must belong to the organization")
	}
	e.fails(e.do(owner, http.MethodPost, orgWorkspaces(org, ""), map[string]any{"name": "Linked", "teamIds": []uuid.UUID{mine, mine}}), http.StatusBadRequest, apperr.CodeValidation, "teamIds must be unique")
	if e.count(`SELECT count(*) FROM workspaces WHERE name = 'Linked'`) != 0 || e.count(`SELECT count(*) FROM workspace_teams WHERE team_id = ?`, foreign) != 0 {
		t.Fatal("a rejected creation left rows behind")
	}
	if !strings.Contains(e.logs.String(), foreign.String()) {
		t.Fatal("the rejected cross-organization team was not logged")
	}
}

func TestListWorkspaces(t *testing.T) {
	e := newEnv(t)
	owner, member, billing, outsider := e.user(), e.user(), e.user(), e.user()
	org, first := e.organization(owner)
	second, third := e.workspace(org, "Second"), e.workspace(org, "Third")
	e.member(org, member, "member")
	e.member(org, billing, "billing")
	e.grant(org, second, member, "read")
	e.grant(org, third, member, "readWrite")

	ids := func(user uuid.UUID, query string) []uuid.UUID {
		var res listResponse
		e.ok(e.do(user, http.MethodGet, orgWorkspaces(org, query), nil), http.StatusOK, &res)
		out := make([]uuid.UUID, len(res.Workspaces))
		for i, w := range res.Workspaces {
			out[i] = w.ID
		}
		return out
	}
	equal := func(got, want []uuid.UUID) bool { return strings.EqualFold(uuidsString(got), uuidsString(want)) }
	if got := ids(owner, ""); !equal(got, []uuid.UUID{first, second, third}) {
		t.Fatalf("owner lists %v", got)
	}
	if got := ids(member, ""); !equal(got, []uuid.UUID{second, third}) {
		t.Fatalf("member lists %v", got)
	}
	if got := ids(member, "?access=write"); !equal(got, []uuid.UUID{third}) {
		t.Fatalf("member writable %v", got)
	}
	if got := ids(billing, ""); len(got) != 0 {
		t.Fatalf("billing lists %v", got)
	}
	if got := ids(owner, "?page=2"); len(got) != 0 {
		t.Fatalf("page 2 %v", got)
	}
	// Writable lists need organization.read_access, which billing lacks; outsiders get nothing at all.
	e.fails(e.do(billing, http.MethodGet, orgWorkspaces(org, "?access=write"), nil), http.StatusForbidden, apperr.CodeForbidden, "")
	e.fails(e.do(outsider, http.MethodGet, orgWorkspaces(org, ""), nil), http.StatusForbidden, apperr.CodeForbidden, "")
}

func uuidsString(ids []uuid.UUID) string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = id.String()
	}
	return strings.Join(s, ",")
}

func TestGetWorkspace(t *testing.T) {
	for _, cloud := range []bool{false, true} {
		t.Run(map[bool]string{false: "self-hosted", true: "cloud"}[cloud], func(t *testing.T) {
			e := newEnv(t, func(c *config.Config) { c.IsFormaCloud = cloud })
			owner, reader, writer, ungranted, billing, outsider := e.user(), e.user(), e.user(), e.user(), e.user(), e.user()
			org, ws := e.organization(owner)
			for _, u := range []uuid.UUID{reader, writer, ungranted} {
				e.member(org, u, "member")
			}
			e.member(org, billing, "billing")
			e.grant(org, ws, reader, "read")
			e.grant(org, ws, writer, "readWrite")

			cases := []struct {
				user uuid.UUID
				want Access
			}{
				{owner, Access{Role: "owner", CanWrite: true, CanManage: true}},
				{reader, Access{Role: "member", IsReadOnly: true}},
				{writer, Access{Role: "member", CanWrite: true}},
			}
			for _, tc := range cases {
				var d Detail
				e.ok(e.do(tc.user, http.MethodGet, wsPath(ws, ""), nil), http.StatusOK, &d)
				if d.ID != ws || d.Access != tc.want {
					t.Fatalf("access %+v, want %+v", d.Access, tc.want)
				}
			}
			e.fails(e.do(ungranted, http.MethodGet, wsPath(ws, ""), nil), http.StatusForbidden, apperr.CodeForbidden, "Not authorized")
			body := e.fails(e.do(billing, http.MethodGet, wsPath(ws, ""), nil), http.StatusForbidden, CodeBillingRole, "")
			want := "/account/settings/profile"
			if cloud {
				want = "/organizations/" + org.String() + "/settings/billing"
			}
			if body.Details["redirect"] != want {
				t.Fatalf("billing redirect %v, want %s", body.Details, want)
			}
			// An outsider learns nothing: a foreign workspace and a missing one are refused alike.
			for _, id := range []uuid.UUID{ws, uuid.New()} {
				e.fails(e.do(outsider, http.MethodGet, wsPath(id, ""), nil), http.StatusForbidden, apperr.CodeForbidden, "Not authorized")
			}
		})
	}
}

// The survey editor reads when scheduled surveys open and close from the workspace: the organization's zone, or the instance's.
func TestWorkspaceSurveyScheduling(t *testing.T) {
	e := newEnv(t, func(c *config.Config) {
		c.SurveySchedulingTimeZone, c.SurveySchedulingLocalHour, c.SurveySchedulingLocalMinute = "Europe/Berlin", 6, 30
	})
	owner := e.user()
	org, ws := e.organization(owner)
	var d Detail
	e.ok(e.do(owner, http.MethodGet, wsPath(ws, ""), nil), http.StatusOK, &d)
	want := scheduling.Settings{TimeZone: "Europe/Berlin", LocalHour: 6, LocalMinute: 30, InstanceTimeZone: "Europe/Berlin"}
	if d.SurveyScheduling != want {
		t.Fatalf("instance default %+v", d.SurveyScheduling)
	}
	e.exec(`UPDATE organizations SET survey_scheduling_time_zone = 'Asia/Tokyo' WHERE id = ?`, org)
	e.ok(e.do(owner, http.MethodGet, wsPath(ws, ""), nil), http.StatusOK, &d)
	want.TimeZone = "Asia/Tokyo"
	if d.SurveyScheduling != want {
		t.Fatalf("organization zone %+v", d.SurveyScheduling)
	}
}

func TestUpdateWorkspace(t *testing.T) {
	e := newEnv(t)
	owner, manageTeam, writer := e.user(), e.user(), e.user()
	org, ws := e.organization(owner)
	other, _ := e.organization(e.user())
	e.member(org, manageTeam, "member")
	e.member(org, writer, "member")
	e.grant(org, ws, manageTeam, "manage")
	e.grant(org, ws, writer, "readWrite")
	e.workspace(org, "Taken")
	e.exec(`INSERT INTO languages (workspace_id, code) VALUES (?, 'de')`, ws)
	e.exec(`UPDATE workspaces SET config = '{"channel": "app", "industry": "saas", "custom": 1}' WHERE id = ?`, ws)
	path := wsPath(ws, "")

	var got Workspace
	// A team with a manage grant edits settings; organizationId is ignored, so the workspace cannot move tenants.
	e.ok(e.do(manageTeam, http.MethodPatch, path, map[string]any{
		"name": " Renamed ", "recontactDays": 0, "placement": "center", "overlay": "dark", "clickOutsideClose": false,
		"linkSurveyBranding": false, "inAppSurveyBranding": false, "organizationId": other.String(),
		"styling": map[string]any{"allowStyleOverwrite": false, "brandColor": map[string]any{"light": "#123456"}},
		"logo":    map[string]any{"url": "/storage/x/public/logo.png"},
		"config":  map[string]any{"defaultSurveyLanguage": " de-DE "},
	}), http.StatusOK, &got)
	if got.Name != "Renamed" || got.RecontactDays != 0 || got.Placement != "center" || got.Overlay != "dark" || got.ClickOutsideClose || got.LinkSurveyBranding || got.InAppSurveyBranding || got.OrganizationID != org {
		t.Fatalf("updated %+v", got)
	}
	var cfg map[string]any
	_ = json.Unmarshal(got.Config, &cfg)
	// Merged onto the stored config: untouched keys survive, the new one is trimmed. A legacy "de" row is the de-DE language.
	if cfg["channel"] != "app" || cfg["industry"] != "saas" || cfg["custom"] != float64(1) || cfg["defaultSurveyLanguage"] != "de-DE" {
		t.Fatalf("config %s", got.Config)
	}
	e.ok(e.do(owner, http.MethodPatch, path, map[string]any{"config": map[string]any{"defaultSurveyLanguage": nil, "channel": "website"}}), http.StatusOK, &got)
	_ = json.Unmarshal(got.Config, &cfg)
	if cfg["defaultSurveyLanguage"] != nil || cfg["channel"] != "website" || cfg["industry"] != "saas" {
		t.Fatalf("config after clearing %s", got.Config)
	}

	refused := []struct {
		user          uuid.UUID
		body          map[string]any
		status        int
		code, message string
	}{
		{writer, map[string]any{"name": "Mine"}, http.StatusForbidden, apperr.CodeForbidden, "Not authorized"},
		{owner, map[string]any{"name": "Taken"}, http.StatusBadRequest, CodeNameTaken, ""},
		{owner, map[string]any{"name": " "}, http.StatusBadRequest, apperr.CodeValidation, ""},
		{owner, map[string]any{"recontactDays": 366}, http.StatusBadRequest, apperr.CodeValidation, ""},
		{owner, map[string]any{"recontactDays": -1}, http.StatusBadRequest, apperr.CodeValidation, ""},
		{owner, map[string]any{"recontactDays": 1.5}, http.StatusBadRequest, apperr.CodeValidation, ""},
		{owner, map[string]any{"placement": "middle"}, http.StatusBadRequest, apperr.CodeValidation, ""},
		{owner, map[string]any{"styling": map[string]any{"brandColor": map[string]any{"light": "#123456"}}}, http.StatusBadRequest, apperr.CodeValidation, ""},
		{owner, map[string]any{"styling": nil}, http.StatusBadRequest, apperr.CodeValidation, ""},
		{owner, map[string]any{"logo": map[string]any{"url": "javascript:alert(1)"}}, http.StatusBadRequest, apperr.CodeValidation, ""},
		{owner, map[string]any{"styling": map[string]any{"allowStyleOverwrite": true, "logo": map[string]any{"url": "data:text/html,x"}}}, http.StatusBadRequest, apperr.CodeValidation, ""},
		{owner, map[string]any{"styling": map[string]any{"allowStyleOverwrite": true, "background": map[string]any{"bgType": "upload"}}}, http.StatusBadRequest, apperr.CodeValidation, ""},
		{owner, map[string]any{"config": map[string]any{"defaultSurveyLanguage": "fa-IR"}}, http.StatusBadRequest, apperr.CodeValidation, "Unsupported default survey language"},
		{owner, map[string]any{"config": map[string]any{"defaultSurveyLanguage": "fr-FR"}}, http.StatusForbidden, apperr.CodeOperationNotAllowed, "The default survey language must be one of the workspace's survey languages"},
		{owner, map[string]any{"config": map[string]any{"channel": "email"}}, http.StatusBadRequest, apperr.CodeValidation, ""},
		// Custom head scripts are the owner's and the manager's, not a manage team's.
		{manageTeam, map[string]any{"customHeadScripts": "<script></script>"}, http.StatusForbidden, apperr.CodeForbidden, ""},
	}
	for _, r := range refused {
		e.fails(e.do(r.user, http.MethodPatch, path, r.body), r.status, r.code, r.message)
	}
	var name string
	if err := e.db.Raw(`SELECT name FROM workspaces WHERE id = ?`, ws).Row().Scan(&name); err != nil || name != "Renamed" {
		t.Fatalf("a refused update changed the name to %q", name)
	}

	script := "<script>console.log(1)</script>"
	e.ok(e.do(owner, http.MethodPatch, path, map[string]any{"customHeadScripts": script}), http.StatusOK, &got)
	if got.CustomHeadScripts == nil || *got.CustomHeadScripts != script {
		t.Fatalf("custom head scripts %v", got.CustomHeadScripts)
	}
	e.ok(e.do(owner, http.MethodPatch, path, map[string]any{"customHeadScripts": nil}), http.StatusOK, &got)
	if got.CustomHeadScripts != nil {
		t.Fatal("null did not clear the custom head scripts")
	}
	if !strings.Contains(e.logs.String(), `"action":"updated"`) {
		t.Fatal("no audit event for the update")
	}
}

func TestCustomHeadScriptsAreSelfHostedOnly(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.IsFormaCloud = true })
	owner := e.user()
	_, ws := e.organization(owner)
	e.fails(e.do(owner, http.MethodPatch, wsPath(ws, ""), map[string]any{"customHeadScripts": "<script></script>"}), http.StatusForbidden, apperr.CodeOperationNotAllowed, "")
}

func TestDeleteWorkspace(t *testing.T) {
	e := newEnv(t)
	owner, manageTeam, outsider := e.user(), e.user(), e.user()
	org, first := e.organization(owner)
	second := e.workspace(org, "Second")
	e.member(org, manageTeam, "member")
	e.grant(org, second, manageTeam, "manage")
	del := func(user, ws uuid.UUID, confirmation string) *Deletion {
		var d Deletion
		e.ok(e.do(user, http.MethodPost, wsPath(ws, "/delete"), map[string]string{"confirmationName": confirmation}), http.StatusOK, &d)
		return &d
	}

	e.fails(e.do(owner, http.MethodPost, wsPath(second, "/delete"), map[string]string{"confirmationName": " "}), http.StatusBadRequest, CodeDeleteConfirmationRequired, "Workspace name confirmation is required to delete this workspace.")
	// The name is compared only after authorization: an outsider cannot probe a workspace, whether the name is right or the workspace exists.
	for _, id := range []uuid.UUID{second, uuid.New()} {
		e.fails(e.do(outsider, http.MethodPost, wsPath(id, "/delete"), map[string]string{"confirmationName": "Second"}), http.StatusForbidden, apperr.CodeForbidden, "Not authorized")
	}
	// A manage grant edits a workspace but does not delete it.
	e.fails(e.do(manageTeam, http.MethodPost, wsPath(second, "/delete"), map[string]string{"confirmationName": "Second"}), http.StatusForbidden, apperr.CodeForbidden, "")
	e.fails(e.do(owner, http.MethodPost, wsPath(second, "/delete"), map[string]string{"confirmationName": "Secnd"}), http.StatusBadRequest, CodeDeleteConfirmationMismatch, "Workspace name confirmation does not match")

	// The surviving workspace has no survey yet, so the destination is onboarding.
	e.survey(second)
	d := del(owner, second, "  sECOND ")
	if d.Workspace.ID != second || d.Destination.WorkspaceID == nil || *d.Destination.WorkspaceID != first || d.Destination.Path != "/organizations/"+org.String()+"/workspaces/new/survey" {
		t.Fatalf("deletion %+v", d.Destination)
	}
	if e.count(`SELECT count(*) FROM workspaces WHERE id = ?`, second) != 0 || e.count(`SELECT count(*) FROM surveys WHERE workspace_id = ?`, second) != 0 {
		t.Fatal("workspace rows survived")
	}
	e.fails(e.do(owner, http.MethodPost, wsPath(first, "/delete"), map[string]string{"confirmationName": DefaultName}), http.StatusForbidden, apperr.CodeOperationNotAllowed, "You can't delete the last workspace.")

	third := e.workspace(org, "Third")
	e.exec(`UPDATE workspaces SET legacy_environment_id = 'clegacyenvaaaaaaaaaaaaaa1' WHERE id = ?`, third)
	e.survey(first)
	d = del(owner, third, "Third")
	if d.Destination.Path != "/workspaces/"+first.String()+"/" {
		t.Fatalf("destination after onboarding %+v", d.Destination)
	}

	// Only the two deletions that happened cleaned up files: under the workspace id, and under the environment id a workspace from before the rename stored its files under.
	want := [][]string{{second.String()}, {third.String(), "clegacyenvaaaaaaaaaaaaaa1"}}
	if got := e.files.calls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("deleted files under %v, want %v", got, want)
	}
}

func TestOnboarding(t *testing.T) {
	e := newEnv(t)
	owner, member := e.user(), e.user()
	org, ws := e.organization(owner)
	e.member(org, member, "member")
	path := "/api/app/organizations/" + org.String() + "/onboarding"

	var o Onboarding
	e.ok(e.do(owner, http.MethodGet, path, nil), http.StatusOK, &o)
	if o.WorkspaceID != ws || o.CompletedPath != nil {
		t.Fatalf("onboarding %+v", o)
	}
	e.survey(ws)
	e.ok(e.do(owner, http.MethodGet, path, nil), http.StatusOK, &o)
	if o.CompletedPath == nil || *o.CompletedPath != "/workspaces/"+ws.String()+"/" {
		t.Fatalf("completed onboarding %+v", o)
	}
	e.fails(e.do(member, http.MethodGet, path, nil), http.StatusNotFound, apperr.CodeNotFound, "")
}

func TestResolveEnvironment(t *testing.T) {
	e := newEnv(t)
	owner, billing, ungranted, outsider := e.user(), e.user(), e.user(), e.user()
	org, ws := e.organization(owner)
	e.member(org, billing, "billing")
	e.member(org, ungranted, "member")
	e.exec(`UPDATE workspaces SET legacy_environment_id = 'clr8k2m3n0001abcd' WHERE id = ?`, ws)

	for _, id := range []string{ws.String(), "clr8k2m3n0001abcd"} {
		var res environmentResponse
		e.ok(e.do(owner, http.MethodGet, "/api/app/environments/"+id, nil), http.StatusOK, &res)
		if res.WorkspaceID != ws {
			t.Fatalf("%s resolved to %s", id, res.WorkspaceID)
		}
	}
	// The navigation gate admits billing members; anyone who may not navigate the workspace, and an unknown id, are not found.
	e.ok(e.do(billing, http.MethodGet, "/api/app/environments/"+ws.String(), nil), http.StatusOK, nil)
	for _, u := range []uuid.UUID{ungranted, outsider} {
		e.fails(e.do(u, http.MethodGet, "/api/app/environments/"+ws.String(), nil), http.StatusNotFound, apperr.CodeNotFound, "")
	}
	e.fails(e.do(owner, http.MethodGet, "/api/app/environments/unknown", nil), http.StatusNotFound, apperr.CodeNotFound, "")
}

func TestValidStorageURL(t *testing.T) {
	cases := map[string]bool{
		"/storage/a/public/b.png": true, "https://cdn.example.com/x.png": true, "http://example.com": true, "HTTPS://EXAMPLE.COM/a": true,
		"javascript:alert(1)": false, "data:image/png;base64,AA": false, "vbscript:x": false, "//evil.example/x": false, "storage/x": false, "": false,
	}
	for in, want := range cases {
		if got := validStorageURL(in); got != want {
			t.Errorf("validStorageURL(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestResolveClientID(t *testing.T) {
	e := newEnv(t)
	org, ws := e.organization(e.user())
	e.exec(`UPDATE workspaces SET legacy_environment_id = 'clegacyenvaaaaaaaaaaaaaa1' WHERE id = ?`, ws)
	// A workspace migrated from forma2 carries uuidv5 of its old id.
	migrated := legacyid.ID("clmigratedwsaaaaaaaaaaa1")
	e.exec(`INSERT INTO workspaces (id, organization_id, name) VALUES (?, ?, 'Migrated')`, migrated, org)

	for raw, want := range map[string]uuid.UUID{
		ws.String():                  ws,
		strings.ToUpper(ws.String()): ws,
		"clegacyenvaaaaaaaaaaaaaa1":  ws,
		"clmigratedwsaaaaaaaaaaa1":   migrated,
		migrated.String():            migrated,
		"clunknownaaaaaaaaaaaaaaa1":  uuid.Nil,
		uuid.NewString():             uuid.Nil,
		"../" + ws.String():          uuid.Nil,
		strings.Repeat("x", 4096):    uuid.Nil,
	} {
		got, err := e.repo.ResolveClientID(t.Context(), raw)
		if err != nil {
			t.Fatalf("ResolveClientID(%.40q): %v", raw, err)
		}
		if want == uuid.Nil {
			if got != nil {
				t.Errorf("ResolveClientID(%.40q) = %+v, want nothing", raw, got)
			}
			continue
		}
		if got == nil || got.ID != want || got.OrganizationID != org {
			t.Errorf("ResolveClientID(%.40q) = %+v, want workspace %s of %s", raw, got, want, org)
		}
	}
}
