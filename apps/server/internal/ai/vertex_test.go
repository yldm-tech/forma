package ai

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/genai"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// vertexServer is Google's OAuth token endpoint and Vertex AI's generateContent, so the service account credentials are exchanged for a token as they would be against Google.
type vertexServer struct {
	*httptest.Server
	rec     recorder
	respond func(w http.ResponseWriter, r *http.Request)
}

func newVertexServer(t *testing.T) *vertexServer {
	t.Helper()
	s := &vertexServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			if err := r.ParseForm(); err != nil || r.PostForm.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" || r.PostForm.Get("assertion") == "" {
				t.Errorf("token request %v", r.PostForm)
			}
			writeJSON(w, http.StatusOK, map[string]any{"access_token": "ya29.forma", "token_type": "Bearer", "expires_in": 3600})
			return
		}
		s.rec.record(t, r)
		s.respond(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

// serviceAccount is a service account key whose token_uri is the test server.
func (s *vertexServer) serviceAccount(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{
		"type": "service_account", "project_id": "forma-test", "private_key_id": "key-1",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"client_email": "forma@forma-test.iam.gserviceaccount.com", "client_id": "1", "token_uri": s.URL + "/token",
	})
	return string(b)
}

func vertexResponse(text, finish string) map[string]any {
	return map[string]any{
		"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{
			map[string]any{"text": "thinking about it", "thought": true},
			map[string]any{"text": text},
		}}, "finishReason": finish}},
		"usageMetadata": map[string]any{"promptTokenCount": 15, "candidatesTokenCount": 8, "thoughtsTokenCount": 5, "totalTokenCount": 28},
	}
}

// The AI SDK's createVertex: generateContent on the regional v1beta1 API with a token from the service account in AI_GOOGLE_CLOUD_CREDENTIALS_JSON, and for an object application/json with the schema in Google's OpenAPI dialect.
func TestVertexGenerateObject(t *testing.T) {
	s := newVertexServer(t)
	s.respond = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, vertexResponse(`{"name": "Onboarding", "range": null}`, "STOP"))
	}
	c := testClient(config.AIConfig{Provider: "google", Model: "gemini-2.5-flash", GoogleCloudProject: "forma-test", GoogleCloudLocation: "europe-west4", GoogleCloudCredentialsJSON: config.Secret(s.serviceAccount(t))}, endpoints{vertex: s.URL + "/"})
	out, err := c.GenerateObject(t.Context(), FeatureSurveyGeneration, testObjectRequest())
	if err != nil {
		t.Fatal(err)
	}
	if string(out.Object) != `{"name": "Onboarding", "range": null}` || *out.Usage.InputTokens != 15 || *out.Usage.OutputTokens != 13 || *out.Usage.ReasoningTokens != 5 {
		t.Fatalf("result %s %+v", out.Object, out.Usage)
	}
	r := s.rec.last(t)
	if r.path != "/v1beta1/projects/forma-test/locations/europe-west4/publishers/google/models/gemini-2.5-flash:generateContent" {
		t.Fatalf("path %s", r.path)
	}
	if r.header.Get("Authorization") != "Bearer ya29.forma" {
		t.Fatalf("authorization %q", r.header.Get("Authorization"))
	}
	expectBody(t, r.body, map[string]any{
		"systemInstruction.parts.0.text":      "You write drafts.",
		"contents.0.role":                     "user",
		"contents.0.parts.0.text":             "Write one.",
		"generationConfig.maxOutputTokens":    float64(100),
		"generationConfig.responseMimeType":   "application/json",
		"generationConfig.responseSchema":     map[string]any{"type": "OBJECT", "required": []any{"name", "range"}, "properties": map[string]any{"name": map[string]any{"type": "STRING", "minLength": float64(1)}, "range": map[string]any{"type": "STRING", "enum": []any{"5", "7"}, "nullable": true}}},
		"generationConfig.responseJsonSchema": nil,
	})
}

// forma2's multi-region rule: us and eu run on the v1 API of the multi-region host. Credentials can also come from the file at AI_GOOGLE_CLOUD_APPLICATION_CREDENTIALS.
func TestVertexMultiRegionWithCredentialsFile(t *testing.T) {
	s := newVertexServer(t)
	s.respond = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, vertexResponse("Hello", "STOP"))
	}
	path := filepath.Join(t.TempDir(), "service-account.json")
	if err := os.WriteFile(path, []byte(s.serviceAccount(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	c := testClient(config.AIConfig{Provider: "google", Model: "gemini-2.5-pro", GoogleCloudProject: "forma-test", GoogleCloudLocation: "eu", GoogleCloudApplicationCredentials: path}, endpoints{vertex: s.URL + "/"})
	out, err := c.GenerateText(t.Context(), "", Request{Prompt: "Say hello"})
	if err != nil || out.Text != "Hello" {
		t.Fatalf("%+v %v", out, err)
	}
	r := s.rec.last(t)
	if r.path != "/v1/projects/forma-test/locations/eu/publishers/google/models/gemini-2.5-pro:generateContent" || r.header.Get("Authorization") != "Bearer ya29.forma" {
		t.Fatalf("request %s %v", r.path, r.header)
	}
	expectBody(t, r.body, map[string]any{"systemInstruction": nil, "generationConfig.responseMimeType": nil})
}

func TestVertexStreamObjectAndLimits(t *testing.T) {
	s := newVertexServer(t)
	finish := "STOP"
	s.respond = func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("alt") != "sse" {
			t.Errorf("stream query %q", r.URL.RawQuery)
		}
		chunk := func(text, finish string) string {
			c := map[string]any{"content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": text}}}}
			if finish != "" {
				c["finishReason"] = finish
			}
			b, _ := json.Marshal(map[string]any{"candidates": []any{c}, "usageMetadata": map[string]any{"promptTokenCount": 15, "candidatesTokenCount": 8}})
			return "data: " + string(b)
		}
		writeSSE(w, []string{chunk(`{"name": "A`, ""), chunk(`B", "range": "7"}`, finish)})
	}
	c := testClient(config.AIConfig{Provider: "google", Model: "gemini-2.5-flash", GoogleCloudProject: "forma-test", GoogleCloudLocation: "global", GoogleCloudCredentialsJSON: config.Secret(s.serviceAccount(t))}, endpoints{vertex: s.URL + "/"})
	var partials []any
	out, err := c.StreamObject(t.Context(), FeatureSurveyGeneration, testObjectRequest(), func(p any) { partials = append(partials, p) })
	if err != nil {
		t.Fatal(err)
	}
	if string(out.Object) != `{"name": "AB", "range": "7"}` {
		t.Fatalf("object %s", out.Object)
	}
	if want := []any{map[string]any{"name": "A"}, map[string]any{"name": "AB", "range": "7"}}; !reflect.DeepEqual(partials, want) {
		t.Fatalf("partials %v", partials)
	}
	if r := s.rec.last(t); !strings.HasSuffix(r.path, "/models/gemini-2.5-flash:streamGenerateContent") {
		t.Fatalf("path %s", r.path)
	}
	finish = "MAX_TOKENS"
	_, err = c.StreamObject(t.Context(), FeatureSurveyGeneration, testObjectRequest(), func(any) {})
	var limit *OutputTokenLimitError
	if !errors.As(err, &limit) {
		t.Fatalf("%v", err)
	}
}

func TestVertexQuotaExceeded(t *testing.T) {
	s := newVertexServer(t)
	s.respond = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{"code": 429, "message": "Resource exhausted", "status": "RESOURCE_EXHAUSTED"}})
	}
	c := testClient(config.AIConfig{Provider: "google", Model: "gemini-2.5-flash", GoogleCloudProject: "forma-test", GoogleCloudLocation: "us-central1", GoogleCloudCredentialsJSON: config.Secret(s.serviceAccount(t))}, endpoints{vertex: s.URL + "/"})
	_, err := c.GenerateObject(t.Context(), FeatureSurveyGeneration, testObjectRequest())
	if info := ClassifyProviderError(err, time.Now()); info == nil || !info.QuotaExhausted || info.StatusCode != 429 {
		t.Fatalf("%v %+v", err, info)
	}
}

// openAPISchema follows the AI SDK's convertJSONSchemaToOpenAPISchema on the shapes forma2's schemas use.
func TestOpenAPISchema(t *testing.T) {
	var draft map[string]any
	if err := json.Unmarshal([]byte(`{"type":"object","properties":{
		"tags":{"anyOf":[{"minItems":2,"maxItems":8,"type":"array","items":{"type":"string","minLength":1,"maxLength":80}},{"type":"null"}]},
		"kind":{"type":["string","null"]},
		"fixed":{"const":"x"},
		"any":{"type":"object"},
		"either":{"anyOf":[{"type":"string"},{"type":"number"},{"type":"null"}]}
	},"required":["tags"],"additionalProperties":false,"description":"A draft"}`), &draft); err != nil {
		t.Fatal(err)
	}
	got := openAPISchema(draft, true)
	minLength := int64(1)
	want := &genai.Schema{Type: genai.TypeObject, Description: "A draft", Required: []string{"tags"}, Properties: map[string]*genai.Schema{
		"tags":   {Type: genai.TypeArray, Nullable: genai.Ptr(true), Items: &genai.Schema{Type: genai.TypeString, MinLength: &minLength}},
		"kind":   {AnyOf: []*genai.Schema{{Type: genai.TypeString}}, Nullable: genai.Ptr(true)},
		"fixed":  {Enum: []string{"x"}},
		"any":    {Type: genai.TypeObject},
		"either": {AnyOf: []*genai.Schema{{Type: genai.TypeString}, {Type: genai.TypeNumber}}, Nullable: genai.Ptr(true)},
	}}
	if !reflect.DeepEqual(got, want) {
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(want)
		t.Fatalf("schema\n got %s\nwant %s", a, b)
	}
	if openAPISchema(map[string]any{"type": "object"}, true) != nil {
		t.Fatal("an empty root object is sent as no schema")
	}
}
