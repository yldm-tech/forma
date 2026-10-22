package ai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// testClient is a client for cfg whose providers send to the given endpoints and never retry, so a refused call fails at once.
func testClient(cfg config.AIConfig, e endpoints) *Client {
	zero := 0
	e.retries = &zero
	c := NewClient(config.Config{AI: cfg}, slog.New(slog.DiscardHandler))
	c.endpoints = e
	return c
}

// testSchema is a closed object of the shape forma2's zod export produces: every key required, null allowed through anyOf.
var testSchema = json.RawMessage(`{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"name":{"type":"string","minLength":1},"range":{"anyOf":[{"type":"string","enum":["5","7"]},{"type":"null"}]}},"required":["name","range"],"additionalProperties":false}`)

func testObjectRequest() ObjectRequest {
	temperature := 0.2
	return ObjectRequest{
		Request:           Request{System: "You write drafts.", Prompt: "Write one.", Temperature: &temperature, MaxOutputTokens: 100, Timeout: 5 * time.Second},
		Schema:            testSchema,
		SchemaName:        "FormaThing",
		SchemaDescription: "A thing.",
	}
}

// recorded is one request a fake provider received.
type recorded struct {
	method, path, query string
	header              http.Header
	body                map[string]any
}

type recorder struct {
	mu       sync.Mutex
	requests []recorded
}

func (r *recorder) record(t *testing.T, req *http.Request) recorded {
	t.Helper()
	raw, _ := io.ReadAll(req.Body)
	var body map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("request body is not JSON: %s", raw)
		}
	}
	rec := recorded{method: req.Method, path: req.URL.EscapedPath(), query: req.URL.RawQuery, header: req.Header.Clone(), body: body}
	r.mu.Lock()
	r.requests = append(r.requests, rec)
	r.mu.Unlock()
	return rec
}

func (r *recorder) last(t *testing.T) recorded {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.requests) == 0 {
		t.Fatal("the provider received no request")
	}
	return r.requests[len(r.requests)-1]
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeSSE writes server-sent events as OpenAI's APIs and Vertex's alt=sse stream them.
func writeSSE(w http.ResponseWriter, events []string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	for _, e := range events {
		_, _ = fmt.Fprint(w, e, "\n\n")
		w.(http.Flusher).Flush()
	}
}

// jsonPath reads a value from a decoded JSON body by dot-separated keys and indexes.
func jsonPath(v any, path string) any {
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

func expectBody(t *testing.T, body map[string]any, want map[string]any) {
	t.Helper()
	for path, value := range want {
		got := jsonPath(body, path)
		if !reflect.DeepEqual(got, value) {
			encoded, _ := json.Marshal(got)
			t.Errorf("%s = %s, want %v", path, encoded, value)
		}
	}
}

func schemaValue(t *testing.T) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(testSchema, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestGenerateObjectRejectsUnusableAnswers(t *testing.T) {
	var answer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"id": "c1", "object": "chat.completion", "created": 1, "model": "m", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": answer}}}})
	}))
	defer srv.Close()
	c := testClient(config.AIConfig{Provider: "openai-compatible", Model: "m", OpenAICompatibleBaseURL: srv.URL}, endpoints{})
	for _, tc := range []struct{ answer, cause string }{
		{`not json`, "could not parse the response"},
		{`{"name": "x"}`, "response did not match schema"},
		{`{"name": "", "range": null}`, "response did not match schema"},
	} {
		answer = tc.answer
		_, err := c.GenerateObject(t.Context(), FeatureSurveyGeneration, testObjectRequest())
		var noObject *NoObjectError
		if !errors.As(err, &noObject) || !strings.Contains(err.Error(), tc.cause) {
			t.Errorf("%s: %v", tc.answer, err)
		}
	}
	answer = `{"name": "x", "range": "7"}`
	req := testObjectRequest()
	req.Check = func(object json.RawMessage) error {
		if bytes.Contains(object, []byte(`"7"`)) {
			return errors.New("range: CES questions must use a range of 5 or 7")
		}
		return nil
	}
	if _, err := c.GenerateObject(t.Context(), FeatureSurveyGeneration, req); err == nil || !strings.Contains(err.Error(), "CES questions") {
		t.Errorf("check: %v", err)
	}
}

func TestUnconfiguredClientRefusesWithAConfigurationError(t *testing.T) {
	c := testClient(config.AIConfig{Provider: "azure", Model: "gpt"}, endpoints{})
	_, err := c.GenerateText(t.Context(), "", Request{Prompt: "hi"})
	var ce *ConfigurationError
	if !errors.As(err, &ce) || ce.Code != StatusProviderNotConfigured || ce.Message != "Active AI provider is not configured correctly. Missing: AI_AZURE_API_KEY, AI_AZURE_BASE_URL or AI_AZURE_RESOURCE_NAME" {
		t.Fatalf("%v", err)
	}
	if c.Configured() {
		t.Fatal("configured")
	}
}

func TestClassifyProviderError(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	status, retryAfter := http.StatusTooManyRequests, ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		writeJSON(w, status, map[string]any{"error": map[string]any{"message": "slow down", "type": "rate_limit"}})
	}))
	defer srv.Close()
	c := testClient(config.AIConfig{Provider: "openai-compatible", Model: "m", OpenAICompatibleBaseURL: srv.URL}, endpoints{})
	call := func() error {
		_, err := c.GenerateText(t.Context(), "", Request{Prompt: "hi"})
		return err
	}
	for _, tc := range []struct {
		status     int
		retryAfter string
		want       ProviderErrorInfo
	}{
		{429, "30", ProviderErrorInfo{QuotaExhausted: true, Retryable: true, StatusCode: 429, RetryAfter: intPtr(30)}},
		{429, now.Add(90 * time.Second).Format(http.TimeFormat), ProviderErrorInfo{QuotaExhausted: true, Retryable: true, StatusCode: 429, RetryAfter: intPtr(90)}},
		{429, now.Add(-time.Minute).Format(http.TimeFormat), ProviderErrorInfo{QuotaExhausted: true, Retryable: true, StatusCode: 429, RetryAfter: intPtr(0)}},
		{429, "soon", ProviderErrorInfo{QuotaExhausted: true, Retryable: true, StatusCode: 429}},
		{503, "", ProviderErrorInfo{Retryable: true, StatusCode: 503}},
		{400, "", ProviderErrorInfo{StatusCode: 400}},
	} {
		status, retryAfter = tc.status, tc.retryAfter
		got := ClassifyProviderError(call(), now)
		if got == nil || !reflect.DeepEqual(*got, tc.want) {
			t.Errorf("%d %q: %+v", tc.status, tc.retryAfter, got)
		}
	}
	if ClassifyProviderError(errors.New("socket hang up"), now) != nil {
		t.Error("a non-provider error was classified")
	}
}

func intPtr(n int) *int { return &n }
