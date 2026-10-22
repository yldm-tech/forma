package ai

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/openai/openai-go/v3"
	"google.golang.org/genai"
)

// ConfigurationError is forma2's AIConfigurationError: the instance's AI configuration cannot serve a generation.
type ConfigurationError struct {
	Code          string
	Message       string
	Provider      string
	Model         string
	MissingFields []string
	InvalidFields []string
}

func (e *ConfigurationError) Error() string { return e.Message }

// OutputTokenLimitError is forma2's AIOutputTokenLimitError: the model stopped at the output token limit, so no complete structured output exists.
type OutputTokenLimitError struct {
	MaxOutputTokens int
	OutputTokens    *int64
	ReasoningTokens *int64
}

func (e *OutputTokenLimitError) Error() string {
	return "AI generation stopped because the output token limit was reached"
}

// NoObjectError is the AI SDK's NoObjectGeneratedError: the generation finished but its text is not an object of the requested schema.
type NoObjectError struct {
	FinishReason FinishReason
	Cause        error
}

func (e *NoObjectError) Error() string {
	if e.Cause != nil {
		return "No object generated: " + e.Cause.Error()
	}
	return fmt.Sprintf("No object generated: the generation finished with %q", e.FinishReason)
}

func (e *NoObjectError) Unwrap() error { return e.Cause }

// ProviderErrorInfo is forma2's AIProviderErrorInfo: what a provider failure means for the caller.
type ProviderErrorInfo struct {
	// QuotaExhausted is a 429 from the provider.
	QuotaExhausted bool
	Retryable      bool
	StatusCode     int
	// RetryAfter is the provider's retry-after in seconds, when it sent one.
	RetryAfter *int
}

// ClassifyProviderError is forma2's classifyAIProviderError: the HTTP status a provider answered with, read from the SDK error of whichever provider failed. It returns nil for errors that are not provider responses.
func ClassifyProviderError(err error, now time.Time) *ProviderErrorInfo {
	status, header, ok := providerResponse(err)
	if !ok {
		return nil
	}
	quota := status == http.StatusTooManyRequests
	return &ProviderErrorInfo{
		QuotaExhausted: quota,
		// A 429 is always worth retrying later; otherwise the SDKs' own rule, a timeout, conflict or server error.
		Retryable:  quota || status == http.StatusRequestTimeout || status == http.StatusConflict || status >= http.StatusInternalServerError,
		StatusCode: status,
		RetryAfter: retryAfterSeconds(header, now),
	}
}

func providerResponse(err error) (int, http.Header, bool) {
	var oe *openai.Error
	if errors.As(err, &oe) {
		var h http.Header
		if oe.Response != nil {
			h = oe.Response.Header
		}
		return oe.StatusCode, h, true
	}
	var re *awshttp.ResponseError
	if errors.As(err, &re) {
		var h http.Header
		if re.Response != nil {
			h = re.Response.Header
		}
		return re.HTTPStatusCode(), h, true
	}
	var ge genai.APIError
	if errors.As(err, &ge) {
		// The genai SDK keeps no response headers on its errors, so a Vertex 429 carries no retry-after.
		return ge.Code, nil, true
	}
	return 0, nil, false
}

// retryAfterSeconds is forma2's parseRetryAfterSeconds: delay-seconds, or an HTTP-date turned into the seconds left (never negative).
func retryAfterSeconds(h http.Header, now time.Time) *int {
	raw := h.Get("Retry-After")
	if raw == "" {
		return nil
	}
	if n, err := strconv.Atoi(leadingInteger(raw)); err == nil {
		return &n
	}
	if at, err := http.ParseTime(raw); err == nil {
		n := max(0, int(math.Ceil(at.Sub(now).Seconds())))
		return &n
	}
	return nil
}

// leadingInteger is the prefix parseInt reads: an optional sign and the digits after it.
func leadingInteger(s string) string {
	end := 0
	if end < len(s) && (s[end] == '-' || s[end] == '+') {
		end++
	}
	start := end
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == start {
		return ""
	}
	return s[:end]
}
