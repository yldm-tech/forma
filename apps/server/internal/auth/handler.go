package auth

import (
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/rowid"
	"github.com/yldm-tech/forma/apps/server/internal/survey/scheduling"
)

// Handler serves /api/app/auth.
type Handler struct {
	cfg       config.Config
	svc       *Service
	sessions  *Sessions
	twoFactor *TwoFactor
	cookies   cookieSettings
}

func NewHandler(cfg config.Config, svc *Service, sessions *Sessions, twoFactor *TwoFactor) *Handler {
	return &Handler{cfg: cfg, svc: svc, sessions: sessions, twoFactor: twoFactor, cookies: newCookieSettings(cfg)}
}

// Register mounts the routes on the /api/app group, which already applies the error envelope and the CSRF check to every mutation.
func (h *Handler) Register(r gin.IRouter) {
	g := r.Group("/auth")
	g.GET("/options", h.options)
	g.GET("/me", h.sessions.Require(), h.me)
	g.PATCH("/me", h.sessions.Require(), h.updateProfile)
	g.GET("/me/notifications", h.sessions.Require(), h.notifications)
	g.PATCH("/me/notifications", h.sessions.Require(), h.setNotification)
	g.POST("/signup", h.signup)
	g.POST("/sign-in", h.signIn)
	g.POST("/sign-out", h.sessions.Optional(), h.signOut)
	g.POST("/verification/resend", h.resendVerification)
	g.POST("/verification/verify", h.sessions.Optional(), h.verifyEmail)
	g.POST("/password/forgot", h.forgotPassword)
	g.POST("/password/reset", h.resetPassword)
	g.POST("/password/change", h.sessions.Require(), h.changePassword)
	g.POST("/password/reset-link", h.sessions.Require(), h.requestResetLink)
	g.GET("/sessions", h.sessions.Require(), h.listSessions)
	g.POST("/sessions/revoke-others", h.sessions.Require(), h.revokeOtherSessions)
	g.DELETE("/sessions/:id", h.sessions.Require(), h.revokeSession)
	g.POST("/email/change", h.sessions.Require(), h.requestEmailChange)
	g.POST("/email/verify", h.confirmEmailChange)
	g.POST("/account/delete", h.sessions.Require(), h.deleteAccount)
	g.GET("/deletion-preview", h.sessions.Require(), h.deletionPreview)
	g.POST("/account/delete/request", h.sessions.Require(), h.requestAccountDeletion)
	g.POST("/account/delete/confirm", h.sessions.Require(), h.confirmAccountDeletion)
	g.POST("/two-factor/setup", h.sessions.Require(), h.setupTwoFactor)
	g.POST("/two-factor/enable", h.sessions.Require(), h.enableTwoFactor)
	g.POST("/two-factor/disable", h.sessions.Require(), h.disableTwoFactor)
	g.POST("/two-factor/verify-totp", h.verifyTOTP)
	g.POST("/two-factor/verify-backup-code", h.verifyBackupCode)
}

// bind decodes the body and runs the request's own field rules.
func bind[T interface{ validate() error }](c *gin.Context, req T) bool {
	if err := httpapi.BindJSON(c, req); err != nil {
		httpapi.Fail(c, err)
		return false
	}
	if err := req.validate(); err != nil {
		httpapi.Fail(c, err)
		return false
	}
	return true
}

// actor is the authenticated caller; routes that call it are mounted behind Sessions.Require.
func actor(c *gin.Context) httpapi.Actor {
	a, _ := httpapi.ActorFrom(c.Request.Context())
	return a
}

type successResponse struct {
	Success bool `json:"success"`
}

type optionsResponse struct {
	EmailAuthEnabled          bool   `json:"emailAuthEnabled"`
	SignupEnabled             bool   `json:"signupEnabled"`
	PasswordResetEnabled      bool   `json:"passwordResetEnabled"`
	EmailVerificationRequired bool   `json:"emailVerificationRequired"`
	SMTPConfigured            bool   `json:"smtpConfigured"`
	TurnstileSiteKey          string `json:"turnstileSiteKey,omitempty"`
	// SSOProviders are the identity providers offered (google, github, azuread, openid), in the order the page shows them; OIDCDisplayName labels the generic one's button.
	SSOProviders    []string `json:"ssoProviders"`
	OIDCDisplayName string   `json:"oidcDisplayName,omitempty"`
	// TermsURL and PrivacyURL are forma2's TERMS_URL and PRIVACY_URL, linked under the sign-up form when set.
	TermsURL   string `json:"termsUrl,omitempty"`
	PrivacyURL string `json:"privacyUrl,omitempty"`
	// IsFormaCloud makes the sign-up form offer the product update emails instead of the security ones.
	IsFormaCloud bool `json:"isFormaCloud"`
	// SpamProtectionAvailable is whether reCAPTCHA keys are configured, so the survey editor offers spam protection: without them the server refuses a survey that turns it on.
	SpamProtectionAvailable bool `json:"spamProtectionAvailable"`
}

// options tells the sign-in and sign-up pages what this instance offers.
func (h *Handler) options(c *gin.Context) {
	signup, err := h.svc.uninvitedSignupAllowed(c.Request.Context())
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	res := optionsResponse{
		EmailAuthEnabled:          !h.cfg.EmailAuthDisabled,
		SignupEnabled:             signup && !h.cfg.EmailAuthDisabled,
		PasswordResetEnabled:      !h.cfg.PasswordResetDisabled,
		EmailVerificationRequired: !h.cfg.EmailVerificationDisabled,
		SMTPConfigured:            h.cfg.SMTPConfigured(),
		SSOProviders:              enabledSSOProviders(h.cfg.SSO),
		TermsURL:                  h.cfg.TermsURL,
		PrivacyURL:                h.cfg.PrivacyURL,
		IsFormaCloud:              h.cfg.IsFormaCloud,
		SpamProtectionAvailable:   h.cfg.RecaptchaConfigured(),
	}
	if h.cfg.SSO.OIDCEnabled() {
		res.OIDCDisplayName = h.cfg.SSO.OIDCDisplayName
	}
	if h.cfg.TurnstileConfigured() {
		res.TurnstileSiteKey = h.cfg.TurnstileSiteKey
	}
	c.JSON(http.StatusOK, res)
}

type meResponse struct {
	ID               uuid.UUID `json:"id"`
	Name             string    `json:"name"`
	Email            string    `json:"email"`
	EmailVerified    bool      `json:"emailVerified"`
	Locale           string    `json:"locale"`
	IdentityProvider string    `json:"identityProvider"`
	// HasPassword is whether the account can sign in with a password, external identities or not: such an account manages its own two-factor authentication.
	HasPassword      bool       `json:"hasPassword"`
	TwoFactorEnabled bool       `json:"twoFactorEnabled"`
	CreatedAt        time.Time  `json:"createdAt"`
	LastLoginAt      *time.Time `json:"lastLoginAt"`
}

func newMeResponse(acc *account) meResponse {
	res := meResponse{
		ID: acc.ID, Name: acc.Name, Email: acc.Email, EmailVerified: acc.EmailVerified, Locale: acc.Locale,
		IdentityProvider: acc.identityProvider(), HasPassword: acc.hasPassword(), TwoFactorEnabled: acc.TwoFactorEnabled, CreatedAt: acc.CreatedAt,
	}
	if acc.LastLoginAt.Valid {
		res.LastLoginAt = &acc.LastLoginAt.Time
	}
	return res
}

func (h *Handler) me(c *gin.Context) {
	acc, err := h.svc.currentAccount(c.Request.Context(), actor(c))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, newMeResponse(acc))
}

// profileRequest is the name and language part of forma2's ZUserPersonalInfoUpdateInput; an absent field is left unchanged.
type profileRequest struct {
	Name   *string `json:"name"`
	Locale *string `json:"locale"`
}

func (r *profileRequest) validate() error {
	var errs fieldErrors
	if r.Name != nil {
		name := checkName(&errs, *r.Name)
		r.Name = &name
	}
	if r.Locale != nil {
		if *r.Locale == "" {
			errs.add("locale", "required", "")
		}
		checkLocale(&errs, "locale", *r.Locale)
	}
	return errs.err()
}

func (h *Handler) updateProfile(c *gin.Context) {
	req := &profileRequest{}
	if !bind(c, req) {
		return
	}
	acc, err := h.svc.updateProfile(c.Request.Context(), actor(c), profileChange(*req))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, newMeResponse(acc))
}

type notificationsResponse struct {
	Organizations []NotificationOrganization `json:"organizations"`
}

func (h *Handler) notifications(c *gin.Context) {
	orgs, err := h.svc.notifications(c.Request.Context(), actor(c))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, notificationsResponse{Organizations: orgs})
}

// notificationRequest switches one setting: type "alert" with a survey id, "unsubscribedOrganizationIds" with an organization id, where enabled means the organization's new surveys alert the user, or "unsubscribedWeeklySummaryOrganizationIds" with an organization id, where enabled means the user gets the organization's weekly summary email.
type notificationRequest struct {
	Type    string    `json:"type" binding:"required,oneof=alert unsubscribedOrganizationIds unsubscribedWeeklySummaryOrganizationIds"`
	ID      uuid.UUID `json:"id" binding:"required"`
	Enabled *bool     `json:"enabled" binding:"required"`
}

func (h *Handler) setNotification(c *gin.Context) {
	var req notificationRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	if err := h.svc.setNotification(c.Request.Context(), actor(c), req.Type, req.ID, *req.Enabled); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type signupRequest struct {
	Name           string `json:"name"`
	Email          string `json:"email"`
	Password       string `json:"password"`
	InviteToken    string `json:"inviteToken"`
	UserLocale     string `json:"userLocale"`
	TurnstileToken string `json:"turnstileToken"`
	// TimeZone is the browser's zone (Intl.DateTimeFormat().resolvedOptions().timeZone), which the organization an uninvited sign-up creates displays dates and schedules surveys in. It is a default, not a choice the user made, so a zone this build cannot load is dropped rather than refused, as the organization module's create does: the organization then falls back to UTC and the instance's zone, and a sign-up never fails over a browser's zone name.
	TimeZone *string `json:"timeZone"`
	// The mailing-list choices (the security list off Forma Cloud, the product updates list on it) are accepted and ignored: forma2 posted them to its EE server's mailing endpoints, which this server does not call.
	SubscribeToSecurityUpdates bool `json:"subscribeToSecurityUpdates"`
	SubscribeToProductUpdates  bool `json:"subscribeToProductUpdates"`
	turnstileRequired          bool
}

func (r *signupRequest) validate() error {
	var errs fieldErrors
	r.Name = checkName(&errs, r.Name)
	checkEmail(&errs, r.Email)
	checkPassword(&errs, "password", r.Password)
	checkLocale(&errs, "userLocale", r.UserLocale)
	if r.TimeZone != nil && !scheduling.ValidZone(*r.TimeZone) {
		r.TimeZone = nil
	}
	if r.turnstileRequired && r.TurnstileToken == "" {
		errs.add("turnstileToken", "required", "")
	}
	return errs.err()
}

func (h *Handler) signup(c *gin.Context) {
	req := &signupRequest{turnstileRequired: h.cfg.TurnstileConfigured()}
	if !bind(c, req) {
		return
	}
	attr := readAttribution(c)
	intent, err := h.svc.signup(c.Request.Context(), signupInput{
		Name: req.Name, Email: req.Email, Password: req.Password, InviteToken: req.InviteToken, UserLocale: req.UserLocale, TurnstileToken: req.TurnstileToken, TimeZone: req.TimeZone,
	})
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	h.cookies.set(c, h.cookies.signupIntent, intent)
	h.cookies.clearAttribution(c, attr)
	c.JSON(http.StatusOK, successResponse{Success: true})
}

type signInRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (r *signInRequest) validate() error {
	var errs fieldErrors
	checkEmail(&errs, r.Email)
	if r.Password == "" {
		errs.add("password", "required", "")
	}
	return errs.err()
}

type signInResponse struct {
	// Status is "signed_in", or "two_factor_required" when the second factor must be submitted to the two-factor endpoints.
	Status string `json:"status"`
}

func (h *Handler) signIn(c *gin.Context) {
	req := &signInRequest{}
	if !bind(c, req) {
		return
	}
	attr := readAttribution(c)
	res, err := h.svc.signIn(c.Request.Context(), req.Email, req.Password, clientMetaFrom(c))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	if res.pending != nil {
		h.cookies.set(c, h.cookies.twoFactor, *res.pending)
		c.JSON(http.StatusOK, signInResponse{Status: "two_factor_required"})
		return
	}
	h.cookies.setSession(c, *res.session)
	h.cookies.clear(c, h.cookies.twoFactor)
	h.cookies.clearAttribution(c, attr)
	c.JSON(http.StatusOK, signInResponse{Status: "signed_in"})
}

type signOutRequest struct {
	Reason         string     `json:"reason"`
	OrganizationID *uuid.UUID `json:"organizationId"`
}

func (r *signOutRequest) validate() error {
	var errs fieldErrors
	if r.Reason != "" && !slices.Contains(signOutReasons, r.Reason) {
		errs.add("reason", "oneof", strings.Join(signOutReasons, " "))
	}
	return errs.err()
}

// signOut ends the session if there is one and always clears the cookie, so the browser is signed out whatever state the session was in. The body is optional.
func (h *Handler) signOut(c *gin.Context) {
	req := &signOutRequest{}
	if c.Request.ContentLength != 0 && !bind(c, req) {
		return
	}
	if a, ok := httpapi.ActorFrom(c.Request.Context()); ok {
		if err := h.svc.signOut(c.Request.Context(), a, req.Reason, req.OrganizationID); err != nil {
			httpapi.Fail(c, err)
			return
		}
	}
	h.cookies.clear(c, h.cookies.session)
	c.Status(http.StatusNoContent)
}

type emailRequest struct {
	Email string `json:"email"`
}

func (r *emailRequest) validate() error {
	var errs fieldErrors
	checkEmail(&errs, r.Email)
	return errs.err()
}

func (h *Handler) resendVerification(c *gin.Context) {
	req := &emailRequest{}
	if !bind(c, req) {
		return
	}
	intent, _ := c.Cookie(h.cookies.signupIntent)
	renewed, err := h.svc.resendVerification(c.Request.Context(), req.Email, intent)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	if renewed != nil {
		h.cookies.set(c, h.cookies.signupIntent, *renewed)
	}
	c.JSON(http.StatusOK, successResponse{Success: true})
}

type tokenRequest struct {
	Token string `json:"token"`
}

func (r *tokenRequest) validate() error {
	var errs fieldErrors
	if r.Token == "" {
		errs.add("token", "required", "")
	}
	return errs.err()
}

type verifyEmailResponse struct {
	SignedIn bool `json:"signedIn"`
}

// verifyEmail confirms an address. When it does not sign the browser in, the SPA sends the user to the sign-in page with a "verified" notice, as forma2 redirected to /auth/login?verified=1.
func (h *Handler) verifyEmail(c *gin.Context) {
	req := &tokenRequest{}
	if !bind(c, req) {
		return
	}
	var current *httpapi.Actor
	if a, ok := httpapi.ActorFrom(c.Request.Context()); ok {
		current = &a
	}
	intent, _ := c.Cookie(h.cookies.signupIntent)
	attr := readAttribution(c)
	res, err := h.svc.verifyEmail(c.Request.Context(), req.Token, intent, current, clientMetaFrom(c))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	if res.session != nil {
		h.cookies.setSession(c, *res.session)
		h.cookies.clear(c, h.cookies.signupIntent)
	}
	h.cookies.clearAttribution(c, attr)
	c.JSON(http.StatusOK, verifyEmailResponse{SignedIn: res.SignedIn})
}

func (h *Handler) forgotPassword(c *gin.Context) {
	req := &emailRequest{}
	if !bind(c, req) {
		return
	}
	if err := h.svc.forgotPassword(c.Request.Context(), req.Email); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, successResponse{Success: true})
}

type resetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (r *resetPasswordRequest) validate() error {
	var errs fieldErrors
	if r.Token == "" {
		errs.add("token", "min", "1")
	}
	checkPassword(&errs, "password", r.Password)
	return errs.err()
}

func (h *Handler) resetPassword(c *gin.Context) {
	req := &resetPasswordRequest{}
	if !bind(c, req) {
		return
	}
	if err := h.svc.resetPassword(c.Request.Context(), req.Token, req.Password); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, successResponse{Success: true})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (r *changePasswordRequest) validate() error {
	var errs fieldErrors
	if r.CurrentPassword == "" {
		errs.add("currentPassword", "required", "")
	}
	checkPassword(&errs, "newPassword", r.NewPassword)
	return errs.err()
}

func (h *Handler) changePassword(c *gin.Context) {
	req := &changePasswordRequest{}
	if !bind(c, req) {
		return
	}
	if err := h.svc.changePassword(c.Request.Context(), actor(c), req.CurrentPassword, req.NewPassword); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) requestResetLink(c *gin.Context) {
	if err := h.svc.requestResetLink(c.Request.Context(), actor(c)); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, successResponse{Success: true})
}

type sessionResponse struct {
	ID uuid.UUID `json:"id"`
	// Current marks the session of the request, the browser asking.
	Current      bool      `json:"current"`
	CreatedAt    time.Time `json:"createdAt"`
	LastActiveAt time.Time `json:"lastActiveAt"`
	ExpiresAt    time.Time `json:"expiresAt"`
	// IPAddress and UserAgent are what the browser sent when it signed in; null when it sent none.
	IPAddress *string `json:"ipAddress"`
	UserAgent *string `json:"userAgent"`
}

type sessionsResponse struct {
	// Sessions are the user's unexpired sessions, the current one first, then the most recently active; never null.
	Sessions []sessionResponse `json:"sessions"`
}

func (h *Handler) listSessions(c *gin.Context) {
	a := actor(c)
	list, err := h.svc.listSessions(c.Request.Context(), a)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	res := sessionsResponse{Sessions: make([]sessionResponse, 0, len(list))}
	for _, s := range list {
		res.Sessions = append(res.Sessions, sessionResponse{
			ID: s.ID, Current: s.ID == a.SessionID, CreatedAt: s.CreatedAt, LastActiveAt: s.LastActiveAt, ExpiresAt: s.ExpiresAt, IPAddress: s.IPAddress, UserAgent: s.UserAgent,
		})
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, res)
}

// revokeSession ends one of the caller's sessions. Ending the current one also clears the browser's cookie, as sign-out does.
func (h *Handler) revokeSession(c *gin.Context) {
	a := actor(c)
	id, ok := rowid.Parse(c.Param("id"))
	if !ok {
		httpapi.Fail(c, apperr.NotFound("Session", ""))
		return
	}
	if err := h.svc.revokeSession(c.Request.Context(), a, id); err != nil {
		httpapi.Fail(c, err)
		return
	}
	if id == a.SessionID {
		h.cookies.clear(c, h.cookies.session)
	}
	c.Status(http.StatusNoContent)
}

type revokedSessionsResponse struct {
	Revoked int64 `json:"revoked"`
}

func (h *Handler) revokeOtherSessions(c *gin.Context) {
	n, err := h.svc.revokeOtherSessions(c.Request.Context(), actor(c))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, revokedSessionsResponse{Revoked: n})
}

type emailChangeRequest struct {
	Email string `json:"email"`
	// Password is optional in the schema, but when present it must satisfy the password policy (forma2's ZUserPersonalInfoUpdateInput).
	Password *string `json:"password"`
}

func (r *emailChangeRequest) validate() error {
	var errs fieldErrors
	checkEmail(&errs, r.Email)
	if r.Password != nil {
		checkPassword(&errs, "password", *r.Password)
	}
	return errs.err()
}

// requestEmailChange answers 204 whether the change was applied, a confirmation link was sent, or the address is held by another account.
func (h *Handler) requestEmailChange(c *gin.Context) {
	req := &emailChangeRequest{}
	if !bind(c, req) {
		return
	}
	password := ""
	if req.Password != nil {
		password = *req.Password
	}
	if err := h.svc.requestEmailChange(c.Request.Context(), actor(c), req.Email, password); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type emailChangedResponse struct {
	Email string `json:"email"`
}

func (h *Handler) confirmEmailChange(c *gin.Context) {
	req := &tokenRequest{}
	if !bind(c, req) {
		return
	}
	email, err := h.svc.confirmEmailChange(c.Request.Context(), req.Token)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, emailChangedResponse{Email: email})
}

type deleteAccountRequest struct {
	Password string `json:"password"`
}

func (r *deleteAccountRequest) validate() error {
	if r.Password == "" {
		return apperr.InvalidInput(CodePasswordRequired, "Password confirmation is required to delete your account.")
	}
	return nil
}

type accountDeletedResponse struct {
	// RedirectURL is where the SPA navigates now that the account is gone.
	RedirectURL string `json:"redirectUrl"`
}

func (h *Handler) deleteAccount(c *gin.Context) {
	req := &deleteAccountRequest{}
	if !bind(c, req) {
		return
	}
	redirect, err := h.svc.deleteAccount(c.Request.Context(), actor(c), req.Password)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	h.cookies.clear(c, h.cookies.session)
	c.JSON(http.StatusOK, accountDeletedResponse{RedirectURL: redirect})
}

type deletionPreviewResponse struct {
	// Organizations are the ones the caller is the only active owner of, deleted with the account; never null.
	Organizations []soleOwnedOrganization `json:"organizations"`
}

func (h *Handler) deletionPreview(c *gin.Context) {
	orgs, err := h.svc.deletionPreview(c.Request.Context(), actor(c))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, deletionPreviewResponse{Organizations: orgs})
}

type deletionRequestedResponse struct {
	EmailSent bool `json:"emailSent"`
}

func (h *Handler) requestAccountDeletion(c *gin.Context) {
	if err := h.svc.requestAccountDeletion(c.Request.Context(), actor(c)); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, deletionRequestedResponse{EmailSent: true})
}

func (h *Handler) confirmAccountDeletion(c *gin.Context) {
	req := &tokenRequest{}
	if !bind(c, req) {
		return
	}
	redirect, err := h.svc.confirmAccountDeletion(c.Request.Context(), actor(c), req.Token)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	h.cookies.clear(c, h.cookies.session)
	c.JSON(http.StatusOK, accountDeletedResponse{RedirectURL: redirect})
}
