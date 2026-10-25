package survey

import (
	"net/http"
	"slices"

	"github.com/yldm-tech/forma/apps/server/internal/v3api/openapi"
)

// The parts of the v3 survey document whose structure the OpenAPI document leaves to POST /api/v3/surveys/validate: their translatable texts are keyed by locale code (en-US, ja-JP, ...), and their rules are the editor's.
var v3DocumentParts = map[string]string{
	"metadata":     "Survey metadata, such as the link preview's title, description and ogImage.",
	"welcomeCard":  "The welcome card shown before the first block.",
	"blocks":       "The pages of the survey: each block holds its elements (questions) and its logic. At least one is required.",
	"endings":      "The ending cards and redirects shown after the last block.",
	"hiddenFields": "The hidden fields the survey link or SDK can fill.",
	"variables":    "The survey's variables, which logic can calculate.",
}

// V3OpenAPI describes /api/v3/surveys for the published OpenAPI document. POST /api/v3/surveys/templates accepts only a signed-in session and is not part of it.
func V3OpenAPI() openapi.Spec {
	survey := openapi.Infer[v3Resource]()
	for key, desc := range v3DocumentParts {
		survey.Properties[key] = &openapi.Schema{Description: desc}
	}
	survey.Properties["targeting"].Properties["filters"] = &openapi.Schema{Type: "array", Description: "The segment filters of an app survey."}

	body := func(required ...string) *openapi.Schema {
		props := map[string]*openapi.Schema{
			"name":   {Type: "string", MinLength: new(1)},
			"status": openapi.Enum("", Statuses...),
			"languages": {Type: "array", Description: "The survey's languages.", Items: openapi.Object("", map[string]*openapi.Schema{
				"code":    openapi.String("A locale code, such as ja-JP."),
				"default": {Type: "boolean"},
				"enabled": {Type: "boolean"},
			}, "code")},
			"distribution": openapi.Infer[*v3Distribution](),
			"targeting":    openapi.Object("Who sees an app survey.", map[string]*openapi.Schema{"filters": survey.Properties["targeting"].Properties["filters"]}, "filters"),
		}
		for key, desc := range v3DocumentParts {
			props[key] = &openapi.Schema{Description: desc}
		}
		if slices.Contains(required, "workspaceId") {
			props["workspaceId"] = openapi.UUID("The workspace the survey belongs to.")
			props["type"] = openapi.Enum("A link survey (the default) or an in-app survey.", TypeLink, TypeApp)
			props["defaultLanguage"] = &openapi.Schema{Type: "string", Description: "The locale code the texts are written in.", Default: []byte(`"` + defaultV3Language + `"`)}
		}
		return openapi.Object("", props, required...)
	}
	create, patch := body("workspaceId", "name", "blocks"), body()
	patch.Description = "Only the keys present change."

	list := openapi.Infer[ListResult]()
	list.Properties["data"].Items = openapi.Ref("SurveyListItem")

	surveyID := openapi.PathID("surveyId", "The survey.")
	one := func(status int, desc string) []openapi.Response {
		return []openapi.Response{{Status: status, Description: desc, Schema: openapi.Data(openapi.Ref("Survey"))}}
	}
	lifecycle := func(path, id, summary, desc string) openapi.Operation {
		return openapi.Operation{
			Method: http.MethodPost, Path: path, ID: id, Tag: "Surveys", Summary: summary, Description: desc,
			Parameters: []openapi.Parameter{surveyID},
			Responses:  []openapi.Response{{Status: http.StatusOK, Description: "The survey's new state.", Schema: openapi.Data(openapi.Infer[v3Lifecycle]())}},
			Problems:   []int{http.StatusBadRequest, http.StatusForbidden},
		}
	}

	return openapi.Spec{
		Schemas: map[string]*openapi.Schema{
			"Survey":          survey,
			"SurveyListItem":  openapi.Infer[v3ListItem](),
			"SurveyDocument":  create,
			"SurveyPatch":     patch,
			"SurveyValidated": openapi.Infer[ValidationResult](),
		},
		Operations: []openapi.Operation{
			{
				Method: http.MethodGet, Path: "/api/v3/surveys", ID: "listSurveys", Tag: "Surveys",
				Summary: "List surveys", Description: "The surveys of a workspace, without their content. Needs read access to the workspace.",
				Parameters: []openapi.Parameter{
					openapi.WorkspaceQuery(),
					openapi.Query("limit", "Page size.", openapi.Integer("", 1, 250, 20)),
					openapi.CursorQuery(),
					openapi.Query("includeTotalCount", "Whether meta counts the matching surveys.", &openapi.Schema{Type: "string", Enum: []any{"true", "false"}, Default: []byte(`"true"`)}),
					openapi.Query(filterNameParam, "Only surveys whose name contains this text.", &openapi.Schema{Type: "string", MaxLength: new(512)}),
					openapi.Query(filterStatusParam, "Only surveys in these states; archived lists the archived surveys too.", openapi.EnumList(append(slices.Clone(Statuses), "archived")...)),
					openapi.Query(filterTypeParam, "Only surveys of these types.", openapi.EnumList(TypeLink, TypeApp)),
					openapi.Query("sortBy", "The order of the list.", &openapi.Schema{Type: "string", Enum: []any{SortCreatedAt, SortUpdatedAt, SortName, SortRelevance}, Default: []byte(`"` + SortUpdatedAt + `"`)}),
				},
				Responses: []openapi.Response{{Status: http.StatusOK, Description: "A page of surveys.", Schema: list}},
				Problems:  []int{http.StatusBadRequest, http.StatusForbidden},
			},
			{
				Method: http.MethodPost, Path: "/api/v3/surveys", ID: "createSurvey", Tag: "Surveys",
				Summary: "Create a survey", Description: "Creates a survey from a v3 survey document. Unknown keys are refused; validate a document first with POST /api/v3/surveys/validate. Needs write access to the workspace.",
				Parameters: []openapi.Parameter{openapi.Query("createdFrom", "Where the document came from, for analytics.", openapi.Enum("", "blank", "template", "xm-template", "ai"))},
				Body:       openapi.Ref("SurveyDocument"),
				Responses:  one(http.StatusCreated, "The created survey."),
				Problems:   []int{http.StatusBadRequest, http.StatusForbidden, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity},
			},
			{
				Method: http.MethodPost, Path: "/api/v3/surveys/validate", ID: "validateSurvey", Tag: "Surveys",
				Summary: "Validate a survey document", Description: "A dry run of a create or a patch: it saves nothing and lists every problem it finds. Needs read access to the workspace.",
				Body: openapi.Object("", map[string]*openapi.Schema{
					"operation": openapi.Enum("Whether data is a create document or a patch.", "create", "patch"),
					"surveyId":  openapi.UUID("The survey a patch is for."),
					"data":      {Description: "A SurveyDocument for a create, a SurveyPatch for a patch."},
				}, "operation", "data"),
				Responses: []openapi.Response{{Status: http.StatusOK, Description: "Whether the document is valid, and what is wrong with it.", Schema: openapi.Data(openapi.Ref("SurveyValidated"))}},
				Problems:  []int{http.StatusBadRequest, http.StatusForbidden, http.StatusRequestEntityTooLarge},
			},
			{
				Method: http.MethodPost, Path: "/api/v3/surveys/generate", ID: "generateSurvey", Tag: "Surveys",
				Summary: "Generate a survey draft with AI", Description: "Drafts a create document from a prompt; send its payload to POST /api/v3/surveys to save it. Needs write access to the workspace and AI on the instance. It has its own rate limit.",
				Body: openapi.Object("", map[string]*openapi.Schema{
					"workspaceId": openapi.UUID(""),
					"prompt":      {Type: "string", MinLength: new(generatePromptMinLength), MaxLength: new(generatePromptMaxLength), Description: "What the survey should ask."},
					"type":        openapi.Enum("", TypeLink, TypeApp),
					"languages":   {Type: "array", Items: openapi.String("A locale code."), MinItems: new(1)},
				}, "workspaceId", "prompt"),
				Responses: []openapi.Response{{Status: http.StatusOK, Description: "The draft and its validation.", Schema: openapi.Data(openapi.Infer[GenerateResult]())}},
				Problems:  []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusBadGateway, http.StatusServiceUnavailable},
			},
			{
				Method: http.MethodGet, Path: "/api/v3/surveys/{surveyId}", ID: "getSurvey", Tag: "Surveys",
				Summary: "Get a survey", Description: "A survey as a v3 document. Needs read access to its workspace; a survey that does not exist is refused the same way, with a 403.",
				Parameters: []openapi.Parameter{surveyID, openapi.Query("lang", "Only these languages' texts, comma-separated or repeated.", &openapi.Schema{Type: "array", Items: openapi.String("")})},
				Responses:  one(http.StatusOK, "The survey."),
				Problems:   []int{http.StatusBadRequest, http.StatusForbidden},
			},
			{
				Method: http.MethodPatch, Path: "/api/v3/surveys/{surveyId}", ID: "updateSurvey", Tag: "Surveys",
				Summary: "Update a survey", Description: "Changes the keys the body carries. Needs write access to the workspace.",
				Parameters: []openapi.Parameter{surveyID},
				Body:       openapi.Ref("SurveyPatch"),
				Responses:  one(http.StatusOK, "The updated survey."),
				Problems:   []int{http.StatusBadRequest, http.StatusForbidden, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity},
			},
			{
				Method: http.MethodDelete, Path: "/api/v3/surveys/{surveyId}", ID: "deleteSurvey", Tag: "Surveys",
				Summary: "Delete a survey", Description: "Deletes the survey and its responses. Needs write access to the workspace.",
				Parameters: []openapi.Parameter{surveyID},
				Responses:  []openapi.Response{{Status: http.StatusNoContent, Description: "Deleted."}},
				Problems:   []int{http.StatusBadRequest, http.StatusForbidden},
			},
			lifecycle("/api/v3/surveys/{surveyId}/archive", "archiveSurvey", "Archive a survey", "Hides the survey from the default list. Needs write access to the workspace."),
			lifecycle("/api/v3/surveys/{surveyId}/restore", "restoreSurvey", "Restore a survey", "Brings an archived survey back. Needs write access to the workspace."),
		},
	}
}
