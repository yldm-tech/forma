package survey

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// withSurveyDomain gives the instance a PUBLIC_URL of its own, which custom head scripts need.
func withSurveyDomain(c *config.Config) {
	c.PublicURL, _ = url.Parse("http://surveys.example.com")
}

const turnedOff = "Custom head scripts are turned off on this instance: its survey pages are served on the admin domain, where a script could act as any signed-in user who opens the survey. The operator can turn them on by setting PUBLIC_URL to a separate domain for surveys."

// v1Body is a survey the v1 management API creates, with the given custom head scripts.
func v1Body(scripts string) map[string]any {
	return map[string]any{
		"name": "Created", "customHeadScripts": scripts,
		"blocks": []any{map[string]any{"id": "clblockscriptsaaaaaaaaaa1", "name": "Main", "elements": []any{
			map[string]any{"id": "q1", "type": "openText", "headline": map[string]any{"default": "Why?"}, "required": false, "inputType": "text", "charLimit": map[string]any{"enabled": false}},
		}}},
	}
}

// apiKeyID is the id of the key e.apiKey minted with the secret raw.
func (e *env) apiKeyID(raw string) uuid.UUID {
	e.t.Helper()
	sum := sha256.Sum256([]byte(raw))
	return e.insert(`SELECT id FROM api_keys WHERE lookup_hash = ?`, hex.EncodeToString(sum[:]))
}

func (e *env) storedScripts(id string) (scripts, mode string) {
	e.t.Helper()
	return e.text(`SELECT custom_head_scripts FROM surveys WHERE id = ?`, id), e.text(`SELECT custom_head_scripts_mode FROM surveys WHERE id = ?`, id)
}

// A survey's custom head scripts run unsanitized on its public page, like the workspace's, so they follow the workspace's rule: the organization's owners and managers change them, a member who may edit the survey does not, though saving the survey with them untouched still works.
func TestSurveyCustomScriptsAreTheManagersOnly(t *testing.T) {
	e := newEnv(t, withSurveyDomain)
	owner, manager, member := e.user(), e.user(), e.user()
	org, ws := e.organization(owner)
	e.member(org, manager, "manager")
	e.member(org, member, "member")
	e.grant(org, ws, member, "readWrite")
	id := e.createLink(owner, ws)
	script := "<script>window.survey=1</script>"

	sv := e.editorSurvey(member, id)
	sv["customHeadScripts"] = script
	e.fails(e.do(member, http.MethodPut, appPath(id, ""), sv), http.StatusForbidden, apperr.CodeForbidden, "Not authorized")
	sv["customHeadScripts"] = nil
	sv["customHeadScriptsMode"] = "replace"
	e.fails(e.do(member, http.MethodPut, appPath(id, ""), sv), http.StatusForbidden, apperr.CodeForbidden, "Not authorized")
	if scripts, mode := e.storedScripts(id); scripts != "" || mode != "add" {
		t.Fatalf("a refused save stored %q in %q mode", scripts, mode)
	}

	sv = e.editorSurvey(manager, id)
	sv["customHeadScripts"] = script
	sv["customHeadScriptsMode"] = "replace"
	e.ok(e.do(manager, http.MethodPut, appPath(id, ""), sv), http.StatusOK, nil)
	if scripts, mode := e.storedScripts(id); scripts != script || mode != "replace" {
		t.Fatalf("the manager's save stored %q in %q mode", scripts, mode)
	}

	// The member edits the survey the manager gave scripts; the scripts ride along unchanged.
	sv = e.editorSurvey(member, id)
	sv["name"] = "Edited by a member"
	var saved map[string]any
	e.ok(e.do(member, http.MethodPut, appPath(id, ""), sv), http.StatusOK, &saved)
	if saved["name"] != "Edited by a member" || saved["customHeadScripts"] != script {
		t.Fatalf("saved %v %v", saved["name"], saved["customHeadScripts"])
	}
	stale := e.editorSurvey(member, id)
	sv["customHeadScripts"] = "<script>window.other=1</script>"
	e.fails(e.do(member, http.MethodPut, appPath(id, ""), sv), http.StatusForbidden, apperr.CodeForbidden, "Not authorized")
	sv["customHeadScripts"] = nil
	e.fails(e.do(member, http.MethodPut, appPath(id, ""), sv), http.StatusForbidden, apperr.CodeForbidden, "Not authorized")

	// The manager clears them; the member's editor, loaded before, cannot put them back.
	sv = e.editorSurvey(manager, id)
	sv["customHeadScripts"] = nil
	e.ok(e.do(manager, http.MethodPut, appPath(id, ""), sv), http.StatusOK, nil)
	e.fails(e.do(member, http.MethodPut, appPath(id, ""), stale), http.StatusForbidden, apperr.CodeForbidden, "Not authorized")
	if scripts, _ := e.storedScripts(id); scripts != "" {
		t.Fatalf("a stale save restored %q", scripts)
	}
	e.ok(e.do(owner, http.MethodPut, appPath(id, ""), stale), http.StatusOK, nil)
	if scripts, _ := e.storedScripts(id); scripts != script {
		t.Fatalf("the owner's save stored %q", scripts)
	}
}

// The v1 management API writes surveys with an API key. A key never holds an organization role, so it changes custom head scripts only with the manage permission on the workspace; a write key is refused, on create as on update.
func TestSurveyCustomScriptsThroughAnAPIKey(t *testing.T) {
	e := newEnv(t, withSurveyDomain)
	owner := e.user()
	org, ws := e.organization(owner)
	id := e.createLink(owner, ws)
	writeKey, manageKey := APIKeyCaller(e.apiKeyID(e.apiKey(org, ws, "write"))), APIKeyCaller(e.apiKeyID(e.apiKey(org, ws, "manage")))
	e.authz.Deliver(t)
	ctx := context.Background()
	script := "<script>window.survey=1</script>"
	forbidden := func(err error) bool {
		ae, ok := apperr.As(err)
		return ok && ae.Status == http.StatusForbidden && ae.Code == apperr.CodeForbidden
	}

	current, err := e.repo.byID(ctx, uuid.MustParse(id))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.UpdateV1(ctx, writeKey, current, map[string]any{"customHeadScripts": script}); !forbidden(err) {
		t.Fatalf("write key update: %v", err)
	}
	if _, err := e.svc.UpdateV1(ctx, writeKey, current, map[string]any{"name": "Renamed"}); err != nil {
		t.Fatalf("write key update without scripts: %v", err)
	}
	if current, err = e.repo.byID(ctx, current.ID); err != nil {
		t.Fatal(err)
	}
	updated, err := e.svc.UpdateV1(ctx, manageKey, current, map[string]any{"customHeadScripts": script})
	if err != nil || updated.CustomHeadScripts == nil || *updated.CustomHeadScripts != script {
		t.Fatalf("manage key update: %v %v", updated, err)
	}
	// Unchanged scripts ride along on a write key's update, as on a member's save.
	if _, err := e.svc.UpdateV1(ctx, writeKey, updated, map[string]any{"name": "Again", "customHeadScripts": script}); err != nil {
		t.Fatalf("write key update with the stored scripts: %v", err)
	}

	if _, err := e.svc.CreateV1(ctx, writeKey, ws, v1Body(script)); !forbidden(err) {
		t.Fatalf("write key create: %v", err)
	}
	created, err := e.svc.CreateV1(ctx, manageKey, ws, v1Body(script))
	if err != nil || created.CustomHeadScripts == nil || *created.CustomHeadScripts != script {
		t.Fatalf("manage key create: %v %v", created, err)
	}
}

// Without a separate survey domain the public pages share the admin origin, and Forma Cloud never runs custom head scripts: setting them is refused even for an owner, with a message that tells the operator what to change. Clearing scripts stored before, and saving a survey that carries them unchanged, still work.
func TestSurveyCustomScriptsNeedTheInstance(t *testing.T) {
	cases := []struct {
		name      string
		configure func(*config.Config)
		message   string
	}{
		{"no PUBLIC_URL", func(*config.Config) {}, turnedOff},
		{"PUBLIC_URL on the admin host", func(c *config.Config) { c.PublicURL, _ = url.Parse(webApp) }, turnedOff},
		{"Forma Cloud", func(c *config.Config) { withSurveyDomain(c); c.IsFormaCloud = true }, "Custom head scripts are not available on Forma Cloud"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, tc.configure)
			owner, member := e.user(), e.user()
			org, ws := e.organization(owner)
			e.member(org, member, "member")
			e.grant(org, ws, member, "readWrite")
			id := e.createLink(owner, ws)
			key := APIKeyCaller(e.apiKeyID(e.apiKey(org, ws, "manage")))

			sv := e.editorSurvey(owner, id)
			sv["customHeadScripts"] = "<script>window.survey=1</script>"
			e.fails(e.do(owner, http.MethodPut, appPath(id, ""), sv), http.StatusForbidden, apperr.CodeOperationNotAllowed, tc.message)
			e.authz.Deliver(t)
			if _, err := e.svc.CreateV1(context.Background(), key, ws, v1Body("<script></script>")); err == nil {
				t.Fatal("a manage key created a survey with scripts")
			} else if ae, ok := apperr.As(err); !ok || ae.Code != apperr.CodeOperationNotAllowed {
				t.Fatalf("manage key create: %v", err)
			}

			// A value stored before the rule: a member's save keeps it, the owner clears it.
			e.exec(`UPDATE surveys SET custom_head_scripts = '<script>stored()</script>' WHERE id = ?`, id)
			sv = e.editorSurvey(member, id)
			sv["name"] = "Edited"
			e.ok(e.do(member, http.MethodPut, appPath(id, ""), sv), http.StatusOK, nil)
			sv = e.editorSurvey(owner, id)
			sv["customHeadScripts"] = nil
			e.ok(e.do(owner, http.MethodPut, appPath(id, ""), sv), http.StatusOK, nil)
			if scripts, _ := e.storedScripts(id); scripts != "" {
				t.Fatalf("clearing left %q", scripts)
			}
		})
	}
}
