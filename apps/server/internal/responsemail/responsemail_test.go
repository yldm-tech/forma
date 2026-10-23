package responsemail

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/server/internal/response"
)

// The expected outputs of forma2's survey-response-email.test.ts. bluemonday writes a void element as <br/> where sanitize-html wrote <br />; mail clients read both the same.
func TestSanitizeBody(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{`<p>Hi Jane</p><script>alert("x")</script><img src=x>`, `<p>Hi Jane</p>`},
		{
			`<p class="fb-editor-paragraph">What I eat in a day</p><ol class="fb-editor-list-ol" start="2"><li value="2" class="fb-editor-listitem"><span>Banana</span></li><li value="3" class="fb-editor-listitem"><span>Mango</span></li></ol>`,
			`<p class="fb-editor-paragraph">What I eat in a day</p><ol class="fb-editor-list-ol" start="2"><li value="2" class="fb-editor-listitem"><span>Banana</span></li><li value="3" class="fb-editor-listitem"><span>Mango</span></li></ol>`,
		},
		{`<ul><li>Fruit<ul><li>Banana</li></ul></li><li>Bread</li></ul>`, `<ul><li>Fruit<ul><li>Banana</li></ul></li><li>Bread</li></ul>`},
		{
			`<ul><li><b><strong>bold</strong></b> <i><em>italic</em></i><br /><a href="https://forma.yldm.ai">link</a></li></ul>`,
			`<ul><li><b><strong>bold</strong></b> <i><em>italic</em></i><br/><a href="https://forma.yldm.ai">link</a></li></ul>`,
		},
		{
			`<p style="color:red" onclick="steal()"><a href="https://forma.yldm.ai" target="_blank" rel="noopener">ok</a><a href="javascript:alert(1)">bad</a></p>`,
			`<p><a href="https://forma.yldm.ai" target="_blank" rel="noopener">ok</a><a>bad</a></p>`,
		},
		{`<p dir="rtl"><a href="/relative">r</a><a href="mailto:a@example.com">m</a></p>`, `<p dir="rtl"><a href="/relative">r</a><a>m</a></p>`},
	}
	for _, tc := range cases {
		if got := string(SanitizeBody(tc.in, nil, nil, "en-US")); got != tc.want {
			t.Errorf("SanitizeBody(%q)\n got %q\nwant %q", tc.in, got, tc.want)
		}
	}
	got := string(SanitizeBody(`<script>alert(1)</script><style>p{}</style><iframe src=x></iframe><img src="x" onerror="alert(1)" /><table><tr><td>cell</td></tr></table>`, nil, nil, "en-US"))
	for _, absent := range []string{"<script", "<style", "<iframe", "<img", "<table", "<tr", "<td", "onerror", "alert(1)", "p{}"} {
		if strings.Contains(got, absent) {
			t.Errorf("sanitized body kept %q: %q", absent, got)
		}
	}
	if !strings.Contains(got, "cell") {
		t.Errorf("the text of a dropped table must stay: %q", got)
	}
}

// A respondent's answer is escaped as it is recalled into the body, so the allowlist that keeps the author's links cannot keep one smuggled in through an answer.
func TestSanitizeBodyEscapesRecalledAnswers(t *testing.T) {
	t.Parallel()
	values := map[string]any{"name": `<a href="https://evil.example">click</a>`}
	got := string(SanitizeBody(`<p>Hi #recall:name/fallback:there#</p>`, values, nil, "en-US"))
	if got != `<p>Hi &lt;a href=&#34;https://evil.example&#34;&gt;click&lt;/a&gt;</p>` {
		t.Fatalf("got %q", got)
	}
}

func TestParseRecall(t *testing.T) {
	t.Parallel()
	values := map[string]any{
		"name": "Jane", "plan": "from data", "when": "2026-01-05", "legacy": "5-1-2026", "notADate": "2026-02-30",
		"multi": []any{"a", "", "b", nil}, "empty": "", "address": map[string]any{"city": "Berlin", "street": ""}, "n": 7.0,
		"loop": "#recall:loop/fallback:x#",
	}
	variables := map[string]any{"plan": "pro"}
	cases := map[string]string{
		"Hi #recall:name/fallback:there#!":                 "Hi Jane!",
		"#recall:plan/fallback:x#":                         "pro",
		"#recall:missing/fallback:dear nbspfriend#":        "dear  friend",
		"#recall:empty/fallback:fb#":                       "fb",
		"#recall:when/fallback:x#":                         "Monday, January 5, 2026",
		"#recall:legacy/fallback:x#":                       "Monday, January 5, 2026",
		"#recall:notADate/fallback:x#":                     "2026-02-30",
		"#recall:multi/fallback:x#":                        "a, b",
		"#recall:address/fallback:x#":                      "Berlin",
		"#recall:n/fallback:x#":                            "7",
		"#recall:loop/fallback:x#":                         "#recall:loop/fallback:x#",
		"#recall:unterminated":                             "#recall:unterminated",
		"$& #recall:name/fallback:x# $'":                   "$& Jane $'",
		"#recall:name/fallback:a##recall:plan/fallback:b#": "Janepro",
	}
	for in, want := range cases {
		if got := ParseRecall(in, values, variables, "en-US", false); got != want {
			t.Errorf("ParseRecall(%q) = %q, want %q", in, got, want)
		}
	}
	for locale, want := range map[string]string{"ja-JP": "2026年1月5日月曜日", "zh-Hans-CN": "2026年1月5日星期一", "zh-Hant-TW": "2026年1月5日 星期一", "de-DE": "Monday, January 5, 2026"} {
		if got := ParseRecall("#recall:when/fallback:x#", values, nil, locale, false); got != want {
			t.Errorf("%s date = %q, want %q", locale, got, want)
		}
	}
	if got := ParseRecall(`#recall:x/fallback:a"b#`, map[string]any{"x": `<b>&'`}, nil, "en-US", true); got != "&lt;b&gt;&amp;&#39;" {
		t.Errorf("escaped = %q", got)
	}
}

func TestResolveRecipient(t *testing.T) {
	t.Parallel()
	a := &Answers{Data: map[string]any{
		"email": "jane@example.com", "bad": "not-an-email", "contact": []any{"Jane", "Doe", "jane@example.com", "+123"},
		"short": []any{"Jane", "Doe"}, "blank": []any{"Jane", "Doe", ""}, "badContact": []any{"a", "b", "nope"}, "num": 3.0,
		"verifiedEmail": "verified@example.com",
	}}
	cases := map[string][2]string{
		"teammate@example.com": {"teammate@example.com", ""},
		"email":                {"jane@example.com", ""},
		"verifiedEmail":        {"verified@example.com", ""},
		"contact":              {"jane@example.com", ""},
		"missing":              {"", ReasonValueNotFound},
		"bad":                  {"", ReasonInvalid},
		"short":                {"", ReasonNoEmail},
		"blank":                {"", ReasonNoEmail},
		"badContact":           {"", ReasonInvalid},
		"num":                  {"", ReasonInvalid},
	}
	for to, want := range cases {
		email, reason := a.ResolveRecipient(to)
		if email != want[0] || reason != want[1] {
			t.Errorf("ResolveRecipient(%q) = %q, %q; want %q, %q", to, email, reason, want[0], want[1])
		}
	}
}

func TestLanguageCode(t *testing.T) {
	t.Parallel()
	de, en, unknown, empty := "DE", "en", "fr", ""
	sv := &Survey{Languages: []Language{{Code: "en", Default: true}, {Code: "de"}}}
	for in, want := range map[*string]string{&de: "de", &en: "default", &unknown: "default", &empty: "default", nil: "default"} {
		if got := sv.LanguageCode(in); got != want {
			t.Errorf("LanguageCode(%v) = %q, want %q", in, got, want)
		}
	}
	if got := (&Survey{}).LanguageCode(&de); got != "default" {
		t.Errorf("a survey without languages = %q", got)
	}
}

// seed is a survey with every element kind the email renders, a German translation, one computed and one ingested field, and a response to it.
func seed(t *testing.T, db *gorm.DB) (*Survey, *response.Response) {
	t.Helper()
	exec := func(q string, args ...any) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := db.Raw(q, args...).Row().Scan(&id); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return id
	}
	org := exec(`INSERT INTO organizations (name) VALUES ('Acme') RETURNING id`)
	ws := exec(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'W') RETURNING id`, org)
	blocks := `[{"id":"b1","name":"Block 1","elements":[
		{"id":"q1","type":"openText","headline":{"default":"<p>Hi #recall:hidden1/fallback:you#, what do you think?</p>","de":"Was denken Sie?"},"required":false,"inputType":"text","charLimit":{"enabled":false}},
		{"id":"q2","type":"fileUpload","headline":{"default":"Upload"},"required":false,"allowMultipleFiles":true},
		{"id":"q3","type":"pictureSelection","headline":{"default":"Pick"},"required":false,"allowMulti":true,"choices":[{"id":"c1","imageUrl":"/storage/ws/public/cat.png"},{"id":"c2","imageUrl":"https://cdn.example.com/dog.png"}]},
		{"id":"q4","type":"ranking","headline":{"default":"Rank"},"required":false,"choices":[{"id":"r1","label":{"default":"A"}}]},
		{"id":"q5","type":"address","headline":{"default":"Address"},"required":false},
		{"id":"q6","type":"nps","headline":{"default":"Score"},"required":false,"lowerLabel":{"default":"low"},"upperLabel":{"default":"high"}},
		{"id":"q7","type":"openText","headline":{"default":"Skipped"},"required":false,"inputType":"text"},
		{"id":"q8","type":"pictureSelection","headline":{"default":"Unanswered pictures"},"required":false,"choices":[{"id":"c1","imageUrl":"https://cdn.example.com/x.png"}]}
	]}]`
	sid := exec(`INSERT INTO surveys (workspace_id, name, blocks) VALUES (?, 'Feedback', ?::jsonb) RETURNING id`, ws, blocks)
	en := exec(`INSERT INTO languages (workspace_id, code, alias) VALUES (?, 'en', NULL) RETURNING id`, ws)
	de := exec(`INSERT INTO languages (workspace_id, code, alias) VALUES (?, 'de', NULL) RETURNING id`, ws)
	if err := db.Exec(`INSERT INTO survey_languages (survey_id, language_id, is_default) VALUES (?, ?, true), (?, ?, false)`, sid, en, sid, de).Error; err != nil {
		t.Fatal(err)
	}
	for i, f := range []struct{ key, name, source, dataType string }{{"score", "Score", "computed", "number"}, {"tier", "Tier", "computed", "string"}, {"hidden1", "Hidden One", "ingested", "string"}, {"hidden2", "Hidden Two", "ingested", "string"}} {
		def := exec(`INSERT INTO embedded_data (workspace_id, survey_id, name, source, data_type) VALUES (?, ?, ?, ?, ?) RETURNING id`, ws, sid, f.name, f.source, f.dataType)
		if err := db.Exec(`INSERT INTO survey_embedded_data (workspace_id, survey_id, embedded_data_id, storage_key, position) VALUES (?, ?, ?, ?, ?)`, ws, sid, def, f.key, i).Error; err != nil {
			t.Fatal(err)
		}
	}
	sv, err := LoadSurvey(context.Background(), db, sid)
	if err != nil || sv == nil {
		t.Fatalf("LoadSurvey = %v, %v", sv, err)
	}
	now := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
	r := &response.Response{
		ID: uuid.New(), CreatedAt: now, UpdatedAt: now, SurveyID: sid, Finished: true,
		Data:      json.RawMessage(`{"q1":"Great","q2":"/storage/ws/private/report.pdf","q3":["c2","c1","gone"],"q4":["A","","B"],"q5":{"street":"Main 1","zip":"","city":"Berlin"},"q6":9,"hidden1":"Ada","hidden2":""}`),
		Variables: json.RawMessage(`{"score":0,"tier":true}`),
		TTC:       json.RawMessage(`{}`), Meta: json.RawMessage(`{}`),
	}
	return sv, r
}

func TestElementsAndBlocks(t *testing.T) {
	t.Parallel()
	db := pgtest.GORM(t)
	sv, r := seed(t, db)
	a, err := Decode(r)
	if err != nil {
		t.Fatal(err)
	}
	elements, err := a.Elements(sv)
	if err != nil {
		t.Fatal(err)
	}
	want := []mailer.ResponseElement{
		{Label: "Hi Ada, what do you think?", Type: "openText", Text: "Great"},
		{Label: "Upload", Type: "fileUpload", Items: []string{"/storage/ws/private/report.pdf"}},
		{Label: "Pick", Type: "pictureSelection", Items: []string{"https://cdn.example.com/dog.png", "/storage/ws/public/cat.png"}},
		{Label: "Rank", Type: "ranking", Items: []string{"A", "", "B"}},
		{Label: "Address", Type: "address", Text: "street: Main 1\ncity: Berlin"},
		{Label: "Score", Type: "nps", Text: "9"},
		{Label: "Skipped", Type: "openText"},
		{Label: "Unanswered pictures", Type: "pictureSelection", Items: []string{}},
	}
	if !reflect.DeepEqual(elements, want) {
		t.Fatalf("elements =\n%#v\nwant\n%#v", elements, want)
	}
	german := "de"
	r.Language = &german
	a, _ = Decode(r)
	if elements, _ := a.Elements(sv); elements[0].Label != "Was denken Sie?" || elements[1].Label != "" {
		t.Errorf("German labels = %q, %q", elements[0].Label, elements[1].Label)
	}
	if got := a.ComputedValues(sv); !reflect.DeepEqual(got, []mailer.EmbeddedValue{{Name: "Score", Number: true, Value: "0"}}) {
		t.Errorf("computed = %#v", got)
	}
	if got := a.HiddenValues(sv); !reflect.DeepEqual(got, []mailer.HiddenValue{{ID: "hidden1", Value: "Ada"}}) {
		t.Errorf("hidden = %#v", got)
	}

	c, err := a.FollowUpContent(sv, EmailOptions{Subject: "S", Body: "<p>Hi #recall:hidden1/fallback:x#</p>", PublicBase: "https://surveys.example.com", LogoURL: "/storage/ws/public/logo.png"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Body != "<p>Hi Ada</p>" || c.ResponseData != nil || c.Variables != nil || c.HiddenFields != nil || c.LogoURL != "https://surveys.example.com/storage/ws/public/logo.png" {
		t.Errorf("without response data = %#v", c)
	}
	c, _ = a.FollowUpContent(sv, EmailOptions{Body: "b", AttachResponseData: true, IncludeHiddenFields: true, PublicBase: "https://surveys.example.com"})
	if c.ResponseData[1].Items[0] != "https://surveys.example.com/storage/ws/private/report.pdf" || c.ResponseData[2].Items[1] != "https://surveys.example.com/storage/ws/public/cat.png" {
		t.Errorf("storage URLs not resolved: %#v", c.ResponseData[1:3])
	}
	if c.Variables != nil || len(c.HiddenFields) != 1 {
		t.Errorf("variables must wait for includeVariables: %#v", c)
	}
}
