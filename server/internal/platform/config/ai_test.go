package config

import (
	"strings"
	"testing"
)

func TestLoadAI(t *testing.T) {
	setRequired(t)
	t.Setenv("AI_PROVIDER", "openai-compatible")
	t.Setenv("AI_MODEL", "Qwen/Qwen2.5-7B-Instruct")
	t.Setenv("AI_MODEL_TRANSLATION", "Qwen/Qwen2.5-1.5B-Instruct")
	t.Setenv("AI_OPENAI_COMPATIBLE_BASE_URL", "http://vllm:8000/v1")
	t.Setenv("AI_OPENAI_COMPATIBLE_API_KEY", "sk-test")
	t.Setenv("AI_OPENAI_COMPATIBLE_SUPPORTS_STRUCTURED_OUTPUTS", "true")
	t.Setenv("AI_OPENAI_COMPATIBLE_HEADERS_JSON", `{"X-Tenant": "forma"}`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	a := cfg.AI
	if a.Provider != AIProviderOpenAICompatible || a.Model != "Qwen/Qwen2.5-7B-Instruct" || a.ModelTranslation != "Qwen/Qwen2.5-1.5B-Instruct" || a.OpenAICompatibleBaseURL != "http://vllm:8000/v1" || a.OpenAICompatibleAPIKey != "sk-test" || a.OpenAICompatibleSupportsStructuredOutputs != "true" {
		t.Fatalf("AI %+v", a)
	}
	if strings.Contains(a.OpenAICompatibleAPIKey.String(), "sk-test") {
		t.Fatal("the API key prints")
	}
}

func TestLoadWithoutAIProvider(t *testing.T) {
	setRequired(t)
	// Variables of a provider that is not active are not checked beyond their own format.
	t.Setenv("AI_AWS_REGION", "us-east-1")
	t.Setenv("AI_MODEL", "orphan")
	cfg, err := Load()
	if err != nil || cfg.AI.Provider != "" {
		t.Fatalf("Load: %+v %v", cfg.AI, err)
	}
}

// forma2's validateActiveAIProviderConfiguration and the per-variable URL rules: a misconfigured provider fails the start.
func TestLoadRejectsInvalidAI(t *testing.T) {
	cases := map[string]struct {
		env  map[string]string
		want string
	}{
		"an unknown provider":              {map[string]string{"AI_PROVIDER": "openai", "AI_MODEL": "gpt"}, "AI_PROVIDER must be one of aws, google, azure, openai-compatible"},
		"a provider without a model":       {map[string]string{"AI_PROVIDER": "aws", "AI_AWS_REGION": "us-east-1", "AI_AWS_ACCESS_KEY_ID": "id", "AI_AWS_SECRET_ACCESS_KEY": "secret"}, "AI_MODEL is required when AI_PROVIDER is set"},
		"aws without credentials":          {map[string]string{"AI_PROVIDER": "aws", "AI_MODEL": "nova", "AI_AWS_REGION": "us-east-1"}, "AI_AWS_ACCESS_KEY_ID is required when AI_PROVIDER=aws"},
		"google without a location":        {map[string]string{"AI_PROVIDER": "google", "AI_MODEL": "gemini", "AI_GOOGLE_CLOUD_PROJECT": "p"}, "AI_GOOGLE_CLOUD_LOCATION is required when AI_PROVIDER=google"},
		"google credentials array":         {map[string]string{"AI_PROVIDER": "google", "AI_MODEL": "gemini", "AI_GOOGLE_CLOUD_PROJECT": "p", "AI_GOOGLE_CLOUD_LOCATION": "global", "AI_GOOGLE_CLOUD_CREDENTIALS_JSON": "[]"}, "AI_GOOGLE_CLOUD_CREDENTIALS_JSON must be a valid JSON object"},
		"azure without a key":              {map[string]string{"AI_PROVIDER": "azure", "AI_MODEL": "gpt", "AI_AZURE_RESOURCE_NAME": "r"}, "AI_AZURE_API_KEY is required when AI_PROVIDER=azure"},
		"azure without an endpoint":        {map[string]string{"AI_PROVIDER": "azure", "AI_MODEL": "gpt", "AI_AZURE_API_KEY": "k"}, "AI_AZURE_BASE_URL or AI_AZURE_RESOURCE_NAME is required when AI_PROVIDER=azure"},
		"an azure base that is no URL":     {map[string]string{"AI_AZURE_BASE_URL": "openai"}, "AI_AZURE_BASE_URL must be a URL"},
		"openai-compatible without a base": {map[string]string{"AI_PROVIDER": "openai-compatible", "AI_MODEL": "qwen"}, "AI_OPENAI_COMPATIBLE_BASE_URL is required when AI_PROVIDER=openai-compatible"},
		"a base that is not http":          {map[string]string{"AI_OPENAI_COMPATIBLE_BASE_URL": "ftp://vllm/v1"}, "AI_OPENAI_COMPATIBLE_BASE_URL must be a valid http(s) URL"},
		"headers that are not strings":     {map[string]string{"AI_PROVIDER": "openai-compatible", "AI_MODEL": "qwen", "AI_OPENAI_COMPATIBLE_BASE_URL": "http://vllm:8000/v1", "AI_OPENAI_COMPATIBLE_HEADERS_JSON": `{"X-Retries": 3}`}, "AI_OPENAI_COMPATIBLE_HEADERS_JSON must be a JSON object of string values"},
		"query parameters not JSON":        {map[string]string{"AI_PROVIDER": "openai-compatible", "AI_MODEL": "qwen", "AI_OPENAI_COMPATIBLE_BASE_URL": "http://vllm:8000/v1", "AI_OPENAI_COMPATIBLE_QUERY_PARAMS_JSON": "a=b"}, "AI_OPENAI_COMPATIBLE_QUERY_PARAMS_JSON must be a JSON object of string values"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			setRequired(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load: %v, want %q", err, tc.want)
			}
		})
	}
}
