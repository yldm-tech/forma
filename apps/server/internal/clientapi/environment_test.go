package clientapi

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

func (e *env) state(t *testing.T, id string) map[string]any {
	t.Helper()
	e.clock.nextWindow()
	res := e.do("GET", "/api/v1/client/"+id+"/environment", nil)
	if res.Status != 200 {
		t.Fatalf("environment %d %s", res.Status, res.Body)
	}
	if res.Header.Get("Cache-Control") != stateCache {
		t.Fatalf("cache %q", res.Header.Get("Cache-Control"))
	}
	return res.data(t)["data"].(map[string]any)
}

func surveysOf(state map[string]any) []map[string]any {
	var out []map[string]any
	for _, s := range state["surveys"].([]any) {
		out = append(out, s.(map[string]any))
	}
	return out
}

func (e *env) flushState(ws uuid.UUID) {
	e.cache.Del(e.t.Context(), "fb:env:"+ws.String()+":state")
}

// forma2's getWorkspaceStateData: only live app surveys, newest first, at most 30, never archived ones.
func TestStateListsLiveAppSurveys(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	var newest uuid.UUID
	for i := range 31 {
		newest = e.survey(ws, "type", "app", "name", "App "+string(rune('a'+i)))
		e.exec(`UPDATE surveys SET created_at = now() + make_interval(secs => ?) WHERE id = ?`, i, newest)
	}
	e.survey(ws, "name", "Link")
	draft := e.survey(ws, "type", "app", "status", "draft")
	archived := e.survey(ws, "type", "app")
	e.exec(`UPDATE surveys SET archived_at = now(), created_at = now() + interval '1 hour' WHERE id IN (?, ?)`, archived, draft)

	surveys := surveysOf(e.state(t, ws.String()))
	if len(surveys) != maxStateSurveys {
		t.Fatalf("%d surveys, want %d", len(surveys), maxStateSurveys)
	}
	if surveys[0]["id"] != newest.String() {
		t.Fatalf("first survey %v, want the newest %s", surveys[0]["id"], newest)
	}
	for _, s := range surveys {
		if s["type"] != "app" || s["status"] != "inProgress" || s["id"] == archived.String() || s["id"] == draft.String() {
			t.Fatalf("listed %v", s["id"])
		}
		if s["name"] != surveyNamePlaceholder || s["segment"] != nil || s["questions"] == nil {
			t.Fatalf("survey shape %v", s)
		}
	}
}

// The segment is reduced to placeholders: whether it filters, never how; environmentId and workspaceId are the legacy environment id when the workspace has one.
func TestStateSanitizesSegments(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	targeted := e.survey(ws, "type", "app")
	open := e.survey(ws, "type", "app")
	e.segment(ws, "Targeted", `[{"id":"f1","connector":null,"resource":{"id":"r1","root":{"type":"attribute","contactAttributeKey":"plan"},"value":"pro","qualifier":{"operator":"equals"}}}]`, targeted)
	e.segment(ws, "Open", `[]`, open)

	for _, s := range surveysOf(e.state(t, ws.String())) {
		seg := s["segment"].(map[string]any)
		if seg["title"] != segmentTitlePlaceholder || seg["environmentId"] != ws.String() || seg["workspaceId"] != ws.String() || seg["createdAt"] != "1970-01-01T00:00:00.000Z" {
			t.Fatalf("segment %v", seg)
		}
		filters, _ := json.Marshal(seg["filters"])
		switch s["id"] {
		case targeted.String():
			var placeholder any
			_ = json.Unmarshal(placeholderFilter, &placeholder)
			want, _ := json.Marshal([]any{placeholder})
			if seg["hasFilters"] != true || string(filters) != string(want) {
				t.Fatalf("targeted segment %v", seg)
			}
		case open.String():
			if seg["hasFilters"] != false || string(filters) != `[]` {
				t.Fatalf("open segment %v", seg)
			}
		}
		if _, ok := s["interactionRefresh"]; ok {
			t.Fatal("interactionRefresh without interaction filters")
		}
	}

	e.exec(`UPDATE workspaces SET legacy_environment_id = 'cllegacyenvironmentaaaa1' WHERE id = ?`, ws)
	e.flushState(ws)
	seg := surveysOf(e.state(t, ws.String()))[0]["segment"].(map[string]any)
	if seg["environmentId"] != "cllegacyenvironmentaaaa1" || seg["workspaceId"] != "cllegacyenvironmentaaaa1" {
		t.Fatalf("legacy ids %v", seg)
	}
}

// forma2's appendLegacyLanguageCodes (data.test.ts "appends bare legacy language codes…"): a multi-language survey lists each language again under its legacy aliases, after the canonical ones and skipping codes it already has; a single-language survey is left alone.
func TestStateAddsLegacyLanguageCodes(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	multi := e.survey(ws, "type", "app")
	single := e.survey(ws, "type", "app")
	en, de, zh := e.language(ws, "en-US", nil), e.language(ws, "de-DE", nil), e.language(ws, "zh-Hant-TW", nil)
	tw := e.language(ws, "zh-TW", ptr("tw"))
	e.surveyLanguage(multi, en, true)
	e.surveyLanguage(multi, de, false)
	e.surveyLanguage(multi, zh, false)
	e.surveyLanguage(multi, tw, false)
	e.surveyLanguage(single, de, true)

	for _, s := range surveysOf(e.state(t, ws.String())) {
		var codes []string
		for _, l := range s["languages"].([]any) {
			lang := l.(map[string]any)["language"].(map[string]any)
			if lang["projectId"] != lang["workspaceId"] {
				t.Fatalf("projectId %v", lang)
			}
			codes = append(codes, lang["code"].(string))
		}
		want := []string{"en-US", "de-DE", "zh-Hant-TW", "zh-TW", "en", "de", "zh-Hant"}
		if s["id"] == single.String() {
			want = []string{"de-DE"}
		}
		if len(codes) != len(want) {
			t.Fatalf("%v codes %v, want %v", s["id"], codes, want)
		}
		for i := range want {
			if codes[i] != want[i] {
				t.Fatalf("%v codes %v, want %v", s["id"], codes, want)
			}
		}
	}
}

// forma2's buildSurveyInteractionRefreshMap: a survey-interaction filter of a live survey, directly or inside a segment it refers to, sets its event on the surveys it names; once one does, every survey carries the gate.
func TestStateInteractionRefresh(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	seen := e.survey(ws, "type", "app")
	other := e.survey(ws, "type", "app")
	targeted := e.survey(ws, "type", "app")
	nested := e.segment(ws, "Nested", `[{"id":"f1","connector":null,"resource":{"id":"r1","root":{"type":"surveyInteraction"},"value":{"surveyScope":"specific","surveyIds":["`+seen.String()+`"],"within":{"amount":7,"unit":"days"}},"qualifier":{"operator":"haveCompleted"}}}]`)
	e.segment(ws, "Targeting", `[{"id":"f2","connector":null,"resource":{"id":"r2","root":{"type":"surveyInteraction"},"value":{"surveyScope":"specific","surveyIds":["`+seen.String()+`"],"within":{"amount":7,"unit":"days"}},"qualifier":{"operator":"haveSeen"}}},
		{"id":"f3","connector":"and","resource":{"id":"r3","root":{"type":"segment","segmentId":"`+nested.String()+`"},"value":"`+nested.String()+`","qualifier":{"operator":"userIsIn"}}}]`, targeted)

	for _, s := range surveysOf(e.state(t, ws.String())) {
		refresh := s["interactionRefresh"].(map[string]any)
		want := map[string]any{"onDisplay": false, "onResponse": false, "onFinished": false}
		if s["id"] == seen.String() {
			want = map[string]any{"onDisplay": true, "onResponse": false, "onFinished": true}
		}
		for k, v := range want {
			if refresh[k] != v {
				t.Fatalf("%v (other %v) refresh %v, want %v", s["id"], other, refresh, want)
			}
		}
	}
}

// The state's other parts: the workspace's settings with absolute storage URLs (and the same under project), action classes, triggers, display percentage, the reCAPTCHA site key only when reCAPTCHA is configured, and app setup marked complete by the first build.
func TestStateComposition(t *testing.T) {
	e := newEnv(t, func(c *config.Config) {
		c.RecaptchaSiteKey, c.RecaptchaSecretKey = "site-key", "secret-key"
		c.PublicURL, _ = url.Parse("https://surveys.example.com")
	})
	ws := e.workspace("styling", `{"allowStyleOverwrite":true,"background":{"bg":"/storage/x/public/bg.png"},"logo":"/storage/x/private/l.png"}`, "placement", "center")
	checkout := e.insert(`INSERT INTO action_classes (workspace_id, name, type, key) VALUES (?, 'Checkout', 'code', 'checkout') RETURNING id`, ws)
	sv := e.survey(ws, "type", "app", "styling", `{"cardBackgroundColor":{"light":"#fff"},"image":"/storage/x/public/img.png"}`)
	e.exec(`UPDATE surveys SET display_percentage = 50.5 WHERE id = ?`, sv)
	e.exec(`INSERT INTO survey_triggers (survey_id, action_class_id) VALUES (?, ?)`, sv, checkout)

	state := e.state(t, ws.String())
	settings := state["workspace"].(map[string]any)
	styling := settings["styling"].(map[string]any)
	if styling["background"].(map[string]any)["bg"] != "https://surveys.example.com/storage/x/public/bg.png" || styling["logo"] != webAppURL+"/storage/x/private/l.png" {
		t.Fatalf("styling %v", styling)
	}
	if settings["placement"] != "center" {
		t.Fatalf("settings %v", settings)
	}
	project, _ := json.Marshal(state["project"])
	workspace, _ := json.Marshal(settings)
	if string(project) != string(workspace) {
		t.Fatalf("project %s, workspace %s", project, workspace)
	}
	if state["recaptchaSiteKey"] != "site-key" {
		t.Fatalf("site key %v", state["recaptchaSiteKey"])
	}
	classes := state["actionClasses"].([]any)
	if len(classes) != 1 || classes[0].(map[string]any)["key"] != "checkout" {
		t.Fatalf("action classes %v", classes)
	}
	s := surveysOf(state)[0]
	if s["displayPercentage"] != 50.5 || s["styling"].(map[string]any)["image"] != "https://surveys.example.com/storage/x/public/img.png" {
		t.Fatalf("survey %v", s)
	}
	if triggers, _ := json.Marshal(s["triggers"]); string(triggers) != `[{"actionClass":{"name":"Checkout"}}]` {
		t.Fatalf("triggers %s", triggers)
	}
	if e.count(`SELECT count(*) FROM workspaces WHERE id = ? AND app_setup_completed`, ws) != 1 {
		t.Fatal("app setup not marked complete")
	}

	t.Run("without reCAPTCHA", func(t *testing.T) {
		plain := newEnv(t)
		pws := plain.workspace()
		plain.survey(pws, "type", "app")
		plain.exec(`UPDATE surveys SET display_percentage = 0 WHERE workspace_id = ?`, pws)
		st := plain.state(t, pws.String())
		if _, ok := st["recaptchaSiteKey"]; ok {
			t.Fatal("a site key without reCAPTCHA configured")
		}
		if surveysOf(st)[0]["displayPercentage"] != nil {
			t.Fatalf("a display percentage of 0 is null: %v", surveysOf(st)[0]["displayPercentage"])
		}
	})
}

// The state is cached for a minute under forma2's key; the id resolution for an hour; a miss is never cached.
func TestStateAndIDResolutionAreCached(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace("legacy_environment_id", "clcachedenvaaaaaaaaaaaa1")
	e.survey(ws, "type", "app")
	if n := len(surveysOf(e.state(t, "clcachedenvaaaaaaaaaaaa1"))); n != 1 {
		t.Fatalf("%d surveys", n)
	}
	e.survey(ws, "type", "app")
	if n := len(surveysOf(e.state(t, ws.String()))); n != 1 {
		t.Fatalf("a cached state lists %d surveys", n)
	}
	if ttl := e.cache.TTL(t.Context(), "fb:env:"+ws.String()+":state").Val(); ttl <= 0 || ttl > stateTTL {
		t.Fatalf("state TTL %v", ttl)
	}
	if ttl := e.cache.TTL(t.Context(), "forma:env:clcachedenvaaaaaaaaaaaa1:id-resolution").Val(); ttl <= stateTTL || ttl > idResolutionTTL {
		t.Fatalf("id resolution TTL %v", ttl)
	}
	e.flushState(ws)
	if n := len(surveysOf(e.state(t, ws.String()))); n != 2 {
		t.Fatalf("after the cache entry expired: %d surveys", n)
	}

	missing := "clmissingworkspaceaaaaa1"
	e.do("GET", "/api/v1/client/"+missing+"/environment", nil).expectError(t, 404, "not_found", "Workspace not found")
	if n := e.cache.Exists(t.Context(), "forma:env:"+missing+":id-resolution").Val(); n != 0 {
		t.Fatal("a miss was cached")
	}
	// A cache that cannot be read is read through.
	e.cache.Set(t.Context(), "forma:env:"+ws.String()+":id-resolution", "not json", 0)
	e.state(t, ws.String())
}

// forma2 caches the same raw id under "fb:env:<id>:id-resolution", holding its own cuids. The two servers share one Valkey during the cutover and after a rollback, so this server neither reads forma2's entry nor overwrites it.
func TestIDResolutionLeavesForma2sEntryAlone(t *testing.T) {
	e := newEnv(t)
	legacy := "clsharedenvaaaaaaaaaaaa1"
	ws := e.workspace("legacy_environment_id", legacy)
	e.survey(ws, "type", "app")
	forma2Key := "fb:env:" + legacy + ":id-resolution"
	forma2Entry := `{"id":"clforma2workspaceaaaaaa1","organizationId":"clforma2organizationa1"}`
	e.cache.Set(t.Context(), forma2Key, forma2Entry, 0)

	if n := len(surveysOf(e.state(t, legacy))); n != 1 {
		t.Fatalf("%d surveys", n)
	}
	if got := e.cache.Get(t.Context(), forma2Key).Val(); got != forma2Entry {
		t.Fatalf("forma2's entry was overwritten with %s", got)
	}
	var cached map[string]string
	if err := json.Unmarshal([]byte(e.cache.Get(t.Context(), "forma:env:"+legacy+":id-resolution").Val()), &cached); err != nil || cached["id"] != ws.String() {
		t.Fatalf("own entry %v, %v", cached, err)
	}
}

// The environment and identify routes check the id's form before resolving it: a cuid or UUID, surrounding spaces ignored.
func TestIDFormat(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	e.do("GET", "/api/v1/client/not-a-cuid!/environment", nil).expectError(t, 400, "bad_request", "Invalid ID format")
	e.do("POST", "/api/v2/client/NOT_VALID/user", map[string]any{"userId": "u"}).expectError(t, 400, "bad_request", "Invalid workspace ID format")
	if res := e.do("GET", "/api/v1/client/%20"+ws.String()+"%20/environment", nil); res.Status != 200 {
		t.Fatalf("a padded id: %d %s", res.Status, res.Body)
	}
	details := e.do("GET", "/api/v2/client/clunknownaaaaaaaaaaaaaa1/environment", nil).expectError(t, 404, "not_found", "Workspace not found")
	if details["resource_id"] != "clunknownaaaaaaaaaaaaaa1" || details["resource_type"] != "Workspace" {
		t.Fatalf("details %v", details)
	}
}
