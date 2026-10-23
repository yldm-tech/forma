package survey

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yldm-tech/forma/apps/server/internal/ai"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/v3api"
)

// generatedDraftJSON is a model answer of forma2's ForAI schema: an open text, an NPS and a choice question in one block, then a CES block.
const generatedDraftJSON = `{"language":"en-US","name":"Onboarding feedback","description":"Why new users stop","welcomeCard":{"enabled":true,"headline":null,"subheader":null,"buttonLabel":null},"ending":null,"blocks":[` +
	`{"name":"Experience","questions":[` +
	`{"type":"openText","headline":" What stopped you? ","subheader":null,"required":true,"placeholder":"Tell us","longAnswer":true,"choices":null,"rows":null,"columns":null,"lowerLabel":null,"upperLabel":null,"scale":null,"format":null,"range":null},` +
	`{"type":"nps","headline":"How likely are you to recommend us?","subheader":null,"required":false,"placeholder":null,"longAnswer":null,"choices":null,"rows":null,"columns":null,"lowerLabel":"Not likely","upperLabel":"Very likely","scale":null,"format":null,"range":null},` +
	`{"type":"multipleChoiceSingle","headline":"Which step was hardest?","subheader":null,"required":true,"placeholder":null,"longAnswer":null,"choices":["Setup","Inviting the team"],"rows":null,"columns":null,"lowerLabel":null,"upperLabel":null,"scale":null,"format":null,"range":null}]},` +
	`{"name":"Effort","questions":[{"type":"ces","headline":"How easy was setup?","subheader":null,"required":true,"placeholder":null,"longAnswer":null,"choices":null,"rows":null,"columns":null,"lowerLabel":null,"upperLabel":null,"scale":"number","format":null,"range":"5"}]}]}`

// aiProvider is an OpenAI-compatible endpoint that writes survey drafts and translations; draft, when set, replaces the draft it answers with.
type aiProvider struct {
	*httptest.Server
	mu       sync.Mutex
	bodies   []map[string]any
	draft    string
	status   int
	finish   string
	onStream func(r *http.Request)
}

func newAIProvider(t *testing.T) *aiProvider {
	t.Helper()
	p := &aiProvider{draft: generatedDraftJSON, status: http.StatusOK, finish: "stop"}
	p.Server = httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.Close)
	return p
}

func (p *aiProvider) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	p.mu.Lock()
	p.bodies = append(p.bodies, body)
	draft, status, finish, onStream := p.draft, p.status, p.finish, p.onStream
	p.mu.Unlock()
	if status != http.StatusOK {
		w.Header().Set("Retry-After", "17")
		w.Header().Set("Retry-After-Ms", "1")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error":{"message":"slow down"}}`)
		return
	}
	format, _ := body["response_format"].(map[string]any)
	schema, _ := format["json_schema"].(map[string]any)
	content := draft
	if schema["name"] != "FormaSurveyDraft" {
		content = translate(body)
	}
	if body["stream"] == true {
		w.Header().Set("Content-Type", "text/event-stream")
		third := len(content) / 3
		for i, piece := range []string{content[:third], content[third : 2*third], content[2*third:]} {
			reason := ""
			if i == 2 {
				reason = fmt.Sprintf(`,"finish_reason":%q`, finish)
			}
			delta, _ := json.Marshal(piece)
			_, _ = fmt.Fprintf(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%s}%s}]}\n\n", delta, reason)
			w.(http.Flusher).Flush()
			if i == 0 && onStream != nil {
				onStream(r)
			}
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		return
	}
	answer, _ := json.Marshal(map[string]any{"id": "c", "object": "chat.completion", "created": 1, "model": "m",
		"choices": []any{map[string]any{"index": 0, "finish_reason": finish, "message": map[string]any{"role": "assistant", "content": content}}},
		"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}})
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(answer)
}

var translateTarget = regexp.MustCompile(`Translate each item from (\S+) to (\S+)\.`)

// translate answers a translation request with the target language in front of each text; French comes back with its first item empty.
func translate(body map[string]any) string {
	messages, _ := body["messages"].([]any)
	system, _ := messages[0].(map[string]any)["content"].(string)
	user, _ := messages[1].(map[string]any)["content"].(string)
	target := translateTarget.FindStringSubmatch(system)[2]
	var items []struct{ ID, Text string }
	_ = json.Unmarshal([]byte(user), &items)
	out := map[string]string{}
	for i, item := range items {
		out[item.ID] = target + ": " + item.Text
		if target == "fr-FR" && i == 0 {
			out[item.ID] = ""
		}
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func (p *aiProvider) requests() []map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.bodies)
}

func (p *aiProvider) set(f func(p *aiProvider)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	f(p)
}

// newAIEnv is the survey module on an instance whose AI provider is the returned fake.
func newAIEnv(t *testing.T) (*env, *aiProvider) {
	t.Helper()
	provider := newAIProvider(t)
	e := newEnv(t, func(c *config.Config) {
		c.AI = config.AIConfig{Provider: config.AIProviderOpenAICompatible, Model: "qwen", OpenAICompatibleBaseURL: provider.URL, OpenAICompatibleSupportsStructuredOutputs: "1"}
	})
	return e, provider
}

type generateResponse struct {
	Data struct {
		Language   string             `json:"language"`
		Payload    map[string]any     `json:"payload"`
		Validation GenerateValidation `json:"validation"`
	} `json:"data"`
}

func at(v any, path string) any {
	for _, key := range strings.Split(path, ".") {
		switch x := v.(type) {
		case map[string]any:
			v = x[key]
		case []any:
			var i int
			if _, err := fmt.Sscan(key, &i); err != nil || i >= len(x) {
				return nil
			}
			v = x[i]
		default:
			return nil
		}
	}
	return v
}

func expectAt(t *testing.T, v any, want map[string]any) {
	t.Helper()
	for path, value := range want {
		got := at(v, path)
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(value)
		if string(a) != string(b) {
			t.Errorf("%s = %s, want %s", path, a, b)
		}
	}
}

// forma2's generate service: the draft becomes a create payload with forma2's defaults, rating-like questions in blocks of their own, validated as POST /api/v3/surveys validates it, and creatable as it is.
func TestV3GenerateWritesACreatablePayload(t *testing.T) {
	e, provider := newAIEnv(t)
	owner := e.user()
	org, ws := e.organization(owner)
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	prompt := "Understand why new users stop during onboarding and what would help them finish setup."

	var res generateResponse
	e.ok(e.v3Key(e.apiKey(org, ws, "write"), http.MethodPost, surveysPath("/generate"), fmt.Sprintf(`{"workspaceId": %q, "prompt": %q}`, ws, prompt)), http.StatusOK, &res)
	if res.Data.Language != "en-US" || !res.Data.Validation.Valid || len(res.Data.Validation.InvalidParams) != 0 || !slices.Equal(res.Data.Validation.Languages, []languageRequest{{Code: "en-US", Default: true, Enabled: true}}) {
		t.Fatalf("result %+v", res.Data.Validation)
	}
	p := res.Data.Payload
	expectAt(t, p, map[string]any{
		"workspaceId":                              ws.String(),
		"type":                                     "link",
		"name":                                     "Onboarding feedback",
		"status":                                   "draft",
		"defaultLanguage":                          "en-US",
		"languages":                                []any{map[string]any{"code": "en-US", "default": true, "enabled": true}},
		"metadata":                                 map[string]any{"title": map[string]any{"en-US": "Onboarding feedback"}, "description": map[string]any{"en-US": "Why new users stop"}},
		"welcomeCard":                              map[string]any{"enabled": true, "headline": map[string]any{"en-US": "Onboarding feedback"}, "buttonLabel": map[string]any{"en-US": "Start"}, "timeToFinish": true, "showResponseCount": false},
		"blocks.0.name":                            "Experience",
		"blocks.0.elements.0.type":                 "openText",
		"blocks.0.elements.0.headline":             map[string]any{"en-US": "What stopped you?"},
		"blocks.0.elements.0.placeholder":          map[string]any{"en-US": "Tell us"},
		"blocks.0.elements.0.longAnswer":           true,
		"blocks.0.elements.0.inputType":            "text",
		"blocks.0.elements.0.charLimit":            map[string]any{"enabled": false},
		"blocks.0.elements.0.isDraft":              true,
		"blocks.0.elements.0.required":             true,
		"blocks.1.name":                            "How likely are you to recommend us?",
		"blocks.1.elements.0.type":                 "nps",
		"blocks.1.elements.0.lowerLabel":           map[string]any{"en-US": "Not likely"},
		"blocks.1.elements.0.isColorCodingEnabled": false,
		"blocks.2.name":                            "Experience",
		"blocks.2.elements.0.choices":              []any{map[string]any{"id": "choice_1", "label": map[string]any{"en-US": "Setup"}}, map[string]any{"id": "choice_2", "label": map[string]any{"en-US": "Inviting the team"}}},
		"blocks.2.elements.0.shuffleOption":        "none",
		"blocks.2.elements.0.displayType":          "list",
		"blocks.3.elements.0.type":                 "ces",
		"blocks.3.elements.0.range":                5,
		"blocks.3.elements.0.scale":                "number",
		"endings.0.type":                           "endScreen",
		"endings.0.headline":                       map[string]any{"en-US": "Thanks for your feedback"},
		"hiddenFields":                             map[string]any{"enabled": false},
		"variables":                                []any{},
		"distribution":                             nil,
		"blocks.4":                                 nil,
	})
	for i, id := range []string{at(p, "blocks.0.elements.0.id").(string), at(p, "blocks.3.elements.0.id").(string)} {
		if !regexp.MustCompile(fmt.Sprintf(`^q_%d_[0-9a-z]{8}$`, []int{1, 4}[i])).MatchString(id) {
			t.Fatalf("element id %q", id)
		}
	}

	// The model call: forma2's schema, prompts and settings.
	body := provider.requests()[0]
	expectAt(t, body, map[string]any{
		"model":                            "qwen",
		"temperature":                      0.2,
		"max_tokens":                       8192,
		"response_format.type":             "json_schema",
		"response_format.json_schema.name": "FormaSurveyDraft",
		"response_format.json_schema.description":     "A concise Forma survey draft that can be converted to a v3 create payload.",
		"response_format.json_schema.strict":          true,
		"response_format.json_schema.schema.required": []any{"language", "name", "description", "welcomeCard", "ending", "blocks"},
	})
	system, user := at(body, "messages.0.content").(string), at(body, "messages.1.content").(string)
	if !strings.HasPrefix(system, "You generate concise Forma survey drafts.\nReturn only data that matches the provided schema.\n") || !strings.Contains(system, "Allowed survey languages: de-DE, en-US, es-ES,") || !strings.HasSuffix(system, "The final product will be created as a link survey draft.") {
		t.Fatalf("system prompt %q", system)
	}
	if !strings.HasPrefix(user, "Create a draft link survey from this request.\n") || !strings.Contains(user, "\nPreferred survey language: en-US.\n") || !strings.HasSuffix(user, "\n\nUser request:\n"+prompt) {
		t.Fatalf("prompt %q", user)
	}

	// The payload is a create body as it is.
	payload, _ := json.Marshal(p)
	var created resource
	e.ok(e.v3(owner, http.MethodPost, surveysPath("?createdFrom=ai"), string(payload)), http.StatusCreated, &created)
	if e.count(`SELECT count(*) FROM surveys WHERE workspace_id = ?`, ws) != 1 {
		t.Fatal("the generated payload was not created")
	}
}

// forma2's finishV3SurveyGeneration: each further language is translated in order; one that comes back with a gap is dropped, the draft is kept.
func TestV3GenerateTranslatesFurtherLanguages(t *testing.T) {
	e, provider := newAIEnv(t)
	owner := e.user()
	org, ws := e.organization(owner)
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	e.exec(`INSERT INTO languages (workspace_id, code) VALUES (?, 'de-DE')`, ws)

	var res generateResponse
	body := fmt.Sprintf(`{"workspaceId": %q, "prompt": "Understand why new users stop during onboarding", "type": "app", "languages": ["en-US", "de-DE", "fr-FR"]}`, ws)
	e.ok(e.v3(owner, http.MethodPost, surveysPath("/generate"), body), http.StatusOK, &res)
	want := []languageRequest{{Code: "en-US", Default: true, Enabled: true}, {Code: "de-DE", Enabled: true}}
	if !slices.Equal(res.Data.Validation.Languages, want) {
		t.Fatalf("languages %+v", res.Data.Validation.Languages)
	}
	expectAt(t, res.Data.Payload, map[string]any{
		"languages":                     []any{map[string]any{"code": "en-US", "default": true, "enabled": true}, map[string]any{"code": "de-DE", "default": false, "enabled": true}},
		"metadata.title":                map[string]any{"en-US": "Onboarding feedback", "de-DE": "de-DE: Onboarding feedback"},
		"blocks.2.elements.0.choices.1": map[string]any{"id": "choice_2", "label": map[string]any{"en-US": "Inviting the team", "de-DE": "de-DE: Inviting the team"}},
		"blocks.0.name":                 "Experience",
		"distribution":                  map[string]any{"displayOption": "displayOnce", "triggers": []any{}},
	})
	reqs := provider.requests()
	if len(reqs) != 3 {
		t.Fatalf("%d provider calls", len(reqs))
	}
	if !strings.Contains(at(reqs[0], "messages.0.content").(string), "This is an in-app survey shown inside the user's web or mobile app") {
		t.Fatal("the app survey line is missing")
	}
	if !strings.Contains(at(reqs[1], "messages.0.content").(string), "from en-US to de-DE") || !strings.Contains(at(reqs[2], "messages.0.content").(string), "from en-US to fr-FR") {
		t.Fatal("languages were not translated in order")
	}
	if !strings.Contains(e.logs.String(), "Dropped a generated survey language that was only partially translated") {
		t.Fatal("the dropped language was not logged")
	}
	payload, _ := json.Marshal(res.Data.Payload)
	e.ok(e.v3(owner, http.MethodPost, surveysPath(""), string(payload)), http.StatusCreated, nil)
}

// forma2's mapV3SurveyGenerateError.
func TestV3GenerateErrors(t *testing.T) {
	e, provider := newAIEnv(t)
	owner := e.user()
	org, ws := e.organization(owner)
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	body := fmt.Sprintf(`{"workspaceId": %q, "prompt": "Understand why new users stop during onboarding"}`, ws)
	generate := func() *httptest.ResponseRecorder { return e.v3(owner, http.MethodPost, surveysPath("/generate"), body) }

	provider.set(func(p *aiProvider) { p.status = http.StatusTooManyRequests })
	rec := generate()
	p := e.problem(rec, http.StatusTooManyRequests, "The AI provider is temporarily rate-limited. Try again shortly.")
	if p.Code != "too_many_requests" || rec.Header().Get("Retry-After") != "17" {
		t.Fatalf("%+v %v", p, rec.Header())
	}

	provider.set(func(p *aiProvider) { p.status, p.finish = http.StatusOK, "length" })
	if p := e.problem(generate(), http.StatusUnprocessableEntity, "The generated survey exceeded the AI output token limit. Simplify the prompt or split it into smaller surveys."); p.Code != "ai_output_too_long" {
		t.Fatalf("%+v", p)
	}

	// A draft that breaks the schema's refinements is no object at all, as the AI SDK rejected it.
	provider.set(func(p *aiProvider) {
		p.finish = "stop"
		p.draft = strings.Replace(generatedDraftJSON, `"range":"5"`, `"range":"10"`, 1)
	})
	if p := e.problem(generate(), http.StatusBadGateway, "The AI provider could not generate a valid survey draft. Try again or add more detail."); p.Code != "bad_gateway" {
		t.Fatalf("%+v", p)
	}
	if !strings.Contains(e.logs.String(), "Failed to generate v3 survey create payload") || !strings.Contains(e.logs.String(), "CES questions must use a range of 5 or 7") {
		t.Fatal("the bad gateway was not logged with its cause")
	}
	if e.count(`SELECT count(*) FROM surveys WHERE workspace_id = ?`, ws) != 0 {
		t.Fatal("generate created a survey")
	}
}

type streamEvent struct {
	Type          string               `json:"type"`
	RequestID     string               `json:"requestId"`
	Seq           int                  `json:"seq"`
	Draft         json.RawMessage      `json:"draft"`
	LanguageCode  string               `json:"languageCode"`
	Index         int                  `json:"index"`
	Total         int                  `json:"total"`
	Language      string               `json:"language"`
	Payload       map[string]any       `json:"payload"`
	Validation    *GenerateValidation  `json:"validation"`
	Code          string               `json:"code"`
	Detail        string               `json:"detail"`
	InvalidParams []v3api.InvalidParam `json:"invalid_params"`
	RetryAfter    *int                 `json:"retryAfter"`
}

const streamPath = "/api/app/surveys/generate/stream"

func readStream(t *testing.T, rec *httptest.ResponseRecorder) []streamEvent {
	t.Helper()
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != GenerationStreamContentType || rec.Header().Get("Cache-Control") != "no-cache, no-store, no-transform" || rec.Header().Get("X-Accel-Buffering") != "no" {
		t.Fatalf("stream %d %v: %s", rec.Code, rec.Header(), rec.Body)
	}
	var events []streamEvent
	scanner := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		var ev streamEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			t.Fatalf("frame %q: %v", scanner.Text(), err)
		}
		events = append(events, ev)
	}
	return events
}

func eventTypes(events []streamEvent) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = ev.Type
	}
	return out
}

// forma2's streamV3SurveyGeneration: start, snapshots ending with the completed draft, one translating event per further language, then done; every guard answers before the stream opens.
func TestGenerateStream(t *testing.T) {
	e, provider := newAIEnv(t)
	owner, reader := e.user(), e.user()
	org, ws := e.organization(owner)
	e.member(org, reader, "member")
	e.grant(org, ws, reader, "read")
	body := map[string]any{"workspaceId": ws.String(), "prompt": "Understand why new users stop during onboarding", "languages": []string{"en-US", "de-DE"}}

	e.fails(e.do(owner, http.MethodPost, streamPath, body), http.StatusForbidden, ai.CodeSmartToolsDisabled, "AI smart tools are disabled for this organization.")
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	e.fails(e.do(reader, http.MethodPost, streamPath, body), http.StatusForbidden, apperr.CodeForbidden, v3api.ForbiddenMessage)
	vague := e.fails(e.do(owner, http.MethodPost, streamPath, map[string]any{"workspaceId": ws.String(), "prompt": "a survey please"}), http.StatusBadRequest, apperr.CodeBadRequest, "Prompt needs more detail")
	if params, _ := vague.Details["invalid_params"].([]any); len(params) != 1 {
		t.Fatalf("details %+v", vague.Details)
	}
	e.fails(e.do(owner, http.MethodPost, streamPath, map[string]any{"workspaceId": ws.String(), "prompt": "ab"}), http.StatusBadRequest, apperr.CodeBadRequest, "Invalid request body")
	e.fails(e.request(http.MethodPost, streamPath, body, nil), http.StatusUnauthorized, apperr.CodeNotAuthenticated, "")
	if len(provider.requests()) != 0 {
		t.Fatal("a refused request reached the provider")
	}

	events := readStream(t, e.do(owner, http.MethodPost, streamPath, body))
	types := eventTypes(events)
	if types[0] != "start" || events[0].RequestID == "" || types[len(types)-1] != "done" {
		t.Fatalf("events %v", types)
	}
	var partials []streamEvent
	for _, ev := range events {
		if ev.Type == "partial" {
			partials = append(partials, ev)
		}
	}
	for i, ev := range partials {
		if ev.Seq != i+1 {
			t.Fatalf("seq %d at %d", ev.Seq, i)
		}
	}
	var last, final map[string]any
	_ = json.Unmarshal(partials[len(partials)-1].Draft, &last)
	_ = json.Unmarshal([]byte(generatedDraftJSON), &final)
	if fmt.Sprint(last) != fmt.Sprint(final) {
		t.Fatalf("the last snapshot is not the completed draft: %s", partials[len(partials)-1].Draft)
	}
	translating := slices.Index(types, "translating")
	if translating < slices.Index(types, "partial") || events[translating].LanguageCode != "de-DE" || events[translating].Index != 1 || events[translating].Total != 1 {
		t.Fatalf("translating %+v in %v", events[translating], types)
	}
	done := events[len(events)-1]
	if done.Language != "en-US" || len(done.Validation.Languages) != 2 || at(done.Payload, "metadata.title.de-DE") != "de-DE: Onboarding feedback" {
		t.Fatalf("done %+v", done)
	}
	if !at(provider.requests()[0], "stream").(bool) {
		t.Fatal("the draft was not streamed from the provider")
	}
}

// A failure after the body opened is an in-band error event with a fixed detail, and the stream still ends cleanly.
func TestGenerateStreamErrors(t *testing.T) {
	e, provider := newAIEnv(t)
	owner := e.user()
	org, ws := e.organization(owner)
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	body := map[string]any{"workspaceId": ws.String(), "prompt": "Understand why new users stop during onboarding"}

	provider.set(func(p *aiProvider) { p.status = http.StatusTooManyRequests })
	events := readStream(t, e.do(owner, http.MethodPost, streamPath, body))
	last := events[len(events)-1]
	if last.Type != "error" || last.Code != "ai_quota_exceeded" || last.Detail != "The AI provider is temporarily rate-limited. Try again shortly." || last.RetryAfter == nil || *last.RetryAfter != 17 {
		t.Fatalf("events %+v", events)
	}

	provider.set(func(p *aiProvider) {
		p.status = http.StatusOK
		p.draft = strings.Replace(generatedDraftJSON, `"Onboarding feedback"`, `"prompt rejected: ask employees about ACME Corp salaries"`, 1)
		p.draft = strings.Replace(p.draft, `"range":"5"`, `"range":"10"`, 1)
	})
	events = readStream(t, e.do(owner, http.MethodPost, streamPath, body))
	last = events[len(events)-1]
	if last.Type != "error" || last.Code != "ai_generation_failed" || strings.Contains(last.Detail, "ACME") {
		t.Fatalf("events %+v", last)
	}
	if !strings.Contains(e.logs.String(), "AI survey generation stream failed") {
		t.Fatal("the failure was not logged")
	}

	provider.set(func(p *aiProvider) { p.draft, p.finish = generatedDraftJSON, "length" })
	events = readStream(t, e.do(owner, http.MethodPost, streamPath, body))
	if last := events[len(events)-1]; last.Code != "ai_output_too_long" {
		t.Fatalf("events %+v", last)
	}
}

// A client that hangs up stops the provider call; nothing more is written and nothing is logged as a failure.
func TestGenerateStreamClientAbort(t *testing.T) {
	e, provider := newAIEnv(t)
	owner := e.user()
	org, ws := e.organization(owner)
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	provider.set(func(p *aiProvider) {
		p.onStream = func(r *http.Request) {
			cancel()
			// The provider keeps the request open until the client's side is gone.
			select {
			case <-r.Context().Done():
			case <-time.After(10 * time.Second):
			}
		}
	})
	e.authz.Deliver(t)
	raw, _ := json.Marshal(map[string]any{"workspaceId": ws.String(), "prompt": "Understand why new users stop during onboarding"})
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, streamPath, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", webApp)
	req.Header.Set(httpapi.CSRFHeader, "XMLHttpRequest")
	req.Header.Set(testUserHeader, owner.String())
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	for _, ev := range readStream(t, rec) {
		if ev.Type == "error" || ev.Type == "done" {
			t.Fatalf("an aborted stream wrote %s", ev.Type)
		}
	}
	if !strings.Contains(e.logs.String(), "AI survey generation aborted by the client") || strings.Contains(e.logs.String(), "AI survey generation stream failed") {
		t.Fatalf("logs %s", e.logs.String())
	}
}

func TestShouldEmitSnapshot(t *testing.T) {
	start := time.Unix(1000, 0)
	cases := []struct {
		name string
		now  time.Time
		last *time.Time
		s, l string
		want bool
	}{
		{"the first snapshot", start, nil, `{"name":"On"}`, "", true},
		{"inside the window", start.Add(10 * time.Millisecond), &start, `{"name":"Onb"}`, `{"name":"On"}`, false},
		{"after the window", start.Add(snapshotThrottle), &start, `{"name":"Onb"}`, `{"name":"On"}`, true},
		{"identical however late", start.Add(time.Hour), &start, `{"name":"On"}`, `{"name":"On"}`, false},
	}
	for _, tc := range cases {
		if got := shouldEmitSnapshot(tc.now, tc.last, tc.s, tc.l); got != tc.want {
			t.Errorf("%s: %v", tc.name, got)
		}
	}
}

func TestToStreamErrorEvent(t *testing.T) {
	params := []v3api.InvalidParam{{Name: "generatedSurvey.name", Reason: "Required"}}
	cases := []struct {
		err  error
		want streamErrorEvent
	}{
		{&apperr.Error{Status: http.StatusTooManyRequests, Code: ai.CodeQuotaExceeded, RetryAfter: 42}, streamErrorEvent{Type: "error", Code: "ai_quota_exceeded", Detail: "The AI provider is temporarily rate-limited. Try again shortly.", RetryAfter: new(42)}},
		{&ai.OutputTokenLimitError{MaxOutputTokens: 8192}, streamErrorEvent{Type: "error", Code: "ai_output_too_long", Detail: "The generated survey exceeded the AI output token limit. Simplify the prompt or split it into smaller surveys."}},
		{&generatedPayloadError{params: params}, streamErrorEvent{Type: "error", Code: "ai_generated_payload_invalid", Detail: "The generated survey draft could not be validated.", InvalidParams: params}},
		{fmt.Errorf("prompt rejected: 'ask employees about their salary at ACME Corp'"), streamErrorEvent{Type: "error", Code: "ai_generation_failed", Detail: "The AI provider could not finish the survey draft. Try again or add more detail."}},
	}
	for _, tc := range cases {
		got := toStreamErrorEvent(tc.err)
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(tc.want)
		if string(a) != string(b) {
			t.Errorf("%v: %s", tc.err, a)
		}
	}
}

// The embedded draft schema is forma2's zod export: its language enum is the dashboard locales, and every object is closed with all keys required.
func TestGenerateDraftSchema(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(generateDraftSchema, &schema); err != nil {
		t.Fatal(err)
	}
	var enum []string
	for _, v := range at(schema, "properties.language.enum").([]any) {
		enum = append(enum, v.(string))
	}
	if !slices.Equal(enum, userLocales) {
		t.Fatalf("language enum %v", enum)
	}
	var walk func(node any)
	walk = func(node any) {
		switch n := node.(type) {
		case map[string]any:
			if n["type"] == "object" {
				props, _ := n["properties"].(map[string]any)
				required, _ := n["required"].([]any)
				if n["additionalProperties"] != false || len(required) != len(props) {
					t.Errorf("object %v is not closed and fully required", n)
				}
			}
			for _, v := range n {
				walk(v)
			}
		case []any:
			for _, v := range n {
				walk(v)
			}
		}
	}
	walk(schema)
	var types []string
	for _, v := range at(schema, "properties.blocks.items.properties.questions.items.properties.type.enum").([]any) {
		types = append(types, v.(string))
	}
	if !slices.Equal(types, generatedElementTypes) {
		t.Fatalf("types %v", types)
	}
}

// forma2's schemas.test.ts for the draft: trimming, bounds, strict objects and the refinements.
func TestParseGeneratedDraft(t *testing.T) {
	d, params := parseGeneratedDraft([]byte(generatedDraftJSON), true)
	if len(params) != 0 || d.Blocks[0].Questions[0].Headline != "What stopped you?" || d.Blocks[1].Questions[0].Range != 5 {
		t.Fatalf("%+v %v", d, params)
	}
	element := func(over string) string {
		return strings.Replace(generatedDraftJSON, `{"type":"openText","headline":" What stopped you? ","subheader":null,"required":true,"placeholder":"Tell us","longAnswer":true,"choices":null,"rows":null,"columns":null,"lowerLabel":null,"upperLabel":null,"scale":null,"format":null,"range":null}`, over, 1)
	}
	base := `"subheader":null,"required":true,"placeholder":null,"longAnswer":null,"lowerLabel":null,"upperLabel":null,"scale":null,"format":null`
	cases := []struct {
		name, draft, param, reason string
		forAI                      bool
	}{
		{"a choice question without choices", element(`{"type":"ranking","headline":"Rank",` + base + `,"choices":null,"rows":null,"columns":null,"range":null}`), "blocks.0.questions.0.choices", "Choice questions must include choices", true},
		{"a matrix without rows", element(`{"type":"matrix","headline":"Rate",` + base + `,"choices":null,"rows":null,"columns":["a","b"],"range":null}`), "blocks.0.questions.0.rows", "Matrix questions must include rows", true},
		{"a csat range of 7", element(`{"type":"csat","headline":"Rate",` + base + `,"choices":null,"rows":null,"columns":null,"range":"7"}`), "blocks.0.questions.0.range", "CSAT questions must use a range of 5", true},
		{"a numeric range for the model", element(`{"type":"rating","headline":"Rate",` + base + `,"choices":null,"rows":null,"columns":null,"range":7}`), "blocks.0.questions.0.range", `Invalid option: expected one of "5"|"7"|"10"`, true},
		{"rows the model must send", element(`{"type":"date","headline":"When",` + base + `,"choices":null,"columns":null,"range":null}`), "blocks.0.questions.0.rows", "Invalid input: expected array, received undefined", true},
		{"a blank headline", element(`{"type":"date","headline":"   ",` + base + `,"choices":null,"rows":null,"columns":null,"range":null}`), "blocks.0.questions.0.headline", "Too small: expected string to have >=1 characters", true},
		{"one choice", element(`{"type":"multipleChoiceMulti","headline":"Pick",` + base + `,"choices":["a"],"rows":null,"columns":null,"range":null}`), "blocks.0.questions.0.choices", "Too small: expected array to have >=2 items", true},
		{"an unknown key", element(`{"type":"date","headline":"When",` + base + `,"choices":null,"rows":null,"columns":null,"range":null,"logic":[]}`), "blocks.0.questions.0", `Unrecognized key: "logic"`, true},
		{"an unsupported language", strings.Replace(generatedDraftJSON, `"language":"en-US"`, `"language":"en"`, 1), "language", `Invalid option: expected one of "de-DE"|"en-US"|"es-ES"|"fr-FR"|"hu-HU"|"ja-JP"|"nl-NL"|"pt-BR"|"pt-PT"|"ro-RO"|"ru-RU"|"sv-SE"|"tr-TR"|"zh-Hans-CN"|"zh-Hant-TW"`, false},
		{"no blocks", strings.Split(generatedDraftJSON, `"blocks":[`)[0] + `"blocks":[]}`, "blocks", "Too small: expected array to have >=1 items", false},
	}
	for _, tc := range cases {
		_, params := parseGeneratedDraft([]byte(tc.draft), tc.forAI)
		found := false
		for _, p := range params {
			found = found || (p.Name == tc.param && p.Reason == tc.reason)
		}
		if !found {
			t.Errorf("%s: %+v", tc.name, params)
		}
	}
	// Outside the ForAI schema rows, columns and format may be absent, and a range may be a number.
	d, params = parseGeneratedDraft([]byte(element(`{"type":"rating","headline":"Rate","subheader":null,"required":true,"placeholder":null,"longAnswer":null,"lowerLabel":null,"upperLabel":null,"scale":"star","choices":null,"range":10}`)), false)
	if len(params) != 0 || d.Blocks[0].Questions[0].Range != 10 {
		t.Fatalf("%+v %v", d, params)
	}
}

func TestNormalizeBlocks(t *testing.T) {
	q := func(typ, headline string) generatedElement { return generatedElement{Type: typ, Headline: headline} }
	got := normalizeBlocks([]generatedBlock{
		{Name: "Single", Questions: []generatedElement{q("rating", "Alone")}},
		{Name: "Mixed", Questions: []generatedElement{q("openText", "A"), q("openText", "B"), q("matrix", "Grid"), q("date", "C")}},
	})
	var names []string
	for _, b := range got {
		names = append(names, fmt.Sprintf("%s:%d", b.Name, len(b.Questions)))
	}
	if !slices.Equal(names, []string{"Single:1", "Mixed:2", "Grid:1", "Mixed:1"}) {
		t.Fatalf("blocks %v", names)
	}
}
