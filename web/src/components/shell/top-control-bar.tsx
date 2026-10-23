import { useQuery } from "@tanstack/react-query";
import type { Me } from "../../lib/queries/auth";
import { billingSummaryQuery } from "../../lib/queries/billing";
import { Breadcrumb, BreadcrumbList } from "../ui/breadcrumb";
import { CommandPalette } from "./command-palette";
import { OrganizationBreadcrumb } from "./organization-breadcrumb";
import { UserDropdown } from "./user-dropdown";
import { WorkspaceBreadcrumb } from "./workspace-breadcrumb";

export interface ShellOrganization {
  id: string;
  name: string;
}

/** forma2's TopControlBar: the organization and (inside a workspace) workspace switchers on the left; on the right the command palette, scoped to the current workspace, and the user menu. */
export const TopControlBar = ({
  user,
  organization,
  workspace,
  membershipRole,
  showWorkspaceBreadcrumb = true,
}: {
  user: Me;
  organization: ShellOrganization;
  /** The workspace the palette's destinations belong to. Absent for a user with no workspace in the organization. */
  workspace?: { id: string; name: string };
  membershipRole?: string;
  /** False on the organization and account settings pages, whose address names no workspace: the palette still uses the current one, but the top bar does not claim it. */
  showWorkspaceBreadcrumb?: boolean;
}) => {
  const isOwnerOrManager = membershipRole === "owner" || membershipRole === "manager";
  const { data: billing } = useQuery(billingSummaryQuery(organization.id));
  const breadcrumbWorkspace = showWorkspaceBreadcrumb ? workspace : undefined;
  return (
    <div className="flex h-14 w-full items-center justify-between bg-slate-50 px-6">
      <Breadcrumb>
        <BreadcrumbList className="gap-0">
          <OrganizationBreadcrumb
            organizationId={organization.id}
            organizationName={organization.name}
            isLastCrumb={!breadcrumbWorkspace}
          />
          {breadcrumbWorkspace && (
            <WorkspaceBreadcrumb
              workspaceId={breadcrumbWorkspace.id}
              workspaceName={breadcrumbWorkspace.name}
              organizationId={organization.id}
              isOwnerOrManager={isOwnerOrManager}
              isMembershipPending={membershipRole === undefined}
            />
          )}
        </BreadcrumbList>
      </Breadcrumb>
      <div className="flex items-center gap-3">
        {/* Everywhere in the product, one keystroke away: the sidebar shows only the top areas, and this reaches the rest. */}
        {workspace && (
          <CommandPalette
            workspaceId={workspace.id}
            organizationId={organization.id}
            isBilling={membershipRole === "billing"}
            isOwnerOrManager={isOwnerOrManager}
            isFormaCloud={billing?.isFormaCloud ?? false}
          />
        )}
        <UserDropdown user={user} organizationId={organization.id} isBilling={membershipRole === "billing"} />
      </div>
    </div>
  );
};
