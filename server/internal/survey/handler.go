package survey

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/v3api"
)

// Handler serves the editor's survey routes under /api/app (session, CSRF) and the survey management API under /api/v3/surveys.
type Handler struct {
	svc      *Service
	sessions httpapi.SessionAuth
	guard    *v3api.Guard
	limiter  *ratelimit.Limiter
	log      *slog.Logger
}

func NewHandler(svc *Service, sessions httpapi.SessionAuth, guard *v3api.Guard, limiter *ratelimit.Limiter, log *slog.Logger) *Handler {
	return &Handler{svc: svc, sessions: sessions, guard: guard, limiter: limiter, log: log}
}

// Register mounts the /api/app routes on the group that already applies the error envelope and the CSRF check.
func (h *Handler) Register(r gin.IRouter) {
	g := r.Group("", h.sessions.Require())
	g.GET("/surveys/:surveyId", h.get)
	g.PUT("/surveys/:surveyId", h.update(false))
	g.PUT("/surveys/:surveyId/draft", h.update(true))
	g.POST("/surveys/:surveyId/copy", h.copy)
	g.PUT("/surveys/:surveyId/slug", h.setSlug)
	g.DELETE("/surveys/:surveyId/slug", h.removeSlug)
	g.POST("/surveys/:surveyId/single-use-links", h.singleUseLinks)
	g.PUT("/surveys/:surveyId/segment", h.attachSegment)
	g.GET("/surveys/:surveyId/quotas", h.quotas)
	g.POST("/surveys/:surveyId/quotas", h.createQuota)
	g.PATCH("/quotas/:quotaId", h.updateQuota)
	g.DELETE("/quotas/:quotaId", h.deleteQuota)
	g.GET("/quotas/:quotaId/response-count", h.quotaResponseCount)
	g.GET("/workspaces/:workspaceId/survey-templates", h.templates)
	g.GET("/public-domain", h.publicDomain)
	// The create-with-AI dialog's streamed generation spends from the same per-user bucket as POST /api/v3/surveys/generate.
	g.POST("/surveys/generate/stream", ratelimit.Middleware(h.limiter, ratelimit.APIV3SurveyGenerate, ratelimit.ByUser), h.generateStream)
}

// RegisterV3 mounts /api/v3/surveys on the engine.
func (h *Handler) RegisterV3(r gin.IRouter) {
	h.MountV3(r.Group("/api/v3/surveys", h.guard.Handlers(v3api.SessionOrAPIKey)...))
	session := r.Group("/api/v3/surveys/templates", h.guard.Handlers(v3api.SessionOnly)...)
	session.POST("", h.v3Template)
	// forma2 charged AI generation to its own limit in place of api:v3.
	generate := r.Group("/api/v3/surveys/generate", h.guard.LimitedHandlers(v3api.SessionOrAPIKey, ratelimit.APIV3SurveyGenerate)...)
	generate.POST("", h.v3Generate)
}

// MountV3 mounts the survey management operations on g, the /api/v3/surveys group. g must authenticate the caller first and put it on the request context (v3api.WithActor): RegisterV3 mounts them behind the v3 guard, MCP behind its own authentication.
func (h *Handler) MountV3(g gin.IRouter) {
	g.GET("", h.v3List)
	g.POST("", h.v3Create)
	g.POST("/validate", h.v3Validate)
	g.GET("/:surveyId", h.v3Get)
	g.PATCH("/:surveyId", h.v3Patch)
	g.DELETE("/:surveyId", h.v3Delete)
	g.POST("/:surveyId/archive", h.v3Archive)
	g.POST("/:surveyId/restore", h.v3Restore)
}

// appError is a v3 problem as the /api/app envelope: its status, code and detail, with the offending params under details.
func appError(err error) error {
	var p *v3api.Problem
	if !errors.As(err, &p) {
		return err
	}
	e := &apperr.Error{Status: p.Status, Code: p.Code, Message: p.Detail, RetryAfter: p.RetryAfter}
	if len(p.InvalidParams) > 0 {
		e.Details = map[string]any{"invalid_params": p.InvalidParams}
	}
	return e
}

// generateStream answers every refusal with the error envelope before the stream opens, then streams the generation as NDJSON, flushing each event.
func (h *Handler) generateStream(c *gin.Context) {
	body, err := v3api.ReadJSON(c)
	if err != nil {
		httpapi.Fail(c, appError(err))
		return
	}
	value, err := decodeValue(body)
	if err != nil {
		httpapi.Fail(c, apperr.BadRequest("Malformed JSON input, please check your request body"))
		return
	}
	in, err := ParseGenerate(value)
	if err != nil {
		httpapi.Fail(c, appError(err))
		return
	}
	ctx := c.Request.Context()
	stream, err := h.svc.StartGenerationStream(ctx, sessionCaller(c), in)
	if err != nil {
		httpapi.Fail(c, appError(err))
		return
	}
	c.Header("Content-Type", GenerationStreamContentType)
	// no-transform forbids an intermediary from coalescing the body; X-Accel-Buffering keeps nginx from holding it.
	c.Header("Cache-Control", "no-cache, no-store, no-transform")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	c.Writer.Flush()
	enc := json.NewEncoder(c.Writer)
	enc.SetEscapeHTML(false)
	stream.Run(ctx, httpserver.RequestID(c), func(event any) {
		if ctx.Err() != nil {
			return
		}
		if err := enc.Encode(event); err != nil {
			return
		}
		c.Writer.Flush()
	})
}

func sessionCaller(c *gin.Context) Caller {
	a, _ := httpapi.ActorFrom(c.Request.Context())
	return UserCaller(a.UserID)
}

func (h *Handler) param(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := httpapi.ParamUUID(c, name)
	if err != nil {
		httpapi.Fail(c, err)
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) get(c *gin.Context) {
	id, ok := h.param(c, "surveyId")
	if !ok {
		return
	}
	sv, err := h.svc.Get(c.Request.Context(), sessionCaller(c), id)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, sv)
}

func (h *Handler) update(draft bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := h.param(c, "surveyId")
		if !ok {
			return
		}
		var in UpdateInput
		if err := httpapi.BindJSON(c, &in); err != nil {
			httpapi.Fail(c, err)
			return
		}
		in.ID = id
		sv, err := h.svc.Update(c.Request.Context(), sessionCaller(c), in, draft)
		if err != nil {
			httpapi.Fail(c, err)
			return
		}
		c.JSON(http.StatusOK, sv)
	}
}

type copyRequest struct {
	TargetWorkspaceID uuid.UUID `json:"targetWorkspaceId" binding:"required"`
}

func (h *Handler) copy(c *gin.Context) {
	id, ok := h.param(c, "surveyId")
	if !ok {
		return
	}
	var req copyRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	sv, err := h.svc.Copy(c.Request.Context(), sessionCaller(c), id, req.TargetWorkspaceID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, sv)
}

type slugRequest struct {
	Slug string `json:"slug" binding:"required"`
}

func (h *Handler) setSlug(c *gin.Context) {
	id, ok := h.param(c, "surveyId")
	if !ok {
		return
	}
	var req slugRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	sv, err := h.svc.SetSlug(c.Request.Context(), sessionCaller(c), id, &req.Slug)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, sv)
}

func (h *Handler) removeSlug(c *gin.Context) {
	id, ok := h.param(c, "surveyId")
	if !ok {
		return
	}
	sv, err := h.svc.SetSlug(c.Request.Context(), sessionCaller(c), id, nil)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, sv)
}

func (h *Handler) singleUseLinks(c *gin.Context) {
	id, ok := h.param(c, "surveyId")
	if !ok {
		return
	}
	var in SingleUseLinksInput
	if err := httpapi.BindJSON(c, &in); err != nil {
		httpapi.Fail(c, err)
		return
	}
	links, err := h.svc.SingleUseLinks(c.Request.Context(), sessionCaller(c), id, in)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"links": links})
}

type segmentRequest struct {
	SegmentID uuid.UUID `json:"segmentId" binding:"required"`
}

func (h *Handler) attachSegment(c *gin.Context) {
	id, ok := h.param(c, "surveyId")
	if !ok {
		return
	}
	var req segmentRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	sv, err := h.svc.AttachSegment(c.Request.Context(), sessionCaller(c), id, req.SegmentID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, sv)
}

func (h *Handler) quotas(c *gin.Context) {
	id, ok := h.param(c, "surveyId")
	if !ok {
		return
	}
	quotas, err := h.svc.Quotas(c.Request.Context(), sessionCaller(c), id)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"quotas": quotas})
}

func (h *Handler) createQuota(c *gin.Context) {
	id, ok := h.param(c, "surveyId")
	if !ok {
		return
	}
	var in QuotaInput
	if err := httpapi.BindJSON(c, &in); err != nil {
		httpapi.Fail(c, err)
		return
	}
	q, err := h.svc.CreateQuota(c.Request.Context(), sessionCaller(c), id, in)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, q)
}

func (h *Handler) updateQuota(c *gin.Context) {
	id, ok := h.param(c, "quotaId")
	if !ok {
		return
	}
	var in QuotaInput
	if err := httpapi.BindJSON(c, &in); err != nil {
		httpapi.Fail(c, err)
		return
	}
	q, err := h.svc.UpdateQuota(c.Request.Context(), sessionCaller(c), id, in)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, q)
}

func (h *Handler) deleteQuota(c *gin.Context) {
	id, ok := h.param(c, "quotaId")
	if !ok {
		return
	}
	if err := h.svc.DeleteQuota(c.Request.Context(), sessionCaller(c), id); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) quotaResponseCount(c *gin.Context) {
	id, ok := h.param(c, "quotaId")
	if !ok {
		return
	}
	n, err := h.svc.QuotaResponseCount(c.Request.Context(), sessionCaller(c), id)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"count": n})
}

func (h *Handler) templates(c *gin.Context) {
	id, ok := h.param(c, "workspaceId")
	if !ok {
		return
	}
	templates, err := h.svc.Templates(c.Request.Context(), sessionCaller(c), id)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"templates": templates, "xmTemplateIds": XMTemplateIDs})
}

// publicDomain tells the list and the editor where a survey's public link lives, which forma2's pages read on the server.
func (h *Handler) publicDomain(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"publicDomain": h.svc.PublicDomain()})
}

// ---- /api/v3/surveys ----

func v3Caller(c *gin.Context) Caller {
	a, _ := v3api.ActorFrom(c.Request.Context())
	return Caller{Actor: a}
}

func (h *Handler) fail(c *gin.Context, err error) {
	v3api.Fail(c, h.log, err)
}

// v3SurveyID reads the surveyId route parameter, 400 "Invalid route parameters" when it is not an id.
func (h *Handler) v3SurveyID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("surveyId"))
	if err != nil {
		v3api.WriteProblem(c, v3api.BadRequest("Invalid route parameters", v3api.InvalidParam{Name: "surveyId", Reason: "Invalid UUID"}))
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) v3List(c *gin.Context) {
	q, problem := ParseListQuery(c.Request.URL.Query())
	if problem != nil {
		v3api.WriteProblem(c, problem)
		return
	}
	out, err := h.svc.ListV3(c.Request.Context(), v3Caller(c), q)
	if err != nil {
		h.fail(c, err)
		return
	}
	v3api.List(c, out.Data, out.Meta)
}

func (h *Handler) v3Create(c *gin.Context) {
	body, err := v3api.ReadJSON(c)
	if err != nil {
		h.fail(c, err)
		return
	}
	if createdFrom := c.Query("createdFrom"); c.Request.URL.Query().Has("createdFrom") {
		switch createdFrom {
		case "blank", "template", "xm-template", "ai":
		default:
			v3api.WriteProblem(c, v3api.BadRequest("Invalid query parameters", v3api.InvalidParam{Name: "createdFrom", Reason: enumReason([]string{"blank", "template", "xm-template", "ai"})}))
			return
		}
	}
	r, err := h.svc.CreateV3(c.Request.Context(), v3Caller(c), body)
	if err != nil {
		h.fail(c, err)
		return
	}
	v3api.Created(c, "/api/v3/surveys/"+r.ID.String(), r)
}

func (h *Handler) v3Validate(c *gin.Context) {
	body, err := v3api.ReadJSON(c)
	if err != nil {
		h.fail(c, err)
		return
	}
	if err := v3api.NoQuery(c); err != nil {
		h.fail(c, err)
		return
	}
	out, err := h.svc.ValidateV3(c.Request.Context(), v3Caller(c), body)
	if err != nil {
		h.fail(c, err)
		return
	}
	v3api.OK(c, out)
}

func (h *Handler) v3Get(c *gin.Context) {
	id, ok := h.v3SurveyID(c)
	if !ok {
		return
	}
	if err := v3api.AllowQuery(c, "lang"); err != nil {
		h.fail(c, err)
		return
	}
	var lang []string
	if values, ok := c.Request.URL.Query()["lang"]; ok {
		parsed, message := parseLanguageQuery(values)
		if message != "" {
			v3api.WriteProblem(c, v3api.BadRequest("Invalid query parameters", v3api.InvalidParam{Name: "lang", Reason: message}))
			return
		}
		lang = parsed
	}
	r, err := h.svc.GetV3(c.Request.Context(), v3Caller(c), id, lang)
	if err != nil {
		h.fail(c, err)
		return
	}
	v3api.OK(c, r)
}

func (h *Handler) v3Patch(c *gin.Context) {
	id, ok := h.v3SurveyID(c)
	if !ok {
		return
	}
	body, err := v3api.ReadJSON(c)
	if err != nil {
		h.fail(c, err)
		return
	}
	if err := v3api.NoQuery(c); err != nil {
		h.fail(c, err)
		return
	}
	r, err := h.svc.PatchV3(c.Request.Context(), v3Caller(c), id, body)
	if err != nil {
		h.fail(c, err)
		return
	}
	v3api.OK(c, r)
}

func (h *Handler) v3Delete(c *gin.Context) {
	id, ok := h.v3SurveyID(c)
	if !ok {
		return
	}
	if err := v3api.NoQuery(c); err != nil {
		h.fail(c, err)
		return
	}
	if err := h.svc.DeleteV3(c.Request.Context(), v3Caller(c), id); err != nil {
		h.fail(c, err)
		return
	}
	v3api.NoContent(c)
}

func (h *Handler) v3Lifecycle(archive bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := h.v3SurveyID(c)
		if !ok {
			return
		}
		if err := v3api.NoQuery(c); err != nil {
			h.fail(c, err)
			return
		}
		run := h.svc.RestoreV3
		if archive {
			run = h.svc.ArchiveV3
		}
		out, err := run(c.Request.Context(), v3Caller(c), id)
		if err != nil {
			h.fail(c, err)
			return
		}
		v3api.OK(c, out)
	}
}

func (h *Handler) v3Archive(c *gin.Context) { h.v3Lifecycle(true)(c) }

func (h *Handler) v3Restore(c *gin.Context) { h.v3Lifecycle(false)(c) }

func (h *Handler) v3Template(c *gin.Context) {
	body, err := v3api.ReadJSON(c)
	if err != nil {
		h.fail(c, err)
		return
	}
	in, err := ParseTemplateCreate(body)
	if err != nil {
		h.fail(c, err)
		return
	}
	r, err := h.svc.CreateFromTemplateV3(c.Request.Context(), v3Caller(c), in)
	if err != nil {
		h.fail(c, err)
		return
	}
	v3api.Created(c, "/api/v3/surveys/"+r.ID.String(), r)
}

func (h *Handler) v3Generate(c *gin.Context) {
	body, err := v3api.ReadJSON(c)
	if err != nil {
		h.fail(c, err)
		return
	}
	value, err := decodeValue(body)
	if err != nil {
		h.fail(c, v3api.BadRequest("Invalid request body", v3api.InvalidParam{Name: "body", Reason: "Malformed JSON input, please check your request body"}))
		return
	}
	in, err := ParseGenerate(value)
	if err != nil {
		h.fail(c, err)
		return
	}
	out, err := h.svc.GenerateV3(c.Request.Context(), v3Caller(c), in)
	if err != nil {
		h.fail(c, err)
		return
	}
	v3api.OK(c, out)
}
