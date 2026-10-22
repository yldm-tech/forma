package clientapi

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/server/internal/linksurvey"
	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/server/internal/response"
	"github.com/yldm-tech/forma/server/internal/response/answers"
)

// responseInput is ZResponseInput (v1) or ZResponseInputV2 as validated: v1 names the respondent by userId, v2 by contactId. createdAt and updatedAt are validated but never stored.
type responseInput struct {
	SurveyID       string
	UserID         *string
	ContactID      *string
	DisplayID      *string
	SingleUseID    *string
	PinAuthToken   *string
	RecaptchaToken *string
	Finished       bool
	EndingID       *string
	Language       *string
	Data           []jsvalue.Member
	Variables      map[string]any
	TTC            map[string]float64
	Meta           map[string]any
}

// parseResponseInput checks a create body against ZResponseInput, or ZResponseInputV2 when v2, in the schema's key order.
func parseResponseInput(body any, text string, v2 bool) (*responseInput, *issues) {
	in := asObject(body)
	var is issues
	out := &responseInput{}
	is.coerceDate("createdAt", in.get("createdAt"))
	is.coerceDate("updatedAt", in.get("updatedAt"))
	out.SurveyID, _ = is.id("surveyId", in.get("surveyId"), required)
	if !v2 {
		out.UserID = is.optString("userId", in.get("userId"), nullish)
	}
	out.DisplayID = is.optString("displayId", in.get("displayId"), nullish)
	out.SingleUseID = is.optString("singleUseId", in.get("singleUseId"), nullish)
	out.PinAuthToken = is.optString("pinAuthToken", in.get("pinAuthToken"), nullish)
	out.RecaptchaToken = is.optString("recaptchaToken", in.get("recaptchaToken"), nullish)
	if f := is.boolean("finished", in.get("finished"), required); f != nil {
		out.Finished = *f
	}
	out.EndingID = is.optString("endingId", in.get("endingId"), nullish)
	out.Language = is.optString("language", in.get("language"), optional)
	out.Data, _ = is.responseData("data", in.get("data"), rawMember(text, "data"), required)
	out.Variables, _ = is.variables("variables", in.get("variables"), rawMember(text, "variables"))
	out.TTC, _ = is.ttc("ttc", in.get("ttc"), rawMember(text, "ttc"))
	out.Meta = is.meta("meta", in.get("meta"))
	if v2 {
		if raw, ok := is.id("contactId", in.get("contactId"), nullish); ok {
			out.ContactID = &raw
		}
	}
	return out, &is
}

func (in *responseInput) metaString(key string) *string {
	if s, ok := in.Meta[key].(string); ok {
		return &s
	}
	return nil
}

func (in *responseInput) metaNumber(key string) *float64 {
	if f, ok := in.Meta[key].(float64); ok {
		return &f
	}
	return nil
}

// clientMeta is the meta the response module captures from: the keys a browser can observe.
func (in *responseInput) clientMeta() response.ClientMeta {
	return response.ClientMeta{
		Source: in.metaString("source"), URL: in.metaString("url"), Action: in.metaString("action"),
		PagePath: in.metaString("pagePath"), PageReferrer: in.metaString("pageReferrer"),
		UtmSource: in.metaString("utmSource"), UtmMedium: in.metaString("utmMedium"), UtmCampaign: in.metaString("utmCampaign"),
		UtmTerm: in.metaString("utmTerm"), UtmContent: in.metaString("utmContent"),
		ScreenWidth: in.metaNumber("screenWidth"), ScreenHeight: in.metaNumber("screenHeight"),
		ViewportWidth: in.metaNumber("viewportWidth"), ViewportHeight: in.metaNumber("viewportHeight"),
		Timezone: in.metaString("timezone"), Locale: in.metaString("locale"),
	}
}

func memberKeys(members []jsvalue.Member) []string {
	keys := make([]string, len(members))
	for i, m := range members {
		keys[i] = m.Key
	}
	return keys
}

// createResponseV1 is forma2's v1 response create, which the survey runtime uses for an identified user: the body (before the workspace), the workspace, the schema, the survey and its checks in forma2's v1 order (workspace, status, PIN, the ingest contract, the verified-email gate, reCAPTCHA, single-use), the uploads and the answers, then the write.
func (h *Handler) createResponseV1(c *gin.Context) {
	ctx := c.Request.Context()
	body, text, err := parseBody(c.Request)
	if err != nil {
		h.bodyError(c, err, msgMalformedJSON)
		return
	}
	ws, ok := h.workspace(c, c.Param("workspaceId"), msgUnexpected)
	if !ok {
		return
	}
	in, is := parseResponseInput(body, text, false)
	if is.failed() {
		writeError(c, badRequest(msgInvalidFields, is.details))
		return
	}
	sv, err := h.responses.Survey(ctx, in.SurveyID)
	if err != nil {
		h.fail(c, err, msgGeneric)
		return
	}
	data, flags, err := h.checkNewResponseV1(ctx, sv, ws.ID, in)
	if err != nil {
		h.fail(c, err, msgGeneric)
		return
	}
	h.writeNewResponse(c, sv, ws.ID, in, data, flags)
}

func (h *Handler) checkNewResponseV1(ctx context.Context, sv *response.Survey, workspaceID uuid.UUID, in *responseInput) (map[string]any, []embeddeddata.Flag, error) {
	if err := sv.CheckWorkspace(workspaceID); err != nil {
		return nil, nil, err
	}
	if err := sv.CheckAccepting(); err != nil {
		return nil, nil, err
	}
	if err := h.checkPin(sv, in.PinAuthToken); err != nil {
		return nil, nil, err
	}
	ingest := h.responses.Ingest(sv, in.Data)
	if err := h.verifiedEmailGate(sv, ingest.Data, in.metaString("url")); err != nil {
		return nil, nil, err
	}
	if err := h.checkRecaptcha(ctx, sv, workspaceID, in.RecaptchaToken); err != nil {
		return nil, nil, err
	}
	singleUseID, err := h.responses.SingleUseID(sv, workspaceID, false, in.SingleUseID, in.metaString("url"))
	if err != nil {
		return nil, nil, err
	}
	in.SingleUseID = singleUseID
	elements, err := surveyElements(sv.Blocks)
	if err != nil {
		return nil, nil, err
	}
	if !h.validClientFileUploads(ingest.Data, elements, workspaceID, sv.ID) {
		return nil, nil, badRequest("Invalid file upload response", nil)
	}
	if err := h.validateAnswers(sv, ingest.Data, in.Language); err != nil {
		return nil, nil, err
	}
	return ingest.Data, ingest.Flags, nil
}

// createResponseV2 is forma2's v2 response create, which the survey runtime uses for anonymous respondents and known contacts: the workspace, the body, the survey, the ingest contract, then checkSurveyValidity (workspace, status, PIN, single-use, the verified-email gate, reCAPTCHA), the uploads, the length of "Other" answers and the answers, then the write.
func (h *Handler) createResponseV2(c *gin.Context) {
	ctx := c.Request.Context()
	ws, ok := h.workspace(c, c.Param("workspaceId"), msgGeneric)
	if !ok {
		return
	}
	body, text, err := parseBody(c.Request)
	if err != nil {
		h.bodyError(c, err, msgInvalidJSON)
		return
	}
	in, is := parseResponseInput(body, text, true)
	if is.failed() {
		writeError(c, badRequest(msgInvalidFields, is.details))
		return
	}
	sv, err := h.responses.Survey(ctx, in.SurveyID)
	if err != nil {
		h.fail(c, err, msgGeneric)
		return
	}
	data, flags, err := h.checkNewResponseV2(ctx, sv, ws.ID, in)
	if err != nil {
		h.fail(c, err, msgGeneric)
		return
	}
	h.writeNewResponse(c, sv, ws.ID, in, data, flags)
}

func (h *Handler) checkNewResponseV2(ctx context.Context, sv *response.Survey, workspaceID uuid.UUID, in *responseInput) (map[string]any, []embeddeddata.Flag, error) {
	ingest := h.responses.Ingest(sv, in.Data)
	if err := sv.CheckWorkspace(workspaceID); err != nil {
		return nil, nil, err
	}
	if err := sv.CheckAccepting(); err != nil {
		return nil, nil, err
	}
	if err := h.checkPin(sv, in.PinAuthToken); err != nil {
		return nil, nil, err
	}
	singleUseID, err := h.responses.SingleUseID(sv, workspaceID, true, in.SingleUseID, in.metaString("url"))
	if err != nil {
		return nil, nil, err
	}
	in.SingleUseID = singleUseID
	if err := h.verifiedEmailGate(sv, ingest.Data, in.metaString("url")); err != nil {
		return nil, nil, err
	}
	if err := h.checkRecaptcha(ctx, sv, workspaceID, in.RecaptchaToken); err != nil {
		return nil, nil, err
	}
	elements, err := surveyElements(sv.Blocks)
	if err != nil {
		return nil, nil, err
	}
	if !h.validClientFileUploads(ingest.Data, elements, workspaceID, sv.ID) {
		return nil, nil, badRequest("Invalid file upload response", nil)
	}
	if id := otherOptionTooLong(ingest.Data, memberKeys(in.Data), elements, in.Language); id != "" {
		return nil, nil, badRequest("Response exceeds character limit", fields{{"questionId", id}})
	}
	if err := h.validateAnswers(sv, ingest.Data, in.Language); err != nil {
		return nil, nil, err
	}
	return ingest.Data, ingest.Flags, nil
}

// writeNewResponse creates the response and answers {id, quotaFull, quota?}.
func (h *Handler) writeNewResponse(c *gin.Context, sv *response.Survey, workspaceID uuid.UUID, in *responseInput, data map[string]any, flags []embeddeddata.Flag) {
	ctx := c.Request.Context()
	create := response.CreateInput{
		WorkspaceID: workspaceID, UserID: in.UserID, DisplayID: in.DisplayID, SingleUseID: in.SingleUseID,
		Finished: in.Finished, EndingID: in.EndingID, Language: in.Language, Data: data, IngestFlags: flags,
		Variables: in.Variables, TTC: in.TTC, Meta: in.clientMeta(),
		Request: response.RequestInfo{UserAgent: parseUserAgent(c.GetHeader("User-Agent")), Country: response.CountryFrom(c.Request.Header)},
	}
	if in.ContactID != nil {
		id, _ := legacyid.Resolve(*in.ContactID)
		create.ContactID = &id
	}
	result, err := h.responses.Create(ctx, sv, create)
	if err != nil {
		h.fail(c, err, msgGeneric)
		return
	}
	writeData(c, result, noStore)
}

// checkPin is the PIN gate of both creates and the update: a survey with a PIN takes responses only with a valid PIN token for it.
func (h *Handler) checkPin(sv *response.Survey, token *string) error {
	if sv.Pin == nil || *sv.Pin == "" {
		return nil
	}
	t := ""
	if token != nil {
		t = *token
	}
	if !linksurvey.VerifyPinToken(h.cfg, t, sv.ID, h.now()) {
		return forbidden("Survey is protected by a PIN", fields{{"surveyId", sv.ID.String()}})
	}
	return nil
}

// verifiedEmailGate is forma2's enforceVerifiedEmailGate: a survey that requires email verification takes a response only when meta.url carries a valid verification token for it, and the address recorded is the token's, never the body's.
func (h *Handler) verifiedEmailGate(sv *response.Survey, data map[string]any, metaURL *string) error {
	if !sv.IsVerifyEmailEnabled {
		return nil
	}
	refuse := forbidden("Survey requires email verification", fields{{"surveyId", sv.ID.String()}})
	if metaURL == nil || *metaURL == "" {
		return refuse
	}
	u, err := url.Parse(strings.TrimSpace(*metaURL))
	if err != nil || u.Scheme == "" {
		return refuse
	}
	token := u.Query().Get("verify")
	if token == "" {
		return refuse
	}
	// The link names the survey as it was sent, which is the id a token issued by forma2 is bound to.
	linkSurveyID, _, _ := strings.Cut(strings.TrimPrefix(u.Path, "/s/"), "/")
	email := linksurvey.VerifyEmailToken(h.cfg, token, sv.ID, linkSurveyID, h.now())
	if email == "" {
		return refuse
	}
	data["verifiedEmail"] = email
	return nil
}

// validateAnswers is forma2's validateResponseData with the v1 error format: 400 "Validation failed" with each failing element's messages.
func (h *Handler) validateAnswers(sv *response.Survey, data map[string]any, language *string) error {
	lang := "en"
	if language != nil {
		lang = *language
	}
	errs, err := answers.Validate(sv.Blocks, data, lang, sv.DefaultLanguage, h.now())
	if err != nil {
		return err
	}
	if len(errs) == 0 {
		return nil
	}
	details := fields{}
	for _, d := range answers.V1Details(errs) {
		details.set(d.Key, d.Message)
	}
	return badRequest("Validation failed", details)
}

// updateResponse is forma2's response update (v1, re-exported by v2): the workspace, the body (ZResponseUpdateInput), the response and its survey (404 Response when either is not the workspace's), the PIN, the ingest contract, the survey's status and the response's, the uploads and the answers merged over the stored ones, then the write. The update never re-checks single-use, reCAPTCHA or the display and never changes meta or the contact.
func (h *Handler) updateResponse(c *gin.Context) {
	ctx := c.Request.Context()
	responseID := c.Param("responseId")
	ws, ok := h.workspace(c, c.Param("workspaceId"), msgUnexpected)
	if !ok {
		return
	}
	body, text, err := parseBody(c.Request)
	if err != nil {
		h.bodyError(c, err, msgMalformedUpdateJSON)
		return
	}
	in := asObject(body)
	var is issues
	update := response.UpdateInput{}
	update.Finished = is.boolean("finished", in.get("finished"), optional)
	endingID := in.get("endingId")
	update.EndingIDSet = !isAbsent(endingID)
	update.EndingID = is.optString("endingId", endingID, nullish)
	data, hasData := is.responseData("data", in.get("data"), rawMember(text, "data"), optional)
	update.Variables, _ = is.variables("variables", in.get("variables"), rawMember(text, "variables"))
	update.TTC, _ = is.ttc("ttc", in.get("ttc"), rawMember(text, "ttc"))
	update.Language = is.optString("language", in.get("language"), optional)
	pinAuthToken := is.optString("pinAuthToken", in.get("pinAuthToken"), nullish)
	if is.failed() {
		writeError(c, badRequest(msgInvalidFields, is.details))
		return
	}

	existing, sv, err := h.responses.ResponseForUpdate(ctx, ws.ID, responseID)
	if err != nil {
		h.fail(c, err, msgGeneric)
		return
	}
	if err := h.checkPin(sv, pinAuthToken); err != nil {
		h.fail(c, err, msgGeneric)
		return
	}
	if !hasData {
		data = nil
	}
	ingest := h.responses.IngestUpdate(sv, data)
	update.IngestFlags = ingest.Flags
	if hasData {
		update.Data = ingest.Data
	}
	if err := response.CheckUpdate(sv, existing); err != nil {
		h.fail(c, err, msgGeneric)
		return
	}
	elements, err := surveyElements(sv.Blocks)
	if err != nil {
		h.fail(c, err, msgGeneric)
		return
	}
	if !h.validClientFileUploads(update.Data, elements, ws.ID, sv.ID) {
		writeError(c, badRequest("Invalid file upload response", nil))
		return
	}
	// forma2 checked the length of "Other" answers here against the survey's legacy questions, which a survey kept in blocks no longer has, so no update was ever refused for it; blocks replaced questions in the rewrite.
	merged := map[string]any{}
	if len(existing.Data) > 0 {
		if err := json.Unmarshal(existing.Data, &merged); err != nil {
			h.fail(c, err, msgGeneric)
			return
		}
	}
	for k, v := range update.Data {
		merged[k] = v
	}
	language := update.Language
	if language == nil {
		language = existing.Language
	}
	if err := h.validateAnswers(sv, merged, language); err != nil {
		h.fail(c, err, msgGeneric)
		return
	}
	result, err := h.responses.Update(ctx, sv, responseID, update)
	if err != nil {
		h.fail(c, err, msgGeneric)
		return
	}
	writeData(c, result, noStore)
}
