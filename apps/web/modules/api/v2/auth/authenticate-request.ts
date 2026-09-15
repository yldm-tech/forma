import { TAuthenticationApiKey } from "@forma/types/auth";
import { Result, err, ok } from "@forma/types/error-handlers";
import {
  type AuthenticateApiKeyOptions,
  authenticateApiKeyFromHeaders,
} from "@/modules/api/lib/api-key-auth";
import { ApiErrorResponseV2 } from "@/modules/api/v2/types/api-error";

export const authenticateRequest = async (
  request: Request,
  options: AuthenticateApiKeyOptions = {}
): Promise<Result<TAuthenticationApiKey, ApiErrorResponseV2>> => {
  const authentication = await authenticateApiKeyFromHeaders(request.headers, options);
  if (!authentication) return err({ type: "unauthorized" });
  return ok(authentication);
};
