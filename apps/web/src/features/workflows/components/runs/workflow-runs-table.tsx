import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "../../../../components/ui/badge";
import { Button } from "../../../../components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "../../../../components/ui/table";
import { timeSince } from "../../../../lib/time";
import { type TWorkflowRunListItem, getWorkflowApiErrorMessage } from "../../api";
import { WORKFLOW_CLIENT_EVENTS, trackWorkflowEvent } from "../../lib/analytics";
import { getWorkflowRunStatusBadge, getWorkflowTriggerTypeLabel } from "../../lib/display";
import { RunsTableSkeleton } from "../workflows-loading";
import { WorkflowRunDetailDrawer } from "./workflow-run-detail-drawer";

interface WorkflowRunsTableProps {
  runs: TWorkflowRunListItem[];
  /** The workspace's runs name their workflow; one workflow's runs name their trigger. */
  showWorkflowColumn?: boolean;
  isLoading: boolean;
  isError: boolean;
  error: unknown;
  onRetry: () => void;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  isFetchNextPageError: boolean;
  onLoadMore: () => void;
}

/** forma2's WorkflowRunsTable: runs newest first, a page at a time; a row opens its detail drawer. */
export const WorkflowRunsTable = ({
  runs,
  showWorkflowColumn = false,
  isLoading,
  isError,
  error,
  onRetry,
  hasNextPage,
  isFetchingNextPage,
  isFetchNextPageError,
  onLoadMore,
}: Readonly<WorkflowRunsTableProps>) => {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null);
  const selectedRun = runs.find((run) => run.id === selectedRunId) ?? null;

  const openRun = (run: TWorkflowRunListItem) => {
    trackWorkflowEvent(WORKFLOW_CLIENT_EVENTS.runDetailOpened, {
      run_id: run.id,
      workflow_id: run.workflowId,
      run_status: run.status,
      scope: showWorkflowColumn ? "workspace" : "workflow",
    });
    setSelectedRunId(run.id);
  };

  if (isLoading && runs.length === 0) return <RunsTableSkeleton />;

  if (isError && runs.length === 0) {
    return (
      <div className="flex w-full flex-col items-center justify-center gap-4 py-16 text-slate-600">
        <p>{getWorkflowApiErrorMessage(error, t("common.something_went_wrong_please_try_again"))}</p>
        <Button variant="secondary" size="sm" onClick={onRetry}>
          {t("common.try_again")}
        </Button>
      </div>
    );
  }

  return (
    <>
      <div className="w-full overflow-x-auto rounded-xl border border-slate-200">
        <Table className="w-full table-fixed">
          <TableHeader>
            <TableRow>
              <TableHead className="h-10 w-[46%] bg-white font-semibold">
                {showWorkflowColumn ? t("common.workflow_name") : t("common.trigger")}
              </TableHead>
              <TableHead className="h-10 w-[18%] bg-white font-semibold">{t("common.status")}</TableHead>
              <TableHead className="h-10 w-[18%] bg-white font-semibold">{t("common.started_at")}</TableHead>
              <TableHead className="h-10 w-[18%] bg-white font-semibold">{t("common.finished_at")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {runs.map((run) => {
              const statusBadge = getWorkflowRunStatusBadge(run, t);
              const triggerLabel = getWorkflowTriggerTypeLabel(run.triggerType, t);
              return (
                <TableRow
                  key={run.id}
                  role="button"
                  tabIndex={0}
                  onClick={() => openRun(run)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter" || event.key === " ") {
                      event.preventDefault();
                      openRun(run);
                    }
                  }}
                  className="focus-visible:ring-ring cursor-pointer hover:bg-slate-100 focus-visible:ring-2 focus-visible:outline-hidden">
                  <TableCell className="min-w-0 px-4 py-2">
                    {showWorkflowColumn ? (
                      <>
                        <p className="truncate text-sm font-medium text-slate-900">{run.workflowName}</p>
                        <p className="truncate text-sm text-slate-500">{triggerLabel}</p>
                      </>
                    ) : (
                      <p className="truncate text-sm text-slate-700">{triggerLabel}</p>
                    )}
                  </TableCell>
                  <TableCell className="px-4 py-2">
                    <Badge text={statusBadge.label} type={statusBadge.type} size="tiny" />
                  </TableCell>
                  <TableCell className="truncate px-4 py-2 text-sm text-slate-600">
                    {run.startedAt ? timeSince(run.startedAt, locale) : t("common.not_set")}
                  </TableCell>
                  <TableCell className="truncate px-4 py-2 text-sm text-slate-600">
                    {run.finishedAt ? timeSince(run.finishedAt, locale) : t("common.not_set")}
                  </TableCell>
                </TableRow>
              );
            })}
            {runs.length === 0 && (
              <TableRow>
                <TableCell colSpan={4} className="h-24 text-center">
                  <p className="text-slate-400">{t("common.no_results")}</p>
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      {hasNextPage ? (
        <div className="flex flex-col items-center gap-2 py-5">
          {/* A failed load-more keeps the rows already loaded; say so in place and let the same button retry. */}
          {isFetchNextPageError ? (
            <p className="text-sm text-red-600">
              {getWorkflowApiErrorMessage(error, t("common.something_went_wrong_please_try_again"))}
            </p>
          ) : null}
          <Button variant="secondary" size="sm" loading={isFetchingNextPage} onClick={onLoadMore}>
            {t("common.load_more")}
          </Button>
        </div>
      ) : null}

      <WorkflowRunDetailDrawer
        run={selectedRun}
        open={selectedRun !== null}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) setSelectedRunId(null);
        }}
      />
    </>
  );
};
