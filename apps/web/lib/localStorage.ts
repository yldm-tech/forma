export const FORMA_SURVEYS_FILTERS_KEY_LS = "forma-surveys-filters";
export const FORMA_WORKFLOWS_FILTERS_KEY_LS = "forma-workflows-filters";
export const FORMA_ENVIRONMENT_ID_LS = "forma-environment-id";
export const FORMA_WORKSPACE_ID_LS = "forma-workspace-id";
export const FORMA_LOGGED_IN_WITH_LS = "forma-logged-in-with";

// Server-readable mirror of the "last active workspace". The proxy sets this from the
// /workspaces/[workspaceId] path so server components (e.g. the workspace-agnostic org-settings
// shell) can resolve the current workspace during render — localStorage is browser-only.
export const FORMA_WORKSPACE_ID_COOKIE = "forma-workspace-id";
