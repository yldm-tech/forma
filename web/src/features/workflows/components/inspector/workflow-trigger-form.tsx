import { useAtomValue, useSetAtom } from "jotai";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Checkbox } from "../../../../components/ui/checkbox";
import { Label } from "../../../../components/ui/label";
import { Select } from "../../../../components/ui/select";
import { cn } from "../../../../lib/cn";
import type { TWorkflowResponseCompletedTriggerNode } from "../../definition";
import { useWorkflowSurveyEndings, useWorkflowSurveyOptions } from "../../hooks/use-trigger-survey-picker";
import { useWorkflowNodeFieldFocus } from "../../hooks/use-workflow-node-field-focus";
import { reconcileEndingCardIds } from "../../lib/trigger-ending-cards";
import { hasBoundTriggerSurveyAtom, prunedTriggerEndingCardIdsAtom, workflowAtom } from "../../state/editor";
import { WorkflowFieldError, WorkflowFieldLabel } from "./workflow-field";

interface WorkflowTriggerFormProps {
  node: TWorkflowResponseCompletedTriggerNode;
  isEditable: boolean;
  onChange: (next: TWorkflowResponseCompletedTriggerNode) => void;
}

// "all" is an empty endingCardIds (any ending, future ones included); "specific" is the checkbox list.
type TEndingScope = "all" | "specific";

/** forma2's WorkflowTriggerForm: the survey whose completed responses fire the workflow, and which of its endings. */
export const WorkflowTriggerForm = ({ node, isEditable, onChange }: Readonly<WorkflowTriggerFormProps>) => {
  const { t } = useTranslation();
  const workspaceId = useAtomValue(workflowAtom)?.workspaceId ?? "";
  const surveyOptionsQuery = useWorkflowSurveyOptions(workspaceId);
  const triggerSurveyId = node.config.surveyId || null;
  const endingsQuery = useWorkflowSurveyEndings(triggerSurveyId);
  const hasBoundSurvey = useAtomValue(hasBoundTriggerSurveyAtom);

  // An unbound survey is restated here, so a jump from the problems dialog lands on a control that says what is wrong. Stale ending ids get no error of their own: the reconcile prunes them before this form sees them, and the notice below reports the widening.
  const isSurveyInvalid = !hasBoundSurvey;

  useWorkflowNodeFieldFocus({
    nodeId: node.id,
    resolveElement: (field) =>
      document.getElementById(
        field === "endingCardIds" ? "workflow-trigger-ending-scope" : "workflow-trigger-survey"
      ),
  });

  // An authority only once the query has settled for the current survey, so a pending fetch never reads as "no endings".
  const surveyEndingIds =
    endingsQuery.isSuccess && endingsQuery.resolvedSurveyId === triggerSurveyId
      ? endingsQuery.endings.map((ending) => ending.id)
      : null;
  // Opens in "specific" when ids are set, or when the page pruned this trigger's picks: the user chose endings that are gone, so ask for a fresh pick rather than show the widened state.
  const prunedEndingCardIds = useAtomValue(prunedTriggerEndingCardIdsAtom);
  const setPrunedEndingCardIds = useSetAtom(prunedTriggerEndingCardIdsAtom);
  const [endingScope, setEndingScope] = useState<TEndingScope>(
    node.config.endingCardIds.length > 0 || prunedEndingCardIds.length > 0 ? "specific" : "all"
  );

  const handleSurveyChange = (surveyId: string) => {
    // Ending ids belong to the previous survey, and so does the prune prompt.
    setEndingScope("all");
    setPrunedEndingCardIds([]);
    onChange({ ...node, config: { ...node.config, surveyId, endingCardIds: [] } });
  };

  const handleScopeChange = (scope: TEndingScope) => {
    setEndingScope(scope);
    // Any deliberate pick answers the prune prompt, in both directions; left set, the form would snap back to "specific" on its next mount.
    setPrunedEndingCardIds([]);
    if (scope === "all" && node.config.endingCardIds.length > 0) {
      onChange({ ...node, config: { ...node.config, endingCardIds: [] } });
    }
  };

  const toggleEnding = (endingId: string, checked: boolean) => {
    // Reconciled before the click is applied, so ids of deleted endings cannot ride along.
    const current = surveyEndingIds
      ? reconcileEndingCardIds(node.config.endingCardIds, surveyEndingIds).endingCardIds
      : node.config.endingCardIds;
    const next = checked
      ? Array.from(new Set([...current, endingId]))
      : current.filter((id) => id !== endingId);
    onChange({ ...node, config: { ...node.config, endingCardIds: next } });
  };

  // An empty list fires on every ending, a widening whenever the intent is, or was before a prune, a specific set.
  const showFiresOnEveryEndingNotice =
    node.config.endingCardIds.length === 0 && (endingScope === "specific" || prunedEndingCardIds.length > 0);

  const renderEndingSelection = () => {
    if (endingsQuery.endings.length === 0) {
      return <p className="text-xs text-slate-500">{t("workspace.workflows.trigger_ending_cards_none")}</p>;
    }
    if (endingScope !== "specific") return null;
    return (
      <div className="flex max-h-48 flex-col gap-2 overflow-y-auto rounded-md border border-slate-200 bg-white px-3 py-2">
        {endingsQuery.endings.map((ending) => (
          <label
            key={ending.id}
            className="flex items-center gap-2 text-sm text-slate-700"
            htmlFor={`workflow-trigger-ending-${ending.id}`}>
            <Checkbox
              id={`workflow-trigger-ending-${ending.id}`}
              checked={node.config.endingCardIds.includes(ending.id)}
              disabled={!isEditable}
              onCheckedChange={(value) => toggleEnding(ending.id, value === true)}
            />
            <span className="truncate">{ending.label}</span>
          </label>
        ))}
      </div>
    );
  };

  const renderEndingChoices = () => {
    if (!node.config.surveyId) {
      return (
        <p className="text-xs text-slate-500">{t("workspace.workflows.trigger_ending_cards_pick_survey")}</p>
      );
    }
    if (endingsQuery.isLoading) {
      return <p className="text-xs text-slate-500">{t("common.loading")}</p>;
    }
    // A survey without endings keeps the scope select and the notice reachable, so a prune that emptied the selection is never silent.
    return (
      <>
        <Select
          id="workflow-trigger-ending-scope"
          value={endingScope}
          disabled={!isEditable}
          onChange={(event) => handleScopeChange(event.target.value as TEndingScope)}>
          <option value="all">{t("workspace.workflows.trigger_ending_cards_scope_all")}</option>
          <option value="specific">{t("workspace.workflows.trigger_ending_cards_scope_specific")}</option>
        </Select>
        {renderEndingSelection()}
        {showFiresOnEveryEndingNotice ? (
          <p className="text-xs text-slate-500">
            {t("workspace.workflows.trigger_ending_cards_select_at_least_one")}
          </p>
        ) : null}
      </>
    );
  };

  const selectedSurveyId = surveyOptionsQuery.options.some((survey) => survey.id === node.config.surveyId)
    ? node.config.surveyId
    : "";

  return (
    <div className="flex flex-col gap-4 px-1">
      <div className="flex flex-col gap-2">
        <WorkflowFieldLabel htmlFor="workflow-trigger-survey" isRequired isInvalid={isSurveyInvalid}>
          {t("workspace.workflows.trigger_survey_label")}
        </WorkflowFieldLabel>
        <Select
          id="workflow-trigger-survey"
          value={selectedSurveyId}
          disabled={!isEditable || surveyOptionsQuery.isLoading}
          aria-invalid={isSurveyInvalid}
          aria-describedby={isSurveyInvalid ? "workflow-trigger-survey-error" : undefined}
          className={cn(isSurveyInvalid && "border-red-500")}
          onChange={(event) => handleSurveyChange(event.target.value)}>
          <option value="" disabled>
            {surveyOptionsQuery.isLoading
              ? t("common.loading")
              : surveyOptionsQuery.options.length === 0
                ? t("workspace.workflows.trigger_survey_empty")
                : t("workspace.workflows.trigger_survey_placeholder")}
          </option>
          {surveyOptionsQuery.options.map((survey) => (
            <option key={survey.id} value={survey.id}>
              {survey.name}
            </option>
          ))}
        </Select>
        {isSurveyInvalid ? (
          <WorkflowFieldError id="workflow-trigger-survey-error">
            {t("workspace.workflows.validation_problem_trigger_survey_unbound")}
          </WorkflowFieldError>
        ) : null}
        <p className="text-xs text-slate-500">{t("workspace.workflows.trigger_survey_description")}</p>
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor="workflow-trigger-ending-scope">
          {t("workspace.workflows.trigger_ending_cards_label")}
        </Label>
        {renderEndingChoices()}
      </div>
    </div>
  );
};
