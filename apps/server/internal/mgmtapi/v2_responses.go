package mgmtapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/storage"
)

// v2Response is a response as the v2 API serializes it: forma2's raw Response row.
type v2Response struct {
	ID                uuid.UUID       `json:"id"`
	CreatedAt         time.Time       `json:"createdAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
	Finished          bool            `json:"finished"`
	SurveyID          uuid.UUID       `json:"surveyId"`
	ContactID         *uuid.UUID      `json:"contactId"`
	EndingID          *string         `json:"endingId"`
	Data              json.RawMessage `json:"data"`
	Variables         json.RawMessage `json:"variables"`
	TTC               json.RawMessage `json:"ttc"`
	Meta              json.RawMessage `json:"meta"`
	ContactAttributes json.RawMessage `json:"contactAttributes"`
	SingleUseID       *string         `json:"singleUseId"`
	Language          *string         `json:"language"`
	DisplayID         *uuid.UUID      `json:"displayId"`
}

func toV2Response(r *response.Response) v2Response {
	out := v2Response{
		ID: r.ID, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Finished: r.Finished, SurveyID: r.SurveyID, EndingID: r.EndingID,
		Data: r.Data, Variables: r.Variables, TTC: r.TTC, Meta: r.Meta, ContactAttributes: r.ContactAttributes, SingleUseID: r.SingleUseID,
		Language: r.Language, DisplayID: r.DisplayID,
	}
	if r.Contact != nil {
		out.ContactID = &r.Contact.ID
	}
	return out
}

// v2ResolvedResponse is the response with the relative storage URLs in its data made absolute.
func (h *Handler) v2ResolvedResponse(r *response.Response) v2Response {
	out := toV2Response(r)
	out.Data = h.resolvedData(r.Data)
	return out
}

// v2ListResponses is forma2's GET /api/v2/management/responses: the common filter plus surveyId and contactId over the workspaces the key may read.
func (h *Handler) v2ListResponses(c *gin.Context) {
	q := v2Query(c)
	var is issues
	f := is.commonFilter(q)
	surveyID, hasSurvey := is.id("surveyId", q.get("surveyId"), optional)
	contactID := filterID(is.optString("contactId", q.get("contactId"), optional))
	is.dateRange(f)
	if is.failed() {
		v2Error(c, http.StatusUnprocessableEntity, is.v2Details())
		return
	}
	if !h.v2Limit(c) {
		return
	}
	ids, err := h.authorizedWorkspaces(c)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	query := response.ManagedQuery{WorkspaceIDs: ids, ContactID: contactID, DateField: f.DateField, Start: f.Start, End: f.End, OrderBy: f.orderColumn(), Ascending: f.Ascending, Limit: f.Limit, Offset: f.Skip}
	if hasSurvey {
		id, _ := legacyid.Resolve(surveyID)
		query.SurveyID = &id
	}
	ctx := c.Request.Context()
	found, err := response.ListManaged(ctx, h.db, query)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	total, err := response.CountManaged(ctx, h.db, query)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	out := make([]v2Response, len(found))
	for i := range found {
		out[i] = h.v2ResolvedResponse(&found[i])
	}
	v2OK(c, out, f.meta(total))
}

// v2Meta is ZResponse's meta (packages/database/zod/responses.ts): source, url, the user agent's parts, country and action; anything else is stripped.
func (is *issues) v2Meta(path string, v any, p presence) map[string]any {
	if p.skip(v) {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		is.typeError(path, "object", v)
		return nil
	}
	o := object(m)
	out := map[string]any{}
	for _, k := range []string{"source", "url"} {
		if s := is.optString(join(path, k), o.get(k), optional); s != nil {
			out[k] = *s
		}
	}
	if ua := is.userAgent(join(path, "userAgent"), o.get("userAgent")); ua != nil {
		out["userAgent"] = ua
	}
	for _, k := range []string{"country", "action"} {
		if s := is.optString(join(path, k), o.get(k), optional); s != nil {
			out[k] = *s
		}
	}
	return out
}

// v2ResponseInput is ZResponseInput (apps/web/modules/api/v2/management/responses/types/responses.ts): ZResponse's picked members in its declared order, the optional ones partial, and userId. contactId is not among them, so a caller cannot name a contact directly.
func parseV2ResponseInput(body map[string]any) (string, response.ManagedInput, *issues) {
	o := object(body)
	var is issues
	var in response.ManagedInput
	in.CreatedAt = is.coerceDate("createdAt", o.get("createdAt"), optional)
	in.UpdatedAt = is.coerceDate("updatedAt", o.get("updatedAt"), optional)
	if f := is.boolean("finished", o.get("finished"), required); f != nil {
		in.Finished = *f
	}
	surveyID, _ := is.id("surveyId", o.get("surveyId"), required)
	if id, ok := is.id("endingId", o.get("endingId"), nullish); ok {
		in.EndingID = &id
	}
	in.Data = is.responseData(o.get("data"), required)
	in.Variables = is.variables(o.get("variables"), optional)
	in.TTC = is.ttc(o.get("ttc"), optional)
	in.Meta = is.v2Meta("meta", o.get("meta"), optional)
	in.SingleUseID = is.optString("singleUseId", o.get("singleUseId"), nullish)
	in.Language = is.optString("language", o.get("language"), nullish)
	in.DisplayID = is.optString("displayId", o.get("displayId"), nullish)
	in.UserID = is.optString("userId", o.get("userId"), optional)
	return surveyID, in, &is
}

// v2CheckAnswers runs forma2's checks of a v2 write's answers against the survey: the file uploads (400 on response), an "Other" text over the limit (400 naming the question), and validateResponseData (400 with one detail per failed rule). It answers itself and reports whether the write may go on.
func (h *Handler) v2CheckAnswers(c *gin.Context, sv *response.Survey, data map[string]any, language *string) bool {
	valid, err := h.validUploads(c, sv, data)
	if err != nil {
		h.v2Unexpected(c, err)
		return false
	}
	if !valid {
		v2Error(c, http.StatusBadRequest, detail("response", messageInvalidUpload))
		return false
	}
	elements, err := storage.AnswerElements(sv.Blocks)
	if err != nil {
		h.v2Unexpected(c, err)
		return false
	}
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if id := storage.OtherOptionTooLong(data, keys, elements, language); id != "" {
		v2Error(c, http.StatusBadRequest, []v2Detail{{Field: "response", Issue: "Response for question " + id + " exceeds character limit", Meta: map[string]any{"questionId": id}}})
		return false
	}
	errs, err := h.answerErrors(sv, data, language)
	if err != nil {
		h.v2Unexpected(c, err)
		return false
	}
	if len(errs) > 0 {
		var details []v2Detail
		for _, e := range errs {
			for _, rule := range e.Errors {
				details = append(details, v2Detail{Field: "response.data." + e.ElementID, Issue: rule.Message, Meta: map[string]any{"elementId": e.ElementID, "ruleId": rule.RuleID, "ruleType": rule.RuleType}})
			}
		}
		v2Error(c, http.StatusBadRequest, details)
		return false
	}
	return true
}

// v2CreateResponse is forma2's POST /api/v2/management/responses: ZResponseInput, then the survey (404), workspace.write on it (401), the answers' checks, and the response, created with the caller's meta under the survey's anonymize policy and its quotas evaluated.
func (h *Handler) v2CreateResponse(c *gin.Context) {
	body, ok := v2Body(c)
	if !ok {
		return
	}
	rawSurveyID, in, is := parseV2ResponseInput(body)
	if is.failed() {
		v2Error(c, http.StatusUnprocessableEntity, is.v2Details())
		return
	}
	if !h.v2Limit(c) {
		return
	}
	ctx := c.Request.Context()
	surveyID, _ := legacyid.Resolve(rawSurveyID)
	sv, err := response.LoadSurvey(ctx, h.db, surveyID)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	if sv == nil {
		v2Error(c, http.StatusNotFound, detail("survey", "not found"))
		return
	}
	if !h.v2Authorize(c, http.MethodPost, sv.WorkspaceID) {
		return
	}
	if !h.v2CheckAnswers(c, sv, in.Data, in.Language) {
		return
	}
	created, err := h.responses.CreateManaged(ctx, sv, in)
	if err != nil {
		switch {
		case errors.Is(err, response.ErrDisplayNotFound):
			v2Error(c, http.StatusNotFound, detail("display", "not found"))
		case errors.Is(err, response.ErrOrganizationNotFound):
			v2Error(c, http.StatusNotFound, detail("organization", "not found"))
		default:
			h.v2Fail(c, err)
		}
		return
	}
	h.v2Record(c, created.ID.String(), nil, toV2Response(created))
	v2Created(c, toV2Response(created))
}

// v2Fail answers an expected error of a domain module in v2's format, anything else as the generic 500.
func (h *Handler) v2Fail(c *gin.Context, err error) {
	if e, ok := apperr.As(err); ok && e.Status < http.StatusInternalServerError {
		v2Error(c, e.Status, detail("error", e.Message))
		return
	}
	h.v2Unexpected(c, err)
}

// v2ResponseFor loads the response id names with its survey and authorizes method on the survey's workspace, answering itself when it cannot: 404 response, 401 when refused.
func (h *Handler) v2ResponseFor(c *gin.Context, id uuid.UUID, method string) (*response.Response, *response.Survey) {
	ctx := c.Request.Context()
	r, err := response.Load(ctx, h.db, id)
	if err != nil {
		h.v2Unexpected(c, err)
		return nil, nil
	}
	var sv *response.Survey
	if r != nil {
		if sv, err = response.LoadSurvey(ctx, h.db, r.SurveyID); err != nil {
			h.v2Unexpected(c, err)
			return nil, nil
		}
	}
	if r == nil || sv == nil {
		v2Error(c, http.StatusNotFound, detail("response", "not found"))
		return nil, nil
	}
	if !h.v2Authorize(c, method, sv.WorkspaceID) {
		return nil, nil
	}
	return r, sv
}

func (h *Handler) v2GetResponse(c *gin.Context) {
	id, ok := v2PathID(c, "responseId")
	if !ok || !h.v2Limit(c) {
		return
	}
	r, _ := h.v2ResponseFor(c, id, http.MethodGet)
	if r == nil {
		return
	}
	v2OK(c, h.v2ResolvedResponse(r), nil)
}

// v2DeleteResponse is forma2's DELETE /responses/{id}: workspace.manage, then the response and its display deleted and its uploads cleaned up. The answer is the deleted row as stored.
func (h *Handler) v2DeleteResponse(c *gin.Context) {
	id, ok := v2PathID(c, "responseId")
	if !ok || !h.v2Limit(c) {
		return
	}
	r, _ := h.v2ResponseFor(c, id, http.MethodDelete)
	if r == nil {
		return
	}
	h.v2Record(c, r.ID.String(), toV2Response(r), nil)
	deleted, err := h.responses.DeleteManaged(c.Request.Context(), r.ID)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	if deleted == nil {
		v2Error(c, http.StatusNotFound, detail("response", "not found"))
		return
	}
	v2OK(c, toV2Response(deleted), nil)
}

// parseV2ResponseUpdate is the v2 update schema, ZResponse without id and surveyId: every member required, the nullable ones as an explicit null.
func parseV2ResponseUpdate(body map[string]any) (response.ReplaceInput, *issues) {
	o := object(body)
	var is issues
	var in response.ReplaceInput
	if t := is.coerceDate("createdAt", o.get("createdAt"), required); t != nil {
		in.CreatedAt = *t
	}
	if t := is.coerceDate("updatedAt", o.get("updatedAt"), required); t != nil {
		in.UpdatedAt = *t
	}
	if f := is.boolean("finished", o.get("finished"), required); f != nil {
		in.Finished = *f
	}
	if id, ok := is.id("contactId", o.get("contactId"), nullable); ok {
		in.ContactID = &id
	}
	if id, ok := is.id("endingId", o.get("endingId"), nullable); ok {
		in.EndingID = &id
	}
	in.Data = is.responseData(o.get("data"), required)
	in.Variables = is.variables(o.get("variables"), required)
	in.TTC = is.ttc(o.get("ttc"), required)
	in.Meta = is.v2Meta("meta", o.get("meta"), required)
	if attrs, ok := is.stringRecord("contactAttributes", o.get("contactAttributes"), nullable); ok {
		in.ContactAttributes = attrs
	}
	in.SingleUseID = is.optString("singleUseId", o.get("singleUseId"), nullable)
	in.Language = is.optString("language", o.get("language"), nullable)
	in.DisplayID = is.optString("displayId", o.get("displayId"), nullable)
	return in, &is
}

// v2UpdateResponse is forma2's PUT /responses/{id}: the whole response but its id and survey, then workspace.write on its survey's workspace, the answers' checks, the contact and display kept within the response's workspace and survey (a uniform 404 otherwise), and the columns replaced with the quotas evaluated.
func (h *Handler) v2UpdateResponse(c *gin.Context) {
	body, ok := v2Body(c)
	if !ok {
		return
	}
	in, is := parseV2ResponseUpdate(body)
	if is.failed() {
		v2Error(c, http.StatusUnprocessableEntity, is.v2Details())
		return
	}
	id, ok := v2PathID(c, "responseId")
	if !ok || !h.v2Limit(c) {
		return
	}
	current, sv := h.v2ResponseFor(c, id, http.MethodPut)
	if current == nil {
		return
	}
	h.v2Record(c, current.ID.String(), toV2Response(current), nil)
	if !h.v2CheckAnswers(c, sv, in.Data, in.Language) {
		return
	}
	updated, err := h.responses.ReplaceManaged(c.Request.Context(), sv, current.ID, in)
	if err != nil {
		switch {
		case errors.Is(err, response.ErrContactNotFound):
			v2Error(c, http.StatusNotFound, detail("contactId", "not found"))
		case errors.Is(err, response.ErrDisplayNotFound):
			v2Error(c, http.StatusNotFound, detail("displayId", "not found"))
		default:
			if e, ok := apperr.As(err); ok && e.Status == http.StatusNotFound {
				v2Error(c, http.StatusNotFound, detail("response", "not found"))
				return
			}
			h.v2Fail(c, err)
		}
		return
	}
	h.v2Record(c, "", nil, toV2Response(updated))
	v2OK(c, h.v2ResolvedResponse(updated), nil)
}
