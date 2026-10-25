// Package clientapi is forma2's client API, /api/v1/client/{id}/* and /api/v2/client/{id}/*: the public, unauthenticated endpoints the customer-site SDK (packages/js-core) and the survey runtime (packages/surveys) call to read a workspace's state, identify a user, record displays and responses and upload files. Its paths, status codes, headers and bodies are frozen: every route answers as forma2 did, which the contract test replays from recorded exchanges.
//
// The {id} in a path is a workspace id, a forma2 workspace id, or the environment id an SDK snippet installed before the workspace rename still sends. Everything a route reads or writes is scoped to the workspace it resolved.
package clientapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/contact"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/rowid"
	"github.com/yldm-tech/forma/apps/server/internal/platform/valkey"
	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/storage"
	"github.com/yldm-tech/forma/apps/server/internal/workspace"
)

// The CORS headers next.config.mjs set on every /api/(v1|v2)/client response. Next.js never overwrote a header that was already set, and these were set first, so they masked the narrower ones forma2's handlers added: every client API response carries exactly these. Vary is what Next.js added to every route handler's response.
const (
	allowMethods = "GET,OPTIONS,PATCH,DELETE,POST,PUT"
	allowHeaders = "X-CSRF-Token, X-Requested-With, Accept, Accept-Version, Content-Length, Content-MD5, Content-Type, Date, X-Api-Version, Cache-Control"
	routeVary    = "rsc, next-router-state-tree, next-router-prefetch, next-router-segment-prefetch"
)

// Handler serves the client API.
type Handler struct {
	cfg        config.Config
	db         *gorm.DB
	cache      *redis.Client
	limiter    *ratelimit.Limiter
	workspaces *workspace.Repository
	responses  *response.Service
	contacts   *contact.Service
	segments   *contact.Evaluator
	files      *storage.Service
	recaptcha  *Recaptcha
	log        *slog.Logger
	now        func() time.Time
}

func NewHandler(cfg config.Config, db *gorm.DB, cache *redis.Client, limiter *ratelimit.Limiter, workspaces *workspace.Repository, responses *response.Service, contacts *contact.Service, files *storage.Service, recaptcha *Recaptcha, log *slog.Logger) *Handler {
	return &Handler{
		cfg: cfg, db: db, cache: cache, limiter: limiter, workspaces: workspaces, responses: responses, contacts: contacts,
		segments: contact.NewEvaluator(db, log), files: files, recaptcha: recaptcha, log: log, now: time.Now,
	}
}

// Register mounts both API versions. Each route's OPTIONS answers with the Cache-Control forma2's OPTIONS export sent; every other method is charged to the client's address under forma2's api:client limit before it runs, except the storage route's own storage:upload limit.
func (h *Handler) Register(r gin.IRouter) {
	client := ratelimitBy(h, ratelimit.APIClient)
	upload := ratelimitBy(h, ratelimit.StorageUpload)
	for _, version := range []string{"v1", "v2"} {
		g := r.Group("/api/"+version+"/client/:workspaceId", cors)
		g.OPTIONS("/environment", options(preflightCache))
		g.GET("/environment", client, h.environment)
		g.OPTIONS("/displays", options(preflightCache))
		g.OPTIONS("/responses", options(preflightCache))
		g.OPTIONS("/responses/:responseId", options(noStore))
		g.PUT("/responses/:responseId", client, h.updateResponse)
		g.OPTIONS("/storage", options(preflightCache))
		g.POST("/storage", upload, h.storageUpload)
		g.OPTIONS("/user", options(preflightCache))
		g.POST("/user", client, h.identify)
	}
	v1 := r.Group("/api/v1/client/:workspaceId", cors)
	v1.POST("/displays", client, h.createDisplayV1)
	v1.OPTIONS("/displays/:displayId/response", options(noStore))
	v1.GET("/displays/:displayId/response", client, h.displayResponse)
	v1.POST("/responses", client, h.createResponseV1)
	v2 := r.Group("/api/v2/client/:workspaceId", cors)
	v2.POST("/displays", client, h.createDisplayV2)
	v2.POST("/responses", client, h.createResponseV2)
}

func cors(c *gin.Context) {
	h := c.Writer.Header()
	h.Set("Access-Control-Allow-Credentials", "true")
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", allowMethods)
	h.Set("Access-Control-Allow-Headers", allowHeaders)
	h.Set("Vary", routeVary)
}

// options is a route's OPTIONS export: {"data": {}} with its Cache-Control.
func options(cache string) gin.HandlerFunc {
	return func(c *gin.Context) { writeData(c, fields{}, cache) }
}

// ratelimitBy charges one request to the client's address under rule, forma2's applyIPRateLimit. Over the limit is 429 with forma2's message and no Retry-After; the limiter fails open when Valkey is unreachable. With GATEWAY_RATE_LIMITING=1 a route the gateway policy covers is not charged here, as forma2's client API wrapper skipped it.
func ratelimitBy(h *Handler, rule ratelimit.Rule) gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.cfg.GatewayRateLimiting && ratelimit.CoveredByGateway(c.Request.Method, c.Request.URL.Path, ratelimit.GatewayAuthNone) {
			c.Next()
			return
		}
		res, err := h.limiter.Check(c.Request.Context(), rule, ratelimit.IPIdentifier(c.Request.Context()), 1)
		if err != nil {
			h.fail(c, err, msgGeneric)
			return
		}
		if !res.Allowed {
			writeError(c, tooManyRequests())
			return
		}
		c.Next()
	}
}

// idResolutionTTL is how long a resolved client id is cached.
const idResolutionTTL = time.Hour

// idResolutionKey is where the workspace a client id names is cached.
func idResolutionKey(id uuid.UUID) string {
	return valkey.KeyPrefix + "env:" + id.String() + ":id-resolution"
}

// resolveWorkspace is the workspace the {id} in the path names, by its id, or nil. A hit is cached for an hour, a miss never, and a cache failure reads through to the database.
func (h *Handler) resolveWorkspace(ctx context.Context, raw string) (*workspace.ClientWorkspace, error) {
	id, ok := rowid.Parse(raw)
	if !ok {
		return nil, nil
	}
	key := idResolutionKey(id)
	if b, err := h.cache.Get(ctx, key).Bytes(); err == nil {
		var cached struct {
			ID             uuid.UUID `json:"id"`
			OrganizationID uuid.UUID `json:"organizationId"`
		}
		if json.Unmarshal(b, &cached) == nil {
			return &workspace.ClientWorkspace{ID: cached.ID, OrganizationID: cached.OrganizationID}, nil
		}
	} else if !errors.Is(err, redis.Nil) {
		h.log.WarnContext(ctx, "client id resolution cache read failed", "err", err)
	}
	ws, err := h.workspaces.ResolveClientID(ctx, id.String())
	if err != nil || ws == nil {
		return ws, err
	}
	b, _ := json.Marshal(map[string]uuid.UUID{"id": ws.ID, "organizationId": ws.OrganizationID})
	if err := h.cache.Set(ctx, key, b, idResolutionTTL).Err(); err != nil {
		h.log.WarnContext(ctx, "client id resolution cache write failed", "err", err)
	}
	return ws, nil
}

// workspace resolves the route's {id}, answering 404 Workspace naming it as sent when nothing matches; ok is false when the request was answered.
func (h *Handler) workspace(c *gin.Context, raw string, generic string) (*workspace.ClientWorkspace, bool) {
	ws, err := h.resolveWorkspace(c.Request.Context(), raw)
	if err != nil {
		h.fail(c, err, generic)
		return nil, false
	}
	if ws == nil {
		writeError(c, notFound("Workspace", &raw))
		return nil, false
	}
	return ws, true
}

// bodyError answers a body that could not be read: 413 over the limit, 400 with message and JSON.parse's error otherwise.
func (h *Handler) bodyError(c *gin.Context, err error, malformedMessage string) {
	if errors.Is(err, errBodyTooLarge) {
		writeError(c, newError(http.StatusRequestEntityTooLarge, codePayloadTooLarge, "Payload Too Large", fields{{"error", err.Error()}}))
		return
	}
	var se *syntaxError
	if errors.As(err, &se) {
		writeError(c, badRequest(malformedMessage, fields{{"error", se.message}}))
		return
	}
	h.fail(c, err, msgGeneric)
}

// Malformed body messages, which differ by route in forma2.
const (
	msgMalformedJSON       = "Malformed JSON input, please check your request body"
	msgInvalidJSON         = "Invalid JSON in request body"
	msgMalformedUpdateJSON = "Malformed JSON in request body"
	msgInvalidFields       = "Fields are missing or incorrectly formatted"
)
