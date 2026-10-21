import type { Me } from "../../lib/queries/auth";
import { Breadcrumb, BreadcrumbList } from "../ui/breadcrumb";
import { OrganizationBreadcrumb } from "./organization-breadcrumb";
import { UserDropdown } from "./user-dropdown";
import { WorkspaceBreadcrumb } from "./workspace-breadcrumb";

export interface ShellOrganization {
  id: string;
  name: string;
}

/** forma2's TopControlBar: the organization and (inside a workspace) workspace switchers on the left, the user menu on the right. */
export const TopControlBar = ({
  user,
  organization,
  workspace,
  membershipRole,
}: {
  user: Me;
  organization: ShellOrganization;
  /** Absent on the organization and account settings pages, which belong to no workspace. */
  workspace?: { id: string; name: string };
  membershipRole?: string;
}) => (
  <div className="flex h-14 w-full items-center justify-between bg-slate-50 px-6">
    <Breadcrumb>
      <BreadcrumbList className="gap-0">
        <OrganizationBreadcrumb
          organizationId={organization.id}
          organizationName={organization.name}
          isLastCrumb={!workspace}
        />
        {workspace && (
          <WorkspaceBreadcrumb
            workspaceId={workspace.id}
            workspaceName={workspace.name}
            organizationId={organization.id}
            isOwnerOrManager={membershipRole === "owner" || membershipRole === "manager"}
            isMembershipPending={membershipRole === undefined}
          />
        )}
      </BreadcrumbList>
    </Breadcrumb>
    <UserDropdown user={user} organizationId={organization.id} isBilling={membershipRole === "billing"} />
  </div>
);
