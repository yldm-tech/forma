package actionclass

import (
	"net/http"

	"github.com/yldm-tech/forma/apps/server/internal/v3api"
	"github.com/yldm-tech/forma/apps/server/internal/v3api/openapi"
)

// V3OpenAPI describes GET /api/v3/action-classes for the published OpenAPI document.
func V3OpenAPI() openapi.Spec {
	return openapi.Spec{
		Schemas: map[string]*openapi.Schema{"ActionClass": openapi.Infer[v3ActionClass]()},
		Operations: []openapi.Operation{{
			Method: http.MethodGet, Path: "/api/v3/action-classes", ID: "listActionClasses", Tag: "Action classes",
			Summary:     "List action classes",
			Description: "The actions of a workspace that can trigger in-app surveys, in id order. Needs read access to the workspace.",
			Parameters: []openapi.Parameter{
				openapi.WorkspaceQuery(),
				openapi.Query("limit", "Page size.", openapi.Integer("", 1, v3api.ListMaxLimit, v3api.ListDefaultLimit)),
				openapi.CursorQuery(),
			},
			Responses: []openapi.Response{{Status: http.StatusOK, Description: "A page of action classes.", Schema: openapi.List(openapi.Ref("ActionClass"), openapi.Infer[v3api.ListMeta]())}},
			Problems:  []int{http.StatusBadRequest, http.StatusForbidden},
		}},
	}
}
