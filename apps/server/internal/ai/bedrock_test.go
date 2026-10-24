package ai

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/smithy-go/eventstream"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

const bedrockModel = "anthropic.claude-3-5-haiku-20241022-v1:0"

func bedrockConfig() config.AIConfig {
	return config.AIConfig{Provider: "aws", Model: bedrockModel, AWSRegion: "eu-central-1", AWSAccessKeyID: "AKIDFORMA", AWSSecretAccessKey: "secret", AWSSessionToken: "session-token"}
}

func converseResponse(content []any, stopReason string) map[string]any {
	return map[string]any{
		"output":     map[string]any{"message": map[string]any{"role": "assistant", "content": content}},
		"stopReason": stopReason,
		"usage":      map[string]any{"inputTokens": 30, "outputTokens": 11, "totalTokens": 41},
		"metrics":    map[string]any{"latencyMs": 5},
	}
}

// The AI SDK's Bedrock request: Converse signed with the static AI_AWS_* credentials, the system prompt and one user message, and for an object the json tool the model is forced to call; its input is the object.
func TestBedrockGenerateObject(t *testing.T) {
	var rec recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(t, r)
		writeJSON(w, http.StatusOK, converseResponse([]any{
			map[string]any{"toolUse": map[string]any{"toolUseId": "tool_1", "name": "json", "input": map[string]any{"name": "Onboarding", "range": "5"}}},
		}, "tool_use"))
	}))
	defer srv.Close()
	c := testClient(bedrockConfig(), endpoints{bedrock: srv.URL})
	out, err := c.GenerateObject(t.Context(), FeatureSurveyGeneration, testObjectRequest())
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out.Object, &got)
	if !reflect.DeepEqual(got, map[string]any{"name": "Onboarding", "range": "5"}) || out.FinishReason != FinishStop || *out.Usage.InputTokens != 30 || *out.Usage.OutputTokens != 11 || out.Usage.ReasoningTokens != nil {
		t.Fatalf("result %s %+v", out.Object, out)
	}
	r := rec.last(t)
	if r.method != http.MethodPost || r.path != "/model/anthropic.claude-3-5-haiku-20241022-v1%3A0/converse" {
		t.Fatalf("request %s %s", r.method, r.path)
	}
	if auth := r.header.Get("Authorization"); !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=AKIDFORMA/") || !strings.Contains(auth, "/eu-central-1/bedrock/aws4_request") {
		t.Fatalf("authorization %q", auth)
	}
	if r.header.Get("X-Amz-Security-Token") != "session-token" {
		t.Fatalf("session token %q", r.header.Get("X-Amz-Security-Token"))
	}
	expectBody(t, r.body, map[string]any{
		"system.0.text":                           "You write drafts.",
		"messages.0.role":                         "user",
		"messages.0.content.0.text":               "Write one.",
		"inferenceConfig.maxTokens":               float64(100),
		"toolConfig.tools.0.toolSpec.name":        "json",
		"toolConfig.tools.0.toolSpec.description": "Respond with a JSON object.",
		"toolConfig.tools.0.toolSpec.inputSchema": map[string]any{"json": schemaValue(t)},
		"toolConfig.toolChoice":                   map[string]any{"any": map[string]any{}},
	})
	if temp, _ := jsonPath(r.body, "inferenceConfig.temperature").(float64); temp < 0.199 || temp > 0.201 {
		t.Fatalf("temperature %v", temp)
	}
}

func TestBedrockGenerateTextAndLimits(t *testing.T) {
	var rec recorder
	stop := "end_turn"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(t, r)
		writeJSON(w, http.StatusOK, converseResponse([]any{map[string]any{"text": "Hello "}, map[string]any{"text": "there"}}, stop))
	}))
	defer srv.Close()
	c := testClient(bedrockConfig(), endpoints{bedrock: srv.URL})
	temperature := 1.5
	out, err := c.GenerateText(t.Context(), "", Request{Prompt: "Say hello", Temperature: &temperature})
	if err != nil || out.Text != "Hello there" || out.FinishReason != FinishStop {
		t.Fatalf("%+v %v", out, err)
	}
	// Bedrock accepts temperatures up to 1; the AI SDK clamps. A text request carries no tools.
	expectBody(t, rec.last(t).body, map[string]any{"inferenceConfig.temperature": float64(1), "toolConfig": nil, "system": nil})

	stop = "max_tokens"
	_, err = c.GenerateObject(t.Context(), FeatureSurveyGeneration, testObjectRequest())
	var limit *OutputTokenLimitError
	if !errors.As(err, &limit) || *limit.OutputTokens != 11 {
		t.Fatalf("%v", err)
	}
	stop = "guardrail_intervened"
	_, err = c.GenerateObject(t.Context(), FeatureSurveyGeneration, testObjectRequest())
	var noObject *NoObjectError
	if !errors.As(err, &noObject) || noObject.FinishReason != FinishContentFilter {
		t.Fatalf("%v", err)
	}
}

// writeEvents writes an AWS event stream, the binary framing ConverseStream answers with.
func writeEvents(t *testing.T, w http.ResponseWriter, events [][2]string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
	w.WriteHeader(http.StatusOK)
	enc := eventstream.NewEncoder()
	for _, e := range events {
		msg := eventstream.Message{Payload: []byte(e[1])}
		msg.Headers.Set(":message-type", eventstream.StringValue("event"))
		msg.Headers.Set(":event-type", eventstream.StringValue(e[0]))
		msg.Headers.Set(":content-type", eventstream.StringValue("application/json"))
		if err := enc.Encode(w, msg); err != nil {
			t.Error(err)
		}
		w.(http.Flusher).Flush()
	}
}

func TestBedrockStreamObject(t *testing.T) {
	var rec recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(t, r)
		writeEvents(t, w, [][2]string{
			{"messageStart", `{"role":"assistant"}`},
			{"contentBlockStart", `{"contentBlockIndex":0,"start":{"toolUse":{"toolUseId":"tool_1","name":"json"}}}`},
			{"contentBlockDelta", `{"contentBlockIndex":0,"delta":{"toolUse":{"input":"{\"name\": \"On"}}}`},
			{"contentBlockDelta", `{"contentBlockIndex":0,"delta":{"toolUse":{"input":"boarding\", \"range\": null}"}}}`},
			{"contentBlockStop", `{"contentBlockIndex":0}`},
			{"messageStop", `{"stopReason":"tool_use"}`},
			{"metadata", `{"usage":{"inputTokens":30,"outputTokens":12,"totalTokens":42},"metrics":{"latencyMs":5}}`},
		})
	}))
	defer srv.Close()
	c := testClient(bedrockConfig(), endpoints{bedrock: srv.URL})
	var partials []any
	out, err := c.StreamObject(t.Context(), FeatureSurveyGeneration, testObjectRequest(), func(p any) { partials = append(partials, p) })
	if err != nil {
		t.Fatal(err)
	}
	if string(out.Object) != `{"name": "Onboarding", "range": null}` || *out.Usage.OutputTokens != 12 {
		t.Fatalf("result %s %+v", out.Object, out.Usage)
	}
	if want := []any{map[string]any{"name": "On"}, map[string]any{"name": "Onboarding", "range": nil}}; !reflect.DeepEqual(partials, want) {
		t.Fatalf("partials %v", partials)
	}
	if r := rec.last(t); r.path != "/model/anthropic.claude-3-5-haiku-20241022-v1%3A0/converse-stream" {
		t.Fatalf("path %s", r.path)
	}
}

func TestBedrockThrottling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Amzn-Errortype", "ThrottlingException:http://internal.amazon.com/coral/com.amazon.bedrock/")
		w.Header().Set("Retry-After", "12")
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"message": "Too many requests, please wait before trying again."})
	}))
	defer srv.Close()
	c := testClient(bedrockConfig(), endpoints{bedrock: srv.URL})
	_, err := c.GenerateObject(t.Context(), FeatureSurveyGeneration, testObjectRequest())
	info := ClassifyProviderError(err, time.Now())
	if info == nil || !info.QuotaExhausted || info.RetryAfter == nil || *info.RetryAfter != 12 {
		t.Fatalf("%v %+v", err, info)
	}
}
