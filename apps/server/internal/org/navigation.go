package org

import (
	"context"
	"slices"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/workspace"
)

// SPA paths of the setup flow and sign-in, forma2's.
const (
	setupIntroPath = "/setup/intro"
	loginPath      = "/auth/login"
	createOrgPath  = "/setup/organization/create"
)

// TwoFactorPath is the SPA page that guides a member to the two-factor authentication their organization requires.
func TwoFactorPath(organizationID uuid.UUID) string {
	return "/organizations/" + organizationID.String() + "/two-factor"
}

// Home is where the root page sends the browser. With Path empty, the SPA opens the workspace it remembers when that is one of WorkspaceIDs, else the first of them, as forma2's ClientWorkspaceRedirect did.
type Home struct {
	Path         string      `json:"path,omitempty"`
	WorkspaceIDs []uuid.UUID `json:"workspaceIds"`
	// Pending is true while the user belongs to an organization the authorization projection has not delivered yet (a sign-up, an accepted invite or an SSO sign-in that just joined one): the rest of the answer leaves that organization out, so the SPA asks again shortly before following it.
	Pending bool `json:"pending"`
}

// Home is forma2's root page: a signed-out browser goes to the setup intro on a fresh instance and to sign-in otherwise; a user without an organization creates one; a user whose first organization refuses them for want of two-factor authentication, and who can open no other workspace, is guided to turn it on; an owner or manager of their first organization whose onboarding workspace has no survey yet goes through the new-workspace flow; a user who can open no workspace lands on their first organization; anyone else gets the workspaces they can open, in organization order.
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
	listed := make([]uuid.UUID, len(orgs))
	for i, o := range orgs {
		listed[i] = o.ID
	}
	pending, err := s.repo.hasMembershipOutside(ctx, *userID, listed)
	if err != nil {
		return nil, err
	}
	if len(orgs) == 0 {
		return &Home{Path: createOrgPath, WorkspaceIDs: []uuid.UUID{}, Pending: pending}, nil
	}
	ids, err := s.navigation.ReadableIDs(ctx, *userID, listed)
	if err != nil {
		return nil, err
	}
	// The workspaces of an organization whose two-factor requirement refuses the user are left out of ids. Its onboarding is out of reach too, and with nothing else to open, the user is guided to turn it on rather than to an empty landing page.
	first := orgs[0]
	manages := first.Role == RoleOwner || first.Role == RoleManager
	firstBlocked := false
	if len(ids) == 0 || manages {
		blocked, err := s.authz.SecondFactorBlockedOrganizations(ctx, *userID)
		if err != nil {
			return nil, err
		}
		firstBlocked = slices.Contains(blocked, first.ID)
	}
	if len(ids) == 0 && firstBlocked {
		return &Home{Path: TwoFactorPath(first.ID), WorkspaceIDs: ids, Pending: pending}, nil
	}
	if manages && !firstBlocked {
		path, err := s.navigation.OnboardingRedirect(ctx, *userID, first.ID)
		if err != nil {
			return nil, err
		}
		if path != "" {
			return &Home{Path: path, WorkspaceIDs: ids, Pending: pending}, nil
		}
	}
	if len(ids) == 0 {
		return &Home{Path: workspace.LandingPath(orgs[0].ID), WorkspaceIDs: ids, Pending: pending}, nil
	}
	return &Home{WorkspaceIDs: ids, Pending: pending}, nil
}

// Navigation is where the organization route sends the browser, and whether its landing page offers to create a workspace.
type Navigation struct {
	Path               string `json:"path"`
	CanCreateWorkspace bool   `json:"canCreateWorkspace"`
}

// Navigate is forma2's /organizations/{id} route and the landing layout behind it: members only (anyone else gets a not-found), a member the organization's two-factor requirement refuses goes to enrol, a billing member goes to the billing path, a member who can open no workspace lands on the organization's landing page, and anyone else opens their first workspace. forma2 checked for workspaces before the billing role, so billing members, who can open none, always reached the landing page; here they reach the billing path as intended.
func (s *Service) Navigate(ctx context.Context, userID, organizationID uuid.UUID) (*Navigation, error) {
	m, err := s.memberWith(ctx, userID, organizationID, authz.OrganizationRead)
	if err != nil {
		return nil, err
	}
	nav := &Navigation{CanCreateWorkspace: m.Role == RoleOwner || m.Role == RoleManager}
	_, blocked, err := s.repo.secondFactor(ctx, userID, organizationID)
	if err != nil {
		return nil, err
	}
	if blocked {
		nav.Path, nav.CanCreateWorkspace = TwoFactorPath(organizationID), false
		return nav, nil
	}
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
