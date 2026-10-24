import { type ErrorComponentProps, Link, useRouter } from "@tanstack/react-router";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { ApiError } from "../../lib/api";
import { reportError } from "../../lib/telemetry/sentry";
import { Button } from "../ui/button";
import { ErrorComponent } from "../ui/error-component";
import { LoadingBlock } from "../ui/page";
import { appHref } from "../../lib/base-path";

/** forma2's error boundary (app/error.tsx): a rate limit gets its own wording and no buttons, anything else the generic error with a retry and a way back to the dashboard. An error the app does not expect is reported to Sentry. */
export const RouteError = ({ error, reset }: ErrorComponentProps) => {
  const { t } = useTranslation();
  const router = useRouter();
  useEffect(() => reportError(error), [error]);
  const rateLimited = error instanceof ApiError && error.isRateLimited;
  return (
    <main className="flex h-full min-h-[50dvh] w-full flex-col items-center justify-center p-8">
      <div role="alert">
        <ErrorComponent
          title={rateLimited ? t("common.error_rate_limit_title") : t("common.error_component_title")}
          description={
            rateLimited ? t("common.error_rate_limit_description") : t("common.error_component_description")
          }
        />
      </div>
      {!rateLimited && (
        <div className="mt-2">
          <Button
            variant="secondary"
            className="mr-2"
            onClick={() => {
              reset();
              void router.invalidate();
            }}>
            {t("common.try_again")}
          </Button>
          {/* A full load, as forma2's window.location.href: whatever broke is not carried into the dashboard. */}
          <Button onClick={() => globalThis.location.assign(appHref("/"))}>
            {t("common.go_to_dashboard")}
          </Button>
        </div>
      )}
    </main>
  );
};

export const RoutePending = () => {
  const { t } = useTranslation();
  return (
    <div className="space-y-4 p-6" role="status" aria-label={t("common.loading")}>
      <LoadingBlock />
      <LoadingBlock className="w-60" />
    </div>
  );
};

/** forma2's not-found page (app/not-found.tsx): the code, what happened and the way home. */
export const NotFound = () => {
  const { t } = useTranslation();
  return (
    <main className="mx-auto flex h-full max-w-xl flex-col items-center justify-center py-16 text-center">
      <p className="text-sm font-semibold text-zinc-900" data-testid="error-code">
        404
      </p>
      <h1 className="mt-2 text-2xl font-bold text-zinc-900">{t("common.page_not_found")}</h1>
      <p className="mt-2 text-base text-zinc-600" data-testid="error-message">
        {t("common.page_not_found_description")}
      </p>
      <Button asChild className="mt-8">
        <Link to="/">{t("common.back_to_home")}</Link>
      </Button>
    </main>
  );
};
