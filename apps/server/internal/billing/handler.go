package billing

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
)

// Handler serves the billing routes of /api/app: the billing settings page, the confirmation page after Checkout, the onboarding plan step, and the summary the layouts' banners read. Each route is one of forma2's billing server actions.
type Handler struct {
	svc      *Service
	sessions httpapi.SessionAuth
}

func NewHandler(svc *Service, sessions httpapi.SessionAuth) *Handler {
	return &Handler{svc: svc, sessions: sessions}
}

// Register mounts the routes on the /api/app group, which already applies the error envelope and the CSRF check to every mutation.
func (h *Handler) Register(r gin.IRouter) {
	g := r.Group("/organizations/:organizationId/billing", h.sessions.Require())
	g.GET("", h.page)
	g.GET("/summary", h.summary)
	g.GET("/upgrade-preview", h.previewUpgrade)
	g.POST("/portal", h.portal)
	g.POST("/checkout", h.checkout)
	g.POST("/setup-checkout", h.setupCheckout)
	g.POST("/setup-checkout/finalize", h.finalizeSetupCheckout)
	g.POST("/plan", h.changePlan)
	g.DELETE("/pending-change", h.undoPendingChange)
	g.POST("/payment-issue", h.reportPaymentIssue)
	g.POST("/retry-setup", h.retrySetup)
	g.POST("/hobby", h.startHobby)
	g.POST("/trial", h.startProTrial)
	g.POST("/wait/plan", h.waitForPlan)
	g.POST("/wait/payment-method", h.waitForPaymentMethod)
}

// ids reads the caller and the :organizationId path parameter, failing the request when the id is malformed.
func ids(c *gin.Context) (userID, organizationID uuid.UUID, ok bool) {
	organizationID, err := httpapi.ParamUUID(c, "organizationId")
	if err != nil {
		httpapi.Fail(c, err)
		return uuid.Nil, uuid.Nil, false
	}
	a, _ := httpapi.ActorFrom(c.Request.Context())
	return a.UserID, organizationID, true
}

func respond(c *gin.Context, v any, err error) {
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

type urlResponse struct {
	URL string `json:"url"`
}

type successResponse struct {
	Success bool `json:"success"`
}

func (h *Handler) page(c *gin.Context) {
	user, org, ok := ids(c)
	if !ok {
		return
	}
	p, err := h.svc.Page(c.Request.Context(), user, org)
	respond(c, p, err)
}

func (h *Handler) summary(c *gin.Context) {
	user, org, ok := ids(c)
	if !ok {
		return
	}
	s, err := h.svc.Summary(c.Request.Context(), user, org)
	respond(c, s, err)
}

// paidTarget is forma2's { targetPlan: pro | scale, targetInterval }.
type paidTarget struct {
	TargetPlan     string `json:"targetPlan" form:"targetPlan" binding:"required,oneof=pro scale"`
	TargetInterval string `json:"targetInterval" form:"targetInterval" binding:"required,oneof=monthly yearly"`
}

func (h *Handler) previewUpgrade(c *gin.Context) {
	user, org, ok := ids(c)
	if !ok {
		return
	}
	var q paidTarget
	if err := httpapi.BindQuery(c, &q); err != nil {
		httpapi.Fail(c, err)
		return
	}
	preview, err := h.svc.PreviewUpgrade(c.Request.Context(), user, org, q.TargetPlan, q.TargetInterval)
	respond(c, gin.H{"preview": preview}, err)
}

func (h *Handler) portal(c *gin.Context) {
	user, org, ok := ids(c)
	if !ok {
		return
	}
	url, err := h.svc.PortalURL(c.Request.Context(), user, org)
	respond(c, urlResponse{URL: url}, err)
}

func (h *Handler) checkout(c *gin.Context) {
	user, org, ok := ids(c)
	if !ok {
		return
	}
	var req paidTarget
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	url, err := h.svc.CheckoutURL(c.Request.Context(), user, org, req.TargetPlan, req.TargetInterval)
	respond(c, urlResponse{URL: url}, err)
}

type setupCheckoutRequest struct {
	TargetPlan     *string `json:"targetPlan" binding:"omitempty,oneof=pro scale"`
	TargetInterval *string `json:"targetInterval" binding:"omitempty,oneof=monthly yearly"`
}

func (h *Handler) setupCheckout(c *gin.Context) {
	user, org, ok := ids(c)
	if !ok {
		return
	}
	var req setupCheckoutRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	var intent *UpgradeIntent
	if req.TargetPlan != nil {
		intent = &UpgradeIntent{Plan: *req.TargetPlan, Interval: IntervalMonthly}
		if req.TargetInterval != nil {
			intent.Interval = *req.TargetInterval
		}
	}
	url, err := h.svc.SetupCheckoutURL(c.Request.Context(), user, org, intent)
	respond(c, urlResponse{URL: url}, err)
}

type finalizeRequest struct {
	CheckoutSessionID string `json:"checkoutSessionId" binding:"required"`
}

func (h *Handler) finalizeSetupCheckout(c *gin.Context) {
	user, org, ok := ids(c)
	if !ok {
		return
	}
	var req finalizeRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	result, err := h.svc.FinalizeSetupCheckout(c.Request.Context(), user, org, req.CheckoutSessionID)
	respond(c, result, err)
}

// planRequest is forma2's ZChangeBillingPlanAction: Hobby is monthly only.
type planRequest struct {
	TargetPlan     string `json:"targetPlan" binding:"required,oneof=hobby pro scale"`
	TargetInterval string `json:"targetInterval" binding:"required,oneof=monthly yearly"`
}

func (h *Handler) changePlan(c *gin.Context) {
	user, org, ok := ids(c)
	if !ok {
		return
	}
	var req planRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	if req.TargetPlan == PlanHobby && req.TargetInterval != IntervalMonthly {
		httpapi.Fail(c, apperr.Validation([]apperr.FieldError{{Field: "targetInterval", Rule: "eq", Param: IntervalMonthly}}))
		return
	}
	change, err := h.svc.ChangePlan(c.Request.Context(), user, org, req.TargetPlan, req.TargetInterval)
	respond(c, change, err)
}

func (h *Handler) undoPendingChange(c *gin.Context) {
	user, org, ok := ids(c)
	if !ok {
		return
	}
	respond(c, successResponse{Success: true}, h.svc.UndoPendingChange(c.Request.Context(), user, org))
}

type paymentIssueRequest struct {
	PaymentIntentID string `json:"paymentIntentId" binding:"required"`
}

func (h *Handler) reportPaymentIssue(c *gin.Context) {
	user, org, ok := ids(c)
	if !ok {
		return
	}
	var req paymentIssueRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	respond(c, successResponse{Success: true}, h.svc.ReportPaymentIssue(c.Request.Context(), user, org, req.PaymentIntentID))
}

func (h *Handler) retrySetup(c *gin.Context) {
	user, org, ok := ids(c)
	if !ok {
		return
	}
	respond(c, successResponse{Success: true}, h.svc.RetrySetup(c.Request.Context(), user, org))
}

func (h *Handler) startHobby(c *gin.Context) {
	user, org, ok := ids(c)
	if !ok {
		return
	}
	respond(c, successResponse{Success: true}, h.svc.StartHobby(c.Request.Context(), user, org))
}

func (h *Handler) startProTrial(c *gin.Context) {
	user, org, ok := ids(c)
	if !ok {
		return
	}
	respond(c, successResponse{Success: true}, h.svc.StartProTrial(c.Request.Context(), user, org))
}

type waitPlanRequest struct {
	TargetPlan string `json:"targetPlan" binding:"required,oneof=hobby pro scale"`
}

func (h *Handler) waitForPlan(c *gin.Context) {
	user, org, ok := ids(c)
	if !ok {
		return
	}
	var req waitPlanRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	plan, err := h.svc.WaitForPlan(c.Request.Context(), user, org, req.TargetPlan)
	respond(c, gin.H{"plan": plan}, err)
}

func (h *Handler) waitForPaymentMethod(c *gin.Context) {
	user, org, ok := ids(c)
	if !ok {
		return
	}
	hasCard, err := h.svc.WaitForPaymentMethod(c.Request.Context(), user, org)
	respond(c, gin.H{"hasPaymentMethod": hasCard}, err)
}
