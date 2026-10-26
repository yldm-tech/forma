import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ShieldAlertIcon, XIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription } from "../../components/ui/alert";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Label } from "../../components/ui/label";
import { SettingsCard } from "../../components/ui/page";
import { Select } from "../../components/ui/select";
import { errorMessage } from "../../lib/errors";
import {
  type Member,
  type OrganizationDetail,
  type TwoFactorStatus,
  membersQuery,
  organizationKeys,
  useUpdateOrganization,
} from "../../lib/queries/organizations";
import { accessFlags } from "../../lib/roles";
import { readStorage, writeStorage } from "../../lib/storage";
import { toast } from "../../lib/toast";

/** The grace periods an owner chooses from, in days; 0 enforces the requirement at once. The server accepts up to 90. */
export const TWO_FACTOR_GRACE_PERIODS = [0, 3, 7, 14, 30] as const;

/** Whether the organization requires two-factor authentication of the caller, who has not turned it on: they are reminded during the grace period and refused after it. */
export const mustTurnOnTwoFactor = (organization: OrganizationDetail): boolean =>
  organization.twoFactorEnforcedAt !== null && organization.twoFactorStatus === "disabled";

/** Whether the instant has passed: for a requirement, whether its grace period is over. */
const isPast = (iso: string): boolean => new Date(iso).getTime() <= Date.now();

const formatDate = (language: string, iso: string): string =>
  new Intl.DateTimeFormat(language, { dateStyle: "long", timeStyle: "short" }).format(new Date(iso));

/** How many members still have to turn two-factor authentication on: those who sign in with a password and have not. */
export const membersWithoutTwoFactor = (members: readonly Member[]): number =>
  members.filter((member) => member.twoFactor === "disabled").length;

/**
 * The organization's two-factor requirement on its general settings page. Owners turn it on with a grace period, after turning it on for their own account (the requirement would refuse them at once otherwise, and the server says so), and turn it off; managers and members see whether it applies. Owners and managers also see how many members still have to turn it on, which the members table details.
 */
export const TwoFactorRequirementCard = ({ organization }: { organization: OrganizationDetail }) => {
  const { t, i18n } = useTranslation();
  const { isOwner, isOwnerOrManager } = accessFlags(organization.membership.role);
  const update = useUpdateOrganization(organization.id);
  const queryClient = useQueryClient();
  const enforcedAt = organization.twoFactorEnforcedAt;
  const members = useQuery({
    ...membersQuery(organization.id),
    enabled: isOwnerOrManager && enforcedAt !== null,
  });
  const [graceDays, setGraceDays] = useState<number>(7);
  const ownStatusBlocks = organization.twoFactorStatus === "disabled";

  const save = async (requireTwoFactor: boolean) => {
    try {
      await update.mutateAsync(
        requireTwoFactor ? { requireTwoFactor, twoFactorGracePeriodDays: graceDays } : { requireTwoFactor }
      );
      toast.success(
        requireTwoFactor
          ? t("organizations.two_factor.requirement_saved")
          : t("organizations.two_factor.requirement_removed")
      );
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
    await queryClient.invalidateQueries({ queryKey: organizationKeys.detail(organization.id) });
  };

  const missing = members.data ? membersWithoutTwoFactor(members.data) : null;
  return (
    <SettingsCard
      width="full"
      title={t("organizations.two_factor.card_title")}
      description={t("organizations.two_factor.card_description")}>
      <div className="max-w-xl space-y-4 pb-4">
        {enforcedAt === null ? (
          <p className="text-sm text-slate-700">{t("organizations.two_factor.optional_status")}</p>
        ) : (
          <p className="text-sm text-slate-700">
            {!isPast(enforcedAt)
              ? t("organizations.two_factor.required_status_grace", {
                  date: formatDate(i18n.language, enforcedAt),
                })
              : t("organizations.two_factor.required_status_enforced", {
                  date: formatDate(i18n.language, enforcedAt),
                })}
          </p>
        )}
        {enforcedAt !== null && missing !== null && (
          <p className="text-sm text-slate-700">
            {t("organizations.two_factor.missing_count", { count: missing })}
          </p>
        )}
        {!isOwner ? (
          <p className="text-xs text-slate-500">{t("organizations.two_factor.owner_only")}</p>
        ) : enforcedAt === null ? (
          <>
            {ownStatusBlocks && (
              <Alert variant="warning" role="status">
                <AlertDescription>
                  {t("organizations.two_factor.enable_own_first")}{" "}
                  <Link to="/account/settings/profile" className="font-medium underline">
                    {t("organizations.two_factor.open_profile")}
                  </Link>
                </AlertDescription>
              </Alert>
            )}
            <div className="space-y-2">
              <Label htmlFor="two-factor-grace-period">
                {t("organizations.two_factor.grace_period_label")}
              </Label>
              <p id="two-factor-grace-period-description" className="text-sm text-slate-500">
                {t("organizations.two_factor.grace_period_description")}
              </p>
              <Select
                id="two-factor-grace-period"
                aria-describedby="two-factor-grace-period-description"
                containerClassName="max-w-xs"
                value={graceDays}
                disabled={ownStatusBlocks}
                onChange={(e) => setGraceDays(Number(e.target.value))}>
                {TWO_FACTOR_GRACE_PERIODS.map((days) => (
                  <option key={days} value={days}>
                    {days === 0
                      ? t("organizations.two_factor.grace_period_none")
                      : t("organizations.two_factor.grace_period_days", { days })}
                  </option>
                ))}
              </Select>
            </div>
            <Button
              size="sm"
              loading={update.isPending}
              disabled={ownStatusBlocks}
              onClick={() => void save(true)}>
              {t("organizations.two_factor.require_button")}
            </Button>
          </>
        ) : (
          <Button size="sm" variant="secondary" loading={update.isPending} onClick={() => void save(false)}>
            {t("organizations.two_factor.stop_button")}
          </Button>
        )}
      </div>
    </SettingsCard>
  );
};

/** A member's two-factor status in the members table, which owners and managers see. */
export const TwoFactorStatusBadge = ({ status }: { status: TwoFactorStatus }) => {
  const { t } = useTranslation();
  if (status === "enabled")
    return <Badge type="success" size="tiny" text={t("organizations.two_factor.status_enabled")} />;
  if (status === "disabled")
    return <Badge type="warning" size="tiny" text={t("organizations.two_factor.status_disabled")} />;
  const hint = t("organizations.two_factor.status_sso_hint");
  return (
    <span title={hint}>
      <Badge type="gray" size="tiny" text={t("organizations.two_factor.status_sso")} />
      <span className="sr-only">{hint}</span>
    </span>
  );
};

/** The day a reminder was dismissed, per organization: it stays away for the rest of that day and comes back on the next. */
const reminderKey = (organizationId: string) => `forma-two-factor-reminder-${organizationId}`;

/** The user's own calendar day, not UTC's: a reminder put away in the morning east of UTC must not come back when the UTC date changes later that day. */
const today = () => {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
};

/**
 * The reminder a member gets in the organization's pages during its grace period when they have not turned two-factor authentication on: what is required, by when, and a link to set it up. It can be put away for the day; once the grace period is over the server refuses them and the pages send them to set it up instead.
 */
export const TwoFactorReminder = ({ organization }: { organization: OrganizationDetail }) => {
  const { t, i18n } = useTranslation();
  const [dismissed, setDismissed] = useState(() => readStorage(reminderKey(organization.id)) === today());
  if (dismissed || organization.twoFactorBlocked || !mustTurnOnTwoFactor(organization)) return null;
  const enforcedAt = organization.twoFactorEnforcedAt ?? "";
  const dismiss = () => {
    writeStorage(reminderKey(organization.id), today());
    setDismissed(true);
  };
  return (
    <div className="pointer-events-none fixed inset-0 z-100 flex items-end justify-end px-4 py-6 sm:p-6">
      <section
        aria-labelledby="two-factor-reminder-title"
        className="pointer-events-auto relative flex w-full max-w-sm rounded-lg bg-white p-4 shadow-lg ring-1 ring-black/5">
        <ShieldAlertIcon className="size-6 shrink-0 text-amber-500" aria-hidden="true" />
        <div className="ml-3 w-0 flex-1 pr-6">
          <h2 id="two-factor-reminder-title" className="text-base font-medium text-slate-900">
            {t("organizations.two_factor.reminder_title")}
          </h2>
          <p className="mt-1 text-sm text-slate-600">
            {t("organizations.two_factor.reminder_body", {
              organization: organization.name,
              date: formatDate(i18n.language, enforcedAt),
            })}
          </p>
          <Link
            to="/organizations/$organizationId/two-factor"
            params={{ organizationId: organization.id }}
            className="mt-2 inline-block text-sm font-medium text-slate-900 underline">
            {t("organizations.two_factor.reminder_action")}
          </Link>
        </div>
        <button
          type="button"
          className="absolute top-4 right-4 inline-flex rounded-md bg-white text-slate-400 hover:text-slate-500 focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2 focus:outline-hidden"
          onClick={dismiss}>
          <span className="sr-only">{t("common.close")}</span>
          <XIcon className="size-5" aria-hidden="true" />
        </button>
      </section>
    </div>
  );
};
