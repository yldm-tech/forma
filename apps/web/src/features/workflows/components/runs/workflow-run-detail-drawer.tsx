import { Link } from "@tanstack/react-router";
import { SquareArrowOutUpRight } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "../../../../components/ui/badge";
import { IdBadge } from "../../../../components/ui/id-badge";
import { Sheet, SheetSection } from "../../../../components/ui/sheet";
import { timeSince } from "../../../../lib/time";
import { type TWorkflowRunListItem, type TWorkflowRunSummary, getWorkflowApiErrorMessage } from "../../api";
import { useWorkflowRun } from "../../hooks/queries";
import { getWorkflowRunStatusBadge, getWorkflowTriggerTypeLabel } from "../../lib/display";
import { isContinuedFailure } from "../../lib/run-display";
import { RunJsonCode } from "./run-json-code";
import { WorkflowRunSteps } from "./workflow-run-steps";

const RunSummarySection = ({
  run,
  locale,
  hasContinuedFailures,
}: Readonly<{ run: TWorkflowRunSummary; locale: string; hasContinuedFailures: boolean }>) => {
  const { t } = useTranslation();
  // A run that went on past failed steps set to continue completed, but not cleanly; the badge says so before the steps are read.
  const statusBadge =
    run.status === "completed" && hasContinuedFailures
      ? { label: t("workspace.workflows.run_completed_with_failures"), type: "warning" as const }
      : getWorkflowRunStatusBadge(run.status, t);
  return (
    <SheetSection className="bg-slate-50">
      <h3 className="mb-4 text-lg font-semibold text-slate-900">{t("common.summary")}</h3>
      <dl className="space-y-4 text-sm">
        <div>
          <dt className="text-slate-500">{t("common.id")}</dt>
          <dd className="mt-1">
            <IdBadge id={run.id} />
          </dd>
        </div>
        <div>
          <dt className="text-slate-500">{t("common.status")}</dt>
          <dd className="mt-1">
            <Badge text={statusBadge.label} type={statusBadge.type} size="normal" />
          </dd>
        </div>
        <div>
          <dt className="text-slate-500">{t("common.started_at")}</dt>
          <dd className="text-slate-900">
            {run.startedAt ? timeSince(run.startedAt, locale) : t("common.not_set")}
          </dd>
        </div>
        <div>
          <dt className="text-slate-500">{t("common.finished_at")}</dt>
          <dd className="text-slate-900">
            {run.finishedAt ? timeSince(run.finishedAt, locale) : t("common.not_set")}
          </dd>
        </div>
        <div>
          <dt className="text-slate-500">{t("common.attempt")}</dt>
          <dd className="text-slate-900">{run.attempt}</dd>
        </div>
        {run.error ? (
          <div>
            <dt className="text-slate-500">{t("common.error")}</dt>
            <dd className="mt-1 rounded-md bg-red-50 px-3 py-2 text-red-700">{run.error}</dd>
          </div>
        ) : null}
      </dl>
    </SheetSection>
  );
};

/** forma2's WorkflowRunDetailDrawer: the list row paints the header and summary at once; the run's steps, trigger payload and data are fetched while the drawer is open. */
export const WorkflowRunDetailDrawer = ({
  run,
  open,
  onOpenChange,
}: Readonly<{ run: TWorkflowRunListItem | null; open: boolean; onOpenChange: (open: boolean) => void }>) => {
  const { t, i18n } = useTranslation();
  const {
    data: detail,
    isLoading,
    isError,
    error,
  } = useWorkflowRun({ runId: run?.id ?? null, enabled: open });

  let steps: React.ReactNode = null;
  if (isLoading) {
    steps = (
      <div className="space-y-3" aria-hidden="true">
        <div className="h-16 w-full animate-pulse rounded-lg bg-slate-200" />
        <div className="h-16 w-full animate-pulse rounded-lg bg-slate-200" />
      </div>
    );
  } else if (isError) {
    steps = (
      <p className="text-sm text-slate-600">
        {getWorkflowApiErrorMessage(error, t("common.something_went_wrong_please_try_again"))}
      </p>
    );
  } else if (detail) {
    steps = <WorkflowRunSteps logs={detail.logs} />;
  }

  return (
    <Sheet
      open={open}
      onOpenChange={onOpenChange}
      title={run?.workflowName ?? t("common.workflow_runs")}
      description={run ? getWorkflowTriggerTypeLabel(run.triggerType, t) : undefined}>
      {run ? (
        <>
          <Link
            to="/workspaces/$workspaceId/workflows/$workflowId"
            params={{ workspaceId: run.workspaceId, workflowId: run.workflowId }}
            className="flex w-fit items-center gap-2 text-sm font-medium text-slate-900 underline-offset-4 hover:underline">
            <SquareArrowOutUpRight className="size-4" aria-hidden="true" />
            {t("common.view_workflow")}
          </Link>
          <div className="space-y-6">
            {/* The fetched detail once it is there, so the summary cannot show stale list values beside fresh steps. */}
            <RunSummarySection
              run={detail ?? run}
              locale={i18n.language}
              hasContinuedFailures={detail?.logs.some(isContinuedFailure) ?? false}
            />
            <SheetSection>
              <h3 className="mb-4 text-lg font-semibold text-slate-900">{t("common.steps")}</h3>
              {steps}
            </SheetSection>
            {detail ? (
              <>
                <SheetSection>
                  <h3 className="mb-4 text-lg font-semibold text-slate-900">{t("common.trigger_payload")}</h3>
                  <RunJsonCode value={detail.triggerPayload} />
                </SheetSection>
                <SheetSection>
                  <h3 className="mb-4 text-lg font-semibold text-slate-900">{t("common.run_data")}</h3>
                  <RunJsonCode value={detail.data} />
                </SheetSection>
              </>
            ) : null}
          </div>
        </>
      ) : null}
    </Sheet>
  );
};
