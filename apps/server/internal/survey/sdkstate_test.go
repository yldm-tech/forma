package survey

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

// The survey list's SDK state names the live app surveys past the environment state's cap: the oldest ones, newest of them first, with drafts, link surveys and archived ones left out of both the count and the list.
func TestSDKState(t *testing.T) {
	e := newEnv(t)
	owner, reader, outsider := e.user(), e.user(), e.user()
	org, ws := e.organization(owner)
	e.member(org, reader, "member")
	e.grant(org, ws, reader, "read")
	path := "/api/app/workspaces/" + ws.String() + "/sdk-state"

	var state SDKState
	e.ok(e.do(owner, http.MethodGet, path, nil), http.StatusOK, &state)
	if state.Limit != MaxSDKStateSurveys || state.LiveAppSurveyCount != 0 || state.ExcludedCount != 0 || state.Excluded == nil || len(state.Excluded) != 0 {
		t.Fatalf("empty workspace %+v", state)
	}

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range MaxSDKStateSurveys + 2 {
		e.exec(`INSERT INTO surveys (workspace_id, name, status, type, created_at) VALUES (?, ?, 'inProgress', 'app', ?)`, ws, fmt.Sprintf("app %03d", i), base.Add(time.Duration(i)*time.Hour))
	}
	// Older than every live one, so they would be the first excluded if they counted.
	e.exec(`INSERT INTO surveys (workspace_id, name, status, type, created_at) VALUES (?, 'draft app', 'draft', 'app', ?)`, ws, base.Add(-time.Hour))
	e.exec(`INSERT INTO surveys (workspace_id, name, status, type, created_at) VALUES (?, 'live link', 'inProgress', 'link', ?)`, ws, base.Add(-2*time.Hour))
	e.exec(`INSERT INTO surveys (workspace_id, name, status, type, created_at, archived_at) VALUES (?, 'archived app', 'inProgress', 'app', ?, now())`, ws, base.Add(-3*time.Hour))

	e.ok(e.do(reader, http.MethodGet, path, nil), http.StatusOK, &state)
	if state.LiveAppSurveyCount != MaxSDKStateSurveys+2 || state.ExcludedCount != 2 || len(state.Excluded) != 2 {
		t.Fatalf("over the cap %+v", state)
	}
	if state.Excluded[0].Name != "app 001" || state.Excluded[1].Name != "app 000" {
		t.Fatalf("excluded %+v, want app 001 then app 000", state.Excluded)
	}

	// Past maxListedSDKStateExclusions the list stops growing; the count does not.
	for i := range maxListedSDKStateExclusions {
		e.exec(`INSERT INTO surveys (workspace_id, name, status, type, created_at) VALUES (?, ?, 'inProgress', 'app', ?)`, ws, fmt.Sprintf("old %03d", i), base.Add(-time.Duration(10+i)*time.Hour))
	}
	e.ok(e.do(owner, http.MethodGet, path, nil), http.StatusOK, &state)
	if state.ExcludedCount != int64(2+maxListedSDKStateExclusions) || len(state.Excluded) != maxListedSDKStateExclusions || state.Excluded[0].Name != "app 001" {
		t.Fatalf("many excluded %+v", state)
	}

	e.fails(e.do(outsider, http.MethodGet, path, nil), http.StatusForbidden, apperr.CodeForbidden, "")
}
