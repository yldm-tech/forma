import { DatabaseError } from "@forma/types/errors";
import { responses } from "@/lib/api/response";
import { THandlerParams, withV1ApiWrapper } from "@/lib/api/with-api-logging";
import { getContactAttributes } from "./lib/contact-attributes";

export const GET = withV1ApiWrapper({
  handler: async ({ authentication }: THandlerParams) => {
    if (!authentication || !("apiKeyId" in authentication)) {
      return { response: responses.notAuthenticatedResponse() };
    }

    try {
      const workspaceIds = [
        ...new Set(authentication.workspacePermissions.map((permission) => permission.workspaceId)),
      ];

      const attributes = await getContactAttributes(workspaceIds);
      return {
        response: responses.successResponse(attributes),
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
