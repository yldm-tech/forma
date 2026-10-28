// Package turnstile verifies Cloudflare Turnstile tokens with Cloudflare's siteverify API. Sign-up (internal/auth) and the spam protection of survey responses (internal/clientapi) share it, with the instance's TURNSTILE_SECRET_KEY.
package turnstile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
)

const (
	// VerifyURL is Cloudflare's siteverify endpoint.
	VerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	timeout   = 5 * time.Second
	// responseLimit bounds what is read of Cloudflare's answer, a few hundred bytes.
	responseLimit = 1 << 20
	// maxTokenLength is the longest token Cloudflare issues; a longer one is refused without asking Cloudflare.
	maxTokenLength = 2048
)

// ErrUnavailable is the error Verify returns when Cloudflare gave no verdict on the token: the request failed or timed out, the answer was not a 200 or not JSON, or Cloudflare reported a problem of its own or of the instance's secret key rather than of the token. Callers must not read it as an accepted token.
var ErrUnavailable = errors.New("turnstile verification unavailable")

// unavailableCodes are the siteverify error codes that say nothing about the token: Cloudflare's own failure, the instance's secret key missing or wrong, and a request Cloudflare could not read.
var unavailableCodes = []string{"internal-error", "missing-input-secret", "invalid-input-secret", "bad-request"}

// Verifier checks tokens against one secret key.
type Verifier struct {
	client    *http.Client
	verifyURL string
	secret    string
}

// New is the verifier of the instance's TURNSTILE_SECRET_KEY, on Cloudflare's endpoint.
func New(cfg config.Config) *Verifier {
	return NewWithEndpoint(cfg.TurnstileSecretKey, VerifyURL)
}

// NewWithEndpoint is a verifier that posts to verifyURL, the seam tests point at a stand-in for Cloudflare.
func NewWithEndpoint(secret, verifyURL string) *Verifier {
	return &Verifier{client: &http.Client{Timeout: timeout}, verifyURL: verifyURL, secret: secret}
}

// Verify reports whether Cloudflare accepts token, presented by the client at remoteIP (httpapi.ClientIP; an address the proxy chain did not vouch for is not sent). An empty or overlong token is refused without a request. A token Cloudflare refuses (invalid, expired or already spent) is false with a nil error; when Cloudflare gives no verdict the error wraps ErrUnavailable.
func (v *Verifier) Verify(ctx context.Context, token, remoteIP string) (bool, error) {
	if token == "" || len(token) > maxTokenLength {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request := map[string]string{"secret": v.secret, "response": token}
	if remoteIP != "" && remoteIP != httpapi.UntrustedClientIP {
		request["remoteip"] = remoteIP
	}
	body, err := json.Marshal(request)
	if err != nil {
		return false, fmt.Errorf("%w: encode request: %w", ErrUnavailable, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.verifyURL, bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("%w: build request: %w", ErrUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := v.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return false, fmt.Errorf("%w: siteverify answered %d", ErrUnavailable, res.StatusCode)
	}
	var out struct {
		Success    bool     `json:"success"`
		ErrorCodes []string `json:"error-codes"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, responseLimit)).Decode(&out); err != nil {
		return false, fmt.Errorf("%w: decode answer: %w", ErrUnavailable, err)
	}
	if out.Success {
		return true, nil
	}
	for _, code := range out.ErrorCodes {
		if slices.Contains(unavailableCodes, code) {
			return false, fmt.Errorf("%w: siteverify reported %v", ErrUnavailable, out.ErrorCodes)
		}
	}
	return false, nil
}
