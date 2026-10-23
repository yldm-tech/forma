package auth

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics/analyticstest"
)

// TestSignUpAndSignInEvents: a password sign-up identifies the person by id and email domain, reports the organization it created and the sign-up itself, and a later sign-in reports how many organizations the user is in. No event carries the address or the name.
func TestSignUpAndSignInEvents(t *testing.T) {
	rec := analyticstest.Start(t)
	e := newEnv(t)
	b := e.browser()
	expectStatus(t, b.post("/auth/signup", signupBody("Ada@Example.com")), http.StatusOK, "")
	acc, err := e.svc.repo.accountByEmail(t.Context(), "ada@example.com")
	if err != nil || acc == nil {
		t.Fatalf("account: %v, %v", acc, err)
	}
	user := acc.ID.String()
	var org struct{ ID, WorkspaceID string }
	e.db.Raw(`SELECT o.id::text AS id, w.id::text AS workspace_id FROM organizations o JOIN workspaces w ON w.organization_id = o.id JOIN memberships m ON m.organization_id = o.id WHERE m.user_id = ?`, acc.ID).Scan(&org)

	identify := rec.Events(t, "$identify", 1)[0]
	if identify.DistinctID != user || identify.Set["email_domain"] != "example.com" || identify.Set["email"] != nil || identify.Set["name"] != nil {
		t.Errorf("$identify = %+v", identify)
	}
	created := rec.Events(t, "organization_created", 1)[0]
	if created.DistinctID != user || created.Properties["organization_id"] != org.ID || created.Properties["is_first_org"] != true {
		t.Errorf("organization_created = %+v", created)
	}
	if ws := rec.Events(t, "workspace_created", 1)[0]; ws.Properties["workspace_id"] != org.WorkspaceID {
		t.Errorf("workspace_created = %+v", ws)
	}
	signedUp := rec.Events(t, "user_signed_up", 1)[0]
	p := signedUp.Properties
	if signedUp.DistinctID != user || p["auth_provider"] != "credentials" || p["signup_source"] != "direct" || p["email_domain"] != "example.com" || p["invite_organization_id"] != org.ID {
		t.Errorf("user_signed_up = %+v", signedUp)
	}

	e.exec(`UPDATE users SET email_verified = true WHERE id = ?`, acc.ID)
	e.browser().signIn("ada@example.com", strongPassword)
	signedIn := rec.Events(t, "user_signed_in", 1)[0]
	if signedIn.DistinctID != user || signedIn.Properties["auth_provider"] != "password" || signedIn.Properties["organization_count"] != float64(1) || signedIn.Properties["is_first_login_today"] != true {
		t.Errorf("user_signed_in = %+v", signedIn)
	}
	for _, m := range rec.All() {
		b, _ := json.Marshal(m)
		if strings.Contains(strings.ToLower(string(b)), "ada@example.com") || strings.Contains(string(b), "Ada Lovelace\"") {
			t.Errorf("%s carries the address or the name: %s", m.Event, b)
		}
	}
}
