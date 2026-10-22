import { useSetAtom } from "jotai";
import { useEffect } from "react";
import type { TWorkflowDefinition } from "../definition";
import { reconcileDefinitionEndingCardIds } from "../lib/trigger-ending-cards";
import { prunedTriggerEndingCardIdsAtom, setWorkflowDefinitionAtom } from "../state/editor";
import { useWorkflowSurveyEndings } from "./use-trigger-survey-picker";

/** forma2's useReconcileTriggerEndingCards: drops trigger endingCardIds whose endings no longer exist on the bound survey, so the canvas summary stays honest and enable is not blocked by phantom ids; the autosave persists the repair. Dropped ids go to prunedTriggerEndingCardIdsAtom, since emptying the list reads as "all endings". Skipped while the definition cannot be edited. */
export const useReconcileTriggerEndingCards = ({
  definition,
  isEditable,
}: Readonly<{ definition: TWorkflowDefinition | null; isEditable: boolean }>): void => {
  const setDefinition = useSetAtom(setWorkflowDefinitionAtom);
  const setPrunedEndingCardIds = useSetAtom(prunedTriggerEndingCardIdsAtom);

  const trigger = definition?.trigger ?? null;
  const surveyId = trigger?.config.surveyId ?? null;
  const hasEndingCardIds = (trigger?.config.endingCardIds.length ?? 0) > 0;

  // "All endings" has nothing to reconcile, so there is nothing to fetch.
  const { isSuccess, resolvedSurveyId, endings } = useWorkflowSurveyEndings(
    hasEndingCardIds ? surveyId : null
  );

  useEffect(() => {
    if (!isEditable) return;
    // Only a settled answer for this survey may prune; a pending or failed query must never read as "no endings".
    if (!isSuccess || resolvedSurveyId === null || resolvedSurveyId !== surveyId) return;
    const reconciled = reconcileDefinitionEndingCardIds(
      definition,
      endings.map((ending) => ending.id)
    );
    if (!reconciled) return;
    setDefinition(reconciled.definition);
    // Empty for a de-duplication: the selection still means what the user picked.
    if (reconciled.removedEndingCardIds.length > 0) setPrunedEndingCardIds(reconciled.removedEndingCardIds);
  }, [
    definition,
    endings,
    isEditable,
    isSuccess,
    resolvedSurveyId,
    surveyId,
    setDefinition,
    setPrunedEndingCardIds,
  ]);
};
