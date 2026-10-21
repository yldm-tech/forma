package org

import (
	"context"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/workspace"
)

// SPA paths of the setup flow and sign-in, forma2's.
const (
	setupIntroPath = "/setup/intro"
	loginPath      = "/auth/login"
	createOrgPath  = "/setup/organization/create"
)

// Home is where the root page sends the browser. With Path empty, the SPA opens the workspace it remembers when that is one of WorkspaceIDs, else the first of them, as forma2's ClientWorkspaceRedirect did.
type Home struct {
	Path         string      `json:"path,omitempty"`
	WorkspaceIDs []uuid.UUID `json:"workspaceIds"`
}

// Home is forma2's root page: a signed-out browser goes to the setup intro on a fresh instance and to sign-in otherwise; a user without an organization creates one; an owner or manager of their first organization goes through the onboarding gate; a user who can open no workspace lands on their first organization; anyone else gets the workspaces they can open, in organization order.
func (s *Service) Home(ctx context.Context, userID *uuid.UUID) (*Home, error) {
	if userID == nil {
		hasUser, err := s.repo.hasAnyUser(ctx)
		if err != nil {
			return nil, err
		}
		if !hasUser {
			return &Home{Path: setupIntroPath, WorkspaceIDs: []uuid.UUID{}}, nil
		}
		return &Home{Path: loginPath, WorkspaceIDs: []uuid.UUID{}}, nil
	}
	orgs, err := s.List(ctx, *userID, 0)
	if err != nil {
		return nil, err
	}
	if len(orgs) == 0 {
		return &Home{Path: createOrgPath, WorkspaceIDs: []uuid.UUID{}}, nil
	}
	first := orgs[0]
	if first.Role == RoleOwner || first.Role == RoleManager {
		path, err := s.navigation.OnboardingRedirect(ctx, *userID, first.ID)
		if err != nil {
			return nil, err
		}
		if path != "" {
			return &Home{Path: path, WorkspaceIDs: []uuid.UUID{}}, nil
		}
	}
	orgIDs := make([]uuid.UUID, len(orgs))
	for i, o := range orgs {
		orgIDs[i] = o.ID
	}
	ids, err := s.navigation.ReadableIDs(ctx, *userID, orgIDs)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return &Home{Path: workspace.LandingPath(first.ID), WorkspaceIDs: ids}, nil
	}
	return &Home{WorkspaceIDs: ids}, nil
}

// Navigation is where the organization route sends the browser, and whether its landing page offers to create a workspace.
type Navigation struct {
	Path               string `json:"path"`
	CanCreateWorkspace bool   `json:"canCreateWorkspace"`
}

// Navigate is forma2's /organizations/{id} route and the landing layout behind it: members only (anyone else gets a not-found), a billing member goes to the billing path, a member who can open no workspace lands on the organization's landing page, and anyone else opens their first workspace. forma2 checked for workspaces before the billing role, so billing members, who can open none, always reached the landing page; here they reach the billing path as intended.
func (s *Service) Navigate(ctx context.Context, userID, organizationID uuid.UUID) (*Navigation, error) {
	m, err := s.memberWith(ctx, userID, organizationID, authz.OrganizationRead)
	if err != nil {
		return nil, err
	}
	nav := &Navigation{CanCreateWorkspace: m.Role == RoleOwner || m.Role == RoleManager}
	if m.Role == RoleBilling {
		nav.Path = workspace.BillingPath(organizationID, s.cfg.IsFormaCloud)
		return nav, nil
	}
	ids, err := s.navigation.ReadableIDs(ctx, userID, []uuid.UUID{organizationID})
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		nav.Path = workspace.LandingPath(organizationID)
		return nav, nil
	}
	nav.Path = workspace.HomePath(ids[0])
	return nav, nil
}
