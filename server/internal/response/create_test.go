package response

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/survey"
)

// The cases of forma2's client create tests: V1R/lib/response.test.ts, V2R/lib/response.test.ts, V1R/route.test.ts and V2R/route.test.ts, CR/response-error tests, and anonymize.test.ts, against a real database.

func stored(e *env, id uuid.UUID) map[string]any {
	e.t.Helper()
	var raw []byte
	if err := e.db.Raw(`SELECT row_to_json(r)::jsonb FROM responses r WHERE id = ?`, id).Row().Scan(&raw); err != nil {
		e.t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal(raw, &row); err != nil {
		e.t.Fatal(err)
	}
	return row
}

func pipelineEvents(e *env, responseID uuid.UUID) []string {
	e.t.Helper()
	var events []string
	if err := e.db.Raw(`SELECT args->>'event' FROM river_job WHERE kind = 'response-pipeline.process' AND args->'response'->>'id' = ? ORDER BY id`, responseID.String()).Scan(&events).Error; err != nil {
		e.t.Fatal(err)
	}
	return events
}

func TestCreateStoresTheResponseAsForma2Did(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws, surveyOpts{hiddenFieldIDs: []string{"plan"}})
	e.field(ws, sv, "plan", "ingested", "string", false)
	contact := e.contact(ws, "user-1", "email", "ada@example.com")
	ctx := context.Background()

	res, err := e.submit(ctx, ws, sv, `{"q1":"hello","plan":"gold","rogue":"x"}`, CreateInput{
		UserID: ptr("user-1"), Finished: true, Language: ptr(" de "), Variables: map[string]any{"v": 1.0},
		TTC:     map[string]float64{"q1": 1000, "q2": -5, "q3": 1e12},
		Meta:    ClientMeta{Source: ptr("link"), URL: ptr("https://example.com/s/x?a=1"), PagePath: ptr("/s/x"), ScreenWidth: ptr(1280.0)},
		Request: RequestInfo{UserAgent: UserAgent{Browser: "Chrome", OS: "macOS"}, Country: "DE"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.QuotaFull.QuotaFull || res.Quota != nil {
		t.Fatalf("quota %+v on a survey without quotas", res.QuotaFull)
	}
	row := stored(e, res.ID)
	want := map[string]any{
		"finished":           true,
		"ending_id":          nil,
		"data":               map[string]any{"q1": "hello", "plan": "gold"},
		"variables":          map[string]any{"v": 1.0},
		"ttc":                map[string]any{"q1": 1000.0, "q2": 0.0, "q3": 86400000.0, "_total": 86401000.0},
		"language":           "de-DE",
		"single_use_id":      nil,
		"display_id":         nil,
		"contact_id":         contact.String(),
		"contact_attributes": map[string]any{"userId": "user-1", "email": "ada@example.com"},
		"ingest_flags":       []any{},
		"meta": map[string]any{
			"source": "link", "url": "https://example.com/s/x?a=1", "pagePath": "/s/x", "screenWidth": 1280.0, "country": "DE",
			"userAgent": map[string]any{"browser": "Chrome", "os": "macOS", "device": "desktop"},
		},
	}
	for k, v := range want {
		if !reflect.DeepEqual(row[k], v) {
			t.Errorf("%s = %#v, want %#v", k, row[k], v)
		}
	}
	if got := pipelineEvents(e, res.ID); !reflect.DeepEqual(got, []string{EventResponseCreated, EventResponseFinished}) {
		t.Fatalf("pipeline events %v", got)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(e.jsonText(`SELECT args::text FROM river_job WHERE kind = 'response-pipeline.process' LIMIT 1`)), &payload); err != nil {
		t.Fatal(err)
	}
	response := payload["response"].(map[string]any)
	if payload["workspaceId"] != ws.String() || payload["surveyId"] != sv.String() || response["contact"].(map[string]any)["userId"] != "user-1" || response["finished"] != true {
		t.Fatalf("pipeline payload %v", payload)
	}
	if _, leaked := response["ingestFlags"]; leaked {
		t.Fatal("the pipeline payload carries ingest flags")
	}
}

func (e *env) jsonText(query string, args ...any) string {
	e.t.Helper()
	var s string
	if err := e.db.Raw(query, args...).Row().Scan(&s); err != nil {
		e.t.Fatal(err)
	}
	return s
}

func TestCreateWithoutTTCOrContact(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws, surveyOpts{})
	res, err := e.submit(context.Background(), ws, sv, `{}`, CreateInput{UserID: ptr("nobody"), Finished: false, EndingID: ptr("end1")})
	if err != nil {
		t.Fatal(err)
	}
	row := stored(e, res.ID)
	// A userId nobody has leaves the response without a contact: the response path never creates one.
	if row["contact_id"] != nil || row["contact_attributes"] != nil || e.count(`SELECT count(*) FROM contacts`) != 0 {
		t.Fatalf("contact %v, attributes %v", row["contact_id"], row["contact_attributes"])
	}
	if !reflect.DeepEqual(row["ttc"], map[string]any{}) || row["ending_id"] != "end1" || row["language"] != nil {
		t.Fatalf("ttc %v ending %v language %v", row["ttc"], row["ending_id"], row["language"])
	}
	// A partial response without a ttc gets no total.
	if got := pipelineEvents(e, res.ID); !reflect.DeepEqual(got, []string{EventResponseCreated}) {
		t.Fatalf("pipeline events %v", got)
	}
}

func TestCreateV2AttachesOnlyAnExistingContactOfTheWorkspace(t *testing.T) {
	e := newEnv(t)
	org, ws := e.organization(e.user())
	sv := e.survey(ws, surveyOpts{})
	own := e.contact(ws, "u1")
	foreign := e.contact(e.workspace(org), "u2")
	ctx := context.Background()

	res, err := e.submit(ctx, ws, sv, `{}`, CreateInput{ContactID: &own, Finished: false})
	if err != nil {
		t.Fatal(err)
	}
	if row := stored(e, res.ID); row["contact_id"] != own.String() {
		t.Fatalf("contact %v, want %s", row["contact_id"], own)
	}
	res, err = e.submit(ctx, ws, sv, `{}`, CreateInput{ContactID: &foreign, Finished: false})
	if err != nil {
		t.Fatal(err)
	}
	if row := stored(e, res.ID); row["contact_id"] != nil {
		t.Fatalf("a contact of another workspace was attached: %v", row["contact_id"])
	}
}

func TestCreateRefusesBeforeWriting(t *testing.T) {
	e := newEnv(t)
	org, ws := e.organization(e.user())
	sv := e.survey(ws, surveyOpts{})
	paused := e.survey(ws, surveyOpts{status: survey.StatusPaused})
	other := e.survey(e.workspace(org), surveyOpts{})
	ctx := context.Background()

	_, err := e.submit(ctx, ws, other, `{}`, CreateInput{})
	appErr(t, err, http.StatusBadRequest, "Survey is part of another workspace", map[string]any{"workspaceId": ws.String()})
	_, err = e.submit(ctx, ws, paused, `{}`, CreateInput{})
	appErr(t, err, http.StatusForbidden, "Survey is not accepting submissions", map[string]any{"surveyId": paused.String()})
	_, err = e.svc.Survey(ctx, "clunknownsurveyaaaaaaaaa1")
	appErr(t, err, http.StatusNotFound, "Survey not found", map[string]any{"resource_id": "clunknownsurveyaaaaaaaaa1", "resource_type": "Survey"})
	_, err = e.svc.Survey(ctx, "not a cuid")
	appErr(t, err, http.StatusBadRequest, "Validation failed: Invalid cuid2", map[string]any{})
	if n := e.count(`SELECT count(*) FROM responses`); n != 0 || sv == uuid.Nil {
		t.Fatalf("%d responses written", n)
	}
}

func TestCreateChecksTheDisplay(t *testing.T) {
	e := newEnv(t)
	org, ws := e.organization(e.user())
	sv := e.survey(ws, surveyOpts{})
	otherSurvey := e.survey(ws, surveyOpts{})
	otherWorkspaceSurvey := e.survey(e.workspace(org), surveyOpts{})
	c1 := e.contact(ws, "u1")
	e.contact(ws, "u2")
	ctx := context.Background()

	cases := []struct {
		name    string
		display string
		userID  *string
		message string
	}{
		{"unknown", uuid.NewString(), nil, "not found"},
		{"another workspace", e.display(otherWorkspaceSurvey, nil).String(), nil, "belongs to a different workspace"},
		{"another survey", e.display(otherSurvey, nil).String(), nil, "is associated with a different survey"},
		{"another contact", e.display(sv, &c1).String(), ptr("u2"), "belongs to a different contact"},
		{"a contact display without the contact", e.display(sv, &c1).String(), nil, "belongs to a different contact"},
	}
	for _, c := range cases {
		_, err := e.submit(ctx, ws, sv, `{}`, CreateInput{DisplayID: &c.display, UserID: c.userID})
		appErr(t, err, http.StatusBadRequest, "Display "+c.display+" "+c.message, map[string]any{})
	}

	display := e.display(sv, &c1).String()
	res, err := e.submit(ctx, ws, sv, `{}`, CreateInput{DisplayID: &display, UserID: ptr("u1")})
	if err != nil {
		t.Fatal(err)
	}
	if row := stored(e, res.ID); row["display_id"] != display {
		t.Fatalf("display %v, want %s", row["display_id"], display)
	}
	_, err = e.submit(ctx, ws, sv, `{}`, CreateInput{DisplayID: &display, UserID: ptr("u1")})
	appErr(t, err, http.StatusBadRequest, "Display "+display+" is already linked to a response", map[string]any{})
	if n := e.count(`SELECT count(*) FROM responses`); n != 1 {
		t.Fatalf("%d responses, want only the one that answered the display", n)
	}
}

func TestADisplayAnsweredConcurrentlyIsTheOwnershipError(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws, surveyOpts{})
	display := e.display(sv, nil)
	// The ownership check passed, then another response took the display: the unique index answers.
	err := createError(e.db.Exec(`INSERT INTO responses (survey_id, display_id) VALUES (?, ?), (?, ?)`, sv, display, sv, display).Error, ptr(display.String()))
	appErr(t, err, http.StatusBadRequest, "Display "+display.String()+" is already linked to a response", nil)
	err = createError(e.db.Exec(`INSERT INTO responses (survey_id, single_use_id) VALUES (?, 'a'), (?, 'a')`, sv, sv).Error, nil)
	appErr(t, err, http.StatusConflict, "Response already submitted for this single-use link", map[string]any{})
}

func TestCreateCapturesTheClientAddressOnlyWhenAsked(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	cases := []struct {
		name      string
		opts      surveyOpts
		forwarded string
		want      any
	}{
		{"capture off", surveyOpts{}, "203.0.113.7", nil},
		{"capture on", surveyOpts{captureIP: true}, "203.0.113.7", "203.0.113.7"},
		{"an IPv6 address is masked to its /64", surveyOpts{captureIP: true}, "2001:db8:1:2:3:4:5:6", "2001:0db8:0001:0002:0000:0000:0000:0000"},
		{"no trusted address", surveyOpts{captureIP: true}, "", "untrusted-client-ip"},
		{"anonymized responses never capture it", surveyOpts{captureIP: true, anonymize: true}, "203.0.113.7", nil},
	}
	for _, c := range cases {
		sv := e.survey(ws, c.opts)
		res, err := e.submit(e.clientContext(c.forwarded), ws, sv, `{}`, CreateInput{Meta: ClientMeta{}})
		if err != nil {
			t.Fatal(err)
		}
		meta := stored(e, res.ID)["meta"].(map[string]any)
		if meta["ipAddress"] != c.want {
			t.Errorf("%s: ipAddress %v, want %v", c.name, meta["ipAddress"], c.want)
		}
	}
}

func TestAnonymizeKeepsOnlyWhatIdentifiesNoOne(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws, surveyOpts{anonymize: true})
	res, err := e.submit(context.Background(), ws, sv, `{}`, CreateInput{
		Meta: ClientMeta{
			Source: ptr("link"), URL: ptr("https://example.com/s/x?email=a@b.c#t"), Action: ptr("Clicked"), PagePath: ptr("/s/x"),
			PageReferrer: ptr("https://news.example.org/weekly?issue=42"), UtmSource: ptr("news"), ViewportWidth: ptr(800.0), Timezone: ptr("Europe/Berlin"), Locale: ptr("de-AT"),
		},
		Request: RequestInfo{UserAgent: UserAgent{Browser: "Chrome", OS: "macOS", Device: "mobile"}, Country: "DE"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"source": "link", "url": "https://example.com/s/x", "action": "Clicked", "pagePath": "/s/x",
		"pageReferrer": "https://news.example.org/weekly", "utmSource": "news", "viewportWidth": 800.0, "timezone": "Europe/Berlin", "locale": "de-AT",
	}
	if meta := stored(e, res.ID)["meta"]; !reflect.DeepEqual(meta, want) {
		t.Fatalf("meta %v\nwant %v", meta, want)
	}
}

func TestAnonymizeIsTheIdentityWhenOff(t *testing.T) {
	meta := map[string]any{"country": "DE", "url": "https://x.test/?a=1", "userAgent": map[string]any{"browser": "Chrome"}}
	if got := anonymize(meta, false); !reflect.DeepEqual(got, meta) {
		t.Fatalf("anonymize off changed %v", got)
	}
	got := anonymize(map[string]any{"userAgent": map[string]any{"browser": "Chrome", "custom": "kept"}, "custom": 1.0}, true)
	if !reflect.DeepEqual(got, map[string]any{"userAgent": map[string]any{"custom": "kept"}, "custom": 1.0}) {
		t.Fatalf("anonymize dropped what no catalog entry names: %v", got)
	}
}

func TestCreateSingleUse(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws, surveyOpts{singleUse: `{"enabled": true, "isEncrypted": false}`})
	ctx := context.Background()
	token := survey.SingleUseSignature(e.cfg.EncryptionKey, sv, "abc123")
	link := "https://app.example.com/s/" + sv.String() + "?suId=abc123&suToken=" + token

	cases := []struct {
		singleUseID *string
		url         *string
		message     string
		extra       map[string]any
	}{
		{nil, &link, "Missing single use id", nil},
		{ptr("abc123"), nil, "Missing or invalid URL in response metadata", nil},
		{ptr("abc123"), ptr("not a url"), "Invalid URL in response metadata", map[string]any{"error": "Invalid URL"}},
		{ptr("abc123"), ptr("https://app.example.com/s/x"), "Missing single use id", nil},
		{ptr("other"), &link, "Invalid single use id", nil},
		{ptr("abc123"), ptr("https://app.example.com/s/x?suId=abc123&suToken=bad"), "Invalid single use id", nil},
	}
	for _, c := range cases {
		_, err := e.submit(ctx, ws, sv, `{}`, CreateInput{SingleUseID: c.singleUseID, Meta: ClientMeta{URL: c.url}})
		d := map[string]any{"surveyId": sv.String(), "environmentId": ws.String()}
		for k, v := range c.extra {
			d[k] = v
		}
		appErr(t, err, http.StatusBadRequest, c.message, d)
	}

	res, err := e.submit(ctx, ws, sv, `{}`, CreateInput{SingleUseID: ptr("abc123"), Meta: ClientMeta{URL: &link}, Finished: true})
	if err != nil {
		t.Fatal(err)
	}
	if row := stored(e, res.ID); row["single_use_id"] != "abc123" {
		t.Fatalf("single use id %v", row["single_use_id"])
	}
	_, err = e.submit(ctx, ws, sv, `{}`, CreateInput{SingleUseID: ptr("abc123"), Meta: ClientMeta{URL: &link}})
	appErr(t, err, http.StatusConflict, "Response already submitted for this single-use link", map[string]any{})
}

func TestSingleUseV2ReportsTheWorkspace(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	svID := e.survey(ws, surveyOpts{singleUse: `{"enabled": true, "isEncrypted": true}`})
	sv, err := e.svc.Survey(context.Background(), svID.String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.SingleUseID(sv, ws, true, nil, nil)
	appErr(t, err, http.StatusBadRequest, "Missing single use id", map[string]any{"surveyId": svID.String(), "workspaceId": ws.String()})
	// An app survey, or a link survey without single-use links, passes the id through untouched.
	plain, err := e.svc.Survey(context.Background(), e.survey(ws, surveyOpts{}).String())
	if err != nil {
		t.Fatal(err)
	}
	if id, err := e.svc.SingleUseID(plain, ws, true, ptr("x"), nil); err != nil || *id != "x" {
		t.Fatalf("SingleUseID = %v, %v", id, err)
	}
}

func TestCreateRunsTheIngestContract(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws, surveyOpts{hiddenFieldIDs: []string{"plan", "locked", "seats"}})
	e.field(ws, sv, "Plan", "ingested", "string", false)
	e.field(ws, sv, "locked", "ingested", "string", true)
	e.field(ws, sv, "seats", "ingested", "number", false)
	e.field(ws, sv, "score", "computed", "number", false)
	res, err := e.submit(context.Background(), ws, sv, `{"q1":"answer","plan":"gold","locked":"x","seats":"many","score":"9","rogue":"y","q2":{"nested":{"deep":1}}}`, CreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	row := stored(e, res.ID)
	if want := map[string]any{"q1": "answer", "Plan": "gold", "seats": "many"}; !reflect.DeepEqual(row["data"], want) {
		t.Fatalf("data %v, want %v", row["data"], want)
	}
	if want := []any{map[string]any{"key": "seats", "reason": "coercion_failed"}}; !reflect.DeepEqual(row["ingest_flags"], want) {
		t.Fatalf("ingest flags %v", row["ingest_flags"])
	}
	if !contains(e.logs.String(), "Embedded Data ingest dropped or flagged incoming keys") {
		t.Fatal("the drops were not logged")
	}

	// A survey that declares hidden fields but has no ingested rows drops everything, loudly.
	bare := e.survey(ws, surveyOpts{hiddenFieldIDs: []string{"plan"}})
	res, err = e.submit(context.Background(), ws, bare, `{"plan":"gold"}`, CreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if data := stored(e, res.ID)["data"]; !reflect.DeepEqual(data, map[string]any{}) {
		t.Fatalf("data %v", data)
	}
	if !contains(e.logs.String(), "resolved no ingested fields for a survey that declares hidden fields") {
		t.Fatal("the fail-closed allow-list was not warned about")
	}
}
