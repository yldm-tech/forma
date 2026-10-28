package clientapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/response"
)

// Spam protection unavailable: Cloudflare gave no verdict on a Turnstile token (turnstile.ErrUnavailable). The response is refused with 503, which the survey runtime retries like any failed send, instead of being taken without a check.
const (
	codeServiceUnavailable           = "service_unavailable"
	codeSpamProtectionUnavailable    = "spam_protection_unavailable"
	msgSpamProtectionUnavailable     = "Spam protection is temporarily unavailable. Please try again."
	msgMissingTurnstileToken         = "Missing turnstile token"
	msgTurnstileVerificationRejected = "Turnstile verification failed"
)

// checkSpamProtection is forma2's verifyResponseRecaptcha, shared by both response creates, with the instance's one spam protection service (config.SpamProtectionProvider): a survey whose recaptcha setting is on needs a token (400), its organization's billing record (404 Organization), and the service's acceptance (400). With Turnstile the token is the body's turnstileToken, verified with the client's address, and Cloudflare not answering is a 503; otherwise it is recaptchaToken, checked against the survey's score threshold as before. Both refusals carry details.code recaptcha_verification_failed, the code the survey runtime shows its spam protection error for. Only the create is checked: the updates that carry the rest of a partial response never need a fresh token. Without the spam protection entitlement forma2 only logged and still verified; the rewrite has no licence gate, so it always verifies.
func (h *Handler) checkSpamProtection(ctx context.Context, sv *response.Survey, workspaceID uuid.UUID, in *responseInput) error {
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
	turnstile := h.cfg.SpamProtectionProvider() == config.SpamProtectionTurnstile
	token := in.RecaptchaToken
	if turnstile {
		token = in.TurnstileToken
	}
	if token == nil || *token == "" {
		if turnstile {
			h.log.WarnContext(ctx, "Missing turnstile token")
			return badRequest(msgMissingTurnstileToken, fields{{"code", codeRecaptchaFailed}})
		}
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
	if !turnstile {
		if !h.recaptcha.Verify(ctx, *token, settings.Threshold) {
			return badRequest("reCAPTCHA verification failed", fields{{"code", codeRecaptchaFailed}})
		}
		return nil
	}
	ok, err := h.turnstile.Verify(ctx, *token, httpapi.ClientIP(ctx))
	if err != nil {
		h.log.ErrorContext(ctx, "Turnstile verification unavailable; refusing the response", "err", err)
		return newError(http.StatusServiceUnavailable, codeServiceUnavailable, msgSpamProtectionUnavailable, fields{{"code", codeSpamProtectionUnavailable}})
	}
	if !ok {
		h.log.WarnContext(ctx, "Turnstile verification failed")
		return badRequest(msgTurnstileVerificationRejected, fields{{"code", codeRecaptchaFailed}})
	}
	return nil
}
