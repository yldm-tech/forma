package notification

import (
	"context"
	"log/slog"
	"mime"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/auth"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
)

// Switches reads and switches a user's notification settings without a session, for the user a signed unsubscribe link names. auth.Service implements it.
type Switches interface {
	NotificationSwitchOf(ctx context.Context, userID uuid.UUID, kind string, id uuid.UUID) (auth.NotificationSwitch, error)
	SetNotificationSwitch(ctx context.Context, userID uuid.UUID, kind string, id uuid.UUID, enabled bool) error
}

// Handler answers the unsubscribe links of the notification emails. The signed links of the emails sent now open the public SPA page /app/unsubscribe, which reads and switches the setting through /api/unsubscribe; mail clients post their one-click unsubscribe (RFC 8058) there too. The unsigned link of the emails sent before, /app/workspaces/{id}/settings/notifications, is a browser navigation that needs the server: it is under the app's base path ahead of the SPA, which owns every other page there.
type Handler struct {
	cfg      config.Config
	switches Switches
	limiter  *ratelimit.Limiter
	log      *slog.Logger
}

func NewHandler(cfg config.Config, switches Switches, limiter *ratelimit.Limiter, log *slog.Logger) *Handler {
	return &Handler{cfg: cfg, switches: switches, limiter: limiter, log: log}
}

// Register mounts the old link's path and the token API. The API is outside /api/app: a mail client's one-click post comes from the mail provider, without the SPA's origin and CSRF header, and the token is the authorization. It answers errors with /api/app's envelope and is rate limited per client address.
func (h *Handler) Register(r gin.IRouter) {
	r.GET(config.AppBasePath+"/workspaces/:workspaceId/settings/notifications", h.unsubscribe)
	api := r.Group(UnsubscribeAPIPath, noStore, httpapi.ErrorHandler(h.log), ratelimit.Middleware(h.limiter, ratelimit.PublicUnsubscribe, ratelimit.ByClientIP))
	// Reading never changes the setting: mail scanners and link previews fetch the links an email carries.
	api.GET("", h.lookup)
	api.POST("", h.set)
}

func noStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Next()
}

// unsubscribe redirects to the account notification settings with the switch the link names, which the page turns off once the user is signed in.
func (h *Handler) unsubscribe(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusFound, UnsubscribeTarget(c.Query("type"), c.Query("elementId")))
}

// invalidToken answers a token that does not verify.
var invalidToken = apperr.InvalidInput("invalid_token", "This unsubscribe link is not valid.")

// lookup is the switch ?token= names, as the notifications page shows it to the token's user: which kind, for which survey or organization, by name, and whether it is on.
func (h *Handler) lookup(c *gin.Context) {
	u, err := VerifyUnsubscribeToken(h.cfg, c.Query("token"))
	if err != nil {
		httpapi.Fail(c, invalidToken)
		return
	}
	s, err := h.switches.NotificationSwitchOf(c.Request.Context(), u.UserID, u.Type, u.ElementID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, s)
}

// setRequest turns the token's switch off (enabled false) or back on, from the SPA page.
type setRequest struct {
	Token   string `json:"token" binding:"required"`
	Enabled *bool  `json:"enabled" binding:"required"`
}

// oneClickBody is the form field and value RFC 8058 has a mail client post.
const (
	oneClickField = "List-Unsubscribe"
	oneClickValue = "One-Click"
)

// set switches the token's setting and answers with it as it now is. A JSON body is the SPA page's {token, enabled}. A form body is a mail client's one-click unsubscribe: List-Unsubscribe=One-Click posted to the List-Unsubscribe URL, whose ?token= names the switch, which it turns off.
func (h *Handler) set(c *gin.Context) {
	var token string
	var enabled bool
	mediaType, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
	switch mediaType {
	case "application/x-www-form-urlencoded", "multipart/form-data":
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, httpapi.MaxBodyBytes)
		if c.PostForm(oneClickField) != oneClickValue {
			httpapi.Fail(c, apperr.BadRequest("A one-click unsubscribe posts List-Unsubscribe=One-Click"))
			return
		}
		token = c.Query("token")
	default:
		var in setRequest
		if err := httpapi.BindJSON(c, &in); err != nil {
			httpapi.Fail(c, err)
			return
		}
		token, enabled = in.Token, *in.Enabled
	}
	u, err := VerifyUnsubscribeToken(h.cfg, token)
	if err != nil {
		httpapi.Fail(c, invalidToken)
		return
	}
	ctx := c.Request.Context()
	if err := h.switches.SetNotificationSwitch(ctx, u.UserID, u.Type, u.ElementID, enabled); err != nil {
		httpapi.Fail(c, err)
		return
	}
	s, err := h.switches.NotificationSwitchOf(ctx, u.UserID, u.Type, u.ElementID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, s)
}
