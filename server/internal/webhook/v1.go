package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/apikey"
	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/v1api"
	"github.com/yldm-tech/forma/server/internal/workspace"
)

// cuid2 is zod's cuid2 shape, every forma2 id.
var cuid2 = regexp.MustCompile(`^[0-9a-z]+$`)

// maxBodyBytes is forma2's DEFAULT_REQUEST_BODY_LIMIT_BYTES.
const maxBodyBytes = 2 * 1024 * 1024

// Messages of the v1 API's unexpected failures: an error escaping the handler (forma2's wrapper) and one its handleApiError caught.
const (
	messageUnexpected = "An unexpected error occurred."
	messageGeneric    = "Something went wrong. Please try again."
)

// V1Handler serves /api/v1/webhooks, the subscription API Zapier, Make, n8n and Activepieces call with an API key: forma2's paths, envelopes and status codes, an authorization failure included (401 unauthorized, not 403).
type V1Handler struct {
	cfg        config.Config
	svc        *Service
	keys       *apikey.Authenticator
	workspaces *workspace.Repository
	log        *slog.Logger
}

func NewV1Handler(cfg config.Config, svc *Service, keys *apikey.Authenticator, workspaces *workspace.Repository, log *slog.Logger) *V1Handler {
	return &V1Handler{cfg: cfg, svc: svc, keys: keys, workspaces: workspaces, log: log}
}

// Register mounts the routes outside /api/app: they answer with forma2's v1 envelope and accept an API key only.
func (h *V1Handler) Register(r gin.IRouter) {
	g := r.Group("/api/v1/webhooks", h.authenticate)
	g.GET("", h.list)
	g.POST("", h.audited(audit.ActionCreated, h.create))
	g.GET("/:webhookId", h.get)
	g.DELETE("/:webhookId", h.audited(audit.ActionDeleted, h.delete))
}

// Context keys of the authenticated key and the audit record.
const (
	principalKey = "webhook.v1.principal"
	auditKey     = "webhook.v1.audit"
)

func principal(c *gin.Context) *apikey.Principal {
	v, _ := c.Get(principalKey)
	p, _ := v.(*apikey.Principal)
	return p
}

// authenticate is the front of forma2's withV1ApiWrapper for a management route: an API key (a session is never read), then api:v1, 100 requests a minute per key.
func (h *V1Handler) authenticate(c *gin.Context) {
	ctx := c.Request.Context()
	p, err := h.keys.Authenticate(ctx, c.Request.Header, apikey.Options{})
	if err != nil {
		h.unexpected(c, err, messageUnexpected)
		return
	}
	if p == nil {
		v1api.NotAuthenticated(c)
		return
	}
	c.Set(principalKey, p)
	if err := h.svc.limiter.Apply(ctx, ratelimit.APIV1, p.APIKeyID.String()); err != nil {
		if e, ok := apperr.As(err); ok && e.Status == http.StatusTooManyRequests {
			v1api.TooManyRequests(c, apperr.TooManyRequestsMessage)
			return
		}
		h.unexpected(c, err, messageGeneric)
		return
	}
	c.Next()
}

func (h *V1Handler) unexpected(c *gin.Context, err error, message string) {
	h.log.ErrorContext(c.Request.Context(), "v1 webhooks request failed", "route", c.FullPath(), "err", err)
	v1api.InternalError(c, message)
}

// v1Audit is the audit record of one v1 mutation, filled in by the handler as it learns the target.
type v1Audit struct {
	targetID  string
	oldObject any
	newObject any
	failed    bool
}

// audited records the request's audit event once the handler has answered, as forma2's wrapper did: the API key as the actor in its organization, success only for a 2xx that the handler did not mark failed, and the request id on a failure.
func (h *V1Handler) audited(action audit.Action, handler gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		rec := &v1Audit{targetID: audit.Unknown}
		c.Set(auditKey, rec)
		handler(c)
		p := principal(c)
		e := audit.Event{
			Action: action, TargetType: audit.TargetWebhook, TargetID: rec.targetID, ActorType: audit.ActorAPI,
			ActorID: p.APIKeyID.String(), OrganizationID: p.OrganizationID.String(), Status: audit.StatusFailure,
			OldObject: rec.oldObject, NewObject: rec.newObject,
			APIURL: h.cfg.WebAppURL.Scheme + "://" + h.cfg.WebAppURL.Host + c.Request.URL.RequestURI(),
		}
		if status := c.Writer.Status(); status >= 200 && status < 300 && !rec.failed {
			e.Status = audit.StatusSuccess
		} else {
			e.EventID = httpserver.RequestID(c)
		}
		ctx := c.Request.Context()
		h.svc.audit.Log(ctx, audit.FromRequest(ctx, e))
	}
}

func auditRecord(c *gin.Context) *v1Audit {
	v, _ := c.Get(auditKey)
	rec, _ := v.(*v1Audit)
	return rec
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
	p := principal(c)
	ids := []uuid.UUID{}
	for _, g := range p.WorkspacePermissions {
		if !slices.Contains(ids, g.WorkspaceID) {
			ids = append(ids, g.WorkspaceID)
		}
	}
	found, err := h.svc.repo.OfWorkspaces(ctx, ids)
	if err != nil {
		h.unexpected(c, err, messageGeneric)
		return
	}
	views, err := h.withEnvironmentIDs(ctx, found, false)
	if err != nil {
		h.unexpected(c, err, messageGeneric)
		return
	}
	v1api.Success(c, views)
}

// can is forma2's can(apiKey, workspace.<method's action>, workspace).
func (h *V1Handler) can(c *gin.Context, method string, workspaceID uuid.UUID) (bool, error) {
	return h.svc.authz.Can(c.Request.Context(), principal(c).Actor(), authz.WorkspaceActionForMethod(method), authz.Workspace(workspaceID))
}

// byPathID loads the webhook the path names, by its id or the forma2 id it was migrated from. It answers 404 itself and returns nil when there is none.
func (h *V1Handler) byPathID(c *gin.Context) *Webhook {
	raw := c.Param("webhookId")
	id, _ := legacyid.Resolve(raw)
	w, err := h.svc.repo.ByID(c.Request.Context(), id)
	if err != nil {
		h.unexpected(c, err, messageUnexpected)
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
	allowed, err := h.can(c, http.MethodGet, w.WorkspaceID)
	if err != nil {
		h.unexpected(c, err, messageUnexpected)
		return
	}
	if !allowed {
		v1api.Unauthorized(c)
		return
	}
	views, err := h.withEnvironmentIDs(c.Request.Context(), []*Webhook{w}, false)
	if err != nil {
		h.unexpected(c, err, messageUnexpected)
		return
	}
	v1api.Success(c, views[0])
}

// delete is forma2's DELETE /{webhookId}: 404 for an unknown id, then workspace.manage (401 when refused); a failed delete is answered 404 and audited as a failure.
func (h *V1Handler) delete(c *gin.Context) {
	rec := auditRecord(c)
	rec.targetID = c.Param("webhookId")
	w := h.byPathID(c)
	if w == nil {
		return
	}
	allowed, err := h.can(c, http.MethodDelete, w.WorkspaceID)
	if err != nil {
		h.unexpected(c, err, messageUnexpected)
		return
	}
	if !allowed {
		v1api.Unauthorized(c)
		return
	}
	rec.oldObject = w.view()
	deleted, err := h.svc.repo.delete(c.Request.Context(), w.ID)
	if err != nil || !deleted {
		if err != nil {
			h.log.ErrorContext(c.Request.Context(), "Error deleting webhook", "err", err)
		}
		rec.failed = true
		v1api.NotFound(c, "Webhook", c.Param("webhookId"), false)
		return
	}
	v1api.Success(c, h.bestEffort(c, w, false))
}

// create is forma2's POST: the body read with its 2 MiB limit, the workspace resolved from workspaceId or the legacy environmentId and authorized for workspace.write, the input validated, the URL's SSRF check, then the webhook with a new secret. The answer is the only one that carries the secret.
func (h *V1Handler) create(c *gin.Context) {
	ctx := c.Request.Context()
	body, err := readJSONObject(c.Request)
	if err != nil {
		if errors.Is(err, errBodyTooLarge) {
			v1api.PayloadTooLarge(c, map[string]any{"error": err.Error()})
			return
		}
		v1api.BadRequest(c, "Malformed JSON input, please check your request body", nil, false)
		return
	}
	rawID := body["workspaceId"]
	if !truthy(rawID) {
		rawID = body["environmentId"]
	}
	if !truthy(rawID) {
		v1api.BadRequest(c, "workspaceId must be provided", nil, false)
		return
	}
	rawWorkspace, ok := rawID.(string)
	if !ok {
		v1api.BadRequest(c, "workspaceId must be a string", nil, false)
		return
	}
	ws, err := h.workspaces.ResolveClientID(ctx, rawWorkspace)
	if err != nil {
		h.unexpected(c, err, messageUnexpected)
		return
	}
	if ws == nil {
		v1api.NotFound(c, "Workspace", rawWorkspace, false)
		return
	}
	allowed, err := h.can(c, http.MethodPost, ws.ID)
	if err != nil {
		h.unexpected(c, err, messageUnexpected)
		return
	}
	if !allowed {
		v1api.Unauthorized(c)
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
		h.unexpected(c, err, messageGeneric)
		return
	}
	rec := auditRecord(c)
	rec.targetID, rec.newObject = w.ID.String(), w.auditObject()
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

var errBodyTooLarge = fmt.Errorf("Request body must not exceed %d bytes", maxBodyBytes) //nolint:staticcheck // forma2's message, which clients see

// readJSONObject is forma2's parseJsonBodyWithLimit: a declared or actual body over 2 MiB is refused, and the body must be JSON. A body that is JSON but not an object reads as an object without members, as forma2's property reads on it found nothing.
func readJSONObject(r *http.Request) (map[string]any, error) {
	if r.ContentLength > maxBodyBytes {
		return nil, errBodyTooLarge
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	if len(b) > maxBodyBytes {
		return nil, errBodyTooLarge
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	obj, _ := v.(map[string]any)
	if obj == nil {
		obj = map[string]any{}
	}
	return obj, nil
}

// truthy is JavaScript's Boolean(v) for a JSON value.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != ""
	}
	return true
}

// jsType is the type name zod's messages report for a JSON value.
func jsType(v any, present bool) string {
	switch v.(type) {
	case nil:
		if !present {
			return "undefined"
		}
		return "null"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	}
	return "object"
}

func quotedOptions(values []string) string {
	q := make([]string, len(values))
	for i, v := range values {
		q[i] = `"` + v + `"`
	}
	return strings.Join(q, "|")
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
			details["name"] = "Invalid input: expected string, received " + jsType(v, true)
		}
	}
	if v, present := body["source"]; present {
		s, ok := v.(string)
		if !ok || !slices.Contains(Sources, s) {
			details["source"] = "Invalid option: expected one of " + quotedOptions(Sources)
		}
		in.Source = s
	}
	if v, present := body["surveyIds"]; present {
		list, ok := v.([]any)
		if !ok {
			details["surveyIds"] = "Invalid input: expected array, received " + jsType(v, true)
		}
		for i, e := range list {
			s, ok := e.(string)
			if !ok {
				details[fmt.Sprintf("surveyIds.%d", i)] = "Invalid input: expected string, received " + jsType(e, true)
				continue
			}
			if _, err := uuid.Parse(s); (err != nil || len(s) != 36) && !cuid2.MatchString(s) {
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
		details["triggers"] = "Invalid input: expected array, received " + jsType(v, present)
	case len(list) == 0:
		details["triggers"] = "At least one trigger is required"
	}
	for i, e := range list {
		s, ok := e.(string)
		if !ok || !slices.Contains(Triggers, s) {
			details[fmt.Sprintf("triggers.%d", i)] = "Invalid option: expected one of " + quotedOptions(Triggers)
			continue
		}
		in.Triggers = append(in.Triggers, s)
	}
	v, present = body["url"]
	s, ok := v.(string)
	switch {
	case !ok:
		details["url"] = "Invalid input: expected string, received " + jsType(v, present)
	case !isURL(s):
		details["url"] = "Invalid URL"
	}
	in.URL = s
	if len(details) > 0 {
		return in, details
	}
	return in, nil
}

// isURL is zod's z.url(): an absolute URL with a scheme and a host.
func isURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme != "" && u.Host != ""
}
