import type { TWorkflowDefinition } from "../definition";

export interface TEndingCardReconciliation {
  /** Stored ids that still resolve to a survey ending, de-duplicated, order preserved. */
  endingCardIds: string[];
  /** Stored ids that no longer resolve to a survey ending. Dropped duplicates are not missing endings and are not listed. */
  removedEndingCardIds: string[];
}

/** forma2's reconcileEndingCardIds: a trigger's stored endingCardIds against the survey's current ending ids. Deleting an ending in the survey editor leaves its id on the triggers that named it, which enable then rejects. */
export const reconcileEndingCardIds = (
  storedEndingCardIds: readonly string[],
  surveyEndingIds: readonly string[]
): TEndingCardReconciliation => {
  const available = new Set(surveyEndingIds);
  const endingCardIds: string[] = [];
  const removedEndingCardIds: string[] = [];
  const seen = new Set<string>();
  for (const endingCardId of storedEndingCardIds) {
    if (seen.has(endingCardId)) continue;
    seen.add(endingCardId);
    if (available.has(endingCardId)) endingCardIds.push(endingCardId);
    else removedEndingCardIds.push(endingCardId);
  }
  return { endingCardIds, removedEndingCardIds };
};

/** The definition with its trigger's ending ids reconciled, or null when the stored selection is already clean, so a caller does not dirty the editor on every render. */
export const reconcileDefinitionEndingCardIds = (
  definition: TWorkflowDefinition | null,
  surveyEndingIds: readonly string[]
): { definition: TWorkflowDefinition; removedEndingCardIds: string[] } | null => {
  const trigger = definition?.trigger;
  if (!definition || !trigger) return null;
  const { endingCardIds, removedEndingCardIds } = reconcileEndingCardIds(
    trigger.config.endingCardIds,
    surveyEndingIds
  );
  // Reconciling only drops entries, so an equal length means nothing changed.
  if (endingCardIds.length === trigger.config.endingCardIds.length) return null;
  return {
    definition: { ...definition, trigger: { ...trigger, config: { ...trigger.config, endingCardIds } } },
    removedEndingCardIds,
  };
};
