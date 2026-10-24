package invite

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
)

func decode(t *testing.T, r response, dst any) {
	t.Helper()
	if err := json.Unmarshal(r.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode %s: %v", r.Body.String(), err)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

// forma2's InvitePage: what a link shows before anything is accepted.
func TestInspectInvite(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	o := e.organization()
	grace := e.userWith("Grace", "Grace@Example.com", "en-US")
	_, valid := e.seedInvite(o.id, o.owner.id, "grace@example.com", "member", time.Hour)
	_, expired := e.seedInvite(o.id, o.owner.id, "late@example.com", "member", -time.Minute)

	cases := []struct {
		name  string
		as    user
		token string
		want  string
	}{
		{"signed out", user{}, valid, `{"status":"valid","email":"grace@example.com","organizationName":"Acme","inviterName":"Owner"}`},
		{"the invitee, in another case", grace, valid, `{"status":"valid","email":"grace@example.com","organizationName":"Acme","inviterName":"Owner"}`},
		// The page names the invited address, so the visitor knows which account to sign in with.
		{"someone else", o.member, valid, `{"status":"email_mismatch","email":"grace@example.com","organizationName":"Acme","inviterName":"Owner"}`},
		{"an expired invite", user{}, expired, `{"status":"expired"}`},
		{"an unknown link", user{}, "bm90LWEtbGluaw", `{"status":"not_found"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := e.do(tc.as, http.MethodPost, "/invites/inspect", map[string]string{"token": tc.token})
			expect(t, r, http.StatusOK, "")
			if r.Body.String() != tc.want {
				t.Fatalf("inspect %s, want %s", r.Body.String(), tc.want)
			}
		})
	}
	expect(t, e.do(user{}, http.MethodPost, "/invites/inspect", map[string]string{"token": ""}), http.StatusBadRequest, "validation_error")
}

// forma2's createMembershipAction on the invite page: the membership with the invite's role, its teams (as a contributor for a member), the organization's alerts off with other settings kept, the invite consumed, the inviter told, and SpiceDB following.
func TestAcceptInvite(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	o := e.organization()
	inviter := e.join(o.id, e.userWith("Olivia Owner", "olivia@example.com", "ja-JP"), "owner")
	team := e.team(o.id, "Design")
	ws := e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Website') RETURNING id`, o.id)
	e.exec(`INSERT INTO workspace_teams (workspace_id, team_id, permission) VALUES (?, ?, 'readWrite')`, ws, team)
	grace := e.userWith("Grace Hopper", "GRACE@example.com", "en-US")
	e.exec(`UPDATE users SET notification_settings = '{"alert": {"x": true}, "weeklySummary": {"w": false}, "unsubscribedOrganizationIds": ["older"]}' WHERE id = ?`, grace.id)
	inviteID, token := e.seedInvite(o.id, inviter.id, "grace@example.com", "member", time.Hour, team)
	e.drain()

	expect(t, e.do(user{}, http.MethodPost, "/invites/accept", map[string]string{"token": token}), http.StatusUnauthorized, "not_authenticated")
	expect(t, e.do(o.member, http.MethodPost, "/invites/accept", map[string]string{"token": token}), http.StatusForbidden, CodeEmailMismatch)

	r := e.do(grace, http.MethodPost, "/invites/accept", map[string]string{"token": token})
	expect(t, r, http.StatusOK, "")
	if r.json()["organizationId"] != o.id.String() {
		t.Fatalf("accept answered %s", r.Body.String())
	}
	var role string
	var accepted bool
	if err := e.db.Raw(`SELECT role, accepted FROM memberships WHERE organization_id = ? AND user_id = ?`, o.id, grace.id).Row().Scan(&role, &accepted); err != nil {
		t.Fatal(err)
	}
	if role != "member" || !accepted || e.count(`SELECT count(*) FROM team_members WHERE team_id = ? AND user_id = ? AND role = 'contributor'`, team, grace.id) != 1 {
		t.Fatalf("membership %q accepted %v, or the team was not joined", role, accepted)
	}
	var settings string
	e.db.Raw(`SELECT notification_settings::text FROM users WHERE id = ?`, grace.id).Scan(&settings)
	if settings != `{"alert": {"x": true}, "weeklySummary": {"w": false}, "unsubscribedOrganizationIds": ["older", "`+o.id.String()+`"]}` {
		t.Fatalf("notification settings %s", settings)
	}
	if e.count(`SELECT count(*) FROM invites WHERE id = ?`, inviteID)+e.count(`SELECT count(*) FROM invite_tokens WHERE invite_id = ?`, inviteID) != 0 {
		t.Fatal("the invite outlived its acceptance")
	}
	msgs := e.sent("olivia@example.com")
	jaSubject, _, _ := e.svc.mailer.Render("ja-JP", mailer.InviteAccepted{})
	if len(msgs) != 1 || msgs[0].Subject != jaSubject || !contains(msgs[0].HTML, "Grace Hopper") {
		t.Fatalf("accepted mail %+v", msgs)
	}
	if ev := e.auditEvents(audit.ActionCreated, audit.TargetMembership); len(ev) != 1 || ev[0]["organizationId"] != o.id.String() {
		t.Fatalf("created/membership events %v", ev)
	}

	e.drain()
	ok, err := e.authorizer.Can(context.Background(), authz.User(grace.id), authz.WorkspaceWrite, authz.Workspace(ws))
	if err != nil || !ok {
		t.Fatalf("the invitee cannot write the team's workspace: %v, %v", ok, err)
	}

	// The link worked once.
	expect(t, e.do(grace, http.MethodPost, "/invites/accept", map[string]string{"token": token}), http.StatusNotFound, CodeInviteNotFound)
}

// An expired invite cannot be accepted; an owner or manager joins the invite's teams as an admin; a user who is already a member keeps their role.
func TestAcceptInviteRules(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	o := e.organization()
	team := e.team(o.id, "Design")

	late := e.userWith("Late", "late@example.com", "en-US")
	_, expired := e.seedInvite(o.id, o.owner.id, "late@example.com", "member", -time.Minute)
	expect(t, e.do(late, http.MethodPost, "/invites/accept", map[string]string{"token": expired}), http.StatusBadRequest, CodeInviteExpired)
	if e.count(`SELECT count(*) FROM memberships WHERE user_id = ?`, late.id) != 0 {
		t.Fatal("an expired invite was accepted")
	}

	boss := e.userWith("Boss", "boss@example.com", "en-US")
	_, token := e.seedInvite(o.id, o.owner.id, "boss@example.com", "manager", time.Hour, team)
	expect(t, e.do(boss, http.MethodPost, "/invites/accept", map[string]string{"token": token}), http.StatusOK, "")
	if e.count(`SELECT count(*) FROM team_members WHERE team_id = ? AND user_id = ? AND role = 'admin'`, team, boss.id) != 1 {
		t.Fatal("a manager did not join the team as an admin")
	}

	// A stale owner invite to someone who has since become a member does not promote them, and still joins them to its teams by the role they hold.
	_, stale := e.seedInvite(o.id, o.owner.id, o.member.email, "owner", time.Hour, team)
	expect(t, e.do(o.member, http.MethodPost, "/invites/accept", map[string]string{"token": stale}), http.StatusOK, "")
	var role string
	e.db.Raw(`SELECT role FROM memberships WHERE organization_id = ? AND user_id = ?`, o.id, o.member.id).Scan(&role)
	if role != "member" || e.count(`SELECT count(*) FROM team_members WHERE team_id = ? AND user_id = ? AND role = 'contributor'`, team, o.member.id) != 1 {
		t.Fatalf("an existing member's role became %q", role)
	}
}
