package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// The AI providers an instance can run on, forma2's AI_PROVIDERS. Only one is active per instance.
const (
	AIProviderAWS              = "aws"
	AIProviderGoogle           = "google"
	AIProviderAzure            = "azure"
	AIProviderOpenAICompatible = "openai-compatible"
)

// AIProviders is forma2's AI_PROVIDERS, in its order.
var AIProviders = []string{AIProviderAWS, AIProviderGoogle, AIProviderAzure, AIProviderOpenAICompatible}

// AIConfig is forma2's instance-level AI configuration: one provider, the model it serves, optional per-feature model overrides of that same provider, and the active provider's credentials. AI is configured only when AI_PROVIDER is set; the provider's own variables are checked at startup exactly as forma2's env validation checked them.
type AIConfig struct {
	Provider string `env:"AI_PROVIDER"`
	Model    string `env:"AI_MODEL"`
	// ModelTranslation and ModelExampleResponses name another model of the same provider for one feature; unset means AI_MODEL.
	ModelTranslation      string `env:"AI_MODEL_TRANSLATION"`
	ModelExampleResponses string `env:"AI_MODEL_EXAMPLE_RESPONSES"`

	// Google Cloud Vertex AI. Credentials come from CREDENTIALS_JSON, else the file at APPLICATION_CREDENTIALS, else Application Default Credentials.
	GoogleCloudProject                string `env:"AI_GOOGLE_CLOUD_PROJECT"`
	GoogleCloudLocation               string `env:"AI_GOOGLE_CLOUD_LOCATION"`
	GoogleCloudCredentialsJSON        Secret `env:"AI_GOOGLE_CLOUD_CREDENTIALS_JSON"`
	GoogleCloudApplicationCredentials string `env:"AI_GOOGLE_CLOUD_APPLICATION_CREDENTIALS"`

	// Amazon Bedrock, with static credentials only.
	AWSRegion          string `env:"AI_AWS_REGION"`
	AWSAccessKeyID     string `env:"AI_AWS_ACCESS_KEY_ID"`
	AWSSecretAccessKey Secret `env:"AI_AWS_SECRET_ACCESS_KEY"`
	AWSSessionToken    Secret `env:"AI_AWS_SESSION_TOKEN"`

	// Azure OpenAI / Foundry. BaseURL (ending at /openai) wins over ResourceName; Model is the deployment name.
	AzureBaseURL      string `env:"AI_AZURE_BASE_URL"`
	AzureResourceName string `env:"AI_AZURE_RESOURCE_NAME"`
	AzureAPIKey       Secret `env:"AI_AZURE_API_KEY"`
	AzureAPIVersion   string `env:"AI_AZURE_API_VERSION"`

	// An OpenAI-compatible chat completions endpoint (vLLM, ...). SupportsStructuredOutputs is forma2's flag, on for "true" or "1" in any case.
	OpenAICompatibleBaseURL                   string `env:"AI_OPENAI_COMPATIBLE_BASE_URL"`
	OpenAICompatibleAPIKey                    Secret `env:"AI_OPENAI_COMPATIBLE_API_KEY"`
	OpenAICompatibleProviderName              string `env:"AI_OPENAI_COMPATIBLE_PROVIDER_NAME"`
	OpenAICompatibleSupportsStructuredOutputs string `env:"AI_OPENAI_COMPATIBLE_SUPPORTS_STRUCTURED_OUTPUTS"`
	OpenAICompatibleHeadersJSON               Secret `env:"AI_OPENAI_COMPATIBLE_HEADERS_JSON"`
	OpenAICompatibleQueryParamsJSON           Secret `env:"AI_OPENAI_COMPATIBLE_QUERY_PARAMS_JSON"`
}

// validate is forma2's startup check of the AI variables (validateActiveAIProviderConfiguration plus the per-variable URL rules): a provider must be a known one and come with a model, and the active provider's required variables must be set and well formed.
func (a AIConfig) validate() error {
	if a.AzureBaseURL != "" {
		if u, err := url.Parse(a.AzureBaseURL); err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("AI_AZURE_BASE_URL must be a URL, got %q", a.AzureBaseURL)
		}
	}
	if a.OpenAICompatibleBaseURL != "" && !IsHTTPURL(a.OpenAICompatibleBaseURL) {
		return errors.New("AI_OPENAI_COMPATIBLE_BASE_URL must be a valid http(s) URL")
	}
	if a.Provider == "" {
		return nil
	}
	known := false
	for _, p := range AIProviders {
		known = known || p == a.Provider
	}
	if !known {
		return fmt.Errorf("AI_PROVIDER must be one of %s, got %q", strings.Join(AIProviders, ", "), a.Provider)
	}
	if a.Model == "" {
		return errors.New("AI_MODEL is required when AI_PROVIDER is set")
	}
	required := func(name, value string) error {
		if value == "" {
			return fmt.Errorf("%s is required when AI_PROVIDER=%s", name, a.Provider)
		}
		return nil
	}
	switch a.Provider {
	case AIProviderAWS:
		return errors.Join(required("AI_AWS_REGION", a.AWSRegion), required("AI_AWS_ACCESS_KEY_ID", a.AWSAccessKeyID), required("AI_AWS_SECRET_ACCESS_KEY", string(a.AWSSecretAccessKey)))
	case AIProviderGoogle:
		err := errors.Join(required("AI_GOOGLE_CLOUD_PROJECT", a.GoogleCloudProject), required("AI_GOOGLE_CLOUD_LOCATION", a.GoogleCloudLocation))
		if a.GoogleCloudCredentialsJSON != "" && !IsJSONObject(string(a.GoogleCloudCredentialsJSON)) {
			err = errors.Join(err, errors.New("AI_GOOGLE_CLOUD_CREDENTIALS_JSON must be a valid JSON object"))
		}
		return err
	case AIProviderAzure:
		err := required("AI_AZURE_API_KEY", string(a.AzureAPIKey))
		if a.AzureBaseURL == "" && a.AzureResourceName == "" {
			err = errors.Join(err, errors.New("AI_AZURE_BASE_URL or AI_AZURE_RESOURCE_NAME is required when AI_PROVIDER=azure"))
		}
		return err
	default:
		err := required("AI_OPENAI_COMPATIBLE_BASE_URL", a.OpenAICompatibleBaseURL)
		for _, v := range []struct {
			name  string
			value Secret
		}{{"AI_OPENAI_COMPATIBLE_HEADERS_JSON", a.OpenAICompatibleHeadersJSON}, {"AI_OPENAI_COMPATIBLE_QUERY_PARAMS_JSON", a.OpenAICompatibleQueryParamsJSON}} {
			if v.value == "" {
				continue
			}
			if _, perr := ParseStringRecord(string(v.value)); perr != nil {
				err = errors.Join(err, fmt.Errorf("%s must be a JSON object of string values", v.name))
			}
		}
		return err
	}
}

// IsHTTPURL is forma2's isValidHttpUrl: an absolute URL with the http or https scheme.
func IsHTTPURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// IsJSONObject reports whether value is JSON whose top level is an object.
func IsJSONObject(value string) bool {
	var v any
	if err := json.Unmarshal([]byte(value), &v); err != nil {
		return false
	}
	_, ok := v.(map[string]any)
	return ok
}

// ParseStringRecord is forma2's parseStringRecordJson: a JSON object whose values are all strings.
func ParseStringRecord(value string) (map[string]string, error) {
	var raw any
	if err := json.Unmarshal([]byte(value), &raw); err != nil {
		return nil, errors.New("value must be valid JSON")
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("value must be a JSON object of string values")
	}
	out := make(map[string]string, len(obj))
	for k, v := range obj {
		s, ok := v.(string)
		if !ok {
			return nil, errors.New("value must be a JSON object of string values")
		}
		out[k] = s
	}
	return out, nil
}
