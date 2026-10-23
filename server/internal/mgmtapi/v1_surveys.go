package mgmtapi

import (
	"errors"
	"math"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/mgmtapi/mgmtv1"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/server/internal/platform/v1api"
	"github.com/yldm-tech/forma/server/internal/storage"
	"github.com/yldm-tech/forma/server/internal/survey"
)

// v1SurveyObject is forma2's v1 survey answer: the survey with derived questions, projectOverwrites, its stored relative storage URLs made absolute, and environmentId.
func (h *Handler) v1SurveyObject(sv *survey.Survey, environmentID *string) (map[string]any, error) {
	obj, err := survey.V1Object(sv)
	if err != nil {
		return nil, err
	}
	obj = storage.ResolveURLs(h.cfg, obj).(map[string]any)
	if environmentID != nil {
		obj["environmentId"] = *environmentID
	}
	return obj, nil
}

// v1ListSurveys is forma2's GET /surveys: the surveys of every workspace the key has a grant on, most recently updated first, paginated by limit (default 250) and offset. An organization-only key lists nothing.
func (h *Handler) v1ListSurveys(c *gin.Context) {
	limit, offset, ok := v1Pagination(c, maxItemsPerPage, "offset")
	if !ok {
		return
	}
	ctx := c.Request.Context()
	ids := grantedWorkspaces(c)
	found, err := h.surveys.ListV1(ctx, ids, limit, offset)
	if err != nil {
		h.v1Fail(c, err, mgmtv1.MessageGeneric)
		return
	}
	envs, err := h.environmentIDs(ctx, ids)
	if err != nil {
		h.v1Fail(c, err, mgmtv1.MessageGeneric)
		return
	}
	out := make([]any, len(found))
	for i, sv := range found {
		env := envs[sv.WorkspaceID]
		if out[i], err = h.v1SurveyObject(sv, &env); err != nil {
			h.v1Fail(c, err, mgmtv1.MessageGeneric)
			return
		}
	}
	v1OK(c, out)
}

// v1Survey loads the survey the path names and authorizes method on its workspace, answering itself when it cannot: 400 for a malformed id, 404 Survey, 401 when refused.
func (h *Handler) v1Survey(c *gin.Context, method string) *survey.Survey {
	id, ok := v1ID(c, "surveyId")
	if !ok {
		return nil
	}
	sv, err := h.surveys.Load(c.Request.Context(), id)
	if err != nil {
		h.v1Fail(c, err, mgmtv1.MessageGeneric)
		return nil
	}
	if sv == nil {
		v1api.NotFound(c, "Survey", c.Param("surveyId"), false)
		return nil
	}
	if !h.v1.Authorize(c, method, sv.WorkspaceID) {
		return nil
	}
	return sv
}

func (h *Handler) v1GetSurvey(c *gin.Context) {
	sv := h.v1Survey(c, http.MethodGet)
	if sv == nil {
		return
	}
	env, err := h.environmentID(c.Request.Context(), sv.WorkspaceID)
	if err != nil {
		h.v1Fail(c, err, mgmtv1.MessageGeneric)
		return
	}
	obj, err := h.v1SurveyObject(sv, &env)
	if err != nil {
		h.v1Fail(c, err, mgmtv1.MessageGeneric)
		return
	}
	v1OK(c, obj)
}

// normalizeProjectOverwrites is forma2's normalizeProjectOverwritesInput: projectOverwrites is accepted as the old name of workspaceOverwrites, which wins when both are sent.
func normalizeProjectOverwrites(body map[string]any) {
	if v, ok := body["projectOverwrites"]; ok {
		if _, has := body["workspaceOverwrites"]; !has {
			body["workspaceOverwrites"] = v
		}
		delete(body, "projectOverwrites")
	}
}

// v1SurveyError answers a failed survey write: the input's schema issues as 400 details (with CORS on a create), the module's refusals as forma2's handleApiError mapped them.
func (h *Handler) v1SurveyError(c *gin.Context, err error, cors bool) {
	var invalid *survey.V1Invalid
	if errors.As(err, &invalid) {
		details := map[string]any{}
		for _, i := range invalid.Issues {
			details[i.Name()] = i.Message
		}
		v1api.BadRequest(c, messageInvalidFields, details, cors)
		return
	}
	if e, ok := apperr.As(err); ok && e.Status == http.StatusNotFound {
		resource, id, _ := strings.Cut(e.Message, " with ID ")
		resource = strings.TrimSuffix(resource, " not found")
		if id == "" {
			v1api.NotFoundUnnamed(c, resource, false)
			return
		}
		v1api.NotFound(c, resource, strings.TrimSuffix(id, " not found"), false)
		return
	}
	h.v1Fail(c, err, mgmtv1.MessageGeneric)
}

// v1CreateSurvey is forma2's POST /surveys: the body read, the workspace resolved and authorized for write, then the survey created from blocks or legacy questions.
func (h *Handler) v1CreateSurvey(c *gin.Context) {
	body, ok := mgmtv1.ReadBody(c)
	if !ok {
		return
	}
	normalizeProjectOverwrites(body)
	ws, ok := h.v1.ResolveWorkspace(c, body, http.MethodPost)
	if !ok {
		return
	}
	delete(body, "environmentId")
	delete(body, "workspaceId")
	ctx := c.Request.Context()
	sv, err := h.surveys.CreateV1(ctx, survey.APIKeyCaller(mgmtv1.PrincipalOf(c).APIKeyID), ws.ID, body)
	if err != nil {
		h.v1SurveyError(c, err, true)
		return
	}
	obj, err := h.v1SurveyObject(sv, nil)
	if err != nil {
		h.v1Fail(c, err, mgmtv1.MessageGeneric)
		return
	}
	h.bestEffortEnvironmentID(c, obj, sv.WorkspaceID)
	rec := mgmtv1.AuditOf(c)
	rec.TargetID, rec.NewObject = sv.ID.String(), sv
	v1OK(c, obj)
}

// v1UpdateSurvey is forma2's PUT /surveys/{id}: workspace.write on the survey's workspace, then the body over the stored survey.
func (h *Handler) v1UpdateSurvey(c *gin.Context) {
	rec := mgmtv1.AuditOf(c)
	sv := h.v1Survey(c, http.MethodPut)
	if sv == nil {
		return
	}
	rec.TargetID, rec.OldObject = sv.ID.String(), sv
	if err := h.repo.organizationExists(c.Request.Context(), sv.WorkspaceID); err != nil {
		if errors.Is(err, errNotFound) {
			v1api.NotFoundUnnamed(c, "Organization", false)
			return
		}
		h.v1Fail(c, err, mgmtv1.MessageGeneric)
		return
	}
	body, ok := mgmtv1.ReadBody(c)
	if !ok {
		return
	}
	normalizeProjectOverwrites(body)
	for _, readOnly := range []string{"id", "createdAt", "updatedAt", "workspaceId", "environmentId", "createdBy", "archivedAt"} {
		delete(body, readOnly)
	}
	updated, err := h.surveys.UpdateV1(c.Request.Context(), survey.APIKeyCaller(mgmtv1.PrincipalOf(c).APIKeyID), sv, body)
	if err != nil {
		h.v1SurveyError(c, err, false)
		return
	}
	obj, err := h.v1SurveyObject(updated, nil)
	if err != nil {
		h.v1Fail(c, err, mgmtv1.MessageGeneric)
		return
	}
	h.bestEffortEnvironmentID(c, obj, updated.WorkspaceID)
	rec.NewObject = updated
	v1OK(c, obj)
}

// v1DeleteSurvey is forma2's DELETE /surveys/{id}: workspace.manage, then the survey and an app survey's private segment deleted outright. The answer is the deleted row with its segment and triggers.
func (h *Handler) v1DeleteSurvey(c *gin.Context) {
	rec := mgmtv1.AuditOf(c)
	rec.TargetID = c.Param("surveyId")
	sv := h.v1Survey(c, http.MethodDelete)
	if sv == nil {
		return
	}
	rec.OldObject = sv
	deleted, err := h.surveys.DeleteV1(c.Request.Context(), sv.ID)
	if err != nil {
		h.v1Fail(c, err, mgmtv1.MessageGeneric)
		return
	}
	if deleted == nil {
		v1api.NotFound(c, "Survey", c.Param("surveyId"), false)
		return
	}
	obj, err := survey.V1DeletedObject(deleted)
	if err != nil {
		h.v1Fail(c, err, mgmtv1.MessageGeneric)
		return
	}
	h.bestEffortEnvironmentID(c, obj, deleted.WorkspaceID)
	v1OK(c, obj)
}

// v1SingleUseIDs is forma2's GET /surveys/{id}/singleUseIds: workspace.write (forma2 checked it as a POST), a link survey with single use enabled, then limit links (default 10, 1 to 5000) on the public domain, encrypted or signed as the survey's setting says.
func (h *Handler) v1SingleUseIDs(c *gin.Context) {
	sv := h.v1Survey(c, http.MethodPost)
	if sv == nil {
		return
	}
	if sv.Type != survey.TypeLink {
		v1api.BadRequest(c, "Single use links are only available for link surveys", nil, false)
		return
	}
	settings := sv.SingleUseSettings()
	if !settings.Enabled {
		v1api.BadRequest(c, "Single use links are not enabled for this survey", nil, false)
		return
	}
	count := 10
	if raw, present := c.GetQuery("limit"); present {
		n := jsvalue.ParseNumber(raw)
		switch {
		case n < 1:
			v1api.BadRequest(c, "Limit cannot be less than 1", nil, false)
			return
		case n > 5000:
			v1api.BadRequest(c, "Limit cannot be more than 5000", nil, false)
			return
		case math.IsNaN(n):
			v1OK(c, []string{})
			return
		default:
			count = int(n)
		}
	}
	links, err := h.surveys.SingleUseLinks(c.Request.Context(), survey.APIKeyCaller(mgmtv1.PrincipalOf(c).APIKeyID), sv.ID, survey.SingleUseLinksInput{IsEncrypted: settings.IsEncrypted, Count: &count})
	if err != nil {
		h.v1Fail(c, err, mgmtv1.MessageGeneric)
		return
	}
	base := h.surveys.PublicDomain() + "/s/" + sv.ID.String() + "?suId="
	out := make([]string, len(links))
	for i, l := range links {
		out[i] = base + l.SuID
		if l.SuToken != "" {
			out[i] += "&suToken=" + l.SuToken
		}
	}
	v1OK(c, out)
}