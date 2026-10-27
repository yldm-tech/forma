package auth

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz/authztest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// withAuthz gives the service a SpiceDB and the projection delivery, as the server has; the other tests run without one. Nothing delivers the outbox in the background: what the requests do not deliver themselves stays pending, as it would behind a worker that is minutes late.
func (e *env) withAuthz() *authztest.Env {
	e.t.Helper()
	a := authztest.New(e.t, e.cfg, e.db)
	e.svc.delivery = a.Delivery()
	return a
}

func (e *env) pendingProjection() int64 {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM authz_projection_outbox WHERE processed_at IS NULL`)
}

// Deleting an account revokes the user's relationships and every one of the organizations deleted with it: a relationship for each member, team, workspace and API key. All of them reach SpiceDB before the request answers; left to the worker they would pass the freshness window and stop every authorization check in the deployment.
func TestDeleteAccountIsProjectedBeforeItAnswers(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	a := e.withAuthz()
	id := e.seedUser("ada@example.com", strongPassword, userOpts{})
	other := e.seedUser("grace@example.com", strongPassword, userOpts{})
	soleOwned := e.orgWith(map[uuid.UUID]string{id: "owner", other: "member"})
	shared := e.orgWith(map[uuid.UUID]string{id: "owner", other: "owner"})
	team := e.insertID(`INSERT INTO teams (organization_id, name) VALUES (?, 'Team') RETURNING id`, soleOwned)
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'contributor')`, team, other)
	ws := e.insertID(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Workspace') RETURNING id`, soleOwned)
	e.exec(`INSERT INTO workspace_teams (workspace_id, team_id, permission) VALUES (?, ?, 'read')`, ws, team)
	key := e.insertID(`INSERT INTO api_keys (organization_id, created_by, label, hashed_key, lookup_hash) VALUES (?, ?, 'key', 'x', ?) RETURNING id`, soleOwned, id, uuid.NewString())
	e.exec(`INSERT INTO api_key_workspaces (api_key_id, workspace_id, permission) VALUES (?, ?, 'read')`, key, ws)
	sharedTeam := e.insertID(`INSERT INTO teams (organization_id, name) VALUES (?, 'Team') RETURNING id`, shared)
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'admin')`, sharedTeam, id)
	a.Deliver(t)

	b := e.browser()
	b.signIn("ada@example.com", strongPassword)
	expectStatus(t, b.post("/auth/account/delete", map[string]string{"password": strongPassword}), http.StatusOK, "")
	if e.count(`SELECT count(*) FROM organizations WHERE id = ?`, soleOwned) != 0 {
		t.Fatal("the sole-owned organization survived")
	}
	if n := e.pendingProjection(); n != 0 {
		t.Fatalf("%d events left for the worker after the account's deletion", n)
	}
}

// A sign-up's own organization, its owner and its first workspace are in SpiceDB when the sign-up answers, and so is an SSO sign-up's default organization when the sign-up created it.
func TestSignupsAreProjectedBeforeTheyAnswer(t *testing.T) {
	t.Parallel()
	t.Run("credentials", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		e.withAuthz()
		expectStatus(t, e.browser().post("/auth/signup", signupBody("ada@example.com")), http.StatusOK, "")
		if n := e.count(`SELECT count(*) FROM memberships m JOIN users u ON u.id = m.user_id WHERE u.email = 'ada@example.com' AND m.role = 'owner'`); n != 1 {
			t.Fatalf("%d owned organizations, want 1", n)
		}
		if n := e.pendingProjection(); n != 0 {
			t.Fatalf("%d events left for the worker after the sign-up", n)
		}
	})
	t.Run("sso default organization", func(t *testing.T) {
		t.Parallel()
		orgID := uuid.New()
		e, fake := ssoEnv(t, providerGoogle, func(cfg *config.Config) { cfg.SSO.DefaultOrganizationID = orgID.String() })
		e.withAuthz()
		fake.set(func(f *fakeIdP) { f.profile = googleProfile("g-1", "ada@acme.example") })
		expectRedirect(t, e.browser().ssoSignIn(fake, "google", ""), appBase+"/")
		if e.count(`SELECT count(*) FROM workspaces WHERE organization_id = ?`, orgID) != 1 {
			t.Fatal("the default organization was not created with its workspace")
		}
		if n := e.pendingProjection(); n != 0 {
			t.Fatalf("%d events left for the worker after the SSO sign-up", n)
		}
	})
}
