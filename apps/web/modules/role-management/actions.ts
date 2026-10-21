"use server";

import { z } from "zod";
import { prisma } from "@forma/database";
import { Prisma } from "@forma/database/prisma";
import { ZId, ZUuid } from "@forma/types/common";
import {
  AuthenticationError,
  OperationNotAllowedError,
  ResourceNotFoundError,
  ValidationError,
} from "@forma/types/errors";
import { ZMembershipUpdateInput } from "@forma/types/memberships";
import { assertCan } from "@/lib/authorization";
import { IS_FORMA_CLOUD } from "@/lib/constants";
import { getMembershipByUserIdOrganizationId } from "@/lib/membership/service";
import { getOrganization } from "@/lib/organization/service";
import { authenticatedActionClient } from "@/lib/utils/action-client";
import { getOrganizationIdFromInviteId } from "@/lib/utils/helper";
import { withAuditLogging } from "@/modules/audit-logs/lib/handler";
import { applyRateLimit } from "@/modules/core/rate-limit/helpers";
import { rateLimitConfigs } from "@/modules/core/rate-limit/rate-limit-configs";
import { getAccessControlPermission } from "@/modules/license-check/lib/utils";
import { getOrganizationOwnerCount } from "@/modules/organization/settings/teams/lib/membership";
import { assertCanManageOrganizationUsers } from "@/modules/organization/settings/teams/lib/user-management-access";
import { getInviteRole, updateInvite } from "@/modules/role-management/lib/invite";
import { updateMembership } from "@/modules/role-management/lib/membership";
import { ZInviteUpdateInput } from "@/modules/role-management/types/invites";

export const checkRoleManagementPermission = async (organizationId: string) => {
  const organization = await getOrganization(organizationId);
  if (!organization) {
    throw new ResourceNotFoundError("Organization", organizationId);
  }

  const isAccessControlAllowed = await getAccessControlPermission();
  if (!isAccessControlAllowed) {
    throw new OperationNotAllowedError("Role management is not allowed for this organization");
  }
};

const ZUpdateInviteAction = z.object({
  inviteId: ZUuid,
  data: ZInviteUpdateInput,
});

export type TUpdateInviteAction = z.infer<typeof ZUpdateInviteAction>;

export const updateInviteAction = authenticatedActionClient.inputSchema(ZUpdateInviteAction).action(
  withAuditLogging("updated", "invite", async ({ ctx, parsedInput }) => {
    const organizationId = await getOrganizationIdFromInviteId(parsedInput.inviteId);

    const currentUserMembership = await getMembershipByUserIdOrganizationId(ctx.user.id, organizationId);
    if (!currentUserMembership) {
      throw new AuthenticationError("User not a member of this organization");
    }

    await assertCanManageOrganizationUsers(ctx.user.id, organizationId);

    await assertCan({ type: "user", id: ctx.user.id }, "organization.manage", {
      type: "organization",
      id: organizationId,
    });
    await applyRateLimit(rateLimitConfigs.actions.stateMutation, organizationId);

    if (!IS_FORMA_CLOUD && parsedInput.data.role === "billing") {
      throw new ValidationError("Billing role is not allowed");
    }

    if (currentUserMembership.role === "manager" && parsedInput.data.role !== "member") {
      throw new OperationNotAllowedError("Managers can only invite members");
    }

    await checkRoleManagementPermission(organizationId);

    ctx.auditLoggingCtx.organizationId = organizationId;
    ctx.auditLoggingCtx.inviteId = parsedInput.inviteId;
    // `role` is the only field this action can change, so it is the only one the audit entry needs — and the only one the snapshots must actually contain, or the diff comes out empty and a privilege escalation leaves no trace.
    ctx.auditLoggingCtx.oldObject = { role: await getInviteRole(parsedInput.inviteId) };

    const result = await updateInvite(parsedInput.inviteId, parsedInput.data);

    ctx.auditLoggingCtx.newObject = { role: parsedInput.data.role };
    return result;
  })
);

const ZUpdateMembershipAction = z.object({
  userId: ZId,
  organizationId: ZId,
  data: ZMembershipUpdateInput,
});

export const updateMembershipAction = authenticatedActionClient.inputSchema(ZUpdateMembershipAction).action(
  withAuditLogging("updated", "membership", async ({ ctx, parsedInput }) => {
    const currentUserMembership = await getMembershipByUserIdOrganizationId(
      ctx.user.id,
      parsedInput.organizationId
    );
    if (!currentUserMembership) {
      throw new AuthenticationError("User not a member of this organization");
    }
    await assertCanManageOrganizationUsers(ctx.user.id, parsedInput.organizationId);

    await assertCan({ type: "user", id: ctx.user.id }, "organization.manage", {
      type: "organization",
      id: parsedInput.organizationId,
    });
    await applyRateLimit(rateLimitConfigs.actions.stateMutation, parsedInput.organizationId);

    if (!IS_FORMA_CLOUD && parsedInput.data.role === "billing") {
      throw new ValidationError("Billing role is not allowed");
    }

    if (currentUserMembership.role === "manager" && parsedInput.data.role !== "member") {
      throw new OperationNotAllowedError("Managers can only assign users to the member role");
    }

    const targetMembership = await getMembershipByUserIdOrganizationId(
      parsedInput.userId,
      parsedInput.organizationId
    );
    if (currentUserMembership.role !== "owner" && targetMembership?.role === "owner") {
      throw new OperationNotAllowedError("Only owners can change the role of an owner");
    }

    await checkRoleManagementPermission(parsedInput.organizationId);

    ctx.auditLoggingCtx.organizationId = parsedInput.organizationId;
    ctx.auditLoggingCtx.membershipId = `${parsedInput.userId}-${parsedInput.organizationId}`;
    ctx.auditLoggingCtx.oldObject = targetMembership;

    const isDemotingOwner = targetMembership?.role === "owner" && parsedInput.data.role !== "owner";

    // The owner count and the role update must be one atomic unit: read then act, in two separate
    // statements, lets two owners demoting each other concurrently both read "more than one owner"
    // and both writes land, leaving zero owners. Serializable isolation makes Postgres abort one of
    // the two transactions instead.
    const result = isDemotingOwner
      ? await prisma.$transaction(
          async (tx) => {
            const ownerCount = await getOrganizationOwnerCount(parsedInput.organizationId, tx);

            if (ownerCount <= 1) {
              throw new ValidationError("You cannot demote the last owner of the organization");
            }

            return updateMembership(parsedInput.userId, parsedInput.organizationId, parsedInput.data, tx);
          },
          { isolationLevel: Prisma.TransactionIsolationLevel.Serializable }
        )
      : await updateMembership(parsedInput.userId, parsedInput.organizationId, parsedInput.data);

    ctx.auditLoggingCtx.newObject = result;
    return result;
  })
);
