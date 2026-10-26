package workflow

import (
	"net/http"
	"reflect"

	"github.com/yldm-tech/forma/apps/server/internal/v3api/openapi"
)

// V3OpenAPI describes /api/v3/workflows for the published OpenAPI document.
func V3OpenAPI() openapi.Spec {
	workflow := openapi.Infer[Resource]()
	workflow.Properties["definition"] = openapi.Ref("WorkflowDefinition")
	definition := openapi.InferWith[Definition](map[reflect.Type]*openapi.Schema{reflect.TypeFor[Node](): openapi.Infer[nodeJSON]()})
	definition.Description = "The workflow graph: one response.completed trigger, the nodes after it (send_email, http_request, slack_message, discord_message, teams_message, tag_response and set_contact_attribute actions, and if_else branches) and the edges between them. Secrets (an http_request header's secret and signingSecret, a chat message's webhookUrl) are written as {\"value\": \"...\"} and read back sealed, as {\"sealed\": \"...\", \"hint\": \"...\"}; send the sealed form back unchanged to keep a secret. Adding or changing an http_request needs manage access to the workspace."

	workflowID := openapi.PathID("workflowId", "The workflow.")
	limit := openapi.Query("limit", "Page size.", openapi.Integer("", 1, 100, 20))
	description := openapi.Nullable(&openapi.Schema{Type: "string", MaxLength: new(500)})
	name := &openapi.Schema{Type: "string", MinLength: new(1), MaxLength: new(120)}
	one := func(desc string) []openapi.Response {
		return []openapi.Response{{Status: http.StatusOK, Description: desc, Schema: openapi.Data(openapi.Ref("Workflow"))}}
	}
	created := func(desc string) []openapi.Response {
		return []openapi.Response{{Status: http.StatusCreated, Description: desc, Schema: openapi.Data(openapi.Ref("Workflow"))}}
	}
	lifecycle := func(method, path, id, summary, desc string) openapi.Operation {
		return openapi.Operation{
			Method: method, Path: path, ID: id, Tag: "Workflows", Summary: summary, Description: desc,
			Parameters: []openapi.Parameter{workflowID}, Responses: one("The workflow after the change."),
			Problems: []int{http.StatusBadRequest, http.StatusForbidden, http.StatusConflict, http.StatusUnprocessableEntity},
		}
	}

	return openapi.Spec{
		Schemas: map[string]*openapi.Schema{
			"Workflow":            workflow,
			"WorkflowListItem":    openapi.Infer[ListItem](),
			"WorkflowDefinition":  definition,
			"WorkflowRunListItem": openapi.Infer[RunListItem](),
			"WorkflowRun":         openapi.NotNull(openapi.Infer[RunResource](), "logs"),
			"WorkflowTestResult":  openapi.NotNull(openapi.Infer[TestResult](), "problems"),
		},
		Operations: []openapi.Operation{
			{
				Method: http.MethodGet, Path: "/api/v3/workflows", ID: "listWorkflows", Tag: "Workflows",
				Summary: "List workflows", Description: "The workflows of a workspace, without their definitions. Needs read access to the workspace.",
				Parameters: []openapi.Parameter{
					openapi.WorkspaceQuery(), limit, openapi.CursorQuery(),
					openapi.Query("filter[status][in]", "Only workflows in these states.", openapi.EnumList(workflowStatuses...)),
					openapi.Query("filter[name][contains]", "Only workflows whose name contains this text.", &openapi.Schema{Type: "string", MinLength: new(1), MaxLength: new(512)}),
					openapi.Query("sortBy", "The order of the list.", openapi.Enum("", sortOrders...)),
				},
				Responses: []openapi.Response{{Status: http.StatusOK, Description: "A page of workflows.", Schema: openapi.List(openapi.Ref("WorkflowListItem"), openapi.Infer[listMeta]())}},
				Problems:  []int{http.StatusBadRequest, http.StatusForbidden},
			},
			{
				Method: http.MethodPost, Path: "/api/v3/workflows", ID: "createWorkflow", Tag: "Workflows",
				Summary: "Create a workflow", Description: "Creates a draft workflow. Needs write access to the workspace.",
				Body: openapi.Object("", map[string]*openapi.Schema{
					"workspaceId": openapi.UUID("The workspace the workflow belongs to."),
					"name":        name,
					"description": description,
					"status":      openapi.Enum("A new workflow is always a draft.", StatusDraft),
					"definition":  openapi.Ref("WorkflowDefinition"),
				}, "workspaceId", "name"),
				Responses: created("The created workflow."),
				Problems:  []int{http.StatusBadRequest, http.StatusForbidden},
			},
			{
				Method: http.MethodGet, Path: "/api/v3/workflows/runs", ID: "listWorkflowRuns", Tag: "Workflow runs",
				Summary: "List workflow runs", Description: "The runs of a workspace's workflows. Needs read access to the workspace.",
				Parameters: []openapi.Parameter{
					openapi.WorkspaceQuery(), limit, openapi.CursorQuery(),
					openapi.Query("workflowId", "Only the runs of this workflow.", openapi.UUID("")),
					openapi.Query("responseId", "Only the runs this response started.", openapi.UUID("")),
					openapi.Query("filter[status][in]", "Only runs in these states.", openapi.EnumList(runStatuses...)),
					openapi.Query("filter[isDryRun][eq]", "Only test runs, or only real ones.", openapi.Enum("", "true", "false")),
				},
				Responses: []openapi.Response{{Status: http.StatusOK, Description: "A page of runs.", Schema: openapi.List(openapi.Ref("WorkflowRunListItem"), openapi.Infer[listMeta]())}},
				Problems:  []int{http.StatusBadRequest, http.StatusForbidden},
			},
			{
				Method: http.MethodGet, Path: "/api/v3/workflows/runs/{runId}", ID: "getWorkflowRun", Tag: "Workflow runs",
				Summary: "Get a workflow run", Description: "A run with its trigger payload and its steps. Needs read access to the run's workspace.",
				Parameters: []openapi.Parameter{openapi.PathID("runId", "The run.")},
				Responses:  []openapi.Response{{Status: http.StatusOK, Description: "The run.", Schema: openapi.Data(openapi.Ref("WorkflowRun"))}},
				Problems:   []int{http.StatusBadRequest, http.StatusForbidden},
			},
			{
				Method: http.MethodGet, Path: "/api/v3/workflows/{workflowId}", ID: "getWorkflow", Tag: "Workflows",
				Summary: "Get a workflow", Description: "A workflow with its definition. Needs read access to its workspace; a workflow that does not exist is refused the same way, with a 403.",
				Parameters: []openapi.Parameter{workflowID}, Responses: one("The workflow."),
				Problems: []int{http.StatusBadRequest, http.StatusForbidden},
			},
			{
				Method: http.MethodPatch, Path: "/api/v3/workflows/{workflowId}", ID: "updateWorkflow", Tag: "Workflows",
				Summary: "Update a workflow", Description: "Changes the keys the body carries. Needs write access to the workspace.",
				Parameters: []openapi.Parameter{workflowID},
				Body:       openapi.Object("", map[string]*openapi.Schema{"name": name, "description": description, "definition": openapi.Ref("WorkflowDefinition")}),
				Responses:  one("The updated workflow."),
				Problems:   []int{http.StatusBadRequest, http.StatusForbidden, http.StatusConflict, http.StatusUnprocessableEntity},
			},
			{
				Method: http.MethodDelete, Path: "/api/v3/workflows/{workflowId}", ID: "deleteWorkflow", Tag: "Workflows",
				Summary: "Delete a workflow", Description: "Needs write access to the workspace.",
				Parameters: []openapi.Parameter{workflowID},
				Responses:  []openapi.Response{{Status: http.StatusNoContent, Description: "Deleted."}},
				Problems:   []int{http.StatusBadRequest, http.StatusForbidden},
			},
			{
				Method: http.MethodPost, Path: "/api/v3/workflows/{workflowId}/duplicate", ID: "duplicateWorkflow", Tag: "Workflows",
				Summary: "Duplicate a workflow", Description: "Copies the workflow as a new draft, optionally under a new name. Needs write access to the workspace, and manage access when the workflow has an HTTP request step.",
				Parameters: []openapi.Parameter{workflowID},
				Body:       openapi.Object("", map[string]*openapi.Schema{"name": name}), BodyOptional: true,
				Responses: created("The copy."),
				Problems:  []int{http.StatusBadRequest, http.StatusForbidden},
			},
			lifecycle(http.MethodPost, "/api/v3/workflows/{workflowId}/enable", "enableWorkflow", "Enable a workflow", "Starts running the workflow on new responses. The definition must be executable; a 422 lists what is missing."),
			lifecycle(http.MethodPost, "/api/v3/workflows/{workflowId}/disable", "disableWorkflow", "Disable a workflow", "Stops running the workflow on new responses."),
			lifecycle(http.MethodPost, "/api/v3/workflows/{workflowId}/archive", "archiveWorkflow", "Archive a workflow", "Moves the workflow to the archived state, in which it does not run."),
			lifecycle(http.MethodPost, "/api/v3/workflows/{workflowId}/unarchive", "unarchiveWorkflow", "Unarchive a workflow", "Returns an archived workflow to the drafts."),
			{
				Method: http.MethodPost, Path: "/api/v3/workflows/{workflowId}/test", ID: "testWorkflow", Tag: "Workflows",
				Summary: "Test a workflow", Description: "A dry run: every reason enabling the workflow would be refused, without creating a run or sending anything. Needs read access to the workspace.",
				Parameters: []openapi.Parameter{workflowID},
				Responses:  []openapi.Response{{Status: http.StatusOK, Description: "What the dry run found.", Schema: openapi.Data(openapi.Ref("WorkflowTestResult"))}},
				Problems:   []int{http.StatusBadRequest, http.StatusForbidden, http.StatusUnprocessableEntity},
			},
		},
	}
}
