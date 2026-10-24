import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertTitle } from "../../../components/ui/alert";
import { cn } from "../../../lib/cn";

/** forma2's getTrialVariant: red in the last three days, amber in the last week. */
export const trialAlertVariant = (daysRemaining: number): "error" | "warning" | "info" => {
  if (daysRemaining <= 3) return "error";
  if (daysRemaining <= 7) return "warning";
  return "info";
};

/** forma2's TrialAlert: how long the trial has left, coloured by how soon it ends, with the caller's description and action beneath. */
export const TrialAlert = ({
  trialDaysRemaining,
  size,
  className,
  children,
}: {
  trialDaysRemaining: number;
  size?: "small";
  className?: string;
  children?: ReactNode;
}) => {
  const { t } = useTranslation();
  const title =
    trialDaysRemaining <= 0
      ? t("common.trial_expired")
      : trialDaysRemaining === 1
        ? t("common.trial_one_day_remaining")
        : t("common.trial_days_remaining", { count: trialDaysRemaining });
  return (
    <Alert
      variant={trialAlertVariant(trialDaysRemaining)}
      size={size}
      className={cn("max-w-4xl", className)}
      role="status">
      <AlertTitle>{title}</AlertTitle>
      {children}
    </Alert>
  );
};
