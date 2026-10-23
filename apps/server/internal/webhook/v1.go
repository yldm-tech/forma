package webhook

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/apikey"
	"github.com/yldm-tech/forma/apps/server/internal/mgmtapi/mgmtv1"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/v1api"
)

// V1Handler serves /api/v1/webhooks, the subscription API Zapier, Make, n8n and Activepieces call with an API key: forma2's paths, envelopes and status codes, an authorization failure included (401 unauthorized, not 403).
type V1Handler struct {
	svc *Service
	v1  *mgmtv1.Wrapper
	log *slog.Logger
}

func NewV1Handler(svc *Service, v1 *mgmtv1.Wrapper, log *slog.Logger) *V1Handler {
	return &V1Handler{svc: svc, v1: v1, log: log}
}

// Register mounts the routes outside /api/app: they answer with forma2's v1 envelope and accept an API key only, charged to forma2's api:v1 limit.
func (h *V1Handler) Register(r gin.IRouter) {
	g := r.Group("/api/v1/webhooks", mgmtv1.Front(h.log), h.v1.APIKey(apikey.Options{}, ratelimit.APIV1))
	g.GET("", h.list)
	g.POST("", h.v1.Audited(audit.ActionCreated, audit.TargetWebhook, h.create))
	g.GET("/:webhookId", h.get)
	g.DELETE("/:webhookId", h.v1.Audited(audit.ActionDeleted, audit.TargetWebhook, h.delete))
}

// withEnvironmentIDs is forma2's addLegacyEnvironmentIdToList: each webhook with environmentId, its workspace's legacy environment id or the workspace's own id.
func (h *V1Handler) withEnvironmentIDs(ctx context.Context, webhooks []*Webhook, withSecret bool) ([]View, error) {
	ids := []uuid.UUID{}
	for _, w := range webhooks {
		if !slices.Contains(ids, w.WorkspaceID) {
			ids = append(ids, w.WorkspaceID)
		}
	}
	envs, err := h.svc.repo.environmentIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]View, len(webhooks))
	for i, w := range webhooks {
		out[i] = w.view()
		if withSecret {
			out[i] = w.viewWithSecret()
		}
		env, ok := envs[w.WorkspaceID]
		if !ok {
			env = w.WorkspaceID.String()
		}
		out[i].EnvironmentID = &env
	}
	return out, nil
}

// bestEffort is forma2's addLegacyEnvironmentIdBestEffort, for an answer to a write that has already committed: a failed lookup leaves environmentId out rather than turning the write into an error the caller would retry.
func (h *V1Handler) bestEffort(c *gin.Context, w *Webhook, withSecret bool) View {
	views, err := h.withEnvironmentIDs(c.Request.Context(), []*Webhook{w}, withSecret)
	if err != nil {
		h.log.ErrorContext(c.Request.Context(), "Failed to resolve legacy environmentId for a committed write", "err", err, "workspace_id", w.WorkspaceID)
		if withSecret {
			return w.viewWithSecret()
		}
		return w.view()
	}
	return views[0]
}

// list is forma2's GET: every webhook of every workspace the key has a grant on, without secrets.
func (h *V1Handler) list(c *gin.Context) {
	ctx := c.Request.Context()
	p := mgmtv1.PrincipalOf(c)
	ids := []uuid.UUID{}
	for _, g := range p.WorkspacePermissions {
		if !slices.Contains(ids, g.WorkspaceID) {
			ids = append(ids, g.WorkspaceID)
		}
	}
	found, err := h.svc.repo.OfWorkspaces(ctx, ids)
	if err != nil {
		h.v1.Unexpected(c, err, mgmtv1.MessageGeneric)
		return
	}
	views, err := h.withEnvironmentIDs(ctx, found, false)
	if err != nil {
		h.v1.Unexpected(c, err, mgmtv1.MessageGeneric)
		return
	}
	v1api.Success(c, views)
}

// byPathID loads the webhook the path names, by its id or the forma2 id it was migrated from. It answers 404 itself and returns nil when there is none.
func (h *V1Handler) byPathID(c *gin.Context) *Webhook {
	raw := c.Param("webhookId")
	id, _ := legacyid.Resolve(raw)
	w, err := h.svc.repo.ByID(c.Request.Context(), id)
	if err != nil {
		h.v1.Unexpected(c, err, mgmtv1.MessageUnexpected)
		return nil
	}
	if w == nil {
		v1api.NotFound(c, "Webhook", raw, false)
		return nil
	}
	return w
}

// get is forma2's GET /{webhookId}: 404 for an unknown id, then workspace.read on the webhook's workspace (401 when refused). The secret is never returned.
func (h *V1Handler) get(c *gin.Context) {
	w := h.byPathID(c)
	if w == nil {
		return
	}
	if !h.v1.Authorize(c, http.MethodGet, w.WorkspaceID) {
		return
	}
	views, err := h.withEnvironmentIDs(c.Request.Context(), []*Webhook{w}, false)
	if err != nil {
		h.v1.Unexpected(c, err, mgmtv1.MessageUnexpected)
		return
	}
	v1api.Success(c, views[0])
}

// delete is forma2's DELETE /{webhookId}: 404 for an unknown id, then workspace.manage (401 when refused); a failed delete is answered 404 and audited as a failure.
func (h *V1Handler) delete(c *gin.Context) {
	rec := mgmtv1.AuditOf(c)
	rec.TargetID = c.Param("webhookId")
	w := h.byPathID(c)
	if w == nil {
		return
	}
	if !h.v1.Authorize(c, http.MethodDelete, w.WorkspaceID) {
		return
	}
	rec.OldObject = w.view()
	deleted, err := h.svc.repo.delete(c.Request.Context(), w.ID)
	if err != nil || !deleted {
		if err != nil {
			h.log.ErrorContext(c.Request.Context(), "Error deleting webhook", "err", err)
		}
		rec.Failed = true
		v1api.NotFound(c, "Webhook", c.Param("webhookId"), false)
		return
	}
	v1api.Success(c, h.bestEffort(c, w, false))
}

// create is forma2's POST: the body read with its 2 MiB limit, the workspace resolved from workspaceId or the legacy environmentId and authorized for workspace.write, the input validated, the URL's SSRF check, then the webhook with a new secret. The answer is the only one that carries the secret.
func (h *V1Handler) create(c *gin.Context) {
	ctx := c.Request.Context()
	body, ok := mgmtv1.ReadBody(c)
	if !ok {
		return
	}
	ws, ok := h.v1.ResolveWorkspace(c, body, http.MethodPost)
	if !ok {
		return
	}
	in, details := parseV1Input(body)
	if details != nil {
		v1api.BadRequest(c, "Fields are missing or incorrectly formatted", details, true)
		return
	}
	w, err := h.svc.createV1(ctx, ws.ID, in)
	if err != nil {
		if e, ok := apperr.As(err); ok && e.Status == http.StatusBadRequest {
			v1api.BadRequest(c, e.Message, nil, false)
			return
		}
		h.v1.Unexpected(c, err, mgmtv1.MessageGeneric)
		return
	}
	rec := mgmtv1.AuditOf(c)
	rec.TargetID, rec.NewObject = w.ID.String(), w.auditObject()
	v1api.Success(c, h.bestEffort(c, w, true))
}

// createV1 is the v1 API's createWebhook: the URL's SSRF check and a new secret. Unlike the settings page it does not refuse Discord, as forma2's did not.
func (s *Service) createV1(ctx context.Context, workspaceID uuid.UUID, in Input) (*Webhook, error) {
	f, err := s.fields(ctx, workspaceID, in, nil)
	if err != nil {
		return nil, err
	}
	if _, err := s.guard.Validate(ctx, f.URL); err != nil {
		return nil, err
	}
	id, err := s.repo.create(ctx, workspaceID, f, GenerateSecret())
	if err != nil {
		return nil, err
	}
	return s.repo.ByID(ctx, id)
}

// parseV1Input is forma2's ZWebhookInput.safeParse with its error details, path to message: name (string or null) and source optional, surveyIds an optional list of ids, triggers a required non-empty list of the three events, url a required URL. Other members are ignored.
func parseV1Input(body map[string]any) (Input, map[string]any) {
	var in Input
	details := map[string]any{}
	if v, present := body["name"]; present {
		switch x := v.(type) {
		case nil:
		case string:
			in.Name = &x
		default:
			details["name"] = "Invalid input: expected string, received " + v1api.JSType(v, true)
		}
	}
	if v, present := body["source"]; present {
		s, ok := v.(string)
		if !ok || !slices.Contains(Sources, s) {
			details["source"] = "Invalid option: expected one of " + v1api.QuotedOptions(Sources)
		}
		in.Source = s
	}
	if v, present := body["surveyIds"]; present {
		list, ok := v.([]any)
		if !ok {
			details["surveyIds"] = "Invalid input: expected array, received " + v1api.JSType(v, true)
		}
		for i, e := range list {
			s, ok := e.(string)
			if !ok {
				details[fmt.Sprintf("surveyIds.%d", i)] = "Invalid input: expected string, received " + v1api.JSType(e, true)
				continue
			}
			if !v1api.IsID(s) {
				details[fmt.Sprintf("surveyIds.%d", i)] = "Invalid cuid2"
				continue
			}
			in.SurveyIDs = append(in.SurveyIDs, s)
		}
	}
	v, present := body["triggers"]
	list, ok := v.([]any)
	switch {
	case !ok:
		details["triggers"] = "Invalid input: expected array, received " + v1api.JSType(v, present)
	case len(list) == 0:
		details["triggers"] = "At least one trigger is required"
	}
	for i, e := range list {
		s, ok := e.(string)
		if !ok || !slices.Contains(Triggers, s) {
			details[fmt.Sprintf("triggers.%d", i)] = "Invalid option: expected one of " + v1api.QuotedOptions(Triggers)
			continue
		}
		in.Triggers = append(in.Triggers, s)
	}
	v, present = body["url"]
	s, ok := v.(string)
	switch {
	case !ok:
		details["url"] = "Invalid input: expected string, received " + v1api.JSType(v, present)
	case !v1api.IsURL(s):
		details["url"] = "Invalid URL"
	}
	in.URL = s
	if len(details) > 0 {
		return in, details
	}
	return in, nil
}
