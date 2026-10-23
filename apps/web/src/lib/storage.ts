// Per-browser conveniences kept in localStorage under forma2's key names. Storage can be unavailable (private windows, blocked site data), and a convenience must never break a page, so every access tolerates that.

/** The workspace the user last opened, which the root page reopens (forma2's FORMA_WORKSPACE_ID_LS). */
export const WORKSPACE_ID_KEY = "forma-workspace-id";
/** The same id under its pre-rename name, which older SDK snippets on the same origin still read (forma2's FORMA_ENVIRONMENT_ID_LS). */
export const ENVIRONMENT_ID_KEY = "forma-environment-id";
/** Whether the main navigation is collapsed (forma2's isMainNavCollapsed). */
export const NAV_COLLAPSED_KEY = "isMainNavCollapsed";

export const readStorage = (key: string): string | null => {
  try {
    return globalThis.localStorage?.getItem(key) ?? null;
  } catch {
    return null;
  }
};

export const writeStorage = (key: string, value: string | null): void => {
  try {
    if (value === null) globalThis.localStorage?.removeItem(key);
    else globalThis.localStorage?.setItem(key, value);
  } catch {
    // Storage is unavailable; the convenience is simply not remembered.
  }
};

export const rememberWorkspace = (workspaceId: string): void => {
  writeStorage(WORKSPACE_ID_KEY, workspaceId);
  writeStorage(ENVIRONMENT_ID_KEY, workspaceId);
};

export const forgetWorkspace = (): void => {
  writeStorage(WORKSPACE_ID_KEY, null);
  writeStorage(ENVIRONMENT_ID_KEY, null);
};
