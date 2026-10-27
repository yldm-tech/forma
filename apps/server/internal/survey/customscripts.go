package survey

import (
	"context"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// customScripts is what a survey write sets of the HTML its public page puts in its head: the scripts, and whether they replace the workspace's or are added to them.
type customScripts struct {
	Scripts *string
	Mode    *string
}

func (in *UpdateInput) customScripts() customScripts {
	return customScripts{Scripts: in.CustomHeadScripts, Mode: in.CustomHeadScriptsMode}
}

func scriptsText(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// scriptsMode reads a missing mode as the column's default, which is also how the page treats it.
func scriptsMode(m *string) string {
	if m == nil || *m == "" {
		return "add"
	}
	return *m
}

// changes reports whether next differs from what the survey stores; stored is nil for a new survey, which has no scripts, in add mode.
func (next customScripts) changes(stored *Survey) bool {
	var scripts, mode *string
	if stored != nil {
		scripts, mode = stored.CustomHeadScripts, stored.CustomHeadScriptsMode
	}
	return scriptsText(next.Scripts) != scriptsText(scripts) || scriptsMode(next.Mode) != scriptsMode(mode)
}

// checkCustomScripts holds a survey's custom head scripts to the rule of the workspace's (workspace.Service.update): they run unsanitized on the survey's public page, so only the organization's owners and managers may change them, and only where the instance runs them at all (config.CustomScripts). An API key needs the manage permission on the workspace, the most a key can hold; a key never holds an organization role. The check applies only when the write changes the scripts or their mode, so an editor who saves a survey a manager gave scripts, with the scripts untouched, still can; clearing them and changing the mode need the role but not the instance's consent, since neither runs anything. stored is nil for a new survey.
//
// apply reports whether the write may set the scripts it carries. A signed-in user without the role who saves an existing survey with different scripts keeps the stored ones instead of being refused: the editor sends the whole survey and shows the field read-only to such a user, so the difference is a manager's change made since the editor loaded, which the save must neither revert nor fail over. An API key, and any write that creates a survey, is refused instead, since there the scripts are what the caller asked for.
func (s *Service) checkCustomScripts(ctx context.Context, caller Caller, workspaceID uuid.UUID, stored *Survey, next customScripts) (apply bool, err error) {
	if !next.changes(stored) {
		return false, nil
	}
	if caller.Actor.Type == authz.ActorAPIKey {
		if err := s.authz.Require(ctx, caller.Actor, authz.WorkspaceManage, authz.Workspace(workspaceID)); err != nil {
			return false, err
		}
	} else {
		org, err := s.repo.organizationOf(ctx, workspaceID)
		if err != nil {
			return false, err
		}
		if org == nil {
			return false, apperr.NotFound("Organization", "")
		}
		if stored != nil {
			allowed, err := s.authz.Can(ctx, caller.Actor, authz.OrganizationManage, authz.Organization(*org))
			if err != nil {
				return false, err
			}
			if !allowed {
				return false, nil
			}
		} else if err := s.authz.Require(ctx, caller.Actor, authz.OrganizationManage, authz.Organization(*org)); err != nil {
			return false, err
		}
	}
	var storedScripts *string
	if stored != nil {
		storedScripts = stored.CustomHeadScripts
	}
	if text := scriptsText(next.Scripts); text != "" && text != scriptsText(storedScripts) && s.cfg.CustomScripts() != config.CustomScriptsAvailable {
		return false, apperr.OperationNotAllowed(s.cfg.CustomScriptsRefusal())
	}
	return true, nil
}
