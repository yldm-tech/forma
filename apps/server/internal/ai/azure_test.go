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

func azureResponse(text, status, incomplete string) map[string]any {
	r := map[string]any{
		"id": "resp_1", "object": "response", "created_at": 1, "model": "gpt-4.1", "status": status,
		"output": []any{map[string]any{"type": "message", "id": "msg_1", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}}},
		"usage":  map[string]any{"input_tokens": 20, "output_tokens": 9, "total_tokens": 29, "input_tokens_details": map[string]any{"cached_tokens": 0}, "output_tokens_details": map[string]any{"reasoning_tokens": 4}},
	}
	if incomplete != "" {
		r["incomplete_details"] = map[string]any{"reason": incomplete}
	}
	return r
}

func azureEvent(v map[string]any) string {
	b, _ := json.Marshal(v)
	return "event: " + v["type"].(string) + "\ndata: " + string(b)
}

// The AI SDK's createAzure: the Responses API under <base>/v1 with api-version, the api-key header, AI_MODEL as the deployment, and a strict json_schema text format for an object.
func TestAzureGenerateObject(t *testing.T) {
	var rec recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(t, r)
		writeJSON(w, http.StatusOK, azureResponse(`{"name": "Onboarding", "range": "7"}`, "completed", ""))
	}))
	defer srv.Close()
	c := testClient(config.AIConfig{Provider: "azure", Model: "my-deployment", AzureBaseURL: srv.URL + "/openai", AzureAPIKey: "azure-key"}, endpoints{})
	out, err := c.GenerateObject(t.Context(), FeatureSurveyGeneration, testObjectRequest())
	if err != nil {
		t.Fatal(err)
	}
	if string(out.Object) != `{"name": "Onboarding", "range": "7"}` || *out.Usage.InputTokens != 20 || *out.Usage.OutputTokens != 9 || *out.Usage.ReasoningTokens != 4 {
		t.Fatalf("result %+v", out)
	}
	r := rec.last(t)
	if r.path != "/openai/v1/responses" || r.query != "api-version=v1" || r.header.Get("api-key") != "azure-key" || r.header.Get("Authorization") != "" {
		t.Fatalf("request %s?%s %v", r.path, r.query, r.header)
	}
	expectBody(t, r.body, map[string]any{
		"model":                      "my-deployment",
		"input.0.role":               "system",
		"input.0.content":            "You write drafts.",
		"input.1.role":               "user",
		"input.1.content.0.type":     "input_text",
		"input.1.content.0.text":     "Write one.",
		"max_output_tokens":          float64(100),
		"temperature":                0.2,
		"text.format.type":           "json_schema",
		"text.format.name":           "FormaThing",
		"text.format.description":    "A thing.",
		"text.format.strict":         true,
		"text.format.schema":         schemaValue(t),
		"stream":                     nil,
		"store":                      nil,
		"previous_response_id":       nil,
		"text.format.schema.$schema": "http://json-schema.org/draft-07/schema#",
	})
}

func TestAzureAPIVersionAndIncompleteResponse(t *testing.T) {
	var rec recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(t, r)
		writeJSON(w, http.StatusOK, azureResponse(`{"name": "On`, "incomplete", "max_output_tokens"))
	}))
	defer srv.Close()
	c := testClient(config.AIConfig{Provider: "azure", Model: "d", AzureBaseURL: srv.URL + "/openai/", AzureAPIKey: "k", AzureAPIVersion: "preview"}, endpoints{})
	_, err := c.GenerateObject(t.Context(), FeatureSurveyGeneration, testObjectRequest())
	var limit *OutputTokenLimitError
	if !errors.As(err, &limit) {
		t.Fatalf("%v", err)
	}
	if r := rec.last(t); r.path != "/openai/v1/responses" || r.query != "api-version=preview" {
		t.Fatalf("request %s?%s", r.path, r.query)
	}
}

func TestAzureStreamObject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w, []string{
			azureEvent(map[string]any{"type": "response.created", "sequence_number": 0, "response": azureResponse("", "in_progress", "")}),
			azureEvent(map[string]any{"type": "response.output_text.delta", "sequence_number": 1, "item_id": "msg_1", "output_index": 0, "content_index": 0, "delta": `{"name": "A`}),
			azureEvent(map[string]any{"type": "response.output_text.delta", "sequence_number": 2, "item_id": "msg_1", "output_index": 0, "content_index": 0, "delta": `B", "range": null}`}),
			azureEvent(map[string]any{"type": "response.completed", "sequence_number": 3, "response": azureResponse(`{"name": "AB", "range": null}`, "completed", "")}),
		})
	}))
	defer srv.Close()
	c := testClient(config.AIConfig{Provider: "azure", Model: "d", AzureBaseURL: srv.URL + "/openai", AzureAPIKey: "k"}, endpoints{})
	var partials []any
	out, err := c.StreamObject(t.Context(), FeatureSurveyGeneration, testObjectRequest(), func(p any) { partials = append(partials, p) })
	if err != nil {
		t.Fatal(err)
	}
	if string(out.Object) != `{"name": "AB", "range": null}` || *out.Usage.OutputTokens != 9 {
		t.Fatalf("result %s %+v", out.Object, out.Usage)
	}
	if want := []any{map[string]any{"name": "A"}, map[string]any{"name": "AB", "range": nil}}; !reflect.DeepEqual(partials, want) {
		t.Fatalf("partials %v", partials)
	}
}

func TestAzureFailedStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		failed := azureResponse("", "failed", "")
		failed["error"] = map[string]any{"code": "server_error", "message": "The model crashed"}
		writeSSE(w, []string{azureEvent(map[string]any{"type": "response.failed", "sequence_number": 1, "response": failed})})
	}))
	defer srv.Close()
	c := testClient(config.AIConfig{Provider: "azure", Model: "d", AzureBaseURL: srv.URL + "/openai", AzureAPIKey: "k"}, endpoints{})
	if _, err := c.StreamObject(t.Context(), FeatureSurveyGeneration, testObjectRequest(), func(any) {}); err == nil || err.Error() != "azure: The model crashed" {
		t.Fatalf("%v", err)
	}
}
