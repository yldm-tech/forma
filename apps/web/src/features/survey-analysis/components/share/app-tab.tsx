import type { TActionClass } from "@forma/types/action-classes";
import type { TSegment } from "@forma/types/segment";
import type { TSurvey } from "@forma/types/surveys/types";
import { Link } from "@tanstack/react-router";
import type { TFunction } from "i18next";
import {
  CodeXmlIcon,
  MousePointerClickIcon,
  PercentIcon,
  Repeat1Icon,
  TimerResetIcon,
  UsersIcon,
} from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertButton, AlertDescription, AlertTitle } from "../../../../components/ui/alert";
import { H4, Small } from "../../../../components/ui/typography";
import type { WorkspaceDetail } from "../../../../lib/queries/workspaces";

const noCodeConfigLabels = (t: TFunction): Record<string, string> => ({
  click: t("workspace.actions.click"),
  pageView: t("workspace.actions.page_view"),
  exitIntent: t("workspace.actions.exit_intent"),
  fiftyPercentScroll: t("workspace.actions.fifty_percent_scroll"),
  pageDwell: t("workspace.actions.time_on_page"),
});

const formatRecontactDays = (days: number, t: TFunction) => {
  if (days === 0) return t("workspace.surveys.summary.in_app.display_criteria.time_based_always");
  if (days === 1) return `${days} ${t("workspace.surveys.summary.in_app.display_criteria.time_based_day")}`;
  return `${days} ${t("workspace.surveys.summary.in_app.display_criteria.time_based_days")}`;
};

const DisplayCriteriaItem = ({
  icon,
  title,
  titleSuffix,
  description,
}: {
  icon: ReactNode;
  title: ReactNode;
  titleSuffix?: ReactNode;
  description: ReactNode;
}) => (
  <div className="grid grid-cols-[auto_1fr] gap-x-2">
    <div className="flex items-center justify-center">{icon}</div>
    <div className="flex items-center">
      <Small>
        {title} {titleSuffix && <span className="text-sm font-normal text-slate-500">{titleSuffix}</span>}
      </Small>
    </div>
    <div />
    <div className="flex items-start">
      <Small color="muted" className="mt-1">
        {description}
      </Small>
    </div>
  </div>
);

/** forma2's AppTab: how an app survey reaches its respondents, the SDK connection and the display criteria. */
export const AppTab = ({ survey, workspace }: { survey: TSurvey; workspace: WorkspaceDetail }) => {
  const { t } = useTranslation();
  const noCodeLabels = noCodeConfigLabels(t);

  const waitTime = () => {
    if (survey.recontactDays !== null) return formatRecontactDays(survey.recontactDays, t);
    return formatRecontactDays(workspace.recontactDays, t);
  };

  const displayOption = () => {
    switch (survey.displayOption) {
      case "displayMultiple":
        return t("workspace.surveys.edit.until_they_submit_a_response");
      case "respondMultiple":
        return t("workspace.surveys.edit.keep_showing_while_conditions_match");
      case "displaySome":
        return t("workspace.surveys.edit.show_multiple_times");
      default:
        return t("workspace.surveys.edit.show_only_once");
    }
  };

  const triggerDescription = (actionClass: TActionClass) => {
    if (actionClass.type === "code")
      return `(${t("workspace.surveys.summary.in_app.display_criteria.code_trigger")})`;
    const configType = actionClass.noCodeConfig?.type;
    const configTypeLabel = configType ? (noCodeLabels[configType] ?? configType) : "unknown";
    return `(${t("workspace.surveys.summary.in_app.display_criteria.no_code_trigger")}, ${configTypeLabel})`;
  };

  const segmentTitle = (segment: TSegment | null) => {
    if (segment?.filters?.length) {
      return segment.isPrivate
        ? t("workspace.surveys.summary.in_app.display_criteria.targeted")
        : segment.title;
    }
    return t("workspace.surveys.summary.in_app.display_criteria.everyone");
  };

  return (
    <div className="flex flex-col justify-between gap-y-6 pb-4">
      <div className="flex flex-col gap-y-6">
        <Alert variant={workspace.appSetupCompleted ? "success" : "warning"} role="status">
          <AlertTitle>
            {workspace.appSetupCompleted
              ? t("workspace.surveys.summary.in_app.connection_title")
              : t("workspace.surveys.summary.in_app.no_connection_title")}
          </AlertTitle>
          <AlertDescription>
            {workspace.appSetupCompleted
              ? t("workspace.surveys.summary.in_app.connection_description")
              : t("workspace.surveys.summary.in_app.no_connection_description")}
          </AlertDescription>
          {!workspace.appSetupCompleted && (
            <AlertButton asChild>
              <Link
                to="/workspaces/$workspaceId/integrations/app-connection"
                params={{ workspaceId: workspace.id }}>
                {t("common.set_up_forma_sdk")}
              </Link>
            </AlertButton>
          )}
        </Alert>

        <div className="flex flex-col gap-y-3">
          <H4>{t("workspace.surveys.summary.in_app.display_criteria")}</H4>
          <div className="flex w-full flex-col space-y-4 rounded-xl border border-slate-200 bg-white p-3 text-left shadow-xs">
            <DisplayCriteriaItem
              icon={<TimerResetIcon className="size-4" />}
              title={waitTime()}
              titleSuffix={
                survey.recontactDays !== null
                  ? `(${t("workspace.surveys.summary.in_app.display_criteria.overwritten")})`
                  : undefined
              }
              description={t("workspace.surveys.summary.in_app.display_criteria.time_based_description")}
            />
            <DisplayCriteriaItem
              icon={<UsersIcon className="size-4" />}
              title={segmentTitle(survey.segment)}
              description={t("workspace.surveys.summary.in_app.display_criteria.audience_description")}
            />
            {survey.triggers.map((trigger) => (
              <DisplayCriteriaItem
                key={trigger.actionClass.id}
                icon={
                  trigger.actionClass.type === "code" ? (
                    <CodeXmlIcon className="size-4" />
                  ) : (
                    <MousePointerClickIcon className="size-4" />
                  )
                }
                title={trigger.actionClass.name}
                titleSuffix={triggerDescription(trigger.actionClass)}
                description={t("workspace.surveys.summary.in_app.display_criteria.trigger_description")}
              />
            ))}
            {survey.displayPercentage !== null && survey.displayPercentage > 0 && (
              <DisplayCriteriaItem
                icon={<PercentIcon className="size-4" />}
                title={t("workspace.surveys.summary.in_app.display_criteria.randomizer", {
                  percentage: survey.displayPercentage,
                })}
                description={t("workspace.surveys.summary.in_app.display_criteria.randomizer_description", {
                  percentage: survey.displayPercentage,
                })}
              />
            )}
            <DisplayCriteriaItem
              icon={<Repeat1Icon className="size-4" />}
              title={displayOption()}
              description={t("workspace.surveys.summary.in_app.display_criteria.recontact_description")}
            />
          </div>
        </div>
      </div>
    </div>
  );
};
