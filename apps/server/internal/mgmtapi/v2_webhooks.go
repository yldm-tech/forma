package mgmtapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/apps/server/internal/platform/v1api"
	"github.com/yldm-tech/forma/apps/server/internal/webhook"
)

// v2ListWebhooks is forma2's GET /api/v2/management/webhooks: the common filter over the webhooks of the workspaces the key may read, never with their secrets. surveyIds is declared an array, which a query string never is, so naming it always fails.
func (h *Handler) v2ListWebhooks(c *gin.Context) {
	q := v2Query(c)
	var is issues
	f := is.commonFilter(q)
	if v := q.get("surveyIds"); !isAbsent(v) {
		is.typeError("surveyIds", "array", v)
	}
	is.dateRange(f)
	if is.failed() {
		v2Error(c, http.StatusUnprocessableEntity, is.v2Details())
		return
	}
	if !h.v2Limit(c) {
		return
	}
	ids, err := h.authorizedWorkspaces(c)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	found, total, err := h.webhooks.ListManaged(c.Request.Context(), webhook.ManagedQuery{
		WorkspaceIDs: ids, DateField: f.DateField, Start: f.Start, End: f.End, OrderBy: f.orderColumn(), Ascending: f.Ascending, Limit: f.Limit, Offset: f.Skip,
	})
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	out := make([]webhook.View, len(found))
	for i, w := range found {
		out[i] = w.View()
	}
	v2OK(c, out, f.meta(total))
}

// parseV2WebhookInput is ZWebhook's writable members in its declared order: name (nullable), url, source, triggers (at least one) and surveyIds, all required.
func parseV2WebhookInput(body map[string]any) (webhook.Input, *issues) {
	o := object(body)
	var is issues
	var in webhook.Input
	in.Name = is.optString("name", o.get("name"), nullable)
	if url, ok := is.str("url", o.get("url"), required); ok {
		if !v1api.IsURL(url) {
			is.add("url", "Invalid URL")
		}
		in.URL = url
	}
	in.Source, _ = is.enum("source", o.get("source"), required, webhook.Sources)
	v := o.get("triggers")
	if list, ok := v.([]any); ok {
		if len(list) == 0 {
			is.add("triggers", "At least one trigger is required")
		}
		for i, t := range list {
			if s, ok := is.enum(index("triggers", i), t, required, webhook.Triggers); ok {
				in.Triggers = append(in.Triggers, s)
			}
		}
	} else {
		is.typeError("triggers", "array", v)
	}
	v = o.get("surveyIds")
	if list, ok := v.([]any); ok {
		in.SurveyIDs = []string{}
		for i, s := range list {
			if id, ok := is.id(index("surveyIds", i), s, required); ok {
				in.SurveyIDs = append(in.SurveyIDs, id)
			}
		}
	} else {
		is.typeError("surveyIds", "array", v)
	}
	return in, &is
}

// surveysWorkspace is forma2's getWorkspaceIdFromSurveyIds: the one workspace the surveys are in, nil for none. An unknown survey is 404 survey, surveys of several workspaces 400 surveyIds.
func (h *Handler) surveysWorkspace(ctx context.Context, raw []string) (*uuid.UUID, []v2Detail, int, error) {
	if len(raw) == 0 {
		return nil, nil, 0, nil
	}
	ids := make([]uuid.UUID, 0, len(raw))
	for _, r := range raw {
		id, _ := legacyid.Resolve(r)
		ids = append(ids, id)
	}
	var rows []struct {
		ID          uuid.UUID
		WorkspaceID uuid.UUID
	}
	if err := h.db.WithContext(ctx).Raw(`SELECT id, workspace_id FROM surveys WHERE id = ANY(?::uuid[])`, uuidArray(ids)).Scan(&rows).Error; err != nil {
		return nil, nil, 0, fmt.Errorf("load survey workspaces: %w", err)
	}
	distinct := map[uuid.UUID]bool{}
	for _, id := range ids {
		distinct[id] = true
	}
	if len(rows) != len(distinct) {
		return nil, detail("survey", "not found"), http.StatusNotFound, nil
	}
	workspace := rows[0].WorkspaceID
	for _, row := range rows {
		if row.WorkspaceID != workspace {
			return nil, detail("surveyIds", "not all surveys are in the same workspace"), http.StatusBadRequest, nil
		}
	}
	return &workspace, nil, 0, nil
}

// v2WebhookWriteError answers a failed webhook write: the URL check's refusal is 400 on url.
func (h *Handler) v2WebhookWriteError(c *gin.Context, err error) {
	if e, ok := apperr.As(err); ok && e.Status == http.StatusBadRequest {
		v2Error(c, http.StatusBadRequest, detail("url", e.Message))
		return
	}
	if e, ok := apperr.As(err); ok && e.Status == http.StatusNotFound {
		v2Error(c, http.StatusNotFound, detail("webhook", "not found"))
		return
	}
	h.v2Unexpected(c, err)
}

// v2CreateWebhook is forma2's POST /api/v2/management/webhooks: the workspace resolved and authorized for write (403), the input, the surveys all of that workspace, the URL's SSRF check, then the webhook with a new secret, which this answer alone carries.
func (h *Handler) v2CreateWebhook(c *gin.Context) {
	body, ok := v2Body(c)
	if !ok {
		return
	}
	workspaceID, ok := h.v2ResolveWorkspace(c, body, http.MethodPost)
	if !ok {
		return
	}
	in, is := parseV2WebhookInput(body)
	is.id("workspaceId", object(body).get("workspaceId"), required)
	if is.failed() {
		v2Error(c, http.StatusUnprocessableEntity, is.v2Details())
		return
	}
	if !h.v2Limit(c) {
		return
	}
	ctx := c.Request.Context()
	surveysWorkspace, failure, status, err := h.surveysWorkspace(ctx, in.SurveyIDs)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	if failure != nil {
		v2Error(c, status, failure)
		return
	}
	if surveysWorkspace != nil && *surveysWorkspace != workspaceID {
		v2Error(c, http.StatusBadRequest, detail("surveyIds", "webhook workspace does not match the surveys workspace"))
		return
	}
	created, err := h.webhooks.CreateManaged(ctx, workspaceID, in)
	if err != nil {
		h.v2WebhookWriteError(c, err)
		return
	}
	h.v2Record(c, created.ID.String(), nil, created.AuditObject())
	v2Created(c, created.ViewWithSecret())
}

// v2Webhook loads the webhook id names and authorizes method on its workspace, answering itself when it cannot: 404 webhook, 401 when refused.
func (h *Handler) v2Webhook(c *gin.Context, id uuid.UUID, method string) *webhook.Webhook {
	found, err := h.webhooks.ByID(c.Request.Context(), id)
	if err != nil {
		h.v2Unexpected(c, err)
		return nil
	}
	if found == nil {
		v2Error(c, http.StatusNotFound, detail("webhook", "not found"))
		return nil
	}
	if !h.v2Authorize(c, method, found.WorkspaceID) {
		return nil
	}
	return found
}

func (h *Handler) v2GetWebhook(c *gin.Context) {
	id, ok := v2PathID(c, "webhookId")
	if !ok || !h.v2Limit(c) {
		return
	}
	found := h.v2Webhook(c, id, http.MethodGet)
	if found == nil {
		return
	}
	v2OK(c, found.View(), nil)
}

// v2UpdateWebhook is forma2's PUT /webhooks/{id}: every writable field, the surveys' workspace looked up first, then the webhook (404), workspace.write on it (401), the surveys in its workspace (400), and the update.
func (h *Handler) v2UpdateWebhook(c *gin.Context) {
	body, ok := v2Body(c)
	if !ok {
		return
	}
	in, is := parseV2WebhookInput(body)
	if is.failed() {
		v2Error(c, http.StatusUnprocessableEntity, is.v2Details())
		return
	}
	id, ok := v2PathID(c, "webhookId")
	if !ok || !h.v2Limit(c) {
		return
	}
	h.v2Record(c, c.Param("webhookId"), nil, nil)
	ctx := c.Request.Context()
	surveysWorkspace, failure, status, err := h.surveysWorkspace(ctx, in.SurveyIDs)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	if failure != nil {
		v2Error(c, status, failure)
		return
	}
	found := h.v2Webhook(c, id, http.MethodPut)
	if found == nil {
		return
	}
	if surveysWorkspace != nil && *surveysWorkspace != found.WorkspaceID {
		v2Error(c, http.StatusBadRequest, detail("surveyIds", "webhook workspace does not match the surveys workspace"))
		return
	}
	updated, err := h.webhooks.ReplaceManaged(ctx, found.ID, in)
	if err != nil {
		h.v2WebhookWriteError(c, err)
		return
	}
	h.v2Record(c, "", found.AuditObject(), updated.AuditObject())
	v2OK(c, updated.View(), nil)
}

// v2DeleteWebhook is forma2's DELETE /webhooks/{id}: workspace.manage, then the webhook, answered without its secret.
func (h *Handler) v2DeleteWebhook(c *gin.Context) {
	id, ok := v2PathID(c, "webhookId")
	if !ok || !h.v2Limit(c) {
		return
	}
	found := h.v2Webhook(c, id, http.MethodDelete)
	if found == nil {
		return
	}
	h.v2Record(c, found.ID.String(), found.AuditObject(), nil)
	deleted, err := h.webhooks.DeleteManaged(c.Request.Context(), found.ID)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	if deleted == nil {
		v2Error(c, http.StatusNotFound, detail("webhook", "not found"))
		return
	}
	v2OK(c, deleted.View(), nil)
}
