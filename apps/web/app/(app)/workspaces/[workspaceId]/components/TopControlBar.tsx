"use client";

import { TOrganizationRole } from "@forma/types/memberships";
import { TUser } from "@forma/types/user";
import { WorkspaceAndOrgSwitch } from "@/app/(app)/workspaces/[workspaceId]/components/workspace-and-org-switch";
import { UserDropdown } from "@/modules/settings/components/user-dropdown";
import { useWorkspaceContext } from "@/modules/workspaces/context/workspace-context";

interface TopControlBarProps {
  user: TUser;
  publicDomain: string;
  currentOrganizationId: string;
  isMultiOrgEnabled: boolean;
  organizationWorkspacesLimit: number;
  isFormaCloud: boolean;
  isLicenseActive: boolean;
  isOwnerOrManager: boolean;
  isAccessControlAllowed: boolean;
  membershipRole?: TOrganizationRole;
  // False on the workspace-agnostic settings routes, where no workspace is in scope.
  showWorkspaceBreadcrumb?: boolean;
}

export const TopControlBar = ({
  user,
  publicDomain,
  currentOrganizationId,
  isMultiOrgEnabled,
  organizationWorkspacesLimit,
  isFormaCloud,
  isLicenseActive,
  isOwnerOrManager,
  isAccessControlAllowed,
  membershipRole,
  showWorkspaceBreadcrumb = true,
}: Readonly<TopControlBarProps>) => {
  const { workspace } = useWorkspaceContext();
  const isMembershipPending = membershipRole === undefined;

  return (
    <div
      className="flex h-14 w-full items-center justify-between bg-slate-50 px-6"
      data-testid="fb__global-top-control-bar">
      <WorkspaceAndOrgSwitch
        currentWorkspaceId={workspace.id}
        currentOrganizationId={currentOrganizationId}
        isMultiOrgEnabled={isMultiOrgEnabled}
        organizationWorkspacesLimit={organizationWorkspacesLimit}
        isFormaCloud={isFormaCloud}
        isLicenseActive={isLicenseActive}
        isOwnerOrManager={isOwnerOrManager}
        isMembershipPending={isMembershipPending}
        isAccessControlAllowed={isAccessControlAllowed}
        showWorkspaceBreadcrumb={showWorkspaceBreadcrumb}
      />

      {/* The account menu sits here rather than at the foot of the sidebar: it is the one control
          that belongs to the person rather than to whatever they are looking at, and the top right
          is where that is looked for. */}
      <UserDropdown
        user={user}
        organizationId={currentOrganizationId}
        publicDomain={publicDomain}
        isBilling={membershipRole === "billing"}
        placement="topBar"
      />
    </div>
  );
};
