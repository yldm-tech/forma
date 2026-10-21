package auth

import (
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
)

// Handler serves /api/app/auth.
type Handler struct {
	cfg      config.Config
	svc      *Service
	sessions *Sessions
	cookies  cookieSettings
}

func NewHandler(cfg config.Config, svc *Service, sessions *Sessions) *Handler {
	return &Handler{cfg: cfg, svc: svc, sessions: sessions, cookies: newCookieSettings(cfg)}
}

// Register mounts the routes on the /api/app group, which already applies the error envelope and the CSRF check to every mutation.
func (h *Handler) Register(r gin.IRouter) {
	g := r.Group("/auth")
	g.GET("/options", h.options)
	g.GET("/me", h.sessions.Require(), h.me)
	g.POST("/signup", h.signup)
	g.POST("/sign-in", h.signIn)
	g.POST("/sign-out", h.sessions.Optional(), h.signOut)
	g.POST("/verification/resend", h.resendVerification)
	g.POST("/verification/verify", h.sessions.Optional(), h.verifyEmail)
	g.POST("/password/forgot", h.forgotPassword)
	g.POST("/password/reset", h.resetPassword)
	g.POST("/password/change", h.sessions.Require(), h.changePassword)
	g.POST("/password/reset-link", h.sessions.Require(), h.requestResetLink)
	g.POST("/email/change", h.sessions.Require(), h.requestEmailChange)
	g.POST("/email/verify", h.confirmEmailChange)
	g.POST("/account/delete", h.sessions.Require(), h.deleteAccount)
	g.POST("/account/delete/request", h.sessions.Require(), h.requestAccountDeletion)
	g.POST("/account/delete/confirm", h.sessions.Require(), h.confirmAccountDeletion)
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
	}
	if h.cfg.TurnstileConfigured() {
		res.TurnstileSiteKey = h.cfg.TurnstileSiteKey
	}
	c.JSON(http.StatusOK, res)
}

type meResponse struct {
	ID               uuid.UUID  `json:"id"`
	Name             string     `json:"name"`
	Email            string     `json:"email"`
	EmailVerified    bool       `json:"emailVerified"`
	Locale           string     `json:"locale"`
	IdentityProvider string     `json:"identityProvider"`
	TwoFactorEnabled bool       `json:"twoFactorEnabled"`
	CreatedAt        time.Time  `json:"createdAt"`
	LastLoginAt      *time.Time `json:"lastLoginAt"`
}

func (h *Handler) me(c *gin.Context) {
	acc, err := h.svc.repo.accountByID(c.Request.Context(), actor(c).UserID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	if acc == nil {
		httpapi.Fail(c, apperr.NotAuthenticated("Not authenticated"))
		return
	}
	res := meResponse{
		ID: acc.ID, Name: acc.Name, Email: acc.Email, EmailVerified: acc.EmailVerified, Locale: acc.Locale,
		IdentityProvider: acc.identityProvider(), TwoFactorEnabled: acc.TwoFactorEnabled, CreatedAt: acc.CreatedAt,
	}
	if acc.LastLoginAt.Valid {
		res.LastLoginAt = &acc.LastLoginAt.Time
	}
	c.JSON(http.StatusOK, res)
}

type signupRequest struct {
	Name           string `json:"name"`
	Email          string `json:"email"`
	Password       string `json:"password"`
	InviteToken    string `json:"inviteToken"`
	UserLocale     string `json:"userLocale"`
	TurnstileToken string `json:"turnstileToken"`
	// The mailing-list choices are accepted and ignored: forma2 subscribed through Brevo, which is not part of this server.
	SubscribeToSecurityUpdates bool `json:"subscribeToSecurityUpdates"`
	SubscribeToProductUpdates  bool `json:"subscribeToProductUpdates"`
	turnstileRequired          bool
}

func (r *signupRequest) validate() error {
	var errs fieldErrors
	r.Name = checkName(&errs, "name", r.Name)
	checkEmail(&errs, "email", r.Email)
	checkPassword(&errs, "password", r.Password)
	checkLocale(&errs, "userLocale", r.UserLocale)
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
	intent, err := h.svc.signup(c.Request.Context(), signupInput{
		Name: req.Name, Email: req.Email, Password: req.Password, InviteToken: req.InviteToken, UserLocale: req.UserLocale, TurnstileToken: req.TurnstileToken,
	})
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	h.cookies.set(c, h.cookies.signupIntent, intent)
	c.JSON(http.StatusOK, successResponse{Success: true})
}

type signInRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (r *signInRequest) validate() error {
	var errs fieldErrors
	checkEmail(&errs, "email", r.Email)
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
	checkEmail(&errs, "email", r.Email)
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
	res, err := h.svc.verifyEmail(c.Request.Context(), req.Token, intent, current, clientMetaFrom(c))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	if res.session != nil {
		h.cookies.setSession(c, *res.session)
		h.cookies.clear(c, h.cookies.signupIntent)
	}
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

type emailChangeRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (r *emailChangeRequest) validate() error {
	var errs fieldErrors
	checkEmail(&errs, "email", r.Email)
	return errs.err()
}

// requestEmailChange answers 204 whether the change was applied, a confirmation link was sent, or the address is held by another account.
func (h *Handler) requestEmailChange(c *gin.Context) {
	req := &emailChangeRequest{}
	if !bind(c, req) {
		return
	}
	if err := h.svc.requestEmailChange(c.Request.Context(), actor(c), req.Email, req.Password); err != nil {
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
