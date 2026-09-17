"use server";

import { z } from "zod";
import { ZId, ZStorageUrl } from "@forma/types/common";
import { assertCan } from "@/lib/authorization";
import { authenticatedActionClient } from "@/lib/utils/action-client";
import { withAuditLogging } from "@/modules/audit-logs/lib/handler";
import { applyRateLimit } from "@/modules/core/rate-limit/helpers";
import { rateLimitConfigs } from "@/modules/core/rate-limit/rate-limit-configs";
import { checkWhiteLabelPermission } from "@/modules/whitelabel/email-customization/actions";
import { updateOrganizationFaviconUrl } from "@/modules/whitelabel/favicon-customization/lib/organization";

const ZUpdateOrganizationFaviconUrlAction = z.object({
  organizationId: ZId,
  faviconUrl: ZStorageUrl,
});

export const updateOrganizationFaviconUrlAction = authenticatedActionClient
  .inputSchema(ZUpdateOrganizationFaviconUrlAction)
  .action(
    withAuditLogging("updated", "organization", async ({ ctx, parsedInput }) => {
      const { organizationId, faviconUrl } = parsedInput;

      await assertCan({ type: "user", id: ctx.user.id }, "organization.manage", {
        type: "organization",
        id: organizationId,
      });
      await applyRateLimit(rateLimitConfigs.actions.stateMutation, organizationId);

      await checkWhiteLabelPermission(organizationId);

      ctx.auditLoggingCtx.organizationId = organizationId;
      ctx.auditLoggingCtx.newObject = { faviconUrl };

      return await updateOrganizationFaviconUrl(organizationId, faviconUrl);
    })
  );

const ZRemoveOrganizationFaviconUrlAction = z.object({
  organizationId: ZId,
});

export const removeOrganizationFaviconUrlAction = authenticatedActionClient
  .inputSchema(ZRemoveOrganizationFaviconUrlAction)
  .action(
    withAuditLogging("updated", "organization", async ({ ctx, parsedInput }) => {
      const { organizationId } = parsedInput;

      await assertCan({ type: "user", id: ctx.user.id }, "organization.manage", {
        type: "organization",
        id: organizationId,
      });
      await applyRateLimit(rateLimitConfigs.actions.stateMutation, organizationId);

      await checkWhiteLabelPermission(organizationId);

      ctx.auditLoggingCtx.organizationId = organizationId;
      ctx.auditLoggingCtx.oldObject = { faviconUrl: "" };

      return await updateOrganizationFaviconUrl(organizationId, null);
    })
  );
