package auth

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
)

// codeMaxLength bounds a submitted code; a TOTP code is six digits and a backup code eleven characters with its hyphen.
const codeMaxLength = 64

func checkCode(errs *fieldErrors, field, code string, required bool) {
	switch {
	case code == "" && required:
		errs.add(field, "required", "")
	case len(code) > codeMaxLength:
		errs.add(field, "max", strconv.Itoa(codeMaxLength))
	}
}

type passwordRequest struct {
	Password string `json:"password"`
}

func (r *passwordRequest) validate() error {
	var errs fieldErrors
	if r.Password == "" {
		errs.add("password", "required", "")
	}
	return errs.err()
}

type twoFactorSetupResponse struct {
	// Secret is the key in base32, for typing into an authenticator app; KeyURI is the otpauth:// URI and DataURI the same as a QR code PNG.
	Secret      string   `json:"secret"`
	KeyURI      string   `json:"keyUri"`
	DataURI     string   `json:"dataUri"`
	BackupCodes []string `json:"backupCodes"`
}

func (h *Handler) setupTwoFactor(c *gin.Context) {
	req := &passwordRequest{}
	if !bind(c, req) {
		return
	}
	res, err := h.twoFactor.setup(c.Request.Context(), actor(c), req.Password)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, twoFactorSetupResponse(res))
}

type codeRequest struct {
	Code string `json:"code"`
}

func (r *codeRequest) validate() error {
	var errs fieldErrors
	checkCode(&errs, "code", r.Code, true)
	return errs.err()
}

type messageResponse struct {
	Message string `json:"message"`
}

func (h *Handler) enableTwoFactor(c *gin.Context) {
	req := &codeRequest{}
	if !bind(c, req) {
		return
	}
	if err := h.twoFactor.enable(c.Request.Context(), actor(c), req.Code); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, messageResponse{Message: messageTwoFactorEnabled})
}

type disableTwoFactorRequest struct {
	Password   string `json:"password"`
	Code       string `json:"code"`
	BackupCode string `json:"backupCode"`
}

func (r *disableTwoFactorRequest) validate() error {
	var errs fieldErrors
	if r.Password == "" {
		errs.add("password", "required", "")
	}
	checkCode(&errs, "code", r.Code, false)
	checkCode(&errs, "backupCode", r.BackupCode, false)
	return errs.err()
}

func (h *Handler) disableTwoFactor(c *gin.Context) {
	req := &disableTwoFactorRequest{}
	if !bind(c, req) {
		return
	}
	if err := h.twoFactor.disable(c.Request.Context(), actor(c), req.Password, req.Code, req.BackupCode); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, messageResponse{Message: messageTwoFactorDisabled})
}

func (h *Handler) verifyTOTP(c *gin.Context) {
	h.verifySecondFactor(c, factorTOTP)
}

func (h *Handler) verifyBackupCode(c *gin.Context) {
	h.verifySecondFactor(c, factorBackupCode)
}

// verifySecondFactor completes a sign-in waiting in this browser. The two-factor cookie is dropped once the pending sign-in is gone: completed, spent by too many refused codes or by an identity that can no longer be linked, or no longer valid.
func (h *Handler) verifySecondFactor(c *gin.Context, factor secondFactor) {
	req := &codeRequest{}
	if !bind(c, req) {
		return
	}
	token, _ := c.Cookie(h.cookies.twoFactor)
	attr := readAttribution(c)
	sess, err := h.twoFactor.verifySignIn(c.Request.Context(), token, factor, req.Code, clientMetaFrom(c))
	if err != nil {
		if e, ok := apperr.As(err); ok && (e.Code == CodeTooManyAttempts || e.Code == CodeInvalidTwoFactorCookie || e.Code == CodeAccountNotLinked) {
			h.cookies.clear(c, h.cookies.twoFactor)
		}
		httpapi.Fail(c, err)
		return
	}
	h.cookies.setSession(c, sess)
	h.cookies.clear(c, h.cookies.twoFactor)
	h.cookies.clearAttribution(c, attr)
	c.JSON(http.StatusOK, signInResponse{Status: "signed_in"})
}
