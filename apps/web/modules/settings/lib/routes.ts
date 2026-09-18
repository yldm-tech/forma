// Single source of truth for settings URLs after the route-scoping refactor:
// - organization settings live at /organizations/[organizationId]/settings/*
// - account settings live at /account/settings/*
// - workspace settings stay at /workspaces/[workspaceId]/settings/workspace/*
// Keeping these in one place stops the section bases from drifting back into hardcoded strings.

export const organizationSettingsPath = (organizationId: string, slug: string): string =>
  `/organizations/${organizationId}/settings/${slug}`;

export const accountSettingsPath = (slug: string): string => `/account/settings/${slug}`;

export const workspaceSettingsPath = (workspaceId: string, slug: string): string =>
  `/workspaces/${workspaceId}/settings/workspace/${slug}`;

// Where billing-role users (and other "you can't see this settings page" cases) get sent.
//
// On Cloud that is the billing page, which is theirs. Off Cloud there is nothing for them in the
// organization: every settings page there needs `organization.read_access`, which the billing role
// does not have. It used to land on an enterprise-licence page, but that page became an inventory of
// features that are all on once the licence check was removed — a screen that answered nothing —
// and sending a role to a page that exists only to receive it is not a destination. Their own
// account settings is somewhere they can actually act.
export const getOrganizationBillingPath = (organizationId: string, isFormaCloud: boolean): string =>
  isFormaCloud ? organizationSettingsPath(organizationId, "billing") : "/account/settings/profile";
