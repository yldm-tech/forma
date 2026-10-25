package mgmtapi

import (
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/apikey"
	"github.com/yldm-tech/forma/apps/server/internal/authz"
)

// healthTimeout bounds each dependency check of /api/v2/health.
const healthTimeout = 2 * time.Second

// v2Health is forma2's GET /api/v2/health: unauthenticated and uncharged, whether the database and the cache answer. A dependency that does not is reported false with a 200.
func (h *Handler) v2Health(c *gin.Context) {
	check := func(ping func(ctx context.Context) error) bool {
		ctx, cancel := context.WithTimeout(c.Request.Context(), healthTimeout)
		defer cancel()
		return ping(ctx) == nil
	}
	database := check(func(ctx context.Context) error { return h.db.WithContext(ctx).Exec(`SELECT 1`).Error })
	cache := check(func(ctx context.Context) error { return h.cache.Ping(ctx).Err() })
	v2OK(c, map[string]any{"main_database": database, "cache_database": cache}, nil)
}

// v2Roles is forma2's GET /api/v2/roles: the organization roles a user can hold, billing only on Forma Cloud.
func (h *Handler) v2Roles(c *gin.Context) {
	if !h.v2Limit(c) {
		return
	}
	roles := []string{"owner", "manager", "member"}
	if h.cfg.IsFormaCloud {
		roles = append(roles, "billing")
	}
	v2OK(c, roles, nil)
}

// v2Me is forma2's GET /api/v2/me: what the key may do, for a key with read access to its organization's access control (401 otherwise). Its workspace grants are those SpiceDB confirms and whose workspaces still exist in its organization. environmentPermissions stays in the payload for the clients that read it, always empty: a workspace has no environments.
func (h *Handler) v2Me(c *gin.Context) {
	if !h.v2Limit(c) {
		return
	}
	ctx := c.Request.Context()
	p := v2Principal(c)
	allowed, err := h.authz.Can(ctx, p.Actor(), authz.OrganizationReadAccess, authz.Organization(p.OrganizationID))
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	if !allowed {
		h.log.WarnContext(ctx, "API key without read access to its organization asked for /api/v2/me", "organizationId", p.OrganizationID.String())
		v2Error(c, http.StatusUnauthorized, nil)
		return
	}
	readable, err := h.authz.LookupWorkspaceIDs(ctx, p.Actor(), false)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	var ids []uuid.UUID
	for _, g := range p.WorkspacePermissions {
		if slices.Contains(readable, g.WorkspaceID) {
			ids = append(ids, g.WorkspaceID)
		}
	}
	found, err := h.repo.workspaces(ctx, ids)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	type workspacePermission struct {
		Permissions   string    `json:"permissions"`
		WorkspaceID   uuid.UUID `json:"workspaceId"`
		WorkspaceName string    `json:"workspaceName"`
	}
	workspaces := []workspacePermission{}
	for _, g := range p.WorkspacePermissions {
		w, ok := found[g.WorkspaceID]
		if !ok || w.OrganizationID != p.OrganizationID {
			continue
		}
		workspaces = append(workspaces, workspacePermission{Permissions: g.Permission, WorkspaceID: w.ID, WorkspaceName: w.Name})
	}
	v2OK(c, struct {
		WorkspacePermissions   []workspacePermission     `json:"workspacePermissions"`
		EnvironmentPermissions []struct{}                `json:"environmentPermissions"`
		OrganizationID         uuid.UUID                 `json:"organizationId"`
		OrganizationAccess     apikey.OrganizationAccess `json:"organizationAccess"`
	}{workspaces, []struct{}{}, p.OrganizationID, p.OrganizationAccess}, nil)
}
