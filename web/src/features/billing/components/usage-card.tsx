import { Trans, useTranslation } from "react-i18next";
import { Badge } from "../../../components/ui/badge";
import { cn } from "../../../lib/cn";

/** forma2's BillingSlider: how much of a limit is used, amber from 90 %. */
const UsageBar = ({ value, max }: { value: number; max: number }) => {
  const percentage = Math.min((value / max) * 100, 100);
  return (
    <div
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={max}
      aria-valuenow={Math.min(value, max)}
      className="relative h-2 w-full overflow-hidden rounded-full bg-slate-200">
      <div
        style={{ width: `${percentage}%` }}
        className={cn(
          "h-full rounded-full transition-all",
          percentage >= 90 ? "bg-amber-600" : "bg-slate-800"
        )}
      />
    </div>
  );
};

/** forma2's UsageCard: a metric's use against its limit, or a badge when it is unlimited. Nothing is shown for a zero limit. */
export const UsageCard = ({
  metric,
  currentCount,
  limit,
  unlimitedLabel,
}: {
  metric: string;
  currentCount: number;
  /** null is unlimited. */
  limit: number | null;
  unlimitedLabel: string;
}) => {
  const { i18n } = useTranslation();
  const locale = i18n.resolvedLanguage ?? i18n.language;
  if (limit === null) {
    return (
      <div className="flex items-center justify-between">
        <p className="text-sm font-semibold text-slate-700">{metric}</p>
        <Badge type="success" size="normal" text={unlimitedLabel} />
      </div>
    );
  }
  if (!limit) return null;
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <p className="text-sm font-semibold text-slate-700">{metric}</p>
        <p className="text-sm text-slate-600">
          <Trans
            i18nKey="workspace.settings.billing.usage_count_of_limit_used"
            values={{ current: currentCount.toLocaleString(locale), limit: limit.toLocaleString(locale) }}
            components={{ muted: <span className="text-slate-400" /> }}
          />
        </p>
      </div>
      <UsageBar value={currentCount} max={limit} />
    </div>
  );
};
