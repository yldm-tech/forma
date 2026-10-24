package invite

import (
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// forma2's getInvitesByOrganizationId: every pending invite, expired ones included, 30 a page when a page is asked for, to the organization's product members.
func TestListInvites(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	o := e.organization()
	expired, _ := e.seedInvite(o.id, o.owner.id, "late@example.com", "member", -time.Hour)
	for i := range 30 {
		e.seedInvite(o.id, o.owner.id, "p"+strconv.Itoa(i)+"@example.com", "member", inviteLifetime)
	}
	other := e.organization()
	e.seedInvite(other.id, other.owner.id, "theirs@example.com", "member", inviteLifetime)
	e.drain()

	all := e.do(o.member, http.MethodGet, invitePath(o), nil)
	expect(t, all, http.StatusOK, "")
	var list []map[string]any
	decode(t, all, &list)
	if len(list) != 31 || list[0]["id"] != expired.String() || list[0]["email"] != "late@example.com" || list[0]["role"] != "member" || list[0]["name"] != "Invitee" || list[0]["expiresAt"] == nil || list[0]["createdAt"] == nil {
		t.Fatalf("%d invites, first %v", len(list), list[0])
	}
	var page []map[string]any
	decode(t, e.do(o.owner, http.MethodGet, invitePath(o)+"?page=1", nil), &page)
	if len(page) != 30 {
		t.Fatalf("page 1 has %d invites", len(page))
	}
	decode(t, e.do(o.owner, http.MethodGet, invitePath(o)+"?page=2", nil), &page)
	if len(page) != 1 {
		t.Fatalf("page 2 has %d invites", len(page))
	}
	// forma2 paged only when a page was given; 0 is no page.
	decode(t, e.do(o.owner, http.MethodGet, invitePath(o)+"?page=0", nil), &page)
	if len(page) != 31 {
		t.Fatalf("page 0 has %d invites, want all", len(page))
	}
	expect(t, e.do(o.owner, http.MethodGet, invitePath(o)+"?page=-1", nil), http.StatusBadRequest, "validation_error")
	expect(t, e.do(o.billing, http.MethodGet, invitePath(o), nil), http.StatusForbidden, "forbidden")
	expect(t, e.do(other.owner, http.MethodGet, invitePath(o), nil), http.StatusForbidden, "forbidden")
}

// forma2's deleteInviteAction: the organization comes from the invite, the user-management gates apply, the invite and its links are gone, and the audit entry keeps the role.
func TestDeleteInvite(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(c *config.Config) { c.InviteDisabled = true })
	o := e.organization()
	id, token := e.seedInvite(o.id, o.owner.id, "grace@example.com", "manager", inviteLifetime)
	e.drain()

	expect(t, e.do(o.member, http.MethodDelete, "/invites/"+id.String(), nil), http.StatusForbidden, "operation_not_allowed")
	expect(t, e.do(e.organization().owner, http.MethodDelete, "/invites/"+id.String(), nil), http.StatusForbidden, "")
	expect(t, e.do(o.manager, http.MethodDelete, "/invites/"+id.String(), nil), http.StatusNoContent, "")
	if e.count(`SELECT count(*) FROM invites WHERE id = ?`, id)+e.count(`SELECT count(*) FROM invite_tokens WHERE invite_id = ?`, id) != 0 {
		t.Fatal("the invite or its link survived")
	}
	if ins := e.svc.mustInspect(t, token); ins.Status != StatusNotFound {
		t.Fatalf("a revoked link inspects as %+v", ins)
	}
	missing := uuid.New()
	expect(t, e.do(o.owner, http.MethodDelete, "/invites/"+missing.String(), nil), http.StatusNotFound, "not_found", "invite with ID "+missing.String()+" not found")
	deleted := e.auditEvents(audit.ActionDeleted, audit.TargetInvite)
	var success map[string]any
	for _, ev := range deleted {
		if ev["status"] == "success" {
			success = ev
		}
	}
	if success == nil || success["changes"].(map[string]any)["role"] != "manager" || success["organizationId"] != o.id.String() {
		t.Fatalf("deleted/invite events %v", deleted)
	}
}

// forma2's resendInviteAction: a fresh link mailed and seven more days; here the earlier links stop working.
func TestResendInvite(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	o := e.organization()
	id, old := e.seedInvite(o.id, o.owner.id, "grace@example.com", "member", time.Hour)
	other := e.organization()
	e.drain()
	path := invitePath(o) + "/" + id.String() + "/resend"

	r := e.do(o.manager, http.MethodPost, path, nil)
	expect(t, r, http.StatusOK, "")
	if r.json()["email"] != "grace@example.com" {
		t.Fatalf("resend answered %s", r.Body.String())
	}
	var days float64
	e.db.Raw(`SELECT extract(epoch FROM expires_at - now()) / 86400 FROM invites WHERE id = ?`, id).Scan(&days)
	if days < 6.99 {
		t.Fatalf("expiry %v days away, want seven", days)
	}
	fresh := e.lastLink("grace@example.com")
	if e.svc.mustInspect(t, old).Status != StatusNotFound || e.svc.mustInspect(t, fresh).Status != StatusValid {
		t.Fatal("resend did not replace the link")
	}
	// The mail names the invite's creator as the inviter.
	if msgs := e.sent("grace@example.com"); len(msgs) != 1 || !contains(msgs[0].HTML, "Owner") {
		t.Fatalf("resent mail %+v", msgs)
	}

	expect(t, e.do(other.owner, http.MethodPost, invitePath(other)+"/"+id.String()+"/resend", nil), http.StatusBadRequest, "validation_error", "Invite does not belong to the organization")
	expect(t, e.do(o.owner, http.MethodPost, invitePath(o)+"/"+uuid.NewString()+"/resend", nil), http.StatusNotFound, "not_found")
	expect(t, e.do(o.member, http.MethodPost, path, nil), http.StatusForbidden, "operation_not_allowed")

	// A resend whose mail fails revokes nothing: the link already mailed and a copied share link keep working.
	shared := e.do(o.owner, http.MethodPost, "/invites/"+id.String()+"/link", nil)
	expect(t, shared, http.StatusOK, "")
	e.mail.Err = errors.New("smtp is down")
	expect(t, e.do(o.owner, http.MethodPost, path, nil), http.StatusBadRequest, CodeEmailSendFailed)
	if e.svc.mustInspect(t, fresh).Status != StatusValid || e.svc.mustInspect(t, shared.json()["inviteToken"].(string)).Status != StatusValid {
		t.Fatal("a failed resend revoked the links the invitee already had")
	}
	e.mail.Err = nil
	expect(t, e.do(o.owner, http.MethodPost, path, nil), http.StatusOK, "")
	if e.svc.mustInspect(t, fresh).Status != StatusNotFound || e.svc.mustInspect(t, shared.json()["inviteToken"].(string)).Status != StatusNotFound ||
		e.svc.mustInspect(t, e.lastLink("grace@example.com")).Status != StatusValid {
		t.Fatal("a successful resend did not replace every earlier link")
	}

	off := newEnv(t, func(c *config.Config) { c.InviteDisabled = true })
	o = off.organization()
	id, _ = off.seedInvite(o.id, o.owner.id, "grace@example.com", "member", time.Hour)
	off.drain()
	expect(t, off.do(o.owner, http.MethodPost, invitePath(o)+"/"+id.String()+"/resend", nil), http.StatusForbidden, "operation_not_allowed", "Invite are disabled")
}

// forma2's createInviteTokenAction: a link to share, seven more days, the mailed link still working; allowed while invites are disabled.
func TestShareLink(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(c *config.Config) { c.InviteDisabled = true })
	o := e.organization()
	id, mailed := e.seedInvite(o.id, o.owner.id, "grace@example.com", "member", time.Hour)
	e.drain()

	r := e.do(o.owner, http.MethodPost, "/invites/"+id.String()+"/link", nil)
	expect(t, r, http.StatusOK, "")
	token := r.json()["inviteToken"].(string)
	if r.json()["inviteUrl"] != webApp+"/invite?token="+token {
		t.Fatalf("link %s", r.Body.String())
	}
	if e.svc.mustInspect(t, token).Status != StatusValid || e.svc.mustInspect(t, mailed).Status != StatusValid {
		t.Fatal("the shared and the mailed link should both open the invite")
	}
	var days float64
	e.db.Raw(`SELECT extract(epoch FROM expires_at - now()) / 86400 FROM invites WHERE id = ?`, id).Scan(&days)
	if days < 6.99 {
		t.Fatalf("expiry %v days away, want seven", days)
	}
	ev := e.auditEvents(audit.ActionUpdated, audit.TargetInvite)
	if len(ev) != 1 || ev[0]["status"] != "success" {
		t.Fatalf("updated/invite events %v", ev)
	}
	expect(t, e.do(o.member, http.MethodPost, "/invites/"+id.String()+"/link", nil), http.StatusForbidden, "operation_not_allowed")
	expect(t, e.do(o.owner, http.MethodPost, "/invites/"+uuid.NewString()+"/link", nil), http.StatusNotFound, "not_found")
}

// forma2's updateInviteAction: the role an invite grants, with the billing and manager rules, audited old and new.
func TestUpdateInviteRole(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	o := e.organization()
	id, _ := e.seedInvite(o.id, o.owner.id, "grace@example.com", "member", inviteLifetime)
	e.drain()
	path := "/invites/" + id.String()

	expect(t, e.do(o.owner, http.MethodPatch, path, map[string]string{"role": "owner"}), http.StatusNoContent, "")
	var role string
	e.db.Raw(`SELECT role FROM invites WHERE id = ?`, id).Scan(&role)
	if role != "owner" {
		t.Fatalf("role %q", role)
	}
	ev := e.auditEvents(audit.ActionUpdated, audit.TargetInvite)
	changes, _ := ev[len(ev)-1]["changes"].(map[string]any)
	if changes == nil || changes["role"] == nil {
		t.Fatalf("the role change left no trace: %v", ev)
	}

	expect(t, e.do(o.manager, http.MethodPatch, path, map[string]string{"role": "manager"}), http.StatusForbidden, "operation_not_allowed", "Managers can only invite members")
	expect(t, e.do(o.manager, http.MethodPatch, path, map[string]string{"role": "member"}), http.StatusNoContent, "")
	expect(t, e.do(o.owner, http.MethodPatch, path, map[string]string{"role": "billing"}), http.StatusBadRequest, "validation_error", "Billing role is not allowed")
	expect(t, e.do(o.owner, http.MethodPatch, path, map[string]string{"role": "admin"}), http.StatusBadRequest, "validation_error")
	expect(t, e.do(e.organization().owner, http.MethodPatch, path, map[string]string{"role": "member"}), http.StatusForbidden, "forbidden", "User not a member of this organization")
	expect(t, e.do(o.owner, http.MethodPatch, "/invites/"+uuid.NewString(), map[string]string{"role": "member"}), http.StatusNotFound, "not_found")
}

// The members page's view: organization-wide user management as USER_MANAGEMENT_MINIMUM_ROLE allows, the teams the caller administers, and the instance switches.
func TestAccess(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	o := e.organization()
	team := e.team(o.id, "Mine")
	e.teamMember(team, o.member, "admin")
	e.drain()
	path := "/organizations/" + o.id.String() + "/user-management"

	r := e.do(o.manager, http.MethodGet, path, nil)
	expect(t, r, http.StatusOK, "")
	if r.Body.String() != `{"canManageUsers":true,"adminTeamIds":[],"inviteDisabled":false,"billingRoleAllowed":false}` {
		t.Fatalf("manager access %s", r.Body.String())
	}
	r = e.do(o.member, http.MethodGet, path, nil)
	if r.Body.String() != `{"canManageUsers":false,"adminTeamIds":["`+team.String()+`"],"inviteDisabled":false,"billingRoleAllowed":false}` {
		t.Fatalf("team admin access %s", r.Body.String())
	}
	expect(t, e.do(o.billing, http.MethodGet, path, nil), http.StatusForbidden, "forbidden")
}
