package workspace

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
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
	if conn.WorkspaceID != ws || conn.LegacyEnvironmentID != nil || conn.AppURL != webApp || conn.AppSetupCompleted {
		t.Fatalf("app connection %+v", conn)
	}
	e.exec(`UPDATE workspaces SET legacy_environment_id = 'clenv123', app_setup_completed = true WHERE id = ?`, ws)
	e.ok(e.do(reader, http.MethodGet, wsPath(ws, "/app-connection"), nil), http.StatusOK, &conn)
	if conn.LegacyEnvironmentID == nil || *conn.LegacyEnvironmentID != "clenv123" || !conn.AppSetupCompleted {
		t.Fatalf("app connection %+v", conn)
	}
	e.fails(e.do(e.user(), http.MethodGet, wsPath(ws, "/app-connection"), nil), http.StatusForbidden, apperr.CodeForbidden, "")
}
