package ai

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/valkey/valkeytest"
)

// exampleBlocks is a survey with an open text, a single choice, a rating, an NPS and a CTA, which no answer can be synthesized for.
const exampleBlocks = `[{"id":"b1","name":"Main","elements":[
	{"id":"why","type":"openText","headline":{"default":"<p>What made you <b>sign up</b>?</p>"},"required":true,"inputType":"text"},
	{"id":"plan","type":"multipleChoiceSingle","headline":{"default":"Which plan?"},"required":true,"choices":[{"id":"c1","label":{"default":"Free"}},{"id":"c2","label":{"default":"Pro"}}]},
	{"id":"stars","type":"rating","headline":{"default":"Rate us"},"required":true,"scale":"star","range":5},
	{"id":"nps","type":"nps","headline":{"default":"Recommend us?"},"required":true},
	{"id":"cta","type":"cta","headline":{"default":"Read more"},"required":false}]}]`

const exampleEndings = `[{"id":"end1","type":"endScreen","headline":{"default":"Thanks"}}]`

// exampleSurvey creates a survey of the workspace with the given blocks and an English default language.
func (e *serviceEnv) exampleSurvey(ws uuid.UUID, blocks string) uuid.UUID {
	e.t.Helper()
	survey := e.insert(`INSERT INTO surveys (workspace_id, name, blocks, endings) VALUES (?, 'Onboarding', ?::jsonb, ?::jsonb) RETURNING id`, ws, blocks, exampleEndings)
	language := e.insert(`INSERT INTO languages (workspace_id, code) VALUES (?, 'en') ON CONFLICT (workspace_id, code) DO UPDATE SET code = EXCLUDED.code RETURNING id`, ws)
	e.exec(`INSERT INTO survey_languages (survey_id, language_id, is_default, enabled) VALUES (?, ?, true, true)`, survey, language)
	return survey
}

// answerOpenTexts makes the provider answer every requested open text with "<rowId>/<elementId>", read from the prompt's context.
func (e *serviceEnv) answerOpenTexts() {
	e.provider.mu.Lock()
	defer e.provider.mu.Unlock()
	e.provider.answer = func(body map[string]any) (int, string) {
		prompt, _ := jsonPath(body, "messages.1.content").(string)
		_, raw, _ := strings.Cut(prompt, "Survey context (JSON):\n")
		var ctx struct {
			Rows []struct {
				RowID     string `json:"rowId"`
				Requested []struct {
					ElementID string `json:"elementId"`
				} `json:"requestedOpenTextAnswers"`
			} `json:"rows"`
		}
		if err := json.Unmarshal([]byte(raw), &ctx); err != nil {
			return http.StatusOK, `{"responses":[]}`
		}
		type answer struct {
			ElementID string `json:"elementId"`
			Answer    string `json:"answer"`
		}
		type row struct {
			RowID   string   `json:"rowId"`
			Answers []answer `json:"answers"`
		}
		out := struct {
			Responses []row `json:"responses"`
		}{}
		for _, r := range ctx.Rows {
			a := row{RowID: r.RowID, Answers: []answer{}}
			for _, q := range r.Requested {
				a.Answers = append(a.Answers, answer{ElementID: q.ElementID, Answer: r.RowID + "/" + q.ElementID})
			}
			out.Responses = append(out.Responses, a)
		}
		raw2, _ := json.Marshal(out)
		return http.StatusOK, string(raw2)
	}
}

func (e *serviceEnv) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.Raw(query, args...).Row().Scan(&n); err != nil {
		e.t.Fatalf("count %q: %v", query, err)
	}
	return n
}

// exampleAudit is the part of an audit record the tests check.
type exampleAudit struct {
	Action         string `json:"action"`
	Status         string `json:"status"`
	OrganizationID string `json:"organizationId"`
	Target         struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"target"`
	Changes map[string]any `json:"changes"`
}

func (e *serviceEnv) audits() []exampleAudit {
	e.t.Helper()
	var out []exampleAudit
	for _, line := range e.logs.lines(e.t, "audit") {
		raw, _ := json.Marshal(line)
		var a exampleAudit
		_ = json.Unmarshal(raw, &a)
		out = append(out, a)
	}
	return out
}

func examplesPath(survey uuid.UUID) string {
	return "/api/app/surveys/" + survey.String() + "/ai/example-responses"
}

// forma2's generateExampleResponsesAction: twenty tagged, backdated responses (a fifth of them abandoned) and six unanswered displays, the open texts written by the model, the closed answers planned, and the attempt audited as an update of the survey.
func TestGenerateExampleResponses(t *testing.T) {
	e := newServiceEnv(t, nil)
	owner := e.user()
	org, ws := e.organization(owner, true)
	survey := e.exampleSurvey(ws, exampleBlocks)
	e.answerOpenTexts()

	var out ExampleResponsesResult
	e.ok(e.do(owner, http.MethodPost, examplesPath(survey), nil), &out)
	if out.CreatedCount != 20 {
		t.Fatalf("created %d", out.CreatedCount)
	}
	if n := e.count(`SELECT count(*) FROM responses r JOIN response_tags rt ON rt.response_id = r.id JOIN tags t ON t.id = rt.tag_id WHERE r.survey_id = ? AND t.name = ?`, survey, ExampleAIGeneratedTagName); n != 20 {
		t.Fatalf("tagged responses %d", n)
	}
	if n := e.count(`SELECT count(*) FROM displays WHERE survey_id = ?`, survey); n != 26 {
		t.Fatalf("displays %d", n)
	}
	if n := e.count(`SELECT count(*) FROM responses WHERE survey_id = ? AND display_id IS NOT NULL`, survey); n != 20 {
		t.Fatalf("responses with their display %d", n)
	}
	if n := e.count(`SELECT count(*) FROM responses WHERE survey_id = ? AND finished AND ending_id = 'end1' AND ttc->>'_total' IS NOT NULL`, survey); n != 16 {
		t.Fatalf("finished responses %d", n)
	}
	if n := e.count(`SELECT count(*) FROM responses WHERE survey_id = ? AND NOT finished AND ending_id IS NULL`, survey); n != 4 {
		t.Fatalf("abandoned responses %d", n)
	}
	if n := e.count(`SELECT count(*) FROM responses WHERE survey_id = ? AND created_at > now() - interval '10 days 1 minute' AND created_at <= now()`, survey); n != 20 {
		t.Fatalf("responses inside the last ten days %d", n)
	}
	if n := e.count(`SELECT count(*) FROM responses WHERE survey_id = ? AND language = 'en' AND meta->>'source' = 'example-generation' AND meta->'userAgent'->>'browser' IS NOT NULL`, survey); n != 20 {
		t.Fatalf("responses with the language and meta %d", n)
	}
	var rows []struct {
		Data     []byte
		Finished bool
	}
	if err := e.db.Raw(`SELECT data, finished FROM responses WHERE survey_id = ?`, survey).Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		var data map[string]any
		_ = json.Unmarshal(r.Data, &data)
		if _, ok := data["cta"]; ok {
			t.Fatalf("a CTA was answered: %v", data)
		}
		if why, ok := data["why"].(string); ok && !strings.HasPrefix(why, "row_") {
			t.Fatalf("the open text is not the model's: %v", data)
		}
		if r.Finished && (data["why"] == nil || (data["plan"] != "Free" && data["plan"] != "Pro") || data["stars"] == nil || data["nps"] == nil) {
			t.Fatalf("a finished response is missing answers: %v", data)
		}
	}
	// The model was told what to answer, as plain prose.
	prompt, _ := jsonPath(e.provider.requests()[0], "messages.1.content").(string)
	if !strings.Contains(prompt, `"question": "What made you sign up ?"`) || strings.Contains(prompt, "<p>") {
		t.Fatalf("prompt %s", prompt)
	}

	audits := e.audits()
	if len(audits) != 1 {
		t.Fatalf("audits %+v", audits)
	}
	if a := audits[0]; a.Action != "updated" || a.Status != "success" || a.Target.Type != "survey" || a.Target.ID != survey.String() || a.OrganizationID != org.String() {
		t.Fatalf("audit %+v", a)
	}
}

// forma2 wrote each example response through createResponseWithQuotaEvaluation: the survey's quotas screen it as it is written, so the quota progress and the responses' quotas show the example data. Abandoned responses do not count towards a quota that does not count partial submissions.
func TestGenerateExampleResponsesScreensQuotas(t *testing.T) {
	e := newServiceEnv(t, nil)
	owner := e.user()
	_, ws := e.organization(owner, true)
	survey := e.exampleSurvey(ws, exampleBlocks)
	quota := e.insert(`INSERT INTO survey_quotas (survey_id, name, "limit", logic, action) VALUES (?, 'Plan chosen', 5,
		'{"connector":"and","conditions":[{"id":"c","leftOperand":{"type":"element","value":"plan"},"operator":"isSubmitted"}]}'::jsonb, 'continueSurvey') RETURNING id`, survey)
	e.answerOpenTexts()

	var out ExampleResponsesResult
	e.ok(e.do(owner, http.MethodPost, examplesPath(survey), nil), &out)
	if out.CreatedCount != 20 {
		t.Fatalf("created %d", out.CreatedCount)
	}
	if n := e.count(`SELECT count(*) FROM response_quota_links l JOIN responses r ON r.id = l.response_id WHERE l.quota_id = ? AND l.status = 'screenedIn' AND r.finished`, quota); n != 5 {
		t.Fatalf("screened in %d, want the quota's limit", n)
	}
	if n := e.count(`SELECT count(*) FROM response_quota_links l JOIN responses r ON r.id = l.response_id WHERE l.quota_id = ? AND l.status = 'screenedOut' AND r.finished`, quota); n != 11 {
		t.Fatalf("screened out %d, want the other finished responses", n)
	}
	if n := e.count(`SELECT count(*) FROM response_quota_links l JOIN responses r ON r.id = l.response_id WHERE l.quota_id = ? AND NOT r.finished`, quota); n != 0 {
		t.Fatalf("%d abandoned responses were screened", n)
	}
}

// A quota that fails to screen is logged and rolled back to its savepoint, as forma2 caught and logged it: the dataset is still written.
func TestGenerateExampleResponsesSurviveAFailingQuota(t *testing.T) {
	e := newServiceEnv(t, nil)
	owner := e.user()
	_, ws := e.organization(owner, true)
	survey := e.exampleSurvey(ws, exampleBlocks)
	e.insert(`INSERT INTO survey_quotas (survey_id, name, "limit", logic, action) VALUES (?, 'Plan chosen', 5,
		'{"connector":"and","conditions":[{"id":"c","leftOperand":{"type":"element","value":"plan"},"operator":"isSubmitted"}]}'::jsonb, 'continueSurvey') RETURNING id`, survey)
	e.answerOpenTexts()
	e.exec(`CREATE FUNCTION fail_quota_link() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'quota hiccup'; END $$`)
	e.exec(`CREATE TRIGGER fail_quota_link BEFORE INSERT ON response_quota_links FOR EACH ROW EXECUTE FUNCTION fail_quota_link()`)

	var out ExampleResponsesResult
	e.ok(e.do(owner, http.MethodPost, examplesPath(survey), nil), &out)
	if out.CreatedCount != 20 || e.count(`SELECT count(*) FROM responses WHERE survey_id = ?`, survey) != 20 {
		t.Fatalf("created %d", out.CreatedCount)
	}
	if n := e.count(`SELECT count(*) FROM response_quota_links`); n != 0 {
		t.Fatalf("links %d", n)
	}
	if len(e.logs.lines(t, "Error evaluating quotas for response")) == 0 {
		t.Fatal("the quota failure was not logged")
	}
}

// A chunk the model fails on gets the fallback answers: the generation still writes every response.
func TestGenerateExampleResponsesFallBackWhenTheModelFails(t *testing.T) {
	e := newServiceEnv(t, nil)
	owner := e.user()
	_, ws := e.organization(owner, true)
	survey := e.exampleSurvey(ws, exampleBlocks)
	e.provider.answer = func(map[string]any) (int, string) { return http.StatusInternalServerError, "" }
	var out ExampleResponsesResult
	e.ok(e.do(owner, http.MethodPost, examplesPath(survey), nil), &out)
	if out.CreatedCount != 20 || e.count(`SELECT count(*) FROM responses WHERE survey_id = ? AND data->>'why' <> ''`, survey) == 0 {
		t.Fatalf("created %d", out.CreatedCount)
	}
	if len(e.logs.lines(t, exampleOpenTextGenerationFailureLog)) == 0 {
		t.Fatal("the model's failure was not logged")
	}
}

// forma2's refusals: write access, AI available to the organization, a survey neither archived nor answered, and something to answer. Each writes nothing and is audited as a failure.
func TestGenerateExampleResponsesRefusals(t *testing.T) {
	e := newServiceEnv(t, nil)
	owner, reader := e.user(), e.user()
	org, ws := e.organization(owner, true)
	e.member(org, reader, "member")
	e.grant(org, ws, reader, "read")
	_, offWS := e.organization(owner, false)

	archived := e.exampleSurvey(ws, exampleBlocks)
	e.exec(`UPDATE surveys SET archived_at = now() WHERE id = ?`, archived)
	answered := e.exampleSurvey(ws, exampleBlocks)
	e.exec(`INSERT INTO responses (survey_id) VALUES (?)`, answered)
	unsupported := e.exampleSurvey(ws, `[{"id":"b1","name":"Main","elements":[{"id":"cta","type":"cta","headline":{"default":"Read more"},"required":false}]}]`)

	for _, tc := range []struct {
		name    string
		user    uuid.UUID
		survey  uuid.UUID
		status  int
		code    string
		message string
	}{
		{"reader", reader, e.exampleSurvey(ws, exampleBlocks), http.StatusForbidden, apperr.CodeForbidden, ""},
		{"AI smart tools off", owner, e.exampleSurvey(offWS, exampleBlocks), http.StatusForbidden, CodeSmartToolsDisabled, ""},
		{"archived", owner, archived, http.StatusForbidden, apperr.CodeOperationNotAllowed, exampleArchivedSurveyMessage},
		{"answered", owner, answered, http.StatusForbidden, apperr.CodeOperationNotAllowed, exampleSurveyHasResponsesMessage},
		{"nothing to answer", owner, unsupported, http.StatusBadRequest, apperr.CodeBadRequest, exampleNoSupportedElementsMessage},
		{"unknown survey", owner, uuid.New(), http.StatusNotFound, apperr.CodeNotFound, ""},
	} {
		env := e.fails(e.do(tc.user, http.MethodPost, examplesPath(tc.survey), nil), tc.status, tc.code)
		if tc.message != "" && env.Message != tc.message {
			t.Errorf("%s: %q", tc.name, env.Message)
		}
	}
	if n := e.count(`SELECT count(*) FROM responses`); n != 1 {
		t.Fatalf("a refused generation wrote responses: %d", n)
	}
	if len(e.provider.requests()) != 0 {
		t.Fatal("a refused generation called the model")
	}
	audits := e.audits()
	if len(audits) != 6 {
		t.Fatalf("audits %+v", audits)
	}
	for _, a := range audits {
		if a.Action != "updated" || a.Status != "failure" || a.Target.Type != "survey" {
			t.Fatalf("audit %+v", a)
		}
	}
}

// An instance without an AI provider refuses before reading the survey.
func TestGenerateExampleResponsesWithoutAI(t *testing.T) {
	e := newServiceEnv(t, func(c *config.AIConfig) { *c = config.AIConfig{} })
	owner := e.user()
	_, ws := e.organization(owner, true)
	e.fails(e.do(owner, http.MethodPost, examplesPath(e.exampleSurvey(ws, exampleBlocks)), nil), http.StatusForbidden, CodeInstanceNotConfigured)
}

// One generation per user per minute: a user who already generated is refused before anything else is checked.
func TestGenerateExampleResponsesRateLimit(t *testing.T) {
	e := newServiceEnv(t, nil)
	owner := e.user()
	_, ws := e.organization(owner, true)
	survey := e.exampleSurvey(ws, exampleBlocks)
	u, _ := url.Parse(webApp)
	cfg := config.Config{WebAppURL: u, AuditLogEnabled: true}
	log := slog.New(slog.NewJSONHandler(e.logs, nil))
	frozen := time.Now()
	limiter := ratelimit.New(cfg, valkeytest.Client(t), log).WithClock(func() time.Time { return frozen })
	if err := limiter.Apply(t.Context(), ratelimit.ActionGenerateExampleResponses, owner.String()); err != nil {
		t.Fatal(err)
	}
	examples := NewExampleResponses(e.svc, e.db, NewQuotaScreener(e.db, log), limiter, audit.New(cfg, log), log)
	_, err := examples.Generate(t.Context(), owner, survey)
	expectCode(t, err, http.StatusTooManyRequests, apperr.CodeTooManyRequests)
	if n := e.count(`SELECT count(*) FROM displays`); n != 0 || len(e.provider.requests()) != 0 {
		t.Fatalf("a rate-limited generation did work: %d displays", n)
	}
}

// The dataset is written all or nothing: a failure part-way leaves no response, display or tag behind, and the survey can be generated for again.
func TestGenerateExampleResponsesIsAtomic(t *testing.T) {
	e := newServiceEnv(t, nil)
	owner := e.user()
	_, ws := e.organization(owner, true)
	survey := e.exampleSurvey(ws, exampleBlocks)
	e.answerOpenTexts()
	e.exec(`CREATE FUNCTION fail_sixth_example() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF (SELECT count(*) FROM responses WHERE survey_id = NEW.survey_id) >= 5 THEN
				RAISE EXCEPTION 'storage hiccup';
			END IF;
			RETURN NEW;
		END $$`)
	e.exec(`CREATE TRIGGER fail_sixth_example BEFORE INSERT ON responses FOR EACH ROW EXECUTE FUNCTION fail_sixth_example()`)

	e.fails(e.do(owner, http.MethodPost, examplesPath(survey), nil), http.StatusInternalServerError, apperr.CodeInternal)
	for table, query := range map[string]string{
		"responses": `SELECT count(*) FROM responses WHERE survey_id = ?`,
		"displays":  `SELECT count(*) FROM displays WHERE survey_id = ?`,
		"tags":      fmt.Sprintf(`SELECT count(*) FROM tags WHERE workspace_id = (SELECT workspace_id FROM surveys WHERE id = ?) AND name = '%s'`, ExampleAIGeneratedTagName),
	} {
		if n := e.count(query, survey); n != 0 {
			t.Fatalf("a failed generation left %d %s", n, table)
		}
	}
	if a := e.audits(); len(a) != 1 || a[0].Status != "failure" {
		t.Fatalf("audits %+v", a)
	}

	e.exec(`DROP TRIGGER fail_sixth_example ON responses`)
	var out ExampleResponsesResult
	e.ok(e.do(owner, http.MethodPost, examplesPath(survey), nil), &out)
	if out.CreatedCount != 20 {
		t.Fatalf("retry created %d", out.CreatedCount)
	}
}

// The planned answers follow forma2's rules: every planned value is one the element accepts, address and contact info in their wire format.
func TestPlanRowsAnswerShapes(t *testing.T) {
	s := &exampleSurvey{Name: "S"}
	var blocks []struct {
		Elements []*exampleElement
	}
	raw := `[{"elements":[
		{"id":"addr","type":"address","headline":{"default":"Address"},"required":true,"addressLine1":{"show":true,"required":true},"addressLine2":{"show":false,"required":false},"city":{"show":true,"required":true},"state":{"show":false,"required":false},"zip":{"show":false,"required":false},"country":{"show":true,"required":true}},
		{"id":"who","type":"contactInfo","headline":{"default":"You"},"required":true,"firstName":{"show":true,"required":true},"lastName":{"show":false,"required":false},"email":{"show":true,"required":true},"phone":{"show":false,"required":false},"company":{"show":false,"required":false}},
		{"id":"grid","type":"matrix","headline":{"default":"Grid"},"required":true,"rows":[{"id":"r1","label":{"default":"Speed"}}],"columns":[{"id":"k1","label":{"default":"Good"}},{"id":"k2","label":{"default":"Bad"}}]},
		{"id":"when","type":"date","headline":{"default":"When"},"required":true},
		{"id":"ok","type":"consent","headline":{"default":"OK?"},"required":true}]}]`
	if err := json.Unmarshal([]byte(raw), &blocks); err != nil {
		t.Fatal(err)
	}
	s.Elements = blocks[0].Elements
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	rows := planRows(s, now)
	if len(rows) != 20 {
		t.Fatalf("rows %d", len(rows))
	}
	last := rows[19]
	addr, _ := last.Data["addr"].([]string)
	if len(addr) != 6 || addr[0] == "" || addr[1] != "" || addr[2] == "" || addr[5] == "" {
		t.Fatalf("address %v", last.Data["addr"])
	}
	who, _ := last.Data["who"].([]string)
	if len(who) != 5 || who[0] == "" || who[1] != "" || !strings.Contains(who[2], "@example.com") {
		t.Fatalf("contact info %v", last.Data["who"])
	}
	if grid, _ := last.Data["grid"].(map[string]any); grid["Speed"] != "Good" && grid["Speed"] != "Bad" {
		t.Fatalf("matrix %v", last.Data["grid"])
	}
	if d, _ := last.Data["when"].(string); len(d) != 10 || d > "2026-10-23" {
		t.Fatalf("date %v", last.Data["when"])
	}
	if last.Data["ok"] != "accepted" || last.Language != nil {
		t.Fatalf("consent %v, language %v", last.Data["ok"], last.Language)
	}
	for _, row := range rows[:4] {
		if row.Finished || len(row.Data) >= 5 {
			t.Fatalf("an abandoned row kept every answer: %v", row.Data)
		}
	}
}

// forma2's fallback answers: a priorities-tinted answer for a product-feedback question, the profile's sentiment otherwise, styled by the profile's polish.
func TestFallbackAnswer(t *testing.T) {
	rough := exampleProfiles[3]
	improve := &exampleElement{}
	improve.Headline = map[string]string{"default": "How could we improve?"}
	if got := fallbackAnswer(rough, improve, 0); got[0] < 'a' || got[0] > 'z' || !strings.Contains(got, "reliability") || !strings.HasSuffix(got, ".") {
		t.Fatalf("improve %q", got)
	}
	other := &exampleElement{}
	other.Headline = map[string]string{"default": "Anything else?"}
	promoter := exampleProfiles[0]
	if got := fallbackAnswer(promoter, other, 0); !contains(fallbackAnswers["promoter"]["detailed"], got) {
		t.Fatalf("generic %q", got)
	}
	if fallbackAnswer(promoter, other, 0) == fallbackAnswer(promoter, other, 1) {
		t.Fatal("an offset did not change the answer")
	}
	if got := stripHTML("<p>Tom &amp; <b>Jerry</b></p>"); got != "Tom & Jerry" {
		t.Fatalf("stripHTML %q", got)
	}
}
