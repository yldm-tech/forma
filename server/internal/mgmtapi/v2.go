package mgmtapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/apikey"
	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/mgmtapi/mgmtv1"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/v1api"
)

// v2Messages are the messages of forma2's v2 error responses, by status (apps/web/modules/api/v2/lib/response.ts).
var v2Messages = map[int]string{
	http.StatusBadRequest: "Bad Request", http.StatusUnauthorized: "Unauthorized", http.StatusForbidden: "Forbidden", http.StatusNotFound: "Not Found",
	http.StatusConflict: "Conflict", http.StatusRequestEntityTooLarge: "Payload Too Large", http.StatusUnprocessableEntity: "Unprocessable Entity",
	http.StatusTooManyRequests: "Too Many Requests", http.StatusInternalServerError: "Internal Server Error", http.StatusServiceUnavailable: "Service Unavailable",
}

// messageInternal is the only detail a v2 500 ever carries: internal messages are never exposed.
const messageInternal = "An error occurred while processing your request. Please try again later."

// v2Detail is one entry of a v2 error's details.
type v2Detail struct {
	Field string         `json:"field"`
	Issue string         `json:"issue"`
	Meta  map[string]any `json:"meta,omitempty"`
}

func detail(field, issue string) []v2Detail { return []v2Detail{{Field: field, Issue: issue}} }

// v2Details is formatZodError: every issue in order, the custom ones' params as meta.
func (is *issues) v2Details() []v2Detail {
	out := make([]v2Detail, len(is.list))
	for i, x := range is.list {
		out[i] = v2Detail{Field: x.path, Issue: x.message, Meta: x.meta}
	}
	return out
}

// v2Write writes a v2 body with Cache-Control "private, no-store" and, when cors is set, forma2's CORS headers.
func v2Write(c *gin.Context, status int, body any, cors bool) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	if cors {
		h := c.Writer.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	}
	c.Header("Cache-Control", v1api.CacheNoStore)
	c.Data(status, "application/json", bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
	c.Abort()
}

// v2Error is forma2's handleApiError: {"error": {code, message, details}}, without details on a 401, 403 or 429, and with only the generic detail on a 500.
func v2Error(c *gin.Context, status int, details []v2Detail) {
	body := map[string]any{"code": status, "message": v2Messages[status]}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests:
	case http.StatusInternalServerError:
		body["details"] = detail("error", messageInternal)
	default:
		if details == nil {
			details = []v2Detail{}
		}
		body["details"] = details
	}
	v2Write(c, status, map[string]any{"error": body}, false)
}

// v2OK is successResponse: 200 with {data, meta} and CORS; meta is left out when nil.
func v2OK(c *gin.Context, data any, meta any) {
	body := map[string]any{"data": jsView(data)}
	if meta != nil {
		body["meta"] = meta
	}
	v2Write(c, http.StatusOK, body, true)
}

// v2Created is createdResponse: 201 with {data}, without CORS.
func v2Created(c *gin.Context, data any) {
	v2Write(c, http.StatusCreated, map[string]any{"data": jsView(data)}, false)
}

// v2Unexpected logs an error the handler cannot answer otherwise and answers forma2's generic 500.
func (h *Handler) v2Unexpected(c *gin.Context, err error) {
	h.log.ErrorContext(c.Request.Context(), "v2 management request failed", "route", c.FullPath(), "err", err)
	v2Error(c, http.StatusInternalServerError, nil)
}

// listMeta is ApiSuccessResponse's meta of a list.
type listMeta struct {
	Total  int64 `json:"total"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
}

// ---- request pipeline ----

// Context keys of the v2 caller and audit record.
const (
	v2PrincipalKey = "mgmtapi.v2.principal"
	v2AuditKey     = "mgmtapi.v2.audit"
)

func v2Principal(c *gin.Context) *apikey.Principal {
	v, _ := c.Get(v2PrincipalKey)
	p, _ := v.(*apikey.Principal)
	return p
}

// v2Auth is the front of forma2's apiWrapper: an API key valid for opts, otherwise 401.
func (h *Handler) v2Auth(opts apikey.Options) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, err := h.keys.Authenticate(c.Request.Context(), c.Request.Header, opts)
		if err != nil {
			h.v2Unexpected(c, err)
			return
		}
		if p == nil {
			v2Error(c, http.StatusUnauthorized, nil)
			return
		}
		c.Set(v2PrincipalKey, p)
		c.Next()
	}
}

// v2Limit charges api:v2 to the key once the request has passed validation, as forma2's apiWrapper did, answering 429 when the window is spent. From here on a failure is the handler's, which a mutation audits.
func (h *Handler) v2Limit(c *gin.Context) bool {
	if rec := v2AuditOf(c); rec != nil {
		rec.reached = true
	}
	if err := h.limiter.Apply(c.Request.Context(), ratelimit.APIV2, v2Principal(c).APIKeyID.String()); err != nil {
		if rec := v2AuditOf(c); rec != nil {
			rec.reached = false
		}
		v2Error(c, http.StatusTooManyRequests, nil)
		return false
	}
	return true
}

// v2Audit is the audit record of one v2 mutation.
type v2Audit struct {
	targetID  string
	oldObject any
	newObject any
	// reached is set once the request passed validation and the rate limit: forma2 audited a failure only from there on.
	reached bool
}

func v2AuditOf(c *gin.Context) *v2Audit {
	v, _ := c.Get(v2AuditKey)
	rec, _ := v.(*v2Audit)
	return rec
}

// v2Audited records the mutation's audit event once the handler has answered: success for a 2xx, failure for an error the handler itself answered, nothing for a request refused before it.
func (h *Handler) v2Audited(action audit.Action, target audit.Target, handler gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		rec := &v2Audit{targetID: audit.Unknown}
		c.Set(v2AuditKey, rec)
		handler(c)
		status := c.Writer.Status()
		success := status >= 200 && status < 300
		if !success && !rec.reached {
			return
		}
		p := v2Principal(c)
		e := audit.Event{
			Action: action, TargetType: target, TargetID: rec.targetID, ActorType: audit.ActorAPI, ActorID: p.APIKeyID.String(),
			OrganizationID: p.OrganizationID.String(), Status: audit.StatusFailure, OldObject: rec.oldObject, NewObject: rec.newObject,
			APIURL:  h.cfg.WebAppURL.Scheme + "://" + h.cfg.WebAppURL.Host + c.Request.URL.RequestURI(),
			EventID: httpserver.RequestID(c),
		}
		if success {
			e.Status = audit.StatusSuccess
		}
		ctx := c.Request.Context()
		h.audit.Log(ctx, audit.FromRequest(ctx, e))
	}
}

func (h *Handler) v2Record(c *gin.Context, targetID string, oldObject, newObject any) {
	if rec := v2AuditOf(c); rec != nil {
		if targetID != "" {
			rec.targetID = targetID
		}
		if oldObject != nil {
			rec.oldObject = oldObject
		}
		if newObject != nil {
			rec.newObject = newObject
		}
	}
}

// v2Body reads the JSON body as forma2's apiWrapper did: 413 over the limit, 400 when it is not JSON (an empty body included). A body that is JSON but not an object reads as an object without members.
func v2Body(c *gin.Context) (map[string]any, bool) {
	body, err := v1api.ReadJSONObject(c.Request)
	if err != nil {
		if errors.Is(err, v1api.ErrBodyTooLarge) {
			v2Error(c, http.StatusRequestEntityTooLarge, detail("body", err.Error()))
			return nil, false
		}
		v2Error(c, http.StatusBadRequest, detail("error", v1api.MalformedJSONMessage))
		return nil, false
	}
	return body, true
}

// v2Query is the query as forma2 parsed it, Object.fromEntries(url.searchParams): a repeated parameter keeps its last value.
func v2Query(c *gin.Context) object {
	out := object{}
	for k, values := range c.Request.URL.Query() {
		if len(values) > 0 {
			out[k] = values[len(values)-1]
		}
	}
	return out
}

// v2ResolveWorkspace is forma2's resolveBodyIdsV2, the body transform of the create routes: the workspace named by workspaceId or the legacy environmentId must exist (404), be in the key's organization and allow the method's action (403 otherwise); its canonical id replaces the body's.
func (h *Handler) v2ResolveWorkspace(c *gin.Context, body map[string]any, method string) (uuid.UUID, bool) {
	raw, present := body["workspaceId"]
	if !present || raw == nil {
		raw = body["environmentId"]
	}
	if !v1api.Truthy(raw) {
		v2Error(c, http.StatusBadRequest, detail("workspaceId", "workspaceId must be provided"))
		return uuid.Nil, false
	}
	s, _ := raw.(string)
	ws, err := h.workspaces.ResolveClientID(c.Request.Context(), s)
	if err != nil {
		h.v2Unexpected(c, err)
		return uuid.Nil, false
	}
	if ws == nil {
		v2Error(c, http.StatusNotFound, detail("workspaceId", "workspace not found"))
		return uuid.Nil, false
	}
	p := v2Principal(c)
	if ws.OrganizationID != p.OrganizationID {
		v2Error(c, http.StatusForbidden, nil)
		return uuid.Nil, false
	}
	allowed, err := h.authz.Can(c.Request.Context(), p.Actor(), authz.WorkspaceActionForMethod(method), authz.Workspace(ws.ID))
	if err != nil {
		h.v2Unexpected(c, err)
		return uuid.Nil, false
	}
	if !allowed {
		v2Error(c, http.StatusForbidden, nil)
		return uuid.Nil, false
	}
	body["workspaceId"] = ws.ID.String()
	return ws.ID, true
}

// v2Can is can(apiKey, workspace.<method's action>, workspace).
func (h *Handler) v2Can(c *gin.Context, method string, workspaceID uuid.UUID) (bool, error) {
	return h.authz.Can(c.Request.Context(), v2Principal(c).Actor(), authz.WorkspaceActionForMethod(method), authz.Workspace(workspaceID))
}

// v2Authorize is v2Can answering for the handler: 401 when refused (v2's single-resource routes answer 401, not 403), 500 when the decision failed.
func (h *Handler) v2Authorize(c *gin.Context, method string, workspaceID uuid.UUID) bool {
	allowed, err := h.v2Can(c, method, workspaceID)
	if err != nil {
		h.v2Unexpected(c, err)
		return false
	}
	if !allowed {
		v2Error(c, http.StatusUnauthorized, nil)
		return false
	}
	return true
}

// authorizedWorkspaces is forma2's getAuthorizedApiKeyWorkspaceIds: the key's grants in its own organization that SpiceDB confirms it may read. A failed lookup fails the request.
func (h *Handler) authorizedWorkspaces(c *gin.Context) ([]uuid.UUID, error) {
	p := v2Principal(c)
	readable, err := h.authz.LookupWorkspaceIDs(c.Request.Context(), p.Actor(), false)
	if err != nil {
		return nil, err
	}
	var out []uuid.UUID
	for _, g := range p.WorkspacePermissions {
		if slices.Contains(readable, g.WorkspaceID) && !slices.Contains(out, g.WorkspaceID) {
			out = append(out, g.WorkspaceID)
		}
	}
	return out, nil
}

// v2PathID reads an id parameter checked by the route's params schema (z.cuid2(), widened to the rewrite's UUIDs): 422 naming the parameter when it is malformed.
func v2PathID(c *gin.Context, name string) (uuid.UUID, bool) {
	raw := c.Param(name)
	if !v1api.IsID(raw) {
		v2Error(c, http.StatusUnprocessableEntity, detail(name, "Invalid cuid2"))
		return uuid.Nil, false
	}
	id, _ := legacyid.Resolve(raw)
	return id, true
}

// ---- the common list filter ----

// listFilter is ZGetFilter (apps/web/modules/api/v2/types/api-filter.ts), parsed.
type listFilter struct {
	Limit     int
	Skip      int
	SortBy    string
	Ascending bool
	Start     *time.Time
	End       *time.Time
	// DateField is the column the dates bound, created_at unless filterDateField says updatedAt.
	DateField string
}

// orderColumn is the column sortBy names.
func (f listFilter) orderColumn() string {
	if f.SortBy == "updatedAt" {
		return "updated_at"
	}
	return "created_at"
}

func (f listFilter) meta(total int64) listMeta {
	return listMeta{Total: total, Limit: f.Limit, Offset: f.Skip}
}

// coerceNumber is z.coerce.number() with its bounds: Number(value), which must not be NaN.
func (is *issues) coerceNumber(path string, v any, low, high float64) (float64, bool) {
	n := jsvalue.Number(v)
	if math.IsNaN(n) {
		is.add(path, "Invalid input: expected number, received NaN")
		return 0, false
	}
	ok := true
	if n < low {
		is.add(path, "Too small: expected number to be >="+jsvalue.FormatNumber(low))
		ok = false
	}
	if n > high {
		is.add(path, "Too big: expected number to be <="+jsvalue.FormatNumber(high))
		ok = false
	}
	return n, ok
}

// commonFilter reads ZGetFilter's members of q in its declared order.
func (is *issues) commonFilter(q object) listFilter {
	f := listFilter{Limit: 50, SortBy: "createdAt", DateField: "created_at"}
	if v := q.get("limit"); !isAbsent(v) {
		if n, ok := is.coerceNumber("limit", v, 1, 250); ok {
			f.Limit = int(n)
		}
	}
	if v := q.get("skip"); !isAbsent(v) {
		if n, ok := is.coerceNumber("skip", v, 0, math.Inf(1)); ok {
			f.Skip = int(n)
		}
	}
	if s, ok := is.enum("sortBy", q.get("sortBy"), optional, []string{"createdAt", "updatedAt"}); ok {
		f.SortBy = s
	}
	if s, ok := is.enum("order", q.get("order"), optional, []string{"asc", "desc"}); ok {
		f.Ascending = s == "asc"
	}
	f.Start = is.coerceDate("startDate", q.get("startDate"), optional)
	f.End = is.coerceDate("endDate", q.get("endDate"), optional)
	if s, ok := is.enum("filterDateField", q.get("filterDateField"), optional, []string{"createdAt", "updatedAt"}); ok && s == "updatedAt" {
		f.DateField = "updated_at"
	}
	return f
}

// dateRange is the refinement every list adds to ZGetFilter, which zod runs only when the members parsed.
func (is *issues) dateRange(f listFilter) {
	if !is.failed() && f.Start != nil && f.End != nil && f.Start.After(*f.End) {
		is.add("", "startDate must be before endDate")
	}
}

func (h *Handler) registerV2(engine gin.IRouter) {
	key := h.v2Auth(apikey.Options{})
	orgKey := h.v2Auth(apikey.Options{AllowOrganizationOnly: true})
	audited := h.v2Audited
	mount(engine, "/api/v2", mgmtv1.Front(h.log), []route{
		r(http.MethodGet, "/health", h.v2Health),
		r(http.MethodGet, "/me", orgKey, h.v2Me),
		r(http.MethodGet, "/roles", orgKey, h.v2Roles),

		r(http.MethodGet, "/management/responses", key, h.v2ListResponses),
		r(http.MethodPost, "/management/responses", key, audited(audit.ActionCreated, audit.TargetResponse, h.v2CreateResponse)),
		r(http.MethodGet, "/management/responses/:responseId", key, h.v2GetResponse),
		r(http.MethodPut, "/management/responses/:responseId", key, audited(audit.ActionUpdated, audit.TargetResponse, h.v2UpdateResponse)),
		r(http.MethodDelete, "/management/responses/:responseId", key, audited(audit.ActionDeleted, audit.TargetResponse, h.v2DeleteResponse)),

		r(http.MethodPost, "/management/contacts", key, audited(audit.ActionCreated, audit.TargetContact, h.v2CreateContact)),
		r(http.MethodPut, "/management/contacts/bulk", key, audited(audit.ActionBulkCreated, audit.TargetContact, h.v2BulkContacts)),

		r(http.MethodGet, "/management/contact-attribute-keys", key, h.v2ListKeys),
		r(http.MethodPost, "/management/contact-attribute-keys", key, audited(audit.ActionCreated, audit.TargetContactAttributeKey, h.v2CreateKey)),
		r(http.MethodGet, "/management/contact-attribute-keys/:contactAttributeKeyId", key, h.v2GetKey),
		r(http.MethodPut, "/management/contact-attribute-keys/:contactAttributeKeyId", key, audited(audit.ActionUpdated, audit.TargetContactAttributeKey, h.v2UpdateKey)),
		r(http.MethodDelete, "/management/contact-attribute-keys/:contactAttributeKeyId", key, audited(audit.ActionDeleted, audit.TargetContactAttributeKey, h.v2DeleteKey)),

		r(http.MethodGet, "/management/webhooks", key, h.v2ListWebhooks),
		r(http.MethodPost, "/management/webhooks", key, audited(audit.ActionCreated, audit.TargetWebhook, h.v2CreateWebhook)),
		r(http.MethodGet, "/management/webhooks/:webhookId", key, h.v2GetWebhook),
		r(http.MethodPut, "/management/webhooks/:webhookId", key, audited(audit.ActionUpdated, audit.TargetWebhook, h.v2UpdateWebhook)),
		r(http.MethodDelete, "/management/webhooks/:webhookId", key, audited(audit.ActionDeleted, audit.TargetWebhook, h.v2DeleteWebhook)),

		r(http.MethodGet, "/management/surveys/:surveyId/contact-links/contacts/:contactId", key, h.v2ContactLink),
		r(http.MethodGet, "/management/surveys/:surveyId/contact-links/segments/:segmentId", key, h.v2SegmentLinks),

		r(http.MethodGet, "/organizations/:organizationId/teams", orgKey, h.v2ListTeams),
		r(http.MethodPost, "/organizations/:organizationId/teams", orgKey, audited(audit.ActionCreated, audit.TargetTeam, h.v2CreateTeam)),
		r(http.MethodGet, "/organizations/:organizationId/teams/:teamId", orgKey, h.v2GetTeam),
		r(http.MethodPut, "/organizations/:organizationId/teams/:teamId", orgKey, audited(audit.ActionUpdated, audit.TargetTeam, h.v2UpdateTeam)),
		r(http.MethodDelete, "/organizations/:organizationId/teams/:teamId", orgKey, audited(audit.ActionDeleted, audit.TargetTeam, h.v2DeleteTeam)),

		r(http.MethodGet, "/organizations/:organizationId/users", orgKey, h.v2ListUsers),
		r(http.MethodPost, "/organizations/:organizationId/users", orgKey, audited(audit.ActionCreated, audit.TargetUser, h.v2CreateUser)),
		r(http.MethodPatch, "/organizations/:organizationId/users", orgKey, audited(audit.ActionUpdated, audit.TargetUser, h.v2UpdateUser)),

		r(http.MethodGet, "/organizations/:organizationId/workspace-teams", orgKey, h.v2ListWorkspaceTeams),
		r(http.MethodPost, "/organizations/:organizationId/workspace-teams", orgKey, audited(audit.ActionCreated, audit.TargetWorkspaceTeam, h.v2CreateWorkspaceTeam)),
		r(http.MethodPut, "/organizations/:organizationId/workspace-teams", orgKey, audited(audit.ActionUpdated, audit.TargetWorkspaceTeam, h.v2UpdateWorkspaceTeam)),
		r(http.MethodDelete, "/organizations/:organizationId/workspace-teams", orgKey, audited(audit.ActionDeleted, audit.TargetWorkspaceTeam, h.v2DeleteWorkspaceTeam)),

		// forma2's next.config.mjs rewrote the Formbricks-era project-teams path, every method, onto workspace-teams.
		r(http.MethodGet, "/organizations/:organizationId/project-teams", orgKey, h.v2ListWorkspaceTeams),
		r(http.MethodPost, "/organizations/:organizationId/project-teams", orgKey, audited(audit.ActionCreated, audit.TargetWorkspaceTeam, h.v2CreateWorkspaceTeam)),
		r(http.MethodPut, "/organizations/:organizationId/project-teams", orgKey, audited(audit.ActionUpdated, audit.TargetWorkspaceTeam, h.v2UpdateWorkspaceTeam)),
		r(http.MethodDelete, "/organizations/:organizationId/project-teams", orgKey, audited(audit.ActionDeleted, audit.TargetWorkspaceTeam, h.v2DeleteWorkspaceTeam)),
	})
}
