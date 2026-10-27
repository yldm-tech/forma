// Package plain is forma2's Plain in-product support chat (apps/web/app/plain): the signed-in user's identity for the chat widget, with the email hash Plain verifies it by and the label that marks a paying customer's threads.
package plain

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
)

// Handler serves GET /api/app/plain/session, what the app's PlainChat widget is initialized with.
type Handler struct {
	cfg      config.Config
	db       *gorm.DB
	sessions httpapi.SessionAuth
}

func NewHandler(cfg config.Config, db *gorm.DB, sessions httpapi.SessionAuth) *Handler {
	return &Handler{cfg: cfg, db: db, sessions: sessions}
}

// Register mounts the route on the /api/app group.
func (h *Handler) Register(r gin.IRouter) {
	r.GET("/plain/session", h.sessions.Require(), h.session)
}

// Session is forma2's PlainChat props: the widget's app id and the customer it identifies.
type Session struct {
	AppID     string  `json:"appId"`
	UserID    string  `json:"userId"`
	Email     string  `json:"email"`
	Name      string  `json:"name"`
	EmailHash *string `json:"emailHash"`
	// ActiveCustomerLabelTypeID is applied to every thread the user opens, so it is resolved before the widget starts.
	ActiveCustomerLabelTypeID *string `json:"activeCustomerLabelTypeId"`
}

// session answers 204 with no body when the chat is not configured, so the app never loads the widget. A chat that is switched off is a normal state, not an error, and an error status here made the browser log a failed request on every page of an instance without Plain.
func (h *Handler) session(c *gin.Context) {
	if !h.cfg.Plain.Configured() {
		c.Header("Cache-Control", "no-store")
		c.Status(http.StatusNoContent)
		return
	}
	actor, _ := httpapi.ActorFrom(c.Request.Context())
	s, err := h.build(c.Request.Context(), actor.UserID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, s)
}

func (h *Handler) build(ctx context.Context, userID uuid.UUID) (*Session, error) {
	var users []struct {
		Name  string
		Email string
	}
	if err := h.db.WithContext(ctx).Raw(`SELECT name, email FROM users WHERE id = ?`, userID).Scan(&users).Error; err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	if len(users) == 0 {
		return nil, apperr.NotFound("User", userID.String())
	}
	s := &Session{AppID: h.cfg.Plain.AppID, UserID: userID.String(), Email: users[0].Email, Name: users[0].Name, EmailHash: EmailHash(string(h.cfg.Plain.ChatHMACSecret), users[0].Email)}
	if label := h.cfg.Plain.ActiveCustomerLabelTypeID; label != "" {
		active, err := h.activeCustomer(ctx, userID)
		if err != nil {
			return nil, err
		}
		if active {
			s.ActiveCustomerLabelTypeID = &label
		}
	}
	return s, nil
}

// EmailHash is forma2's computePlainEmailHash: the hex HMAC-SHA256 of the email under the chat's secret, by which Plain verifies the customer's identity; nil without a secret, when the chat runs unverified. The hash is a bearer credential, so it is only ever computed here.
func EmailHash(secret, email string) *string {
	if secret == "" {
		return nil
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(email))
	sum := hex.EncodeToString(mac.Sum(nil))
	return &sum
}

// activeCustomer is forma2's getIsActiveCustomer: whether the user belongs to an organization on a paid plan (Pro or Scale; Custom is not counted) whose subscription is active or trialing. Stripe plans exist only on Forma Cloud, so elsewhere it is false without a query.
func (h *Handler) activeCustomer(ctx context.Context, userID uuid.UUID) (bool, error) {
	if !h.cfg.IsFormaCloud {
		return false, nil
	}
	var active bool
	err := h.db.WithContext(ctx).Raw(`SELECT EXISTS (
		SELECT 1 FROM memberships m JOIN organization_billing b ON b.organization_id = m.organization_id
		WHERE m.user_id = ? AND b.stripe->>'plan' IN ('pro', 'scale') AND b.stripe->>'subscriptionStatus' IN ('active', 'trialing'))`, userID).Row().Scan(&active)
	if err != nil {
		return false, fmt.Errorf("read active customer: %w", err)
	}
	return active, nil
}
