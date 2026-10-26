import { useSuspenseQuery } from "@tanstack/react-query";
import { Outlet, createFileRoute, redirect, useParams } from "@tanstack/react-router";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { NoMobileOverlay, useIsPhone } from "../components/shell/no-mobile-overlay";
import { ApiError } from "../lib/api";
import { appHref } from "../lib/base-path";
import { changeLocale, isLocale } from "../lib/i18n";
import { loginHref } from "../lib/navigation";
import { useIsPhoneReadyPage } from "../lib/phone";
import { meQuery } from "../lib/queries/auth";
import { PostHogIdentify } from "../lib/telemetry/posthog-identify";

// Every page under this layout needs a session; without one the browser goes to sign-in and comes back afterwards, as forma2's proxy sent it.
export const Route = createFileRoute("/_authenticated")({
  beforeLoad: async ({ context, location }) => {
    try {
      await context.queryClient.ensureQueryData(meQuery);
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        throw redirect({ href: loginHref(appHref(location.href)), replace: true });
      }
      throw error;
    }
  },
  component: AuthenticatedLayout,
});

function AuthenticatedLayout() {
  const { data: me } = useSuspenseQuery(meQuery);
  const { i18n } = useTranslation();

  // The signed-in user's saved language replaces the browser's once it is known.
  useEffect(() => {
    if (isLocale(me.locale) && i18n.language !== me.locale) void changeLocale(i18n, me.locale);
  }, [me.locale, i18n]);

  // forma2's app layout covered every signed-in page on a phone-sized screen. Here only the pages that build and manage are covered: the survey list, a survey's summary and responses, the contacts and a contact open on a phone, read-only, and so does the email unsubscribe page (/account/unsubscribe), which flips the one notification switch its link names (PHONE_READY). Under the overlay the app stays mounted, inert, so nothing behind it takes focus or reaches a screen reader, and clipped to the screen, so a page wider than the phone cannot widen the layout viewport and push the overlay's text off its edge.
  const isPhone = useIsPhone();
  const isPhoneReadyPage = useIsPhoneReadyPage();
  const isCovered = isPhone && !isPhoneReadyPage;
  const { workspaceId } = useParams({ strict: false });
  return (
    <>
      {isCovered && <NoMobileOverlay email={me.email} workspaceId={workspaceId} />}
      <PostHogIdentify userId={me.id} email={me.email} />
      <div inert={isCovered || undefined} className={isCovered ? "h-dvh overflow-hidden" : "contents"}>
        <Outlet />
      </div>
    </>
  );
}
