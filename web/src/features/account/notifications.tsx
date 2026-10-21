import { useSuspenseQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { UsersIcon } from "lucide-react";
import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { PageContentWrapper, PageHeader, SettingsCard } from "../../components/ui/page";
import { Switch } from "../../components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../../components/ui/table";
import { errorMessage } from "../../lib/errors";
import { organizationSettingsPath } from "../../lib/navigation";
import { type UnsubscribeLink, unsubscribeTarget } from "../../lib/notifications";
import {
  type NotificationOrganization,
  notificationsQuery,
  useSetNotification,
} from "../../lib/queries/auth";
import { toast } from "../../lib/toast";

/** forma2's notifications page: per organization, whether new surveys alert the user, and a per-response alert switch for every survey they can see. */
export const NotificationsPage = ({ link }: { link: UnsubscribeLink }) => {
  const { t } = useTranslation();
  const { data: organizations } = useSuspenseQuery(notificationsQuery);
  const setNotification = useSetNotification();
  const handledLink = useRef(false);

  const change = async (type: "alert" | "unsubscribedOrganizationIds", id: string, enabled: boolean) => {
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
    void setNotification.mutateAsync({ ...target, enabled: false }).then(
      () =>
        toast.success(
          target.type === "alert"
            ? t(
                "workspace.settings.notifications.you_will_not_receive_any_more_emails_for_responses_on_this_survey"
              )
            : t(
                "workspace.settings.notifications.you_will_not_be_auto_subscribed_to_this_organizations_surveys_anymore"
              ),
          { id: "notification-switch" }
        ),
      (error: unknown) => toast.error(errorMessage(t, error), { id: "notification-switch" })
    );
  }, [link, organizations, setNotification, t]);

  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("common.notifications")} />
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
    </PageContentWrapper>
  );
};

const OrganizationAlerts = ({
  organization,
  disabled,
  onChange,
}: {
  organization: NotificationOrganization;
  disabled: boolean;
  onChange: (type: "alert" | "unsubscribedOrganizationIds", id: string, enabled: boolean) => void;
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
              <TableHead
                className="w-[25%] text-center"
                title={t("workspace.settings.notifications.every_response_tooltip")}>
                {t("workspace.settings.notifications.every_response")}
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
              organization.surveys.map((survey) => (
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
