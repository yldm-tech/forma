package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/webhook"

	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/apps/server/internal/platform/rowid"
)

// WebhookPath is where Stripe posts events, the endpoint registered in Stripe's dashboard for forma2.
const WebhookPath = "/api/billing/stripe-webhook"

// webhookBodyLimit is forma2's request body cap.
const webhookBodyLimit = 2 << 20

// webhookEvents are the event types forma2 subscribed to; any other is acknowledged and ignored.
var webhookEvents = map[string]bool{
	"checkout.session.completed":                      true,
	"customer.subscription.created":                   true,
	"customer.subscription.updated":                   true,
	"customer.subscription.deleted":                   true,
	"entitlements.active_entitlement_summary.updated": true,
	"subscription_schedule.created":                   true,
	"subscription_schedule.updated":                   true,
	"subscription_schedule.released":                  true,
	"subscription_schedule.canceled":                  true,
	"subscription_schedule.completed":                 true,
	"payment_intent.requires_action":                  true,
	"payment_intent.canceled":                         true,
}

// WebhookHandler is forma2's Stripe webhook route. Every handled event is processed before the answer: Stripe is the source of truth, so processing re-reads the organization's subscriptions from Stripe and writes the snapshot, and a failure answers 500 for Stripe to deliver the event again. Redeliveries and out-of-order events are absorbed by the snapshot's last event id and time.
type WebhookHandler struct {
	engine *Engine
	log    *slog.Logger
}

func NewWebhookHandler(engine *Engine, log *slog.Logger) *WebhookHandler {
	return &WebhookHandler{engine: engine, log: log}
}

// Register mounts the endpoint at its root path, outside /api/app: Stripe authenticates with the signature, not a session.
func (h *WebhookHandler) Register(r gin.IRouter) {
	r.POST(WebhookPath, h.receive)
}

func message(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"message": msg})
}

func received(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"received": true})
}

func (h *WebhookHandler) receive(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, webhookBodyLimit+1))
	if err != nil {
		h.log.ErrorContext(c.Request.Context(), "Unhandled error in Stripe webhook POST handler", "err", err)
		message(c, http.StatusInternalServerError, "Internal server error")
		return
	}
	if len(body) > webhookBodyLimit {
		message(c, http.StatusRequestEntityTooLarge, "Payload Too Large")
		return
	}
	signature := c.GetHeader("Stripe-Signature")
	if signature == "" {
		h.log.WarnContext(c.Request.Context(), "Stripe signature missing from request headers.")
		message(c, http.StatusBadRequest, "Stripe signature missing")
		return
	}
	if !h.engine.stripe.Configured() || h.engine.stripe.webhookSecret == "" {
		h.log.WarnContext(c.Request.Context(), "Stripe webhook skipped: Stripe is not configured")
		message(c, http.StatusServiceUnavailable, "Stripe webhook is not configured")
		return
	}
	// The endpoint's API version is whatever was chosen in Stripe's dashboard; forma2's SDK did not compare it, and the fields read here are the same in every version.
	event, err := webhook.ConstructEventWithOptions(body, signature, h.engine.stripe.webhookSecret, webhook.ConstructEventOptions{Tolerance: webhook.DefaultTolerance, IgnoreAPIVersionMismatch: true})
	if err != nil {
		h.log.ErrorContext(c.Request.Context(), "Error in Stripe webhook handler", "err", err)
		message(c, http.StatusBadRequest, "Webhook Error: "+err.Error())
		return
	}
	if !webhookEvents[string(event.Type)] {
		received(c)
		return
	}
	status, msg, err := h.process(c.Request.Context(), &event)
	if err != nil {
		h.log.ErrorContext(c.Request.Context(), "Failed to process Stripe webhook", "err", err, "eventId", event.ID, "eventType", string(event.Type))
		message(c, http.StatusInternalServerError, "Stripe webhook processing failed; please retry.")
		return
	}
	if status != http.StatusOK {
		h.log.ErrorContext(c.Request.Context(), "Webhook handler failed", "status", status, "message", msg)
		message(c, status, msg)
		return
	}
	received(c)
}

// eventObject is what the handler reads of an event's object, whichever type it is.
type eventObject struct {
	ID                 string          `json:"id"`
	Metadata           map[string]any  `json:"metadata"`
	Customer           json.RawMessage `json:"customer"`
	ClientReferenceID  *string         `json:"client_reference_id"`
	Mode               string          `json:"mode"`
	SetupIntent        json.RawMessage `json:"setup_intent"`
	CancellationReason string          `json:"cancellation_reason"`
}

// customerID is the object's customer when Stripe sent it as an id.
func (o *eventObject) customerID() string {
	var id string
	if json.Unmarshal(o.Customer, &id) == nil {
		return id
	}
	return ""
}

// expandableID is an expandable field's id, whether Stripe sent the id or the object.
func expandableID(raw json.RawMessage) string {
	var id string
	if json.Unmarshal(raw, &id) == nil {
		return id
	}
	var obj struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.ID
	}
	return ""
}

func (o *eventObject) metadata(key string) string {
	v, _ := o.Metadata[key].(string)
	return v
}

// process is forma2's webhookHandler after the signature check. It returns the status and message of an expected refusal, or an error for Stripe to retry.
func (h *WebhookHandler) process(ctx context.Context, event *stripe.Event) (int, string, error) {
	var obj eventObject
	if event.Data == nil || json.Unmarshal(event.Data.Raw, &obj) != nil {
		return 0, "", errors.New("decode the event's object")
	}
	ref := &eventRef{ID: event.ID, Created: event.Created}
	switch event.Type {
	case "payment_intent.requires_action":
		return http.StatusOK, "", h.paymentFailed(ctx, &obj, ref, PaymentErrorRequiresAction, "Payment requires additional authentication. Please complete verification or contact support.")
	case "payment_intent.canceled":
		// Only a cancellation for a failed invoice is a failure; every other reason is benign and must not raise a banner.
		if obj.CancellationReason != "failed_invoice" {
			return http.StatusOK, "", nil
		}
		return http.StatusOK, "", h.paymentFailed(ctx, &obj, ref, PaymentErrorFailedInvoice, "Payment was canceled. Please contact support to complete your upgrade.")
	}
	organizationID, err := h.organization(ctx, &obj)
	if err != nil {
		return 0, "", err
	}
	if organizationID == uuid.Nil {
		h.log.WarnContext(ctx, "Skipping Stripe webhook: organization not resolved", "eventType", string(event.Type), "eventId", event.ID)
		if event.Type == "checkout.session.completed" {
			return http.StatusInternalServerError, "Checkout completed but organization could not be resolved.", nil
		}
		return http.StatusOK, "", nil
	}
	if event.Type == "checkout.session.completed" {
		if err := h.setupCompleted(ctx, &obj); err != nil {
			return 0, "", err
		}
	}
	if err := h.engine.Reconcile(ctx, organizationID); err != nil {
		return 0, "", err
	}
	if _, err := h.engine.Sync(ctx, organizationID, ref); err != nil {
		return 0, "", err
	}
	return http.StatusOK, "", nil
}

// organization is resolveOrganizationId: the object's organizationId metadata, its client reference, else the organization holding its customer.
func (h *WebhookHandler) organization(ctx context.Context, obj *eventObject) (uuid.UUID, error) {
	if id, ok := rowid.Parse(obj.metadata("organizationId")); ok {
		return id, nil
	}
	if obj.ClientReferenceID != nil {
		if id, ok := rowid.Parse(*obj.ClientReferenceID); ok {
			return id, nil
		}
	}
	customer := obj.customerID()
	if customer == "" {
		return uuid.Nil, nil
	}
	return h.engine.repo.organizationByCustomer(ctx, customer)
}

// setupCompleted is forma2's handleSetupCheckoutCompleted: the card a setup checkout collected becomes the default of the customer and of the subscription it was for. The upgrade itself is not applied here, since its charge may need 3D Secure in the browser; the billing page drives it after the redirect.
func (h *WebhookHandler) setupCompleted(ctx context.Context, obj *eventObject) error {
	setupIntentID := expandableID(obj.SetupIntent)
	if obj.Mode != string(stripe.CheckoutSessionModeSetup) || setupIntentID == "" {
		return nil
	}
	intent, err := h.engine.api().V1SetupIntents.Retrieve(ctx, setupIntentID, nil)
	if err != nil {
		return fmt.Errorf("retrieve Stripe setup intent: %w", err)
	}
	if intent.PaymentMethod == nil || intent.PaymentMethod.ID == "" {
		h.log.WarnContext(ctx, "Setup checkout completed but no payment method found", "sessionId", obj.ID)
		return nil
	}
	return h.engine.attachPaymentMethod(ctx, expandableID(obj.Customer), obj.metadata("subscriptionId"), intent.PaymentMethod.ID)
}

// paymentFailed is forma2's handlePaymentIntentRequiresAction and handlePaymentIntentCanceled: the payment-failure banner of the organization holding the PaymentIntent's customer. No subscription sync follows.
func (h *WebhookHandler) paymentFailed(ctx context.Context, obj *eventObject, ref *eventRef, kind, text string) error {
	customer := obj.customerID()
	if customer == "" {
		return nil
	}
	organizationID, err := h.engine.repo.organizationByCustomer(ctx, customer)
	if err != nil || organizationID == uuid.Nil {
		return err
	}
	if err := h.engine.SetPaymentAttemptError(ctx, organizationID, &PaymentAttemptError{Type: kind, PaymentIntentID: obj.ID, Message: text, CreatedAt: jsvalue.ISOString(h.engine.now())}, ref); err != nil {
		return err
	}
	h.log.InfoContext(ctx, "Payment intent failure recorded for organization", "paymentIntentId", obj.ID, "organizationId", organizationID.String(), "type", kind)
	return nil
}
