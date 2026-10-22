import { useQuery } from "@tanstack/react-query";
import { useAtomValue, useSetAtom } from "jotai";
import { type KeyboardEvent, useEffect, useId, useRef } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "../../../lib/cn";
import { workflowKeys, workflowsApi } from "../api";
import { useWorkflowBuilder } from "../hooks/use-workflow-builder";
import { setWorkflowNameAtom, workflowAtom, workflowNameAtom } from "../state/editor";
import { WorkflowStatusPill } from "./workflow-status-pill";

// The PATCH contract's name bound, so the title cannot author a name the API would refuse.
const WORKFLOW_NAME_MAX_LENGTH = 120;

interface WorkflowPageTitleProps {
  workflowId: string;
  isReadOnly: boolean;
  isEditTab: boolean;
  /** The workflow was just created: the title is focused and selected for naming, once. */
  isNew: boolean;
  onNewConsumed: () => void;
}

/** forma2's WorkflowPageTitle: on the editor tab the title is the name editor, bound to the draft, autosaved, committed at once with Enter; on the runs tab, where no builder loads the workflow, the name is fetched. */
export const WorkflowPageTitle = ({
  workflowId,
  isReadOnly,
  isEditTab,
  isNew,
  onNewConsumed,
}: Readonly<WorkflowPageTitleProps>) => {
  const { t } = useTranslation();
  const workflow = useAtomValue(workflowAtom);
  const workflowName = useAtomValue(workflowNameAtom);
  const setWorkflowName = useSetAtom(setWorkflowNameAtom);
  const inputRef = useRef<HTMLInputElement>(null);
  const hasAutoFocusedRef = useRef(false);
  const nameErrorId = useId();
  const { save } = useWorkflowBuilder({ workflowId, isReadOnly, loadOnMount: false });

  // Only off the editor tab, where nothing hydrates the editor; on it the builder's own load is the single source.
  const { data } = useQuery({
    queryKey: workflowKeys.detail(workflowId),
    queryFn: ({ signal }) => workflowsApi.get(workflowId, signal),
    enabled: !isEditTab && workflow?.id !== workflowId,
  });

  // The name is editable in every status but archived, the builder's canEditMetadata.
  const isEditable = isEditTab && Boolean(workflow) && !isReadOnly && workflow?.status !== "archived";

  useEffect(() => {
    if (!isNew || !isEditable || hasAutoFocusedRef.current) return;
    hasAutoFocusedRef.current = true;
    inputRef.current?.focus();
    inputRef.current?.select();
    // One-shot, so a reload or a shared link does not select the title again.
    onNewConsumed();
  }, [isNew, isEditable, onNewConsumed]);

  // Enter commits the rename: the field blurs and the draft is saved at once rather than after the debounce. An input method uses Enter to accept a candidate, which must not commit.
  const handleKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key !== "Enter" || event.nativeEvent.isComposing) return;
    event.preventDefault();
    // An empty name keeps focus for a fix in place; save() still runs, since it explains the refusal.
    if (workflowName.trim()) inputRef.current?.blur();
    void save();
  };

  const resolved = workflow?.id === workflowId ? workflow : data;
  if (!resolved) {
    return (
      <span className="flex h-9 items-center gap-x-3" aria-hidden="true">
        <span className="h-7 w-64 animate-pulse rounded-md bg-slate-200" />
        <span className="h-7 w-24 animate-pulse rounded-full bg-slate-200" />
      </span>
    );
  }

  // An empty name blocks every save; the field itself says so.
  const showNameError = isEditable && workflowName.trim().length === 0;

  return (
    <span className="flex min-w-0 flex-col gap-y-1">
      <span className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
        {isEditable ? (
          <input
            ref={inputRef}
            value={workflowName}
            onChange={(event) => setWorkflowName(event.target.value)}
            onKeyDown={handleKeyDown}
            aria-label={t("common.workflow_name")}
            placeholder={t("common.workflow_name")}
            maxLength={WORKFLOW_NAME_MAX_LENGTH}
            aria-invalid={showNameError || undefined}
            aria-describedby={showNameError ? nameErrorId : undefined}
            size={Math.max(workflowName.length, 12)}
            className={cn(
              "-mx-2 -my-1 max-w-[28rem] min-w-0 rounded-md border border-dashed border-transparent bg-transparent px-2 py-1 [field-sizing:content]",
              "text-3xl font-bold text-slate-800 transition-colors placeholder:text-slate-400",
              "hover:border-slate-300 focus:border-brand-dark focus:ring-0 focus:outline-none focus:hover:border-brand-dark",
              showNameError &&
                "border-red-300 hover:border-red-400 focus:border-red-400 focus:hover:border-red-400"
            )}
          />
        ) : (
          <span className="min-w-0">{resolved.name}</span>
        )}
        <WorkflowStatusPill status={resolved.status} size="large" />
      </span>
      {showNameError ? (
        <span id={nameErrorId} role="alert" className="text-xs font-medium text-red-600">
          {t("workspace.workflows.validation_problem_name_missing")}
        </span>
      ) : null}
    </span>
  );
};
