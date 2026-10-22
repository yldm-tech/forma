package analysis

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

// The analysis views over a real database: forma2's getSurveySummaryAction, getSurveyFilterDataAction, getDisplaysWithContactAction, getResponsesDownloadUrlAction and resetSurveyAction, each authorized on the survey.

const summaryBlocks = `[
	{"id":"b1","name":"B1","elements":[
		{"id":"q1","type":"openText","headline":{"default":"Your name"},"required":false,"inputType":"text"},
		{"id":"nps","type":"nps","headline":{"default":"Recommend?"},"required":false}
	]},
	{"id":"b2","name":"B2","elements":[
		{"id":"pick","type":"multipleChoiceSingle","headline":{"default":"Pick"},"required":false,
			"choices":[{"id":"a","label":{"default":"A"}},{"id":"b","label":{"default":"B"}},{"id":"other","label":{"default":"Other"}}]}
	]}
]`

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func at(minutes int) time.Time { return t0.Add(time.Duration(minutes) * time.Minute) }

func TestSummaryCountsResponsesDisplaysAndQuotas(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	org, ws := e.organization(owner, nil)
	sv := e.survey(ws, surveyOpts{blocks: summaryBlocks})
	d1, d2, d3 := e.display(sv, nil, at(1)), e.display(sv, nil, at(2)), e.display(sv, nil, at(3))
	e.display(sv, nil, at(4))
	e.display(sv, nil, at(0))
	r1 := e.response(sv, row{at: at(1), finished: true, display: &d1, data: `{"q1":"Ada","nps":10,"pick":"A"}`, ttc: `{"q1":1000,"nps":2000,"pick":3000,"_total":6000}`})
	e.response(sv, row{at: at(2), finished: true, display: &d2, data: `{"q1":"Bob","nps":6,"pick":"Zed"}`, ttc: `{"q1":500,"nps":500,"pick":1000,"_total":2000}`})
	e.response(sv, row{at: at(3), display: &d3, data: `{"q1":"Cy"}`, ttc: `{"q1":400,"nps":800}`})
	e.response(sv, row{at: at(4)})
	quota := e.insert(`INSERT INTO survey_quotas (survey_id, name, "limit", logic, action) VALUES (?, 'Q', 1, '{"connector":"and","conditions":[]}'::jsonb, 'continueSurvey') RETURNING id`, sv)
	e.exec(`INSERT INTO response_quota_links (response_id, quota_id, status) VALUES (?, ?, 'screenedIn')`, r1, quota)

	var s struct {
		Meta    Meta              `json:"meta"`
		DropOff []DropOff         `json:"dropOff"`
		Summary []json.RawMessage `json:"summary"`
		Quotas  []QuotaProgress   `json:"quotas"`
	}
	decode(t, e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/summary", map[string]any{}), &s)

	wantMeta := Meta{DisplayCount: 5, TotalResponses: 4, StartsPercentage: 80, CompletedResponses: 2, CompletedPercentage: 40, DropOffCount: 2, DropOffPercentage: 50,
		TTCAverage: 2433.33, QuotasCompleted: 1, QuotasCompletedPercentage: 100}
	if s.Meta != wantMeta {
		t.Errorf("meta %+v, want %+v", s.Meta, wantMeta)
	}
	wantDropOff := []DropOff{
		{ElementID: "q1", ElementType: "openText", Headline: "Your name", TTC: 1100, Impressions: 5, DropOffCount: 2, DropOffPercentage: 40},
		{ElementID: "nps", ElementType: "nps", Headline: "Recommend?", TTC: 1100, Impressions: 3, DropOffCount: 1, DropOffPercentage: 33.33},
		{ElementID: "pick", ElementType: "multipleChoiceSingle", Headline: "Pick", TTC: 2000, Impressions: 2, DropOffCount: 0, DropOffPercentage: 0},
	}
	if !reflect.DeepEqual(s.DropOff, wantDropOff) {
		t.Errorf("drop-off %+v, want %+v", s.DropOff, wantDropOff)
	}
	if want := []QuotaProgress{{ID: quota, Name: "Q", Limit: 1, Count: 1, Percentage: 100}}; !reflect.DeepEqual(s.Quotas, want) {
		t.Errorf("quotas %+v, want %+v", s.Quotas, want)
	}
	if len(s.Summary) != 3 {
		t.Fatalf("summary has %d entries", len(s.Summary))
	}
	var text SamplesSummary
	if err := json.Unmarshal(s.Summary[0], &text); err != nil {
		t.Fatal(err)
	}
	if text.ResponseCount != 3 || len(text.Samples) != 3 || text.Samples[0].Value != "Cy" || text.Samples[2].Value != "Ada" {
		t.Errorf("open text summary %s", s.Summary[0])
	}
	var nps struct {
		Total      int   `json:"total"`
		Score      int   `json:"score"`
		Promoters  Count `json:"promoters"`
		Detractors Count `json:"detractors"`
		Dismissed  Count `json:"dismissed"`
	}
	if err := json.Unmarshal(s.Summary[1], &nps); err != nil {
		t.Fatal(err)
	}
	if nps.Total != 3 || nps.Score != 0 || nps.Promoters != (Count{1, 33.33}) || nps.Detractors != (Count{1, 33.33}) || nps.Dismissed != (Count{1, 33.33}) {
		t.Errorf("nps summary %s", s.Summary[1])
	}
	var pick struct {
		ResponseCount int `json:"responseCount"`
		Choices       []struct {
			Value      string  `json:"value"`
			Count      int     `json:"count"`
			Percentage float64 `json:"percentage"`
			Others     []Other `json:"others"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(s.Summary[2], &pick); err != nil {
		t.Fatal(err)
	}
	if pick.ResponseCount != 2 || len(pick.Choices) != 3 || pick.Choices[0].Count != 1 || pick.Choices[0].Percentage != 50 || pick.Choices[2].Value != "Other" ||
		len(pick.Choices[2].Others) != 1 || pick.Choices[2].Others[0].Value != "Zed" {
		t.Errorf("choice summary %s", s.Summary[2])
	}

	// Under a filter on anything but the creation time, only displays whose response matches count.
	decode(t, e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/summary", map[string]any{"filterCriteria": map[string]any{"finished": true}}), &s)
	if s.Meta.DisplayCount != 2 || s.Meta.TotalResponses != 2 || s.Meta.StartsPercentage != 100 || s.Meta.CompletedPercentage != 100 {
		t.Errorf("finished filter meta %+v", s.Meta)
	}
	// A date range bounds the displays by their own creation time, both ends included.
	decode(t, e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/summary", map[string]any{"filterCriteria": map[string]any{"createdAt": map[string]any{"min": at(2), "max": at(3)}}}), &s)
	if s.Meta.DisplayCount != 2 || s.Meta.TotalResponses != 2 || s.Meta.CompletedResponses != 1 {
		t.Errorf("date filter meta %+v", s.Meta)
	}
	// A malformed condition is refused.
	if rec := e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/summary", map[string]any{"filterCriteria": map[string]any{"data": map[string]any{"q1": map[string]any{"op": "nope"}}}}); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed filter: %d %s", rec.Code, rec.Body)
	}

	// Readers of the workspace may see the summary; anyone else, and anyone asking about a survey that does not exist, is not authorized.
	reader, stranger := e.user(), e.user()
	e.grant(org, ws, reader, "read")
	if rec := e.request(reader, http.MethodPost, "/api/app/surveys/"+sv.String()+"/summary", map[string]any{}); rec.Code != http.StatusOK {
		t.Errorf("reader: %d %s", rec.Code, rec.Body)
	}
	for _, c := range []struct {
		user   uuid.UUID
		survey uuid.UUID
	}{{stranger, sv}, {owner, uuid.New()}} {
		rec := e.request(c.user, http.MethodPost, "/api/app/surveys/"+c.survey.String()+"/summary", map[string]any{})
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Not authorized") {
			t.Errorf("forbidden summary: %d %s", rec.Code, rec.Body)
		}
	}
}

func TestFilterDataOffersTheValuesOfTheNewestResponses(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	org, ws := e.organization(owner, nil)
	sv := e.survey(ws, surveyOpts{blocks: summaryBlocks, captureIP: true})
	e.field(ws, sv, "utm", "utm", "ingested", "string")
	e.field(ws, sv, "plan", "plan", "computed", "string")
	e.field(ws, sv, "score", "score", "computed", "number")
	tag := e.insert(`INSERT INTO tags (workspace_id, name) VALUES (?, 'vip') RETURNING id`, ws)
	quota := e.insert(`INSERT INTO survey_quotas (survey_id, name, "limit", logic, action) VALUES (?, 'Q', 5, '{"connector":"and","conditions":[]}'::jsonb, 'continueSurvey') RETURNING id`, sv)
	for i := range 60 {
		e.response(sv, row{at: at(i), data: fmt.Sprintf(`{"utm":"src-%d","q1":"x"}`, i%3), variables: fmt.Sprintf(`{"plan":"p%d","score":%d}`, i, i),
			meta:              fmt.Sprintf(`{"url":"https://example.com/p%d?token=secret","userAgent":{"browser":"Chrome","os":"macOS"},"country":"DE","ipAddress":"10.0.0.1","screenWidth":1440}`, i),
			contactAttributes: ptr(fmt.Sprintf(`{"email":"u%d@example.com","seats":3}`, i%2))})
	}
	e.response(sv, row{at: at(100), data: `{"utm":42}`, meta: `{"url":5}`})

	var got FilterData
	decode(t, e.request(owner, http.MethodGet, "/api/app/surveys/"+sv.String()+"/filter-data", nil), &got)
	if len(got.EnvironmentTags) != 1 || got.EnvironmentTags[0].ID != tag || len(got.Quotas) != 1 || got.Quotas[0].ID != quota {
		t.Errorf("tags %+v, quotas %+v", got.EnvironmentTags, got.Quotas)
	}
	if want := map[string][]string{"email": {"u1@example.com", "u0@example.com"}, "seats": {"3"}}; !reflect.DeepEqual(got.Attributes, want) {
		t.Errorf("attributes %v", got.Attributes)
	}
	if got.Meta["url"] == nil || len(got.Meta["url"]) != 0 || !reflect.DeepEqual(got.Meta["browser"], []string{"Chrome"}) || !reflect.DeepEqual(got.Meta["country"], []string{"DE"}) {
		t.Errorf("meta %v", got.Meta)
	}
	if want := map[string][]string{"utm": {"src-2", "src-1", "src-0"}}; !reflect.DeepEqual(got.HiddenFields, want) {
		t.Errorf("hidden fields %v", got.HiddenFields)
	}
	// Reserved values go through the projection: a number read as text, the URL without its query, at most 50 per field; the IP address only because the survey captures it.
	if n := len(got.ReservedValues["url"]); n != 50 || got.ReservedValues["url"][0] != "5" || got.ReservedValues["url"][1] != "https://example.com/p59" {
		t.Errorf("reserved url values %d %v", n, got.ReservedValues["url"])
	}
	if !reflect.DeepEqual(got.ReservedValues["ipAddress"], []string{"10.0.0.1"}) || got.ReservedValues["screenWidth"] != nil {
		t.Errorf("reserved values %v", got.ReservedValues)
	}
	if n := len(got.VariableValues["plan"]); n != 50 || got.VariableValues["score"] != nil {
		t.Errorf("variable values %v", got.VariableValues)
	}

	// The workspace's tags and the values its respondents left are for readers of the workspace; anyone else, and anyone asking about a survey that does not exist, is not authorized.
	expectReadableOnlyByTheWorkspace(t, e, org, ws, "/api/app/surveys/%s/filter-data", sv)
}

// expectReadableOnlyByTheWorkspace checks a survey's read endpoint, its path a format of the survey id: a reader of the workspace gets it, a user of another organization and a request for an unknown survey get 403 "Not authorized".
func expectReadableOnlyByTheWorkspace(t *testing.T, e *env, org, ws uuid.UUID, path string, sv uuid.UUID) {
	t.Helper()
	reader, stranger := e.user(), e.user()
	e.grant(org, ws, reader, "read")
	e.organization(stranger, nil)
	if rec := e.request(reader, http.MethodGet, fmt.Sprintf(path, sv), nil); rec.Code != http.StatusOK {
		t.Errorf("reader: %d %s", rec.Code, rec.Body)
	}
	for _, c := range []struct {
		name   string
		user   uuid.UUID
		survey uuid.UUID
	}{{"stranger", stranger, sv}, {"unknown survey", e.user(), uuid.New()}, {"reader, unknown survey", reader, uuid.New()}} {
		rec := e.request(c.user, http.MethodGet, fmt.Sprintf(path, c.survey), nil)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Not authorized") {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}
}

func TestDisplaysListsTheContactsWhoSawTheSurvey(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	org, ws := e.organization(owner, nil)
	sv := e.survey(ws, surveyOpts{blocks: summaryBlocks})
	ada := e.contact(ws, "userId", "u-1", "email", "ada@example.com", "plan", "pro")
	bob := e.contact(ws, "userId", "u-2")
	d1 := e.display(sv, &ada, at(1))
	e.display(sv, nil, at(2))
	d3 := e.display(sv, &bob, at(3))

	var got []DisplayWithContact
	decode(t, e.request(owner, http.MethodGet, "/api/app/surveys/"+sv.String()+"/displays?limit=15&offset=0", nil), &got)
	if len(got) != 2 || got[0].ID != d3 || got[1].ID != d1 {
		t.Fatalf("displays %+v", got)
	}
	if !reflect.DeepEqual(got[1].Contact.Attributes, map[string]string{"userId": "u-1", "email": "ada@example.com"}) || got[0].Contact.ID != bob {
		t.Errorf("contacts %+v %+v", got[0].Contact, got[1].Contact)
	}
	decode(t, e.request(owner, http.MethodGet, "/api/app/surveys/"+sv.String()+"/displays?limit=1&offset=1", nil), &got)
	if len(got) != 1 || got[0].ID != d1 {
		t.Errorf("second page %+v", got)
	}
	for _, q := range []string{"limit=0&offset=0", "limit=101&offset=0", "limit=10&offset=-1", "limit=10"} {
		if rec := e.request(owner, http.MethodGet, "/api/app/surveys/"+sv.String()+"/displays?"+q, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", q, rec.Code, rec.Body)
		}
	}

	// The contacts' emails and user ids are for readers of the workspace only.
	expectReadableOnlyByTheWorkspace(t, e, org, ws, "/api/app/surveys/%s/displays?limit=15&offset=0", sv)
}

const exportBlocks = `[{"id":"b1","name":"B1","elements":[
	{"id":"q1","type":"openText","headline":{"default":"Your name"},"required":false,"inputType":"text"},
	{"id":"pick","type":"multipleChoiceSingle","headline":{"default":"Pick <b>#recall:q1/fallback:you#</b>"},"required":false,
		"choices":[{"id":"a","label":{"default":"A","de":"Ä"}},{"id":"other","label":{"default":"Other"}}]},
	{"id":"mx","type":"matrix","headline":{"default":"Rate"},"required":false,
		"rows":[{"id":"r1","label":{"default":"Speed"}},{"id":"r2","label":{"default":"Price"}}],
		"columns":[{"id":"c1","label":{"default":"Good"}},{"id":"c2","label":{"default":"Bad"}}]},
	{"id":"f","type":"fileUpload","headline":{"default":"Upload"},"required":false}
]}]`

// exportFixture seeds the export's survey: two responses, the older one with a contact, a tag, a quota, reserved values and a file upload, in an organization showing times in Manila.
func exportFixture(t *testing.T) (e *env, owner, sv, r1, r2, contact uuid.UUID) {
	e = newEnv(t)
	owner = e.user()
	_, ws := e.organization(owner, ptr("Asia/Manila"))
	sv = e.survey(ws, surveyOpts{name: "Q3 Feedback: Café!", blocks: exportBlocks, verifyEmail: true})
	e.field(ws, sv, "utm", "utm", "ingested", "string")
	e.field(ws, sv, "score_key", "Score", "computed", "number")
	contact = e.contact(ws, "userId", "u-1")
	r1 = e.response(sv, row{at: time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC), finished: true, contact: &contact, language: ptr("de"),
		data:              `{"q1":"=SUM(A1)","pick":"Ä","mx":{"Speed":"Good"},"f":["/storage/ws/public/a.png","/storage/ws/private/b.pdf"],"utm":"newsletter","verifiedEmail":"ada@example.com"}`,
		meta:              `{"url":"https://example.com/p?secret=1","userAgent":{"browser":"Chrome","os":"macOS","device":"desktop"},"country":"DE","screenWidth":1440}`,
		ttc:               `{"_total":12500}`,
		variables:         `{"score_key":7}`,
		contactAttributes: ptr(`{"userId":"u-1","email":"ada@example.com"}`)})
	r2 = e.response(sv, row{at: time.Date(2026, 1, 2, 8, 30, 0, 0, time.UTC), data: `{"pick":"Zed","utm":42}`})
	tag := e.insert(`INSERT INTO tags (workspace_id, name) VALUES (?, 'vip') RETURNING id`, ws)
	e.exec(`INSERT INTO response_tags (response_id, tag_id) VALUES (?, ?)`, r1, tag)
	quota := e.insert(`INSERT INTO survey_quotas (survey_id, name, "limit", logic, action) VALUES (?, 'Q', 5, '{"connector":"and","conditions":[]}'::jsonb, 'continueSurvey') RETURNING id`, sv)
	e.exec(`INSERT INTO response_quota_links (response_id, quota_id, status) VALUES (?, ?, 'screenedIn')`, r1, quota)
	return e, owner, sv, r1, r2, contact
}

var exportHeaders = []string{"No.", "Response ID", "Timestamp", "Finished", "Quotas", "Survey ID", "Forma ID (internal)", "User ID", "Tags",
	"Source", "Url", "Country", "Action", "Browser", "Os", "Device Type", "Language", "Duration Seconds", "Finished At", "Page Path", "Page Referrer",
	"Utm Source", "Utm Medium", "Utm Campaign", "Utm Term", "Utm Content", "Screen Width", "Screen Height", "Viewport Width", "Viewport Height", "Timezone", "Locale",
	"1. Your name", "2. Pick @Your name", "2. Pick @Your name - Option ID", "3. Rate - Speed", "3. Rate - Price", "4. Upload",
	"Score", "utm", "person.email", "person.userId", "Verified Email"}

func TestExportWritesForma2sColumnsAndCells(t *testing.T) {
	e, owner, sv, r1, r2, contact := exportFixture(t)
	q := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
	header := make([]string, len(exportHeaders))
	for i, h := range exportHeaders {
		header[i] = q(h)
	}
	empty := q("")
	newer := []string{"1", q(r2.String()), q("2026-01-02 16:30:00 GMT+8"), q("No"), empty, q(sv.String()), empty, empty, empty,
		empty, empty, empty, empty, empty, empty, empty, empty, empty, q("2026-01-02T08:30:00.000Z"), empty, empty,
		empty, empty, empty, empty, empty, empty, empty, empty, empty, empty, empty,
		empty, q("Zed"), q("other"), "", "", empty,
		"", "42", empty, empty, empty}
	older := []string{"2", q(r1.String()), q("2026-01-02 04:00:00 GMT+8"), q("Yes"), q("Q"), q(sv.String()), q(contact.String()), q("u-1"), q("vip"),
		empty, q("https://example.com/p"), q("DE"), empty, q("Chrome"), q("macOS"), q("desktop"), q("de"), "13", q("2026-01-01T20:00:00.000Z"), empty, empty,
		empty, empty, empty, empty, empty, "1440", empty, empty, empty, empty, empty,
		q("'=SUM(A1)"), q("Ä"), q("a"), q("Good"), empty, q("https://surveys.example.com/storage/ws/public/a.png; http://app.example.com/storage/ws/private/b.pdf"),
		"7", q("newsletter"), q("ada@example.com"), q("u-1"), q("ada@example.com")}
	want := "\ufeff" + strings.Join(header, ",") + "\n" + strings.Join(newer, ",") + "\n" + strings.Join(older, ",")

	rec := e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/responses/export", map[string]any{"format": "csv", "filterCriteria": map[string]any{}})
	if rec.Code != http.StatusOK {
		t.Fatalf("export: %d %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != ContentTypeCSV {
		t.Errorf("content type %q", ct)
	}
	cd := rec.Header().Get("Content-Disposition")
	if !strings.HasPrefix(cd, `attachment; filename="export-q3_feedback_caf_-`) || !strings.HasSuffix(cd, `-utc.csv"`) {
		t.Errorf("content disposition %q", cd)
	}
	if got := rec.Body.String(); got != want {
		gotLines, wantLines := strings.Split(got, "\n"), strings.Split(want, "\n")
		for i := range wantLines {
			if i >= len(gotLines) || gotLines[i] != wantLines[i] {
				t.Errorf("line %d:\n got %s\nwant %s", i, gotLines[min(i, len(gotLines)-1)], wantLines[i])
			}
		}
	}

	// Filtered by response ids, as the table's bulk download asks.
	rec = e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/responses/export", map[string]any{"format": "csv", "filterCriteria": map[string]any{"responseIds": []string{r2.String()}}})
	if lines := strings.Split(rec.Body.String(), "\n"); rec.Code != http.StatusOK || len(lines) != 2 || !strings.HasPrefix(lines[1], `1,"`+r2.String()) {
		t.Errorf("filtered export: %d %s", rec.Code, rec.Body)
	}
	// Without the contact snapshot among the exported responses, no person columns.
	if strings.Contains(rec.Body.String(), "person.") {
		t.Errorf("filtered export has person columns: %s", strings.Split(rec.Body.String(), "\n")[0])
	}
}

func TestExportXLSXHasOneSheetWithTypedCells(t *testing.T) {
	e, owner, sv, r1, _, _ := exportFixture(t)
	rec := e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/responses/export", map[string]any{"format": "xlsx", "filterCriteria": map[string]any{}})
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != ContentTypeXLSX || !strings.HasSuffix(rec.Header().Get("Content-Disposition"), `-utc.xlsx"`) {
		t.Fatalf("export: %d %v", rec.Code, rec.Header())
	}
	f, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if sheets := f.GetSheetList(); !reflect.DeepEqual(sheets, []string{"Sheet1"}) {
		t.Fatalf("sheets %v", sheets)
	}
	rows, err := f.GetRows("Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || !reflect.DeepEqual(rows[0], exportHeaders) {
		t.Fatalf("rows %v", rows)
	}
	cell := func(row int, header string) (string, excelize.CellType) {
		col := 0
		for i, h := range exportHeaders {
			if h == header {
				col = i + 1
			}
		}
		name, _ := excelize.CoordinatesToCellName(col, row)
		v, _ := f.GetCellValue("Sheet1", name)
		typ, _ := f.GetCellType("Sheet1", name)
		return v, typ
	}
	for _, c := range []struct {
		row    int
		header string
		value  string
		typ    excelize.CellType
	}{
		{2, "No.", "1", excelize.CellTypeUnset},
		{3, "Response ID", r1.String(), excelize.CellTypeInlineString},
		{3, "Duration Seconds", "13", excelize.CellTypeUnset},
		{3, "1. Your name", "'=SUM(A1)", excelize.CellTypeInlineString},
		{2, "utm", "42", excelize.CellTypeUnset},
		{2, "3. Rate - Speed", "", excelize.CellTypeUnset},
		{3, "Score", "7", excelize.CellTypeUnset},
	} {
		if v, typ := cell(c.row, c.header); v != c.value || typ != c.typ {
			t.Errorf("row %d %s: %q (%v), want %q (%v)", c.row, c.header, v, typ, c.value, c.typ)
		}
	}
}

func TestExportNeedsAFormatAndCriteria(t *testing.T) {
	e, owner, sv, _, _, _ := exportFixture(t)
	for _, body := range []map[string]any{{"format": "pdf", "filterCriteria": map[string]any{}}, {"format": "csv"}} {
		if rec := e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/responses/export", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: %d %s", body, rec.Code, rec.Body)
		}
	}
	if rec := e.request(e.user(), http.MethodPost, "/api/app/surveys/"+sv.String()+"/responses/export", map[string]any{"format": "csv", "filterCriteria": map[string]any{}}); rec.Code != http.StatusForbidden {
		t.Errorf("stranger: %d %s", rec.Code, rec.Body)
	}
}

func TestResetDeletesResponsesAndDisplaysAndQueuesTheirFiles(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	org, ws := e.organization(owner, nil)
	sv := e.survey(ws, surveyOpts{blocks: exportBlocks})
	other := e.survey(ws, surveyOpts{blocks: exportBlocks})
	for i := range 3 {
		d := e.display(sv, nil, at(i))
		urls := make([]string, 0, 60)
		for j := range 60 {
			urls = append(urls, fmt.Sprintf("/storage/%s/public/%d-%d.png", ws, i, j))
		}
		data, _ := json.Marshal(map[string]any{"f": urls, "q1": "x"})
		e.response(sv, row{at: at(i), display: &d, data: string(data)})
	}
	e.display(sv, nil, at(9))
	kept := e.response(other, row{at: at(1), data: `{"f":["/storage/x/public/keep.png"]}`})

	reader := e.user()
	e.grant(org, ws, reader, "read")
	if rec := e.request(reader, http.MethodPost, "/api/app/surveys/"+sv.String()+"/reset", nil); rec.Code != http.StatusForbidden {
		t.Errorf("reader reset: %d %s", rec.Code, rec.Body)
	}

	var got ResetResult
	decode(t, e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/reset", nil), &got)
	if want := (ResetResult{Success: true, DeletedResponsesCount: 3, DeletedDisplaysCount: 4}); got != want {
		t.Errorf("reset %+v, want %+v", got, want)
	}
	if n := e.count(`SELECT count(*) FROM responses WHERE survey_id = ?`, sv) + e.count(`SELECT count(*) FROM displays WHERE survey_id = ?`, sv); n != 0 {
		t.Errorf("%d rows left", n)
	}
	if n := e.count(`SELECT count(*) FROM responses WHERE id = ?`, kept); n != 1 {
		t.Error("another survey's response was deleted")
	}
	var chunks []int
	if err := e.db.Raw(`SELECT jsonb_array_length(args->'fileUrls') FROM river_job WHERE kind = 'response.delete-files' AND args->>'workspaceId' = ? ORDER BY id`, ws.String()).Scan(&chunks).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(chunks, []int{100, 80}) {
		t.Errorf("file cleanup chunks %v", chunks)
	}

	archived := e.survey(ws, surveyOpts{blocks: exportBlocks, archived: true})
	rec := e.request(owner, http.MethodPost, "/api/app/surveys/"+archived.String()+"/reset", nil)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Cannot reset an archived survey.") || !strings.Contains(rec.Body.String(), "operation_not_allowed") {
		t.Errorf("archived reset: %d %s", rec.Code, rec.Body)
	}
}

// A reset reads the responses' files in pages of 500 in (created_at, id) order, as forma2 did, so a large survey's data is never in memory at once; responses sharing a creation time across a page boundary are neither skipped nor read twice.
func TestResetReadsResponseFilesInPages(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner, nil)
	sv := e.survey(ws, surveyOpts{blocks: exportBlocks})
	const n = 2*responseFileScanPage + 201
	e.exec(`INSERT INTO responses (survey_id, created_at, updated_at, data, ttc, meta, variables)
		SELECT ?, ?::timestamptz + (g / 7) * interval '1 second', ?::timestamptz, jsonb_build_object('f', jsonb_build_array('/storage/' || ? || '/private/surveys/s/elements/f/' || g || '.pdf')), '{}', '{}', '{}'
		FROM generate_series(1, ?) AS g`, sv, t0, t0, ws.String(), n)

	var scans atomic.Int32
	if err := e.db.Callback().Row().After("gorm:row").Register("test:count_response_scans", func(db *gorm.DB) {
		if sql := db.Statement.SQL.String(); strings.Contains(sql, "FROM responses") && strings.Contains(sql, "FOR UPDATE") {
			scans.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	var got ResetResult
	decode(t, e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/reset", nil), &got)
	if got.DeletedResponsesCount != n {
		t.Fatalf("reset %+v", got)
	}
	if s := scans.Load(); s != 3 {
		t.Errorf("%d scans of the responses, want 3 pages", s)
	}
	var lists []string
	if err := e.db.Raw(`SELECT args->>'fileUrls' FROM river_job WHERE kind = 'response.delete-files' ORDER BY id`).Scan(&lists).Error; err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, l := range lists {
		var urls []string
		if err := json.Unmarshal([]byte(l), &urls); err != nil {
			t.Fatal(err)
		}
		for _, u := range urls {
			if seen[u] {
				t.Fatalf("%s queued twice", u)
			}
			seen[u] = true
		}
	}
	if len(seen) != n || len(lists) != (n+storageDeleteChunk-1)/storageDeleteChunk {
		t.Errorf("%d files in %d jobs, want %d", len(seen), len(lists), n)
	}
}
