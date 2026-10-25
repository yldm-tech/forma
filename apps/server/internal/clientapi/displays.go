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
	userID, identity, verified, err := h.verifiedUserID(c.Request.Context(), ws.ID, userID, userHash)
	if err != nil {
		h.fail(c, err, msgGeneric)
		return
	}
	d, err := h.responses.CreateDisplay(c.Request.Context(), response.DisplayInput{WorkspaceID: ws.ID, SurveyID: surveyID, UserID: userID})
	if err != nil {
		h.fail(c, err, msgGeneric)
		return
	}
	h.writeDisplay(c, d, func(contactID uuid.UUID) (string, error) {
		return identity.ContactToken(h.cfg, ws.ID, contactID, verified)
	})
}

// createDisplayV2 is forma2's v2 display create, which the survey runtime calls for an anonymous respondent or a known contact: the workspace, then the body (ZDisplayCreateInputV2), then the display, attached to the contact only when it is one of the workspace's and the reference to it may attach it (attachedContact).
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
	ref := contactRef(&is, in.get("contactId"), optional)
	if is.failed() {
		writeError(c, badRequest(msgInvalidFields, is.details))
		return
	}
	survey, _ := rowid.Parse(surveyID)
	contactID, token, err := h.attachedContact(c.Request.Context(), ws.ID, survey, ref)
	if err != nil {
		h.fail(c, err, msgGeneric)
		return
	}
	d, err := h.responses.CreateDisplay(c.Request.Context(), response.DisplayInput{WorkspaceID: ws.ID, SurveyID: surveyID, ContactID: contactID})
	if err != nil {
		h.fail(c, err, msgGeneric)
		return
	}
	h.writeDisplay(c, d, func(uuid.UUID) (string, error) { return token, nil })
}

// writeDisplay answers a created display, {id, contactId, surveyId}, naming its contact by the token name gives it rather than by id, so that a userId sent to the v1 route does not reveal the contact's id.
func (h *Handler) writeDisplay(c *gin.Context, d *response.Display, name func(contactID uuid.UUID) (string, error)) {
	var contactRef any
	if d.ContactID != nil {
		token, err := name(*d.ContactID)
		if err != nil {
			h.fail(c, err, msgGeneric)
			return
		}
		contactRef = token
	}
	writeData(c, fields{{"id", d.ID}, {"contactId", contactRef}, {"surveyId", d.SurveyID}}, noStore)
}

// contactRef reads a v2 body's contactId: a contact token, or a contact id, which must have an id's shape. "" when there is none.
func contactRef(is *issues, v any, p presence) string {
	if s, ok := v.(string); ok && sdkidentity.IsContactToken(s) {
		return s
	}
	raw, _ := is.id("contactId", v, p)
	return raw
}

// attachedContact is the contact a v2 display or response of surveyID is attached to by the contactId ref it names, a contact id or a contact token, and the token the answer names it by. None ("" ref, a token that does not name a contact of this workspace because it was issued under a key since retired or for another workspace, or a reference the workspace's identity settings do not let attach a contact, sdkidentity.Settings.Attaches and AttachesContactID) is no error: the display or response is recorded without a contact, as for an unknown contact id. Under required verification that keeps a contact id, which identify answered to anyone before it answered tokens, or a token an unverified identify got, from attaching answers to someone else's contact.
func (h *Handler) attachedContact(ctx context.Context, workspaceID, surveyID uuid.UUID, ref string) (*uuid.UUID, string, error) {
	if ref == "" {
		return nil, "", nil
	}
	identity, err := sdkidentity.Load(ctx, h.db, h.cipher, workspaceID)
	if err != nil {
		return nil, "", err
	}
	if sdkidentity.IsContactToken(ref) {
		parsed, ok := sdkidentity.ParseContactToken(h.cfg, workspaceID, ref)
		if !ok || !identity.Attaches(parsed, surveyID) {
			return nil, "", nil
		}
		return &parsed.ID, ref, nil
	}
	if !identity.AttachesContactID() {
		return nil, "", nil
	}
	id, _ := rowid.Parse(ref)
	token, err := identity.ContactToken(h.cfg, workspaceID, id, false)
	if err != nil {
		return nil, "", err
	}
	return &id, token, nil
}

// verifiedUserID is the userId a v1 display or response may attach, with the workspace's identity settings and whether userHash verified it: the userId sent, unless the workspace requires verified identities and userHash does not verify it, in which case none, and the display or response is recorded without a contact rather than refused, so no answer is lost.
func (h *Handler) verifiedUserID(ctx context.Context, workspaceID uuid.UUID, userID, userHash *string) (*string, *sdkidentity.Settings, bool, error) {
	identity, err := sdkidentity.Load(ctx, h.db, h.cipher, workspaceID)
	if err != nil {
		return nil, nil, false, err
	}
	if userID == nil || *userID == "" {
		return userID, identity, false, nil
	}
	hash := ""
	if userHash != nil {
		hash = *userHash
	}
	verified := identity.Verified(*userID, hash)
	if identity.Required && !verified {
		return nil, identity, false, nil
	}
	return userID, identity, verified, nil
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
