package mgmtapi

import (
	"context"
	"errors"
	"math"
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/apikey"
	"github.com/yldm-tech/forma/server/internal/mgmtapi/mgmtv1"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/v1api"
	"github.com/yldm-tech/forma/server/internal/response"
)

// maxItemsPerPage is forma2's MAX_ITEMS_PER_PAGE, the largest page a v1 collection returns.
const maxItemsPerPage = 250

// messageInvalidFields is forma2's answer to a body that fails its schema.
const messageInvalidFields = "Fields are missing or incorrectly formatted"

func (h *Handler) registerV1(engine gin.IRouter) {
	key := h.v1.APIKey(apikey.Options{}, ratelimit.APIV1)
	orgKey := h.v1.APIKey(apikey.Options{AllowOrganizationOnly: true}, ratelimit.APIV1)
	audited := h.v1.Audited
	storage := append(h.v1.APIKeyOrSession(h.sessions.Optional(), ratelimit.StorageUpload), h.v1StorageUpload)
	mount(engine, "/api/v1/management", nil, []route{
		r(http.MethodGet, "/me", h.v1Me),

		r(http.MethodGet, "/surveys", orgKey, h.v1ListSurveys),
		r(http.MethodPost, "/surveys", key, audited(audit.ActionCreated, audit.TargetSurvey, h.v1CreateSurvey)),
		r(http.MethodGet, "/surveys/:surveyId", key, h.v1GetSurvey),
		r(http.MethodPut, "/surveys/:surveyId", key, audited(audit.ActionUpdated, audit.TargetSurvey, h.v1UpdateSurvey)),
		r(http.MethodDelete, "/surveys/:surveyId", key, audited(audit.ActionDeleted, audit.TargetSurvey, h.v1DeleteSurvey)),
		r(http.MethodGet, "/surveys/:surveyId/singleUseIds", key, h.v1SingleUseIDs),

		r(http.MethodGet, "/responses", key, h.v1ListResponses),
		r(http.MethodPost, "/responses", key, audited(audit.ActionCreated, audit.TargetResponse, h.v1CreateResponse)),
		r(http.MethodGet, "/responses/:responseId", key, h.v1GetResponse),
		r(http.MethodPut, "/responses/:responseId", key, audited(audit.ActionUpdated, audit.TargetResponse, h.v1UpdateResponse)),
		r(http.MethodDelete, "/responses/:responseId", key, audited(audit.ActionDeleted, audit.TargetResponse, h.v1DeleteResponse)),

		r(http.MethodGet, "/contacts", key, h.v1ListContacts),
		r(http.MethodGet, "/contacts/:contactId", key, h.v1GetContact),
		r(http.MethodDelete, "/contacts/:contactId", key, audited(audit.ActionDeleted, audit.TargetContact, h.v1DeleteContact)),
		r(http.MethodGet, "/contact-attributes", key, h.v1ListContactAttributes),

		r(http.MethodGet, "/contact-attribute-keys", key, h.v1ListKeys),
		r(http.MethodPost, "/contact-attribute-keys", key, audited(audit.ActionCreated, audit.TargetContactAttributeKey, h.v1CreateKey)),
		r(http.MethodGet, "/contact-attribute-keys/:contactAttributeKeyId", key, h.v1GetKey),
		r(http.MethodPut, "/contact-attribute-keys/:contactAttributeKeyId", key, audited(audit.ActionUpdated, audit.TargetContactAttributeKey, h.v1UpdateKey)),
		r(http.MethodDelete, "/contact-attribute-keys/:contactAttributeKeyId", key, audited(audit.ActionDeleted, audit.TargetContactAttributeKey, h.v1DeleteKey)),

		r(http.MethodGet, "/action-classes", key, h.v1ListActionClasses),
		r(http.MethodPost, "/action-classes", key, audited(audit.ActionCreated, audit.TargetActionClass, h.v1CreateActionClass)),
		r(http.MethodGet, "/action-classes/:actionClassId", key, h.v1GetActionClass),
		r(http.MethodPut, "/action-classes/:actionClassId", key, audited(audit.ActionUpdated, audit.TargetActionClass, h.v1UpdateActionClass)),
		r(http.MethodDelete, "/action-classes/:actionClassId", key, audited(audit.ActionDeleted, audit.TargetActionClass, h.v1DeleteActionClass)),

		r(http.MethodPost, "/storage", storage...),
	})
}

// grantedWorkspaces are the distinct workspaces the request's API key holds a grant on, in grant order.
func grantedWorkspaces(c *gin.Context) []uuid.UUID {
	var ids []uuid.UUID
	for _, g := range mgmtv1.PrincipalOf(c).WorkspacePermissions {
		if !slices.Contains(ids, g.WorkspaceID) {
			ids = append(ids, g.WorkspaceID)
		}
	}
	return ids
}

// v1Pagination is forma2's parsePaginationParams: limit (an integer from 1 to 250, defaultLimit when absent or empty) and the route's offset parameter (an integer of 0 or more). It answers 400 with every failing parameter itself.
func v1Pagination(c *gin.Context, defaultLimit int, offsetParam string) (limit, offset int, ok bool) {
	details := map[string]any{}
	limit = defaultLimit
	if raw := c.Query("limit"); raw != "" {
		n := jsvalue.ParseNumber(raw)
		if n != math.Trunc(n) || n < 1 || n > maxItemsPerPage {
			details["limit"] = "limit must be an integer between 1 and 250"
		} else {
			limit = int(n)
		}
	}
	if raw := c.Query(offsetParam); raw != "" {
		n := jsvalue.ParseNumber(raw)
		if n != math.Trunc(n) || n < 0 || math.IsInf(n, 0) {
			details[offsetParam] = offsetParam + " must be an integer of 0 or greater"
		} else {
			offset = int(n)
		}
	}
	if len(details) > 0 {
		v1api.BadRequest(c, "Invalid pagination parameters", details, true)
		return 0, 0, false
	}
	return limit, offset, true
}

// v1ID reads an id from the path as forma2's validateInputs read a ZId: a UUID, or a forma2 cuid resolved to the row migrated from it; anything else is 400 "Validation failed: Invalid cuid2".
func v1ID(c *gin.Context, param string) (uuid.UUID, bool) {
	raw := c.Param(param)
	id, err := response.ResolveID(raw)
	if err != nil {
		v1api.BadRequest(c, "Validation failed: Invalid cuid2", nil, false)
		return uuid.Nil, false
	}
	return id, true
}

// v1LooseID reads an id the route never validated: any string, resolved as a forma2 id when it is not a UUID, so an unknown one is simply not found.
func v1LooseID(c *gin.Context, param string) uuid.UUID {
	id, _ := legacyid.Resolve(c.Param(param))
	return id
}

// v1Fail answers an error the handler did not expect to answer otherwise, the way forma2's handleApiError mapped its error classes: invalid input 400, a refusal 403, a duplicate 409, a rate limit 429, anything else 500 "Something went wrong. Please try again.".
func (h *Handler) v1Fail(c *gin.Context, err error) {
	if e, ok := apperr.As(err); ok {
		switch e.Status {
		case http.StatusBadRequest:
			v1api.BadRequest(c, e.Message, nil, false)
			return
		case http.StatusUnauthorized:
			v1api.NotAuthenticated(c)
			return
		case http.StatusForbidden:
			v1api.Forbidden(c, e.Message, false)
			return
		case http.StatusConflict:
			v1api.Conflict(c, e.Message, false)
			return
		case http.StatusTooManyRequests:
			v1api.TooManyRequests(c, e.Message)
			return
		}
	}
	h.v1.Unexpected(c, err, mgmtv1.MessageGeneric)
}

// environmentIDs is forma2's addLegacyEnvironmentIdToList lookup: each workspace's legacy environment id, or its own id when it has none.
func (h *Handler) environmentIDs(ctx context.Context, workspaceIDs []uuid.UUID) (map[uuid.UUID]string, error) {
	return h.repo.environmentIDs(ctx, workspaceIDs)
}

// environmentID is one workspace's environment id.
func (h *Handler) environmentID(ctx context.Context, workspaceID uuid.UUID) (string, error) {
	ids, err := h.environmentIDs(ctx, []uuid.UUID{workspaceID})
	if err != nil {
		return "", err
	}
	if id, ok := ids[workspaceID]; ok {
		return id, nil
	}
	return workspaceID.String(), nil
}

// bestEffortEnvironmentID is forma2's addLegacyEnvironmentIdBestEffort, for the answer to a write that has already committed: a failed lookup leaves environmentId out rather than turning the write into an error the caller would retry.
func (h *Handler) bestEffortEnvironmentID(c *gin.Context, obj map[string]any, workspaceID uuid.UUID) {
	env, err := h.environmentID(c.Request.Context(), workspaceID)
	if err != nil {
		h.log.ErrorContext(c.Request.Context(), "Failed to resolve legacy environmentId for a committed write", "err", err, "workspace_id", workspaceID)
		return
	}
	obj["environmentId"] = env
}

// errNotFound is a resource the request named that does not exist.
var errNotFound = errors.New("not found")
