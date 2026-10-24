import { ZWorkspace } from "@forma/types/workspace";

// forma2's rules for the workspace general settings forms (packages/types/workspace.ts, modules/workspaces/settings/general/lib).

/** The message for a workspace name ZWorkspace refuses ("Workspace name cannot be empty"), or undefined. */
export const workspaceNameError = (name: string): string | undefined =>
  ZWorkspace.shape.name.safeParse(name).error?.issues[0]?.message;

/** The message for a cooldown period ZWorkspace refuses: a whole number of days from 0 to 365. The field holds text, parsed as forma2's input parsed it. */
export const cooldownError = (value: string): string | undefined =>
  ZWorkspace.shape.recontactDays.safeParse(Number.parseInt(value, 10)).error?.issues[0]?.message;

const normalizeConfirmation = (value: string) => value.trim().toLowerCase();

/** forma2's hasMatchingWorkspaceDeleteConfirmation: the typed name matches ignoring case and surrounding space. */
export const hasMatchingWorkspaceDeleteConfirmation = (
  confirmationName: string,
  workspaceName: string
): boolean => normalizeConfirmation(confirmationName) === normalizeConfirmation(workspaceName);

/** forma2's truncate: the first length characters and an ellipsis. */
export const truncate = (value: string, length: number): string =>
  value.length > length ? `${value.substring(0, length)}...` : value;
