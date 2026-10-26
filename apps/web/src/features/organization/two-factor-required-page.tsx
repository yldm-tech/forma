import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ShieldCheckIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { UserDropdown } from "../../components/shell/user-dropdown";
import { Button } from "../../components/ui/button";
import { Wordmark } from "../../components/ui/logo";
import { sortByName } from "../../lib/navigation";
import { meQuery } from "../../lib/queries/auth";
import { organizationQuery, organizationsQuery } from "../../lib/queries/organizations";
import { EnableTwoFactorModal } from "../account/two-factor";
import { LeaveOrganizationDialog } from "./leave-organization-dialog";
import { mustTurnOnTwoFactor } from "./two-factor-requirement";

/**
 * Where a member is sent when their organization requires two-factor authentication they have not turned on: from the reminder during the grace period, and from every page of the organization once the server refuses them. It names the organization and the deadline, sets two-factor authentication up in place, and leaves a way out: leaving the organization (not for owners), the member's other organizations and, in the account menu, their profile and signing out. During the grace period the member may also go on to the organization for now.
 */
export const TwoFactorRequiredPage = ({ organizationId }: { organizationId: string }) => {
  const { t, i18n } = useTranslation();
  const { data: me } = useSuspenseQuery(meQuery);
  const { data: organization } = useSuspenseQuery(organizationQuery(organizationId));
  const organizations = useQuery(organizationsQuery);
  const [enrolling, setEnrolling] = useState(false);
  const [leaving, setLeaving] = useState(false);
  const others = sortByName((organizations.data ?? []).filter((o) => o.id !== organizationId));
  const required = mustTurnOnTwoFactor(organization);
  // A member who would rather not turn it on may leave instead, as from the members page; owners cannot leave.
  const canLeave = required && organization.membership.role !== "owner";
  const deadline = organization.twoFactorEnforcedAt
    ? new Intl.DateTimeFormat(i18n.language, { dateStyle: "long", timeStyle: "short" }).format(
        new Date(organization.twoFactorEnforcedAt)
      )
    : "";

  return (
    <div className="flex min-h-dvh flex-col bg-slate-50">
      <header className="flex items-center justify-between p-6">
        <Wordmark className="text-2xl" />
        <UserDropdown
          user={me}
          organizationId={organization.id}
          isBilling={organization.membership.role === "billing"}
        />
      </header>
      <main className="mx-auto flex w-full max-w-xl flex-1 flex-col justify-center gap-y-8 px-6 pb-16">
        <div className="space-y-3">
          <ShieldCheckIcon className="size-10 text-slate-700" strokeWidth={1.5} aria-hidden="true" />
          <h1 className="text-2xl font-bold text-slate-900">
            {t("organizations.two_factor.page_title", { organization: organization.name })}
          </h1>
          {required ? (
            <p className="text-base text-slate-600">
              {organization.twoFactorBlocked
                ? t("organizations.two_factor.page_blocked")
                : t("organizations.two_factor.page_grace", { date: deadline })}
            </p>
          ) : (
            <p role="status" className="text-base text-slate-600">
              {t("organizations.two_factor.page_done", { organization: organization.name })}
            </p>
          )}
        </div>
        <div className="flex flex-wrap gap-3">
          {required ? (
            <>
              <Button onClick={() => setEnrolling(true)}>{t("organizations.two_factor.page_setup")}</Button>
              {!organization.twoFactorBlocked && (
                <Button variant="secondary" asChild>
                  <Link to="/organizations/$organizationId" params={{ organizationId }}>
                    {t("organizations.two_factor.page_later")}
                  </Link>
                </Button>
              )}
              {canLeave && (
                <Button variant="ghost" onClick={() => setLeaving(true)}>
                  {t("workspace.settings.general.leave_organization")}
                </Button>
              )}
            </>
          ) : (
            <Button asChild>
              <Link to="/organizations/$organizationId" params={{ organizationId }}>
                {t("organizations.two_factor.page_continue", { organization: organization.name })}
              </Link>
            </Button>
          )}
        </div>
        {required && others.length > 0 && (
          <nav aria-labelledby="two-factor-other-organizations" className="space-y-2">
            <h2 id="two-factor-other-organizations" className="text-sm font-semibold text-slate-700">
              {t("organizations.two_factor.page_other_organizations")}
            </h2>
            <ul className="space-y-1">
              {others.map((other) => (
                <li key={other.id}>
                  <Link
                    to="/organizations/$organizationId"
                    params={{ organizationId: other.id }}
                    className="text-sm text-slate-700 underline">
                    {other.name}
                  </Link>
                </li>
              ))}
            </ul>
          </nav>
        )}
      </main>
      {enrolling && <EnableTwoFactorModal onClose={() => setEnrolling(false)} />}
      {canLeave && (
        <LeaveOrganizationDialog organizationId={organization.id} open={leaving} onOpenChange={setLeaving} />
      )}
    </div>
  );
};
