import { useAtomValue } from "jotai";
import { CheckIcon, TriangleAlertIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "../../../../lib/cn";
import { useWorkflowSurveyEndings } from "../../hooks/use-trigger-survey-picker";
import { WORKFLOW_CLIENT_EVENTS, summarizeValidationProblems, trackWorkflowEvent } from "../../lib/analytics";
import {
  deriveTriggerEndingProblems,
  workflowAtom,
  workflowDefinitionAtom,
  workflowValidationProblemsAtom,
} from "../../state/editor";
import { WorkflowValidationProblemsDialog } from "../workflow-validation-problems-dialog";

const PILL_CLASS_NAME =
  "inline-flex items-center gap-1 rounded-full border px-2.5 py-0.5 text-xs font-medium";

/** forma2's WorkflowValidationStatus: the loaded workflow's live validation state in the canvas's corner, a quiet badge while valid and a button that lists the problems otherwise. */
export const WorkflowValidationStatus = () => {
  const { t } = useTranslation();
  const workflow = useAtomValue(workflowAtom);
  const definition = useAtomValue(workflowDefinitionAtom);
  const atomProblems = useAtomValue(workflowValidationProblemsAtom);
  const [isProblemsDialogOpen, setIsProblemsDialogOpen] = useState(false);

  // The ending cards check needs the bound survey's current endings, server data the query cache holds, so it is merged here rather than in the editor's atoms. Only a resolved list may flag a problem: loading, disabled or failed is unknown, never an error.
  const trigger = definition?.trigger ?? null;
  const endingsQuery = useWorkflowSurveyEndings(trigger?.config.surveyId ?? null);
  const endingProblems =
    trigger && endingsQuery.isSuccess
      ? deriveTriggerEndingProblems(
          trigger.config.endingCardIds,
          endingsQuery.endings.map((ending) => ending.id)
        )
      : [];
  const problems = [...atomProblems, ...endingProblems];

  if (!workflow) return null;

  return (
    <div className="absolute right-4 bottom-4 z-10" aria-live="polite">
      {problems.length === 0 ? (
        <span className={cn(PILL_CLASS_NAME, "cursor-default border-slate-200 bg-white text-slate-600")}>
          <CheckIcon className="size-3.5 text-green-600" aria-hidden="true" />
          {t("workspace.workflows.validation_status_valid")}
        </span>
      ) : (
        // Unfinished setup while drafting, not failure: amber and "problems", never red "errors".
        <button
          type="button"
          aria-haspopup="dialog"
          className={cn(
            PILL_CLASS_NAME,
            "border-amber-200 bg-amber-100 text-amber-800 transition-colors hover:bg-amber-200 focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-hidden"
          )}
          onClick={() => {
            trackWorkflowEvent(
              WORKFLOW_CLIENT_EVENTS.validationProblemsViewed,
              summarizeValidationProblems(problems)
            );
            setIsProblemsDialogOpen(true);
          }}>
          <TriangleAlertIcon className="size-3.5" aria-hidden="true" />
          {t("workspace.workflows.validation_problems_count", { count: problems.length })}
        </button>
      )}
      <WorkflowValidationProblemsDialog
        open={isProblemsDialogOpen}
        onOpenChange={setIsProblemsDialogOpen}
        problems={problems}
      />
    </div>
  );
};
