import { Link } from "@tanstack/react-router";
import { ArchiveIcon, Code, HelpCircle, Link2Icon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { cn } from "../../../lib/cn";
import type { SurveyListItem } from "../api";
import { formatDateForDisplay, timeSince } from "../time";
import { surveyRowRules } from "./row-rules";
import { type SurveyActions, SurveyDropdownMenu } from "./survey-dropdown-menu";
import { SurveyStatusIndicator } from "./survey-status-indicator";

const SurveyTypeIndicator = ({ type }: { type: string }) => {
  const { t } = useTranslation();
  const { Icon, label } =
    type === "app"
      ? { Icon: Code, label: t("common.app") }
      : type === "link"
        ? { Icon: Link2Icon, label: t("common.link") }
        : { Icon: HelpCircle, label: "Unknown" };
  return (
    <div className="flex items-center gap-x-2 text-sm text-slate-600">
      <Icon className="size-4" aria-hidden="true" />
      <span>{label}</span>
    </div>
  );
};

/** forma2's SurveyCard: one row of the list, opening the survey's editor, with its actions menu. */
export const SurveyCard = ({
  survey,
  workspaceId,
  organizationId,
  isReadOnly,
  publicDomain,
  actions,
}: {
  survey: SurveyListItem;
  workspaceId: string;
  organizationId: string;
  isReadOnly: boolean;
  publicDomain: string | undefined;
  actions: SurveyActions;
}) => {
  const { t, i18n } = useTranslation();
  const rules = surveyRowRules(survey, workspaceId, isReadOnly);
  const statusLabel = {
    inProgress: t("common.in_progress"),
    completed: t("common.completed"),
    draft: t("common.draft"),
    paused: rules.isScheduled ? t("common.scheduled") : t("common.paused"),
  }[survey.status];

  const body = (
    <div
      className={cn(
        "grid w-full grid-cols-8 place-items-center gap-3 rounded-xl border border-slate-200 bg-white p-4 pr-8 shadow-xs transition-colors ease-in-out",
        rules.isClickable && "hover:border-slate-400"
      )}>
      <div className="col-span-2 flex max-w-full items-center justify-self-start text-sm font-medium text-slate-900">
        <div className="w-full truncate">{survey.name}</div>
      </div>
      <div
        className={cn(
          "col-span-1 flex w-fit items-center gap-2 rounded-full py-1 pr-2 pl-1 text-sm whitespace-nowrap text-slate-800",
          rules.isArchived || survey.status === "draft" || survey.status === "paused"
            ? "bg-slate-100"
            : survey.status === "inProgress"
              ? "bg-emerald-50"
              : "bg-slate-200"
        )}>
        {rules.isArchived ? (
          <>
            <span className="rounded-full bg-slate-300 p-1">
              <ArchiveIcon className="size-3 text-slate-600" />
            </span>
            {t("common.archived")}
          </>
        ) : (
          <>
            <SurveyStatusIndicator status={survey.status} isScheduled={rules.isScheduled} />
            {statusLabel}
          </>
        )}
      </div>
      <div className="col-span-1 max-w-full truncate text-sm text-slate-600">
        {survey.completedResponseCount}
      </div>
      <div className="col-span-1 flex justify-between">
        <SurveyTypeIndicator type={survey.type} />
      </div>
      <div className="col-span-1 max-w-full truncate text-sm text-slate-600">
        {formatDateForDisplay(survey.createdAt, i18n.language)}
      </div>
      <div className="col-span-1 max-w-full truncate text-sm text-slate-600">
        {timeSince(survey.updatedAt, i18n.language)}
      </div>
      <div className="col-span-1 max-w-full truncate text-sm text-slate-600">
        {survey.creator?.name ?? "-"}
      </div>
    </div>
  );

  return (
    <div className="relative block">
      {rules.isClickable ? (
        <Link to={rules.href} className="block">
          {body}
        </Link>
      ) : (
        body
      )}
      <div className="absolute top-3.5 right-3">
        <SurveyDropdownMenu
          survey={survey}
          rules={rules}
          workspaceId={workspaceId}
          organizationId={organizationId}
          publicDomain={publicDomain}
          actions={actions}
        />
      </div>
    </div>
  );
};
