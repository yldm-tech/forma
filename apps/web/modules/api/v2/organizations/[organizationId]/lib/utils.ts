import { logger } from "@forma/logger";
import { OrganizationAccessType } from "@forma/types/api-key";
import { TAuthenticationApiKey } from "@forma/types/auth";
import { can } from "@/lib/authorization";
import { getOrganizationAuthorizationActionForAccessType } from "@/lib/authorization/permission-action";

export const hasOrganizationIdAndAccess = async (
  paramOrganizationId: string,
  authentication: TAuthenticationApiKey,
  accessType: OrganizationAccessType
): Promise<boolean> => {
  if (paramOrganizationId !== authentication.organizationId) {
    logger.error("Organization ID from params does not match the authenticated organization ID");

    return false;
  }

  return can(
    { type: "apiKey", id: authentication.apiKeyId },
    getOrganizationAuthorizationActionForAccessType(accessType),
    { type: "organization", id: authentication.organizationId }
  );
};
