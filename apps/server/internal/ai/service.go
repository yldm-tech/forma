package ai

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"go.opentelemetry.io/otel"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

// Error codes, forma2's AI_ERROR_CODES. The first three say AI is unavailable to the organization, the last that its provider is rate limiting it.
const (
	CodeFeaturesNotEnabled    = "ai_features_not_enabled"
	CodeSmartToolsDisabled    = "ai_smart_tools_disabled"
	CodeInstanceNotConfigured = "ai_instance_not_configured"
	CodeQuotaExceeded         = "ai_quota_exceeded"
)

// Reasons AI is unavailable, forma2's TAIUnavailableReason.
const (
	ReasonNotInPlan             = "not_in_plan"
	ReasonNotEnabled            = "not_enabled"
	ReasonInstanceNotConfigured = "instance_not_configured"
	ReasonReadOnly              = "read_only"
)

// QuotaExceededMessage is forma2's wording for a provider that is rate limiting the instance.
const QuotaExceededMessage = "The AI provider is temporarily rate-limited. Try again shortly."

// unavailableMessages are forma2's AI_UNAVAILABLE_DETAILS.
var unavailableMessages = map[string]string{
	CodeFeaturesNotEnabled:    "AI smart tools are not available for this organization.",
	CodeSmartToolsDisabled:    "AI smart tools are disabled for this organization.",
	CodeInstanceNotConfigured: "AI is not configured for this Forma instance.",
}

// UnavailableMessage is the message of an unavailable code, empty for any other code.
func UnavailableMessage(code string) string { return unavailableMessages[code] }

// OrganizationConfig is forma2's TOrganizationAIConfig.
type OrganizationConfig struct {
	OrganizationID         uuid.UUID `json:"organizationId"`
	IsAISmartToolsEnabled  bool      `json:"isAISmartToolsEnabled"`
	IsAISmartToolsEntitled bool      `json:"isAISmartToolsEntitled"`
	IsInstanceConfigured   bool      `json:"isInstanceConfigured"`
}

// UnavailableReason is forma2's getAISmartToolsUnavailableReason, empty when AI is available. An instance without a provider is reported before the organization's toggle, since turning the toggle on cannot fix it and only an operator can.
func (c OrganizationConfig) UnavailableReason() string {
	switch {
	case !c.IsAISmartToolsEntitled:
		return ReasonNotInPlan
	case !c.IsInstanceConfigured:
		return ReasonInstanceNotConfigured
	case !c.IsAISmartToolsEnabled:
		return ReasonNotEnabled
	}
	return ""
}

// Service is forma2's organization AI service (apps/web/lib/ai/service.ts): every AI feature generates through it, so the organization's toggle is checked, a provider 429 becomes ai_quota_exceeded, failures are logged once with what an operator needs, and every completed generation's cost is recorded.
type Service struct {
	client *Client
	repo   *Repository
	authz  *authz.Authorizer
	log    *slog.Logger
	now    func() time.Time
	usage  *usageMeters
}

func NewService(client *Client, repo *Repository, authorizer *authz.Authorizer, log *slog.Logger) *Service {
	return &Service{client: client, repo: repo, authz: authorizer, log: log, now: time.Now, usage: newUsageMeters(otel.GetMeterProvider())}
}

// Client is the instance's AI client.
func (s *Service) Client() *Client { return s.client }

// OrganizationConfig is forma2's getOrganizationAIConfig. AI smart tools are always entitled: forma2's getIsAISmartToolsEnabled had no plan gate left.
func (s *Service) OrganizationConfig(ctx context.Context, organizationID uuid.UUID) (*OrganizationConfig, error) {
	enabled, found, err := s.repo.smartToolsEnabled(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, apperr.NotFound("Organization", organizationID.String())
	}
	return &OrganizationConfig{OrganizationID: organizationID, IsAISmartToolsEnabled: enabled, IsAISmartToolsEntitled: true, IsInstanceConfigured: s.client.Configured()}, nil
}

// unavailable is forma2's OperationNotAllowedError carrying an AI error code.
func unavailable(code string) *apperr.Error {
	return &apperr.Error{Status: http.StatusForbidden, Code: code, Message: unavailableMessages[code]}
}

// Assert is forma2's assertOrganizationAIConfigured: entitlement, then the organization's toggle, then the instance's configuration.
func (s *Service) Assert(ctx context.Context, organizationID uuid.UUID) (*OrganizationConfig, error) {
	cfg, err := s.OrganizationConfig(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	switch cfg.UnavailableReason() {
	case ReasonNotInPlan:
		return nil, unavailable(CodeFeaturesNotEnabled)
	case ReasonNotEnabled:
		return nil, unavailable(CodeSmartToolsDisabled)
	case ReasonInstanceNotConfigured:
		return nil, unavailable(CodeInstanceNotConfigured)
	}
	return cfg, nil
}

// GenerateText is forma2's generateOrganizationAIText.
func (s *Service) GenerateText(ctx context.Context, organizationID uuid.UUID, feature Feature, req Request) (*TextResult, error) {
	cfg, err := s.Assert(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	out, err := s.client.GenerateText(ctx, feature, req)
	if err != nil {
		return nil, s.classify(ctx, err, cfg, "Failed to generate organization AI text")
	}
	s.recordUsage(ctx, organizationID, feature, out.Model, out.Usage)
	return out, nil
}

// GenerateObject is forma2's generateOrganizationAIObject.
func (s *Service) GenerateObject(ctx context.Context, organizationID uuid.UUID, feature Feature, req ObjectRequest) (*ObjectResult, error) {
	cfg, err := s.Assert(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	out, err := s.client.GenerateObject(ctx, feature, req)
	if err != nil {
		return nil, s.classify(ctx, err, cfg, "Failed to generate organization AI object")
	}
	s.recordUsage(ctx, organizationID, feature, out.Model, out.Usage)
	return out, nil
}

// StreamObject is forma2's streamOrganizationAIObject. A generation the caller cancelled records no usage.
func (s *Service) StreamObject(ctx context.Context, organizationID uuid.UUID, feature Feature, req ObjectRequest, onPartial func(any)) (*ObjectResult, error) {
	cfg, err := s.Assert(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	out, err := s.client.StreamObject(ctx, feature, req, onPartial)
	if err != nil {
		return nil, s.classify(ctx, err, cfg, "Failed to stream organization AI object")
	}
	s.recordUsage(ctx, organizationID, feature, out.Model, out.Usage)
	return out, nil
}

// IsAbort reports whether a generation failed because its caller went away, forma2's isAbortError: a user pressing Stop or closing the tab, not an incident.
func IsAbort(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled)
}

// classify is forma2's classifyOrganizationAIFailure: a cancelled generation passes through unlogged; anything else is logged once with the provider's status, and a provider 429 becomes ai_quota_exceeded with its retry-after.
func (s *Service) classify(ctx context.Context, err error, cfg *OrganizationConfig, message string) error {
	if IsAbort(ctx, err) {
		return err
	}
	info := ClassifyProviderError(err, s.now())
	attrs := []any{"organizationId", cfg.OrganizationID.String(), "isInstanceConfigured", cfg.IsInstanceConfigured}
	var ce *ConfigurationError
	if errors.As(err, &ce) {
		attrs = append(attrs, "errorCode", ce.Code)
	}
	if info != nil {
		attrs = append(attrs, "statusCode", info.StatusCode, "isQuotaExhausted", info.QuotaExhausted, "isRetryable", info.Retryable)
	}
	attrs = append(attrs, "err", err)
	s.log.ErrorContext(ctx, message, attrs...)
	if info != nil && info.QuotaExhausted {
		e := &apperr.Error{Status: http.StatusTooManyRequests, Code: CodeQuotaExceeded, Message: QuotaExceededMessage}
		if info.RetryAfter != nil {
			e.RetryAfter = *info.RetryAfter
			e.Details = map[string]any{"retryAfter": *info.RetryAfter}
		}
		return e
	}
	return err
}

// recordUsage is forma2's recordAIGenerationUsage: one log line per completed generation with its token counts, the feature and the model it ran on. Never the prompt or the response.
func (s *Service) recordUsage(ctx context.Context, organizationID uuid.UUID, feature Feature, model string, u Usage) {
	name := string(feature)
	if name == "" {
		name = "unknown"
	}
	if model == "" {
		model = "unknown"
	}
	s.log.InfoContext(ctx, "AI generation usage", "organizationId", organizationID.String(), "feature", name, "model", model,
		"inputTokens", tokenCount(u.InputTokens), "outputTokens", tokenCount(u.OutputTokens), "reasoningTokens", tokenCount(u.ReasoningTokens))
	s.usage.record(ctx, name, model, u)
}

// tokenCount logs a count the provider did not report as null rather than as a pointer.
func tokenCount(n *int64) any {
	if n == nil {
		return nil
	}
	return *n
}
