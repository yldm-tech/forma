package clientapi

import (
	"unicode/utf16"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/apps/server/internal/response"
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
	if is.failed() {
		writeError(c, badRequest(msgInvalidFields, is.details))
		return
	}
	d, err := h.responses.CreateDisplay(c.Request.Context(), response.DisplayInput{WorkspaceID: ws.ID, SurveyID: surveyID, UserID: userID})
	if err != nil {
		h.fail(c, err, msgGeneric)
		return
	}
	writeData(c, d, noStore)
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
	contactRaw, hasContact := is.id("contactId", in.get("contactId"), optional)
	if is.failed() {
		writeError(c, badRequest(msgInvalidFields, is.details))
		return
	}
	var contactID *uuid.UUID
	if hasContact {
		id, _ := legacyid.Resolve(contactRaw)
		contactID = &id
	}
	d, err := h.responses.CreateDisplay(c.Request.Context(), response.DisplayInput{WorkspaceID: ws.ID, SurveyID: surveyID, ContactID: contactID})
	if err != nil {
		h.fail(c, err, msgGeneric)
		return
	}
	writeData(c, d, noStore)
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
