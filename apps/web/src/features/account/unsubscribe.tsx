import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { TFunction } from "i18next";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useIsPhone } from "../../components/shell/no-mobile-overlay";
import { ApiError } from "../../lib/api";
import { errorMessage } from "../../lib/errors";
import {
  type UnsubscribeLink,
  type UnsubscribeSubject,
  isInvalidUnsubscribeToken,
  unsubscribeSubject,
  unsubscribeTokenQuery,
  useSetUnsubscribeToken,
} from "../../lib/notifications";
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
            <p className="text-sm text-pretty break-words text-slate-600">
              {t("workspace.settings.notifications.unsubscribe_page.link_no_longer_applies_description", {
                email: me.email,
              })}
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

/** Where the signed link's page stands: asking to confirm, off by this page, already off when the link was opened, or back on. */
type TokenView = "confirm" | "unsubscribed" | "already_unsubscribed" | "resubscribed";

/**
 * The page a notification email's signed unsubscribe link opens, /unsubscribe?token=..., with or without a session and on any screen: it says which switch the link names and turns it off only when the reader confirms, since mail scanners and link previews open the links an email carries; then it offers to turn it back on. The token names the user, so nothing here needs them to sign in.
 */
export const TokenUnsubscribePage = ({ token }: { token: string }) => {
  const { t } = useTranslation();
  const lookup = useQuery({ ...unsubscribeTokenQuery(token), enabled: token !== "" });
  const setSwitch = useSetUnsubscribeToken(token);
  const [view, setView] = useState<TokenView>();
  const [failure, setFailure] = useState<Failure>();
  const isPhone = useIsPhone();

  const save = async (enabled: boolean) => {
    setFailure(undefined);
    try {
      await setSwitch.mutateAsync(enabled);
      setView(enabled ? "resubscribed" : "unsubscribed");
    } catch (error) {
      setFailure({ enabled, message: errorMessage(t, error) });
    }
  };

  const allSettings = (
    <div className="mt-6 border-t border-slate-200 pt-4 text-sm text-slate-600">
      <Link to="/account/settings/notifications" className={textLinkClasses}>
        {t("workspace.settings.notifications.unsubscribe_page.all_settings")}
      </Link>
      {isPhone && (
        <p className="mt-1 text-xs text-pretty text-slate-500">
          {t("workspace.settings.notifications.unsubscribe_page.all_settings_on_computer")}
        </p>
      )}
    </div>
  );

  if (token === "" || (lookup.isError && isInvalidUnsubscribeToken(lookup.error))) {
    return (
      <TokenMessage
        title={t("account.unsubscribe_page.invalid_link")}
        description={t("account.unsubscribe_page.invalid_link_description")}>
        {allSettings}
      </TokenMessage>
    );
  }
  if (lookup.isError) {
    const gone = lookup.error instanceof ApiError && lookup.error.status === 404;
    return (
      <TokenMessage
        title={
          gone
            ? t("workspace.settings.notifications.unsubscribe_page.link_no_longer_applies")
            : t("workspace.settings.notifications.unsubscribe_page.not_changed")
        }
        description={
          gone
            ? t("account.unsubscribe_page.link_no_longer_applies_description")
            : errorMessage(t, lookup.error)
        }>
        {!gone && (
          <div className="mt-6 flex flex-col gap-2">
            <Button loading={lookup.isFetching} onClick={() => void lookup.refetch()}>
              {t("common.try_again")}
            </Button>
          </div>
        )}
        {allSettings}
      </TokenMessage>
    );
  }
  if (!lookup.data) {
    return <TokenMessage title={t("common.loading")}>{null}</TokenMessage>;
  }

  const subject = lookup.data;
  const current: TokenView = view ?? (subject.enabled ? "confirm" : "already_unsubscribed");
  const title = failure
    ? t("workspace.settings.notifications.unsubscribe_page.not_changed")
    : {
        confirm: confirmTitle(t, subject),
        unsubscribed: t("workspace.settings.notifications.unsubscribe_page.unsubscribed"),
        already_unsubscribed: t("workspace.settings.notifications.unsubscribe_page.already_unsubscribed"),
        resubscribed: t("workspace.settings.notifications.unsubscribe_page.resubscribed"),
      }[current];

  return (
    <FormWrapper>
      <div className="text-center">
        <div aria-live="polite">
          <h1 className="mb-3 text-xl font-semibold text-balance text-slate-800">{title}</h1>
          {failure ? (
            <Alert variant="error">{failure.message}</Alert>
          ) : (
            current !== "confirm" && (
              <p className="text-sm text-pretty break-words text-slate-600">
                <SubjectDescription subject={subject} enabled={current === "resubscribed"} />
              </p>
            )
          )}
        </div>
        <div className="mt-6 flex flex-col gap-2">
          {failure ? (
            <Button loading={setSwitch.isPending} onClick={() => void save(failure.enabled)}>
              {t("common.try_again")}
            </Button>
          ) : current === "confirm" ? (
            <Button loading={setSwitch.isPending} onClick={() => void save(false)}>
              {t("workspace.settings.notifications.unsubscribe_page.unsubscribe")}
            </Button>
          ) : current === "resubscribed" ? (
            <Button variant="secondary" loading={setSwitch.isPending} onClick={() => void save(false)}>
              {t("workspace.settings.notifications.unsubscribe_page.unsubscribe")}
            </Button>
          ) : (
            <Button variant="secondary" loading={setSwitch.isPending} onClick={() => void save(true)}>
              {current === "unsubscribed"
                ? t("workspace.settings.notifications.unsubscribe_page.undo")
                : t("workspace.settings.notifications.unsubscribe_page.subscribe_again")}
            </Button>
          )}
        </div>
        {allSettings}
      </div>
    </FormWrapper>
  );
};

/** A signed link's page with nothing to switch: a heading, what it means, and what follows. */
const TokenMessage = ({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: ReactNode;
}) => (
  <FormWrapper>
    <div className="text-center">
      <div aria-live="polite">
        <h1 className="mb-3 text-xl font-semibold text-balance text-slate-800">{title}</h1>
        {description && <p className="text-sm text-pretty break-words text-slate-600">{description}</p>}
      </div>
      {children}
    </div>
  </FormWrapper>
);

/** The question the signed link's page asks before it switches anything, naming the survey or organization. */
const confirmTitle = (t: TFunction, subject: UnsubscribeSubject) => {
  if (subject.type === "alert") return t("account.unsubscribe_page.confirm_alert", { survey: subject.name });
  if (subject.type === "unsubscribedOrganizationIds") {
    return t("account.unsubscribe_page.confirm_auto_subscribe", { organization: subject.name });
  }
  return t("account.unsubscribe_page.confirm_weekly_summary", { organization: subject.name });
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
