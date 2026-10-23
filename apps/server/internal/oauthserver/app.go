package oauthserver

import (
	"net/http"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
)

// AppHandler serves the SPA's side of the authorization server under /api/app: the consent page at /account/authorize, which forma2 rendered on the server and answered through Better Auth's /oauth2/consent, and the profile page's connected MCP clients, forma2's getOAuthConsents and deleteOAuthConsent.
type AppHandler struct {
	server   *Handler
	sessions httpapi.SessionAuth
}

func NewAppHandler(server *Handler, sessions httpapi.SessionAuth) *AppHandler {
	return &AppHandler{server: server, sessions: sessions}
}

// Register mounts the routes on the /api/app group, which applies the error envelope and the CSRF check.
func (h *AppHandler) Register(r gin.IRouter) {
	g := r.Group("/oauth", h.sessions.Require())
	g.GET("/authorization-requests/:requestId", h.authorizationRequest)
	g.POST("/authorization-requests/:requestId/consent", h.consent)
	g.GET("/consents", h.consents)
	g.DELETE("/consents/:consentId", h.revoke)
}

// clientView is a client's public metadata, forma2's getOAuthClientPublic.
type clientView struct {
	ClientID string  `json:"clientId"`
	Name     string  `json:"name"`
	URI      *string `json:"uri"`
	LogoURI  *string `json:"logoUri"`
}

type authorizationRequestView struct {
	RequestID   uuid.UUID  `json:"requestId"`
	Client      clientView `json:"client"`
	RedirectURI string     `json:"redirectUri"`
	Scopes      []string   `json:"scopes"`
	Resource    string     `json:"resource"`
	// ConsentRequired is false when the user already approved every scope and the resource for this client, so the page may continue without asking again.
	ConsentRequired bool `json:"consentRequired"`
}

// pending is the request in the path if it is still waiting for consent, or 404.
func (h *AppHandler) pending(c *gin.Context) (*AuthRequest, error) {
	id, err := httpapi.ParamUUID(c, "requestId")
	if err != nil {
		return nil, err
	}
	req, err := h.server.repo.authRequest(c.Request.Context(), id)
	if err != nil {
		return nil, err
	}
	if req == nil || req.Done() {
		return nil, apperr.NotFound("Authorization request", id.String())
	}
	return req, nil
}

func (h *AppHandler) authorizationRequest(c *gin.Context) {
	req, err := h.pending(c)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	actor, _ := httpapi.ActorFrom(c.Request.Context())
	client, err := h.server.repo.client(c.Request.Context(), req.clientID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	if client == nil || client.disabled {
		httpapi.Fail(c, apperr.NotFound("Authorization request", req.id.String()))
		return
	}
	covered, err := h.server.covered(c, actor.UserID, req)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, authorizationRequestView{
		RequestID: req.id, Client: clientView{ClientID: client.id, Name: client.name, URI: client.clientURI, LogoURI: client.logoURI},
		RedirectURI: req.redirectURI, Scopes: req.scopes, Resource: req.resource,
		ConsentRequired: !covered || slices.Contains(req.prompt, "consent"),
	})
}

type consentInput struct {
	Accept *bool `json:"accept" binding:"required"`
}

// consentResult is where the browser goes next, the shape Better Auth's /oauth2/consent answered with.
type consentResult struct {
	Redirect bool   `json:"redirect"`
	URL      string `json:"url"`
}

// consent answers a pending request: approving it binds it to the user, adds the scopes and resource to their consent, and continues to the client with a code; denying it sends the client access_denied.
func (h *AppHandler) consent(c *gin.Context) {
	var in consentInput
	if err := httpapi.BindJSON(c, &in); err != nil {
		httpapi.Fail(c, err)
		return
	}
	req, err := h.pending(c)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	ctx := c.Request.Context()
	if !*in.Accept {
		if err := h.server.repo.deleteAuthRequest(ctx, req.id); err != nil {
			httpapi.Fail(c, err)
			return
		}
		c.JSON(http.StatusOK, consentResult{Redirect: true, URL: errorRedirect(req, "access_denied")})
		return
	}
	actor, _ := httpapi.ActorFrom(ctx)
	bound, err := h.server.repo.bindAuthRequest(ctx, req.id, actor.UserID, time.Now())
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	if !bound {
		httpapi.Fail(c, apperr.NotFound("Authorization request", req.id.String()))
		return
	}
	if err := h.server.repo.addConsent(ctx, actor.UserID, req.clientID, req.scopes, req.resource); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, consentResult{Redirect: true, URL: h.server.callbackURL(req.id)})
}

type consentView struct {
	ID        uuid.UUID  `json:"id"`
	Client    clientView `json:"client"`
	Scopes    []string   `json:"scopes"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

func (h *AppHandler) consents(c *gin.Context) {
	actor, _ := httpapi.ActorFrom(c.Request.Context())
	list, err := h.server.repo.consents(c.Request.Context(), "c.user_id = ?", actor.UserID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	out := make([]consentView, len(list))
	for i, x := range list {
		out[i] = consentView{
			ID: x.ID, Client: clientView{ClientID: x.ClientID, Name: x.ClientName, URI: x.ClientURI, LogoURI: x.LogoURI},
			Scopes: x.Scopes, CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt,
		}
	}
	c.JSON(http.StatusOK, out)
}

// revoke deletes one of the user's consents and the client's tokens. An access token already handed out stays usable at /api/mcp until it expires, within 15 minutes, as in forma2.
func (h *AppHandler) revoke(c *gin.Context) {
	id, err := httpapi.ParamUUID(c, "consentId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	actor, _ := httpapi.ActorFrom(c.Request.Context())
	found, err := h.server.repo.deleteConsent(c.Request.Context(), actor.UserID, id)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	if !found {
		httpapi.Fail(c, apperr.NotFound("Consent", id.String()))
		return
	}
	c.Status(http.StatusNoContent)
}
