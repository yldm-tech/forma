package survey

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
)

const (
	// MaxSDKStateSurveys is how many live app surveys the SDK's workspace state (GET /api/v1/client/:workspaceId/environment) lists, newest first. A live app survey is an in-progress, unarchived survey of type app; the ones past the cap never reach a visitor.
	MaxSDKStateSurveys = 100
	// maxListedSDKStateExclusions bounds how many of the surveys past the cap the survey list names; it still counts all of them.
	maxListedSDKStateExclusions = 20
)

// SDKStateSurvey is a live app survey the SDK's workspace state leaves out.
type SDKStateSurvey struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// SDKState tells the survey list whether the SDK's workspace state holds every live app survey of the workspace. Excluded names the newest of those it leaves out, at most maxListedSDKStateExclusions of them; ExcludedCount counts all.
type SDKState struct {
	Limit              int              `json:"limit"`
	LiveAppSurveyCount int64            `json:"liveAppSurveyCount"`
	ExcludedCount      int64            `json:"excludedCount"`
	Excluded           []SDKStateSurvey `json:"excluded"`
}

// SDKState is the workspace's live app surveys measured against MaxSDKStateSurveys, for anyone who can read the workspace.
func (s *Service) SDKState(ctx context.Context, caller Caller, workspaceID uuid.UUID) (*SDKState, error) {
	if err := s.authz.Require(ctx, caller.Actor, authz.WorkspaceRead, authz.Workspace(workspaceID)); err != nil {
		return nil, err
	}
	total, excluded, err := s.repo.sdkStateExclusions(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return &SDKState{
		Limit:              MaxSDKStateSurveys,
		LiveAppSurveyCount: total,
		ExcludedCount:      max(total-MaxSDKStateSurveys, 0),
		Excluded:           excluded,
	}, nil
}

// sdkStateExclusions counts the workspace's live app surveys and lists the newest of the ones past MaxSDKStateSurveys, in the order and under the filter of the environment state's own query (clientapi buildState), so the list names exactly the surveys the SDK never receives.
func (r *Repository) sdkStateExclusions(ctx context.Context, workspaceID uuid.UUID) (int64, []SDKStateSurvey, error) {
	db := r.db.WithContext(ctx)
	var total int64
	if err := db.Raw(`SELECT count(*) FROM surveys WHERE workspace_id = ? AND type = 'app' AND status = 'inProgress' AND archived_at IS NULL`, workspaceID).Scan(&total).Error; err != nil {
		return 0, nil, fmt.Errorf("count live app surveys: %w", err)
	}
	excluded := []SDKStateSurvey{}
	if total <= MaxSDKStateSurveys {
		return total, excluded, nil
	}
	if err := db.Raw(`SELECT id, name FROM surveys WHERE workspace_id = ? AND type = 'app' AND status = 'inProgress' AND archived_at IS NULL
		ORDER BY created_at DESC, id DESC OFFSET ? LIMIT ?`, workspaceID, MaxSDKStateSurveys, maxListedSDKStateExclusions).Scan(&excluded).Error; err != nil {
		return 0, nil, fmt.Errorf("list live app surveys past the SDK state cap: %w", err)
	}
	return total, excluded, nil
}

// sdkState answers the survey list's warning about live app surveys the SDK never receives.
func (h *Handler) sdkState(c *gin.Context) {
	id, ok := h.param(c, "workspaceId")
	if !ok {
		return
	}
	state, err := h.svc.SDKState(c.Request.Context(), sessionCaller(c), id)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, state)
}
