package auth

import (
	"net/http"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
)

// Stable codes the SPA maps to localized messages. The sign-up and reset codes are forma2's (packages/types/errors.ts); the sign-in codes are Better Auth's error codes, lowercased.
const (
	CodeSignupDisabled            = "signup_disabled"
	CodeInviteTokenInvalid        = "invite_token_invalid"
	CodeEmailDomainNotAllowed     = "email_domain_not_allowed"
	CodePasswordCompromised       = "password_compromised"
	CodeCaptchaFailed             = "captcha_verification_failed"
	CodeInvalidEmailOrPassword    = "invalid_email_or_password"
	CodeEmailNotVerified          = "email_not_verified"
	CodeAccountInactive           = "account_inactive"
	CodeEmailPasswordDisabled     = "email_password_disabled"
	CodeInvalidPasswordResetToken = "invalid_password_reset_token"
	CodeInvalidToken              = "invalid_token"
	CodeTokenExpired              = "token_expired"
	CodeInvalidPassword           = "invalid_password"
	CodePasswordRequired          = "password_required"
	CodePasswordNotSet            = "password_not_set"
	CodeEmailSendFailed           = "email_send_failed"
	CodeInvalidTwoFactorCookie    = "invalid_two_factor_cookie"
)

func errSignupDisabled() error {
	return apperr.InvalidInput(CodeSignupDisabled, "Signup is disabled on this instance.")
}

func errInviteTokenInvalid() error {
	return apperr.InvalidInput(CodeInviteTokenInvalid, "The invite link is invalid, has expired, or was issued to a different email address.")
}

func errEmailDomainNotAllowed() error {
	return apperr.InvalidInput(CodeEmailDomainNotAllowed, "Please sign up with your work email address.")
}

func errPasswordCompromised() error {
	return apperr.InvalidInput(CodePasswordCompromised, "The password you entered has been found in a data breach.")
}

func errCaptchaFailed() error {
	return apperr.InvalidInput(CodeCaptchaFailed, "reCAPTCHA verification failed")
}

func errInvalidCredentials() error {
	return &apperr.Error{Status: http.StatusUnauthorized, Code: CodeInvalidEmailOrPassword, Message: "Invalid email or password"}
}

func errEmailNotVerified() error {
	return &apperr.Error{Status: http.StatusForbidden, Code: CodeEmailNotVerified, Message: "Email not verified"}
}

func errAccountInactive() error {
	return &apperr.Error{Status: http.StatusForbidden, Code: CodeAccountInactive, Message: "Your account is currently inactive. Please contact the organization admin."}
}

func errEmailPasswordDisabled() error {
	return apperr.InvalidInput(CodeEmailPasswordDisabled, "Email and password is not enabled")
}

func errPasswordResetDisabled() error {
	return apperr.OperationNotAllowed("Password reset is disabled")
}

func errInvalidPasswordResetToken() error {
	e := apperr.InvalidInput(CodeInvalidPasswordResetToken, "The password reset link is invalid or has expired.")
	e.Details = map[string]any{"reason": "invalid_or_expired"}
	return e
}

func errInvalidToken() error {
	return apperr.InvalidInput(CodeInvalidToken, "The link is invalid.")
}

func errTokenExpired() error {
	return apperr.InvalidInput(CodeTokenExpired, "The link has expired.")
}

func errInvalidPassword() error {
	return apperr.InvalidInput(CodeInvalidPassword, "Invalid password")
}

func errPasswordNotSet() error {
	return apperr.InvalidInput(CodePasswordNotSet, "Password is not set for this user")
}

func errEmailSendFailed() error {
	return apperr.InvalidInput(CodeEmailSendFailed, "Failed to send email")
}

func errInvalidTwoFactorCookie() error {
	return &apperr.Error{Status: http.StatusUnauthorized, Code: CodeInvalidTwoFactorCookie, Message: "Invalid two factor cookie"}
}
