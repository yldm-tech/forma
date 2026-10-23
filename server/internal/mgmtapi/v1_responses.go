package mgmtapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/mgmtapi/mgmtv1"
	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/server/internal/platform/v1api"
	"github.com/yldm-tech/forma/server/internal/response"
	"github.com/yldm-tech/forma/server/internal/response/answers"
	"github.com/yldm-tech/forma/server/internal/storage"
)

// messageInvalidUpload is forma2's answer to a file upload answer outside the survey's uploads.
const messageInvalidUpload = "Invalid file upload response: each file URL must reference a file uploaded to this survey's file-upload element"

// v1ResponseObject is a response as v1 answers it: forma2's TResponse with the relative storage URLs in its data made absolute.
func (h *Handler) v1ResponseObject(r *response.Response) response.Response {
	out := *r
	out.Data = h.resolvedData(r.Data)
	return out
}

func (h *Handler) resolvedData(data json.RawMessage) json.RawMessage {
	var decoded any
	if json.Unmarshal(data, &decoded) != nil {
		return data
	}
	b, err := json.Marshal(storage.ResolveURLs(h.cfg, decoded))
	if err != nil {
		return data
	}
	return b
}

// v1ListResponses is forma2's GET /responses: with surveyId, that survey's responses (404 Survey, 401 without read on its workspace); otherwise those of every workspace the key has a grant on. Newest first, limit (default 25) after skip.
func (h *Handler) v1ListResponses(c *gin.Context) {
	limit, offset, ok := v1Pagination(c, response.DefaultPageSize, "skip")
	if !ok {
		return
	}
	ctx := c.Request.Context()
	q := response.ManagedQuery{WorkspaceIDs: grantedWorkspaces(c), Limit: limit, Offset: offset}
	if raw := c.Query("surveyId"); raw != "" {
		id, err := response.ResolveID(raw)
		if err != nil {
			v1api.BadRequest(c, "Validation failed: Invalid cuid2", nil, false)
			return
		}
		sv, err := response.LoadSurvey(ctx, h.db, id)
		if err != nil {
			h.v1Fail(c, err)
			return
		}
		if sv == nil {
			v1api.NotFound(c, "Survey", raw, true)
			return
		}
		if !h.v1.Authorize(c, http.MethodGet, sv.WorkspaceID) {
			return
		}
		q.WorkspaceIDs, q.SurveyID = []uuid.UUID{sv.WorkspaceID}, &sv.ID
	}
	found, err := response.ListManaged(ctx, h.db, q)
	if err != nil {
		h.v1Fail(c, err)
		return
	}
	out := make([]response.Response, len(found))
	for i := range found {
		out[i] = h.v1ResponseObject(&found[i])
	}
	v1OK(c, out)
}

// v1Response loads the response the path names with its survey and authorizes method on the survey's workspace, answering itself when it cannot: 400 for a malformed id, 404 Response, 404 Survey, 401 when refused.
func (h *Handler) v1Response(c *gin.Context, method string) (*response.Response, *response.Survey) {
	id, ok := v1ID(c, "responseId")
	if !ok {
		return nil, nil
	}
	ctx := c.Request.Context()
	r, err := response.Load(ctx, h.db, id)
	if err != nil {
		h.v1Fail(c, err)
		return nil, nil
	}
	if r == nil {
		v1api.NotFound(c, "Response", c.Param("responseId"), false)
		return nil, nil
	}
	sv, err := response.LoadSurvey(ctx, h.db, r.SurveyID)
	if err != nil {
		h.v1Fail(c, err)
		return nil, nil
	}
	if sv == nil {
		v1api.NotFound(c, "Survey", r.SurveyID.String(), true)
		return nil, nil
	}
	if !h.v1.Authorize(c, method, sv.WorkspaceID) {
		return nil, nil
	}
	return r, sv
}

func (h *Handler) v1GetResponse(c *gin.Context) {
	r, _ := h.v1Response(c, http.MethodGet)
	if r == nil {
		return
	}
	v1OK(c, h.v1ResponseObject(r))
}

// v1DeleteResponse is forma2's DELETE /responses/{id}: workspace.manage, then the response and its display deleted, its uploads cleaned up, its quotas left as they were. The answer is the deleted response as stored.
func (h *Handler) v1DeleteResponse(c *gin.Context) {
	rec := mgmtv1.AuditOf(c)
	rec.TargetID = c.Param("responseId")
	r, _ := h.v1Response(c, http.MethodDelete)
	if r == nil {
		return
	}
	rec.OldObject = r
	deleted, err := h.responses.DeleteManaged(c.Request.Context(), r.ID)
	if err != nil {
		h.v1Fail(c, err)
		return
	}
	if deleted == nil {
		v1api.NotFound(c, "Response", c.Param("responseId"), false)
		return
	}
	v1OK(c, deleted)
}

// legacyPrefixes is forma2's getWorkspaceLegacyStoragePrefixes: the storage prefixes a workspace owns for files uploaded before uploads were scoped to a survey and element, its own id (as the rewrite and as forma2 knew it) and its legacy environment id.
func (h *Handler) legacyPrefixes(c *gin.Context, workspaceID uuid.UUID) ([]string, error) {
	prefixes := []string{workspaceID.String()}
	env, err := h.environmentID(c.Request.Context(), workspaceID)
	if err != nil {
		return nil, err
	}
	if env != workspaceID.String() {
		prefixes = append(prefixes, env)
	}
	return prefixes, nil
}

// validUploads is forma2's validateClientFileUploads for a management caller, which may replay files stored under the workspace's legacy prefixes.
func (h *Handler) validUploads(c *gin.Context, sv *response.Survey, data map[string]any) (bool, error) {
	elements, err := storage.AnswerElements(sv.Blocks)
	if err != nil {
		return false, err
	}
	prefixes, err := h.legacyPrefixes(c, sv.WorkspaceID)
	if err != nil {
		return false, err
	}
	scope := storage.UploadScope{WorkspaceID: sv.WorkspaceID, SurveyID: sv.ID, AppOrigins: []*url.URL{h.cfg.WebAppURL, h.cfg.PublicURL}, LegacyPrefixes: prefixes}
	return storage.ValidAnswerFiles(data, elements, scope), nil
}

// answerErrors is forma2's validateResponseData in the response's language ("en" when it names none).
func (h *Handler) answerErrors(sv *response.Survey, data map[string]any, language *string) ([]answers.ElementErrors, error) {
	lang := "en"
	if language != nil {
		lang = *language
	}
	return answers.Validate(sv.Blocks, data, lang, sv.DefaultLanguage, h.now())
}

// v1AnswerDetails is forma2's formatValidationErrorsForV1Api.
func v1AnswerDetails(errs []answers.ElementErrors) map[string]any {
	details := map[string]any{}
	for _, d := range answers.V1Details(errs) {
		details[d.Key] = d.Message
	}
	return details
}

// v1ResponseInput is ZResponseInput (packages/types/responses.ts) as forma2's v1 management create parsed it.
type v1ResponseInput struct {
	createdAt, updatedAt *time.Time
	surveyID             string
	in                   response.ManagedInput
}

func parseV1ResponseInput(body map[string]any) (v1ResponseInput, *issues) {
	o := object(body)
	var is issues
	var out v1ResponseInput
	out.createdAt = is.coerceDate("createdAt", o.get("createdAt"), optional)
	out.updatedAt = is.coerceDate("updatedAt", o.get("updatedAt"), optional)
	is.id("workspaceId", o.get("workspaceId"), required)
	out.surveyID, _ = is.id("surveyId", o.get("surveyId"), required)
	out.in.UserID = is.optString("userId", o.get("userId"), nullish)
	out.in.DisplayID = is.optString("displayId", o.get("displayId"), nullish)
	out.in.SingleUseID = is.optString("singleUseId", o.get("singleUseId"), nullish)
	is.optString("pinAuthToken", o.get("pinAuthToken"), nullish)
	is.optString("recaptchaToken", o.get("recaptchaToken"), nullish)
	if f := is.boolean("finished", o.get("finished"), required); f != nil {
		out.in.Finished = *f
	}
	out.in.EndingID = is.optString("endingId", o.get("endingId"), nullish)
	out.in.Language = is.optString("language", o.get("language"), optional)
	out.in.Data = is.responseData(o.get("data"), required)
	out.in.Variables = is.variables(o.get("variables"), optional)
	out.in.TTC = is.ttc(o.get("ttc"), optional)
	out.in.Meta = is.meta("meta", o.get("meta"), optional)
	out.in.CreatedAt, out.in.UpdatedAt, out.in.ClampTTC = out.createdAt, out.updatedAt, true
	return out, &is
}

// v1CreateResponse is forma2's POST /responses: the body read, the workspace resolved and authorized for write, ZResponseInput, the survey (404, 400 when it is another workspace's), the file uploads and the answers checked, then the response created with the caller's meta under the survey's anonymize policy.
func (h *Handler) v1CreateResponse(c *gin.Context) {
	body, ok := mgmtv1.ReadBody(c)
	if !ok {
		return
	}
	ws, ok := h.v1.ResolveWorkspace(c, body, http.MethodPost)
	if !ok {
		return
	}
	input, is := parseV1ResponseInput(body)
	if is.failed() {
		v1api.BadRequest(c, messageInvalidFields, is.v1Details(), true)
		return
	}
	ctx := c.Request.Context()
	surveyID, _ := legacyid.Resolve(input.surveyID)
	sv, err := response.LoadSurvey(ctx, h.db, surveyID)
	if err != nil {
		h.v1Fail(c, err)
		return
	}
	if sv == nil {
		v1api.NotFound(c, "Survey", input.surveyID, true)
		return
	}
	if sv.WorkspaceID != ws.ID {
		v1api.BadRequest(c, "Survey is part of another workspace", map[string]any{"workspaceId": ws.ID.String()}, true)
		return
	}
	valid, err := h.validUploads(c, sv, input.in.Data)
	if err != nil {
		h.v1Fail(c, err)
		return
	}
	if !valid {
		v1api.BadRequest(c, messageInvalidUpload, nil, false)
		return
	}
	errs, err := h.answerErrors(sv, input.in.Data, input.in.Language)
	if err != nil {
		h.v1Fail(c, err)
		return
	}
	if len(errs) > 0 {
		v1api.BadRequest(c, "Validation failed", v1AnswerDetails(errs), true)
		return
	}
	created, err := h.responses.CreateManaged(ctx, sv, input.in)
	if err != nil {
		switch {
		case errors.Is(err, response.ErrOrganizationNotFound):
			v1api.NotFoundUnnamed(c, "Organization", false)
		case errors.Is(err, response.ErrDisplayNotFound):
			v1api.NotFound(c, "Display", *input.in.DisplayID, false)
		default:
			h.v1Fail(c, err)
		}
		return
	}
	rec := mgmtv1.AuditOf(c)
	rec.TargetID, rec.NewObject = created.ID.String(), created
	v1api.WriteJSON(c, http.StatusOK, map[string]any{"data": jsView(created)}, true)
}

// v1UpdateResponse is forma2's PUT /responses/{id}: workspace.write, the body's file uploads and answers checked before ZResponseUpdateInput, then data, variables and ttc merged over the stored response, the quotas evaluated and the pipeline told. Unlike the client API it updates a finished response too.
func (h *Handler) v1UpdateResponse(c *gin.Context) {
	rec := mgmtv1.AuditOf(c)
	rec.TargetID = c.Param("responseId")
	current, sv := h.v1Response(c, http.MethodPut)
	if current == nil {
		return
	}
	rec.OldObject = current
	body, ok := mgmtv1.ReadBody(c)
	if !ok {
		return
	}
	o := object(body)
	data, _ := o.get("data").(map[string]any)
	language, _ := o.get("language").(string)
	var languagePtr *string
	if _, isString := o.get("language").(string); isString {
		languagePtr = &language
	}
	if data != nil {
		valid, err := h.validUploads(c, sv, data)
		if err != nil {
			h.v1Fail(c, err)
			return
		}
		if !valid {
			v1api.BadRequest(c, messageInvalidUpload, nil, false)
			return
		}
		errs, err := h.answerErrors(sv, data, languagePtr)
		if err != nil {
			h.v1Fail(c, err)
			return
		}
		if len(errs) > 0 {
			v1api.BadRequest(c, "Validation failed", v1AnswerDetails(errs), true)
			return
		}
	}
	var is issues
	update := response.UpdateInput{}
	update.Finished = is.boolean("finished", o.get("finished"), optional)
	endingID := o.get("endingId")
	update.EndingIDSet = !isAbsent(endingID)
	update.EndingID = is.optString("endingId", endingID, nullish)
	update.Data = is.responseData(o.get("data"), optional)
	update.Variables = is.variables(o.get("variables"), optional)
	if ttc := is.ttc(o.get("ttc"), optional); ttc != nil {
		update.TTC = ttc
	}
	update.Language = is.optString("language", o.get("language"), optional)
	is.optString("pinAuthToken", o.get("pinAuthToken"), nullish)
	if is.failed() {
		v1api.BadRequest(c, messageInvalidFields, is.v1Details(), false)
		return
	}
	ctx := c.Request.Context()
	if _, err := h.responses.Update(ctx, sv, current.ID.String(), update); err != nil {
		h.v1Fail(c, err)
		return
	}
	updated, err := response.Load(ctx, h.db, current.ID)
	if err != nil || updated == nil {
		if err == nil {
			err = errNotFound
		}
		h.v1Fail(c, err)
		return
	}
	rec.NewObject = updated
	v1OK(c, h.v1ResponseObject(updated))
}
