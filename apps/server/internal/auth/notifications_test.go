package auth

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

type notificationsFixture struct {
	user, owned, teamOrg, noAccessOrg, billingOrg uuid.UUID
	ownedSurvey, teamSurvey, hiddenSurvey         uuid.UUID
}

// seedNotifications builds a user who owns one organization (one workspace with a survey), is a member of a second with one workspace through a team and one without, is a member of a third with no team access, and is billing in a fourth.
func (e *env) seedNotifications() notificationsFixture {
	e.t.Helper()
	var f notificationsFixture
	f.user = e.seedUser("ada@example.com", strongPassword, userOpts{})
	org := func(name, role string) uuid.UUID {
		id := e.insertID(`INSERT INTO organizations (name) VALUES (?) RETURNING id`, name)
		e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, ?, true)`, f.user, id, role)
		return id
	}
	workspace := func(orgID uuid.UUID, name string) uuid.UUID {
		return e.insertID(`INSERT INTO workspaces (organization_id, name) VALUES (?, ?) RETURNING id`, orgID, name)
	}
	survey := func(workspaceID uuid.UUID, name string) uuid.UUID {
		return e.insertID(`INSERT INTO surveys (workspace_id, name) VALUES (?, ?) RETURNING id`, workspaceID, name)
	}
	f.owned = org("Owned", "owner")
	f.ownedSurvey = survey(workspace(f.owned, "Owned workspace"), "Owned survey")

	f.teamOrg = org("Team org", "member")
	granted := workspace(f.teamOrg, "Granted")
	f.teamSurvey = survey(granted, "Team survey")
	f.hiddenSurvey = survey(workspace(f.teamOrg, "Not granted"), "Hidden survey")
	team := e.insertID(`INSERT INTO teams (organization_id, name) VALUES (?, 'Team') RETURNING id`, f.teamOrg)
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'contributor')`, team, f.user)
	e.exec(`INSERT INTO workspace_teams (workspace_id, team_id, permission) VALUES (?, ?, 'read')`, granted, team)

	f.noAccessOrg = org("No access", "member")
	survey(workspace(f.noAccessOrg, "Elsewhere"), "Elsewhere survey")
	f.billingOrg = org("Billing", "billing")
	survey(workspace(f.billingOrg, "Billed"), "Billed survey")

	e.exec(`UPDATE users SET notification_settings = ? WHERE id = ?`,
		`{"alert": {"`+f.ownedSurvey.String()+`": true}, "unsubscribedOrganizationIds": ["`+f.owned.String()+`"], "weeklySummary": {"x": true}}`, f.user)
	return f
}

type notificationsBody struct {
	Organizations []NotificationOrganization `json:"organizations"`
}

func (b *browser) notifications() notificationsBody {
	b.e.t.Helper()
	r := b.get("/auth/me/notifications")
	expectStatus(b.e.t, r, http.StatusOK, "")
	var body notificationsBody
	if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
		b.e.t.Fatal(err)
	}
	return body
}

// forma2's notifications page: non-billing organizations, the surveys of the workspaces the user can open, the stored alert switches.
func TestNotificationsList(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	f := e.seedNotifications()
	b := e.sessionFor(f.user)

	expectStatus(t, e.browser().get("/auth/me/notifications"), http.StatusUnauthorized, apperr.CodeNotAuthenticated)

	orgs := b.notifications().Organizations
	if len(orgs) != 2 || orgs[0].ID != f.owned || orgs[1].ID != f.teamOrg {
		t.Fatalf("organizations %+v", orgs)
	}
	if orgs[0].AutoSubscribe || !orgs[1].AutoSubscribe {
		t.Fatalf("auto-subscribe %+v", orgs)
	}
	if s := orgs[0].Surveys; len(s) != 1 || s[0].ID != f.ownedSurvey || !s[0].Alert || s[0].WorkspaceName != "Owned workspace" {
		t.Fatalf("owned surveys %+v", s)
	}
	if s := orgs[1].Surveys; len(s) != 1 || s[0].ID != f.teamSurvey || s[0].Alert {
		t.Fatalf("team surveys %+v", s)
	}
}

func TestNotificationsUpdate(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	f := e.seedNotifications()
	b := e.sessionFor(f.user)
	set := func(kind string, id uuid.UUID, enabled bool) response {
		return b.patch("/auth/me/notifications", map[string]any{"type": kind, "id": id, "enabled": enabled})
	}

	expectStatus(t, set(notificationAlert, f.teamSurvey, true), http.StatusNoContent, "")
	expectStatus(t, set(notificationAlert, f.ownedSurvey, false), http.StatusNoContent, "")
	expectStatus(t, set(notificationOrganizationAutoAlert, f.owned, true), http.StatusNoContent, "")
	expectStatus(t, set(notificationOrganizationAutoAlert, f.teamOrg, false), http.StatusNoContent, "")
	// Repeating a switch leaves one entry.
	expectStatus(t, set(notificationOrganizationAutoAlert, f.teamOrg, false), http.StatusNoContent, "")

	orgs := b.notifications().Organizations
	if !orgs[0].AutoSubscribe || orgs[1].AutoSubscribe || orgs[0].Surveys[0].Alert || !orgs[1].Surveys[0].Alert {
		t.Fatalf("after update %+v", orgs)
	}
	var doc map[string]json.RawMessage
	var raw []byte
	if err := e.db.Raw(`SELECT notification_settings FROM users WHERE id = ?`, f.user).Row().Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if string(doc["unsubscribedOrganizationIds"]) != `["`+f.teamOrg.String()+`"]` || doc["weeklySummary"] == nil {
		t.Fatalf("stored %s", raw)
	}

	// Ids the page does not show this user are refused, unlike forma2, which stored any document.
	for _, tc := range []struct {
		kind string
		id   uuid.UUID
		code string
	}{
		{notificationAlert, f.hiddenSurvey, apperr.CodeNotFound},
		{notificationAlert, uuid.New(), apperr.CodeNotFound},
		{notificationOrganizationAutoAlert, f.billingOrg, apperr.CodeNotFound},
		{notificationOrganizationAutoAlert, f.noAccessOrg, apperr.CodeNotFound},
	} {
		expectStatus(t, set(tc.kind, tc.id, true), http.StatusNotFound, tc.code)
	}
	for _, body := range []map[string]any{
		{"type": "weekly", "id": f.owned, "enabled": true},
		{"type": notificationAlert, "id": f.ownedSurvey},
		{"type": notificationAlert, "id": uuid.Nil, "enabled": true},
	} {
		expectStatus(t, b.patch("/auth/me/notifications", body), http.StatusBadRequest, apperr.CodeValidation)
	}
	expectStatus(t, b.patch("/auth/me/notifications", map[string]any{"type": notificationAlert, "id": "not-an-id", "enabled": true}), http.StatusBadRequest, apperr.CodeBadRequest)
}

// The weekly summary is on for every organization until the user turns it off; the switch is stored as an opt-out list, and only for organizations the page shows.
func TestNotificationsWeeklySummary(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	f := e.seedNotifications()
	b := e.sessionFor(f.user)
	set := func(id uuid.UUID, enabled bool) response {
		return b.patch("/auth/me/notifications", map[string]any{"type": NotificationWeeklySummary, "id": id, "enabled": enabled})
	}
	stored := func() map[string]json.RawMessage {
		t.Helper()
		var raw []byte
		if err := e.db.Raw(`SELECT notification_settings FROM users WHERE id = ?`, f.user).Row().Scan(&raw); err != nil {
			t.Fatal(err)
		}
		doc := map[string]json.RawMessage{}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		return doc
	}

	orgs := b.notifications().Organizations
	if !orgs[0].WeeklySummary || !orgs[1].WeeklySummary {
		t.Fatalf("default %+v", orgs)
	}
	// Switching another setting does not write the weekly summary's key.
	expectStatus(t, b.patch("/auth/me/notifications", map[string]any{"type": notificationAlert, "id": f.teamSurvey, "enabled": true}), http.StatusNoContent, "")
	if _, ok := stored()[NotificationWeeklySummary]; ok {
		t.Fatal("the weekly summary key was written by another switch")
	}

	expectStatus(t, set(f.teamOrg, false), http.StatusNoContent, "")
	expectStatus(t, set(f.teamOrg, false), http.StatusNoContent, "")
	orgs = b.notifications().Organizations
	if !orgs[0].WeeklySummary || orgs[1].WeeklySummary {
		t.Fatalf("after turning off %+v", orgs)
	}
	doc := stored()
	if string(doc[NotificationWeeklySummary]) != `["`+f.teamOrg.String()+`"]` || doc["weeklySummary"] == nil || string(doc["unsubscribedOrganizationIds"]) != `["`+f.owned.String()+`"]` {
		t.Fatalf("stored %v", doc)
	}
	expectStatus(t, set(f.teamOrg, true), http.StatusNoContent, "")
	if string(stored()[NotificationWeeklySummary]) != `[]` || !b.notifications().Organizations[1].WeeklySummary {
		t.Fatal("turning it back on")
	}

	for _, id := range []uuid.UUID{f.billingOrg, f.noAccessOrg, f.ownedSurvey} {
		expectStatus(t, set(id, false), http.StatusNotFound, apperr.CodeNotFound)
	}
}

// An organization that refuses the user, by a suspension or, once its grace period is over, by its two-factor requirement (the user signs in with a password and has not enrolled), is left off the page with its workspaces and surveys, and its switches are refused: the app refuses the user its workspaces, and its alerts and weekly summary are not sent to them.
func TestNotificationsLeaveOutOrganizationsThatRefuseTheUser(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	f := e.seedNotifications()
	b := e.sessionFor(f.user)
	set := func(kind string, id uuid.UUID) response {
		return b.patch("/auth/me/notifications", map[string]any{"type": kind, "id": id, "enabled": true})
	}
	ids := func() []uuid.UUID {
		t.Helper()
		var out []uuid.UUID
		for _, o := range b.notifications().Organizations {
			out = append(out, o.ID)
		}
		return out
	}

	// In the grace period the requirement refuses no one yet.
	e.exec(`UPDATE organizations SET two_factor_enforced_at = now() + interval '1 day' WHERE id = ?`, f.teamOrg)
	if got := ids(); len(got) != 2 || got[0] != f.owned || got[1] != f.teamOrg {
		t.Fatalf("in the grace period %v", got)
	}

	e.exec(`UPDATE organizations SET two_factor_enforced_at = now() - interval '1 minute' WHERE id = ?`, f.teamOrg)
	e.exec(`UPDATE memberships SET suspended_at = now() WHERE user_id = ? AND organization_id = ?`, f.user, f.owned)
	if got := ids(); len(got) != 0 {
		t.Fatalf("refused organizations listed: %v", got)
	}
	expectStatus(t, set(notificationAlert, f.teamSurvey), http.StatusNotFound, apperr.CodeNotFound)
	expectStatus(t, set(notificationAlert, f.ownedSurvey), http.StatusNotFound, apperr.CodeNotFound)
	expectStatus(t, set(notificationOrganizationAutoAlert, f.teamOrg), http.StatusNotFound, apperr.CodeNotFound)
	expectStatus(t, set(NotificationWeeklySummary, f.teamOrg), http.StatusNotFound, apperr.CodeNotFound)

	// Once the user enrols, the requirement no longer refuses them.
	e.exec(`INSERT INTO user_totp (user_id, secret_ciphertext, backup_codes_ciphertext, verified) VALUES (?, '\x00', '\x00', true)`, f.user)
	if got := ids(); len(got) != 1 || got[0] != f.teamOrg {
		t.Fatalf("after enrolling %v", got)
	}
}
