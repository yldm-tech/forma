package auth

import (
	"net/http"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
)

func (b *browser) patch(path string, body any) response {
	return b.do(http.MethodPatch, path, body)
}

// forma2's updateUserAction for the name and language: trimmed ZUserName, ZUserLocale, and an SSO user's name owned by the identity provider.
func TestUpdateProfile(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{})
	b := e.sessionFor(id)

	expectStatus(t, e.browser().patch("/auth/me", map[string]string{"name": "Grace"}), http.StatusUnauthorized, apperr.CodeNotAuthenticated)

	for _, body := range []map[string]any{
		{"name": "   "},
		{"name": "Ada <script>"},
		{"locale": "xx-XX"},
		{"locale": ""},
	} {
		expectStatus(t, b.patch("/auth/me", body), http.StatusBadRequest, apperr.CodeValidation)
	}

	r := b.patch("/auth/me", map[string]string{"name": "  Grace Hopper ", "locale": "ja-JP"})
	expectStatus(t, r, http.StatusOK, "")
	if got := r.json(); got["name"] != "Grace Hopper" || got["locale"] != "ja-JP" || got["email"] != "ada@example.com" {
		t.Fatalf("body %s", r.Body.String())
	}
	acc, err := e.svc.repo.accountByID(t.Context(), id)
	if err != nil || acc.Name != "Grace Hopper" || acc.Locale != "ja-JP" {
		t.Fatalf("stored %+v, %v", acc, err)
	}
	events := e.auditEvents(audit.ActionUpdated)
	if len(events) != 1 || field(events[0], "changes", "name") == nil || field(events[0], "changes", "locale") == nil {
		t.Fatalf("audit %+v", events)
	}

	// Nothing to change writes nothing.
	expectStatus(t, b.patch("/auth/me", map[string]string{"name": "Grace Hopper"}), http.StatusOK, "")
	if n := len(e.auditEvents(audit.ActionUpdated)); n != 1 {
		t.Fatalf("%d audit events after a no-op", n)
	}
}

func TestUpdateProfileSSOUser(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.seedUser("sso@example.com", "", userOpts{noPassword: true, sso: true})
	b := e.sessionFor(id)

	expectStatus(t, b.patch("/auth/me", map[string]string{"name": "Someone Else"}), http.StatusForbidden, apperr.CodeOperationNotAllowed)
	// The current name echoed back, with a language change, is allowed.
	r := b.patch("/auth/me", map[string]string{"name": "Ada Lovelace", "locale": "zh-Hant-TW"})
	expectStatus(t, r, http.StatusOK, "")
	if r.json()["locale"] != "zh-Hant-TW" {
		t.Fatalf("body %s", r.Body.String())
	}
}
