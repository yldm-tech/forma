package clientapi

import (
	"context"
	"unicode/utf16"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/rowid"
	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/sdkidentity"
)

// msgUnexpected is what forma2's v1 wrapper answered when a handler threw.
const msgUnexpected = "An unexpected error occurred."

// maxDisplayUserIDLength is ZDisplayCreateInput's limit on userId.
const maxDisplayUserIDLength = 255

// createDisplayV1 is forma2's v1 display create, which the survey runtime calls for an identified user: the workspace first, then the body (ZDisplayCreateInput), then the display, attached to the contact with that userId, which is created when there is none.
func (h *Handler) createDisplayV1(c *gin.Context) {
	ws, ok := h.workspace(c, c.Param("workspaceId"), msgUnexpected)
	if !ok {
		return
	}
	body, _, err := parseBody(c.Request)
	if err != nil {
		h.bodyError(c, err, msgMalformedJSON)
		return
	}
	in := asObject(body)
	var is issues
	surveyID, _ := is.id("surveyId", in.get("surveyId"), required)
	userID := is.optString("userId", in.get("userId"), optional)
	if userID != nil && len(utf16.Encode([]rune(*userID))) > maxDisplayUserIDLength {
		is.add("userId", "User ID cannot exceed 255 characters")
	}
	is.id("responseId", in.get("responseId"), optional)
	userHash := is.optString("userHash", in.get("userHash"), optional)
	if is.failed() {
		writeError(c, badRequest(msgInvalidFields, is.details))
		return
	}
	userID, err = h.verifiedUserID(c.Request.Context(), ws.ID, userID, userHash)
	if err != nil {
		h.fail(c, err, msgGeneric)
		return
	}
	d, err := h.responses.CreateDisplay(c.Request.Context(), response.DisplayInput{WorkspaceID: ws.ID, SurveyID: surveyID, UserID: userID})
	if err != nil {
		h.fail(c, err, msgGeneric)
		return
	}
	h.writeDisplay(c, ws.ID, d)
}

// createDisplayV2 is forma2's v2 display create, which the survey runtime calls for an anonymous respondent or a known contact: the workspace, then the body (ZDisplayCreateInputV2), then the display, attached to the contact only when it is one of the workspace's.
func (h *Handler) createDisplayV2(c *gin.Context) {
	ws, ok := h.workspace(c, c.Param("workspaceId"), msgGeneric)
	if !ok {
		return
	}
	body, _, err := parseBody(c.Request)
	if err != nil {
		h.bodyError(c, err, msgInvalidJSON)
		return
	}
	in := asObject(body)
	var is issues
	surveyID, _ := is.id("surveyId", in.get("surveyId"), required)
	is.id("responseId", in.get("responseId"), optional)
	contactID := h.contactRef(&is, ws.ID, in.get("contactId"), optional)
	if is.failed() {
		writeError(c, badRequest(msgInvalidFields, is.details))
		return
	}
	d, err := h.responses.CreateDisplay(c.Request.Context(), response.DisplayInput{WorkspaceID: ws.ID, SurveyID: surveyID, ContactID: contactID})
	if err != nil {
		h.fail(c, err, msgGeneric)
		return
	}
	h.writeDisplay(c, ws.ID, d)
}

// writeDisplay answers a created display, {id, contactId, surveyId}, naming its contact by token (sdkidentity.ContactToken) rather than by id, so that a userId sent to the v1 route does not reveal the contact's id.
func (h *Handler) writeDisplay(c *gin.Context, workspaceID uuid.UUID, d *response.Display) {
	var contactRef any
	if d.ContactID != nil {
		token, err := sdkidentity.ContactToken(h.cfg, workspaceID, *d.ContactID)
		if err != nil {
			h.fail(c, err, msgGeneric)
			return
		}
		contactRef = token
	}
	writeData(c, fields{{"id", d.ID}, {"contactId", contactRef}, {"surveyId", d.SurveyID}}, noStore)
}

// contactRef reads a v2 body's contactId: a contact id, or the token an identify or a display create answered in its place. A token that does not name a contact of this workspace (issued under a key since retired, or for another workspace) is no contact, as an unknown contact id is: the display or response is still recorded, without one.
func (h *Handler) contactRef(is *issues, workspaceID uuid.UUID, v any, p presence) *uuid.UUID {
	if s, ok := v.(string); ok && sdkidentity.IsContactToken(s) {
		if id, ok := sdkidentity.ParseContactToken(h.cfg, workspaceID, s); ok {
			return &id
		}
		return nil
	}
	raw, ok := is.id("contactId", v, p)
	if !ok {
		return nil
	}
	id, _ := rowid.Parse(raw)
	return &id
}

// verifiedUserID is the userId a v1 display or response may attach: the one sent, unless the workspace requires verified identities and userHash does not verify it, in which case none, and the display or response is recorded without a contact rather than refused, so no answer is lost.
func (h *Handler) verifiedUserID(ctx context.Context, workspaceID uuid.UUID, userID, userHash *string) (*string, error) {
	if userID == nil || *userID == "" {
		return userID, nil
	}
	identity, err := sdkidentity.Load(ctx, h.db, h.cipher, workspaceID)
	if err != nil {
		return nil, err
	}
	hash := ""
	if userHash != nil {
		hash = *userHash
	}
	if identity.Required && !identity.Verified(*userID, hash) {
		return nil, nil
	}
	return userID, nil
}

// displayResponse is forma2's display-response lookup, how the survey runtime recovers the response of a display after an offline restore: {"responseId": id or null}, 404 Display when the display is not one of the workspace's.
func (h *Handler) displayResponse(c *gin.Context) {
	ws, ok := h.workspace(c, c.Param("workspaceId"), msgUnexpected)
	if !ok {
		return
	}
	id, err := h.responses.DisplayResponseID(c.Request.Context(), ws.ID, c.Param("displayId"))
	if err != nil {
		h.fail(c, err, msgGeneric)
		return
	}
	writeData(c, fields{{"responseId", id}}, noStore)
}
