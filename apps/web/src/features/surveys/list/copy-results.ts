/** The outcome of copying a survey to several workspaces at once, forma2's CopySurveyModal onSubmit: how many copies succeeded, and each failure with the workspace it was for. */
export interface CopyOutcome {
  successCount: number;
  failures: { workspaceId: string; workspaceName: string; error: unknown }[];
}

export const summarizeCopies = (
  targetIds: string[],
  settled: PromiseSettledResult<unknown>[],
  workspaces: { id: string; name: string }[]
): CopyOutcome => {
  const outcome: CopyOutcome = { successCount: 0, failures: [] };
  settled.forEach((result, index) => {
    if (result.status === "fulfilled") {
      outcome.successCount++;
      return;
    }
    const workspaceId = targetIds[index] ?? "";
    outcome.failures.push({
      workspaceId,
      workspaceName: workspaces.find((w) => w.id === workspaceId)?.name ?? workspaceId,
      error: result.reason,
    });
  });
  return outcome;
};

/** The workspaces a survey can be copied to: the writable ones other than its own, by name. */
export const copyTargets = <T extends { id: string; name: string }>(
  writable: T[],
  currentWorkspaceId: string
): T[] => writable.filter((w) => w.id !== currentWorkspaceId).sort((a, b) => a.name.localeCompare(b.name));
