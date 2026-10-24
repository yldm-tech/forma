package invite

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
)

func invitePath(o org) string { return "/organizations/" + o.id.String() + "/invites" }

func body(email, role string, teams ...uuid.UUID) map[string]any {
	if teams == nil {
		teams = []uuid.UUID{}
	}
	return map[string]any{"email": email, "name": "Grace Hopper", "role": role, "teamIds": teams}
}

// forma2's inviteUserAction and inviteUser for owners and managers: the invite is stored lowercased with its teams and a link, mailed in the inviter's language, and audited; the duplicate, membership and team checks refuse with forma2's messages.
func TestCreateInvite(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	o := e.organization()
	inviter := e.join(o.id, e.userWith("Olivia Owner", "olivia@example.com", "ja-JP"), "owner")
	team := e.team(o.id, "Design")
	foreignTeam := e.team(e.insert(`INSERT INTO organizations (name) VALUES ('Other') RETURNING id`), "Theirs")
	known := e.userWith("Known", "known@example.com", "en-US")
	e.drain()

	r := e.do(inviter, http.MethodPost, invitePath(o), body("Grace@Example.COM", "member", team))
	expect(t, r, http.StatusCreated, "")
	id := uuid.MustParse(r.json()["id"].(string))
	var stored struct {
		Email      string
		Name       string
		Role       string
		CreatorID  uuid.UUID
		AcceptorID *uuid.UUID
		Days       float64
	}
	e.db.Raw(`SELECT email, name, role, creator_id, acceptor_id, extract(epoch FROM expires_at - created_at) / 86400 AS days FROM invites WHERE id = ? AND organization_id = ?`, id, o.id).Scan(&stored)
	if stored.Email != "grace@example.com" || stored.Name != "Grace Hopper" || stored.Role != "member" || stored.CreatorID != inviter.id || stored.AcceptorID != nil || stored.Days < 6.99 || stored.Days > 7.01 {
		t.Fatalf("stored invite %+v", stored)
	}
	if e.count(`SELECT count(*) FROM invite_teams WHERE invite_id = ? AND team_id = ?`, id, team) != 1 || e.count(`SELECT count(*) FROM invite_tokens WHERE invite_id = ?`, id) != 1 {
		t.Fatal("the invite's teams or link were not stored")
	}
	msgs := e.sent("grace@example.com")
	jaSubject, _, _ := e.svc.mailer.Render("ja-JP", mailer.Invite{})
	if len(msgs) != 1 || msgs[0].Subject != jaSubject || !strings.Contains(msgs[0].HTML, "Olivia Owner") || !strings.Contains(msgs[0].HTML, webApp+"/app/invite?token=") {
		t.Fatalf("invite mail %+v", msgs)
	}
	if ins := e.svc.mustInspect(t, e.lastLink("grace@example.com")); ins.Status != StatusValid || ins.Email != "grace@example.com" {
		t.Fatalf("the mailed link inspects as %+v", ins)
	}
	created := e.auditEvents(audit.ActionCreated, audit.TargetInvite)
	if len(created) != 1 || created[0]["status"] != "success" || created[0]["organizationId"] != o.id.String() || created[0]["target"].(map[string]any)["id"] != id.String() {
		t.Fatalf("created/invite events %v", created)
	}
	if strings.Contains(e.logs.String(), "grace@example.com") {
		t.Fatal("the invitee's address reached the logs")
	}

	// An existing account is linked as the acceptor.
	expect(t, e.do(o.manager, http.MethodPost, invitePath(o), body("KNOWN@example.com", "member")), http.StatusCreated, "")
	if e.count(`SELECT count(*) FROM invites WHERE organization_id = ? AND acceptor_id = ?`, o.id, known.id) != 1 {
		t.Fatal("the existing account was not linked")
	}

	refusals := map[string]struct {
		by      user
		body    map[string]any
		status  int
		code    string
		message string
	}{
		"the same address in another case":      {inviter, body("GRACE@example.com", "member"), http.StatusBadRequest, CodeInviteExists, "Invite already exists"},
		"a member of the organization":          {inviter, body(strings.ToUpper(o.member.email), "member"), http.StatusBadRequest, CodeUserAlreadyMember, "User is already a member of this organization"},
		"a team named twice":                    {inviter, body("ada@example.com", "member", team, team), http.StatusBadRequest, CodeDuplicateTeamIDs, "teamIds must be unique"},
		"another organization's team":           {inviter, body("ada@example.com", "member", foreignTeam), http.StatusBadRequest, CodeInvalidTeamIDs, "Invalid teamIds"},
		"the billing role on a self-hosted one": {inviter, body("ada@example.com", "billing"), http.StatusBadRequest, "validation_error", "Billing role is not allowed"},
		"a manager inviting a manager":          {o.manager, body("ada@example.com", "manager"), http.StatusForbidden, "operation_not_allowed", "Managers can only invite users as members"},
		"a member with no admin team":           {o.member, body("ada@example.com", "member"), http.StatusForbidden, "forbidden", "Not authorized to invite members"},
		"a billing user":                        {o.billing, body("ada@example.com", "member"), http.StatusForbidden, "forbidden", "Not authorized to invite members"},
		"someone outside the organization":      {e.user("Outsider"), body("ada@example.com", "member"), http.StatusForbidden, "forbidden", "User not a member of this organization"},
		"an address that is not one":            {inviter, body("not-an-email", "member"), http.StatusBadRequest, "validation_error", ""},
		"no name":                               {inviter, map[string]any{"email": "ada@example.com", "name": "  ", "role": "member", "teamIds": []any{}}, http.StatusBadRequest, "validation_error", ""},
		"an unknown role":                       {inviter, body("ada@example.com", "admin"), http.StatusBadRequest, "validation_error", ""},
		"more than 100 teams":                   {inviter, body("ada@example.com", "member", make([]uuid.UUID, 101)...), http.StatusBadRequest, "validation_error", ""},
	}
	for name, tc := range refusals {
		t.Run(name, func(t *testing.T) {
			r := e.do(tc.by, http.MethodPost, invitePath(o), tc.body)
			if tc.message == "" {
				expect(t, r, tc.status, tc.code)
			} else {
				expect(t, r, tc.status, tc.code, tc.message)
			}
		})
	}
	if n := e.count(`SELECT count(*) FROM invites WHERE organization_id = ?`, o.id); n != 2 {
		t.Fatalf("%d invites stored, want only the two accepted ones", n)
	}
	expect(t, e.do(user{}, http.MethodPost, invitePath(o), body("ada@example.com", "member")), http.StatusUnauthorized, "not_authenticated")
}

// Delivery is best effort: a failing mail server neither fails the invite nor strands it.
func TestCreateInviteSurvivesAMailFailure(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	o := e.organization()
	e.drain()
	e.mail.Err = errors.New("smtp is down")
	expect(t, e.do(o.owner, http.MethodPost, invitePath(o), body("grace@example.com", "member")), http.StatusCreated, "")
	if e.count(`SELECT count(*) FROM invites WHERE organization_id = ?`, o.id) != 1 {
		t.Fatal("the invite was not kept")
	}
}

// Forma Cloud allows the billing role; INVITE_DISABLED refuses every way of sending one.
func TestCreateInviteSwitches(t *testing.T) {
	t.Parallel()
	cloud := newEnv(t, func(c *config.Config) { c.IsFormaCloud = true })
	o := cloud.organization()
	cloud.drain()
	expect(t, cloud.do(o.owner, http.MethodPost, invitePath(o), body("grace@example.com", "billing")), http.StatusCreated, "")

	off := newEnv(t, func(c *config.Config) { c.InviteDisabled = true })
	o = off.organization()
	off.drain()
	expect(t, off.do(o.owner, http.MethodPost, invitePath(o), body("grace@example.com", "member")), http.StatusForbidden, "operation_not_allowed", "Invite disabled")
	expect(t, off.do(o.owner, http.MethodPost, invitePath(o)+"/bulk", map[string]any{"invitees": []any{body("grace@example.com", "member")}}), http.StatusForbidden, "operation_not_allowed", "Invite disabled")
	expect(t, off.do(o.owner, http.MethodPost, "/organizations/"+o.id.String()+"/setup-invites", map[string]any{"email": "grace@example.com", "name": "Grace"}), http.StatusForbidden, "operation_not_allowed", "Invite disabled")
	if off.count(`SELECT count(*) FROM invites`) != 0 || len(off.mail.Messages()) != 0 {
		t.Fatal("a disabled instance sent an invite")
	}
}

// forma2's team-admin path: a team admin who is a plain member invites members into the teams they administer, at least one, and nothing else.
func TestTeamAdminInvite(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	o := e.organization()
	mine, sibling := e.team(o.id, "Mine"), e.team(o.id, "Sibling")
	e.teamMember(mine, o.member, "admin")
	e.teamMember(sibling, o.member, "contributor")
	e.drain()

	expect(t, e.do(o.member, http.MethodPost, invitePath(o), body("grace@example.com", "member", mine)), http.StatusCreated, "")
	expect(t, e.do(o.member, http.MethodPost, invitePath(o), body("ada@example.com", "member", mine, sibling)), http.StatusForbidden, "operation_not_allowed", "Team admins can only add users to teams where they are admin")
	expect(t, e.do(o.member, http.MethodPost, invitePath(o), body("ada@example.com", "manager", mine)), http.StatusForbidden, "operation_not_allowed", "Team admins can only invite users as members")
	expect(t, e.do(o.member, http.MethodPost, invitePath(o), body("ada@example.com", "member")), http.StatusBadRequest, "validation_error", "Team admins must add invited users to at least one team")
	// A team admin is not an organization manager: bulk invites stay out of reach.
	expect(t, e.do(o.member, http.MethodPost, invitePath(o)+"/bulk", map[string]any{"invitees": []any{body("ada@example.com", "member", mine)}}), http.StatusForbidden, "operation_not_allowed", "User management is not allowed for your role")
}

// USER_MANAGEMENT_MINIMUM_ROLE governs every organization-level invite action, and like forma2 not a team admin's invites into their own teams.
func TestUserManagementMinimumRole(t *testing.T) {
	t.Parallel()
	owners := newEnv(t, func(c *config.Config) { c.UserManagementMinimumRole = "owner" })
	o := owners.organization()
	inviteID, _ := owners.seedInvite(o.id, o.owner.id, "pending@example.com", "member", inviteLifetime)
	owners.drain()
	notAllowed := "User management is not allowed for your role"
	for _, req := range []struct{ method, path string }{
		{http.MethodPost, invitePath(o)},
		{http.MethodPost, invitePath(o) + "/bulk"},
		{http.MethodDelete, "/invites/" + inviteID.String()},
		{http.MethodPost, "/invites/" + inviteID.String() + "/link"},
		{http.MethodPatch, "/invites/" + inviteID.String()},
		{http.MethodPost, invitePath(o) + "/" + inviteID.String() + "/resend"},
	} {
		payload := map[string]any{"email": "grace@example.com", "name": "Grace", "role": "member", "teamIds": []any{}, "invitees": []any{body("grace@example.com", "member")}}
		expect(t, owners.do(o.manager, req.method, req.path, payload), http.StatusForbidden, "operation_not_allowed", notAllowed)
	}
	expect(t, owners.do(o.owner, http.MethodPost, invitePath(o), body("grace@example.com", "manager")), http.StatusCreated, "")

	disabled := newEnv(t, func(c *config.Config) { c.UserManagementMinimumRole = "disabled" })
	o = disabled.organization()
	team := disabled.team(o.id, "Mine")
	disabled.teamMember(team, o.member, "admin")
	disabled.drain()
	expect(t, disabled.do(o.owner, http.MethodPost, invitePath(o), body("grace@example.com", "member")), http.StatusForbidden, "operation_not_allowed", notAllowed)
	expect(t, disabled.do(o.member, http.MethodPost, invitePath(o), body("grace@example.com", "member", team)), http.StatusCreated, "")
}

// forma2's bulkInviteUsersAction: per-invitee outcomes with forma2's failure reasons, addresses lowercased, the whole batch validated first.
func TestBulkInvite(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	o := e.organization()
	team := e.team(o.id, "Design")
	e.drain()
	path := invitePath(o) + "/bulk"

	rows := []any{
		body("Grace@Example.com", "member", team),
		body("grace@example.com", "member"),
		body(o.member.email, "member"),
		map[string]any{"email": "ada@example.com", "name": "Ada", "role": "member", "teamIds": []string{"not-a-team"}},
		body("alan@example.com", "member", team, team),
		body("barbara@example.com", "owner"),
	}
	r := e.do(o.owner, http.MethodPost, path, map[string]any{"invitees": rows})
	expect(t, r, http.StatusOK, "")
	want := `{"results":[{"email":"grace@example.com","success":true},{"email":"grace@example.com","success":false,"failureReason":"invite_already_exists"},` +
		`{"email":"` + o.member.email + `","success":false,"failureReason":"user_already_member"},{"email":"ada@example.com","success":false,"failureReason":"invalid_team_ids"},` +
		`{"email":"alan@example.com","success":false,"failureReason":"duplicate_team_ids"},{"email":"barbara@example.com","success":true}]}`
	if r.Body.String() != want {
		t.Fatalf("bulk results\n%s\nwant\n%s", r.Body.String(), want)
	}
	if len(e.sent("grace@example.com")) != 1 || len(e.sent("barbara@example.com")) != 1 || len(e.mail.Messages()) != 2 {
		t.Fatal("bulk mails do not match the created invites")
	}
	ev := e.auditEvents(audit.ActionCreated, audit.TargetInvite)
	if len(ev) != 1 || ev[0]["changes"].(map[string]any)["invitedCount"] != float64(2) || ev[0]["changes"].(map[string]any)["totalCount"] != float64(6) {
		t.Fatalf("bulk audit %v", ev)
	}

	many := make([]any, maxBulkInvitees+1)
	for i := range many {
		many[i] = body("p"+uuid.NewString()[:8]+"@example.com", "member")
	}
	refusals := map[string]struct {
		by      user
		rows    []any
		status  int
		code    string
		message string
	}{
		"no invitees":                 {o.owner, []any{}, http.StatusBadRequest, "validation_error", "No invitees provided"},
		"more than 500":               {o.owner, many, http.StatusBadRequest, "validation_error", "A bulk invite is limited to 500 members at a time"},
		"a manager inviting an owner": {o.manager, []any{body("x@example.com", "member"), body("y@example.com", "owner")}, http.StatusForbidden, "operation_not_allowed", "Managers can only invite users as members"},
		"the billing role":            {o.owner, []any{body("x@example.com", "billing")}, http.StatusBadRequest, "validation_error", "Billing role is not allowed"},
		"an invalid address":          {o.owner, []any{body("x@example.com", "member"), body("nope", "member")}, http.StatusBadRequest, "validation_error", "Invalid input"},
		"a name outside ZUserName":    {o.owner, []any{map[string]any{"email": "x@example.com", "name": "<script>", "role": "member"}}, http.StatusBadRequest, "validation_error", "Invalid input"},
		"a billing user":              {o.billing, []any{body("x@example.com", "member")}, http.StatusForbidden, "operation_not_allowed", "User management is not allowed for your role"},
	}
	for name, tc := range refusals {
		t.Run(name, func(t *testing.T) {
			expect(t, e.do(tc.by, http.MethodPost, path, map[string]any{"invitees": tc.rows}), tc.status, tc.code, tc.message)
		})
	}
	if e.count(`SELECT count(*) FROM invites WHERE organization_id = ?`, o.id) != 2 {
		t.Fatal("a refused bulk invite stored invites")
	}
}

// forma2's onboarding invite: owners only, always an owner invite with no teams, sharing the invite checks.
func TestSetupInvite(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	o := e.organization()
	e.drain()
	path := "/organizations/" + o.id.String() + "/setup-invites"

	expect(t, e.do(o.manager, http.MethodPost, path, map[string]any{"email": "grace@example.com", "name": "Grace"}), http.StatusForbidden, "forbidden")
	r := e.do(o.owner, http.MethodPost, path, map[string]any{"email": "Grace@example.com", "name": " Grace Hopper "})
	expect(t, r, http.StatusCreated, "")
	var role, name string
	if err := e.db.Raw(`SELECT role, name FROM invites WHERE id = ? AND email = 'grace@example.com'`, r.json()["id"]).Row().Scan(&role, &name); err != nil {
		t.Fatal(err)
	}
	if role != "owner" || name != "Grace Hopper" || e.count(`SELECT count(*) FROM invite_teams`) != 0 || len(e.sent("grace@example.com")) != 1 {
		t.Fatalf("setup invite role %q name %q", role, name)
	}
	expect(t, e.do(o.owner, http.MethodPost, path, map[string]any{"email": "grace@example.com", "name": "Grace"}), http.StatusBadRequest, CodeInviteExists)
	expect(t, e.do(o.owner, http.MethodPost, path, map[string]any{"email": o.member.email, "name": "Member"}), http.StatusBadRequest, CodeUserAlreadyMember)
	expect(t, e.do(o.owner, http.MethodPost, path, map[string]any{"email": "ada@example.com", "name": "Ada <b>"}), http.StatusBadRequest, "validation_error")
}

// forma2's invite rate limit: one budget per organization per day, counted per recipient across the single, bulk, setup and resend paths; a batch larger than what is left is refused whole and consumes nothing.
func TestInviteRateLimit(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(c *config.Config) { c.RateLimitingDisabled = false; c.InviteRateLimitPer24Hours = 4 })
	o := e.organization()
	e.drain()

	expect(t, e.do(o.owner, http.MethodPost, invitePath(o)+"/bulk", map[string]any{"invitees": []any{body("a@example.com", "member"), body("b@example.com", "member")}}), http.StatusOK, "")
	r := e.do(o.owner, http.MethodPost, invitePath(o)+"/bulk", map[string]any{"invitees": []any{body("c@example.com", "member"), body("d@example.com", "member"), body("e@example.com", "member")}})
	expect(t, r, http.StatusTooManyRequests, "too_many_requests", "Maximum number of requests reached. Please try again later.")
	if r.Header().Get("Retry-After") == "" {
		t.Fatal("no Retry-After on a refused invite")
	}
	expect(t, e.do(o.owner, http.MethodPost, "/organizations/"+o.id.String()+"/setup-invites", map[string]any{"email": "c@example.com", "name": "C"}), http.StatusCreated, "")
	var inviteID uuid.UUID
	if err := e.db.Raw(`SELECT id FROM invites WHERE email = 'a@example.com'`).Row().Scan(&inviteID); err != nil {
		t.Fatal(err)
	}
	expect(t, e.do(o.owner, http.MethodPost, invitePath(o)+"/"+inviteID.String()+"/resend", nil), http.StatusOK, "")
	expect(t, e.do(o.owner, http.MethodPost, invitePath(o), body("d@example.com", "member")), http.StatusTooManyRequests, "too_many_requests")
	if e.count(`SELECT count(*) FROM invites`) != 3 {
		t.Fatal("a refused invite was stored")
	}

	// Another organization has its own budget.
	other := e.organization()
	e.drain()
	expect(t, e.do(other.owner, http.MethodPost, invitePath(other), body("d@example.com", "member")), http.StatusCreated, "")
}

// mustInspect is Inspect without a session.
func (s *Service) mustInspect(t *testing.T, token string) *Inspection {
	t.Helper()
	ins, err := s.Inspect(t.Context(), token, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ins
}
