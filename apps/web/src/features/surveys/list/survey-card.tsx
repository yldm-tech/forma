import { Link } from "@tanstack/react-router";
import { ArchiveIcon, Code, Link2Icon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { cn } from "../../../lib/cn";
import type { SurveyListItem } from "../api";
import { formatDateForDisplay, timeSince } from "../time";
import { surveyRowRules } from "./row-rules";
import { type SurveyActions, SurveyDropdownMenu } from "./survey-dropdown-menu";
import { SurveyStatusIndicator } from "./survey-status-indicator";

/**
 * The columns of the survey table, shared by its header and its rows so they line up: name, status, completed responses, type, created at, updated at, created by and the actions menu. The name takes what the fixed columns leave. Below 87.5rem (1400px) the created at and created by columns are left out, and below lg the type and updated at columns as well, so the name keeps room to be read: with all eight columns a 1280px screen would leave the name about 12rem, with six it keeps about 27rem. The completed column fits its label on two lines (and the info icon) in every locale.
 */
export const SURVEY_TABLE_GRID =
  "grid grid-cols-[minmax(8rem,1fr)_8rem_6.5rem_2.5rem] items-center gap-x-3 lg:grid-cols-[minmax(10rem,1fr)_8rem_6.5rem_5rem_8rem_2.5rem] min-[87.5rem]:grid-cols-[minmax(14rem,1fr)_8rem_6.5rem_5rem_7.5rem_8rem_7rem_2.5rem]";

/** The horizontal padding of a row, which the header repeats so its labels sit over the cells. */
export const SURVEY_TABLE_ROW_PADDING = "border py-3 pr-3 pl-4";

/** Which columns a narrower screen leaves out (see SURVEY_TABLE_GRID). */
export const SURVEY_TABLE_COLUMN = {
  lgOnly: "hidden lg:flex",
  wideOnly: "hidden min-[87.5rem]:flex",
};

/** The full date and time of a timestamp, for the title of a date cell whose text is shortened. */
const formatFullDate = (iso: string, locale: string): string =>
  new Intl.DateTimeFormat(locale, { dateStyle: "long", timeStyle: "short" }).format(new Date(iso));

const SurveyTypeIndicator = ({ type }: { type: string }) => {
  const { t } = useTranslation();
  // The list only returns link and app surveys (forma2's website type is gone); anything else shows no type rather than a made-up label.
  const indicator =
    type === "app"
      ? { Icon: Code, label: t("common.app") }
      : type === "link"
        ? { Icon: Link2Icon, label: t("common.link") }
        : null;
  if (!indicator) return null;
  const { Icon, label } = indicator;
  return (
    <span className="flex items-center gap-x-2 text-sm text-slate-600">
      <Icon className="size-4" aria-hidden="true" />
      <span>{label}</span>
    </span>
  );
};

/**
 * forma2's SurveyCard: one row of the survey table. The survey's name is the link that opens it; a stretched overlay makes the whole row clickable without turning every cell into the link's name, and the actions menu sits above that overlay.
 */
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
  const cell = "flex min-w-0 justify-self-center text-sm text-slate-600";

  return (
    <div
      role="row"
      className={cn(
        SURVEY_TABLE_GRID,
        SURVEY_TABLE_ROW_PADDING,
        "relative rounded-xl border-slate-200 bg-white shadow-xs transition-colors ease-in-out",
        rules.isClickable && "focus-within:border-slate-400 hover:border-slate-400"
      )}>
      <div role="cell" className="min-w-0 text-sm font-medium text-slate-900">
        {rules.isClickable ? (
          <Link
            to={rules.href}
            title={survey.name}
            data-survey-name=""
            className="focus-visible:after:ring-ring block truncate after:absolute after:inset-0 after:rounded-xl focus-visible:outline-hidden focus-visible:after:ring-2 focus-visible:after:ring-offset-2">
            {survey.name}
          </Link>
        ) : (
          // Nothing to open; it can still take the focus Load more hands to the first new row.
          <span
            title={survey.name}
            data-survey-name=""
            tabIndex={-1}
            className="block truncate outline-hidden">
            {survey.name}
          </span>
        )}
      </div>
      <div role="cell" className="flex min-w-0 justify-self-center">
        <span
          className={cn(
            "flex w-fit items-center gap-2 rounded-full py-1 pr-2 pl-1 text-sm whitespace-nowrap text-slate-800",
            rules.isArchived || survey.status === "draft" || survey.status === "paused"
              ? "bg-slate-100"
              : survey.status === "inProgress"
                ? "bg-emerald-50"
                : "bg-slate-200"
          )}>
          {rules.isArchived ? (
            <>
              <span className="rounded-full bg-slate-300 p-1">
                <ArchiveIcon className="size-3 text-slate-600" aria-hidden="true" />
              </span>
              {t("common.archived")}
            </>
          ) : (
            <>
              <SurveyStatusIndicator status={survey.status} isScheduled={rules.isScheduled} />
              {statusLabel}
            </>
          )}
        </span>
      </div>
      <div role="cell" className={cn(cell, "tabular-nums")}>
        {survey.completedResponseCount}
      </div>
      <div role="cell" className={cn(cell, SURVEY_TABLE_COLUMN.lgOnly)}>
        <SurveyTypeIndicator type={survey.type} />
      </div>
      <div role="cell" className={cn(cell, SURVEY_TABLE_COLUMN.wideOnly)}>
        <time
          dateTime={survey.createdAt}
          title={formatFullDate(survey.createdAt, i18n.language)}
          className="truncate">
          {formatDateForDisplay(survey.createdAt, i18n.language)}
        </time>
      </div>
      <div role="cell" className={cn(cell, SURVEY_TABLE_COLUMN.lgOnly)}>
        <time
          dateTime={survey.updatedAt}
          title={formatFullDate(survey.updatedAt, i18n.language)}
          className="truncate">
          {timeSince(survey.updatedAt, i18n.language)}
        </time>
      </div>
      <div role="cell" className={cn(cell, SURVEY_TABLE_COLUMN.wideOnly)}>
        <span className="truncate" title={survey.creator?.name}>
          {survey.creator?.name ?? "-"}
        </span>
      </div>
      <div role="cell" className="relative z-10 flex justify-self-end">
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
