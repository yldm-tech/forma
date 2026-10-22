package clientapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/response"
)

// recaptchaVerifyURL is Google's siteverify endpoint.
const recaptchaVerifyURL = "https://www.google.com/recaptcha/api/siteverify"

// recaptchaTimeout is forma2's abort of a verification that takes too long.
const recaptchaTimeout = 5 * time.Second

// codeRecaptchaFailed is forma2's RECAPTCHA_VERIFICATION_ERROR_CODE, the details.code the survey runtime keys off to show its reCAPTCHA error.
const codeRecaptchaFailed = "recaptcha_verification_failed"

// Recaptcha verifies reCAPTCHA v3 tokens with Google, forma2's verifyRecaptchaToken.
type Recaptcha struct {
	siteKey, secretKey string
	endpoint           string
	client             *http.Client
	log                *slog.Logger
}

func NewRecaptcha(cfg config.Config, log *slog.Logger) *Recaptcha {
	return &Recaptcha{siteKey: cfg.RecaptchaSiteKey, secretKey: cfg.RecaptchaSecretKey, endpoint: recaptchaVerifyURL, client: &http.Client{Timeout: recaptchaTimeout}, log: log}
}

// Verify reports whether Google accepts token with a score of at least threshold. It fails closed: without both keys configured, on a failed request or a non-2xx answer, when Google reports no success, or when the score is below the threshold. A score equal to the threshold passes, and so does an answer without a score.
func (r *Recaptcha) Verify(ctx context.Context, token string, threshold float64) bool {
	if r.siteKey == "" || r.secretKey == "" {
		r.log.ErrorContext(ctx, "reCAPTCHA verification rejected: RECAPTCHA_SITE_KEY and/or RECAPTCHA_SECRET_KEY are not configured. Set both env vars or disable reCAPTCHA on the survey.")
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, recaptchaTimeout)
	defer cancel()
	form := url.Values{"secret": {r.secretKey}, "response": {token}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		r.log.ErrorContext(ctx, "Error verifying reCAPTCHA token", "err", err)
		return false
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := r.client.Do(req)
	if err != nil {
		r.log.ErrorContext(ctx, "Error verifying reCAPTCHA token", "err", err)
		return false
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		r.log.ErrorContext(ctx, fmt.Sprintf("reCAPTCHA HTTP error: %d", res.StatusCode))
		return false
	}
	var answer struct {
		Success bool     `json:"success"`
		Score   *float64 `json:"score"`
	}
	if err := json.NewDecoder(res.Body).Decode(&answer); err != nil {
		r.log.ErrorContext(ctx, "Error verifying reCAPTCHA token", "err", err)
		return false
	}
	if !answer.Success {
		r.log.ErrorContext(ctx, "reCAPTCHA verification failed")
		return false
	}
	if answer.Score != nil && *answer.Score < threshold {
		r.log.ErrorContext(ctx, "reCAPTCHA score below threshold", "score", *answer.Score, "threshold", threshold)
		return false
	}
	return true
}

// checkRecaptcha is forma2's verifyResponseRecaptcha, shared by both response creates: a survey with reCAPTCHA on needs a token (400), its organization's billing record (404 Organization), and Google's acceptance (400). Without the spam protection entitlement forma2 only logged and still verified; the rewrite has no licence gate, so it always verifies.
func (h *Handler) checkRecaptcha(ctx context.Context, sv *response.Survey, workspaceID uuid.UUID, token *string) error {
	var settings struct {
		Enabled   bool    `json:"enabled"`
		Threshold float64 `json:"threshold"`
	}
	if len(sv.Recaptcha) > 0 {
		_ = json.Unmarshal(sv.Recaptcha, &settings)
	}
	if !settings.Enabled {
		return nil
	}
	if token == nil || *token == "" {
		h.log.WarnContext(ctx, "Missing recaptcha token")
		return badRequest("Missing recaptcha token", fields{{"code", codeRecaptchaFailed}})
	}
	var billed bool
	if err := h.db.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM workspaces w JOIN organization_billing b ON b.organization_id = w.organization_id WHERE w.id = ?)`, workspaceID).Scan(&billed).Error; err != nil {
		// forma2 read a failed lookup as a missing record.
		h.log.ErrorContext(ctx, "Failed to get organization billing by workspace ID", "err", err)
	}
	if !billed {
		return notFound("Organization", nil)
	}
	if !h.recaptcha.Verify(ctx, *token, settings.Threshold) {
		return badRequest("reCAPTCHA verification failed", fields{{"code", codeRecaptchaFailed}})
	}
	return nil
}
