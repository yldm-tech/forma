import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { UserCheckIcon, UserXIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { UserDropdown } from "../../components/shell/user-dropdown";
import { Button } from "../../components/ui/button";
import { Wordmark } from "../../components/ui/logo";
import { sortByName } from "../../lib/navigation";
import { meQuery } from "../../lib/queries/auth";
import { organizationQuery, organizationsQuery } from "../../lib/queries/organizations";
import { LeaveOrganizationDialog } from "./leave-organization-dialog";

/**
 * Where a member lands when an owner or manager of the organization suspended their access: from the organization's routes, from any of its pages the server refuses them, and from the root page when every organization of theirs suspended them. It names the organization and says who can restore the access, and leaves a way out: leaving the organization (not for owners, nor for a member for whom it is the only one), the member's other organizations and, in the account menu, their profile and signing out. Once the access is restored it leads back into the organization.
 */
export const MembershipSuspendedPage = ({ organizationId }: { organizationId: string }) => {
  const { t } = useTranslation();
  const { data: me } = useSuspenseQuery(meQuery);
  const { data: organization } = useSuspenseQuery(organizationQuery(organizationId));
  const organizations = useQuery(organizationsQuery);
  const [leaving, setLeaving] = useState(false);
  const suspended = !!organization.membership.suspendedAt;
  const StatusIcon = suspended ? UserXIcon : UserCheckIcon;
  const others = sortByName((organizations.data ?? []).filter((o) => o.id !== organizationId));
  const usable = others.filter((o) => !o.suspendedAt);
  // The server refuses an owner's leaving and the leaving of a member's only organization; the button is not offered for either.
  const canLeave = suspended && organization.membership.role !== "owner" && others.length > 0;

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
          <StatusIcon className="size-10 text-slate-700" strokeWidth={1.5} aria-hidden="true" />
          <h1 className="text-2xl font-bold text-slate-900">
            {suspended
              ? t("organizations.suspended.page_title", { organization: organization.name })
              : t("organizations.suspended.page_restored_title", { organization: organization.name })}
          </h1>
          <p role={suspended ? undefined : "status"} className="text-base text-slate-600">
            {suspended
              ? t("organizations.suspended.page_description", { organization: organization.name })
              : t("organizations.suspended.page_restored_description")}
          </p>
        </div>
        <div className="flex flex-wrap gap-3">
          {suspended ? (
            canLeave && (
              <Button variant="secondary" onClick={() => setLeaving(true)}>
                {t("workspace.settings.general.leave_organization")}
              </Button>
            )
          ) : (
            <Button asChild>
              <Link to="/organizations/$organizationId" params={{ organizationId }}>
                {t("organizations.suspended.page_continue", { organization: organization.name })}
              </Link>
            </Button>
          )}
        </div>
        {suspended && usable.length > 0 && (
          <nav aria-labelledby="suspended-other-organizations" className="space-y-2">
            <h2 id="suspended-other-organizations" className="text-sm font-semibold text-slate-700">
              {t("organizations.suspended.page_other_organizations")}
            </h2>
            <ul className="space-y-1">
              {usable.map((other) => (
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
      {canLeave && (
        <LeaveOrganizationDialog organizationId={organization.id} open={leaving} onOpenChange={setLeaving} />
      )}
    </div>
  );
};
