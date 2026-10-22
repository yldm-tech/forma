import { WorkflowIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

/** The list without rows: filtered says a search or status filter emptied it, as opposed to a workspace without workflows. */
export const WorkflowsEmptyState = ({ filtered }: Readonly<{ filtered: boolean }>) => {
  const { t } = useTranslation();
  return (
    <div className="mx-auto flex max-w-xl flex-col items-center gap-4 py-16 text-center">
      <WorkflowIcon className="size-8 text-slate-400" aria-hidden="true" />
      <div className="space-y-2">
        <h3 className="text-base font-semibold text-slate-900">
          {filtered ? t("workspace.workflows.no_results_title") : t("workspace.workflows.no_workflows_title")}
        </h3>
        <p className="text-sm text-balance text-slate-600">
          {filtered
            ? t("workspace.workflows.no_results_description")
            : t("workspace.workflows.no_workflows_description")}
        </p>
      </div>
    </div>
  );
};
