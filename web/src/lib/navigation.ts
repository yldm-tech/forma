/** The sign-in page for a signed-out visit to path, carrying where to return, as forma2's proxy built it: an absolute URL on this origin. */
export const loginHref = (path: string, origin: string = globalThis.location.origin): string =>
  `/auth/login?callbackUrl=${encodeURIComponent(origin + path)}`;

export const organizationSettingsPath = (
  organizationId: string,
  slug: "general" | "teams" | "api-keys" | "domain" | "billing"
): string => `/organizations/${organizationId}/settings/${slug}`;

export const workspaceHomePath = (workspaceId: string): string => `/workspaces/${workspaceId}/`;

/** The workspace's own settings pages, nested under the workspace as in forma2 (workspaceSettingsPath). */
export type WorkspaceSettingsSlug = "general" | "look" | "teams" | "languages";

export const workspaceSettingsPath = (workspaceId: string, slug: WorkspaceSettingsSlug): string =>
  `/workspaces/${workspaceId}/settings/workspace/${slug}`;

/** forma2's workspace switcher in the settings sidebar keeps the user on the same settings page of the chosen workspace. */
export const workspaceSwitchPath = (pathname: string, currentId: string, selectedId: string): string =>
  pathname.replace(`/workspaces/${currentId}/`, `/workspaces/${selectedId}/`);

/** forma2's organization switcher: choosing the organization you are in opens its settings, any other opens it. */
export const organizationSwitchPath = (selectedId: string, currentId: string): string =>
  selectedId === currentId ? organizationSettingsPath(currentId, "general") : `/organizations/${selectedId}/`;

/** Items of a switcher, sorted by name as forma2's useSwitcherData sorted them. */
export const sortByName = <T extends { name: string }>(items: readonly T[]): T[] =>
  [...items].sort((a, b) => a.name.localeCompare(b.name));
