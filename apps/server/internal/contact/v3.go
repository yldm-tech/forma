package contact

import (
	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/apiv3"
	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
)

// V3Handler serves /api/v3/contact-attribute-keys.
type V3Handler struct {
	svc *Service
}

func NewV3Handler(svc *Service) *V3Handler {
	return &V3Handler{svc: svc}
}

// Register mounts the routes on the /api/v3 group, which authenticates the caller and renders failures as problems.
func (h *V3Handler) Register(r gin.IRouter) {
	r.GET("/contact-attribute-keys", h.list)
}

// V3Key is forma2's TV3ContactAttributeKeyResource: what an agent needs to write targeting filters, without isUnique, the workspace or timestamps.
type V3Key struct {
	ID          string  `json:"id"`
	Key         string  `json:"key"`
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Type        string  `json:"type"`
	DataType    string  `json:"dataType"`
}

type v3KeyList struct {
	Data []V3Key        `json:"data"`
	Meta apiv3.ListMeta `json:"meta"`
}

// list is forma2's listV3ContactAttributeKeys: a reader of the workspace, by session or API key, pages through its keys in id order. There is no contacts entitlement gate.
func (h *V3Handler) list(c *gin.Context) {
	q, err := apiv3.ParseWorkspaceListQuery(c)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	ctx := c.Request.Context()
	if err := apiv3.RequireWorkspace(ctx, h.svc.authz, apiv3.PrincipalFrom(ctx), authz.WorkspaceRead, q.WorkspaceID); err != nil {
		httpapi.Fail(c, err)
		return
	}
	keys, err := h.svc.repo.keys(ctx, q.WorkspaceID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	page, next, err := apiv3.Paginate(keys, func(k Key) string { return k.ID.String() }, q.Limit, q.Cursor)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	data := make([]V3Key, len(page))
	for i, k := range page {
		data[i] = V3Key{ID: k.ID.String(), Key: k.Key, Name: k.Name, Description: k.Description, Type: k.Type, DataType: k.DataType}
	}
	apiv3.OK(c, v3KeyList{Data: data, Meta: apiv3.ListMeta{Limit: q.Limit, NextCursor: next}})
}
