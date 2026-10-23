// Package mgmtapi is forma2's management APIs, /api/v1/management/* and /api/v2/* (apps/web/app/api/v1/management, apps/web/modules/api/v2): the routes integrations and customers' backends call with an organization API key to manage surveys, responses, contacts, attribute keys, action classes, webhooks, teams, users and workspace access. Their paths, envelopes, status codes, error bodies and pagination are frozen; the contract test replays the exchanges recorded against forma2 (testdata/forma2).
//
// Every route authenticates the API key, charges forma2's per-key rate limit, authorizes each resource through authz with the permission its HTTP method implies, and records the audit event of a mutation. v1 answers {"data"} or {"code","message","details"} (platform/v1api, through mgmtv1's wrapper); v2 answers {"data","meta"} or {"error":{"code","message","details"}}. The domain rules live in the modules that own the resources; this package reads and validates the request, authorizes, and serializes.
package mgmtapi

import (
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/actionclass"
	"github.com/yldm-tech/forma/server/internal/apikey"
	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/contact"
	"github.com/yldm-tech/forma/server/internal/mgmtapi/mgmtv1"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/response"
	"github.com/yldm-tech/forma/server/internal/storage"
	"github.com/yldm-tech/forma/server/internal/survey"
	"github.com/yldm-tech/forma/server/internal/webhook"
	"github.com/yldm-tech/forma/server/internal/workspace"
)

// Handler serves both API versions.
type Handler struct {
	cfg           config.Config
	db            *gorm.DB
	repo          *Repository
	cache         *redis.Client
	v1            *mgmtv1.Wrapper
	keys          *apikey.Authenticator
	authz         *authz.Authorizer
	limiter       *ratelimit.Limiter
	audit         *audit.Emitter
	workspaces    *workspace.Repository
	sessions      httpapi.SessionAuth
	surveys       *survey.Service
	responses     *response.Service
	actionClasses *actionclass.Service
	contacts      *contact.Service
	segments      *contact.Evaluator
	webhooks      *webhook.Service
	files         *storage.Service
	log           *slog.Logger
	now           func() time.Time
}

func NewHandler(cfg config.Config, db *gorm.DB, cache *redis.Client, v1 *mgmtv1.Wrapper, keys *apikey.Authenticator, authorizer *authz.Authorizer, limiter *ratelimit.Limiter, emitter *audit.Emitter, workspaces *workspace.Repository, sessions httpapi.SessionAuth, surveys *survey.Service, responses *response.Service, actionClasses *actionclass.Service, contacts *contact.Service, webhooks *webhook.Service, files *storage.Service, log *slog.Logger) *Handler {
	return &Handler{
		cfg: cfg, db: db, repo: NewRepository(db), cache: cache, v1: v1, keys: keys, authz: authorizer, limiter: limiter, audit: emitter, workspaces: workspaces,
		sessions: sessions, surveys: surveys, responses: responses, actionClasses: actionClasses, contacts: contacts, segments: contact.NewEvaluator(db, log), webhooks: webhooks,
		files: files, log: log, now: time.Now,
	}
}

// route is one method on one path, with the handlers after the group's.
type route struct {
	method   string
	path     string
	handlers []gin.HandlerFunc
}

func r(method, path string, handlers ...gin.HandlerFunc) route {
	return route{method: method, path: path, handlers: handlers}
}

// mount registers routes under prefix behind front, and on every path the OPTIONS answer Next.js generated for a route handler: 204 with Allow listing the methods the path exports, unauthenticated and uncharged.
func mount(engine gin.IRouter, prefix string, front []gin.HandlerFunc, routes []route) {
	g := engine.Group(prefix, append([]gin.HandlerFunc{mgmtv1.Headers, authz.DecisionMemo()}, front...)...)
	methods := map[string][]string{}
	var paths []string
	for _, rt := range routes {
		g.Handle(rt.method, rt.path, rt.handlers...)
		if _, seen := methods[rt.path]; !seen {
			paths = append(paths, rt.path)
		}
		methods[rt.path] = append(methods[rt.path], rt.method)
	}
	options := engine.Group(prefix, mgmtv1.Headers)
	for _, p := range paths {
		allow := append([]string{http.MethodOptions}, methods[p]...)
		if slices.Contains(allow, http.MethodGet) {
			allow = append(allow, http.MethodHead)
		}
		slices.Sort(allow)
		header := strings.Join(slices.Compact(allow), ", ")
		options.OPTIONS(p, func(c *gin.Context) {
			c.Header("Allow", header)
			c.Status(http.StatusNoContent)
		})
	}
}

// Register mounts /api/v1/management and /api/v2 outside /api/app: they answer with forma2's envelopes and authenticate by API key.
func (h *Handler) Register(engine gin.IRouter) {
	h.registerV1(engine)
	h.registerV2(engine)
}
