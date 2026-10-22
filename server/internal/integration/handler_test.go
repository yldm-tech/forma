package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
)

// auditEvent is the part of an audit record the tests check.
type auditEvent struct {
	Target         struct{ ID, Type string }
	Action         string
	Status         string
	OrganizationID string `json:"organizationId"`
}

func (e *env) lastAudit() auditEvent {
	e.t.Helper()
	var last *auditEvent
	for line := range strings.SplitSeq(e.logs.String(), "\n") {
		if !strings.Contains(line, `"msg":"audit"`) {
			continue
		}
		var ev auditEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			e.t.Fatal(err)
		}
		last = &ev
	}
	if last == nil {
		e.t.Fatal("no audit event")
	}
	return *last
}

// forma2's redactIntegrationCredentials: the pages get the links and who the integration is connected as, never a token.
func TestListNeverSendsTokens(t *testing.T) {
	e := newEnv(t)
	o := e.owner()
	sv := e.survey(o.ws)
	e.connect(o.ws, TypeGoogleSheets, GoogleKey{AccessToken: "ya29.secret", RefreshToken: "1//secret-refresh"}, "g@example.com", []SheetLink{{LinkBase: base(sv), Elements: elements("q1"), SpreadsheetID: "sheet1"}})
	e.connect(o.ws, TypeNotion, map[string]any{"access_token": "ntn_secret", "bot_id": "bot", "workspace_name": "Acme Notion"}, "", nil)
	e.connect(o.ws, TypeSlack, map[string]any{"access_token": "xoxb-secret", "team": map[string]string{"id": "T1", "name": "Acme Slack"}}, "", nil)
	e.svc.cfg.Integrations.AirtableClientID = ""

	rec := e.do(http.MethodGet, "/api/app/workspaces/"+o.ws.String()+"/integrations", asUser(o.user), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d %s", rec.Code, rec.Body)
	}
	for _, secret := range []string{"secret", "access_token", "refresh_token"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("the list carries %q: %s", secret, rec.Body)
		}
	}
	out := decode[Overview](t, rec)
	if len(out.Integrations) != 3 || !out.Enabled[TypeGoogleSheets] || out.Enabled[TypeAirtable] || !out.Enabled[TypeNotion] || !out.Enabled[TypeSlack] {
		t.Fatalf("overview = %+v", out)
	}
	byType := map[string]View{}
	for _, v := range out.Integrations {
		byType[v.Type] = v
	}
	if c := byType[TypeGoogleSheets].Connection; !c.Connected || *c.Email != "g@example.com" || !strings.Contains(string(byType[TypeGoogleSheets].Config), `"spreadsheetId":"sheet1"`) {
		t.Errorf("google sheets = %+v %s", c, byType[TypeGoogleSheets].Config)
	}
	if c := byType[TypeNotion].Connection; !c.Connected || *c.WorkspaceName != "Acme Notion" {
		t.Errorf("notion = %+v", c)
	}
	if c := byType[TypeSlack].Connection; !c.Connected || *c.TeamName != "Acme Slack" {
		t.Errorf("slack = %+v", c)
	}

	outsider := e.user()
	if rec := e.do(http.MethodGet, "/api/app/workspaces/"+o.ws.String()+"/integrations", asUser(outsider), nil); rec.Code != http.StatusForbidden {
		t.Errorf("an outsider listed: %d", rec.Code)
	}
}

// A link made in forma2 names its survey by the forma2 id; the pages get the id migrated from it, which is how they find the survey to edit the link, and every other field as stored.
func TestListNamesMigratedSurveysByTheirIDs(t *testing.T) {
	e := newEnv(t)
	o := e.owner()
	current := e.survey(o.ws)
	const legacy = "clsv00000000000000000042"
	migrated := e.insert(`INSERT INTO surveys (id, workspace_id, name, status, blocks) VALUES (?, ?, 'Old', 'inProgress', ?::jsonb) RETURNING id`, legacyid.ID(legacy), o.ws, surveyBlocks)
	old := base(current)
	old.SurveyID = legacy
	e.connect(o.ws, TypeSlack, map[string]any{"access_token": "xoxb", "team": map[string]string{"name": "T"}}, "", []SlackLink{
		{LinkBase: old, Elements: elements("q2"), ChannelID: "C1", ChannelName: "general"},
		{LinkBase: base(current), Elements: elements("q1"), ChannelID: "C2"},
	})
	out := decode[Overview](t, e.do(http.MethodGet, "/api/app/workspaces/"+o.ws.String()+"/integrations", asUser(o.user), nil))
	links, err := decodeLinks[SlackLink](out.Integrations[0].Config)
	if err != nil || len(links) != 2 {
		t.Fatalf("links = %+v %v", links, err)
	}
	if links[0].SurveyID != migrated.String() || links[0].ChannelID != "C1" || links[0].ChannelName != "general" || links[0].ElementIDs[0] != "q2" || links[0].SurveyName != "Test Survey" {
		t.Errorf("the migrated link = %+v", links[0])
	}
	if links[1].SurveyID != current.String() || links[1].ChannelID != "C2" {
		t.Errorf("the current link = %+v", links[1])
	}
}

// An integration whose sealed credentials do not open (a rotated ENCRYPTION_KEY) is listed as disconnected rather than failing the page, as forma2 handed back what it could not decrypt.
func TestUndecryptableCredentialsShowAsDisconnected(t *testing.T) {
	e := newEnv(t)
	o := e.owner()
	id := e.connect(o.ws, TypeSlack, map[string]any{"access_token": "xoxb", "team": map[string]string{"name": "T"}}, "", nil)
	e.exec(`UPDATE integration_credentials SET ciphertext = '\x01deadbeef' WHERE integration_id = ?`, id)
	out := decode[Overview](t, e.do(http.MethodGet, "/api/app/workspaces/"+o.ws.String()+"/integrations", asUser(o.user), nil))
	if len(out.Integrations) != 1 || out.Integrations[0].Connection.Connected {
		t.Errorf("overview = %+v", out)
	}
}

// forma2's createOrUpdateIntegrationAction: the links are replaced and the stored credential is kept whatever the client sends; an integration that was never connected cannot be created this way, and every link must name a survey of the workspace.
func TestSaveLinks(t *testing.T) {
	e := newEnv(t)
	o := e.owner()
	org := o.org
	sv := e.survey(o.ws)
	path := "/api/app/workspaces/" + o.ws.String() + "/integrations/slack"
	link := map[string]any{"createdAt": "2024-01-01T00:00:00Z", "surveyId": sv.String(), "surveyName": "Test Survey", "elementIds": []string{"q1"}, "elements": "Selected questions", "channelId": "C1", "channelName": "general", "includeMetadata": true}

	if rec := e.do(http.MethodPut, path, asUser(o.user), map[string]any{"data": []any{link}}); rec.Code != http.StatusNotFound {
		t.Errorf("save before connecting = %d %s", rec.Code, rec.Body)
	}
	id := e.connect(o.ws, TypeSlack, map[string]any{"access_token": "xoxb-kept"}, "", nil)

	withKey := map[string]any{"data": []any{link}, "key": map[string]string{"access_token": ""}, "config": map[string]any{"key": map[string]string{"access_token": "evil"}}}
	rec := e.do(http.MethodPut, path, asUser(o.user), withKey)
	if rec.Code != http.StatusOK || decode[SaveResponse](t, rec).ID != id {
		t.Fatalf("save = %d %s", rec.Code, rec.Body)
	}
	in, _ := e.repo.ByID(context.Background(), id)
	if k, _ := key[SlackKey](in); k.AccessToken != "xoxb-kept" {
		t.Errorf("the credential changed to %+v", k)
	}
	links, err := decodeLinks[SlackLink](in.Config)
	if err != nil || len(links) != 1 || links[0].ChannelID != "C1" || !flag(links[0].IncludeMetadata) || links[0].IncludeCreatedAt != nil {
		t.Errorf("links = %+v, %v", links, err)
	}
	if a := e.lastAudit(); a.Action != "createdUpdated" || a.Target.Type != "integration" || a.Target.ID != id.String() || a.Status != "success" || a.OrganizationID != org.String() {
		t.Errorf("audit = %+v", a)
	}
	if strings.Contains(e.logs.String(), "xoxb-kept") {
		t.Error("the audit log carries the token")
	}

	foreign := e.survey(e.owner().ws)
	link["surveyId"] = foreign.String()
	if rec := e.do(http.MethodPut, path, asUser(o.user), map[string]any{"data": []any{link}}); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_integration_surveys") {
		t.Errorf("a foreign survey: %d %s", rec.Code, rec.Body)
	}
	delete(link, "channelId")
	link["surveyId"] = sv.String()
	if rec := e.do(http.MethodPut, path, asUser(o.user), map[string]any{"data": []any{link}}); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"field":"data[0].channelId"`) {
		t.Errorf("a link without a channel: %d %s", rec.Code, rec.Body)
	}
	reader := e.user()
	e.grant(org, o.ws, reader, "read")
	if rec := e.do(http.MethodPut, path, asUser(reader), map[string]any{"data": []any{}}); rec.Code != http.StatusForbidden {
		t.Errorf("a read-only member saved: %d", rec.Code)
	}
	if rec := e.do(http.MethodPut, "/api/app/workspaces/"+o.ws.String()+"/integrations/n8n", asUser(o.user), map[string]any{"data": []any{}}); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown type: %d", rec.Code)
	}
	if rec := e.do(http.MethodPut, path, asUser(o.user), map[string]any{"data": []any{}}); rec.Code != http.StatusOK || e.linksOf(id) != `{"data": []}` {
		t.Errorf("removing the last link: %d %s", rec.Code, e.linksOf(id))
	}
}

// forma2's deleteIntegrationAction: workspace.write on the integration's workspace; the links and the credential go with it.
func TestDeleteIntegration(t *testing.T) {
	e := newEnv(t)
	o := e.owner()
	org := o.org
	id := e.connect(o.ws, TypeNotion, map[string]any{"access_token": "ntn", "bot_id": "b"}, "", nil)
	reader := e.user()
	e.grant(org, o.ws, reader, "read")

	if rec := e.do(http.MethodDelete, "/api/app/integrations/"+id.String(), asUser(reader), nil); rec.Code != http.StatusForbidden {
		t.Errorf("a read-only member deleted: %d", rec.Code)
	}
	if rec := e.do(http.MethodDelete, "/api/app/integrations/"+id.String(), asUser(o.user), nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body)
	}
	var left int
	e.db.Raw(`SELECT (SELECT count(*) FROM integrations WHERE id = ?) + (SELECT count(*) FROM integration_credentials WHERE integration_id = ?)`, id, id).Scan(&left)
	if left != 0 {
		t.Errorf("%d rows left", left)
	}
	if a := e.lastAudit(); a.Action != "deleted" || a.Target.ID != id.String() || a.Status != "success" {
		t.Errorf("audit = %+v", a)
	}
	if rec := e.do(http.MethodDelete, "/api/app/integrations/"+id.String(), asUser(o.user), nil); rec.Code != http.StatusNotFound {
		t.Errorf("deleting again = %d", rec.Code)
	}
}

// forma2's integrations getSurveys: not completed, not archived, most recently updated first, with what a link can select.
func TestLinkableSurveys(t *testing.T) {
	e := newEnv(t)
	o := e.owner()
	older := e.survey(o.ws)
	newer := e.survey(o.ws)
	e.exec(`UPDATE surveys SET updated_at = now() - interval '1 day' WHERE id = ?`, older)
	done := e.survey(o.ws)
	e.exec(`UPDATE surveys SET status = 'completed' WHERE id = ?`, done)
	archived := e.survey(o.ws)
	e.exec(`UPDATE surveys SET archived_at = now() WHERE id = ?`, archived)

	rec := e.do(http.MethodGet, "/api/app/workspaces/"+o.ws.String()+"/integrations/surveys", asUser(o.user), nil)
	out := decode[[]LinkableSurvey](t, rec)
	if len(out) != 2 || out[0].ID != newer || out[1].ID != older {
		t.Fatalf("surveys = %+v", out)
	}
	s := out[0]
	if len(s.Elements) != 4 || s.Elements[0].ID != "q1" || s.Elements[0].Headline["default"] == "" || len(s.HiddenFields) != 1 || s.HiddenFields[0] != "hidden1" ||
		len(s.Variables) != 1 || s.Variables[0] != (SurveyVariable{StorageKey: "var1", Name: "Variable 1"}) {
		t.Errorf("survey = %+v", s)
	}
}

// forma2's getSpreadsheetNameByIdAction and validateGoogleSheetsConnectionAction.
func TestGoogleSheetsLookups(t *testing.T) {
	e := newEnv(t)
	o := e.owner()
	prefix := "/api/app/workspaces/" + o.ws.String() + "/integrations/googleSheets"
	if rec := e.do(http.MethodGet, prefix+"/spreadsheets/sheet1", asUser(o.user), nil); rec.Code != http.StatusNotFound {
		t.Errorf("no integration: %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(http.MethodGet, prefix+"/validate", asUser(o.user), nil); rec.Code != http.StatusOK || rec.Body.String() != `{"connected":false}` {
		t.Errorf("validate without an integration: %d %s", rec.Code, rec.Body)
	}

	e.fake.reply("GET /google/tokeninfo", http.StatusOK, `{}`)
	e.fake.reply("GET /sheets/v4/spreadsheets/sheet1", http.StatusOK, `{"properties":{"title":"Responses"}}`)
	e.fake.reply("GET /sheets/v4/spreadsheets/locked", http.StatusForbidden, `{"error":{"code":403,"message":"The caller does not have permission","status":"PERMISSION_DENIED"}}`)
	e.fake.reply("GET /sheets/v4/spreadsheets/gone", http.StatusNotFound, `{"error":{"code":404,"message":"Requested entity was not found.","status":"NOT_FOUND"}}`)
	id := e.connect(o.ws, TypeGoogleSheets, freshGoogleKey(e.now), "g@example.com", nil)

	if rec := e.do(http.MethodGet, prefix+"/spreadsheets/sheet1", asUser(o.user), nil); rec.Code != http.StatusOK || rec.Body.String() != `{"name":"Responses"}` {
		t.Errorf("name: %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(http.MethodGet, prefix+"/spreadsheets/locked", asUser(o.user), nil); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"code":"insufficient_permission"`) {
		t.Errorf("locked: %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(http.MethodGet, prefix+"/spreadsheets/gone", asUser(o.user), nil); rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "Error while fetching spreadsheet data") {
		t.Errorf("gone: %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(http.MethodGet, prefix+"/validate", asUser(o.user), nil); rec.Code != http.StatusOK || rec.Body.String() != `{"connected":true}` {
		t.Errorf("validate: %d %s", rec.Code, rec.Body)
	}

	expired, _ := json.Marshal(GoogleKey{AccessToken: "old", ExpiryDate: 1})
	if err := e.repo.SaveCredentials(context.Background(), id, Credentials{Key: expired, Email: "g@example.com"}); err != nil {
		t.Fatal(err)
	}
	if rec := e.do(http.MethodGet, prefix+"/validate", asUser(o.user), nil); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"code":"invalid_grant"`) {
		t.Errorf("validate without a refresh token: %d %s", rec.Code, rec.Body)
	}
	e.fake.reply("POST /google/token", http.StatusBadRequest, `{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`)
	revoked, _ := json.Marshal(GoogleKey{AccessToken: "old", RefreshToken: "revoked", ExpiryDate: 1})
	if err := e.repo.SaveCredentials(context.Background(), id, Credentials{Key: revoked, Email: "g@example.com"}); err != nil {
		t.Fatal(err)
	}
	if rec := e.do(http.MethodGet, prefix+"/spreadsheets/sheet1", asUser(o.user), nil); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"code":"invalid_grant"`) {
		t.Errorf("revoked refresh token: %d %s", rec.Code, rec.Body)
	}
}

// The link modals' provider lookups: Airtable bases, Notion databases with their properties in Notion's order, and Slack channels across pages; a Slack token reported expired deletes the integration, as forma2 did.
func TestProviderLookups(t *testing.T) {
	e := newEnv(t)
	o := e.owner()
	prefix := "/api/app/workspaces/" + o.ws.String() + "/integrations"
	e.fake.reply("GET /airtableapi/v0/meta/bases", http.StatusOK, `{"bases":[{"id":"app1","name":"CRM","permissionLevel":"create"}]}`)
	e.fake.reply("POST /notion/v1/search", http.StatusOK, `{"results":[{"object":"database","id":"db1","title":[{"plain_text":"Feedback"}],"properties":{"Zeta":{"id":"z","name":"Zeta","type":"rich_text"},"Name":{"id":"title","name":"Name","type":"title"}}}]}`)
	e.fake.handle("POST /slack/api/users.conversations", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "cursor=next") {
			_, _ = io.WriteString(w, `{"ok":true,"channels":[{"id":"C2","name":"random"}],"response_metadata":{"next_cursor":""}}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"channels":[{"id":"C1","name":"general"}],"response_metadata":{"next_cursor":"next"}}`)
	})
	e.connect(o.ws, TypeAirtable, freshAirtableKey(e.now), "a@example.com", nil)
	e.connect(o.ws, TypeNotion, map[string]any{"access_token": "ntn", "bot_id": "bot"}, "", nil)
	slackID := e.connect(o.ws, TypeSlack, map[string]any{"access_token": "xoxb"}, "", nil)

	for path, want := range map[string]string{
		"/airtable/bases":   `[{"id":"app1","name":"CRM"}]`,
		"/notion/databases": `[{"id":"db1","name":"Feedback","properties":[{"id":"z","name":"Zeta","type":"rich_text"},{"id":"title","name":"Name","type":"title"}]}]`,
		"/slack/channels":   `[{"id":"C1","name":"general"},{"id":"C2","name":"random"}]`,
	} {
		if rec := e.do(http.MethodGet, prefix+path, asUser(o.user), nil); rec.Code != http.StatusOK || rec.Body.String() != want {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
	conversations := e.fake.to(http.MethodPost, "/slack/api/users.conversations")
	if !strings.Contains(string(conversations[0].Body), "limit=200") || !strings.Contains(string(conversations[0].Body), "types=private_channel%2Cpublic_channel") {
		t.Errorf("users.conversations = %s", conversations[0].Body)
	}

	_, ws2 := e.organization(o.user)
	expired := e.connect(ws2, TypeSlack, map[string]any{"access_token": "xoxe-expired"}, "", nil)
	e.svc.p.slack = e.fake.srv.URL + "/expired-slack"
	e.fake.reply("POST /expired-slack/api/users.conversations", http.StatusOK, `{"ok":false,"error":"token_expired"}`)
	rec := e.do(http.MethodGet, "/api/app/workspaces/"+ws2.String()+"/integrations/slack/channels", asUser(o.user), nil)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), `"message":"token_expired"`) {
		t.Errorf("expired token: %d %s", rec.Code, rec.Body)
	}
	if in, _ := e.repo.ByID(context.Background(), expired); in != nil {
		t.Error("the expired Slack integration was kept")
	}
	if in, _ := e.repo.ByID(context.Background(), slackID); in == nil {
		t.Error("another workspace's Slack integration was deleted")
	}
}

// Connecting from the pages through /api/app answers the same authorize URL as forma2's route.
func TestConnectThroughAppAPI(t *testing.T) {
	e := newEnv(t)
	o := e.owner()
	rec := e.do(http.MethodPost, "/api/app/workspaces/"+o.ws.String()+"/integrations/slack/connect", reqHeaders{testUserHeader: o.user.String(), testSessionHeader: o.session.String()}, nil)
	if rec.Code != http.StatusOK || !strings.Contains(decode[AuthorizeResponse](t, rec).AuthURL, "/slack/oauth/v2/authorize?client_id=slack-id") {
		t.Fatalf("connect = %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(http.MethodPost, "/api/app/workspaces/"+uuid.NewString()+"/integrations/slack/connect", asUser(o.user), nil); rec.Code != http.StatusForbidden {
		t.Errorf("another workspace: %d", rec.Code)
	}
}

// The provider lookups call the provider with the workspace's own tokens, so each is authorized on that workspace before anything is sent: another tenant is refused every one of them, and a read-only member the ones that serve the link modals, which need workspace.write.
func TestLookupsAreTenantScoped(t *testing.T) {
	e := newEnv(t)
	o := e.owner()
	e.survey(o.ws)
	e.airtableFake()
	e.connect(o.ws, TypeGoogleSheets, freshGoogleKey(e.now), "g@example.com", nil)
	e.connect(o.ws, TypeAirtable, freshAirtableKey(e.now), "a@example.com", nil)
	e.connect(o.ws, TypeNotion, map[string]any{"access_token": "ntn", "bot_id": "bot"}, "", nil)
	e.connect(o.ws, TypeSlack, map[string]any{"access_token": "xoxb"}, "", nil)
	outsider := e.owner()
	reader := oauthCaller{user: e.user(), session: uuid.New(), org: o.org, ws: o.ws}
	e.grant(o.org, o.ws, reader.user, "read")
	providerCalls := func() int { return len(e.fake.to(http.MethodGet, "")) + len(e.fake.to(http.MethodPost, "")) }

	prefix := "/api/app/workspaces/" + o.ws.String() + "/integrations"
	routes := []struct {
		path      string
		readerMay bool
	}{
		{"/surveys", true},
		{"/googleSheets/validate", false},
		{"/googleSheets/spreadsheets/sheet1", false},
		{"/airtable/bases", false},
		{"/airtable/bases/app1/tables", true},
		{"/notion/databases", false},
		{"/slack/channels", false},
	}
	for _, r := range routes {
		if rec := e.do(http.MethodGet, prefix+r.path, asUser(outsider.user), nil); rec.Code != http.StatusForbidden {
			t.Errorf("an outsider on %s: %d %s", r.path, rec.Code, rec.Body)
		}
		if r.readerMay {
			continue
		}
		if rec := e.do(http.MethodGet, prefix+r.path, asUser(reader.user), nil); rec.Code != http.StatusForbidden {
			t.Errorf("a read-only member on %s: %d %s", r.path, rec.Code, rec.Body)
		}
	}
	victim := oauthCaller{user: outsider.user, session: outsider.session, ws: o.ws}
	if rec := e.do(http.MethodGet, "/api/v1/integrations/airtable/tables?baseId=app1", victim.headers(), nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("an outsider on the v1 Airtable tables: %d %s", rec.Code, rec.Body)
	}
	if n := providerCalls(); n != 0 {
		t.Fatalf("refused lookups reached the provider %d times", n)
	}

	if rec := e.do(http.MethodGet, prefix+"/surveys", asUser(reader.user), nil); rec.Code != http.StatusOK {
		t.Errorf("a read-only member's surveys: %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(http.MethodGet, prefix+"/airtable/bases/app1/tables", asUser(reader.user), nil); rec.Code != http.StatusOK {
		t.Errorf("a read-only member's Airtable tables: %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(http.MethodGet, "/api/v1/integrations/airtable/tables?baseId=app1", reader.headers(), nil); rec.Code != http.StatusOK {
		t.Errorf("a read-only member's v1 Airtable tables: %d %s", rec.Code, rec.Body)
	}
}
