import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { HelpCircleIcon, SlackIcon, UsersIcon } from "lucide-react";
import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription } from "../../components/ui/alert";
import { PageContentWrapper, PageHeader, SettingsCard } from "../../components/ui/page";
import { Switch } from "../../components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../../components/ui/table";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "../../components/ui/tooltip";
import { errorMessage } from "../../lib/errors";
import { organizationSettingsPath } from "../../lib/navigation";
import { type UnsubscribeLink, alertRows, unsubscribeTarget } from "../../lib/notifications";
import {
  type NotificationOrganization,
  type NotificationType,
  authOptionsQuery,
  notificationsQuery,
  useSetNotification,
} from "../../lib/queries/auth";
import { workspaceQuery } from "../../lib/queries/workspaces";
import { WORKSPACE_ID_KEY, readStorage } from "../../lib/storage";
import { toast } from "../../lib/toast";

/** forma2's notifications page: per organization, whether new surveys alert the user, and a per-response alert switch for every survey they can see; then, per organization, whether the user gets its weekly summary email. Where the instance has no SMTP the switches still save, and the page says no alert will be mailed until it does. */
export const NotificationsPage = ({ link }: { link: UnsubscribeLink }) => {
  const { t } = useTranslation();
  const { data: organizations } = useSuspenseQuery(notificationsQuery);
  const options = useQuery(authOptionsQuery);
  const setNotification = useSetNotification();
  const handledLink = useRef(false);

  const change = async (type: NotificationType, id: string, enabled: boolean) => {
    try {
      await setNotification.mutateAsync({ type, id, enabled });
      toast.success(t("workspace.settings.notifications.notification_settings_updated"), {
        id: "notification-switch",
      });
    } catch (error) {
      toast.error(errorMessage(t, error), { id: "notification-switch" });
    }
  };

  // An unsubscribe link from an email switches its alert off once, when the page opens.
  useEffect(() => {
    if (handledLink.current) return;
    handledLink.current = true;
    const target = unsubscribeTarget(link, organizations);
    if (!target) return;
    const done = {
      alert: t(
        "workspace.settings.notifications.you_will_not_receive_any_more_emails_for_responses_on_this_survey"
      ),
      unsubscribedOrganizationIds: t(
        "workspace.settings.notifications.you_will_not_be_auto_subscribed_to_this_organizations_surveys_anymore"
      ),
      unsubscribedWeeklySummaryOrganizationIds: t(
        "workspace.settings.notifications.you_will_not_receive_the_weekly_summary_for_this_organization_anymore"
      ),
    }[target.type];
    void setNotification.mutateAsync({ ...target, enabled: false }).then(
      () => toast.success(done, { id: "notification-switch" }),
      (error: unknown) => toast.error(errorMessage(t, error), { id: "notification-switch" })
    );
  }, [link, organizations, setNotification, t]);

  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("common.notifications")} />
      {options.data?.smtpConfigured === false && (
        <Alert variant="warning" role="status">
          <AlertDescription>{t("workspace.settings.notifications.email_not_configured")}</AlertDescription>
        </Alert>
      )}
      <SettingsCard
        width="full"
        title={t("workspace.settings.notifications.email_alerts_surveys")}
        description={t("workspace.settings.notifications.set_up_an_alert_to_get_an_email_on_new_responses")}>
        {organizations.length === 0 ? (
          <div className="mb-4 rounded-xl border border-slate-100 bg-white p-4 text-center shadow-2xs">
            <p className="text-sm text-slate-500">{t("common.no_surveys_found")}</p>
          </div>
        ) : (
          organizations.map((organization) => (
            <OrganizationAlerts
              key={organization.id}
              organization={organization}
              disabled={setNotification.isPending}
              onChange={(type, id, enabled) => void change(type, id, enabled)}
            />
          ))
        )}
      </SettingsCard>
      {organizations.length > 0 && (
        <SettingsCard
          width="full"
          title={t("workspace.settings.notifications.weekly_summary")}
          description={t("workspace.settings.notifications.weekly_summary_description")}>
          <ul className="divide-y divide-slate-100 rounded-lg border border-slate-200">
            {organizations.map((organization) => (
              <li key={organization.id} className="flex items-center justify-between gap-x-3 px-4 py-3">
                <div className="flex items-center gap-x-3">
                  <UsersIcon className="h-6 w-7 text-slate-600" />
                  <p className="text-sm font-medium text-slate-800">{organization.name}</p>
                </div>
                <Switch
                  aria-label={`${t("workspace.settings.notifications.weekly_summary")}: ${organization.name}`}
                  checked={organization.weeklySummary}
                  disabled={setNotification.isPending}
                  onCheckedChange={(enabled) =>
                    void change("unsubscribedWeeklySummaryOrganizationIds", organization.id, enabled)
                  }
                />
              </li>
            ))}
          </ul>
        </SettingsCard>
      )}
      <IntegrationsTip />
    </PageContentWrapper>
  );
};

/** forma2's IntegrationsTip: Slack and other channels are integrations of the workspace the user works in, the one this browser last opened, when they can still open it. */
const IntegrationsTip = () => {
  const { t } = useTranslation();
  const remembered = readStorage(WORKSPACE_ID_KEY);
  const workspace = useQuery({ ...workspaceQuery(remembered ?? ""), enabled: !!remembered, retry: false });
  if (!workspace.data) return null;
  return (
    <div className="flex max-w-4xl items-center gap-y-3 rounded-lg border border-blue-100 bg-blue-50 p-4 text-sm text-blue-900 shadow-xs md:gap-y-0 md:text-base">
      <SlackIcon className="mr-3 size-4 text-blue-400" />
      <p className="text-sm">
        {t("workspace.settings.notifications.need_slack_or_discord_notifications")}?
        <Link
          to="/workspaces/$workspaceId/integrations"
          params={{ workspaceId: workspace.data.id }}
          className="ml-1 cursor-pointer text-sm underline">
          {t("workspace.settings.notifications.use_the_integration")}
        </Link>
      </p>
    </div>
  );
};

const OrganizationAlerts = ({
  organization,
  disabled,
  onChange,
}: {
  organization: NotificationOrganization;
  disabled: boolean;
  onChange: (type: NotificationType, id: string, enabled: boolean) => void;
}) => {
  const { t } = useTranslation();
  return (
    <div className="mb-6">
      <div className="mb-5 grid grid-cols-6 items-center gap-x-3">
        <div className="col-span-3 flex items-center gap-x-3">
          <UsersIcon className="h-6 w-7 text-slate-600" />
          <p className="text-sm font-medium text-slate-800">{organization.name}</p>
        </div>
        <div className="col-span-3 flex items-center justify-end pr-2">
          <p id={`auto-subscribe-${organization.id}`} className="pr-4 text-sm text-slate-600">
            {t("workspace.settings.notifications.auto_subscribe_to_new_surveys")}
          </p>
          <Switch
            aria-labelledby={`auto-subscribe-${organization.id}`}
            checked={organization.autoSubscribe}
            disabled={disabled}
            onCheckedChange={(enabled) => onChange("unsubscribedOrganizationIds", organization.id, enabled)}
          />
        </div>
      </div>
      <div className="overflow-hidden rounded-lg border border-slate-200">
        <Table aria-label={organization.name}>
          <TableHeader>
            <TableRow>
              <TableHead className="w-[45%]">{t("common.survey")}</TableHead>
              <TableHead className="w-[30%]">{t("common.workspace")}</TableHead>
              <TableHead className="w-[25%] text-center">
                <TooltipProvider delayDuration={50}>
                  <Tooltip>
                    <TooltipTrigger className="inline-flex cursor-default items-center gap-x-2">
                      <span>{t("workspace.settings.notifications.every_response")}</span>
                      <HelpCircleIcon className="size-4 shrink-0 text-slate-500" />
                    </TooltipTrigger>
                    <TooltipContent>
                      {t("workspace.settings.notifications.every_response_tooltip")}
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {organization.surveys.length === 0 ? (
              <TableRow>
                <TableCell colSpan={3} className="text-center text-slate-500">
                  {t("common.no_surveys_found")}
                </TableCell>
              </TableRow>
            ) : (
              alertRows(organization.surveys).map((survey) => (
                <TableRow key={survey.id}>
                  <TableCell className="font-medium text-slate-900">{survey.name}</TableCell>
                  <TableCell className="text-slate-500">{survey.workspaceName}</TableCell>
                  <TableCell>
                    <div className="flex justify-center">
                      <Switch
                        aria-label={`${t("workspace.settings.notifications.every_response")}: ${survey.name}`}
                        checked={survey.alert}
                        disabled={disabled}
                        onCheckedChange={(enabled) => onChange("alert", survey.id, enabled)}
                      />
                    </div>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
        <p className="px-4 py-3 text-xs text-slate-400">
          {t("workspace.settings.notifications.want_to_loop_in_organization_mates")}{" "}
          <Link className="font-semibold" to={organizationSettingsPath(organization.id, "teams")}>
            {t("common.invite_them")}
          </Link>
        </p>
      </div>
    </div>
  );
};
