import { Link } from "@tanstack/react-router";
import { TriangleAlertIcon, XIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { organizationSettingsPath } from "../../../lib/navigation";
import type { BillingLimits } from "../../../lib/queries/billing";

/** forma2's LimitsReachedBanner: a dismissible notice in the corner of the workspace and settings layouts on Forma Cloud once the month's responses reach a limited plan's allowance. Responses are still accepted and billed as overage; the banner only says so. */
export const LimitsReachedBanner = ({
  organizationId,
  limits,
  responseCount,
}: {
  organizationId: string;
  limits: BillingLimits;
  responseCount: number;
}) => {
  const { t } = useTranslation();
  const [show, setShow] = useState(true);
  const limit = limits.monthly.responses;
  if (!show || limit === null || responseCount < limit) return null;
  return (
    <div
      aria-live="assertive"
      className="pointer-events-none fixed inset-0 z-100 flex min-w-80 items-end px-4 py-6 sm:items-start sm:p-6">
      <div className="flex w-full flex-col items-center gap-y-4 sm:items-end">
        <div className="pointer-events-auto w-full max-w-sm overflow-hidden rounded-lg bg-white shadow-lg ring-1 ring-black/5 transition">
          <div className="relative flex p-4">
            <div className="shrink-0">
              <TriangleAlertIcon className="text-error size-6" aria-hidden="true" />
            </div>
            <div className="ml-3 w-0 flex-1">
              <p className="text-base font-medium text-slate-900">{t("common.limits_reached")}</p>
              <p className="mt-1 text-sm text-slate-500">
                {t("common.you_have_reached_your_monthly_response_limit_of_count", {
                  count: limit,
                })}
              </p>
              <Link
                to={organizationSettingsPath(organizationId, "billing")}
                className="text-sm text-slate-900">
                {t("common.learn_more")}
              </Link>
            </div>
            <button
              type="button"
              className="absolute top-4 right-4 inline-flex rounded-md bg-white text-slate-400 hover:text-slate-500 focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2 focus:outline-hidden"
              onClick={() => setShow(false)}>
              <span className="sr-only">{t("common.close")}</span>
              <XIcon className="size-5" aria-hidden="true" />
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};
