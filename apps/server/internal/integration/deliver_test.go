package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/server/internal/responsemail"
)

const metadataText = "Source: web\nURL: http://example.com\nBrowser: Chrome\nOS: Mac OS\nDevice: Desktop\nCountry: USA\nAction: Action Name\nIP Address: 203.0.113.7"

func base(sv uuid.UUID) LinkBase {
	return LinkBase{CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), SurveyID: sv.String(), SurveyName: "Test Survey"}
}

func elements(ids ...string) Elements {
	return Elements{ElementIDs: ids, Elements: "Selected questions"}
}

// everything is a link base with every extra column switched on but contact attributes.
func everything(sv uuid.UUID) LinkBase {
	b := base(sv)
	b.IncludeHiddenFields, b.IncludeMetadata, b.IncludeCreatedAt, b.IncludeVariables = ptr(true), ptr(true), ptr(true), ptr(true)
	return b
}

func freshAirtableKey(now time.Time) AirtableKey {
	return AirtableKey{AccessToken: "airtable-token", RefreshToken: "airtable-refresh", ExpiryDate: now.Add(time.Hour).UTC().Format(time.RFC3339)}
}

func freshGoogleKey(now time.Time) GoogleKey {
	return GoogleKey{Scope: "s", TokenType: "Bearer", AccessToken: "google-token", RefreshToken: "google-refresh", ExpiryDate: now.Add(time.Hour).UnixMilli()}
}

// airtableFake serves a base with table1, whose fields are known, and records what is created.
func (e *env) airtableFake(known ...string) {
	var created atomic.Value
	created.Store([]string{})
	e.fake.handle("GET /airtableapi/v0/meta/bases/{base}/tables", func(w http.ResponseWriter, _ *http.Request) {
		fields := []map[string]string{}
		for _, n := range append(append([]string{}, known...), created.Load().([]string)...) {
			fields = append(fields, map[string]string{"id": "fld" + n, "name": n})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tables": []map[string]any{{"id": "table1", "name": "Table 1", "fields": fields}, {"id": "table2", "name": "Table 2", "fields": fields}}})
	})
	e.fake.handle("POST /airtableapi/v0/meta/bases/{base}/tables/{table}/fields", func(w http.ResponseWriter, r *http.Request) {
		var f struct{ Name string }
		_ = json.NewDecoder(r.Body).Decode(&f)
		created.Store(append(created.Load().([]string), f.Name))
		_, _ = io.WriteString(w, `{"id":"fldnew"}`)
	})
	e.fake.reply("POST /airtableapi/v0/{base}/{table}", http.StatusOK, `{"id":"rec1"}`)
}

func bodyOf[T any](t *testing.T, c call) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(c.Body, &v); err != nil {
		t.Fatalf("decode %s: %v", c.Body, err)
	}
	return v
}

// forma2's "should call airtableWriteData with correct parameters": the selected elements with headers whose recall shows its fallback, the hidden field labelled by its key, the metadata, the variable and the creation time, after creating the missing fields one at a time.
func TestAirtableWritesARecord(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws)
	e.airtableFake("Question 2")
	e.connect(ws, TypeAirtable, freshAirtableKey(e.now), "a@example.com", []AirtableLink{{LinkBase: everything(sv), Elements: elements("q1", "q2"), BaseID: "base1", TableID: "table1", TableName: "Table 1"}})

	ev := e.respond(ws, sv, `{"plan":"pro"}`)
	if err := e.deliverer.HandleResponse(context.Background(), ev); err != nil {
		t.Fatal(err)
	}

	records := e.fake.to(http.MethodPost, "/airtableapi/v0/base1/table1")
	if len(records) != 1 {
		t.Fatalf("records = %d; log: %s", len(records), e.logs.String())
	}
	got := bodyOf[map[string]any](t, records[0])
	want := map[string]any{"typecast": true, "fields": map[string]any{
		"Question 1 none": "Answer 1", "Question 2": "Choice 1; Choice 2", "hidden1": "Hidden Value",
		"Metadata": metadataText, "Variable 1": "Variable Value", "Created At": "2024-01-01 12:00:00 UTC",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("record = %v, want %v", got, want)
	}
	if h := records[0].Header.Get("Authorization"); h != "Bearer airtable-token" {
		t.Errorf("Authorization = %q", h)
	}
	var created []string
	for _, c := range e.fake.to(http.MethodPost, "/airtableapi/v0/meta/bases/base1/tables/table1/fields") {
		f := bodyOf[map[string]string](t, c)
		if f["type"] != "singleLineText" {
			t.Errorf("field type = %q", f["type"])
		}
		created = append(created, f["name"])
	}
	if want := []string{"Question 1 none", "hidden1", "Metadata", "Variable 1", "Created At"}; !reflect.DeepEqual(created, want) {
		t.Errorf("created fields = %v, want %v", created, want)
	}
}

// forma2's "should format Created At in the provided display time zone".
func TestCreatedAtUsesTheDisplayTimeZone(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws)
	e.airtableFake("Created At")
	link := AirtableLink{LinkBase: base(sv), Elements: elements(), BaseID: "base1", TableID: "table1"}
	link.IncludeCreatedAt = ptr(true)
	e.connect(ws, TypeAirtable, freshAirtableKey(e.now), "a@example.com", []AirtableLink{link})

	ev := e.respond(ws, sv, `null`)
	ev.DisplayTimeZone = "Asia/Manila"
	if err := e.deliverer.HandleResponse(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	records := e.fake.to(http.MethodPost, "/airtableapi/v0/base1/table1")
	if len(records) != 1 || !strings.Contains(string(records[0].Body), `"Created At":"2024-01-01 20:00:00 GMT+8"`) {
		t.Fatalf("records = %+v", records)
	}
}

// forma2's Airtable credential tests: an expired token is refreshed with the stored refresh token, the rotation is stored before the record is written with it, once for several links, and never for a response no link targets.
func TestAirtableRefreshesOnceAndStoresTheRotation(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws)
	other := e.survey(ws)
	e.airtableFake("Question 2")
	e.fake.reply("POST /airtable/oauth2/v1/token", http.StatusOK, `{"access_token":"rotated-token","refresh_token":"rotated-refresh","expires_in":"3600"}`)
	expired := AirtableKey{AccessToken: "old-token", RefreshToken: "old-refresh", ExpiryDate: e.now.Add(time.Minute).UTC().Format(time.RFC3339)}
	id := e.connect(ws, TypeAirtable, expired, "a@example.com", []AirtableLink{
		{LinkBase: base(sv), Elements: elements("q2"), BaseID: "base1", TableID: "table1"},
		{LinkBase: base(sv), Elements: elements("q2"), BaseID: "base1", TableID: "table2"},
	})

	if err := e.deliverer.HandleResponse(context.Background(), e.respond(ws, other, `null`)); err != nil {
		t.Fatal(err)
	}
	if n := len(e.fake.to(http.MethodPost, "/airtable/oauth2/v1/token")); n != 0 {
		t.Fatalf("a response to another survey refreshed %d times", n)
	}

	if err := e.deliverer.HandleResponse(context.Background(), e.respond(ws, sv, `null`)); err != nil {
		t.Fatal(err)
	}
	refreshes := e.fake.to(http.MethodPost, "/airtable/oauth2/v1/token")
	if len(refreshes) != 1 {
		t.Fatalf("refreshes = %d", len(refreshes))
	}
	form, _ := url.ParseQuery(string(refreshes[0].Body))
	if form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != "old-refresh" || form.Get("client_id") != "airtable-id" {
		t.Errorf("refresh form = %v", form)
	}
	for _, path := range []string{"/airtableapi/v0/base1/table1", "/airtableapi/v0/base1/table2"} {
		records := e.fake.to(http.MethodPost, path)
		if len(records) != 1 || records[0].Header.Get("Authorization") != "Bearer rotated-token" {
			t.Errorf("%s: %d records", path, len(records))
		}
	}
	stored, err := e.repo.ByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	k, _ := key[AirtableKey](stored)
	if k.AccessToken != "rotated-token" || k.RefreshToken != "rotated-refresh" || stored.Credentials.Email != "a@example.com" {
		t.Errorf("stored = %+v, email %q", k, stored.Credentials.Email)
	}
	at, _ := time.Parse(time.RFC3339, k.ExpiryDate)
	if d := at.Sub(e.now); d < 59*time.Minute || d > 61*time.Minute {
		t.Errorf("expiry = %s", k.ExpiryDate)
	}
}

// forma2's "reads back the winner's rotation instead of spending the single-use refresh token twice": a delivery that finds the refresh lock held uses the token the holder stored.
func TestAirtableReadsBackTheWinnersRotation(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws)
	e.airtableFake("Question 2")
	expired := AirtableKey{AccessToken: "old-token", RefreshToken: "old-refresh", ExpiryDate: "not a date"}
	id := e.connect(ws, TypeAirtable, expired, "a@example.com", []AirtableLink{{LinkBase: base(sv), Elements: elements("q2"), BaseID: "base1", TableID: "table1"}})
	if err := e.rdb.Set(context.Background(), "forma:oauth:airtable-token-refresh:"+ws.String(), "1", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	rotated, _ := json.Marshal(freshAirtableKey(e.now))
	if err := e.repo.SaveCredentials(context.Background(), id, Credentials{Key: rotated, Email: "a@example.com"}); err != nil {
		t.Fatal(err)
	}
	in, _ := e.repo.ByID(context.Background(), id)
	in.Credentials.Key, _ = json.Marshal(expired)

	k, err := e.deliverer.tokens.airtable(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if k.AccessToken != "airtable-token" || len(e.fake.to(http.MethodPost, "/airtable/oauth2/v1/token")) != 0 {
		t.Errorf("token = %q", k.AccessToken)
	}
}

// forma2's "refreshes anyway when the lock holder never publishes a rotation".
func TestAirtableRefreshesWhenTheHolderNeverPublishes(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	e.fake.reply("POST /airtable/oauth2/v1/token", http.StatusOK, `{"access_token":"mine","refresh_token":"r2","expires_in":3600}`)
	id := e.connect(ws, TypeAirtable, AirtableKey{AccessToken: "old", RefreshToken: "r1", ExpiryDate: e.now.Add(-time.Hour).UTC().Format(time.RFC3339)}, "a@example.com", nil)
	if err := e.rdb.Set(context.Background(), "forma:oauth:airtable-token-refresh:"+ws.String(), "1", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	in, _ := e.repo.ByID(context.Background(), id)
	k, err := e.deliverer.tokens.airtable(context.Background(), in)
	if err != nil || k.AccessToken != "mine" {
		t.Fatalf("token = %+v, %v", k, err)
	}
	if !strings.Contains(e.logs.String(), "refreshing unserialised") {
		t.Error("the unserialised refresh was not logged")
	}
}

// forma2's "should call googleSheetWriteData with correct parameters": the header row is rewritten at A1 on every response and the values appended at A2, both RAW.
func TestGoogleSheetsWritesHeaderAndRow(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws)
	e.fake.reply("GET /google/tokeninfo", http.StatusOK, `{}`)
	e.fake.reply("PUT /sheets/v4/spreadsheets/{id}/values/{range}", http.StatusOK, `{}`)
	e.fake.reply("POST /sheets/v4/spreadsheets/{id}/values/{range}", http.StatusOK, `{}`)
	link := SheetLink{LinkBase: base(sv), Elements: elements("q1", "q3_picture", "q4_files", "missing"), SpreadsheetID: "sheet1", SpreadsheetName: "Sheet"}
	e.connect(ws, TypeGoogleSheets, freshGoogleKey(e.now), "g@example.com", []SheetLink{link})

	if err := e.deliverer.HandleResponse(context.Background(), e.respond(ws, sv, `null`)); err != nil {
		t.Fatal(err)
	}
	updates := e.fake.to(http.MethodPut, "/sheets/v4/spreadsheets/sheet1/values/A1")
	appends := e.fake.to(http.MethodPost, "/sheets/v4/spreadsheets/sheet1/values/A2:append")
	if len(updates) != 1 || len(appends) != 1 {
		t.Fatalf("updates %d, appends %d; log: %s", len(updates), len(appends), e.logs.String())
	}
	for _, c := range append(updates, appends...) {
		if c.Query.Get("valueInputOption") != "RAW" || c.Header.Get("Authorization") != "Bearer google-token" {
			t.Errorf("request %s %v %v", c.Path, c.Query, c.Header.Get("Authorization"))
		}
	}
	header := bodyOf[struct{ Values [][]string }](t, updates[0])
	if want := [][]string{{"Question 1 none", "Question 3", "Files"}}; !reflect.DeepEqual(header.Values, want) {
		t.Errorf("header = %v", header.Values)
	}
	row := bodyOf[struct{ Values [][]string }](t, appends[0])
	want := [][]string{{"Answer 1", "https://surveys.example.com/storage/ws1/public/1.png", "https://app.example.com/storage/ws1/private/a.pdf; https://cdn.example.com/b.png"}}
	if !reflect.DeepEqual(row.Values, want) {
		t.Errorf("row = %v", row.Values)
	}
}

// forma2's googleSheet authorize: a token near expiry is refreshed and the merged credential stored with the email; without a refresh token the integration fails with invalid_grant and nothing is written.
func TestGoogleSheetsRefresh(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws)
	e.fake.reply("POST /google/token", http.StatusOK, `{"access_token":"new-token","expires_in":3600,"token_type":"Bearer","scope":"new-scope"}`)
	e.fake.reply("PUT /sheets/v4/spreadsheets/{id}/values/{range}", http.StatusOK, `{}`)
	e.fake.reply("POST /sheets/v4/spreadsheets/{id}/values/{range}", http.StatusOK, `{}`)
	link := SheetLink{LinkBase: base(sv), Elements: elements("q1"), SpreadsheetID: "sheet1"}
	near := freshGoogleKey(e.now)
	near.ExpiryDate = e.now.Add(time.Minute).UnixMilli()
	id := e.connect(ws, TypeGoogleSheets, near, "g@example.com", []SheetLink{link})

	if err := e.deliverer.HandleResponse(context.Background(), e.respond(ws, sv, `null`)); err != nil {
		t.Fatal(err)
	}
	if n := len(e.fake.to(http.MethodGet, "/google/tokeninfo")); n != 0 {
		t.Errorf("a token near expiry was checked with tokeninfo %d times", n)
	}
	appends := e.fake.to(http.MethodPost, "/sheets/v4/spreadsheets/sheet1/values/A2:append")
	if len(appends) != 1 || appends[0].Header.Get("Authorization") != "Bearer new-token" {
		t.Fatalf("appends = %+v", appends)
	}
	stored, _ := e.repo.ByID(context.Background(), id)
	k, _ := key[GoogleKey](stored)
	if k.AccessToken != "new-token" || k.RefreshToken != "google-refresh" || k.Scope != "new-scope" || k.ExpiryDate <= e.now.UnixMilli() || stored.Credentials.Email != "g@example.com" {
		t.Errorf("stored = %+v", k)
	}

	_, ws2 := e.organization(e.user())
	sv2 := e.survey(ws2)
	blank := near
	blank.RefreshToken = ""
	e.connect(ws2, TypeGoogleSheets, blank, "g@example.com", []SheetLink{{LinkBase: base(sv2), Elements: elements("q1"), SpreadsheetID: "sheet2"}})
	if err := e.deliverer.HandleResponse(context.Background(), e.respond(ws2, sv2, `null`)); err != nil {
		t.Fatal(err)
	}
	if len(e.fake.to(http.MethodPut, "/sheets/v4/spreadsheets/sheet2")) != 0 || !strings.Contains(e.logs.String(), `"msg":"Error in google sheets integration","err":"invalid_grant"`) {
		t.Errorf("blank refresh token: log %s", e.logs.String())
	}
}

// forma2's Slack message: the survey name over a divider, then each label in bold over its value; Slack's labels expand recall against the response, and contact attributes are appended when the link asks for them.
func TestSlackPostsTheMessage(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws)
	e.fake.reply("POST /slack/api/chat.postMessage", http.StatusOK, `{"ok":true,"channel":"C1","ts":"1"}`)
	link := SlackLink{LinkBase: base(sv), Elements: elements("q1", "q2"), ChannelID: "C1", ChannelName: "general"}
	link.IncludeContactAttributes = ptr(true)
	e.connect(ws, TypeSlack, map[string]any{"access_token": "xoxb-1", "token_type": "bot", "team": map[string]string{"id": "T1", "name": "Team"}}, "", []SlackLink{link})

	if err := e.deliverer.HandleResponse(context.Background(), e.respond(ws, sv, `{"plan":"pro","email":"person@example.com"}`)); err != nil {
		t.Fatal(err)
	}
	posts := e.fake.to(http.MethodPost, "/slack/api/chat.postMessage")
	if len(posts) != 1 {
		t.Fatalf("posts = %d; log %s", len(posts), e.logs.String())
	}
	form, _ := url.ParseQuery(string(posts[0].Body))
	// slack-go sends the bot token in the form body, which Slack accepts as it does the bearer header forma2 sent.
	if form.Get("channel") != "C1" || form.Get("token") != "xoxb-1" {
		t.Errorf("channel %q, token %q", form.Get("channel"), form.Get("token"))
	}
	var blocks []map[string]any
	if err := json.Unmarshal([]byte(form.Get("blocks")), &blocks); err != nil {
		t.Fatal(err)
	}
	section := func(text string) map[string]any {
		return map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": text}}
	}
	want := []map[string]any{
		section("Test Survey\n"), {"type": "divider"},
		section("*Question 1 Choice 1, Choice 2*"), section("Answer 1\n"),
		section("*Question 2*"), section("Choice 1; Choice 2\n"),
		// Contact attributes in the order PostgreSQL's jsonb keeps them, which forma2 read too.
		section("*person.plan*"), section("pro\n"),
		section("*person.email*"), section("person@example.com\n"),
	}
	if !reflect.DeepEqual(blocks, want) {
		got, _ := json.Marshal(blocks)
		t.Errorf("blocks = %s", got)
	}
}

// forma2's Notion payloads: picture selections as their URLs (a single one unwrapped for a url column), metadata, the creation time as a date, contact attributes, and getValue's `|| null`.
func TestNotionCreatesThePage(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws)
	e.fake.reply("POST /notion/v1/pages", http.StatusOK, `{"id":"page"}`)
	m := func(id, elType, colName, colType string) NotionMapping {
		return NotionMapping{Element: NotionRef{ID: id, Name: id, Type: elType}, Column: NotionRef{ID: colName, Name: colName, Type: colType}}
	}
	link := NotionLink{LinkBase: base(sv), DatabaseID: "db1", DatabaseName: "DB 1", Mapping: []NotionMapping{
		m("q1", "openText", "Column 1", "rich_text"),
		m("q3_picture", "pictureSelection", "Column 3", "url"),
		m("metadata", "metadata", "Metadata Col", "rich_text"),
		m("createdAt", "createdAt", "Created Col", "date"),
		m("person.plan", "openText", "Plan Col", "rich_text"),
		m("person.missing", "openText", "Missing Col", "rich_text"),
		m("q2", "multipleChoiceMulti", "Multi", "multi_select"),
		m("nothing", "openText", "Checkbox", "checkbox"),
		m("nothing", "openText", "Empty date", "date"),
	}}
	e.connect(ws, TypeNotion, map[string]any{"access_token": "ntn_1", "bot_id": "bot"}, "", []NotionLink{link})

	if err := e.deliverer.HandleResponse(context.Background(), e.respond(ws, sv, `{"plan":"pro"}`)); err != nil {
		t.Fatal(err)
	}
	pages := e.fake.to(http.MethodPost, "/notion/v1/pages")
	if len(pages) != 1 {
		t.Fatalf("pages = %d; log %s", len(pages), e.logs.String())
	}
	if pages[0].Header.Get("Notion-Version") != "2022-06-28" || pages[0].Header.Get("Authorization") != "Bearer ntn_1" {
		t.Errorf("headers = %v", pages[0].Header)
	}
	text := func(s string) []any { return []any{map[string]any{"text": map[string]any{"content": s}}} }
	want := map[string]any{
		"parent": map[string]any{"database_id": "db1"},
		"properties": map[string]any{
			"Column 1":     map[string]any{"rich_text": text("Answer 1")},
			"Column 3":     map[string]any{"url": "https://surveys.example.com/storage/ws1/public/1.png"},
			"Metadata Col": map[string]any{"rich_text": text(metadataText)},
			"Created Col":  map[string]any{"date": map[string]any{"start": "2024-01-01T12:00:00.000Z"}},
			"Plan Col":     map[string]any{"rich_text": text("pro")},
			"Missing Col":  map[string]any{"rich_text": nil},
			"Multi":        map[string]any{"multi_select": []any{map[string]any{"name": "Choice 1"}, map[string]any{"name": "Choice 2"}}},
			"Checkbox":     map[string]any{"checkbox": nil},
			"Empty date":   map[string]any{"date": map[string]any{}},
		},
	}
	if got := bodyOf[map[string]any](t, pages[0]); !reflect.DeepEqual(got, want) {
		raw, _ := json.Marshal(got)
		t.Errorf("page = %s", raw)
	}
}

// forma2's handleIntegrations: every integration is tried, one failing does not stop the others or the pipeline, and its failure is logged with forma2's label; a link for another survey writes nothing. Links made in forma2 name their survey by its old id.
func TestFailuresAreLoggedAndIsolated(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws)
	e.fake.reply("POST /notion/v1/pages", http.StatusBadRequest, `{"object":"error","code":"validation_error","message":"Title is not a property"}`)
	e.fake.reply("POST /slack/api/chat.postMessage", http.StatusOK, `{"ok":false,"error":"channel_not_found"}`)
	e.airtableFake("Question 2")
	e.connect(ws, TypeNotion, map[string]any{"access_token": "ntn_1", "bot_id": "bot"}, "", []NotionLink{{LinkBase: base(sv), DatabaseID: "db1", Mapping: []NotionMapping{{Element: NotionRef{ID: "q1", Type: "openText"}, Column: NotionRef{Name: "Title", Type: "title"}}}}})
	e.connect(ws, TypeSlack, map[string]any{"access_token": "xoxb-1"}, "", []SlackLink{{LinkBase: base(sv), Elements: elements("q1"), ChannelID: "C1"}})
	other := base(sv)
	other.SurveyID = uuid.NewString()
	legacy := base(sv)
	legacy.SurveyID = "clsurvey0000000000000001"
	e.connect(ws, TypeAirtable, freshAirtableKey(e.now), "a@example.com", []AirtableLink{
		{LinkBase: other, Elements: elements("q2"), BaseID: "base1", TableID: "other"},
		{LinkBase: base(sv), Elements: elements("q2"), BaseID: "base1", TableID: "table1"},
	})

	if err := e.deliverer.HandleResponse(context.Background(), e.respond(ws, sv, `null`)); err != nil {
		t.Fatal(err)
	}
	logs := e.logs.String()
	if !strings.Contains(logs, `"msg":"Error in notion integration","err":"Notion API error creating page: 400 Bad Request validation_error Title is not a property"`) {
		t.Errorf("notion failure not logged: %s", logs)
	}
	if !strings.Contains(logs, `"msg":"Error in slack integration","err":"slack chat.postMessage: channel_not_found"`) {
		t.Errorf("slack failure not logged: %s", logs)
	}
	if len(e.fake.to(http.MethodPost, "/airtableapi/v0/base1/table1")) != 1 || len(e.fake.to(http.MethodPost, "/airtableapi/v0/base1/other")) != 0 {
		t.Error("airtable did not write exactly the matching link")
	}

	// A link made in forma2 names the survey by its old id, which resolves to the migrated survey's id.
	migrated := e.insert(`INSERT INTO surveys (id, workspace_id, name, status, blocks) VALUES (?, ?, 'Old', 'inProgress', ?::jsonb) RETURNING id`, legacyid.ID(legacy.SurveyID), ws, surveyBlocks)
	answers, err := responsemail.Decode(&e.respond(ws, migrated, `null`).Response)
	if err != nil {
		t.Fatal(err)
	}
	if !forSurvey(legacy, &submission{answers: answers}) {
		t.Error("a forma2 survey id did not match its migrated survey")
	}
}
