package contact

import (
	"net/http"

	"github.com/yldm-tech/forma/apps/server/internal/apiv3"
	"github.com/yldm-tech/forma/apps/server/internal/v3api/openapi"
)

// V3OpenAPI describes /api/v3/contact-attribute-keys for the published OpenAPI document.
func V3OpenAPI() openapi.Spec {
	return openapi.Spec{
		Schemas: map[string]*openapi.Schema{"ContactAttributeKey": openapi.Infer[V3Key]()},
		Operations: []openapi.Operation{{
			Method: http.MethodGet, Path: "/api/v3/contact-attribute-keys", ID: "listContactAttributeKeys", Tag: "Contact attribute keys",
			Summary:     "List contact attribute keys",
			Description: "The attribute keys of a workspace, which the targeting filters of in-app surveys refer to, in id order. Needs read access to the workspace.",
			Parameters: []openapi.Parameter{
				openapi.WorkspaceQuery(),
				openapi.Query("limit", "Page size.", openapi.Integer("", 1, apiv3.MaxLimit, apiv3.DefaultLimit)),
				openapi.CursorQuery(),
			},
			Responses: []openapi.Response{{Status: http.StatusOK, Description: "A page of attribute keys.", Schema: openapi.List(openapi.Ref("ContactAttributeKey"), openapi.Infer[apiv3.ListMeta]())}},
			Problems:  []int{http.StatusBadRequest, http.StatusForbidden},
		}},
	}
}
