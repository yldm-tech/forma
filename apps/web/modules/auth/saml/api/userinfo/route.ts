import { responses } from "@/lib/api/response";
import { extractAuthToken } from "@/modules/auth/saml/api/userinfo/lib/utils";
import jackson from "@/modules/auth/saml/lib/jackson";

export const GET = async (req: Request) => {
  const jacksonInstance = await jackson();
  if (!jacksonInstance) {
    return responses.forbiddenResponse("SAML SSO is not enabled in your Forma license");
  }
  const { oauthController } = jacksonInstance;
  const token = extractAuthToken(req);

  const user = await oauthController.userInfo(token);

  return Response.json(user);
};
