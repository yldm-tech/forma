package analysis

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/ai"
	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/valkey/valkeytest"
	"github.com/yldm-tech/forma/apps/server/internal/response"
)

func TestRedactContactDetails(t *testing.T) {
	cases := []struct{ in, want string }{
		{"mail me at jane.doe+survey@example.co.uk please", "mail me at [email] please"},
		{"call +1 (555) 010-2030 after six", "call [phone] after six"},
		{"手机 13812345678 随时联系", "手机 [phone] 随时联系"},
		{"03-1234-5678 is the office", "[phone] is the office"},
		{"since 2026-10-26 it is slow", "since 2026-10-26 it is slow"},
		{"on 26.10.2026 it broke", "on 26.10.2026 it broke"},
		{"order 1234567 and version 1.2.3", "order 1234567 and version 1.2.3"},
		{"I paid 49.99 for 12 seats", "I paid 49.99 for 12 seats"},
	}
	for _, c := range cases {
		if got := redactContactDetails(c.in); got != c.want {
			t.Errorf("redact(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSampleTextAnswersBoundsEveryRequest(t *testing.T) {
	var answers []textAnswer
	for i := range 1000 {
		answers = append(answers, textAnswer{ResponseID: uuid.New(), Text: fmt.Sprintf("answer %d %s reach me at user%d@example.com", i, strings.Repeat("word ", 60), i)})
	}
	chunks := sampleTextAnswers(answers)
	if len(chunks) == 0 || len(chunks) > textMaxChunks {
		t.Fatalf("%d chunks", len(chunks))
	}
	total := 0
	var ids []string
	for _, c := range chunks {
		tokens := 0
		for _, a := range c {
			tokens += estimateTokens(a.Text) + textAnswerOverhead
			ids = append(ids, a.ID)
			if strings.Contains(a.Text, "@") {
				t.Fatalf("an email reached the sample: %q", a.Text)
			}
		}
		if tokens > textChunkTokens {
			t.Errorf("chunk of %d estimated tokens, cap %d", tokens, textChunkTokens)
		}
		total += len(c)
	}
	if total > textSampleLimit || total == 0 {
		t.Errorf("sampled %d", total)
	}
	for i, id := range ids {
		if id != fmt.Sprintf("a%d", i+1) {
			t.Fatalf("id %d is %q", i, id)
		}
	}
	// The same answers give the same sample.
	again := sampleTextAnswers(answers)
	if fmt.Sprint(again) != fmt.Sprint(chunks) {
		t.Error("the sample is not stable")
	}

	// A small set is sent whole, in one request, with each long answer truncated.
	small := []textAnswer{{ResponseID: uuid.New(), Text: strings.Repeat("长", textAnswerRunes+50)}, {ResponseID: uuid.New(), Text: "short"}}
	got := sampleTextAnswers(small)
	if len(got) != 1 || len(got[0]) != 2 {
		t.Fatalf("small sample %v", got)
	}
	for _, a := range got[0] {
		if n := len([]rune(a.Text)); n > textAnswerRunes+1 {
			t.Errorf("answer of %d runes", n)
		}
	}
}

func TestFinishThemesCountsAndQuotesFromTheSample(t *testing.T) {
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	sample := []sampledAnswer{
		{ID: "a1", Text: "too pricey", responseID: ids[0]},
		{ID: "a2", Text: "too pricey", responseID: ids[1]},
		{ID: "a3", Text: "costs a lot", responseID: ids[2]},
		{ID: "a4", Text: "support was slow", responseID: ids[3]},
		{ID: "a5", Text: "great", responseID: ids[4]},
	}
	drafts := []draftTheme{
		// A quote outside the theme is dropped, a repeated text is quoted once, and the quotes are topped up from the theme's answers.
		{title: "Support", description: "Slow replies", answers: []int{3}, quotes: []int{4}},
		{title: "Price", description: "Too expensive", answers: []int{0, 1, 2, 0}, quotes: []int{0, 1}},
		{title: "Empty", answers: nil},
	}
	got := finishThemes(drafts, sample)
	want := []TextTheme{
		{Title: "Price", Description: "Too expensive", Count: 3, Percentage: 60, Quotes: []TextQuote{{ResponseID: ids[0], Text: "too pricey"}, {ResponseID: ids[2], Text: "costs a lot"}}},
		{Title: "Support", Description: "Slow replies", Count: 1, Percentage: 20, Quotes: []TextQuote{{ResponseID: ids[3], Text: "support was slow"}}},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("themes\n got %+v\nwant %+v", got, want)
	}
}

func TestOverallSentiment(t *testing.T) {
	cases := []struct {
		counts [3]int
		want   string
	}{
		{[3]int{0, 0, 0}, SentimentNeutral},
		{[3]int{7, 2, 1}, SentimentPositive},
		{[3]int{1, 2, 7}, SentimentNegative},
		{[3]int{1, 8, 1}, SentimentNeutral},
		{[3]int{4, 2, 4}, SentimentMixed},
		{[3]int{5, 4, 1}, SentimentPositive},
		{[3]int{1, 4, 5}, SentimentNegative},
	}
	for _, c := range cases {
		if got := overallSentiment(c.counts); got.Overall != c.want {
			t.Errorf("%v: %s, want %s", c.counts, got.Overall, c.want)
		}
	}
	if s := overallSentiment([3]int{1, 1, 2}); s.Positive != 25 || s.Neutral != 25 || s.Negative != 50 {
		t.Errorf("shares %+v", s)
	}
}

// textProvider is an OpenAI-compatible endpoint that groups answers by keyword: "price" is Pricing, "support" Support; "love" is positive and "hate" negative. Merging joins themes of the same title.
type textProvider struct {
	*httptest.Server
	mu      sync.Mutex
	chunks  []string
	merges  int
	status  int
	answers int
}

func newTextProvider(t *testing.T) *textProvider {
	t.Helper()
	p := &textProvider{status: http.StatusOK}
	p.Server = httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.Close)
	return p
}

func (p *textProvider) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
		ResponseFormat struct {
			JSONSchema struct {
				Name string `json:"name"`
			} `json:"json_schema"`
		} `json:"response_format"`
	}
	_ = json.Unmarshal(raw, &body)
	user := body.Messages[len(body.Messages)-1].Content
	p.mu.Lock()
	status := p.status
	p.mu.Unlock()
	if status != http.StatusOK {
		w.Header().Set("Retry-After", "17")
		w.Header().Set("Retry-After-Ms", "1")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error":{"message":"slow down"}}`)
		return
	}
	var content any
	switch body.ResponseFormat.JSONSchema.Name {
	case "TextAnswerThemes":
		_, list, _ := strings.Cut(user, "Answers (JSON):\n")
		var answers []struct{ ID, Text string }
		_ = json.Unmarshal([]byte(list), &answers)
		p.mu.Lock()
		p.chunks = append(p.chunks, user)
		p.answers += len(answers)
		p.mu.Unlock()
		groups := map[string][]string{}
		sentiment := map[string]int{"positive": 0, "neutral": 0, "negative": 0}
		for _, a := range answers {
			for keyword, title := range map[string]string{"price": "Pricing", "support": "Support"} {
				if strings.Contains(a.Text, keyword) {
					groups[title] = append(groups[title], a.ID)
				}
			}
			switch {
			case strings.Contains(a.Text, "love"):
				sentiment["positive"]++
			case strings.Contains(a.Text, "hate"):
				sentiment["negative"]++
			default:
				sentiment["neutral"]++
			}
		}
		themes := []any{}
		for _, title := range []string{"Pricing", "Support"} {
			if ids := groups[title]; len(ids) > 0 {
				themes = append(themes, map[string]any{"title": title, "description": title + " comes up.", "answerIds": ids, "quoteIds": append([]string{"a99999"}, ids[:min(2, len(ids))]...)})
			}
		}
		content = map[string]any{"themes": themes, "sentiment": sentiment}
	case "MergedThemes":
		_, list, _ := strings.Cut(user, "Themes (JSON):\n")
		var themes []struct{ ID, Title string }
		_ = json.Unmarshal([]byte(list), &themes)
		p.mu.Lock()
		p.merges++
		p.mu.Unlock()
		sources := map[string][]string{}
		var titles []string
		for _, th := range themes {
			if _, ok := sources[th.Title]; !ok {
				titles = append(titles, th.Title)
			}
			sources[th.Title] = append(sources[th.Title], th.ID)
		}
		out := []any{}
		for _, title := range titles {
			out = append(out, map[string]any{"title": title, "description": "Merged " + title + ".", "sourceThemeIds": sources[title]})
		}
		content = map[string]any{"themes": out}
	}
	text, _ := json.Marshal(content)
	answer, _ := json.Marshal(map[string]any{"id": "c", "object": "chat.completion", "created": 1, "model": "m",
		"choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(text)}}},
		"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}})
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(answer)
}

func (p *textProvider) calls() (chunks []string, merges int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.chunks), p.merges
}

// answersRead is how many answers the chunk requests carried.
func (p *textProvider) answersRead() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.answers
}

// newTextEnv is the analysis module with its text summaries: the AI provider is the returned fake when configured is set, none otherwise; summaries are cached in a fresh Valkey and rate limited for real.
func newTextEnv(t *testing.T, configured bool) (*env, *textProvider, *ratelimit.Limiter) {
	t.Helper()
	e := newEnv(t)
	provider := newTextProvider(t)
	cfg := e.cfg
	cfg.RateLimitingDisabled = false
	if configured {
		cfg.AI = config.AIConfig{Provider: config.AIProviderOpenAICompatible, Model: "m", OpenAICompatibleBaseURL: provider.URL, OpenAICompatibleSupportsStructuredOutputs: "1"}
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cache := valkeytest.Client(t)
	// Windows are aligned to the wall clock; a frozen one keeps a scenario from straddling a boundary.
	frozen := time.Now()
	limiter := ratelimit.New(cfg, cache, log).WithClock(func() time.Time { return frozen })
	aiService := ai.NewService(ai.NewClient(cfg, log), ai.NewRepository(e.db), e.authz.Authorizer, log)
	texts := NewTextSummaries(e.svc, aiService, limiter, cache, log)
	e.engine = httpserver.NewEngine(cfg, log)
	NewHandler(e.svc, texts, headerSessions{}, limiter).Register(e.engine.Group("/api/app", httpapi.ErrorHandler(log), httpapi.CSRF(cfg), authz.DecisionMemo()))
	return e, provider, limiter
}

const textBlocks = `[{"id":"b1","name":"B1","elements":[
	{"id":"q1","type":"openText","headline":{"default":"What should we improve?"},"required":false,"inputType":"text"},
	{"id":"pick","type":"multipleChoiceMulti","headline":{"default":"Why did you pick us?"},"required":false,
		"choices":[{"id":"a","label":{"default":"Speed","de":"Tempo"}},{"id":"other","label":{"default":"Something else"}}]},
	{"id":"nps","type":"nps","headline":{"default":"Recommend?"},"required":false}
]}]`

func textInput(elementID, target string, filter *response.Criteria) map[string]any {
	return map[string]any{"elementId": elementID, "target": target, "language": "zh-Hans-CN", "filterCriteria": filter}
}

func textPaths(survey uuid.UUID) (lookup, generate string) {
	base := "/api/app/surveys/" + survey.String() + "/text-summaries"
	return base + "/lookup", base
}

func TestTextSummaryIsGeneratedCachedAndInvalidated(t *testing.T) {
	e, provider, _ := newTextEnv(t, true)
	owner := e.user()
	org, ws := e.organization(owner, nil)
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	sv := e.survey(ws, surveyOpts{blocks: textBlocks})
	lookup, generate := textPaths(sv)
	pricey := e.response(sv, row{at: at(1), finished: true, data: `{"q1":"I hate the price, write to me at ada@example.com"}`})
	e.response(sv, row{at: at(2), finished: true, data: `{"q1":"I love the support team"}`})
	e.response(sv, row{at: at(3), finished: false, data: `{"q1":"price is fine"}`})
	e.response(sv, row{at: at(4), finished: true, data: `{"q1":"   "}`})
	e.response(sv, row{at: at(5), finished: true, data: `{"nps":9}`})

	var state TextSummaryState
	decode(t, e.request(owner, http.MethodPost, lookup, textInput("q1", TextTargetAnswers, nil)), &state)
	if !state.Available || state.Summary != nil {
		t.Fatalf("before generating: %+v", state)
	}

	decode(t, e.request(owner, http.MethodPost, generate, textInput("q1", TextTargetAnswers, nil)), &state)
	s := state.Summary
	if s == nil || s.AnswerCount != 3 || s.SampledCount != 3 || s.ReadLimit != 0 || s.Language != "zh-Hans-CN" || s.GeneratedAt.IsZero() {
		t.Fatalf("summary %+v", s)
	}
	if len(s.Themes) != 2 || s.Themes[0].Title != "Pricing" || s.Themes[0].Count != 2 || s.Themes[0].Percentage != 66.67 || s.Themes[1].Title != "Support" || s.Themes[1].Count != 1 {
		t.Fatalf("themes %+v", s.Themes)
	}
	if s.Sentiment.Overall != SentimentMixed || s.Sentiment.Positive != 33.33 || s.Sentiment.Negative != 33.33 {
		t.Errorf("sentiment %+v", s.Sentiment)
	}
	// The quotes come from the answers themselves, linked to their responses, with the address that never reached the provider removed.
	var quoted bool
	for _, q := range s.Themes[0].Quotes {
		if q.ResponseID == pricey {
			quoted = true
			if q.Text != "I hate the price, write to me at [email]" {
				t.Errorf("quote %q", q.Text)
			}
		}
	}
	if !quoted {
		t.Errorf("quotes %+v", s.Themes[0].Quotes)
	}
	chunks, merges := provider.calls()
	if len(chunks) != 1 || merges != 0 || strings.Contains(chunks[0], "ada@example.com") || !strings.Contains(chunks[0], "What should we improve?") {
		t.Fatalf("provider saw %d chunks, %d merges: %v", len(chunks), merges, chunks)
	}

	// The card reads it back without a call to the provider; asking again without a refresh does not call it either.
	var cached TextSummaryState
	decode(t, e.request(owner, http.MethodPost, lookup, textInput("q1", TextTargetAnswers, nil)), &cached)
	if cached.Summary == nil || !cached.Summary.GeneratedAt.Equal(s.GeneratedAt) {
		t.Fatalf("lookup %+v", cached)
	}
	decode(t, e.request(owner, http.MethodPost, generate, textInput("q1", TextTargetAnswers, nil)), &cached)
	if chunks, _ := provider.calls(); len(chunks) != 1 || cached.Summary == nil || !cached.Summary.GeneratedAt.Equal(s.GeneratedAt) {
		t.Fatalf("generate without refresh called the provider again (%d)", len(chunks))
	}
	// Another language or filter is another summary.
	other := textInput("q1", TextTargetAnswers, nil)
	other["language"] = "en-US"
	decode(t, e.request(owner, http.MethodPost, lookup, other), &cached)
	if cached.Summary != nil {
		t.Error("a summary in another language was served")
	}

	// A new answer makes the summary stale; one to another question does not.
	e.response(sv, row{at: at(6), finished: true, data: `{"nps":3}`})
	decode(t, e.request(owner, http.MethodPost, lookup, textInput("q1", TextTargetAnswers, nil)), &cached)
	if cached.Summary == nil {
		t.Fatal("an answer to another question invalidated the summary")
	}
	e.response(sv, row{at: at(7), finished: true, data: `{"q1":"support again"}`})
	decode(t, e.request(owner, http.MethodPost, lookup, textInput("q1", TextTargetAnswers, nil)), &cached)
	if cached.Summary != nil {
		t.Fatal("a stale summary was served")
	}
	// So does a changed answer.
	decode(t, e.request(owner, http.MethodPost, generate, textInput("q1", TextTargetAnswers, nil)), &cached)
	e.exec(`UPDATE responses SET data = '{"q1":"changed my mind"}' WHERE id = ?`, pricey)
	decode(t, e.request(owner, http.MethodPost, lookup, textInput("q1", TextTargetAnswers, nil)), &cached)
	if cached.Summary != nil {
		t.Fatal("a summary of a changed answer was served")
	}

	// Refresh regenerates a current summary.
	decode(t, e.request(owner, http.MethodPost, generate, textInput("q1", TextTargetAnswers, nil)), &cached)
	refresh := textInput("q1", TextTargetAnswers, nil)
	refresh["refresh"] = true
	before, _ := provider.calls()
	decode(t, e.request(owner, http.MethodPost, generate, refresh), &cached)
	if after, _ := provider.calls(); len(after) != len(before)+1 {
		t.Errorf("refresh made %d calls", len(after)-len(before))
	}
}

func TestTextSummaryRespectsTheFilter(t *testing.T) {
	e, provider, _ := newTextEnv(t, true)
	owner := e.user()
	org, ws := e.organization(owner, nil)
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	sv := e.survey(ws, surveyOpts{blocks: textBlocks})
	_, generate := textPaths(sv)
	e.response(sv, row{at: at(1), finished: true, data: `{"q1":"the price, finished"}`})
	e.response(sv, row{at: at(2), finished: false, data: `{"q1":"support, unfinished"}`})

	var state TextSummaryState
	decode(t, e.request(owner, http.MethodPost, generate, textInput("q1", TextTargetAnswers, &response.Criteria{Finished: ptr(true)})), &state)
	if state.Summary == nil || state.Summary.AnswerCount != 1 || len(state.Summary.Themes) != 1 || state.Summary.Themes[0].Title != "Pricing" {
		t.Fatalf("summary %+v", state.Summary)
	}
	chunks, _ := provider.calls()
	if strings.Contains(chunks[0], "unfinished") {
		t.Error("an answer outside the filter reached the provider")
	}
}

func TestTextSummaryOfOtherValues(t *testing.T) {
	e, provider, _ := newTextEnv(t, true)
	owner := e.user()
	org, ws := e.organization(owner, nil)
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	sv := e.survey(ws, surveyOpts{blocks: textBlocks})
	_, generate := textPaths(sv)
	for _, l := range []struct {
		code      string
		isDefault bool
	}{{"en", true}, {"de", false}} {
		id := e.insert(`INSERT INTO languages (workspace_id, code) VALUES (?, ?) RETURNING id`, ws, l.code)
		e.exec(`INSERT INTO survey_languages (survey_id, language_id, is_default) VALUES (?, ?, ?)`, sv, id, l.isDefault)
	}
	// A choice picked in German is a choice, not an Other value.
	e.response(sv, row{at: at(1), finished: true, data: `{"pick":["Speed","the price"]}`})
	e.response(sv, row{at: at(2), finished: true, data: `{"pick":["Tempo"]}`, language: ptr("de")})
	e.response(sv, row{at: at(3), finished: true, data: `{"pick":["great support"]}`})

	var state TextSummaryState
	decode(t, e.request(owner, http.MethodPost, generate, textInput("pick", TextTargetOther, nil)), &state)
	if state.Summary == nil || state.Summary.AnswerCount != 2 || state.Summary.Target != TextTargetOther {
		t.Fatalf("summary %+v", state.Summary)
	}
	chunks, _ := provider.calls()
	if strings.Contains(chunks[0], "Speed") || strings.Contains(chunks[0], "Tempo") || !strings.Contains(chunks[0], `"Something else"`) {
		t.Errorf("prompt %s", chunks[0])
	}

	// Only free text can be summarized.
	for _, in := range []map[string]any{textInput("nps", TextTargetAnswers, nil), textInput("q1", TextTargetOther, nil), textInput("pick", TextTargetAnswers, nil)} {
		if rec := e.request(owner, http.MethodPost, generate, in); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: status %d", in, rec.Code)
		}
	}
	if rec := e.request(owner, http.MethodPost, generate, textInput("nope", TextTargetAnswers, nil)); rec.Code != http.StatusNotFound {
		t.Errorf("unknown element: status %d", rec.Code)
	}
	bad := textInput("q1", TextTargetAnswers, nil)
	bad["language"] = "not a language"
	if rec := e.request(owner, http.MethodPost, generate, bad); rec.Code != http.StatusBadRequest {
		t.Errorf("bad language: status %d", rec.Code)
	}
}

func TestTextSummaryChunksLargeSetsAndMergesTheirThemes(t *testing.T) {
	e, provider, _ := newTextEnv(t, true)
	owner := e.user()
	org, ws := e.organization(owner, nil)
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	sv := e.survey(ws, surveyOpts{blocks: textBlocks})
	_, generate := textPaths(sv)
	e.exec(`INSERT INTO responses (survey_id, finished, data)
		SELECT ?, true, jsonb_build_object('q1', CASE WHEN g % 2 = 0 THEN 'the price ' ELSE 'the support ' END || repeat('detail ', 40) || g) FROM generate_series(1, 600) g`, sv)

	var state TextSummaryState
	decode(t, e.request(owner, http.MethodPost, generate, textInput("q1", TextTargetAnswers, nil)), &state)
	s := state.Summary
	chunks, merges := provider.calls()
	if len(chunks) < 2 || len(chunks) > textMaxChunks || merges != 1 {
		t.Fatalf("%d chunks, %d merges", len(chunks), merges)
	}
	if s == nil || s.AnswerCount != 600 || s.SampledCount == 0 || s.SampledCount >= 600 || provider.answersRead() != s.SampledCount {
		t.Fatalf("summary %+v, provider read %d", s, provider.answersRead())
	}
	if len(s.Themes) != 2 || s.Themes[0].Count+s.Themes[1].Count != s.SampledCount || len(s.Themes[0].Quotes) < textMinQuotes {
		t.Errorf("themes %+v", s.Themes)
	}
}

func TestTextSummaryAvailabilityAccessAndLimits(t *testing.T) {
	e, provider, limiter := newTextEnv(t, true)
	owner, reader, stranger := e.user(), e.user(), e.user()
	org, ws := e.organization(owner, nil)
	e.grant(org, ws, reader, "read")
	sv := e.survey(ws, surveyOpts{blocks: textBlocks})
	lookup, generate := textPaths(sv)
	e.response(sv, row{at: at(1), finished: true, data: `{"q1":"the price"}`})

	// AI switched off for the organization: the card says so and nothing is generated.
	var state TextSummaryState
	decode(t, e.request(owner, http.MethodPost, lookup, textInput("q1", TextTargetAnswers, nil)), &state)
	if state.Available || state.Reason != ai.ReasonNotEnabled {
		t.Errorf("toggle off: %+v", state)
	}
	rec := e.request(owner, http.MethodPost, generate, textInput("q1", TextTargetAnswers, nil))
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), ai.CodeSmartToolsDisabled) {
		t.Errorf("toggle off: %d %s", rec.Code, rec.Body)
	}

	// Anyone who can read the responses may summarize them; nobody else learns the survey exists.
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	decode(t, e.request(reader, http.MethodPost, generate, textInput("q1", TextTargetAnswers, nil)), &state)
	if state.Summary == nil {
		t.Fatal("a reader could not summarize")
	}
	for _, path := range []string{lookup, generate} {
		if rec := e.request(stranger, http.MethodPost, path, textInput("q1", TextTargetAnswers, nil)); rec.Code != http.StatusForbidden {
			t.Errorf("%s by a stranger: %d", path, rec.Code)
		}
	}

	// A survey nobody answered has nothing to summarize.
	empty := e.survey(ws, surveyOpts{blocks: textBlocks})
	_, emptyGenerate := textPaths(empty)
	if rec := e.request(owner, http.MethodPost, emptyGenerate, textInput("q1", TextTargetAnswers, nil)); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), CodeNoTextAnswers) {
		t.Errorf("no answers: %d %s", rec.Code, rec.Body)
	}

	// The organization's hourly budget is spent (the reader's summary took one unit of it): a cached summary is still served, a new one is refused.
	if res, err := limiter.Check(t.Context(), ratelimit.ActionAITextSummary, org.String(), ratelimit.ActionAITextSummary.Limit-1); err != nil || !res.Allowed {
		t.Fatalf("spend the budget: %+v %v", res, err)
	}
	decode(t, e.request(owner, http.MethodPost, generate, textInput("q1", TextTargetAnswers, nil)), &state)
	if state.Summary == nil {
		t.Error("the cached summary was refused")
	}
	refresh := textInput("q1", TextTargetAnswers, nil)
	refresh["refresh"] = true
	if rec := e.request(owner, http.MethodPost, generate, refresh); rec.Code != http.StatusTooManyRequests {
		t.Errorf("over the limit: %d %s", rec.Code, rec.Body)
	}

	// The provider rate limiting the instance is the AI quota error.
	provider.mu.Lock()
	provider.status = http.StatusTooManyRequests
	provider.mu.Unlock()
	org2, ws2 := e.organization(owner, nil)
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org2)
	sv2 := e.survey(ws2, surveyOpts{blocks: textBlocks})
	e.response(sv2, row{at: at(1), finished: true, data: `{"q1":"the price"}`})
	_, generate2 := textPaths(sv2)
	if rec := e.request(owner, http.MethodPost, generate2, textInput("q1", TextTargetAnswers, nil)); rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), ai.CodeQuotaExceeded) {
		t.Errorf("provider 429: %d %s", rec.Code, rec.Body)
	}
}

func TestTextSummaryWithoutAProvider(t *testing.T) {
	e, _, _ := newTextEnv(t, false)
	owner := e.user()
	org, ws := e.organization(owner, nil)
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	sv := e.survey(ws, surveyOpts{blocks: textBlocks})
	lookup, generate := textPaths(sv)
	e.response(sv, row{at: at(1), finished: true, data: `{"q1":"the price"}`})

	var state TextSummaryState
	decode(t, e.request(owner, http.MethodPost, lookup, textInput("q1", TextTargetAnswers, nil)), &state)
	if state.Available || state.Reason != ai.ReasonInstanceNotConfigured || state.Summary != nil {
		t.Errorf("lookup %+v", state)
	}
	if rec := e.request(owner, http.MethodPost, generate, textInput("q1", TextTargetAnswers, nil)); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), ai.CodeInstanceNotConfigured) {
		t.Errorf("generate %d %s", rec.Code, rec.Body)
	}
}
