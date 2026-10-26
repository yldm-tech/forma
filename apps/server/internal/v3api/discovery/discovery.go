// Package discovery serves what a caller of the v3 management API needs to find its way: GET /api/v3/me, which says who the credentials belong to and which organizations and workspaces they reach, and GET /api/v3/openapi.json, the OpenAPI document of every v3 operation an API key can call.
package discovery

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/actionclass"
	"github.com/yldm-tech/forma/apps/server/internal/apikey"
	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/contact"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/survey"
	"github.com/yldm-tech/forma/apps/server/internal/v3api"
	"github.com/yldm-tech/forma/apps/server/internal/v3api/openapi"
	"github.com/yldm-tech/forma/apps/server/internal/workflow"
)

// documentCacheControl lets clients and proxies keep the document for a few minutes: it changes only with a release.
const documentCacheControl = "public, max-age=300"

// Handler serves GET /api/v3/me and GET /api/v3/openapi.json.
type Handler struct {
	db       *gorm.DB
	authz    *authz.Authorizer
	v3       *v3api.Middleware
	document []byte
}

func NewHandler(cfg config.Config, db *gorm.DB, authorizer *authz.Authorizer, v3 *v3api.Middleware) (*Handler, error) {
	serverURL := ""
	if cfg.WebAppURL != nil {
		serverURL = cfg.WebAppURL.String()
	}
	doc, err := Document(serverURL)
	if err != nil {
		return nil, err
	}
	return &Handler{db: db, authz: authorizer, v3: v3, document: doc}, nil
}

// Document is the OpenAPI document of the v3 API on the instance at serverURL.
func Document(serverURL string) ([]byte, error) {
	return openapi.Document(serverURL, Spec(), survey.V3OpenAPI(), workflow.V3OpenAPI(), contact.V3OpenAPI(), actionclass.V3OpenAPI())
}

// RegisterV3 mounts the routes on the /api/v3 group, which renders failures as problems.
func (h *Handler) RegisterV3(r gin.IRouter) {
	// A key with only organization access is answered too: /me shows nothing beyond the key's own grants, and it is the check the API Keys page suggests for every new key.
	r.GET("/me", h.v3.Auth(v3api.AuthBothOrganizationKey), h.me)
	r.GET("/openapi.json", h.openAPI)
}

// openAPI serves the document to anyone: it holds nothing an instance keeps private, and documentation tools on other origins read it.
func (h *Handler) openAPI(c *gin.Context) {
	c.Header("Cache-Control", documentCacheControl)
	c.Header("Access-Control-Allow-Origin", "*")
	c.Data(http.StatusOK, "application/json; charset=utf-8", h.document)
}

// Me is who a v3 request acts as, and what it can reach.
type Me struct {
	Type          string         `json:"type" jsonschema:"What the request authenticated with: an API key, or a signed-in user's session."`
	APIKey        *MeAPIKey      `json:"apiKey" jsonschema:"The API key, when the request carried one."`
	User          *MeUser        `json:"user" jsonschema:"The signed-in user, when the request carried no API key."`
	Organizations []Organization `json:"organizations" jsonschema:"The organizations the caller belongs to: an API key's own organization, or the user's memberships."`
	Workspaces    []Workspace    `json:"workspaces" jsonschema:"The workspaces the caller may use, by name, with the most it may do in each. Empty for an API key with only organization access."`
}

// MeAPIKey is the key a request authenticated with.
type MeAPIKey struct {
	ID    uuid.UUID `json:"id"`
	Label string    `json:"label"`
	// OrganizationAccess is the key's access to the organization's members and teams (the v2 organization routes).
	OrganizationAccess apikey.OrganizationAccess `json:"organizationAccess"`
	ExpiresAt          *time.Time                `json:"expiresAt" jsonschema:"When the key stops working, chosen when it was created; null for a key that works until it is deleted."`
}

// MeUser is the signed-in user.
type MeUser struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email"`
}

// Organization is one organization of the caller.
type Organization struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Role *string   `json:"role" jsonschema:"The user's role in the organization; null for an API key."`
}

// Workspace is one workspace the caller may use.
type Workspace struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	OrganizationID uuid.UUID `json:"organizationId"`
	Permission     string    `json:"permission" jsonschema:"The most the caller may do in the workspace: read, write (read and change) or manage (write and administer)."`
}

const (
	typeAPIKey = "apiKey"
	typeUser   = "user"
)

// Spec describes this package's operations.
func Spec() openapi.Spec {
	me := openapi.NotNull(openapi.Infer[Me](), "organizations", "workspaces")
	me.Properties["type"].Enum = []any{typeAPIKey, typeUser}
	workspace := me.Properties["workspaces"].Items
	workspace.Properties["permission"].Enum = []any{apikey.PermissionRead, apikey.PermissionWrite, apikey.PermissionManage}
	return openapi.Spec{
		Schemas: map[string]*openapi.Schema{"Me": me},
		Operations: []openapi.Operation{
			{
				Method: http.MethodGet, Path: "/api/v3/me", ID: "getMe", Tag: "Discovery",
				Summary:     "Who am I",
				Description: "The API key (or signed-in user) the request authenticated as, when the key expires, its organization and the workspaces it may use with their permission. A key with only organization access (to members and teams) is answered as well, with no workspaces. Start here to find the workspaceId the other operations take.",
				Responses:   []openapi.Response{{Status: http.StatusOK, Description: "The caller.", Schema: openapi.Data(openapi.Ref("Me"))}},
			},
			{
				Method: http.MethodGet, Path: "/api/v3/openapi.json", ID: "getOpenAPIDocument", Tag: "Discovery", Public: true,
				Summary:     "This OpenAPI document",
				Description: "The OpenAPI 3.1 document of every v3 operation an API key can call. It needs no credentials.",
				Responses:   []openapi.Response{{Status: http.StatusOK, Description: "The document.", Schema: &openapi.Schema{Type: "object"}}},
			},
		},
	}
}

func (h *Handler) me(c *gin.Context) {
	ctx := c.Request.Context()
	p := v3api.PrincipalFrom(ctx)
	var (
		out *Me
		err error
	)
	if p.APIKey != nil {
		out, err = h.keyMe(ctx, p.APIKey)
	} else {
		out, err = h.userMe(ctx, p.UserID)
	}
	if err != nil {
		v3api.Fail(c, err)
		return
	}
	v3api.OK(c, out)
}

// keyMe reads the key's grants as authentication resolved them: only its own organization's workspaces, each with the permission granted.
func (h *Handler) keyMe(ctx context.Context, p *apikey.Principal) (*Me, error) {
	var row struct {
		Label            string
		ExpiresAt        *time.Time
		OrganizationName string
	}
	err := h.db.WithContext(ctx).Raw(`SELECT k.label, k.expires_at, o.name AS organization_name FROM api_keys k JOIN organizations o ON o.id = k.organization_id WHERE k.id = ?`, p.APIKeyID).Scan(&row).Error
	if err != nil {
		return nil, fmt.Errorf("load API key: %w", err)
	}
	workspaces := make([]Workspace, len(p.WorkspacePermissions))
	for i, g := range p.WorkspacePermissions {
		workspaces[i] = Workspace{ID: g.WorkspaceID, Name: g.WorkspaceName, OrganizationID: p.OrganizationID, Permission: g.Permission}
	}
	sortWorkspaces(workspaces)
	return &Me{
		Type:          typeAPIKey,
		APIKey:        &MeAPIKey{ID: p.APIKeyID, Label: row.Label, OrganizationAccess: p.OrganizationAccess, ExpiresAt: row.ExpiresAt},
		Organizations: []Organization{{ID: p.OrganizationID, Name: row.OrganizationName}},
		Workspaces:    workspaces,
	}, nil
}

// userMe lists the user's memberships and the workspaces SpiceDB lets them read within those organizations. Owners and managers manage every workspace of their organization; anyone else gets what their teams grant.
func (h *Handler) userMe(ctx context.Context, userID uuid.UUID) (*Me, error) {
	db := h.db.WithContext(ctx)
	var user MeUser
	if err := db.Raw(`SELECT id, name, email FROM users WHERE id = ?`, userID).Scan(&user).Error; err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	var memberships []struct {
		ID   uuid.UUID
		Name string
		Role string
	}
	if err := db.Raw(`SELECT o.id, o.name, m.role FROM memberships m JOIN organizations o ON o.id = m.organization_id WHERE m.user_id = ?`, userID).Scan(&memberships).Error; err != nil {
		return nil, fmt.Errorf("load memberships: %w", err)
	}
	organizations := make([]Organization, len(memberships))
	roles := map[uuid.UUID]string{}
	for i, m := range memberships {
		organizations[i] = Organization{ID: m.ID, Name: m.Name, Role: &memberships[i].Role}
		roles[m.ID] = m.Role
	}
	collator := collate.New(language.Und)
	slices.SortFunc(organizations, func(a, b Organization) int {
		if c := collator.CompareString(a.Name, b.Name); c != 0 {
			return c
		}
		return slices.Compare(a.ID[:], b.ID[:])
	})

	actor := authz.User(userID)
	readable, err := h.authz.LookupWorkspaceIDs(ctx, actor, false)
	if err != nil {
		return nil, err
	}
	workspaces := []Workspace{}
	if len(readable) > 0 && len(roles) > 0 {
		if err := db.Raw(`SELECT id, name, organization_id FROM workspaces WHERE id IN ? AND organization_id IN ?`, readable, slices.Collect(maps.Keys(roles))).Scan(&workspaces).Error; err != nil {
			return nil, fmt.Errorf("load workspaces: %w", err)
		}
	}
	writable, err := h.authz.LookupWorkspaceIDs(ctx, actor, true)
	if err != nil {
		return nil, err
	}
	for i := range workspaces {
		w := &workspaces[i]
		switch role := roles[w.OrganizationID]; {
		case role == "owner" || role == "manager":
			w.Permission = apikey.PermissionManage
		case !slices.Contains(writable, w.ID):
			w.Permission = apikey.PermissionRead
		default:
			manages, err := h.authz.Can(ctx, actor, authz.WorkspaceManage, authz.Workspace(w.ID))
			if err != nil {
				return nil, err
			}
			w.Permission = apikey.PermissionWrite
			if manages {
				w.Permission = apikey.PermissionManage
			}
		}
	}
	sortWorkspaces(workspaces)
	return &Me{Type: typeUser, User: &user, Organizations: organizations, Workspaces: workspaces}, nil
}

// sortWorkspaces orders workspaces as MCP's list_workspaces does: by name in the root collation, then by id.
func sortWorkspaces(ws []Workspace) {
	collator := collate.New(language.Und)
	slices.SortFunc(ws, func(a, b Workspace) int {
		if c := collator.CompareString(a.Name, b.Name); c != 0 {
			return c
		}
		return slices.Compare(a.ID[:], b.ID[:])
	})
}
