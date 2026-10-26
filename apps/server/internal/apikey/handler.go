package apikey

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/jsonschema"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
)

// organizationAccessSchema is the exported JSON Schema of forma2's ZOrganizationAccess.
const organizationAccessSchema = "api_key.organization_access"

// Handler serves /api/app/organizations/:organizationId/api-keys.
type Handler struct {
	svc      *Service
	sessions httpapi.SessionAuth
	schemas  *jsonschema.Validator
}

func NewHandler(svc *Service, sessions httpapi.SessionAuth, schemas *jsonschema.Validator) *Handler {
	return &Handler{svc: svc, sessions: sessions, schemas: schemas}
}

// Register mounts the routes on the /api/app group, which already applies the error envelope and the CSRF check to every mutation.
func (h *Handler) Register(r gin.IRouter) {
	g := r.Group("/organizations/:organizationId/api-keys", h.sessions.Require())
	g.GET("", h.list)
	g.POST("", h.create)
	g.PATCH("/:apiKeyId", h.update)
	g.DELETE("/:apiKeyId", h.delete)
}

// actor is the authenticated caller; every route is behind SessionAuth.Require.
func actor(c *gin.Context) httpapi.Actor {
	a, _ := httpapi.ActorFrom(c.Request.Context())
	return a
}

func (h *Handler) list(c *gin.Context) {
	orgID, err := httpapi.ParamUUID(c, "organizationId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	keys, err := h.svc.List(c.Request.Context(), actor(c), orgID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, keys)
}

type createRequest struct {
	Label                string              `json:"label"`
	WorkspacePermissions []permissionRequest `json:"workspacePermissions" binding:"omitempty,dive"`
	OrganizationAccess   json.RawMessage     `json:"organizationAccess"`
	// ExpiresAt is an RFC 3339 time, read by input so that a malformed one is a field error; absent or null for a key that never expires.
	ExpiresAt *string `json:"expiresAt"`
}

type permissionRequest struct {
	WorkspaceID string `json:"workspaceId" binding:"required,uuid"`
	Permission  string `json:"permission" binding:"required,oneof=read write manage"`
}

// latestExpiry bounds an expiry from above: before year 9999 in UTC, so that no time zone (at most 14 hours ahead) renders it past year 9999, the last year encoding/json can write a time in. A later one would be stored, then fail the create response and every list of the organization's keys.
var latestExpiry = time.Date(9999, time.January, 1, 0, 0, 0, 0, time.UTC)

// input applies forma2's ZApiKeyCreateInput (organization access as its JSON Schema, no workspace twice) and the rules forma2 left to its form: a label that is not blank, and at least one grant of any kind. An expiry must be an RFC 3339 time after now and before latestExpiry.
func (r *createRequest) input(schemas *jsonschema.Validator, now time.Time) (CreateInput, error) {
	var fields []apperr.FieldError
	if strings.TrimSpace(r.Label) == "" {
		fields = append(fields, apperr.FieldError{Field: "label", Rule: "required"})
	}
	var expiresAt *time.Time
	if r.ExpiresAt != nil {
		at, err := time.Parse(time.RFC3339Nano, *r.ExpiresAt)
		switch {
		case err != nil:
			fields = append(fields, apperr.FieldError{Field: "expiresAt", Rule: "datetime"})
		case !at.After(now):
			fields = append(fields, apperr.FieldError{Field: "expiresAt", Rule: "future"})
		case !at.Before(latestExpiry):
			fields = append(fields, apperr.FieldError{Field: "expiresAt", Rule: "max"})
		default:
			// PostgreSQL keeps microseconds; the view the create returns is read back from the row.
			at = at.UTC()
			expiresAt = &at
		}
	}
	var access OrganizationAccess
	switch {
	case len(r.OrganizationAccess) == 0 || bytes.Equal(r.OrganizationAccess, []byte("null")):
		fields = append(fields, apperr.FieldError{Field: "organizationAccess", Rule: "required"})
	case schemas.Validate(organizationAccessSchema, r.OrganizationAccess) != nil:
		fields = append(fields, apperr.FieldError{Field: "organizationAccess", Rule: "schema"})
	default:
		// Validated above; unknown top-level keys are dropped here, as zod's object schema strips them.
		if err := json.Unmarshal(r.OrganizationAccess, &access); err != nil {
			fields = append(fields, apperr.FieldError{Field: "organizationAccess", Rule: "schema"})
		}
	}
	if len(fields) > 0 {
		return CreateInput{}, apperr.Validation(fields)
	}

	in := CreateInput{Label: r.Label, OrganizationAccess: access, ExpiresAt: expiresAt}
	seen := make(map[uuid.UUID]bool, len(r.WorkspacePermissions))
	for _, p := range r.WorkspacePermissions {
		// The binding tags have checked the format.
		id := uuid.MustParse(p.WorkspaceID)
		if seen[id] {
			e := apperr.Validation([]apperr.FieldError{{Field: "workspacePermissions", Rule: "unique"}})
			e.Message = "Duplicate workspace permissions are not allowed"
			return CreateInput{}, e
		}
		seen[id] = true
		in.WorkspacePermissions = append(in.WorkspacePermissions, WorkspacePermissionInput{WorkspaceID: id, Permission: p.Permission})
	}
	if len(in.WorkspacePermissions) == 0 && !access.grantsAnything() {
		e := apperr.Validation([]apperr.FieldError{{Field: "workspacePermissions", Rule: "required"}})
		e.Message = "An API key needs access to at least one workspace or to the organization"
		return CreateInput{}, e
	}
	return in, nil
}

func (h *Handler) create(c *gin.Context) {
	orgID, err := httpapi.ParamUUID(c, "organizationId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	var req createRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	in, err := req.input(h.schemas, time.Now())
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	created, err := h.svc.Create(c.Request.Context(), actor(c), orgID, in)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	// The plaintext key must not be cached anywhere on its way to the browser.
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, created)
}

type updateRequest struct {
	Label string `json:"label"`
}

func (h *Handler) update(c *gin.Context) {
	orgID, keyID, ok := keyParams(c)
	if !ok {
		return
	}
	var req updateRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	if strings.TrimSpace(req.Label) == "" {
		httpapi.Fail(c, apperr.Validation([]apperr.FieldError{{Field: "label", Rule: "required"}}))
		return
	}
	view, err := h.svc.UpdateLabel(c.Request.Context(), actor(c), orgID, keyID, req.Label)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}

func (h *Handler) delete(c *gin.Context) {
	orgID, keyID, ok := keyParams(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), actor(c), orgID, keyID); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func keyParams(c *gin.Context) (orgID, keyID uuid.UUID, ok bool) {
	orgID, err := httpapi.ParamUUID(c, "organizationId")
	if err == nil {
		keyID, err = httpapi.ParamUUID(c, "apiKeyId")
	}
	if err != nil {
		httpapi.Fail(c, err)
		return uuid.Nil, uuid.Nil, false
	}
	return orgID, keyID, true
}
