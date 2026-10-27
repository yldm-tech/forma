package workspace

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

func languagesPath(ws uuid.UUID, rest string) string {
	return wsPath(ws, "/languages"+rest)
}

func TestCreateLanguage(t *testing.T) {
	e := newEnv(t)
	owner, manager, writer := e.user(), e.user(), e.user()
	org, ws := e.organization(owner)
	e.member(org, manager, "member")
	e.member(org, writer, "member")
	e.grant(org, ws, manager, "manage")
	e.grant(org, ws, writer, "readWrite")

	// A legacy code is stored canonical; the alias is kept as given.
	var l Language
	e.ok(e.do(manager, http.MethodPost, languagesPath(ws, ""), map[string]any{"code": "de", "alias": "german"}), http.StatusCreated, &l)
	if l.Code != "de-DE" || l.Alias == nil || *l.Alias != "german" || l.WorkspaceID != ws {
		t.Fatalf("created %+v", l)
	}
	e.ok(e.do(owner, http.MethodPost, languagesPath(ws, ""), map[string]any{"code": "zh-TW", "alias": nil}), http.StatusCreated, &l)
	if l.Code != "zh-Hant-TW" || l.Alias != nil {
		t.Fatalf("created %+v", l)
	}

	for code, message := range map[string]string{
		"":               "Language code is required",
		"not a language": "Invalid language code: 'not a language'",
		"nso":            "Invalid language code: 'nso'",
		// Well formed, but outside the curated catalog.
		"nso-ZA": "Unsupported language code: 'nso-ZA'",
	} {
		e.fails(e.do(owner, http.MethodPost, languagesPath(ws, ""), map[string]any{"code": code, "alias": nil}), http.StatusBadRequest, apperr.CodeValidation, message)
	}
	// en-US is every new workspace's language already; "de-de" is "de-DE" again.
	for _, code := range []string{"en-US", "de-de"} {
		e.fails(e.do(owner, http.MethodPost, languagesPath(ws, ""), map[string]any{"code": code, "alias": nil}), http.StatusConflict, apperr.CodeConflict, "")
	}
	e.fails(e.do(writer, http.MethodPost, languagesPath(ws, ""), map[string]any{"code": "fr"}), http.StatusForbidden, apperr.CodeForbidden, "Not authorized")
	e.fails(e.do(owner, http.MethodPost, languagesPath(uuid.New(), ""), map[string]any{"code": "fr"}), http.StatusForbidden, apperr.CodeForbidden, "")

	var detail Detail
	e.ok(e.do(owner, http.MethodGet, wsPath(ws, ""), nil), http.StatusOK, &detail)
	if len(detail.Languages) != 3 {
		t.Fatalf("languages %+v", detail.Languages)
	}
}

func TestUpdateLanguage(t *testing.T) {
	e := newEnv(t)
	owner, writer := e.user(), e.user()
	org, ws := e.organization(owner)
	e.member(org, writer, "member")
	e.grant(org, ws, writer, "readWrite")
	other := e.workspace(org, "Second")
	var l, foreign Language
	e.ok(e.do(owner, http.MethodPost, languagesPath(ws, ""), map[string]any{"code": "fr"}), http.StatusCreated, &l)
	e.ok(e.do(owner, http.MethodPost, languagesPath(other, ""), map[string]any{"code": "fr"}), http.StatusCreated, &foreign)

	// Only the alias changes: a code in the body is ignored.
	var updated Language
	e.ok(e.do(owner, http.MethodPatch, languagesPath(ws, "/"+l.ID.String()), map[string]any{"alias": "french", "code": "es"}), http.StatusOK, &updated)
	if updated.Code != "fr-FR" || updated.Alias == nil || *updated.Alias != "french" {
		t.Fatalf("updated %+v", updated)
	}
	e.ok(e.do(owner, http.MethodPatch, languagesPath(ws, "/"+l.ID.String()), map[string]any{"alias": nil}), http.StatusOK, &updated)
	if updated.Alias != nil {
		t.Fatalf("alias not cleared %+v", updated)
	}
	// A language of another workspace is not this workspace's, whatever the caller may do there.
	e.fails(e.do(owner, http.MethodPatch, languagesPath(ws, "/"+foreign.ID.String()), map[string]any{"alias": "x"}), http.StatusNotFound, apperr.CodeNotFound, "")
	e.fails(e.do(writer, http.MethodPatch, languagesPath(ws, "/"+l.ID.String()), map[string]any{"alias": "x"}), http.StatusForbidden, apperr.CodeForbidden, "")
	if e.count(`SELECT count(*) FROM languages WHERE alias = 'x'`) != 0 {
		t.Fatal("a refused alias was written")
	}
}

func TestDeleteLanguage(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	var german, french Language
	e.ok(e.do(owner, http.MethodPost, languagesPath(ws, ""), map[string]any{"code": "fr"}), http.StatusCreated, &french)
	// A legacy-coded row that is the default is protected like a canonical one.
	german.ID = e.insert(`INSERT INTO languages (workspace_id, code) VALUES (?, 'de') RETURNING id`, ws)
	e.ok(e.do(owner, http.MethodPatch, wsPath(ws, ""), map[string]any{"config": map[string]any{"defaultSurveyLanguage": "de-DE"}}), http.StatusOK, nil)

	e.fails(e.do(owner, http.MethodDelete, languagesPath(ws, "/"+german.ID.String()), nil), http.StatusForbidden, apperr.CodeOperationNotAllowed,
		"This language is the workspace's default survey language and cannot be removed")

	// The surveys using a language are listed before it is removed, and lose it with it.
	survey := e.insert(`INSERT INTO surveys (workspace_id, name) VALUES (?, 'Welcome') RETURNING id`, ws)
	e.exec(`INSERT INTO survey_languages (survey_id, language_id) VALUES (?, ?)`, survey, french.ID)
	var using languageSurveysResponse
	e.ok(e.do(owner, http.MethodGet, languagesPath(ws, "/"+french.ID.String()+"/surveys"), nil), http.StatusOK, &using)
	if len(using.Surveys) != 1 || using.Surveys[0] != "Welcome" {
		t.Fatalf("surveys %+v", using)
	}
	var deleted Language
	e.ok(e.do(owner, http.MethodDelete, languagesPath(ws, "/"+french.ID.String()), nil), http.StatusOK, &deleted)
	if deleted.ID != french.ID || e.count(`SELECT count(*) FROM survey_languages WHERE language_id = ?`, french.ID) != 0 {
		t.Fatal("language or its survey use survived")
	}
	e.fails(e.do(owner, http.MethodDelete, languagesPath(ws, "/"+french.ID.String()), nil), http.StatusNotFound, apperr.CodeNotFound, "")

	// Once the default moves, the old one can go.
	e.ok(e.do(owner, http.MethodPatch, wsPath(ws, ""), map[string]any{"config": map[string]any{"defaultSurveyLanguage": "en-US"}}), http.StatusOK, nil)
	e.ok(e.do(owner, http.MethodDelete, languagesPath(ws, "/"+german.ID.String()), nil), http.StatusOK, nil)

	stranger := e.user()
	e.fails(e.do(stranger, http.MethodGet, languagesPath(ws, "/"+german.ID.String()+"/surveys"), nil), http.StatusForbidden, apperr.CodeForbidden, "")
}

func TestAppConnection(t *testing.T) {
	e := newEnv(t)
	owner, reader := e.user(), e.user()
	org, ws := e.organization(owner)
	e.member(org, reader, "member")
	e.grant(org, ws, reader, "read")

	var conn AppConnection
	e.ok(e.do(reader, http.MethodGet, wsPath(ws, "/app-connection"), nil), http.StatusOK, &conn)
	if conn.WorkspaceID != ws || conn.AppURL != webApp || conn.AppSetupCompleted {
		t.Fatalf("app connection %+v", conn)
	}
	e.exec(`UPDATE workspaces SET app_setup_completed = true WHERE id = ?`, ws)
	e.ok(e.do(reader, http.MethodGet, wsPath(ws, "/app-connection"), nil), http.StatusOK, &conn)
	if !conn.AppSetupCompleted {
		t.Fatalf("app connection %+v", conn)
	}
	e.fails(e.do(e.user(), http.MethodGet, wsPath(ws, "/app-connection"), nil), http.StatusForbidden, apperr.CodeForbidden, "")
}

// An alias may name its own language by its base code (ja for ja-JP, zh for zh-Hans-CN) or no language, but not another language of the workspace nor one it does not have; only an alias being written is checked, so one an older rule let in blocks nothing but the language it names.
func TestLanguageAliasesFollowTheBaseLanguageRule(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	english := e.insert(`SELECT id FROM languages WHERE workspace_id = ? AND code = 'en-US'`, ws)
	create := func(code string, alias any) *httptest.ResponseRecorder {
		return e.do(owner, http.MethodPost, languagesPath(ws, ""), map[string]any{"code": code, "alias": alias})
	}
	setAlias := func(id uuid.UUID, alias any) *httptest.ResponseRecorder {
		return e.do(owner, http.MethodPatch, languagesPath(ws, "/"+id.String()), map[string]any{"alias": alias})
	}

	// Seed's aliases, and a language added beside them.
	var japanese, chinese, arabic Language
	e.ok(create("ja-JP", "ja"), http.StatusCreated, &japanese)
	e.ok(create("zh-Hans-CN", "zh"), http.StatusCreated, &chinese)
	e.ok(create("ar-EG", nil), http.StatusCreated, &arabic)

	e.fails(create("ko-KR", "fr"), http.StatusBadRequest, apperr.CodeValidation, "The alias 'fr' of ko-KR is the code of fr-FR, which this workspace does not have. Add that language, or choose an alias that is not a language code.")
	e.fails(create("de-DE", "ja"), http.StatusBadRequest, apperr.CodeValidation, "The alias 'ja' of de-DE is already the alias of ja-JP. Each alias can be used once.")
	e.fails(create("de-DE", "ja-jp"), http.StatusBadRequest, apperr.CodeValidation, "The alias 'ja-jp' of de-DE names ja-JP, another language of this workspace. Choose an alias that is not another language's code.")
	e.fails(create("zh-Hant-TW", "zh-CN"), http.StatusBadRequest, apperr.CodeValidation, "The alias 'zh-CN' of zh-Hant-TW names zh-Hans-CN, another language of this workspace. Choose an alias that is not another language's code.")
	e.fails(setAlias(english, "ja-JP"), http.StatusBadRequest, apperr.CodeValidation, "")
	e.fails(setAlias(english, " ZH "), http.StatusBadRequest, apperr.CodeValidation, "")

	var updated Language
	e.ok(setAlias(english, "en_us"), http.StatusOK, &updated)
	e.ok(setAlias(arabic.ID, "arabic"), http.StatusOK, &updated)

	// An alias stored before the rule: it is written back unchanged, does not block the other languages, and only blocks adding the language it names.
	e.exec(`UPDATE languages SET alias = 'nl' WHERE id = ?`, english)
	e.ok(setAlias(english, "nl"), http.StatusOK, &updated)
	e.ok(setAlias(japanese.ID, "japanese"), http.StatusOK, &updated)
	e.fails(create("nl-NL", nil), http.StatusBadRequest, apperr.CodeValidation, "The alias 'nl' of en-US names nl-NL. Change that alias before adding nl-NL.")
	// A legacy code is the same language as its canonical one.
	e.exec(`INSERT INTO languages (workspace_id, code) VALUES (?, 'de')`, ws)
	e.fails(create("de-DE", nil), http.StatusConflict, apperr.CodeConflict, "Language de-DE already exists in this workspace")
}
