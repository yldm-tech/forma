"use server";

import { z } from "zod";
import { ZId } from "@forma/types/common";
import { ZContactAttributeDataType } from "@forma/types/contact-attribute-key";
import { ResourceNotFoundError } from "@forma/types/errors";
import { isSafeIdentifier } from "@forma/types/safe-identifier";
import { assertCan } from "@/lib/authorization";
import { capturePostHogEvent } from "@/lib/posthog";
import { authenticatedActionClient } from "@/lib/utils/action-client";
import { getOrganizationIdFromWorkspaceId } from "@/lib/utils/helper";
import { withAuditLogging } from "@/modules/audit-logs/lib/handler";
import {
  RESERVED_FUTURE_DEFAULT_ATTRIBUTE_KEY_VALIDATION_MESSAGE,
  isReservedFutureDefaultAttributeKey,
} from "@/modules/contacts/lib/attribute-key-policy";
import {
  createContactAttributeKey,
  deleteContactAttributeKey,
  getContactAttributeKeyById,
  updateContactAttributeKey,
} from "@/modules/contacts/lib/contact-attribute-keys";
import { applyRateLimit } from "@/modules/core/rate-limit/helpers";
import { rateLimitConfigs } from "@/modules/core/rate-limit/rate-limit-configs";

const ZCreateContactAttributeKeyAction = z.object({
  workspaceId: ZId,
  key: z
    .string()
    .refine((val) => isSafeIdentifier(val), {
      error:
        "Key must be a safe identifier: only lowercase letters, numbers, and underscores, and must start with a letter",
    })
    .refine((val) => !isReservedFutureDefaultAttributeKey(val), {
      error: RESERVED_FUTURE_DEFAULT_ATTRIBUTE_KEY_VALIDATION_MESSAGE,
    }),
  name: z.string().optional(),
  description: z.string().optional(),
  dataType: ZContactAttributeDataType.optional(),
});

export const createContactAttributeKeyAction = authenticatedActionClient
  .inputSchema(ZCreateContactAttributeKeyAction)
  .action(
    withAuditLogging("created", "contactAttributeKey", async ({ ctx, parsedInput }) => {
      const workspaceId = parsedInput.workspaceId;
      const organizationId = await getOrganizationIdFromWorkspaceId(workspaceId);

      await assertCan({ type: "user", id: ctx.user.id }, "workspace.write", {
        type: "workspace",
        id: workspaceId,
      });
      await applyRateLimit(rateLimitConfigs.actions.stateMutation, workspaceId);

      ctx.auditLoggingCtx.organizationId = organizationId;

      const contactAttributeKey = await createContactAttributeKey({
        workspaceId,
        key: parsedInput.key,
        name: parsedInput.name,
        description: parsedInput.description,
        dataType: parsedInput.dataType,
      });

      ctx.auditLoggingCtx.newObject = contactAttributeKey;

      capturePostHogEvent(
        ctx.user.id,
        "contact_attribute_key_created",
        {
          organization_id: organizationId,
          workspace_id: workspaceId,
          key: parsedInput.key,
        },
        { organizationId, workspaceId }
      );

      return contactAttributeKey;
    })
  );

const ZUpdateContactAttributeKeyAction = z.object({
  id: ZId,
  name: z.string().optional(),
  description: z.string().optional(),
});
export const updateContactAttributeKeyAction = authenticatedActionClient
  .inputSchema(ZUpdateContactAttributeKeyAction)
  .action(
    withAuditLogging("updated", "contactAttributeKey", async ({ ctx, parsedInput }) => {
      // Fetch existing key to check authorization
      const existingKey = await getContactAttributeKeyById(parsedInput.id);

      if (!existingKey) {
        throw new ResourceNotFoundError("contactAttributeKey", parsedInput.id);
      }

      const workspaceId = existingKey.workspaceId;
      const organizationId = await getOrganizationIdFromWorkspaceId(workspaceId);

      await assertCan({ type: "user", id: ctx.user.id }, "workspace.write", {
        type: "workspace",
        id: workspaceId,
      });
      await applyRateLimit(rateLimitConfigs.actions.stateMutation, workspaceId);

      ctx.auditLoggingCtx.organizationId = organizationId;
      ctx.auditLoggingCtx.oldObject = existingKey;

      const updatedKey = await updateContactAttributeKey(parsedInput.id, {
        name: parsedInput.name,
        description: parsedInput.description,
      });

      ctx.auditLoggingCtx.newObject = updatedKey;

      return updatedKey;
    })
  );

const ZDeleteContactAttributeKeyAction = z.object({
  id: ZId,
});
export const deleteContactAttributeKeyAction = authenticatedActionClient
  .inputSchema(ZDeleteContactAttributeKeyAction)
  .action(
    withAuditLogging("deleted", "contactAttributeKey", async ({ ctx, parsedInput }) => {
      // Fetch existing key to check authorization
      const existingKey = await getContactAttributeKeyById(parsedInput.id);

      if (!existingKey) {
        throw new ResourceNotFoundError("contactAttributeKey", parsedInput.id);
      }

      const workspaceId = existingKey.workspaceId;
      const organizationId = await getOrganizationIdFromWorkspaceId(workspaceId);

      await assertCan({ type: "user", id: ctx.user.id }, "workspace.write", {
        type: "workspace",
        id: workspaceId,
      });
      await applyRateLimit(rateLimitConfigs.actions.stateMutation, workspaceId);

      ctx.auditLoggingCtx.organizationId = organizationId;
      ctx.auditLoggingCtx.oldObject = existingKey;

      const deletedKey = await deleteContactAttributeKey(parsedInput.id);

      return deletedKey;
    })
  );
