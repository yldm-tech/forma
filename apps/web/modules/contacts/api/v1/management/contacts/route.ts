import { DatabaseError } from "@forma/types/errors";
import { responses } from "@/lib/api/response";
import { withV1ApiWrapper } from "@/lib/api/with-api-logging";
import { getContacts } from "./lib/contacts";

const readPositiveInt = (raw: string | null): number | undefined => {
  if (raw === null) return undefined;
  const parsed = Number(raw);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : undefined;
};

export const GET = withV1ApiWrapper({
  handler: async ({ authentication, req }) => {
    if (!authentication || !("apiKeyId" in authentication)) {
      return { response: responses.notAuthenticatedResponse() };
    }

    try {
      const workspaceIds = [
        ...new Set(authentication.workspacePermissions.map((permission) => permission.workspaceId)),
      ];

      const searchParams = new URL(req.url).searchParams;
      const contacts = await getContacts(
        workspaceIds,
        readPositiveInt(searchParams.get("limit")),
        readPositiveInt(searchParams.get("skip"))
      );

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
