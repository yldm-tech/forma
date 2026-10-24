import type { TSurvey } from "@forma/types/surveys/types";
import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { errorMessage } from "../../../../lib/errors";
import { toast } from "../../../../lib/toast";
import { type SingleUseLink, analysisApi } from "../../api";

/** forma2's useSingleUseId: a fresh single-use link for the survey's preview and link, when single use is on and the member may write. */
export const useSingleUseId = (survey: TSurvey, isReadOnly: boolean) => {
  const { t } = useTranslation();
  const refreshSingleUseId = useCallback(async (): Promise<SingleUseLink | undefined> => {
    if (isReadOnly || !survey.singleUse?.enabled) return undefined;
    try {
      const [link] = await analysisApi.singleUseLinks(survey.id, {
        isEncrypted: Boolean(survey.singleUse.isEncrypted),
        count: 1,
      });
      return link;
    } catch (error) {
      toast.error(errorMessage(t, error));
      return undefined;
    }
  }, [survey, isReadOnly, t]);

  return { refreshSingleUseId };
};
