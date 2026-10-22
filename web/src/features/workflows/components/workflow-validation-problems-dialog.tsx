import { useAtomValue, useSetAtom } from "jotai";
import { ArrowRightIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../../components/ui/dialog";
import {
  getWorkflowValidationProblemFocusTarget,
  getWorkflowValidationProblemLocation,
} from "../lib/display";
import {
  type TWorkflowValidationProblem,
  type TWorkflowValidationProblemCode,
  requestWorkflowNodeFieldFocusAtom,
  workflowDefinitionAtom,
} from "../state/editor";

/** forma2's WorkflowValidationProblemsDialog: the live problems behind the canvas's count, each localized by its code. A problem that resolves to one config field is a button that closes the dialog, opens that step and focuses the field; problems fixed elsewhere (the name, a missing trigger, the flow's shape) are passive rows. */
export const WorkflowValidationProblemsDialog = ({
  open,
  onOpenChange,
  problems,
}: Readonly<{
  open: boolean;
  onOpenChange: (open: boolean) => void;
  problems: TWorkflowValidationProblem[];
}>) => {
  const { t } = useTranslation();
  const definition = useAtomValue(workflowDefinitionAtom);
  const requestFieldFocus = useSetAtom(requestWorkflowNodeFieldFocusAtom);

  const problemMessages: Record<TWorkflowValidationProblemCode, string> = {
    name_missing: t("workspace.workflows.validation_problem_name_missing"),
    trigger_missing: t("workspace.workflows.validation_problem_trigger_missing"),
    trigger_survey_unbound: t("workspace.workflows.validation_problem_trigger_survey_unbound"),
    trigger_ending_not_found: t("workspace.workflows.validation_problem_trigger_ending_not_found"),
    trigger_not_connected: t("workspace.workflows.validation_problem_trigger_not_connected"),
    flow_invalid: t("workspace.workflows.validation_problem_flow_invalid"),
    step_not_executable: t("workspace.workflows.validation_problem_step_not_executable"),
    step_incomplete: t("workspace.workflows.validation_problem_step_incomplete"),
    definition_invalid: t("workspace.workflows.validation_problem_generic"),
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange} width="narrow">
      <DialogHeader>
        <DialogTitle>{t("workspace.workflows.validation_problems_title")}</DialogTitle>
        <DialogDescription>{t("workspace.workflows.validation_problems_description")}</DialogDescription>
      </DialogHeader>
      <DialogBody>
        <ul className="space-y-3">
          {problems.map((problem) => {
            const location = getWorkflowValidationProblemLocation(problem, definition, t);
            const focusTarget = getWorkflowValidationProblemFocusTarget(problem, definition);
            const message = problemMessages[problem.code];
            return (
              <li key={`${problem.code}-${problem.field}`}>
                {focusTarget ? (
                  <button
                    type="button"
                    aria-label={t("workspace.workflows.validation_problem_fix_label", { problem: message })}
                    className="flex w-full items-start gap-2 rounded-md border border-slate-200 bg-slate-50 p-3 text-left transition-colors hover:border-slate-300 hover:bg-slate-100 focus-visible:ring-1 focus-visible:ring-slate-400 focus-visible:outline-hidden"
                    onClick={() => {
                      onOpenChange(false);
                      requestFieldFocus(focusTarget);
                    }}>
                    <span className="flex min-w-0 flex-1 flex-col">
                      <span className="text-sm text-slate-800">{message}</span>
                      {location ? <span className="mt-1 text-xs text-slate-500">{location}</span> : null}
                    </span>
                    <ArrowRightIcon className="mt-0.5 size-4 shrink-0 text-slate-400" aria-hidden="true" />
                  </button>
                ) : (
                  <div className="rounded-md border border-slate-200 bg-slate-50 p-3">
                    <p className="text-sm text-slate-800">{message}</p>
                    {location ? <p className="mt-1 text-xs text-slate-500">{location}</p> : null}
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      </DialogBody>
      <DialogFooter>
        <Button type="button" variant="secondary" onClick={() => onOpenChange(false)}>
          {t("common.close")}
        </Button>
      </DialogFooter>
    </Dialog>
  );
};
