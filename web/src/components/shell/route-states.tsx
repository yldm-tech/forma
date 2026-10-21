import { type ErrorComponentProps, useRouter } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { errorMessage } from "../../lib/errors";
import { Button } from "../ui/button";
import { LoadingBlock } from "../ui/page";

/** What a route shows when its data could not be loaded: the error, as the client error mapping words it, and a retry. */
export const RouteError = ({ error, reset }: ErrorComponentProps) => {
  const { t } = useTranslation();
  const router = useRouter();
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
