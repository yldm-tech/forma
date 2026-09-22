import { can } from "@/lib/authorization";
import { getWorkspaceAuthorizationActionForMethod } from "@/lib/authorization/permission-action";
import { authenticatedApiClient } from "@/modules/api/v2/auth/authenticated-api-client";
import { responses } from "@/modules/api/v2/lib/response";
import { handleApiError } from "@/modules/api/v2/lib/utils";
import { resolveBodyIdsV2 } from "@/modules/api/v2/management/lib/workspace-resolver";
import { upsertBulkContacts } from "@/modules/contacts/api/v2/management/contacts/bulk/lib/contact";
import { ZContactBulkUploadRequest } from "@/modules/contacts/types/contact";

export const PUT = async (request: Request) =>
  authenticatedApiClient({
    request,
    schemas: {
      body: ZContactBulkUploadRequest,
    },
    bodyTransform: async (body, auth) => {
      const resolved = await resolveBodyIdsV2(body, auth, "PUT");
      if (!resolved.ok) throw resolved.error;
      return { ...body, ...resolved.data };
    },
    handler: async ({ authentication, parsedInput, auditLog }) => {
      const workspaceId = parsedInput.body?.workspaceId;

      if (!workspaceId) {
        return handleApiError(
          request,
          {
            type: "bad_request",
            details: [{ field: "workspaceId", issue: "missing" }],
          },
          auditLog
        );
      }

      const { contacts } = parsedInput.body ?? { contacts: [] };

      if (
        !(await can(
          { type: "apiKey", id: authentication.apiKeyId },
          getWorkspaceAuthorizationActionForMethod("PUT"),
          { type: "workspace", id: workspaceId }
        ))
      ) {
        return handleApiError(
          request,
          {
            type: "forbidden",
            details: [
              {
                field: "workspaceId",
                issue: "insufficient permissions to create contact in this workspace",
              },
            ],
          },
          auditLog
        );
      }

      const emails = contacts.map((contact) => {
        const email = contact.attributes.find((attr) => attr.attributeKey.key === "email")?.value;
        // `validateEmailAttribute` in the request schema rejects a contact without one, so this is
        // unreachable. It is here so a later schema change cannot quietly hand `undefined` to an upsert
        // whose parameter is typed `string[]`.
        if (email === undefined) {
          throw new Error("Contact passed schema validation without an email attribute");
        }
        return email;
      });

      const upsertBulkContactsResult = await upsertBulkContacts(contacts, workspaceId, emails);

      if (!upsertBulkContactsResult.ok) {
        return handleApiError(request, upsertBulkContactsResult.error, auditLog);
      }

      const { contactIdxWithConflictingUserIds } = upsertBulkContactsResult.data;

      if (contactIdxWithConflictingUserIds.length) {
        return responses.multiStatusResponse({
          data: {
            status: "success",
            message:
              "Contacts bulk upload partially successful. Some contacts were skipped due to conflicting userIds.",
            meta: {
              skippedContacts: contactIdxWithConflictingUserIds.map((idx) => ({
                index: idx,
                userId: contacts[idx].attributes.find((attr) => attr.attributeKey.key === "userId")?.value,
              })),
            },
          },
        });
      }

      return responses.successResponse({
        data: {
          status: "success",
          message: "Contacts bulk upload successful",
        },
      });
    },
    action: "bulkCreated",
    targetType: "contact",
  });
