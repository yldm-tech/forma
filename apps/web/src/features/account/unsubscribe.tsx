import { useSuspenseQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useIsPhone } from "../../components/shell/no-mobile-overlay";
import { errorMessage } from "../../lib/errors";
import { type UnsubscribeLink, type UnsubscribeSubject, unsubscribeSubject } from "../../lib/notifications";
import { meQuery, notificationsQuery, useSetNotification } from "../../lib/queries/auth";
import { Alert, Button, FormWrapper, textLinkClasses } from "../auth/components/ui";

/** Where the page stands: turning the switch off as it opens, off (by this page, or already before the link was opened), back on, or naming no switch the user has. */
type View = "unsubscribing" | "unsubscribed" | "already_unsubscribed" | "resubscribed" | "missing";

/** A save that failed: which way it was turning the switch, to try again, and what to say. */
interface Failure {
  enabled: boolean;
  message: string;
}

/**
 * The page an email's unsubscribe link opens on a phone, where the notification settings stay covered: it turns the named switch off once as it opens, as the settings page does on a bigger screen, then says what is off and offers to undo it, and links to the full settings, which open on a computer. The save is the settings page's own request, so it needs the session and passes the same origin and CSRF checks.
 */
export const UnsubscribePage = ({ link }: { link: UnsubscribeLink }) => {
  const { t } = useTranslation();
  const { data: me } = useSuspenseQuery(meQuery);
  const { data: organizations } = useSuspenseQuery(notificationsQuery);
  const setNotification = useSetNotification();
  const isPhone = useIsPhone();
  // Read once: the settings refetch after each save, and the page keeps describing the switch the link named.
  const [subject] = useState(() => unsubscribeSubject(link, organizations));
  const [view, setView] = useState<View>(() =>
    !subject ? "missing" : subject.enabled ? "unsubscribing" : "already_unsubscribed"
  );
  const [failure, setFailure] = useState<Failure>();
  const started = useRef(false);

  const save = async (target: UnsubscribeSubject, enabled: boolean) => {
    setFailure(undefined);
    try {
      await setNotification.mutateAsync({ type: target.type, id: target.id, enabled });
      setView(enabled ? "resubscribed" : "unsubscribed");
    } catch (error) {
      setFailure({ enabled, message: errorMessage(t, error) });
    }
  };

  // The link's one click: the switch goes off once, when the page opens.
  useEffect(() => {
    if (started.current || !subject?.enabled) return;
    started.current = true;
    void save(subject, false);
    // oxlint-disable-next-line react/exhaustive-deps -- one save per page, guarded by started, for the link the page opened with
  }, []);

  const title = failure
    ? t("workspace.settings.notifications.unsubscribe_page.not_changed")
    : {
        unsubscribing: t("workspace.settings.notifications.unsubscribe_page.unsubscribing"),
        unsubscribed: t("workspace.settings.notifications.unsubscribe_page.unsubscribed"),
        already_unsubscribed: t("workspace.settings.notifications.unsubscribe_page.already_unsubscribed"),
        resubscribed: t("workspace.settings.notifications.unsubscribe_page.resubscribed"),
        missing: t("workspace.settings.notifications.unsubscribe_page.link_no_longer_applies"),
      }[view];

  return (
    <FormWrapper>
      <div className="text-center">
        <div aria-live="polite">
          <h1 className="mb-3 text-xl font-semibold text-balance text-slate-800">{title}</h1>
          {failure ? (
            <Alert variant="error">{failure.message}</Alert>
          ) : view === "missing" ? (
            <p className="text-sm text-pretty text-slate-600">
              {t("workspace.settings.notifications.unsubscribe_page.link_no_longer_applies_description")}
            </p>
          ) : (
            subject &&
            view !== "unsubscribing" && (
              <p className="text-sm text-pretty break-words text-slate-600">
                <SubjectDescription subject={subject} enabled={view === "resubscribed"} />
              </p>
            )
          )}
        </div>
        {subject && (
          <div className="mt-6 flex flex-col gap-2">
            {failure ? (
              <Button loading={setNotification.isPending} onClick={() => void save(subject, failure.enabled)}>
                {t("common.try_again")}
              </Button>
            ) : view === "unsubscribing" ? (
              // The same look as the undo that replaces it, so the button does not fade from dark to light.
              <Button variant="secondary" loading disabled>
                {t("workspace.settings.notifications.unsubscribe_page.unsubscribe")}
              </Button>
            ) : view === "resubscribed" ? (
              <Button
                variant="secondary"
                loading={setNotification.isPending}
                onClick={() => void save(subject, false)}>
                {t("workspace.settings.notifications.unsubscribe_page.unsubscribe")}
              </Button>
            ) : (
              <Button
                variant="secondary"
                loading={setNotification.isPending}
                onClick={() => void save(subject, true)}>
                {view === "unsubscribed"
                  ? t("workspace.settings.notifications.unsubscribe_page.undo")
                  : t("workspace.settings.notifications.unsubscribe_page.subscribe_again")}
              </Button>
            )}
          </div>
        )}
        <div className="mt-6 border-t border-slate-200 pt-4 text-sm text-slate-600">
          <Link to="/account/settings/notifications" className={textLinkClasses}>
            {t("workspace.settings.notifications.unsubscribe_page.all_settings")}
          </Link>
          {isPhone && (
            <p className="mt-1 text-xs text-pretty text-slate-500">
              {t("workspace.settings.notifications.unsubscribe_page.all_settings_on_computer")}
            </p>
          )}
          <p className="mt-4 text-xs break-all text-slate-500">
            {t("common.mobile_overlay_signed_in_as", { email: me.email })}
          </p>
        </div>
      </div>
    </FormWrapper>
  );
};

/** What the switch now does, in words, naming the survey or organization. */
const SubjectDescription = ({ subject, enabled }: { subject: UnsubscribeSubject; enabled: boolean }) => {
  const { t } = useTranslation();
  const name = subject.name;
  if (subject.type === "alert") {
    return enabled
      ? t("workspace.settings.notifications.unsubscribe_page.alert_on", { survey: name })
      : t("workspace.settings.notifications.unsubscribe_page.alert_off", { survey: name });
  }
  if (subject.type === "unsubscribedOrganizationIds") {
    return enabled
      ? t("workspace.settings.notifications.unsubscribe_page.auto_subscribe_on", { organization: name })
      : t("workspace.settings.notifications.unsubscribe_page.auto_subscribe_off", { organization: name });
  }
  return enabled
    ? t("workspace.settings.notifications.unsubscribe_page.weekly_summary_on", { organization: name })
    : t("workspace.settings.notifications.unsubscribe_page.weekly_summary_off", { organization: name });
};
