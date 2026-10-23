package mgmtapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/contact"
)

// v2ListKeys is forma2's GET /api/v2/management/contact-attribute-keys: the common filter over the keys of the workspaces the key may read.
func (h *Handler) v2ListKeys(c *gin.Context) {
	var is issues
	f := is.commonFilter(v2Query(c))
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
	found, total, err := h.repo.keyPage(c.Request.Context(), ids, f)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	v2OK(c, found, f.meta(total))
}

// v2KeyRules are ZContactAttributeKeyCreateInput's refinement of key, which zod runs only once the members parsed.
func (is *issues) v2KeyRules(key string) {
	if !contact.IsSafeIdentifier(key) {
		is.add("key", "Key must be a safe identifier: only lowercase letters, numbers, and underscores, and must start with a letter")
	}
	if contact.IsReservedKey(key) {
		is.add("key", contact.ReservedKeysIssue([]string{key}))
	}
}

// v2CreateKey is forma2's POST /api/v2/management/contact-attribute-keys: the workspace resolved from workspaceId or environmentId (403 when the key may not write it), ZContactAttributeKeyCreateInput, then a custom key named after itself unless named; a key the workspace has is 409.
func (h *Handler) v2CreateKey(c *gin.Context) {
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
	key, hasKey := is.str("key", o.get("key"), required)
	name := is.optString("name", o.get("name"), nullable)
	description := is.optString("description", o.get("description"), nullable)
	is.id("workspaceId", o.get("workspaceId"), required)
	dataType, hasType := is.enum("dataType", o.get("dataType"), optional, dataTypes)
	if hasKey && !is.aborted {
		is.v2KeyRules(key)
	}
	if is.failed() {
		v2Error(c, http.StatusUnprocessableEntity, is.v2Details())
		return
	}
	if !h.v2Limit(c) {
		return
	}
	if !hasType {
		dataType = contact.TypeString
	}
	label := contact.TitleCase(key)
	if name != nil {
		label = *name
	}
	created, err := h.repo.createKey(c.Request.Context(), workspaceID, key, label, description, dataType)
	if errors.Is(err, errKeyExists) {
		v2Error(c, http.StatusConflict, detail("contactAttributeKey", `Contact attribute key with "`+key+`" already exists`))
		return
	}
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	h.v2Record(c, created.ID.String(), nil, created)
	v2Created(c, created)
}

// v2Key loads the key id names and authorizes method on its workspace, answering itself when it cannot: 404 contactAttributeKey, 401 when refused.
func (h *Handler) v2Key(c *gin.Context, id uuid.UUID, method string) *contact.Key {
	found, err := h.repo.key(c.Request.Context(), id)
	if err != nil {
		h.v2Unexpected(c, err)
		return nil
	}
	if found == nil {
		v2Error(c, http.StatusNotFound, detail("contactAttributeKey", "not found"))
		return nil
	}
	if !h.v2Authorize(c, method, found.WorkspaceID) {
		return nil
	}
	return found
}

func (h *Handler) v2GetKey(c *gin.Context) {
	id, ok := v2PathID(c, "contactAttributeKeyId")
	if !ok || !h.v2Limit(c) {
		return
	}
	found := h.v2Key(c, id, http.MethodGet)
	if found == nil {
		return
	}
	v2OK(c, found, nil)
}

// v2UpdateKey is forma2's PUT /contact-attribute-keys/{id}: name and description, both required and nullable; default and unique keys cannot be changed.
func (h *Handler) v2UpdateKey(c *gin.Context) {
	body, ok := v2Body(c)
	if !ok {
		return
	}
	o := object(body)
	var is issues
	name := is.optString("name", o.get("name"), nullable)
	description := is.optString("description", o.get("description"), nullable)
	if is.failed() {
		v2Error(c, http.StatusUnprocessableEntity, is.v2Details())
		return
	}
	id, ok := v2PathID(c, "contactAttributeKeyId")
	if !ok || !h.v2Limit(c) {
		return
	}
	found := h.v2Key(c, id, http.MethodPut)
	if found == nil {
		return
	}
	h.v2Record(c, found.ID.String(), found, nil)
	switch {
	case found.Type == contact.KeyTypeDefault:
		v2Error(c, http.StatusBadRequest, detail("contactAttributeKey", "cannot update default contact attribute key"))
		return
	case found.IsUnique:
		v2Error(c, http.StatusBadRequest, detail("contactAttributeKey", "cannot update unique contact attribute key"))
		return
	}
	updated, err := h.repo.updateKey(c.Request.Context(), found.ID, keyUpdate{Name: name, Description: description, SetName: true, SetDescription: true})
	if errors.Is(err, errKeyExists) {
		v2Error(c, http.StatusConflict, detail("contactAttributeKey", "Contact attribute key update conflict"))
		return
	}
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	if updated == nil {
		v2Error(c, http.StatusNotFound, detail("contactAttributeKey", "not found"))
		return
	}
	h.v2Record(c, "", nil, updated)
	v2OK(c, updated, nil)
}

// v2DeleteKey is forma2's DELETE /contact-attribute-keys/{id}: workspace.manage; default and unique keys cannot be deleted.
func (h *Handler) v2DeleteKey(c *gin.Context) {
	id, ok := v2PathID(c, "contactAttributeKeyId")
	if !ok || !h.v2Limit(c) {
		return
	}
	found := h.v2Key(c, id, http.MethodDelete)
	if found == nil {
		return
	}
	h.v2Record(c, found.ID.String(), found, nil)
	switch {
	case found.Type == contact.KeyTypeDefault:
		v2Error(c, http.StatusBadRequest, detail("contactAttributeKey", "cannot delete default contact attribute key"))
		return
	case found.IsUnique:
		v2Error(c, http.StatusBadRequest, detail("contactAttributeKey", "cannot delete unique contactAttributeKey"))
		return
	}
	deleted, err := h.repo.deleteKey(c.Request.Context(), found.ID)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	if !deleted {
		v2Error(c, http.StatusNotFound, detail("contactAttributeKey", "not found"))
		return
	}
	v2OK(c, found, nil)
}
