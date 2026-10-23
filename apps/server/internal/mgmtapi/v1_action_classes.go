package mgmtapi

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/actionclass"
	"github.com/yldm-tech/forma/server/internal/mgmtapi/mgmtv1"
	"github.com/yldm-tech/forma/server/internal/platform/v1api"
)

// parseActionClassInput is ZActionClassInput (packages/types/action-classes.ts), a union on type: anything but "code" or "noCode" fails on type alone; otherwise the branch's fields are checked in its declared order.
func parseActionClassInput(body map[string]any) (actionclass.Input, *issues) {
	o := object(body)
	var is issues
	var in actionclass.Input
	typ, _ := o.get("type").(string)
	if typ != actionclass.TypeCode && typ != actionclass.TypeNoCode {
		is.add("type", "Invalid input")
		return in, &is
	}
	in.Type = typ
	if name, ok := o.get("name").(string); ok {
		in.Name = jsTrim(name)
		if in.Name == "" {
			is.add("name", "Name must be at least 1 character long")
		}
	} else {
		is.add("name", "Name is required")
	}
	if v := o.get("description"); !isAbsent(v) {
		in.Description = &actionclass.Optional{Value: is.optString("description", v, nullish)}
	}
	if id, ok := is.id("workspaceId", o.get("workspaceId"), required); ok && id == "" {
		is.add("workspaceId", "Workspace ID cannot be empty")
	}
	if typ == actionclass.TypeCode {
		if key, ok := is.str("key", o.get("key"), nullable); ok {
			key = jsTrim(key)
			if key == "" {
				is.add("key", "Too small: expected string to have >=1 characters")
			}
			in.Key = &key
		}
		return in, &is
	}
	config := o.get("noCodeConfig")
	switch {
	case isAbsent(config):
		is.add("noCodeConfig", "Invalid input")
	case config != nil:
		raw, err := json.Marshal(config)
		if err == nil {
			in.NoCodeConfig, err = actionclass.ParseNoCodeConfig(raw)
		}
		if err != nil {
			is.add("noCodeConfig", "Invalid input")
		}
	}
	return in, &is
}

// v1ListActionClasses is forma2's GET /action-classes: every action class of the workspaces the key has a grant on, oldest first.
func (h *Handler) v1ListActionClasses(c *gin.Context) {
	found, err := h.actionClasses.ListManaged(c.Request.Context(), grantedWorkspaces(c))
	if err != nil {
		h.v1.Unexpected(c, err, mgmtv1.MessageGeneric)
		return
	}
	v1OK(c, found)
}

// v1CreateActionClass is forma2's POST /action-classes: the workspace resolved and authorized for write, ZActionClassInput, then the action class; a name or key the workspace uses is 409.
func (h *Handler) v1CreateActionClass(c *gin.Context) {
	body, ok := mgmtv1.ReadBody(c)
	if !ok {
		return
	}
	ws, ok := h.v1.ResolveWorkspace(c, body, http.MethodPost)
	if !ok {
		return
	}
	in, is := parseActionClassInput(body)
	if is.failed() {
		v1api.BadRequest(c, messageInvalidFields, is.v1Details(), true)
		return
	}
	created, err := h.actionClasses.CreateManaged(c.Request.Context(), ws.ID, in)
	if err != nil {
		h.v1Fail(c, err)
		return
	}
	rec := mgmtv1.AuditOf(c)
	rec.TargetID, rec.NewObject = created.ID.String(), created
	v1OK(c, created)
}

// v1ActionClass loads the action class the path names and authorizes method on its workspace, answering itself when it cannot: 400 for a malformed id, 404 "Action Class", 401 when refused.
func (h *Handler) v1ActionClass(c *gin.Context, method string) *actionclass.ActionClass {
	id, ok := v1ID(c, "actionClassId")
	if !ok {
		return nil
	}
	found, err := h.actionClasses.ByID(c.Request.Context(), id)
	if err != nil {
		h.v1Fail(c, err)
		return nil
	}
	if found == nil {
		v1api.NotFound(c, "Action Class", c.Param("actionClassId"), false)
		return nil
	}
	if !h.v1.Authorize(c, method, found.WorkspaceID) {
		return nil
	}
	return found
}

func (h *Handler) v1GetActionClass(c *gin.Context) {
	found := h.v1ActionClass(c, http.MethodGet)
	if found == nil {
		return
	}
	v1OK(c, found)
}

// v1UpdateActionClass is forma2's PUT /action-classes/{id}: workspace.write on the action class's workspace, then a body that must name a workspace the key may write (which the action class never moves to), ZActionClassInput, and the update. The answer lists the surveys the action class triggers.
func (h *Handler) v1UpdateActionClass(c *gin.Context) {
	rec := mgmtv1.AuditOf(c)
	found := h.v1ActionClass(c, http.MethodPut)
	if found == nil {
		return
	}
	rec.TargetID, rec.OldObject = found.ID.String(), found
	body, ok := mgmtv1.ReadBody(c)
	if !ok {
		return
	}
	if _, ok := h.v1.ResolveWorkspace(c, body, http.MethodPut); !ok {
		return
	}
	in, is := parseActionClassInput(body)
	if is.failed() {
		v1api.BadRequest(c, messageInvalidFields, is.v1Details(), false)
		return
	}
	ctx := c.Request.Context()
	updated, err := h.actionClasses.UpdateManaged(ctx, found, in)
	if err != nil {
		h.v1Fail(c, err)
		return
	}
	triggers, err := h.actionClasses.SurveyTriggers(ctx, updated.ID)
	if err != nil {
		h.v1Fail(c, err)
		return
	}
	rec.NewObject = updated
	v1OK(c, struct {
		*actionclass.ActionClass
		SurveyTriggers []actionclass.SurveyTrigger `json:"surveyTriggers"`
	}{updated, triggers})
}

// v1DeleteActionClass is forma2's DELETE /action-classes/{id}: workspace.manage; the surveys it triggered lose the trigger.
func (h *Handler) v1DeleteActionClass(c *gin.Context) {
	rec := mgmtv1.AuditOf(c)
	rec.TargetID = c.Param("actionClassId")
	found := h.v1ActionClass(c, http.MethodDelete)
	if found == nil {
		return
	}
	rec.OldObject = found
	deleted, err := h.actionClasses.DeleteManaged(c.Request.Context(), found)
	if err != nil {
		h.v1Fail(c, err)
		return
	}
	if !deleted {
		v1api.NotFound(c, "Action", found.ID.String(), false)
		return
	}
	v1OK(c, found)
}
