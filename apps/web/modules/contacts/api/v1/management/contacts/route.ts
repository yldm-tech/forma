import { DatabaseError } from "@forma/types/errors";
import { responses } from "@/lib/api/response";
import { withV1ApiWrapper } from "@/lib/api/with-api-logging";
import { getContacts } from "./lib/contacts";

export const GET = withV1ApiWrapper({
  handler: async ({ authentication }) => {
    if (!authentication || !("apiKeyId" in authentication)) {
      return { response: responses.notAuthenticatedResponse() };
    }

    try {
      const workspaceIds = [
        ...new Set(authentication.workspacePermissions.map((permission) => permission.workspaceId)),
      ];

      const contacts = await getContacts(workspaceIds);

      return {
        response: responses.successResponse(contacts),
      };
    } catch (error) {
      if (error instanceof DatabaseError) {
        return {
          response: responses.badRequestResponse(error.message),
        };
      }
      throw error;
    }
  },
});

// Please use the client API to create a new contact
