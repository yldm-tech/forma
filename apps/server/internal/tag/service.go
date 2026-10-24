package tag

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/v3api"
)

// MaxNameLength is the longest tag name, ZV3RenameTagBody's bound.
const MaxNameLength = 255

// CodeNameTaken is forma2's TagError.TAG_NAME_ALREADY_EXISTS, the stable code of a name another tag of the workspace already has.
const CodeNameTaken = "tag_name_already_exists"

// Service holds the tag rules. Every method authorizes through authz before it reads or writes anything.
type Service struct {
	repo    *Repository
	authz   *authz.Authorizer
	limiter *ratelimit.Limiter
	audit   *audit.Emitter
}

func NewService(repo *Repository, authorizer *authz.Authorizer, limiter *ratelimit.Limiter, emitter *audit.Emitter) *Service {
	return &Service{repo: repo, authz: authorizer, limiter: limiter, audit: emitter}
}

// NormalizeName is ZV3RenameTagBody's name rule: trimmed, 1 to 255 characters. It returns false for a name that breaks it.
func NormalizeName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	n := utf8.RuneCountInString(name)
	return name, n >= 1 && n <= MaxNameLength
}

// Create is forma2's createTagAction from the response view: workspace.write, then the state-mutation limit per workspace. forma2 stored the name as typed; here it follows the rename rule (trimmed, 1 to 255 characters), so a tag cannot be created with a name it could never be renamed back to.
func (s *Service) Create(ctx context.Context, userID, workspaceID uuid.UUID, name string) (*Tag, error) {
	var org *uuid.UUID
	t, err := func() (*Tag, error) {
		if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceWrite, authz.Workspace(workspaceID)); err != nil {
			return nil, err
		}
		var err error
		if org, err = s.repo.organizationOf(ctx, workspaceID); err != nil {
			return nil, err
		}
		if err := s.limiter.Apply(ctx, ratelimit.ActionStateMutation, workspaceID.String()); err != nil {
			return nil, err
		}
		t, err := s.repo.create(ctx, workspaceID, name)
		if errors.Is(err, errNameTaken) {
			return nil, apperr.InvalidInput(CodeNameTaken, "Tag with this name already exists")
		}
		return t, err
	}()
	e := audit.Event{Action: audit.ActionCreated, TargetType: audit.TargetTag, ActorType: audit.ActorUser, Status: audit.StatusSuccess, NewObject: t}
	if org != nil {
		e.OrganizationID = org.String()
	}
	if t != nil {
		e.TargetID = t.ID.String()
	}
	if err != nil {
		e.Status, e.NewObject = audit.StatusFailure, nil
	}
	s.audit.Log(ctx, audit.FromRequest(ctx, e))
	return t, err
}

// ListV3 is GET /api/v3/tags: read access to the workspace, every tag with its response count.
func (s *Service) ListV3(ctx context.Context, p *v3api.Principal, workspaceID uuid.UUID) ([]Counted, error) {
	if err := v3api.RequireWorkspace(ctx, s.authz, p, authz.WorkspaceRead, workspaceID); err != nil {
		return nil, err
	}
	return s.repo.ofWorkspace(ctx, workspaceID)
}

// Change is what a v3 mutation tells its audit event: the organization and the target once they are known, and the objects.
type Change struct {
	OrganizationID string
	TargetID       string
	OldObject      any
	NewObject      any
}

// authorizeMutation is forma2's authorizeTagMutation: the workspace comes from the tag itself, never from the request, and must grant workspace.write. An unknown tag is the same 403 as a foreign one, so tag ids cannot be probed.
func (s *Service) authorizeMutation(ctx context.Context, p *v3api.Principal, id uuid.UUID, change *Change) (*Tag, error) {
	t, err := s.repo.byID(ctx, id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, v3api.Forbidden(v3api.ForbiddenDetail)
	}
	if err := v3api.RequireWorkspace(ctx, s.authz, p, authz.WorkspaceWrite, t.WorkspaceID); err != nil {
		return nil, err
	}
	org, err := s.repo.organizationOf(ctx, t.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if org != nil {
		change.OrganizationID = org.String()
	}
	return t, nil
}

// RenameV3 is PATCH /api/v3/tags/{tagId}. A name another tag of the workspace has is a 422 naming the field; anything else that goes wrong is a 500. The answer carries the tag's current count.
func (s *Service) RenameV3(ctx context.Context, p *v3api.Principal, id uuid.UUID, name string, change *Change) (*Counted, error) {
	old, err := s.authorizeMutation(ctx, p, id, change)
	if err != nil {
		return nil, err
	}
	change.TargetID, change.OldObject = id.String(), old
	t, err := s.repo.rename(ctx, id, old.WorkspaceID, name)
	switch {
	case errors.Is(err, errNameTaken):
		return nil, v3api.Unprocessable("Unable to update tag", v3api.InvalidParam{Name: "name", Reason: CodeNameTaken})
	case err != nil:
		return nil, v3api.Unexpected("Unable to update tag", err)
	case t == nil:
		return nil, v3api.Unexpected("Unable to update tag", errors.New("tag deleted during rename"))
	}
	change.NewObject = t
	n, err := s.repo.count(ctx, id)
	if err != nil {
		return nil, err
	}
	return &Counted{Tag: *t, Count: n}, nil
}

// DeleteV3 is DELETE /api/v3/tags/{tagId}: the tag and every response link to it.
func (s *Service) DeleteV3(ctx context.Context, p *v3api.Principal, id uuid.UUID, change *Change) error {
	old, err := s.authorizeMutation(ctx, p, id, change)
	if err != nil {
		return err
	}
	change.TargetID = id.String()
	deleted, err := s.repo.delete(ctx, id, old.WorkspaceID)
	if err != nil {
		return v3api.Unexpected("Unable to delete tag", err)
	}
	if !deleted {
		return v3api.Unexpected("Unable to delete tag", errors.New("tag deleted concurrently"))
	}
	change.OldObject = old
	return nil
}

// MergeV3 is POST /api/v3/tags/{tagId}/merge: both tags must be in the same workspace, since merging would otherwise move responses across a tenant boundary, and a tag cannot be merged into itself.
func (s *Service) MergeV3(ctx context.Context, p *v3api.Principal, id, targetID uuid.UUID, change *Change) error {
	source, err := s.authorizeMutation(ctx, p, id, change)
	if err != nil {
		return err
	}
	if id == targetID {
		return v3api.BadRequest("A tag cannot be merged into itself", v3api.InvalidParam{Name: "newTagId", Reason: "must differ from the tag being merged away"})
	}
	target, err := s.repo.byID(ctx, targetID)
	if err != nil {
		return err
	}
	if target == nil {
		return v3api.Forbidden(v3api.ForbiddenDetail)
	}
	if target.WorkspaceID != source.WorkspaceID {
		return v3api.Forbidden("Tags must be in the same workspace")
	}
	change.TargetID, change.OldObject = id.String()+"-"+targetID.String(), source
	merged, err := s.repo.merge(ctx, id, targetID, source.WorkspaceID)
	if err != nil {
		return v3api.Unexpected("Unable to merge tags", err)
	}
	if !merged {
		return v3api.Unexpected("Unable to merge tags", errors.New("tag deleted during merge"))
	}
	change.NewObject = target
	return nil
}
