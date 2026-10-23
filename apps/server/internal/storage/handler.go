package storage

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/apikey"
	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/v1api"
	"github.com/yldm-tech/forma/apps/server/internal/workspace"
)

// route is forma2's /storage/[workspaceId]/[accessType]/[...filePath]. The id is a workspace id, a forma2 workspace id or a legacy environment id: whatever the stored URL was built with.
const route = "/storage/:workspaceId/:accessType/*filePath"

// allowedMethods is the Allow header Next.js answered OPTIONS with for a route that exports GET and DELETE.
const allowedMethods = "DELETE, GET, HEAD, OPTIONS"

// publicStoragePath is the only part of /storage the public domain serves (forma2's PUBLIC_STORAGE_ROUTES).
var publicStoragePath = regexp.MustCompile(`^/storage/[^/]+/public/`)

// Handler serves and deletes stored files at /storage, outside /api/app: its URLs are embedded in surveys, responses and emails, so the path and the answers are forma2's.
type Handler struct {
	cfg        config.Config
	files      *Service
	workspaces *workspace.Repository
	sessions   httpapi.SessionAuth
	apiKeys    *apikey.Authenticator
	authz      *authz.Authorizer
	limiter    *ratelimit.Limiter
	audit      *audit.Emitter
	log        *slog.Logger
}

func NewHandler(cfg config.Config, files *Service, workspaces *workspace.Repository, sessions httpapi.SessionAuth, apiKeys *apikey.Authenticator, authorizer *authz.Authorizer, limiter *ratelimit.Limiter, emitter *audit.Emitter, log *slog.Logger) *Handler {
	return &Handler{cfg: cfg, files: files, workspaces: workspaces, sessions: sessions, apiKeys: apiKeys, authz: authorizer, limiter: limiter, audit: emitter, log: log}
}

// Register mounts the route with every method Next.js answered on it: GET and DELETE, HEAD as GET without the body, OPTIONS with the Allow header, and a bare 405 for the rest.
func (h *Handler) Register(r gin.IRouter) {
	g := r.Group("", h.publicDomainGuard, h.renderFailures, authz.DecisionMemo())
	// A session is read only where it decides something: a public download is cacheable for a year and must never carry a refreshed session cookie.
	optional := h.sessions.Optional()
	withSession := func(c *gin.Context) {
		if c.Param("accessType") == string(AccessPrivate) || c.Request.Method == http.MethodDelete {
			optional(c)
			return
		}
		c.Next()
	}
	g.GET(route, withSession, h.download)
	g.HEAD(route, withSession, h.download)
	g.DELETE(route, withSession, h.delete)
	g.OPTIONS(route, func(c *gin.Context) {
		c.Header("Allow", allowedMethods)
		c.Status(http.StatusNoContent)
	})
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch} {
		g.Handle(method, route, func(c *gin.Context) { c.Status(http.StatusMethodNotAllowed) })
	}
}

// publicDomainGuard is forma2's domain-aware routing: when PUBLIC_URL is set, a request to the public domain may reach public files only, and anything else under /storage there is a bare 404.
func (h *Handler) publicDomainGuard(c *gin.Context) {
	if h.cfg.PublicURL != nil && c.Request.Host == h.cfg.PublicURL.Host && !publicStoragePath.MatchString(c.Request.URL.EscapedPath()) {
		c.AbortWithStatus(http.StatusNotFound)
	}
}

// renderFailures answers a failure the session middleware reported (the session store was unreachable) the way Next.js answered an uncaught error: a bare 500.
func (h *Handler) renderFailures(c *gin.Context) {
	c.Next()
	if len(c.Errors) > 0 && !c.Writer.Written() {
		h.unexpected(c, c.Errors.Last().Err)
	}
}

func (h *Handler) unexpected(c *gin.Context, err error) {
	h.log.ErrorContext(c.Request.Context(), "storage request failed", "route", c.FullPath(), "err", err)
	c.AbortWithStatus(http.StatusInternalServerError)
}

// fileRequest is the validated part of the path: the access type and the file path after it, decoded once, as Next.js handed it over.
type fileRequest struct {
	rawID    string
	access   AccessType
	fileName string
}

// parseFileRequest is forma2's ZDownloadFileRequest (also ZDeleteFileRequest). The file name may be any name with an extension; only uploads are restricted to the allowed ones. On failure it returns the details of forma2's 400, one zod message per field.
func parseFileRequest(c *gin.Context) (fileRequest, map[string]any) {
	req := fileRequest{rawID: c.Param("workspaceId"), access: AccessType(c.Param("accessType")), fileName: strings.TrimPrefix(c.Param("filePath"), "/")}
	details := map[string]any{}
	// zod trims before it checks, and the refinement's message replaces the empty-string one on the same path.
	if name := jsTrim(req.fileName); !hasExtension(name) {
		details["fileName"] = "File name must have an extension"
	}
	if req.access != AccessPublic && req.access != AccessPrivate {
		details["accessType"] = `Invalid option: expected one of "public"|"private"`
	}
	if len(details) > 0 {
		return req, details
	}
	return req, nil
}

func hasExtension(name string) bool {
	i := strings.LastIndex(name, ".")
	return i >= 0 && i < len(name)-1
}

// download is forma2's GET: the file streamed with the type it was uploaded with, a private file with its extension's (ServedContentType). A private file needs workspace.read; a public one needs nothing. There is no rate limit, as in forma2: survey pages load their images from here.
func (h *Handler) download(c *gin.Context) {
	ctx := c.Request.Context()
	req, details := parseFileRequest(c)
	if details != nil {
		v1api.BadRequest(c, "Fields are missing or incorrectly formatted", details, true)
		return
	}
	ws, err := h.workspaces.ResolveClientID(ctx, req.rawID)
	if err != nil {
		h.unexpected(c, err)
		return
	}
	if ws == nil {
		v1api.NotFound(c, "Workspace", req.rawID, false)
		return
	}
	if req.access == AccessPrivate {
		if _, err := h.authorize(c, ws.ID, authz.WorkspaceRead); err != nil {
			h.refuse(c, err)
			return
		}
	}

	obj, err := h.files.Open(ctx, req.fileName, ws.ID.String(), req.rawID, req.access)
	if err != nil {
		WriteStorageError(c, h.log, err, req.fileName)
		return
	}
	defer func() { _ = obj.Body.Close() }()
	header := c.Writer.Header()
	header.Set("Content-Type", ServedContentType(req.fileName, obj.ContentType, req.access))
	if obj.ContentLength > 0 {
		header.Set("Content-Length", strconv.FormatInt(obj.ContentLength, 10))
	}
	if req.access == AccessPrivate {
		header.Set("Cache-Control", "no-store, no-cache, must-revalidate")
	} else {
		header.Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	c.Status(http.StatusOK)
	if c.Request.Method == http.MethodHead {
		return
	}
	if _, err := io.Copy(c.Writer, obj.Body); err != nil {
		h.log.WarnContext(ctx, "Streaming a stored file was cut short", "err", err)
	}
}

// delete is forma2's DELETE. It always needs workspace.manage, for public files too, and a signed-in user may delete five files a minute; forma2 left API keys to its gateway's limit. Every outcome is audited.
func (h *Handler) delete(c *gin.Context) {
	ctx := c.Request.Context()
	req, details := parseFileRequest(c)
	if details != nil {
		org := ""
		// forma2 looked the organization up by workspace id only, so a legacy environment id is not attributed.
		if ws, err := h.workspaces.ResolveClientID(ctx, req.rawID); err == nil && ws != nil && legacyid.Matches(req.rawID, ws.ID) {
			org = ws.OrganizationID.String()
		}
		h.auditDeletion(c, deletion{workspaceID: req.rawID, organizationID: org, actorID: audit.Unknown, failureReason: "Parameter validation failed"})
		v1api.BadRequest(c, "Fields are missing or incorrectly formatted", details, true)
		return
	}
	ws, err := h.workspaces.ResolveClientID(ctx, req.rawID)
	if err != nil {
		h.unexpected(c, err)
		return
	}
	if ws == nil {
		h.auditDeletion(c, deletion{workspaceID: req.rawID, actorID: audit.Unknown, failureReason: "Workspace not found"})
		v1api.NotFound(c, "Workspace", req.rawID, false)
		return
	}
	failed := deletion{workspaceID: ws.ID.String(), organizationID: ws.OrganizationID.String(), access: req.access, actorID: audit.Unknown}

	caller, err := h.authorize(c, ws.ID, authz.WorkspaceManage)
	if err != nil {
		switch {
		case errors.Is(err, errUnauthorized):
			failed.failureReason = "User not authorized to access workspace"
		case errors.Is(err, errNotAuthenticated):
			failed.failureReason = "User not authenticated"
		}
		if failed.failureReason != "" {
			h.auditDeletion(c, failed)
		}
		h.refuse(c, err)
		return
	}
	if caller != uuid.Nil {
		if err := h.limiter.Apply(ctx, ratelimit.StorageDelete, caller.String()); err != nil {
			if e, ok := apperr.As(err); ok {
				v1api.TooManyRequests(c, e.Message)
				return
			}
			h.unexpected(c, err)
			return
		}
		failed.actorID = caller.String()
	}

	if err := h.files.Delete(ctx, req.fileName, ws.ID.String(), req.rawID, req.access); err != nil {
		failed.failureReason = string(CodeOf(err))
		h.auditDeletion(c, failed)
		WriteStorageError(c, h.log, err, req.fileName)
		return
	}
	succeeded := failed
	succeeded.success = true
	h.auditDeletion(c, succeeded)
	v1api.Success(c, "File deleted successfully")
}

var (
	errNotAuthenticated = errors.New("not authenticated")
	errUnauthorized     = errors.New("unauthorized")
)

// authorize is forma2's authorizePrivateDownload: a signed-in user must hold action on the workspace; without a session, the request's API key must. It returns the user, or uuid.Nil for an API key, and errNotAuthenticated or errUnauthorized when refused.
func (h *Handler) authorize(c *gin.Context, workspaceID uuid.UUID, action authz.Action) (uuid.UUID, error) {
	ctx := c.Request.Context()
	if actor, ok := httpapi.ActorFrom(ctx); ok {
		allowed, err := h.authz.Can(ctx, authz.User(actor.UserID), action, authz.Workspace(workspaceID))
		if err != nil {
			return uuid.Nil, err
		}
		if !allowed {
			return uuid.Nil, errUnauthorized
		}
		return actor.UserID, nil
	}
	key, err := h.apiKeys.Authenticate(ctx, c.Request.Header, apikey.Options{})
	if err != nil {
		return uuid.Nil, err
	}
	if key == nil {
		return uuid.Nil, errNotAuthenticated
	}
	allowed, err := h.authz.Can(ctx, key.Actor(), action, authz.Workspace(workspaceID))
	if err != nil {
		return uuid.Nil, err
	}
	if !allowed {
		return uuid.Nil, errUnauthorized
	}
	return uuid.Nil, nil
}

// refuse answers an authorization failure: forma2's 401s, without CORS headers, or a bare 500 when authorization could not be decided.
func (h *Handler) refuse(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errUnauthorized):
		v1api.Unauthorized(c)
	case errors.Is(err, errNotAuthenticated):
		v1api.NotAuthenticated(c)
	default:
		h.unexpected(c, err)
	}
}

// deletion is one audited outcome of a delete.
type deletion struct {
	workspaceID    string
	organizationID string
	// access is empty where forma2 had not read it yet; its target id then ends in ":undefined".
	access        AccessType
	actorID       string
	success       bool
	failureReason string
}

// auditDeletion is forma2's logFileDeletion.
func (h *Handler) auditDeletion(c *gin.Context, d deletion) {
	access := "undefined"
	newObject := map[string]any{"workspaceId": d.workspaceID}
	if d.access != "" {
		access = string(d.access)
		newObject["accessType"] = access
	}
	status := audit.StatusFailure
	if d.success {
		status = audit.StatusSuccess
	}
	if d.failureReason != "" {
		newObject["failureReason"] = d.failureReason
	}
	ctx := c.Request.Context()
	h.audit.Log(ctx, audit.FromRequest(ctx, audit.Event{
		Action:         audit.ActionDeleted,
		TargetType:     audit.TargetFile,
		TargetID:       d.workspaceID + ":" + access,
		ActorType:      audit.ActorUser,
		ActorID:        d.actorID,
		OrganizationID: d.organizationID,
		Status:         status,
		NewObject:      newObject,
		APIURL:         h.cfg.WebAppURL.Scheme + "://" + h.cfg.WebAppURL.Host + c.Request.URL.RequestURI(),
	}))
}
