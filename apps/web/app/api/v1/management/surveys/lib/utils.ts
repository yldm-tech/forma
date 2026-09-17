import { TSurveyCreateInputWithWorkspaceId } from "@forma/types/surveys/types";
import { responses } from "@/lib/api/response";
import { getIsSpamProtectionEnabled } from "@/modules/license-check/lib/utils";

export const checkFeaturePermissions = async (
  surveyData: TSurveyCreateInputWithWorkspaceId
): Promise<Response | null> => {
  if (surveyData.recaptcha?.enabled) {
    const isSpamProtectionEnabled = await getIsSpamProtectionEnabled();
    if (!isSpamProtectionEnabled) {
      return responses.forbiddenResponse("Spam protection is not enabled for this organization");
    }
  }

  return null;
};
