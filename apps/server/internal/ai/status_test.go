package ai

import (
	"reflect"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// forma2's packages/ai provider.test.ts: the configuration status per provider.
func TestConfigurationStatus(t *testing.T) {
	google := config.AIConfig{Provider: "google", Model: "gemini-2.5-flash", GoogleCloudProject: "test-project", GoogleCloudLocation: "us-central1"}
	cases := []struct {
		name    string
		cfg     config.AIConfig
		want    Status
		message string
	}{
		{
			name: "no provider",
			cfg:  config.AIConfig{Model: "gemini-2.5-flash", GoogleCloudProject: "p"},
			want: Status{Model: "gemini-2.5-flash", MissingFields: []string{"AI_PROVIDER"}, InvalidFields: []string{}, ErrorCode: StatusProviderMissing}, message: "AI_PROVIDER is required",
		},
		{
			name: "an unsupported provider",
			cfg:  config.AIConfig{Provider: "openai"},
			want: Status{MissingFields: []string{}, InvalidFields: []string{"AI_PROVIDER"}, ErrorCode: StatusInvalidProvider}, message: "AI_PROVIDER must be one of: aws, google, azure, openai-compatible",
		},
		{
			name: "aws without a model",
			cfg:  config.AIConfig{Provider: "aws", AWSRegion: "us-east-1", AWSAccessKeyID: "id", AWSSecretAccessKey: "secret"},
			want: Status{Provider: "aws", MissingFields: []string{"AI_MODEL"}, InvalidFields: []string{}, ErrorCode: StatusProviderNotConfigured,
				ProviderStatus: &ProviderStatus{Provider: "aws", MissingFields: []string{"AI_MODEL"}, InvalidFields: []string{}, ErrorCode: ProviderMissingModel}},
			message: "Active AI provider is not configured correctly. Missing: AI_MODEL",
		},
		{
			name: "azure without a key",
			cfg:  config.AIConfig{Provider: "azure", Model: "gpt-4.1", AzureResourceName: "test-resource"},
			want: Status{Provider: "azure", Model: "gpt-4.1", MissingFields: []string{"AI_AZURE_API_KEY"}, InvalidFields: []string{}, ErrorCode: StatusProviderNotConfigured,
				ProviderStatus: &ProviderStatus{Provider: "azure", Model: "gpt-4.1", MissingFields: []string{"AI_AZURE_API_KEY"}, InvalidFields: []string{}, ErrorCode: ProviderMissingCredentials}},
		},
		{
			name: "azure without an endpoint",
			cfg:  config.AIConfig{Provider: "azure", Model: "gpt-4.1", AzureAPIKey: "key"},
			want: Status{Provider: "azure", Model: "gpt-4.1", MissingFields: []string{"AI_AZURE_BASE_URL or AI_AZURE_RESOURCE_NAME"}, InvalidFields: []string{}, ErrorCode: StatusProviderNotConfigured,
				ProviderStatus: &ProviderStatus{Provider: "azure", Model: "gpt-4.1", MissingFields: []string{"AI_AZURE_BASE_URL or AI_AZURE_RESOURCE_NAME"}, InvalidFields: []string{}, ErrorCode: ProviderMissingCredentials}},
		},
		{
			name: "google with ADC",
			cfg:  google,
			want: Status{Provider: "google", Model: "gemini-2.5-flash", IsConfigured: true, MissingFields: []string{}, InvalidFields: []string{},
				ProviderStatus: &ProviderStatus{Provider: "google", Model: "gemini-2.5-flash", IsConfigured: true, MissingFields: []string{}, InvalidFields: []string{}}},
		},
		{
			name: "google with credentials that are not JSON",
			cfg:  withCredentials(google, "{not-json}"),
			want: Status{Provider: "google", Model: "gemini-2.5-flash", MissingFields: []string{}, InvalidFields: []string{"AI_GOOGLE_CLOUD_CREDENTIALS_JSON"}, ErrorCode: StatusProviderNotConfigured,
				ProviderStatus: &ProviderStatus{Provider: "google", Model: "gemini-2.5-flash", MissingFields: []string{}, InvalidFields: []string{"AI_GOOGLE_CLOUD_CREDENTIALS_JSON"}, ErrorCode: ProviderInvalidCredentials}},
			message: "Active AI provider is not configured correctly. Invalid: AI_GOOGLE_CLOUD_CREDENTIALS_JSON",
		},
		{
			name: "google with credentials that are not an object",
			cfg:  withCredentials(google, "[]"),
			want: Status{Provider: "google", Model: "gemini-2.5-flash", MissingFields: []string{}, InvalidFields: []string{"AI_GOOGLE_CLOUD_CREDENTIALS_JSON"}, ErrorCode: StatusProviderNotConfigured,
				ProviderStatus: &ProviderStatus{Provider: "google", Model: "gemini-2.5-flash", MissingFields: []string{}, InvalidFields: []string{"AI_GOOGLE_CLOUD_CREDENTIALS_JSON"}, ErrorCode: ProviderInvalidCredentials}},
		},
		{
			name: "openai-compatible with a base URL and a model",
			cfg:  config.AIConfig{Provider: "openai-compatible", Model: "Qwen/Qwen2.5-7B-Instruct", OpenAICompatibleBaseURL: "http://vllm:8000/v1"},
			want: Status{Provider: "openai-compatible", Model: "Qwen/Qwen2.5-7B-Instruct", IsConfigured: true, MissingFields: []string{}, InvalidFields: []string{},
				ProviderStatus: &ProviderStatus{Provider: "openai-compatible", Model: "Qwen/Qwen2.5-7B-Instruct", IsConfigured: true, MissingFields: []string{}, InvalidFields: []string{}}},
		},
		{
			name: "openai-compatible without a base URL",
			cfg:  config.AIConfig{Provider: "openai-compatible", Model: "Qwen"},
			want: Status{Provider: "openai-compatible", Model: "Qwen", MissingFields: []string{"AI_OPENAI_COMPATIBLE_BASE_URL"}, InvalidFields: []string{}, ErrorCode: StatusProviderNotConfigured,
				ProviderStatus: &ProviderStatus{Provider: "openai-compatible", Model: "Qwen", MissingFields: []string{"AI_OPENAI_COMPATIBLE_BASE_URL"}, InvalidFields: []string{}, ErrorCode: ProviderMissingCredentials}},
		},
		{
			name: "openai-compatible with a base URL that is not one, malformed headers and query parameters, and no model",
			cfg:  config.AIConfig{Provider: "openai-compatible", OpenAICompatibleBaseURL: "not-a-url", OpenAICompatibleHeadersJSON: "{not-json}", OpenAICompatibleQueryParamsJSON: "[]"},
			want: Status{Provider: "openai-compatible", MissingFields: []string{"AI_MODEL"}, InvalidFields: []string{"AI_OPENAI_COMPATIBLE_BASE_URL", "AI_OPENAI_COMPATIBLE_HEADERS_JSON", "AI_OPENAI_COMPATIBLE_QUERY_PARAMS_JSON"}, ErrorCode: StatusProviderNotConfigured,
				ProviderStatus: &ProviderStatus{Provider: "openai-compatible", MissingFields: []string{"AI_MODEL"}, InvalidFields: []string{"AI_OPENAI_COMPATIBLE_BASE_URL", "AI_OPENAI_COMPATIBLE_HEADERS_JSON", "AI_OPENAI_COMPATIBLE_QUERY_PARAMS_JSON"}, ErrorCode: ProviderInvalidCredentials}},
			message: "Active AI provider is not configured correctly. Missing: AI_MODEL Invalid: AI_OPENAI_COMPATIBLE_BASE_URL, AI_OPENAI_COMPATIBLE_HEADERS_JSON, AI_OPENAI_COMPATIBLE_QUERY_PARAMS_JSON",
		},
		{
			name: "values that are only whitespace",
			cfg:  config.AIConfig{Provider: " aws ", Model: "  ", AWSRegion: " ", AWSAccessKeyID: "id", AWSSecretAccessKey: "secret"},
			want: Status{Provider: "aws", MissingFields: []string{"AI_AWS_REGION", "AI_MODEL"}, InvalidFields: []string{}, ErrorCode: StatusProviderNotConfigured,
				ProviderStatus: &ProviderStatus{Provider: "aws", MissingFields: []string{"AI_AWS_REGION", "AI_MODEL"}, InvalidFields: []string{}, ErrorCode: ProviderMissingModel}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ConfigurationStatus(tc.cfg)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("status\n got %+v %+v\nwant %+v %+v", got, got.ProviderStatus, tc.want, tc.want.ProviderStatus)
			}
			if InstanceConfigured(tc.cfg) != tc.want.IsConfigured {
				t.Fatal("InstanceConfigured disagrees with the status")
			}
			if tc.message != "" && configurationMessage(got) != tc.message {
				t.Fatalf("message %q", configurationMessage(got))
			}
		})
	}
}

func withCredentials(cfg config.AIConfig, credentials string) config.AIConfig {
	cfg.GoogleCloudCredentialsJSON = config.Secret(credentials)
	return cfg
}

func TestModelPerFeature(t *testing.T) {
	cfg := config.Config{AI: config.AIConfig{Provider: "aws", Model: "sonnet", ModelTranslation: " haiku ", AWSRegion: "us-east-1", AWSAccessKeyID: "id", AWSSecretAccessKey: "secret"}}
	c := NewClient(cfg, nil)
	for feature, want := range map[Feature]string{FeatureTranslation: "haiku", FeatureExampleResponses: "sonnet", FeatureSurveyGeneration: "sonnet", "": "sonnet"} {
		if got := c.Model(feature); got != want {
			t.Errorf("Model(%q) = %q, want %q", feature, got, want)
		}
	}
	cfg.AI.ModelExampleResponses = "nova"
	if got := NewClient(cfg, nil).Model(FeatureExampleResponses); got != "nova" {
		t.Errorf("example responses run on %q", got)
	}
}
