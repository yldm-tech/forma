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
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

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
		// Full-width digits and dashes, as a Japanese or Chinese input method types them.
		{"電話は０９０−１２３４−５６７８です", "電話は[phone]です"},
		{"０９０－１２３４－５６７８", "[phone]"},
		{"tel（03）1234ー5678まで", "tel[phone]まで"},
		{"call 090‐1234‐5678 or 090–1234–5678", "call [phone] or [phone]"},
		{"手机　138 1234 5678", "手机　[phone]"},
		// Full-width ＠ and full-width addresses, as a Japanese or Chinese input method types them.
		{"taro＠example.com", "[email]"},
		{"ｔａｒｏ＠ｅｘａｍｐｌｅ．ｃｏｍ", "[email]"},
		{"連絡先：ｔａｒｏ＠ｅｘａｍｐｌｅ．ｃｏｍ、０９０－１２３４－５６７８", "連絡先：[email]、[phone]"},
		// Only the address is replaced: the full-width punctuation around it stays as written.
		{"价格不透明，邮箱ｗａｎｇ＠ｅｘａｍｐｌｅ．ｃｎ，电话０９０－１２３４－５６７８", "价格不透明，邮箱[email]，电话[phone]"},
		{"taro﹫example.com", "[email]"},
		// A match is replaced over every rune it was folded from: ℡ folds to TEL, which the address takes in.
		{"℡taro@example.com まで", "[email] まで"},
		{"\xff taro@example.com", "\xff [email]"},
		// An ASCII address written into CJK text without spaces is redacted alone.
		{"メールはtaro＠example.co.jpです", "メールは[email]です"},
		{"请发到zhang.san@example.com谢谢", "请发到[email]谢谢"},
		// Internationalized addresses: Unicode local parts, domains and top-level domains.
		{"张三@例子.公司", "[email]"},
		{"请联系 张三@例子.公司 or zhang@例子.中国", "请联系 [email] or [email]"},
		{"taro@例え.jp and taro@mail.例え.テスト", "[email] and [email]"},
		{"उपयोगकर्ता@उदाहरण.भारत", "[email]"},
		{"jörg@bücher.de", "[email]"},
		{"jo\u0308rg@bu\u0308cher.de", "[email]"},
		{"Grüße, jörg@example.com!", "Grüße, [email]!"},
		{"пишите иван@пример.рф или ivan@example.ru", "пишите [email] или [email]"},
		// The ideographic full stop a Chinese or Japanese input method types for '.', full-width (。) or half-width (｡), is a dot in a domain; elsewhere it stays as written.
		{"連絡は taro＠example。co。jp までお願いします", "連絡は [email] までお願いします"},
		{"请回复到我的QQ邮箱：12345678@qq。com，谢谢", "请回复到我的QQ邮箱：[email]，谢谢"},
		{"ｔａｒｏ＠ｅｘａｍｐｌｅ。ｃｏｍ", "[email]"},
		{"taro@example｡com", "[email]"},
		{"taro@例子。中国", "[email]"},
		{"です。taro@example.com。ありがとう", "です。[email]。ありがとう"},
		// A local part that mixes scripts is redacted whole when a '.', '_', '+', '-' or '%' joins its runs.
		{"张三.zhang@example.com 是我的邮箱", "[email] 是我的邮箱"},
		{"иван.ivanov@mail.ru", "[email]"},
		{"邮箱 李_li＠example．com", "邮箱 [email]"},
		// Not addresses: an @ without a domain and a dot, and an answer with nothing to redact keeps its full-width forms.
		{"＠channel 5＠100円", "＠channel 5＠100円"},
		{"ＡＢＣ社のサービスに満足", "ＡＢＣ社のサービスに満足"},
		// 4-2-4 numbers are not dates.
		{"0166-12-3456 and 0120-12-3456", "[phone] and [phone]"},
		// Numbers written next to each other are redacted, not kept as one number too long to be a phone; dates among them stay.
		{"090-1234-5678 03-1234-5678", "[phone]"},
		{"4111 1111 1111 1111", "[phone]"},
		{"2026-10-26 2026-10-27", "2026-10-26 2026-10-27"},
		{"2026-10-26 090-1234-5678", "2026-10-26 [phone]"},
		{"２０２６－１０－２６に壊れた", "２０２６－１０－２６に壊れた"},
		{"10-26-2026 again", "10-26-2026 again"},
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
//
// A hostile provider, as answers that steer the model could make it, also puts every answer id of the sample that its chunk did not carry into each theme and each theme's quotes, and reports 100000 positive answers for the chunk that carries a1. When hold is set, each request signals arrived and waits for hold to close.
type textProvider struct {
	*httptest.Server
	mu      sync.Mutex
	chunks  []string
	merges  int
	status  int
	answers int
	hostile bool
	hold    chan struct{}
	arrived chan struct{}
}

func newTextProvider(t *testing.T) *textProvider {
	t.Helper()
	p := &textProvider{status: http.StatusOK, arrived: make(chan struct{}, 16)}
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
	status, hostile, hold := p.status, p.hostile, p.hold
	p.mu.Unlock()
	if hold != nil {
		p.arrived <- struct{}{}
		<-hold
	}
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
		var foreign []string
		if hostile {
			carried := map[string]bool{}
			for _, a := range answers {
				carried[a.ID] = true
			}
			for i := 1; i <= textSampleLimit; i++ {
				if id := fmt.Sprintf("a%d", i); !carried[id] {
					foreign = append(foreign, id)
				}
			}
			if carried["a1"] {
				sentiment["positive"] = 100000
			}
		}
		themes := []any{}
		for _, title := range []string{"Pricing", "Support"} {
			if ids := groups[title]; len(ids) > 0 {
				quotes := append(append([]string{"a99999"}, foreign...), ids[:min(2, len(ids))]...)
				themes = append(themes, map[string]any{"title": title, "description": title + " comes up.", "answerIds": append(slices.Clone(ids), foreign...), "quoteIds": quotes})
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
func newTextEnv(t *testing.T, configured bool) (*env, *textProvider, *TextSummaries) {
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
	e.texts = texts
	return e, provider, texts
}

// runTextJobs works the queued text summary generations as `forma worker` would and marks them completed, returning what each one's Work returned.
func (e *env) runTextJobs() []error {
	e.t.Helper()
	var rows []struct {
		ID   int64
		Args []byte
	}
	if err := e.db.Raw(`SELECT id, args FROM river_job WHERE kind = ? AND state = 'available' ORDER BY id`, TextSummaryArgs{}.Kind()).Scan(&rows).Error; err != nil {
		e.t.Fatal(err)
	}
	w := &textSummaryWorker{texts: e.texts}
	var errs []error
	for _, r := range rows {
		var args TextSummaryArgs
		if err := json.Unmarshal(r.Args, &args); err != nil {
			e.t.Fatal(err)
		}
		e.exec(`UPDATE river_job SET state = 'running', attempt = 1, attempted_at = now() WHERE id = ?`, r.ID)
		errs = append(errs, w.Work(e.t.Context(), &river.Job[TextSummaryArgs]{JobRow: &rivertype.JobRow{ID: r.ID, Kind: TextSummaryArgs{}.Kind(), Attempt: 1}, Args: args}))
		e.exec(`UPDATE river_job SET state = 'completed', finalized_at = now() WHERE id = ?`, r.ID)
	}
	return errs
}

// generateText asks for a summary as the card does: a summary that is current comes back at once (200); one that is queued (202) is generated by working the queue and then read back, as the card's polling does.
func (e *env) generateText(user uuid.UUID, generate string, in map[string]any) TextSummaryState {
	e.t.Helper()
	rec := e.request(user, http.MethodPost, generate, in)
	var state TextSummaryState
	if rec.Code == http.StatusAccepted {
		if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil || state.Status != TextStatusPending || state.PendingSeconds == nil {
			e.t.Fatalf("queued generation answered %s (%v)", rec.Body, err)
		}
		for _, err := range e.runTextJobs() {
			if err != nil {
				e.t.Fatalf("text summary job: %v", err)
			}
		}
		return e.lookupText(user, generate+"/lookup", in)
	}
	decode(e.t, rec, &state)
	return state
}

// lookupText reads the summary card's state.
func (e *env) lookupText(user uuid.UUID, lookup string, in map[string]any) TextSummaryState {
	e.t.Helper()
	var state TextSummaryState
	decode(e.t, e.request(user, http.MethodPost, lookup, in), &state)
	return state
}

// putTextSummary stores a cache entry under key as a generation does.
func putTextSummary(t *testing.T, texts *TextSummaries, key string, entry cachedTextSummary) {
	t.Helper()
	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := texts.cache.Set(t.Context(), key, b, textCacheTTL).Err(); err != nil {
		t.Fatal(err)
	}
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

	state = e.generateText(owner, generate, textInput("q1", TextTargetAnswers, nil))
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
	cached = e.generateText(owner, generate, textInput("q1", TextTargetAnswers, nil))
	if chunks, _ := provider.calls(); len(chunks) != 1 || cached.Summary == nil || !cached.Summary.GeneratedAt.Equal(s.GeneratedAt) {
		t.Fatalf("generate without refresh called the provider again (%d)", len(chunks))
	}
	// A reader in another language is shown the summary in the language it was written in, labelled by it; another filter is another summary.
	other := textInput("q1", TextTargetAnswers, nil)
	other["language"] = "en-US"
	cached = e.lookupText(owner, lookup, other)
	if cached.Summary == nil || cached.Summary.Language != "zh-Hans-CN" || !cached.Summary.GeneratedAt.Equal(s.GeneratedAt) || cached.Status != "" {
		t.Errorf("lookup in another language: %+v", cached)
	}
	decode(t, e.request(owner, http.MethodPost, lookup, textInput("q1", TextTargetAnswers, &response.Criteria{Finished: ptr(true)})), &cached)
	if cached.Summary != nil {
		t.Error("a summary under another filter was served")
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
	cached = e.generateText(owner, generate, textInput("q1", TextTargetAnswers, nil))
	e.exec(`UPDATE responses SET data = '{"q1":"changed my mind"}' WHERE id = ?`, pricey)
	decode(t, e.request(owner, http.MethodPost, lookup, textInput("q1", TextTargetAnswers, nil)), &cached)
	if cached.Summary != nil {
		t.Fatal("a summary of a changed answer was served")
	}

	// Refresh regenerates a current summary.
	cached = e.generateText(owner, generate, textInput("q1", TextTargetAnswers, nil))
	refresh := textInput("q1", TextTargetAnswers, nil)
	refresh["refresh"] = true
	before, _ := provider.calls()
	cached = e.generateText(owner, generate, refresh)
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

	state := e.generateText(owner, generate, textInput("q1", TextTargetAnswers, &response.Criteria{Finished: ptr(true)}))
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

	state := e.generateText(owner, generate, textInput("pick", TextTargetOther, nil))
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

	state := e.generateText(owner, generate, textInput("q1", TextTargetAnswers, nil))
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
	e, provider, texts := newTextEnv(t, true)
	owner, writer, reader, stranger := e.user(), e.user(), e.user(), e.user()
	org, ws := e.organization(owner, nil)
	e.grant(org, ws, writer, "readWrite")
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

	// Generating a summary needs edit access, as every other AI tool does. A member who can only read the responses sees the current summary and is told why they cannot make one; nobody else learns the survey exists.
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	if rec := e.request(reader, http.MethodPost, generate, textInput("q1", TextTargetAnswers, nil)); rec.Code != http.StatusForbidden {
		t.Errorf("generate by a reader: %d %s", rec.Code, rec.Body)
	}
	decode(t, e.request(reader, http.MethodPost, lookup, textInput("q1", TextTargetAnswers, nil)), &state)
	if state.Available || state.Reason != ai.ReasonReadOnly || state.Summary != nil {
		t.Errorf("lookup by a reader before any summary: %+v", state)
	}
	state = e.generateText(writer, generate, textInput("q1", TextTargetAnswers, nil))
	if state.Summary == nil {
		t.Fatal("a member with edit access could not summarize")
	}
	decode(t, e.request(reader, http.MethodPost, lookup, textInput("q1", TextTargetAnswers, nil)), &state)
	if state.Available || state.Reason != ai.ReasonReadOnly || state.Summary == nil {
		t.Errorf("lookup by a reader: %+v", state)
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

	// The organization's hourly budget is spent (the writer's summary took one unit of it): a cached summary is still served, a new one is refused.
	if res, err := texts.limiter.Check(t.Context(), ratelimit.ActionAITextSummary, org.String(), ratelimit.ActionAITextSummary.Limit-1); err != nil || !res.Allowed {
		t.Fatalf("spend the budget: %+v %v", res, err)
	}
	state = e.generateText(owner, generate, textInput("q1", TextTargetAnswers, nil))
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
	state = e.generateText(owner, generate2, textInput("q1", TextTargetAnswers, nil))
	if state.Summary != nil || state.Status != TextStatusFailed || state.Error == nil || state.Error.Code != ai.CodeQuotaExceeded {
		t.Errorf("provider 429: %+v %+v", state, state.Error)
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

// AI on but the organization's "Allow AI to read responses" switch off: the card says so, shows no summary, not even one made while the switch was on, and nothing reaches the provider. Turning it on lets a summary be made again.
func TestTextSummaryNeedsResponseReading(t *testing.T) {
	e, provider, _ := newTextEnv(t, true)
	owner, reader := e.user(), e.user()
	org, ws := e.organization(owner, nil)
	e.grant(org, ws, reader, "read")
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	sv := e.survey(ws, surveyOpts{blocks: textBlocks})
	lookup, generate := textPaths(sv)
	e.response(sv, row{at: at(1), finished: true, data: `{"q1":"the price"}`})

	var state TextSummaryState
	state = e.generateText(owner, generate, textInput("q1", TextTargetAnswers, nil))
	if state.Summary == nil {
		t.Fatal("no summary with both switches on")
	}

	e.exec(`UPDATE organizations SET is_ai_response_reading_enabled = false WHERE id = ?`, org)
	for _, user := range []uuid.UUID{owner, reader} {
		state = TextSummaryState{}
		decode(t, e.request(user, http.MethodPost, lookup, textInput("q1", TextTargetAnswers, nil)), &state)
		if state.Available || state.Reason != ai.ReasonResponseReadingDisabled || state.Summary != nil {
			t.Errorf("lookup with reading off: %+v", state)
		}
	}
	before, _ := provider.calls()
	refresh := textInput("q1", TextTargetAnswers, nil)
	refresh["refresh"] = true
	for _, in := range []map[string]any{textInput("q1", TextTargetAnswers, nil), refresh} {
		if rec := e.request(owner, http.MethodPost, generate, in); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), ai.CodeResponseReadingDisabled) {
			t.Errorf("generate with reading off: %d %s", rec.Code, rec.Body)
		}
	}
	if after, _ := provider.calls(); len(after) != len(before) {
		t.Errorf("the provider was called %d times with reading off", len(after)-len(before))
	}

	// AI itself off is still reported as such, whatever the reading switch says.
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = false WHERE id = ?`, org)
	decode(t, e.request(owner, http.MethodPost, lookup, textInput("q1", TextTargetAnswers, nil)), &state)
	if state.Reason != ai.ReasonNotEnabled {
		t.Errorf("lookup with both off: %+v", state)
	}

	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true, is_ai_response_reading_enabled = true WHERE id = ?`, org)
	decode(t, e.request(owner, http.MethodPost, lookup, textInput("q1", TextTargetAnswers, nil)), &state)
	if !state.Available || state.Summary == nil {
		t.Errorf("lookup with reading back on: %+v", state)
	}
}

func TestTextSummaryMemberLimit(t *testing.T) {
	e, _, texts := newTextEnv(t, true)
	owner, writer := e.user(), e.user()
	org, ws := e.organization(owner, nil)
	e.grant(org, ws, writer, "readWrite")
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	sv := e.survey(ws, surveyOpts{blocks: textBlocks})
	_, generate := textPaths(sv)
	e.response(sv, row{at: at(1), finished: true, data: `{"q1":"the price"}`})

	// One member's hourly budget is spent: they are refused, another member of the organization is not.
	if res, err := texts.limiter.Check(t.Context(), ratelimit.ActionAITextSummaryMember, owner.String(), ratelimit.ActionAITextSummaryMember.Limit); err != nil || !res.Allowed {
		t.Fatalf("spend the budget: %+v %v", res, err)
	}
	if rec := e.request(owner, http.MethodPost, generate, textInput("q1", TextTargetAnswers, nil)); rec.Code != http.StatusTooManyRequests {
		t.Errorf("over the member limit: %d %s", rec.Code, rec.Body)
	}
	state := e.generateText(writer, generate, textInput("q1", TextTargetAnswers, nil))
	if state.Summary == nil {
		t.Error("another member was refused")
	}
	// The refused request did not charge the organization.
	if res, err := texts.limiter.Check(t.Context(), ratelimit.ActionAITextSummary, org.String(), ratelimit.ActionAITextSummary.Limit-1); err != nil || !res.Allowed {
		t.Errorf("the organization was charged for a refused request: %+v %v", res, err)
	}
}

func TestChunkSentimentIsBounded(t *testing.T) {
	cases := []struct {
		positive, neutral, negative, n int
		want                           [3]int
	}{
		{3, 4, 3, 10, [3]int{3, 4, 3}},
		{-5, 2, 1, 10, [3]int{0, 2, 1}},
		{100000, 0, 0, 10, [3]int{10, 0, 0}},
		{100000, 10, 0, 10, [3]int{5, 5, 0}},
		{6, 6, 6, 9, [3]int{3, 3, 3}},
	}
	for _, c := range cases {
		if got := chunkSentiment(c.positive, c.neutral, c.negative, c.n); got != c.want {
			t.Errorf("%+v: %v", c, got)
		}
	}
}

func TestTextSummaryKeepsEachChunkToItsOwnAnswers(t *testing.T) {
	e, provider, _ := newTextEnv(t, true)
	provider.hostile = true
	owner := e.user()
	org, ws := e.organization(owner, nil)
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	sv := e.survey(ws, surveyOpts{blocks: textBlocks})
	_, generate := textPaths(sv)
	e.exec(`INSERT INTO responses (survey_id, finished, data)
		SELECT ?, true, jsonb_build_object('q1', CASE WHEN g % 2 = 0 THEN 'the price ' ELSE 'the support ' END || repeat('detail ', 40) || g) FROM generate_series(1, 600) g`, sv)

	state := e.generateText(owner, generate, textInput("q1", TextTargetAnswers, nil))
	s := state.Summary
	if chunks, _ := provider.calls(); s == nil || len(chunks) < 2 {
		t.Fatalf("summary %+v over %d chunks", s, len(chunks))
	}
	// A theme counts only the answers its own chunk matched to it, and quotes only those.
	if len(s.Themes) != 2 || s.Themes[0].Count+s.Themes[1].Count != s.SampledCount {
		t.Errorf("themes counted answers of other chunks: %+v", s.Themes)
	}
	for _, theme := range s.Themes {
		keyword := map[string]string{"Pricing": "price", "Support": "support"}[theme.Title]
		for _, q := range theme.Quotes {
			if !strings.Contains(q.Text, keyword) {
				t.Errorf("%s quotes %q", theme.Title, q.Text)
			}
		}
	}
	// One chunk claiming 100000 positive answers weighs no more than the answers it carried.
	if s.Sentiment.Positive >= 50 || s.Sentiment.Overall == SentimentPositive {
		t.Errorf("sentiment %+v", s.Sentiment)
	}
}

// A generation runs in `forma worker`, not in the request: the request answers 202 at once, the card reads it pending until the job has stored the summary, and asking again meanwhile queues and charges nothing more. The job pays the provider once.
func TestTextSummaryIsGeneratedInTheBackground(t *testing.T) {
	e, provider, texts := newTextEnv(t, true)
	owner, writer := e.user(), e.user()
	org, ws := e.organization(owner, nil)
	e.grant(org, ws, writer, "readWrite")
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	sv := e.survey(ws, surveyOpts{blocks: textBlocks})
	lookup, generate := textPaths(sv)
	e.response(sv, row{at: at(1), finished: true, data: `{"q1":"the price"}`})
	in := textInput("q1", TextTargetAnswers, nil)

	for _, user := range []uuid.UUID{owner, writer} {
		rec := e.request(user, http.MethodPost, generate, in)
		var state TextSummaryState
		if err := json.Unmarshal(rec.Body.Bytes(), &state); rec.Code != http.StatusAccepted || err != nil || state.Status != TextStatusPending || state.PendingSeconds == nil || state.Summary != nil {
			t.Fatalf("generate: %d %s", rec.Code, rec.Body)
		}
	}
	if n := e.count(`SELECT count(*) FROM river_job WHERE kind = ? AND queue = 'ai' AND max_attempts = 1`, TextSummaryArgs{}.Kind()); n != 1 {
		t.Fatalf("%d generations queued for one summary", n)
	}
	if chunks, _ := provider.calls(); len(chunks) != 0 {
		t.Fatal("the request called the provider")
	}
	// Only the first request was charged.
	if res, err := texts.limiter.Check(t.Context(), ratelimit.ActionAITextSummaryMember, writer.String(), ratelimit.ActionAITextSummaryMember.Limit); err != nil || !res.Allowed {
		t.Errorf("a request that queued nothing was charged: %+v %v", res, err)
	}
	state := e.lookupText(owner, lookup, in)
	if state.Status != TextStatusPending || state.PendingSeconds == nil || state.Summary != nil {
		t.Fatalf("lookup while queued: %+v", state)
	}

	for _, err := range e.runTextJobs() {
		if err != nil {
			t.Fatal(err)
		}
	}
	state = e.lookupText(owner, lookup, in)
	if state.Status != "" || state.Summary == nil {
		t.Fatalf("lookup after the job: %+v", state)
	}
	if chunks, _ := provider.calls(); len(chunks) != 1 {
		t.Errorf("%d provider calls", len(chunks))
	}

	// The summary is current: asking again answers it at once and queues nothing.
	rec := e.request(owner, http.MethodPost, generate, in)
	if rec.Code != http.StatusOK || e.count(`SELECT count(*) FROM river_job WHERE kind = ?`, TextSummaryArgs{}.Kind()) != 1 {
		t.Errorf("generate of a current summary: %d %s", rec.Code, rec.Body)
	}
	// A refresh is queued again once the first generation has finished, and is pending next to the summary it refreshes.
	refresh := textInput("q1", TextTargetAnswers, nil)
	refresh["refresh"] = true
	rec = e.request(owner, http.MethodPost, generate, refresh)
	state = TextSummaryState{}
	if err := json.Unmarshal(rec.Body.Bytes(), &state); rec.Code != http.StatusAccepted || err != nil || state.Summary == nil {
		t.Fatalf("refresh: %d %s", rec.Code, rec.Body)
	}
	state = e.lookupText(owner, lookup, in)
	if state.Status != TextStatusPending || state.Summary == nil {
		t.Errorf("lookup while refreshing: %+v", state)
	}
}

// A generation that fails is reported by the lookup with the error it ended with, until it is asked for again; one the organization's switches turned off in the meantime never reaches the provider.
func TestTextSummaryFailuresAreReported(t *testing.T) {
	e, provider, _ := newTextEnv(t, true)
	owner := e.user()
	org, ws := e.organization(owner, nil)
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	sv := e.survey(ws, surveyOpts{blocks: textBlocks})
	lookup, generate := textPaths(sv)
	e.response(sv, row{at: at(1), finished: true, data: `{"q1":"the price"}`})
	in := textInput("q1", TextTargetAnswers, nil)

	// The provider failing is an unexpected error: the job fails with it, the card says to try again.
	provider.mu.Lock()
	provider.status = http.StatusInternalServerError
	provider.mu.Unlock()
	if rec := e.request(owner, http.MethodPost, generate, in); rec.Code != http.StatusAccepted {
		t.Fatalf("generate %d %s", rec.Code, rec.Body)
	}
	if errs := e.runTextJobs(); len(errs) != 1 || errs[0] == nil {
		t.Fatalf("job errors %v", errs)
	}
	state := e.lookupText(owner, lookup, in)
	if state.Status != TextStatusFailed || state.Error == nil || state.Error.Code != CodeTextSummaryFailed || state.Summary != nil {
		t.Fatalf("lookup after a failure: %+v", state)
	}

	// Asked again, it is pending again, and the switch turned off before the job ran stops it.
	provider.mu.Lock()
	provider.status = http.StatusOK
	provider.mu.Unlock()
	if rec := e.request(owner, http.MethodPost, generate, in); rec.Code != http.StatusAccepted {
		t.Fatalf("generate again %d %s", rec.Code, rec.Body)
	}
	state = e.lookupText(owner, lookup, in)
	if state.Status != TextStatusPending || state.Error != nil {
		t.Fatalf("lookup when asked again: %+v", state)
	}
	before, _ := provider.calls()
	e.exec(`UPDATE organizations SET is_ai_response_reading_enabled = false WHERE id = ?`, org)
	if errs := e.runTextJobs(); len(errs) != 1 || errs[0] != nil {
		t.Fatalf("an expected failure failed the job: %v", errs)
	}
	if after, _ := provider.calls(); len(after) != len(before) {
		t.Error("a generation reached the provider after reading was turned off")
	}
	e.exec(`UPDATE organizations SET is_ai_response_reading_enabled = true WHERE id = ?`, org)
	state = e.lookupText(owner, lookup, in)
	if state.Status != TextStatusFailed || state.Error == nil || state.Error.Code != ai.CodeResponseReadingDisabled {
		t.Errorf("lookup after a refused generation: %+v", state)
	}
}

// A pending status outlived by the summary it asked for, as when a duplicate request marks it after the job has cleared it, is not reported.
func TestTextSummaryIgnoresALeftoverPendingStatus(t *testing.T) {
	e, _, texts := newTextEnv(t, true)
	owner := e.user()
	org, ws := e.organization(owner, nil)
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	sv := e.survey(ws, surveyOpts{blocks: textBlocks})
	lookup, generate := textPaths(sv)
	e.response(sv, row{at: at(1), finished: true, data: `{"q1":"the price"}`})
	in := textInput("q1", TextTargetAnswers, nil)
	state := e.generateText(owner, generate, in)
	if state.Summary == nil {
		t.Fatal("no summary")
	}
	scope, err := texts.scope(t.Context(), owner, sv, TextSummaryInput{ElementID: "q1", Target: TextTargetAnswers, Language: "zh-Hans-CN"})
	if err != nil {
		t.Fatal(err)
	}
	texts.setStatus(t.Context(), scope.key, textStatus{Status: TextStatusPending, Since: state.Summary.GeneratedAt.Add(-time.Second)}, time.Minute)
	state = e.lookupText(owner, lookup, in)
	if state.Status != "" || state.Summary == nil {
		t.Errorf("lookup with a leftover pending status: %+v", state)
	}
}

// A filter the job reads back from River differs in its bytes from the one it was asked with: jsonb decodes the escapes of a condition value and reorders the keys of an object. The summary is still stored under the key it was asked for, and the lookup finds it.
func TestTextSummaryFilterSurvivesTheQueue(t *testing.T) {
	e, _, _ := newTextEnv(t, true)
	owner := e.user()
	org, ws := e.organization(owner, nil)
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	sv := e.survey(ws, surveyOpts{blocks: textBlocks})
	lookup, generate := textPaths(sv)
	e.response(sv, row{at: at(1), finished: true, data: `{"q1":"the price"}`})
	// The filter keeps the answers containing "price", its p written as the escape \u0070.
	in := rawJSON(`{"elementId":"q1","target":"answers","language":"zh-Hans-CN","filterCriteria":{"data":{"q1":{"op":"contains","value":"\u0070rice"}}}}`)

	if rec := e.request(owner, http.MethodPost, generate, in); rec.Code != http.StatusAccepted {
		t.Fatalf("generate: %d %s", rec.Code, rec.Body)
	}
	for _, err := range e.runTextJobs() {
		if err != nil {
			t.Fatal(err)
		}
	}
	var state TextSummaryState
	decode(t, e.request(owner, http.MethodPost, lookup, in), &state)
	if state.Status != "" || state.Summary == nil || state.Summary.AnswerCount != 1 {
		t.Fatalf("lookup after the job: %+v", state)
	}
	// The summary is current: asking again answers it at once and queues nothing.
	if rec := e.request(owner, http.MethodPost, generate, in); rec.Code != http.StatusOK {
		t.Errorf("generate again: %d %s", rec.Code, rec.Body)
	}
}

// A generation of the same summary already queued or running is not charged again, even when its pending status is gone (expired while the job waited, or lost with a killed worker): the request marks it pending again instead.
func TestTextSummaryDuplicateIsNotCharged(t *testing.T) {
	e, _, texts := newTextEnv(t, true)
	owner := e.user()
	org, ws := e.organization(owner, nil)
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	sv := e.survey(ws, surveyOpts{blocks: textBlocks})
	lookup, generate := textPaths(sv)
	e.response(sv, row{at: at(1), finished: true, data: `{"q1":"the price"}`})
	in := textInput("q1", TextTargetAnswers, nil)

	if rec := e.request(owner, http.MethodPost, generate, in); rec.Code != http.StatusAccepted {
		t.Fatalf("generate: %d %s", rec.Code, rec.Body)
	}
	scope, err := texts.scope(t.Context(), owner, sv, TextSummaryInput{ElementID: "q1", Target: TextTargetAnswers, Language: "zh-Hans-CN"})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		texts.clearStatus(t.Context(), scope.key)
		rec := e.request(owner, http.MethodPost, generate, in)
		var state TextSummaryState
		if err := json.Unmarshal(rec.Body.Bytes(), &state); rec.Code != http.StatusAccepted || err != nil || state.Status != TextStatusPending {
			t.Fatalf("generate %d with the job queued: %d %s", i, rec.Code, rec.Body)
		}
		if state := e.lookupText(owner, lookup, in); state.Status != TextStatusPending {
			t.Fatalf("lookup %d with the job queued: %+v", i, state)
		}
	}
	if n := e.count(`SELECT count(*) FROM river_job WHERE kind = ?`, TextSummaryArgs{}.Kind()); n != 1 {
		t.Fatalf("%d jobs", n)
	}
	// Only the request that queued the job was charged: the rest of the member's and the organization's budgets is still there.
	if res, err := texts.limiter.Check(t.Context(), ratelimit.ActionAITextSummaryMember, owner.String(), ratelimit.ActionAITextSummaryMember.Limit-1); err != nil || !res.Allowed {
		t.Errorf("the member was charged for a duplicate: %+v %v", res, err)
	}
	if res, err := texts.limiter.Check(t.Context(), ratelimit.ActionAITextSummary, org.String(), ratelimit.ActionAITextSummary.Limit-1); err != nil || !res.Allowed {
		t.Errorf("the organization was charged for a duplicate: %+v %v", res, err)
	}
}

// Only an open-text question that asks for text has answers worth summarizing: one asking for an email address, a phone number, a URL or a number is not offered, and refused.
func TestTextSummaryOnlyOfFreeText(t *testing.T) {
	e, _, _ := newTextEnv(t, true)
	owner := e.user()
	org, ws := e.organization(owner, nil)
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	sv := e.survey(ws, surveyOpts{blocks: `[{"id":"b1","name":"B1","elements":[
		{"id":"plain","type":"openText","headline":{"default":"Anything else?"},"required":false},
		{"id":"ot_email","type":"openText","headline":{"default":"Email?"},"required":false,"inputType":"email"},
		{"id":"ot_phone","type":"openText","headline":{"default":"Phone?"},"required":false,"inputType":"phone"},
		{"id":"ot_url","type":"openText","headline":{"default":"Website?"},"required":false,"inputType":"url"},
		{"id":"ot_number","type":"openText","headline":{"default":"How many?"},"required":false,"inputType":"number"}
	]}]`})
	lookup, generate := textPaths(sv)
	e.response(sv, row{at: at(1), finished: true, data: `{"plain":"the price","ot_email":"ada@example.com","ot_phone":"+1 555 010 2030","ot_url":"https://example.com","ot_number":"3"}`})

	var state TextSummaryState
	decode(t, e.request(owner, http.MethodPost, lookup, textInput("plain", TextTargetAnswers, nil)), &state)
	if !state.Available {
		t.Errorf("an open-text question without an input type: %+v", state)
	}
	for _, id := range []string{"ot_email", "ot_phone", "ot_url", "ot_number"} {
		decode(t, e.request(owner, http.MethodPost, lookup, textInput(id, TextTargetAnswers, nil)), &state)
		if state.Available || state.Reason != ReasonNotFreeText || state.Summary != nil {
			t.Errorf("lookup of %s: %+v", id, state)
		}
		if rec := e.request(owner, http.MethodPost, generate, textInput(id, TextTargetAnswers, nil)); rec.Code != http.StatusBadRequest {
			t.Errorf("generate of %s: %d %s", id, rec.Code, rec.Body)
		}
	}
}

func TestTextSummaryStaleDeleteSparesAFreshSummary(t *testing.T) {
	_, _, texts := newTextEnv(t, true)
	ctx := t.Context()
	key := textKeyPrefix(uuid.New()) + "stale"
	putTextSummary(t, texts, key, cachedTextSummary{Fingerprint: "old"})
	stale := texts.cached(ctx, key)
	// A generation stores a current summary between the lookup's read and its delete.
	putTextSummary(t, texts, key, cachedTextSummary{Fingerprint: "new"})
	texts.forget(ctx, key, stale.raw)
	fresh := texts.cached(ctx, key)
	if fresh == nil || fresh.Fingerprint != "new" {
		t.Fatalf("the fresh summary was deleted: %+v", fresh)
	}
	texts.forget(ctx, key, fresh.raw)
	if texts.cached(ctx, key) != nil {
		t.Error("a stale summary was kept")
	}
}

func TestTextSummaryIsDroppedOnQuestionEditsAndReset(t *testing.T) {
	e, _, texts := newTextEnv(t, true)
	owner := e.user()
	org, ws := e.organization(owner, nil)
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	sv := e.survey(ws, surveyOpts{blocks: textBlocks})
	lookup, generate := textPaths(sv)
	e.response(sv, row{at: at(1), finished: true, data: `{"pick":["Speed","the price"]}`})
	e.response(sv, row{at: at(2), finished: true, data: `{"q1":"the support"}`})

	// Renaming a choice changes which values are Other: the summary of the Other values is stale.
	var state TextSummaryState
	state = e.generateText(owner, generate, textInput("pick", TextTargetOther, nil))
	e.exec(`UPDATE surveys SET blocks = replace(blocks::text, '"Speed"', '"Pace"')::jsonb WHERE id = ?`, sv)
	decode(t, e.request(owner, http.MethodPost, lookup, textInput("pick", TextTargetOther, nil)), &state)
	if state.Summary != nil {
		t.Error("a summary of the Other values outlived a renamed choice")
	}
	// So is one of a question whose headline changed.
	state = e.generateText(owner, generate, textInput("q1", TextTargetAnswers, nil))
	e.exec(`UPDATE surveys SET blocks = replace(blocks::text, 'What should we improve', 'What should we fix')::jsonb WHERE id = ?`, sv)
	decode(t, e.request(owner, http.MethodPost, lookup, textInput("q1", TextTargetAnswers, nil)), &state)
	if state.Summary != nil {
		t.Error("a summary outlived its question's edit")
	}

	// A reset drops every summary of the survey at once.
	state = e.generateText(owner, generate, textInput("q1", TextTargetAnswers, nil))
	keys, err := texts.cache.Keys(t.Context(), textKeyPrefix(sv)+"*").Result()
	if err != nil || len(keys) == 0 {
		t.Fatalf("cached summaries %v %v", keys, err)
	}
	if rec := e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/reset", nil); rec.Code != http.StatusOK {
		t.Fatalf("reset %d %s", rec.Code, rec.Body)
	}
	if keys, err := texts.cache.Keys(t.Context(), textKeyPrefix(sv)+"*").Result(); err != nil || len(keys) != 0 {
		t.Errorf("summaries left after a reset: %v %v", keys, err)
	}
}
