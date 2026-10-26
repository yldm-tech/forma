package workflow

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
)

// TestSendResult is a test send's outcome: whether the receiver accepted it, the status it answered with, and the reason it failed.
type TestSendResult struct {
	OK      bool   `json:"ok"`
	Status  int    `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

// maxStepIDLength bounds a step id read from a path.
const maxStepIDLength = 200

// TestSend sends one http_request or chat step of the workflow's saved draft once, now, with no response: the placeholders the workflow and its survey can fill are filled and every other one is sent as written, so the receiver sees where each value goes. Only these steps send to a receiver someone can check; tagging and attribute steps would change data and have no test. It needs write access to the workspace, and for an http_request the access that may author one.
func (s *Service) TestSend(ctx context.Context, userID, workflowID uuid.UUID, stepID string) (*TestSendResult, error) {
	w, err := s.repo.byID(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, apperr.NotFound("Workflow", workflowID.String())
	}
	actor := authz.User(userID)
	if err := s.authz.Require(ctx, actor, authz.WorkspaceWrite, authz.Workspace(w.WorkspaceID)); err != nil {
		return nil, err
	}
	d, issues := ParseDefinition(w.Definition)
	if len(issues) > 0 {
		return nil, apperr.BadRequest("The workflow's definition is not valid.")
	}
	var step *Node
	for i := range d.Nodes {
		if d.Nodes[i].ID == stepID {
			step = &d.Nodes[i]
		}
	}
	if step == nil {
		return nil, apperr.NotFound("Workflow step", stepID)
	}
	if step.HTTP == nil && step.Chat == nil {
		return nil, apperr.BadRequest("Only HTTP request and chat message steps can send a test.")
	}
	if step.HTTP != nil {
		if err := s.authz.Require(ctx, actor, authz.WorkspaceManage, authz.Workspace(w.WorkspaceID)); err != nil {
			return nil, err
		}
	}
	if len(ActionProblems(*step)) > 0 {
		return nil, apperr.BadRequest("Finish setting up this step and let it save before sending a test.")
	}
	surveyID, surveyName := "", ""
	if d.Trigger != nil {
		if id, err := uuid.Parse(d.Trigger.Config.SurveyID); err == nil {
			var names []string
			if err := s.db.WithContext(ctx).Raw(`SELECT name FROM surveys WHERE id = ? AND workspace_id = ?`, id, w.WorkspaceID).Scan(&names).Error; err != nil {
				return nil, fmt.Errorf("load trigger survey name: %w", err)
			}
			if len(names) > 0 {
				surveyID, surveyName = id.String(), names[0]
			}
		}
	}
	messageID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("test send message id: %w", err)
	}
	env := actionEnv{workspaceID: w.WorkspaceID, template: testTemplate(w.ID, surveyID, surveyName), messageID: "test_" + messageID.String()}
	o := s.actions.perform(ctx, env, *step)
	result := &TestSendResult{OK: o.status == StepSucceeded, Message: o.err}
	if status, ok := o.output["status"].(int); ok {
		result.Status = status
	}
	return result, nil
}

func (h *Handler) testSend(c *gin.Context) {
	workflowID, err := httpapi.ParamUUID(c, "workflowId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	stepID := c.Param("stepId")
	if stepID == "" || len(stepID) > maxStepIDLength {
		httpapi.Fail(c, apperr.BadRequest("Invalid step id"))
		return
	}
	a, _ := httpapi.ActorFrom(c.Request.Context())
	res, err := h.svc.TestSend(c.Request.Context(), a.UserID, workflowID, stepID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}
