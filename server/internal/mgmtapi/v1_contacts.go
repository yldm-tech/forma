package mgmtapi

import (
	"errors"
	"math"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/contact"
	"github.com/yldm-tech/forma/server/internal/mgmtapi/mgmtv1"
	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/server/internal/platform/v1api"
)

// contactsPageLimit is forma2's CONTACTS_PAGE_LIMIT_MAX: the most contacts one v1 page returns, and its size when the caller names none.
const contactsPageLimit = 500

// positiveInt is forma2's readPositiveInt: an integer above zero, or nothing; any other value is ignored rather than refused.
func positiveInt(c *gin.Context, name string) (int, bool) {
	raw := c.Query(name)
	if raw == "" {
		return 0, false
	}
	n := jsvalue.ParseNumber(raw)
	if n != math.Trunc(n) || n <= 0 || math.IsInf(n, 0) {
		return 0, false
	}
	return int(n), true
}

// v1ListContacts is forma2's GET /contacts: the raw contacts of every workspace the key has a grant on, newest first, at most 500 a page.
func (h *Handler) v1ListContacts(c *gin.Context) {
	limit := contactsPageLimit
	if n, ok := positiveInt(c, "limit"); ok {
		limit = min(n, contactsPageLimit)
	}
	offset, _ := positiveInt(c, "skip")
	found, err := h.repo.contacts(c.Request.Context(), grantedWorkspaces(c), limit, offset)
	if err != nil {
		h.v1.Unexpected(c, err, mgmtv1.MessageUnexpected)
		return
	}
	v1OK(c, found)
}

// v1Contact loads the contact the path names and authorizes method on its workspace, answering itself when it cannot.
func (h *Handler) v1Contact(c *gin.Context, method string) *contactRow {
	id, ok := v1ID(c, "contactId")
	if !ok {
		return nil
	}
	found, err := h.repo.contact(c.Request.Context(), id)
	if err != nil {
		h.v1Fail(c, err)
		return nil
	}
	if found == nil {
		v1api.NotFound(c, "Contact", c.Param("contactId"), false)
		return nil
	}
	if !h.v1.Authorize(c, method, found.WorkspaceID) {
		return nil
	}
	return found
}

func (h *Handler) v1GetContact(c *gin.Context) {
	found := h.v1Contact(c, http.MethodGet)
	if found == nil {
		return
	}
	v1OK(c, found)
}

// v1DeleteContact is forma2's DELETE /contacts/{id}: workspace.manage, then the contact with its attribute values and responses.
func (h *Handler) v1DeleteContact(c *gin.Context) {
	rec := mgmtv1.AuditOf(c)
	rec.TargetID = c.Param("contactId")
	found := h.v1Contact(c, http.MethodDelete)
	if found == nil {
		return
	}
	rec.OldObject = found
	deleted, err := h.repo.deleteContact(c.Request.Context(), found.ID)
	if err != nil {
		h.v1Fail(c, err)
		return
	}
	if !deleted {
		v1api.NotFound(c, "Contact", c.Param("contactId"), false)
		return
	}
	v1OK(c, map[string]any{"success": "Contact deleted successfully"})
}

// v1ListContactAttributes is forma2's GET /contact-attributes: every attribute value under a key of a workspace the key has a grant on, unpaginated.
func (h *Handler) v1ListContactAttributes(c *gin.Context) {
	found, err := h.repo.contactAttributes(c.Request.Context(), grantedWorkspaces(c))
	if err != nil {
		h.v1Fail(c, err)
		return
	}
	v1OK(c, found)
}

// v1ListKeys is forma2's GET /contact-attribute-keys: the keys of every workspace the key has a grant on.
func (h *Handler) v1ListKeys(c *gin.Context) {
	found, err := h.repo.keys(c.Request.Context(), grantedWorkspaces(c))
	if err != nil {
		h.v1Fail(c, err)
		return
	}
	v1OK(c, found)
}

// keyRules are ZContactAttributeKeyCreateInput's refinements of key: a safe identifier that is not reserved.
func (is *issues) keyRules(path, key string) {
	if !contact.IsSafeIdentifier(key) {
		is.add(path, "Key must be a safe identifier: only lowercase letters, numbers, and underscores, and must start with a letter")
	}
	if contact.IsReservedKey(key) {
		is.add(path, contact.ReservedKeyValidationMessage)
	}
}

// dataTypes are ZContactAttributeDataType's values.
var dataTypes = []string{contact.TypeString, contact.TypeNumber, contact.TypeDate}

// v1CreateKey is forma2's POST /contact-attribute-keys: the workspace resolved and authorized for write, ZContactAttributeKeyCreateInput (type must be "custom"), then the key, named after itself when no name is given. A key the workspace has is 400, and so is any other database failure; the 150-key ceiling is an unexpected error, as forma2 did not catch it.
func (h *Handler) v1CreateKey(c *gin.Context) {
	body, ok := mgmtv1.ReadBody(c)
	if !ok {
		return
	}
	ws, ok := h.v1.ResolveWorkspace(c, body, http.MethodPost)
	if !ok {
		return
	}
	o := object(body)
	var is issues
	key, hasKey := is.str("key", o.get("key"), required)
	if hasKey {
		is.keyRules("key", key)
	}
	description := is.optString("description", o.get("description"), optional)
	is.enum("type", o.get("type"), required, []string{contact.KeyTypeCustom})
	dataType, hasType := is.enum("dataType", o.get("dataType"), optional, dataTypes)
	is.str("workspaceId", o.get("workspaceId"), required)
	name := is.optString("name", o.get("name"), optional)
	if is.failed() {
		v1api.BadRequest(c, messageInvalidFields, is.v1Details(), true)
		return
	}
	ctx := c.Request.Context()
	n, err := h.repo.keyCount(ctx, ws.ID)
	if err != nil {
		h.v1Fail(c, err)
		return
	}
	if n >= contact.MaxAttributeKeys {
		h.v1.Unexpected(c, errors.New("maximum number of attribute keys reached"), mgmtv1.MessageUnexpected)
		return
	}
	if !hasType {
		dataType = contact.TypeString
	}
	label := contact.TitleCase(key)
	if name != nil {
		label = *name
	}
	if description == nil {
		empty := ""
		description = &empty
	}
	created, err := h.repo.createKey(ctx, ws.ID, key, label, description, dataType)
	if errors.Is(err, errKeyExists) {
		v1api.BadRequest(c, "Attribute key already exists", nil, false)
		return
	}
	if err != nil {
		v1api.BadRequest(c, err.Error(), nil, false)
		return
	}
	rec := mgmtv1.AuditOf(c)
	rec.TargetID, rec.NewObject = created.ID.String(), created
	v1OK(c, created)
}

// v1Key loads the key the path names, unvalidated as forma2 read it, and authorizes method on its workspace, answering itself when it cannot.
func (h *Handler) v1Key(c *gin.Context, method string) *contact.Key {
	found, err := h.repo.key(c.Request.Context(), v1LooseID(c, "contactAttributeKeyId"))
	if err != nil {
		h.v1Fail(c, err)
		return nil
	}
	if found == nil {
		v1api.NotFound(c, "Attribute Key", c.Param("contactAttributeKeyId"), false)
		return nil
	}
	if !h.v1.Authorize(c, method, found.WorkspaceID) {
		return nil
	}
	return found
}

func (h *Handler) v1GetKey(c *gin.Context) {
	found := h.v1Key(c, http.MethodGet)
	if found == nil {
		return
	}
	v1OK(c, found)
}

// v1UpdateKey is forma2's PUT /contact-attribute-keys/{id}: workspace.write, then ZContactAttributeKeyUpdateInput, which may also change the key itself and its data type.
func (h *Handler) v1UpdateKey(c *gin.Context) {
	rec := mgmtv1.AuditOf(c)
	rec.TargetID = c.Param("contactAttributeKeyId")
	found := h.v1Key(c, http.MethodPut)
	if found == nil {
		return
	}
	rec.OldObject = found
	body, ok := mgmtv1.ReadBody(c)
	if !ok {
		return
	}
	o := object(body)
	var is issues
	var u keyUpdate
	u.Description = is.optString("description", o.get("description"), optional)
	u.Name = is.optString("name", o.get("name"), optional)
	if key, ok := is.str("key", o.get("key"), optional); ok {
		is.keyRules("key", key)
		u.Key = &key
	}
	if dataType, ok := is.enum("dataType", o.get("dataType"), optional, dataTypes); ok {
		u.DataType = &dataType
	}
	if is.failed() {
		v1api.BadRequest(c, messageInvalidFields, is.v1Details(), false)
		return
	}
	updated, err := h.repo.updateKey(c.Request.Context(), found.ID, u)
	if errors.Is(err, errKeyExists) {
		v1api.BadRequest(c, "Attribute key already exists", nil, false)
		return
	}
	if err != nil {
		h.v1Fail(c, err)
		return
	}
	if updated == nil {
		v1api.InternalError(c, "Some error occurred while updating contact attribute key")
		return
	}
	rec.NewObject = updated
	v1OK(c, updated)
}

// v1DeleteKey is forma2's DELETE /contact-attribute-keys/{id}: workspace.manage; a default key is refused.
func (h *Handler) v1DeleteKey(c *gin.Context) {
	rec := mgmtv1.AuditOf(c)
	rec.TargetID = c.Param("contactAttributeKeyId")
	found := h.v1Key(c, http.MethodDelete)
	if found == nil {
		return
	}
	rec.OldObject = found
	if found.Type == contact.KeyTypeDefault {
		v1api.BadRequest(c, "Default Contact Attribute Keys cannot be deleted", nil, false)
		return
	}
	deleted, err := h.repo.deleteKey(c.Request.Context(), found.ID)
	if err != nil {
		h.v1Fail(c, err)
		return
	}
	if !deleted {
		v1api.NotFound(c, "Attribute Key", c.Param("contactAttributeKeyId"), false)
		return
	}
	v1OK(c, found)
}
