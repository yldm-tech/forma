"use client";

import { Trans, useTranslation } from "react-i18next";
import { Badge } from "@/modules/ui/components/badge";
import { BillingSlider } from "./billing-slider";

interface UsageCardProps {
  metric: string;
  currentCount: number;
  limit: number | null;
  isUnlimited: boolean;
  unlimitedLabel: string;
}

export const UsageCard = ({ metric, currentCount, limit, isUnlimited, unlimitedLabel }: UsageCardProps) => {
  const { i18n } = useTranslation();
  const locale = i18n.resolvedLanguage ?? i18n.language ?? "en-US";

  if (isUnlimited) {
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
      <BillingSlider value={currentCount} max={limit} />
    </div>
  );
};
