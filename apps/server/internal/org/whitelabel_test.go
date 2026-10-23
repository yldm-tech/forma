package org

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
)

func (e *env) whitelabel(org uuid.UUID) string {
	e.t.Helper()
	var s string
	e.scalar(&s, `SELECT whitelabel::text FROM organizations WHERE id = ?`, org)
	return s
}

// lastAudit is the latest audit event, failing the test when there is none.
func (e *env) lastAudit() map[string]any {
	e.t.Helper()
	events := e.auditEvents()
	if len(events) == 0 {
		e.t.Fatal("no audit event")
	}
	return events[len(events)-1]
}

func TestWhitelabelEmailLogo(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	c := e.organization(owner, "Acme")
	path := orgPath(c.ID, "/whitelabel/email-logo")
	logo := "/storage/" + c.WorkspaceID.String() + "/public/logo--fid--abc.png"

	e.exec(`UPDATE organizations SET whitelabel = '{"faviconUrl": "/storage/x/public/f.ico"}' WHERE id = ?`, c.ID)
	e.ok(e.do(owner, http.MethodPut, path, map[string]string{"logoUrl": logo}), http.StatusNoContent, nil)
	if got := e.whitelabel(c.ID); got != `{"logoUrl": "`+logo+`", "faviconUrl": "/storage/x/public/f.ico"}` {
		t.Fatalf("whitelabel = %s", got)
	}
	ev := e.lastAudit()
	target := ev["target"].(map[string]any)
	changes, _ := ev["changes"].(map[string]any)
	if ev["action"] != "updated" || target["type"] != "organization" || target["id"] != c.ID.String() || ev["status"] != "success" || changes["logoUrl"] != logo {
		t.Fatalf("audit event %v", ev)
	}

	// forma2's ZStorageUrl: a script URL never reaches an email or a page.
	for _, bad := range []string{"javascript:alert(1)", "data:image/png;base64,AAAA", ""} {
		e.fails(e.do(owner, http.MethodPut, path, map[string]string{"logoUrl": bad}), http.StatusBadRequest, apperr.CodeValidation, "")
	}

	e.ok(e.do(owner, http.MethodDelete, path, nil), http.StatusNoContent, nil)
	if got := e.whitelabel(c.ID); got != `{"logoUrl": null, "faviconUrl": "/storage/x/public/f.ico"}` {
		t.Fatalf("whitelabel after removal = %s", got)
	}
	if changes, _ := e.lastAudit()["changes"].(map[string]any); changes["logoUrl"] != "" {
		t.Fatalf("removal audit %v", e.lastAudit())
	}
}

func TestWhitelabelFavicon(t *testing.T) {
	e := newEnv(t)
	owner, manager := e.user(), e.user()
	c := e.organization(owner, "Acme")
	e.member(c.ID, manager, RoleManager)
	path := orgPath(c.ID, "/whitelabel/favicon")

	e.ok(e.do(manager, http.MethodPut, path, map[string]string{"faviconUrl": "https://cdn.example.com/f.png"}), http.StatusNoContent, nil)
	if got := e.whitelabel(c.ID); got != `{"faviconUrl": "https://cdn.example.com/f.png"}` {
		t.Fatalf("whitelabel = %s", got)
	}
	if changes, _ := e.lastAudit()["changes"].(map[string]any); changes["faviconUrl"] != "https://cdn.example.com/f.png" {
		t.Fatalf("audit %v", e.lastAudit())
	}
	e.fails(e.do(manager, http.MethodPut, path, map[string]string{"faviconUrl": "vbscript:x"}), http.StatusBadRequest, apperr.CodeValidation, "")

	e.ok(e.do(manager, http.MethodDelete, path, nil), http.StatusNoContent, nil)
	if got := e.whitelabel(c.ID); got != `{"faviconUrl": null}` {
		t.Fatalf("whitelabel after removal = %s", got)
	}
}

// Every whitelabel write needs organization.manage: a member and a billing member are refused, and the refusal is audited as a failure.
func TestWhitelabelNeedsManage(t *testing.T) {
	e := newEnv(t)
	owner, member, billingMember := e.user(), e.user(), e.user()
	c := e.organization(owner, "Acme")
	e.member(c.ID, member, RoleMember)
	e.member(c.ID, billingMember, RoleBilling)
	for _, user := range []uuid.UUID{member, billingMember} {
		for _, req := range []struct {
			method, path string
			body         any
		}{
			{http.MethodPut, "/whitelabel/email-logo", map[string]string{"logoUrl": "/storage/a/public/l.png"}},
			{http.MethodDelete, "/whitelabel/email-logo", nil},
			{http.MethodPut, "/whitelabel/favicon", map[string]string{"faviconUrl": "/storage/a/public/f.png"}},
			{http.MethodDelete, "/whitelabel/favicon", nil},
			{http.MethodPost, "/whitelabel/test-email", nil},
		} {
			e.fails(e.do(user, req.method, orgPath(c.ID, req.path), req.body), http.StatusForbidden, apperr.CodeForbidden, "")
		}
		if ev := e.lastAudit(); ev["status"] != "failure" || ev["action"] != "updated" {
			t.Fatalf("refusal audit %v", ev)
		}
	}
	if got := e.whitelabel(c.ID); got != `{}` {
		t.Fatalf("whitelabel = %s", got)
	}
	if n := len(e.mail.Messages()); n != 0 {
		t.Fatalf("%d emails sent", n)
	}
	// Outside the organization the answer is the same refusal, and a missing organization is no different.
	e.fails(e.do(e.user(), http.MethodPut, orgPath(c.ID, "/whitelabel/favicon"), map[string]string{"faviconUrl": "/storage/a/public/f.png"}), http.StatusForbidden, apperr.CodeForbidden, "")
	e.fails(e.do(owner, http.MethodDelete, orgPath(uuid.New(), "/whitelabel/favicon"), nil), http.StatusForbidden, apperr.CodeForbidden, "")
}

// The writes share forma2's stateMutation limit, counted per organization.
func TestWhitelabelRateLimit(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.RateLimitingDisabled = false })
	owner := e.user()
	c := e.organization(owner, "Acme")
	key := fmt.Sprintf("fb:rate_limit:action:state-mutation:%s:%d", c.ID, e.now.Unix()/60*60)
	if err := e.cache.Set(t.Context(), key, 120, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	e.fails(e.do(owner, http.MethodPut, orgPath(c.ID, "/whitelabel/email-logo"), map[string]string{"logoUrl": "/storage/a/public/l.png"}), http.StatusTooManyRequests, apperr.CodeTooManyRequests, "")
	e.fails(e.do(owner, http.MethodDelete, orgPath(c.ID, "/whitelabel/favicon"), nil), http.StatusTooManyRequests, apperr.CodeTooManyRequests, "")
	if got := e.whitelabel(c.ID); got != `{}` {
		t.Fatalf("whitelabel = %s", got)
	}
	// The test email is not rate limited, as in forma2.
	e.ok(e.do(owner, http.MethodPost, orgPath(c.ID, "/whitelabel/test-email"), nil), http.StatusNoContent, nil)
}

// The test email goes to the caller, in their locale, with the organization's logo made absolute.
func TestWhitelabelTestEmail(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	c := e.organization(owner, "Acme")
	e.exec(`UPDATE users SET name = 'Grace', locale = 'ja-JP' WHERE id = ?`, owner)
	logo := "/storage/" + c.WorkspaceID.String() + "/public/logo--fid--abc.png"
	e.ok(e.do(owner, http.MethodPut, orgPath(c.ID, "/whitelabel/email-logo"), map[string]string{"logoUrl": logo}), http.StatusNoContent, nil)
	audits := len(e.auditEvents())

	e.ok(e.do(owner, http.MethodPost, orgPath(c.ID, "/whitelabel/test-email"), nil), http.StatusNoContent, nil)
	sent := e.mail.Messages()
	if len(sent) != 1 {
		t.Fatalf("%d emails sent", len(sent))
	}
	var email string
	e.scalar(&email, `SELECT email FROM users WHERE id = ?`, owner)
	m, err := mailer.NewWithTransport(e.cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	subject, html, err := m.Render("ja-JP", mailer.EmailCustomizationPreview{UserName: "Grace", LogoURL: webApp + logo})
	if err != nil {
		t.Fatal(err)
	}
	if sent[0].To != email || sent[0].Subject != subject || sent[0].HTML != html || !strings.Contains(html, webApp+logo) {
		t.Fatalf("email = %+v", sent[0])
	}
	if len(e.auditEvents()) != audits {
		t.Fatal("the test email was audited")
	}

	// Without a logo the preview carries Forma's.
	e.ok(e.do(owner, http.MethodDelete, orgPath(c.ID, "/whitelabel/email-logo"), nil), http.StatusNoContent, nil)
	e.ok(e.do(owner, http.MethodPost, orgPath(c.ID, "/whitelabel/test-email"), nil), http.StatusNoContent, nil)
	if sent := e.mail.Messages(); len(sent) != 2 || strings.Contains(sent[1].HTML, logo) {
		t.Fatalf("emails = %+v", sent)
	}
	e.fails(e.do(owner, http.MethodPost, orgPath(uuid.New(), "/whitelabel/test-email"), nil), http.StatusNotFound, apperr.CodeNotFound, "")
}

func TestPrettyURLs(t *testing.T) {
	e := newEnv(t)
	owner, member, billingMember := e.user(), e.user(), e.user()
	c := e.organization(owner, "Acme")
	e.member(c.ID, member, RoleMember)
	e.member(c.ID, billingMember, RoleBilling)
	other := e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Other') RETURNING id`, c.ID)
	first := e.insert(`INSERT INTO surveys (workspace_id, name, slug, created_at) VALUES (?, 'First', 'first', now() - interval '1 hour') RETURNING id`, c.WorkspaceID)
	second := e.insert(`INSERT INTO surveys (workspace_id, name, slug, status) VALUES (?, 'Second', 'second', 'inProgress') RETURNING id`, other)
	e.survey(c.WorkspaceID)
	elsewhere := e.organization(e.user(), "Elsewhere")
	e.exec(`INSERT INTO surveys (workspace_id, name, slug) VALUES (?, 'Foreign', 'foreign')`, elsewhere.WorkspaceID)

	var res prettyURLsResponse
	e.ok(e.do(member, http.MethodGet, orgPath(c.ID, "/pretty-urls"), nil), http.StatusOK, &res)
	if len(res.Surveys) != 2 {
		t.Fatalf("surveys = %+v", res.Surveys)
	}
	a, b := res.Surveys[0], res.Surveys[1]
	if a.ID != first || a.Slug != "first" || a.Name != "First" || a.Status != "draft" || a.Workspace.ID != c.WorkspaceID || a.Workspace.Name != "My workspace" || a.Workspace.OrganizationID != c.ID {
		t.Errorf("first = %+v", a)
	}
	if b.ID != second || b.Slug != "second" || b.Status != "inProgress" || b.Workspace.ID != other || b.Workspace.Name != "Other" {
		t.Errorf("second = %+v", b)
	}

	e.fails(e.do(billingMember, http.MethodGet, orgPath(c.ID, "/pretty-urls"), nil), http.StatusNotFound, apperr.CodeNotFound, "")
	e.fails(e.do(e.user(), http.MethodGet, orgPath(c.ID, "/pretty-urls"), nil), http.StatusNotFound, apperr.CodeNotFound, "")
}

// The domain settings page exists only self-hosted.
func TestPrettyURLsOnCloud(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.IsFormaCloud = true })
	owner := e.user()
	c := e.organization(owner, "Acme")
	e.fails(e.do(owner, http.MethodGet, orgPath(c.ID, "/pretty-urls"), nil), http.StatusNotFound, apperr.CodeNotFound, "")
}
