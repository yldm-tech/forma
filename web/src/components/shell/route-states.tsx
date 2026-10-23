import { type ErrorComponentProps, useRouter } from "@tanstack/react-router";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { errorMessage } from "../../lib/errors";
import { reportError } from "../../lib/telemetry/sentry";
import { Button } from "../ui/button";
import { LoadingBlock } from "../ui/page";

/** What a route shows when its data could not be loaded or it failed to render: the error, as the client error mapping words it, and a retry. An error the app does not expect is reported to Sentry, as forma2's error boundary did. */
export const RouteError = ({ error, reset }: ErrorComponentProps) => {
  const { t } = useTranslation();
  const router = useRouter();
  useEffect(() => reportError(error), [error]);
  return (
    <main className="mx-auto max-w-xl p-8">
      <h1 className="text-xl font-semibold text-slate-800">{t("common.something_went_wrong")}</h1>
      <p className="mt-2 text-sm text-slate-600" role="alert">
        {errorMessage(t, error)}
      </p>
      <Button
        className="mt-4"
        variant="secondary"
        onClick={() => {
          reset();
          void router.invalidate();
        }}>
        {t("common.try_again")}
      </Button>
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

export const NotFound = () => {
  const { t } = useTranslation();
  return (
    <main className="mx-auto max-w-xl p-8">
      <h1 className="text-xl font-semibold text-slate-800">{t("common.page_not_found")}</h1>
    </main>
  );
};
