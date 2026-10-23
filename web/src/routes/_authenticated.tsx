import { useSuspenseQuery } from "@tanstack/react-query";
import { Outlet, createFileRoute, redirect } from "@tanstack/react-router";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { NoMobileOverlay } from "../components/shell/no-mobile-overlay";
import { ApiError } from "../lib/api";
import { changeLocale, isLocale } from "../lib/i18n";
import { loginHref } from "../lib/navigation";
import { meQuery } from "../lib/queries/auth";
import { PostHogIdentify } from "../lib/telemetry/posthog-identify";

// Every page under this layout needs a session; without one the browser goes to sign-in and comes back afterwards, as forma2's proxy sent it.
export const Route = createFileRoute("/_authenticated")({
  beforeLoad: async ({ context, location }) => {
    try {
      await context.queryClient.ensureQueryData(meQuery);
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        throw redirect({ href: loginHref(location.href), replace: true });
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

  return (
    <>
      {/* forma2's app layout covered every signed-in page on a phone-sized screen. */}
      <NoMobileOverlay />
      <PostHogIdentify userId={me.id} email={me.email} />
      <Outlet />
    </>
  );
}
