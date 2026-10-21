/** The sign-in page for a signed-out visit to path, carrying where to return, as forma2's proxy built it: an absolute URL on this origin. */
export const loginHref = (path: string, origin: string = globalThis.location.origin): string =>
  `/auth/login?callbackUrl=${encodeURIComponent(origin + path)}`;

export const organizationSettingsPath = (
  organizationId: string,
  slug: "general" | "teams" | "api-keys"
): string => `/organizations/${organizationId}/settings/${slug}`;

export const workspaceHomePath = (workspaceId: string): string => `/workspaces/${workspaceId}/`;

/** forma2's organization switcher: choosing the organization you are in opens its settings, any other opens it. */
export const organizationSwitchPath = (selectedId: string, currentId: string): string =>
  selectedId === currentId ? organizationSettingsPath(currentId, "general") : `/organizations/${selectedId}/`;

/** Items of a switcher, sorted by name as forma2's useSwitcherData sorted them. */
export const sortByName = <T extends { name: string }>(items: readonly T[]): T[] =>
  [...items].sort((a, b) => a.name.localeCompare(b.name));
