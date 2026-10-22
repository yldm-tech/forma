package ai

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

func chatCompletion(content, finish string) map[string]any {
	return map[string]any{
		"id": "chatcmpl-1", "object": "chat.completion", "created": 1, "model": "Qwen",
		"choices": []any{map[string]any{"index": 0, "finish_reason": finish, "message": map[string]any{"role": "assistant", "content": content}}},
		"usage":   map[string]any{"prompt_tokens": 12, "completion_tokens": 7, "total_tokens": 19, "completion_tokens_details": map[string]any{"reasoning_tokens": 3}},
	}
}

func chatChunk(delta, finish string) string {
	choice := map[string]any{"index": 0, "delta": map[string]any{"content": delta}}
	if finish != "" {
		choice["finish_reason"] = finish
	}
	b, _ := json.Marshal(map[string]any{"id": "chatcmpl-1", "object": "chat.completion.chunk", "created": 1, "model": "Qwen", "choices": []any{choice}})
	return "data: " + string(b)
}

// The AI SDK's openai-compatible chat request: the operator's base URL, headers and query parameters, a bearer key when one is set, and a strict json_schema response format when the endpoint supports structured outputs.
func TestOpenAICompatibleGenerateObject(t *testing.T) {
	var rec recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(t, r)
		writeJSON(w, http.StatusOK, chatCompletion(`{"name": "Onboarding", "range": null}`, "stop"))
	}))
	defer srv.Close()
	c := testClient(config.AIConfig{
		Provider: "openai-compatible", Model: "Qwen/Qwen2.5-7B-Instruct", OpenAICompatibleBaseURL: srv.URL + "/v1",
		OpenAICompatibleAPIKey: "sk-test", OpenAICompatibleSupportsStructuredOutputs: "TRUE",
		OpenAICompatibleHeadersJSON: `{"X-Tenant": "forma"}`, OpenAICompatibleQueryParamsJSON: `{"api-version": "2"}`,
	}, endpoints{})
	out, err := c.GenerateObject(t.Context(), FeatureSurveyGeneration, testObjectRequest())
	if err != nil {
		t.Fatal(err)
	}
	if string(out.Object) != `{"name": "Onboarding", "range": null}` || out.Model != "Qwen/Qwen2.5-7B-Instruct" || out.FinishReason != FinishStop {
		t.Fatalf("result %+v", out)
	}
	if *out.Usage.InputTokens != 12 || *out.Usage.OutputTokens != 7 || *out.Usage.ReasoningTokens != 3 {
		t.Fatalf("usage %+v", out.Usage)
	}
	r := rec.last(t)
	if r.method != http.MethodPost || r.path != "/v1/chat/completions" || r.query != "api-version=2" {
		t.Fatalf("request %s %s?%s", r.method, r.path, r.query)
	}
	if r.header.Get("Authorization") != "Bearer sk-test" || r.header.Get("X-Tenant") != "forma" {
		t.Fatalf("headers %v", r.header)
	}
	expectBody(t, r.body, map[string]any{
		"model":                                   "Qwen/Qwen2.5-7B-Instruct",
		"messages.0.role":                         "system",
		"messages.0.content":                      "You write drafts.",
		"messages.1.role":                         "user",
		"messages.1.content":                      "Write one.",
		"max_tokens":                              float64(100),
		"temperature":                             0.2,
		"response_format.type":                    "json_schema",
		"response_format.json_schema.name":        "FormaThing",
		"response_format.json_schema.description": "A thing.",
		"response_format.json_schema.strict":      true,
		"response_format.json_schema.schema":      schemaValue(t),
		"stream":                                  nil,
	})
}

// Without structured outputs the AI SDK asks for json_object; a missing API key sends no Authorization, and the OpenAI SDK's own environment variables never reach the operator's endpoint.
func TestOpenAICompatibleWithoutStructuredOutputsOrKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-leaked")
	t.Setenv("OPENAI_ORG_ID", "org-leaked")
	var rec recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(t, r)
		writeJSON(w, http.StatusOK, chatCompletion(`{"name": "A", "range": "5"}`, "stop"))
	}))
	defer srv.Close()
	c := testClient(config.AIConfig{Provider: "openai-compatible", Model: "Qwen", OpenAICompatibleBaseURL: srv.URL, OpenAICompatibleSupportsStructuredOutputs: "0"}, endpoints{})
	req := testObjectRequest()
	req.SchemaName, req.Temperature = "", nil
	if _, err := c.GenerateObject(t.Context(), FeatureSurveyGeneration, req); err != nil {
		t.Fatal(err)
	}
	r := rec.last(t)
	if r.header.Get("Authorization") != "" || r.header.Get("OpenAI-Organization") != "" {
		t.Fatalf("headers %v", r.header)
	}
	expectBody(t, r.body, map[string]any{"response_format": map[string]any{"type": "json_object"}, "temperature": nil})
}

func TestOpenAICompatibleGenerateText(t *testing.T) {
	var rec recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(t, r)
		writeJSON(w, http.StatusOK, chatCompletion("Hello there", "stop"))
	}))
	defer srv.Close()
	c := testClient(config.AIConfig{Provider: "openai-compatible", Model: "Qwen", OpenAICompatibleBaseURL: srv.URL}, endpoints{})
	out, err := c.GenerateText(t.Context(), "", Request{Prompt: "Say hello"})
	if err != nil || out.Text != "Hello there" || out.FinishReason != FinishStop {
		t.Fatalf("%+v %v", out, err)
	}
	expectBody(t, rec.last(t).body, map[string]any{"messages.0.role": "user", "messages.1": nil, "response_format": nil, "max_tokens": nil})
}

// streamObject: each new snapshot of the object goes to onPartial as the text grows, and the finished object is validated like a blocking one.
func TestOpenAICompatibleStreamObject(t *testing.T) {
	var rec recorder
	finish := "stop"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(t, r)
		writeSSE(w, []string{
			chatChunk(`{"name": "On`, ""),
			chatChunk(`boarding"`, ""),
			chatChunk(``, ""),
			chatChunk(`, "range": "5"}`, finish),
			"data: [DONE]",
		})
	}))
	defer srv.Close()
	c := testClient(config.AIConfig{Provider: "openai-compatible", Model: "Qwen", OpenAICompatibleBaseURL: srv.URL, OpenAICompatibleSupportsStructuredOutputs: "1"}, endpoints{})
	var partials []any
	out, err := c.StreamObject(t.Context(), FeatureSurveyGeneration, testObjectRequest(), func(p any) { partials = append(partials, p) })
	if err != nil {
		t.Fatal(err)
	}
	if string(out.Object) != `{"name": "Onboarding", "range": "5"}` {
		t.Fatalf("object %s", out.Object)
	}
	want := []any{map[string]any{"name": "On"}, map[string]any{"name": "Onboarding"}, map[string]any{"name": "Onboarding", "range": "5"}}
	if !reflect.DeepEqual(partials, want) {
		t.Fatalf("partials %v", partials)
	}
	expectBody(t, rec.last(t).body, map[string]any{"stream": true, "response_format.type": "json_schema"})

	finish = "length"
	_, err = c.StreamObject(t.Context(), FeatureSurveyGeneration, testObjectRequest(), func(any) {})
	var limit *OutputTokenLimitError
	if !errors.As(err, &limit) || limit.MaxOutputTokens != 100 || err.Error() != "AI generation stopped because the output token limit was reached" {
		t.Fatalf("%v", err)
	}
}

func TestOpenAICompatibleOutputTokenLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, chatCompletion(`{"name": "On`, "length"))
	}))
	defer srv.Close()
	c := testClient(config.AIConfig{Provider: "openai-compatible", Model: "Qwen", OpenAICompatibleBaseURL: srv.URL}, endpoints{})
	_, err := c.GenerateObject(t.Context(), FeatureSurveyGeneration, testObjectRequest())
	var limit *OutputTokenLimitError
	if !errors.As(err, &limit) || *limit.OutputTokens != 7 || *limit.ReasoningTokens != 3 {
		t.Fatalf("%v", err)
	}
}
