"use server";

import { z } from "zod";
import { ZId } from "@forma/types/common";
import { OperationNotAllowedError, ResourceNotFoundError } from "@forma/types/errors";
import { assertCan } from "@/lib/authorization";
import { getOrganization } from "@/lib/organization/service";
import { authenticatedActionClient } from "@/lib/utils/action-client";
import { withAuditLogging } from "@/modules/audit-logs/lib/handler";
import { applyRateLimit } from "@/modules/core/rate-limit/helpers";
import { rateLimitConfigs } from "@/modules/core/rate-limit/rate-limit-configs";
import { sendEmailCustomizationPreviewEmail } from "@/modules/email";
import { getWhiteLabelPermission } from "@/modules/license-check/lib/utils";
import {
  removeOrganizationEmailLogoUrl,
  updateOrganizationEmailLogoUrl,
} from "@/modules/whitelabel/email-customization/lib/organization";

export const checkWhiteLabelPermission = async (organizationId: string) => {
  const organization = await getOrganization(organizationId);

  if (!organization) {
    throw new ResourceNotFoundError("Organization", organizationId);
  }

  const isWhiteLabelAllowed = await getWhiteLabelPermission(organizationId);

  if (!isWhiteLabelAllowed) {
    throw new OperationNotAllowedError("White label is not allowed for this organization");
  }
};

const ZUpdateOrganizationEmailLogoUrlAction = z.object({
  organizationId: ZId,
  logoUrl: z.string(),
});

export const updateOrganizationEmailLogoUrlAction = authenticatedActionClient
  .inputSchema(ZUpdateOrganizationEmailLogoUrlAction)
  .action(
    withAuditLogging("updated", "organization", async ({ ctx, parsedInput }) => {
      await assertCan({ type: "user", id: ctx.user.id }, "organization.manage", {
        type: "organization",
        id: parsedInput.organizationId,
      });
      await applyRateLimit(rateLimitConfigs.actions.stateMutation, parsedInput.organizationId);

      await checkWhiteLabelPermission(parsedInput.organizationId);
      ctx.auditLoggingCtx.organizationId = parsedInput.organizationId;
      ctx.auditLoggingCtx.newObject = { logoUrl: parsedInput.logoUrl };
      return await updateOrganizationEmailLogoUrl(parsedInput.organizationId, parsedInput.logoUrl);
    })
  );

const ZRemoveOrganizationEmailLogoUrlAction = z.object({
  organizationId: ZId,
});

export const removeOrganizationEmailLogoUrlAction = authenticatedActionClient
  .inputSchema(ZRemoveOrganizationEmailLogoUrlAction)
  .action(
    withAuditLogging("updated", "organization", async ({ ctx, parsedInput }) => {
      await assertCan({ type: "user", id: ctx.user.id }, "organization.manage", {
        type: "organization",
        id: parsedInput.organizationId,
      });
      await applyRateLimit(rateLimitConfigs.actions.stateMutation, parsedInput.organizationId);

      await checkWhiteLabelPermission(parsedInput.organizationId);
      ctx.auditLoggingCtx.organizationId = parsedInput.organizationId;
      ctx.auditLoggingCtx.oldObject = { logoUrl: "" };
      return await removeOrganizationEmailLogoUrl(parsedInput.organizationId);
    })
  );

const ZSendTestEmailAction = z.object({
  organizationId: ZId,
});

export const sendTestEmailAction = authenticatedActionClient
  .inputSchema(ZSendTestEmailAction)
  .action(async ({ ctx, parsedInput }) => {
    const organization = await getOrganization(parsedInput.organizationId);

    if (!organization) {
      throw new ResourceNotFoundError("Organization", parsedInput.organizationId);
    }

    await assertCan({ type: "user", id: ctx.user.id }, "organization.manage", {
      type: "organization",
      id: organization.id,
    });

    await checkWhiteLabelPermission(organization.id);

    await sendEmailCustomizationPreviewEmail(
      ctx.user.email,
      ctx.user.name,
      ctx.user.locale,
      organization?.whitelabel?.logoUrl || ""
    );

    return { success: true };
  });
