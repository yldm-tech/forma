import { NextRequest } from "next/server";
import { can } from "@/lib/authorization";
import { getWorkspaceAuthorizationActionForMethod } from "@/lib/authorization/permission-action";
import { authenticatedApiClient } from "@/modules/api/v2/auth/authenticated-api-client";
import { responses } from "@/modules/api/v2/lib/response";
import { handleApiError } from "@/modules/api/v2/lib/utils";
import { resolveBodyIdsV2 } from "@/modules/api/v2/management/lib/workspace-resolver";
import { createContact } from "@/modules/contacts/api/v2/management/contacts/lib/contact";
import { ZContactCreateRequest } from "@/modules/contacts/types/contact";

export const POST = async (request: NextRequest) =>
  authenticatedApiClient({
    request,
    schemas: {
      body: ZContactCreateRequest,
    },
    bodyTransform: async (body, auth) => {
      const resolved = await resolveBodyIdsV2(body, auth, "POST");
      if (!resolved.ok) throw resolved.error;
      return { ...body, ...resolved.data };
    },

    handler: async ({ authentication, parsedInput, auditLog }) => {
      const { body } = parsedInput;

      const { workspaceId } = body;

      if (
        !(await can(
          { type: "apiKey", id: authentication.apiKeyId },
          getWorkspaceAuthorizationActionForMethod("POST"),
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

      const createContactResult = await createContact(body);

      if (!createContactResult.ok) {
        return handleApiError(request, createContactResult.error, auditLog);
      }

      const createdContact = createContactResult.data;

      if (auditLog) {
        auditLog.targetId = createdContact.id;
        auditLog.newObject = createdContact;
      }

      return responses.createdResponse(createContactResult);
    },
    action: "created",
    targetType: "contact",
  });
