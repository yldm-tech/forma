package org

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/responsemail"
)

// Keys of organizations.whitelabel, forma2's ZOrganizationWhitelabel.
const (
	whitelabelLogoURL    = "logoUrl"
	whitelabelFaviconURL = "faviconUrl"
)

// validStorageURL is forma2's ZStorageUrl: a relative storage path, or an http(s) URL. The scheme is constrained because these values are rendered into src and href attributes, where javascript: and data: URLs would be stored XSS.
func validStorageURL(s string) bool {
	if strings.HasPrefix(s, "/storage/") {
		return true
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	return scheme == "https" || scheme == "http"
}

// SetWhitelabelURL is forma2's updateOrganizationEmailLogoUrlAction and updateOrganizationFaviconUrlAction, and with a nil value their remove counterparts: organization.manage (owners and managers), the state-mutation rate limit on the organization, then the one key of organizations.whitelabel set, the others kept. Each emits forma2's updated/organization audit event carrying the new value, or the removed key as "".
func (s *Service) SetWhitelabelURL(ctx context.Context, userID, organizationID uuid.UUID, key string, value *string) error {
	err := s.setWhitelabelURL(ctx, userID, organizationID, key, value)
	var oldObject, newObject any
	if value != nil {
		newObject = map[string]string{key: *value}
	} else {
		oldObject = map[string]string{key: ""}
	}
	s.record(ctx, audit.ActionUpdated, audit.TargetOrganization, organizationID.String(), organizationID.String(), err, oldObject, newObject)
	return err
}

func (s *Service) setWhitelabelURL(ctx context.Context, userID, organizationID uuid.UUID, key string, value *string) error {
	if err := s.authz.Require(ctx, authz.User(userID), authz.OrganizationManage, authz.Organization(organizationID)); err != nil {
		return err
	}
	if err := s.limiter.Apply(ctx, ratelimit.ActionStateMutation, organizationID.String()); err != nil {
		return err
	}
	return s.repo.transaction(ctx, func(tx *Repository) error {
		exists, err := tx.lock(ctx, organizationID)
		if err != nil {
			return err
		}
		if !exists {
			return apperr.NotFound("Organization", organizationID.String())
		}
		return tx.setWhitelabel(ctx, organizationID, key, value)
	})
}

// SendTestEmail is forma2's sendTestEmailAction: an owner or manager (organization.manage) receives the email customization preview with the organization's logo, in their own locale. As in forma2 it is neither rate limited nor audited.
func (s *Service) SendTestEmail(ctx context.Context, userID, organizationID uuid.UUID) error {
	o, err := s.repo.byID(ctx, organizationID, s.cfg.IsFormaCloud)
	if err != nil {
		return err
	}
	if o == nil {
		return apperr.NotFound("Organization", organizationID.String())
	}
	if err := s.authz.Require(ctx, authz.User(userID), authz.OrganizationManage, authz.Organization(organizationID)); err != nil {
		return err
	}
	logo, err := s.repo.whitelabelLogo(ctx, organizationID)
	if err != nil {
		return err
	}
	u, err := s.repo.user(ctx, userID)
	if err != nil {
		return err
	}
	if u == nil {
		return apperr.NotFound("User", userID.String())
	}
	content := mailer.EmailCustomizationPreview{UserName: u.Name}
	if logo != "" {
		content.LogoURL = responsemail.ResolveStorageURL(responsemail.PublicBase(s.cfg), logo)
	}
	sent, err := s.mailer.SendTemplate(ctx, u.Email, u.Locale, content)
	if err != nil {
		return err
	}
	if !sent {
		return errMailNotSent()
	}
	return nil
}

// CodeEmailSendFailed is the auth and invite modules' code for a mail that did not go out.
const CodeEmailSendFailed = "email_send_failed"

// errMailNotSent refuses a test email the mailer skipped because no SMTP server is configured, so the page reports a failure instead of a delivery that never happened.
func errMailNotSent() error {
	return apperr.InvalidInput(CodeEmailSendFailed, "Failed to send email")
}

// setWhitelabel sets one key of the organization's whitelabel settings, null for a nil value, keeping the others.
func (r *Repository) setWhitelabel(ctx context.Context, organizationID uuid.UUID, key string, value *string) error {
	if err := r.db.WithContext(ctx).Exec(
		`UPDATE organizations SET whitelabel = whitelabel || jsonb_build_object(?::text, ?::text), updated_at = now() WHERE id = ?`,
		key, value, organizationID,
	).Error; err != nil {
		return fmt.Errorf("update organization whitelabel: %w", err)
	}
	return nil
}

// whitelabelLogo is the organization's email logo URL, "" without one.
func (r *Repository) whitelabelLogo(ctx context.Context, organizationID uuid.UUID) (string, error) {
	var logos []string
	if err := r.db.WithContext(ctx).Raw(
		`SELECT COALESCE(whitelabel ->> 'logoUrl', '') FROM organizations WHERE id = ?`, organizationID,
	).Scan(&logos).Error; err != nil {
		return "", fmt.Errorf("load organization logo: %w", err)
	}
	if len(logos) == 0 {
		return "", nil
	}
	return logos[0], nil
}

// recipient is the user a test email goes to.
type recipient struct {
	Name   string
	Email  string
	Locale string
}

// user returns nil when the user does not exist.
func (r *Repository) user(ctx context.Context, userID uuid.UUID) (*recipient, error) {
	var rows []recipient
	if err := r.db.WithContext(ctx).Raw(`SELECT name, email, locale FROM users WHERE id = ?`, userID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

type emailLogoRequest struct {
	LogoURL string `json:"logoUrl"`
}

func (r *emailLogoRequest) validate() error {
	if !validStorageURL(r.LogoURL) {
		return apperr.Validation([]apperr.FieldError{{Field: "logoUrl", Rule: "storageUrl"}})
	}
	return nil
}

type faviconRequest struct {
	FaviconURL string `json:"faviconUrl"`
}

func (r *faviconRequest) validate() error {
	if !validStorageURL(r.FaviconURL) {
		return apperr.Validation([]apperr.FieldError{{Field: "faviconUrl", Rule: "storageUrl"}})
	}
	return nil
}

func (h *Handler) setWhitelabelURL(c *gin.Context, key string, value *string) {
	id, ok := organizationID(c)
	if !ok {
		return
	}
	if err := h.svc.SetWhitelabelURL(c.Request.Context(), userID(c), id, key, value); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) setEmailLogo(c *gin.Context) {
	req := &emailLogoRequest{}
	if !bind(c, req) {
		return
	}
	h.setWhitelabelURL(c, whitelabelLogoURL, &req.LogoURL)
}

func (h *Handler) removeEmailLogo(c *gin.Context) {
	h.setWhitelabelURL(c, whitelabelLogoURL, nil)
}

func (h *Handler) setFavicon(c *gin.Context) {
	req := &faviconRequest{}
	if !bind(c, req) {
		return
	}
	h.setWhitelabelURL(c, whitelabelFaviconURL, &req.FaviconURL)
}

func (h *Handler) removeFavicon(c *gin.Context) {
	h.setWhitelabelURL(c, whitelabelFaviconURL, nil)
}

func (h *Handler) sendTestEmail(c *gin.Context) {
	id, ok := organizationID(c)
	if !ok {
		return
	}
	if err := h.svc.SendTestEmail(c.Request.Context(), userID(c), id); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
