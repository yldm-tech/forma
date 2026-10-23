package mgmtapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/contact"
	"github.com/yldm-tech/forma/server/internal/platform/userinput"
)

// maxBulkContacts is ZContactBulkUploadRequest's ceiling on one upload.
const maxBulkContacts = 250

// v2CreateContact is forma2's POST /api/v2/management/contacts: the workspace resolved and authorized for write (403), ZContactCreateRequest (attributes must carry a valid email), then the contact; an email or userId another contact has is 409, a key the workspace lacks 400.
func (h *Handler) v2CreateContact(c *gin.Context) {
	body, ok := v2Body(c)
	if !ok {
		return
	}
	workspaceID, ok := h.v2ResolveWorkspace(c, body, http.MethodPost)
	if !ok {
		return
	}
	o := object(body)
	var is issues
	is.id("workspaceId", o.get("workspaceId"), required)
	attributes, valid := is.stringRecord("attributes", o.get("attributes"), required)
	if valid {
		switch email := attributes["email"]; {
		case email == "":
			is.add("attributes", "Email attribute is required")
		case !userinput.ValidEmailFormat(email):
			is.add("attributes", "Invalid email format")
		}
	}
	if is.failed() {
		v2Error(c, http.StatusUnprocessableEntity, is.v2Details())
		return
	}
	if !h.v2Limit(c) {
		return
	}
	if !h.v2CanWrite(c, workspaceID) {
		return
	}
	created, err := h.contacts.CreateManaged(c.Request.Context(), workspaceID, attributes)
	var missing *contact.MissingKeysError
	switch {
	case errors.Is(err, contact.ErrEmailTaken):
		v2Error(c, http.StatusConflict, detail("email", "contact with this email already exists"))
	case errors.Is(err, contact.ErrUserIDTaken):
		v2Error(c, http.StatusConflict, detail("userId", "contact with this userId already exists"))
	case errors.As(err, &missing):
		v2Error(c, http.StatusBadRequest, detail("attributes", "attribute keys not found: "+strings.Join(missing.Keys, ", ")+". "))
	case err != nil:
		h.v2Unexpected(c, err)
	default:
		h.v2Record(c, created.ID.String(), nil, created)
		v2Created(c, created)
	}
}

// v2CanWrite is the contact routes' own check after validation: a key refused write on the workspace is 403.
func (h *Handler) v2CanWrite(c *gin.Context, workspaceID uuid.UUID) bool {
	allowed, err := h.v2Can(c, http.MethodPost, workspaceID)
	if err != nil {
		h.v2Unexpected(c, err)
		return false
	}
	if !allowed {
		v2Error(c, http.StatusForbidden, nil)
		return false
	}
	return true
}

// parseBulkContacts is ZContactBulkUploadRequest's contacts: at most 250, each a list of {attributeKey: {key, name}, value}, then its refinement over all of them (a valid email per contact, no email or userId twice, no key twice within a contact), which zod runs once the list parsed.
func parseBulkContacts(is *issues, v any) []contact.BulkContact {
	list, ok := v.([]any)
	if !ok {
		is.typeError("contacts", "array", v)
		return nil
	}
	if len(list) > maxBulkContacts {
		is.add("contacts", "Maximum 250 contacts allowed at a time.")
	}
	out := make([]contact.BulkContact, len(list))
	for i, e := range list {
		path := index("contacts", i)
		m, ok := e.(map[string]any)
		if !ok {
			is.typeError(path, "object", e)
			continue
		}
		attrsPath := join(path, "attributes")
		attrs, ok := object(m).get("attributes").([]any)
		if !ok {
			is.typeError(attrsPath, "array", object(m).get("attributes"))
			continue
		}
		for j, a := range attrs {
			aPath := index(attrsPath, j)
			am, ok := a.(map[string]any)
			if !ok {
				is.typeError(aPath, "object", a)
				continue
			}
			var attr contact.BulkAttribute
			keyPath := join(aPath, "attributeKey")
			if km, ok := object(am).get("attributeKey").(map[string]any); ok {
				attr.Key, _ = is.str(join(keyPath, "key"), object(km).get("key"), required)
				attr.Name, _ = is.str(join(keyPath, "name"), object(km).get("name"), required)
			} else {
				is.typeError(keyPath, "object", object(am).get("attributeKey"))
			}
			attr.Value, _ = is.str(join(aPath, "value"), object(am).get("value"), required)
			out[i].Attributes = append(out[i].Attributes, attr)
		}
	}
	if is.failed() {
		return out
	}
	var seenEmails, seenUserIDs, duplicateEmails, duplicateUserIDs []string
	for i, c := range out {
		suffix := " for contact at index " + strconv.Itoa(i)
		email := ""
		for _, a := range c.Attributes {
			if a.Key == "email" {
				email = a.Value
				break
			}
		}
		switch {
		case email == "":
			is.add("contacts", "Email attribute is required"+suffix)
		case !userinput.ValidEmailFormat(email):
			is.add("contacts", "Invalid email format"+suffix)
		case contains(seenEmails, email):
			if !contains(duplicateEmails, email) {
				duplicateEmails = append(duplicateEmails, email)
			}
		default:
			seenEmails = append(seenEmails, email)
		}
		for _, a := range c.Attributes {
			if a.Key == "userId" && a.Value != "" {
				if contains(seenUserIDs, a.Value) {
					if !contains(duplicateUserIDs, a.Value) {
						duplicateUserIDs = append(duplicateUserIDs, a.Value)
					}
				} else {
					seenUserIDs = append(seenUserIDs, a.Value)
				}
				break
			}
		}
		counts := map[string]int{}
		var duplicateKeys []string
		for _, a := range c.Attributes {
			counts[a.Key]++
			if counts[a.Key] == 2 {
				duplicateKeys = append(duplicateKeys, a.Key)
			}
		}
		if len(duplicateKeys) > 0 {
			is.addMeta("contacts", "Duplicate attribute keys found"+suffix+". Please ensure each attribute key is unique", map[string]any{"duplicateKeys": duplicateKeys, "contactIndex": i})
		}
	}
	if len(duplicateEmails) > 0 {
		is.addMeta("contacts", "Duplicate emails found in the records, please ensure each email is unique.", map[string]any{"duplicateEmails": duplicateEmails})
	}
	if len(duplicateUserIDs) > 0 {
		is.addMeta("contacts", "Duplicate userIds found in the records, please ensure each userId is unique.", map[string]any{"duplicateUserIds": duplicateUserIDs})
	}
	return out
}

// v2BulkContacts is forma2's PUT /api/v2/management/contacts/bulk: the workspace resolved and authorized for write (403), ZContactBulkUploadRequest, then the upsert. Contacts skipped for a userId another contact holds make the answer a 207 that lists them.
func (h *Handler) v2BulkContacts(c *gin.Context) {
	body, ok := v2Body(c)
	if !ok {
		return
	}
	workspaceID, ok := h.v2ResolveWorkspace(c, body, http.MethodPut)
	if !ok {
		return
	}
	o := object(body)
	var is issues
	is.id("workspaceId", o.get("workspaceId"), required)
	contacts := parseBulkContacts(&is, o.get("contacts"))
	if is.failed() {
		v2Error(c, http.StatusUnprocessableEntity, is.v2Details())
		return
	}
	if !h.v2Limit(c) {
		return
	}
	if !h.v2CanWrite(c, workspaceID) {
		return
	}
	skipped, err := h.contacts.BulkUpsert(c.Request.Context(), workspaceID, contacts)
	var invalid *contact.BulkInvalidError
	if errors.As(err, &invalid) {
		details := make([]v2Detail, len(invalid.Issues))
		for i, issue := range invalid.Issues {
			details[i] = v2Detail{Field: "attributes", Issue: issue}
		}
		v2Error(c, http.StatusBadRequest, details)
		return
	}
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	if len(skipped) > 0 {
		type skippedContact struct {
			Index  int     `json:"index"`
			UserID *string `json:"userId"`
		}
		list := make([]skippedContact, len(skipped))
		for i, idx := range skipped {
			list[i] = skippedContact{Index: idx}
			for _, a := range contacts[idx].Attributes {
				if a.Key == "userId" {
					v := a.Value
					list[i].UserID = &v
					break
				}
			}
		}
		v2Write(c, http.StatusMultiStatus, map[string]any{"data": map[string]any{
			"status": "success", "message": "Contacts bulk upload partially successful. Some contacts were skipped due to conflicting userIds.",
			"meta": map[string]any{"skippedContacts": list},
		}}, false)
		return
	}
	v2OK(c, map[string]any{"status": "success", "message": "Contacts bulk upload successful"}, nil)
}
