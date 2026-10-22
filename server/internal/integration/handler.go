package integration

import (
	"encoding/json"
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
)

// Handler serves the integrations pages' routes under /api/app: the overview, the surveys a link can name, connecting, saving and deleting, and the provider lookups the link modals make (spreadsheet names, Airtable bases and tables, Notion databases, Slack channels).
type Handler struct {
	svc      *Service
	sessions httpapi.SessionAuth
}

func NewHandler(svc *Service, sessions httpapi.SessionAuth) *Handler {
	return &Handler{svc: svc, sessions: sessions}
}

// Register mounts the routes on the /api/app group, which applies the error envelope and the CSRF check.
func (h *Handler) Register(r gin.IRouter) {
	g := r.Group("", h.sessions.Require())
	g.GET("/workspaces/:workspaceId/integrations", h.list)
	g.GET("/workspaces/:workspaceId/integrations/surveys", h.surveys)
	g.POST("/workspaces/:workspaceId/integrations/:type/connect", h.connect)
	g.PUT("/workspaces/:workspaceId/integrations/:type", h.save)
	g.DELETE("/integrations/:integrationId", h.delete)
	g.GET("/workspaces/:workspaceId/integrations/googleSheets/validate", h.validateGoogleSheets)
	g.GET("/workspaces/:workspaceId/integrations/googleSheets/spreadsheets/:spreadsheetId", h.spreadsheet)
	g.GET("/workspaces/:workspaceId/integrations/airtable/bases", h.airtableBases)
	g.GET("/workspaces/:workspaceId/integrations/airtable/bases/:baseId/tables", h.airtableTables)
	g.GET("/workspaces/:workspaceId/integrations/notion/databases", h.notionDatabases)
	g.GET("/workspaces/:workspaceId/integrations/slack/channels", h.slackChannels)
}

func actor(c *gin.Context) httpapi.Actor {
	a, _ := httpapi.ActorFrom(c.Request.Context())
	return a
}

// respond writes out as 200, or the error.
func respond[T any](c *gin.Context, out T, err error) {
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func workspaceParam(c *gin.Context) (uuid.UUID, bool) {
	ws, err := httpapi.ParamUUID(c, "workspaceId")
	if err != nil {
		httpapi.Fail(c, err)
		return uuid.Nil, false
	}
	return ws, true
}

// typeParam reads the integration type of the path; an unknown one is 404.
func typeParam(c *gin.Context) (string, bool) {
	typ := c.Param("type")
	if !slices.Contains(Types, typ) {
		httpapi.Fail(c, apperr.NotFound("Integration type", typ))
		return "", false
	}
	return typ, true
}

func (h *Handler) list(c *gin.Context) {
	ws, ok := workspaceParam(c)
	if !ok {
		return
	}
	out, err := h.svc.List(c.Request.Context(), actor(c).UserID, ws)
	respond(c, out, err)
}

func (h *Handler) surveys(c *gin.Context) {
	ws, ok := workspaceParam(c)
	if !ok {
		return
	}
	out, err := h.svc.Surveys(c.Request.Context(), actor(c).UserID, ws)
	respond(c, out, err)
}

// AuthorizeResponse is where to send the browser to connect, forma2's {authUrl}.
type AuthorizeResponse struct {
	AuthURL string `json:"authUrl"`
}

func (h *Handler) connect(c *gin.Context) {
	ws, ok := workspaceParam(c)
	if !ok {
		return
	}
	typ, ok := typeParam(c)
	if !ok {
		return
	}
	u, err := h.svc.Authorize(c.Request.Context(), actor(c), ws, typ)
	respond(c, AuthorizeResponse{AuthURL: u}, err)
}

// SaveResponse is the saved integration's id, all forma2's action returned.
type SaveResponse struct {
	ID uuid.UUID `json:"id"`
}

// linksInput is the body of a save: every link of the integration, as the modals keep them.
type linksInput[T any] struct {
	Data []T `json:"data" binding:"required,dive"`
}

// bindLinks reads and validates the links of one integration type and returns the config to store and the surveys they name.
func bindLinks[T linkOf](c *gin.Context) (json.RawMessage, []string, error) {
	var in linksInput[T]
	if err := httpapi.BindJSON(c, &in); err != nil {
		return nil, nil, err
	}
	surveys := make([]string, len(in.Data))
	for i, l := range in.Data {
		surveys[i] = l.base().SurveyID
	}
	config, err := encodeLinks(in.Data)
	return config, surveys, err
}

func (h *Handler) save(c *gin.Context) {
	ws, ok := workspaceParam(c)
	if !ok {
		return
	}
	typ, ok := typeParam(c)
	if !ok {
		return
	}
	var config json.RawMessage
	var surveys []string
	var err error
	switch typ {
	case TypeGoogleSheets:
		config, surveys, err = bindLinks[SheetLink](c)
	case TypeAirtable:
		config, surveys, err = bindLinks[AirtableLink](c)
	case TypeNotion:
		config, surveys, err = bindLinks[NotionLink](c)
	case TypeSlack:
		config, surveys, err = bindLinks[SlackLink](c)
	}
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	id, err := h.svc.SaveLinks(c.Request.Context(), actor(c).UserID, ws, typ, config, surveys)
	respond(c, SaveResponse{ID: id}, err)
}

func (h *Handler) delete(c *gin.Context) {
	id, err := httpapi.ParamUUID(c, "integrationId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), actor(c).UserID, id); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ValidateResponse is whether the workspace has a Google Sheets integration whose credential works.
type ValidateResponse struct {
	Connected bool `json:"connected"`
}

func (h *Handler) validateGoogleSheets(c *gin.Context) {
	ws, ok := workspaceParam(c)
	if !ok {
		return
	}
	connected, err := h.svc.ValidateGoogleSheets(c.Request.Context(), actor(c).UserID, ws)
	respond(c, ValidateResponse{Connected: connected}, err)
}

// SpreadsheetResponse is a spreadsheet's title.
type SpreadsheetResponse struct {
	Name string `json:"name"`
}

func (h *Handler) spreadsheet(c *gin.Context) {
	ws, ok := workspaceParam(c)
	if !ok {
		return
	}
	name, err := h.svc.SpreadsheetName(c.Request.Context(), actor(c).UserID, ws, c.Param("spreadsheetId"))
	respond(c, SpreadsheetResponse{Name: name}, err)
}

func (h *Handler) airtableBases(c *gin.Context) {
	ws, ok := workspaceParam(c)
	if !ok {
		return
	}
	out, err := h.svc.AirtableBases(c.Request.Context(), actor(c).UserID, ws)
	respond(c, out, err)
}

func (h *Handler) airtableTables(c *gin.Context) {
	ws, ok := workspaceParam(c)
	if !ok {
		return
	}
	out, err := h.svc.AirtableTables(c.Request.Context(), actor(c).UserID, ws, c.Param("baseId"))
	respond(c, out, err)
}

func (h *Handler) notionDatabases(c *gin.Context) {
	ws, ok := workspaceParam(c)
	if !ok {
		return
	}
	out, err := h.svc.NotionDatabases(c.Request.Context(), actor(c).UserID, ws)
	respond(c, out, err)
}

func (h *Handler) slackChannels(c *gin.Context) {
	ws, ok := workspaceParam(c)
	if !ok {
		return
	}
	out, err := h.svc.SlackChannels(c.Request.Context(), actor(c).UserID, ws)
	respond(c, out, err)
}
