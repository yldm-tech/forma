import type { TeamPermission, TeamRole, TeamUpdate } from "./queries/teams";

export interface TeamForm {
  name: string;
  members: { userId: string; role: TeamRole }[];
  workspaces: { workspaceId: string; permission: TeamPermission }[];
}

export interface TeamFormErrors {
  name?: string;
  members?: string;
  workspaces?: string;
}

/** forma2's ZTeamSettingsFormSchema: a name, and at least one member and one workspace, each row filled in. */
export const validateTeamForm = (form: TeamForm): TeamFormErrors => {
  const errors: TeamFormErrors = {};
  if (form.name.trim() === "") errors.name = "Team name is required";
  if (form.members.length === 0) errors.members = "Please add at least one member";
  else if (form.members.some((member) => !member.userId)) errors.members = "Please select a member";
  if (form.workspaces.length === 0) errors.workspaces = "Please add at least one workspace";
  else if (form.workspaces.some((workspace) => !workspace.workspaceId)) errors.workspaces = "Please select a workspace";
  return errors;
};

export const toTeamUpdate = (form: TeamForm): TeamUpdate => ({
  name: form.name.trim(),
  members: form.members.filter((member) => member.userId),
  workspaces: form.workspaces.filter((workspace) => workspace.workspaceId),
});

/** The options of one row's picker: everything not chosen in another row, sorted by name. */
export const remainingOptions = <T extends { id: string; name: string }>(
  all: readonly T[],
  chosen: readonly string[],
  current: string
): { value: string; label: string }[] =>
  all
    .filter((item) => !chosen.includes(item.id) || item.id === current)
    .map((item) => ({ value: item.id, label: item.name }))
    .sort((a, b) => a.label.localeCompare(b.label, undefined, { sensitivity: "base" }));
