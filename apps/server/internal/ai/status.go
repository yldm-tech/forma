package ai

import (
	"slices"
	"strings"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// Status error codes, forma2's AIConfigurationErrorCode.
const (
	StatusProviderMissing       = "providerMissing"
	StatusInvalidProvider       = "invalidProvider"
	StatusProviderNotConfigured = "providerNotConfigured"
)

// Provider status error codes, forma2's AIProviderStatusErrorCode.
const (
	ProviderMissingCredentials = "missingCredentials"
	ProviderInvalidCredentials = "invalidCredentials"
	ProviderMissingModel       = "missingModel"
)

// ProviderStatus is forma2's AIProviderStatus.
type ProviderStatus struct {
	Provider      string   `json:"provider"`
	IsConfigured  bool     `json:"isConfigured"`
	Model         string   `json:"model,omitempty"`
	MissingFields []string `json:"missingFields"`
	InvalidFields []string `json:"invalidFields"`
	ErrorCode     string   `json:"errorCode,omitempty"`
}

// Status is forma2's AIConfigurationStatus: whether the instance can run AI, and if not, which variables are missing or malformed.
type Status struct {
	Provider       string          `json:"provider,omitempty"`
	Model          string          `json:"model,omitempty"`
	IsConfigured   bool            `json:"isConfigured"`
	MissingFields  []string        `json:"missingFields"`
	InvalidFields  []string        `json:"invalidFields"`
	ErrorCode      string          `json:"errorCode,omitempty"`
	ProviderStatus *ProviderStatus `json:"providerStatus,omitempty"`
}

// normalize is forma2's normalizeValue: trimmed, with blank meaning unset.
func normalize(value string) string {
	return strings.TrimSpace(value)
}

// supportsStructuredOutputs is forma2's parseBooleanFlag.
func supportsStructuredOutputs(value string) bool {
	v := strings.ToLower(normalize(value))
	return v == "true" || v == "1"
}

// ConfigurationStatus is forma2's getAiConfigurationStatus for the configuration the process started with.
func ConfigurationStatus(cfg config.AIConfig) Status {
	provider := normalize(cfg.Provider)
	model := normalize(cfg.Model)
	if provider == "" {
		return Status{Model: model, MissingFields: []string{"AI_PROVIDER"}, InvalidFields: []string{}, ErrorCode: StatusProviderMissing}
	}
	if !slices.Contains(config.AIProviders, provider) {
		return Status{Model: model, MissingFields: []string{}, InvalidFields: []string{"AI_PROVIDER"}, ErrorCode: StatusInvalidProvider}
	}
	ps := providerStatus(provider, cfg)
	if !ps.IsConfigured {
		return Status{Provider: provider, Model: ps.Model, MissingFields: ps.MissingFields, InvalidFields: ps.InvalidFields, ErrorCode: StatusProviderNotConfigured, ProviderStatus: &ps}
	}
	return Status{Provider: provider, Model: ps.Model, IsConfigured: true, MissingFields: []string{}, InvalidFields: []string{}, ProviderStatus: &ps}
}

// InstanceConfigured is forma2's isInstanceAIConfigured.
func InstanceConfigured(cfg config.AIConfig) bool {
	return ConfigurationStatus(cfg).IsConfigured
}

// providerStatus is forma2's getProviderStatus: the adapter's own checks, then the model.
func providerStatus(provider string, cfg config.AIConfig) ProviderStatus {
	missing, invalid := []string{}, []string{}
	need := func(name, value string) {
		if normalize(value) == "" {
			missing = append(missing, name)
		}
	}
	switch provider {
	case config.AIProviderAWS:
		need("AI_AWS_REGION", cfg.AWSRegion)
		need("AI_AWS_ACCESS_KEY_ID", cfg.AWSAccessKeyID)
		need("AI_AWS_SECRET_ACCESS_KEY", string(cfg.AWSSecretAccessKey))
	case config.AIProviderAzure:
		need("AI_AZURE_API_KEY", string(cfg.AzureAPIKey))
		if normalize(cfg.AzureBaseURL) == "" && normalize(cfg.AzureResourceName) == "" {
			missing = append(missing, "AI_AZURE_BASE_URL or AI_AZURE_RESOURCE_NAME")
		}
	case config.AIProviderGoogle:
		need("AI_GOOGLE_CLOUD_PROJECT", cfg.GoogleCloudProject)
		need("AI_GOOGLE_CLOUD_LOCATION", cfg.GoogleCloudLocation)
		if j := normalize(string(cfg.GoogleCloudCredentialsJSON)); j != "" && !config.IsJSONObject(j) {
			invalid = append(invalid, "AI_GOOGLE_CLOUD_CREDENTIALS_JSON")
		}
	case config.AIProviderOpenAICompatible:
		if base := normalize(cfg.OpenAICompatibleBaseURL); base == "" {
			missing = append(missing, "AI_OPENAI_COMPATIBLE_BASE_URL")
		} else if !config.IsHTTPURL(base) {
			invalid = append(invalid, "AI_OPENAI_COMPATIBLE_BASE_URL")
		}
		for _, v := range []struct {
			name  string
			value config.Secret
		}{{"AI_OPENAI_COMPATIBLE_HEADERS_JSON", cfg.OpenAICompatibleHeadersJSON}, {"AI_OPENAI_COMPATIBLE_QUERY_PARAMS_JSON", cfg.OpenAICompatibleQueryParamsJSON}} {
			if j := normalize(string(v.value)); j != "" {
				if _, err := config.ParseStringRecord(j); err != nil {
					invalid = append(invalid, v.name)
				}
			}
		}
	}
	model := normalize(cfg.Model)
	if model == "" && !slices.Contains(missing, "AI_MODEL") {
		missing = append(missing, "AI_MODEL")
	}
	s := ProviderStatus{Provider: provider, IsConfigured: len(missing) == 0 && len(invalid) == 0, Model: model, MissingFields: missing, InvalidFields: invalid}
	switch {
	case len(invalid) > 0:
		s.ErrorCode = ProviderInvalidCredentials
	case slices.Contains(missing, "AI_MODEL"):
		s.ErrorCode = ProviderMissingModel
	case len(missing) > 0:
		s.ErrorCode = ProviderMissingCredentials
	}
	return s
}

// configurationMessage is forma2's getAIConfigurationErrorMessage.
func configurationMessage(s Status) string {
	switch s.ErrorCode {
	case StatusProviderMissing:
		return "AI_PROVIDER is required"
	case StatusInvalidProvider:
		return "AI_PROVIDER must be one of: " + strings.Join(config.AIProviders, ", ")
	case StatusProviderNotConfigured:
		parts := []string{"Active AI provider is not configured correctly."}
		if len(s.MissingFields) > 0 {
			parts = append(parts, "Missing: "+strings.Join(s.MissingFields, ", "))
		}
		if len(s.InvalidFields) > 0 {
			parts = append(parts, "Invalid: "+strings.Join(s.InvalidFields, ", "))
		}
		return strings.Join(parts, " ")
	default:
		return "AI is not configured"
	}
}
