import type { TSurvey } from "@forma/types/surveys/types";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Confetti } from "../../../components/ui/confetti";
import type { WorkspaceDetail } from "../../../lib/queries/workspaces";
import { toast } from "../../../lib/toast";
import type { AnalysisSearch } from "./survey-analysis-cta";

/**
 * forma2's SuccessMessage: the editor hands a publish over with ?success=true and a schedule with ?scheduled=true. A publish gets the confetti and a toast, and a link survey's share modal opens on its start view (?share=true); a schedule gets its toast. Either param is removed once read, so a reload does not celebrate again.
 */
export const SuccessMessage = ({
  survey,
  workspace,
  search,
  setSearch,
}: {
  survey: TSurvey;
  workspace: WorkspaceDetail;
  search: AnalysisSearch;
  setSearch: (patch: Partial<AnalysisSearch>, replace: boolean) => void;
}) => {
  const { t } = useTranslation();
  const [confetti, setConfetti] = useState(false);
  const isAppSurvey = survey.type === "app";
  const appSetupCompleted = workspace.appSetupCompleted;
  const { success, scheduled } = search;

  useEffect(() => {
    if (scheduled) {
      toast.success(t("workspace.surveys.summary.survey_scheduled_successfully"), {
        id: "survey-schedule-success-toast",
      });
      setSearch({ scheduled: undefined }, true);
      return;
    }
    if (success) {
      setConfetti(true);
      toast.success(
        isAppSurvey && !appSetupCompleted
          ? t("workspace.surveys.summary.almost_there")
          : t("workspace.surveys.summary.congrats"),
        { id: "survey-publish-success-toast" }
      );
      setSearch({ success: undefined, ...(survey.type === "link" ? { share: true } : {}) }, true);
    }
    // setSearch is rebuilt every render; the effect answers the params, read once each.
    // oxlint-disable-next-line react/exhaustive-deps -- see above
  }, [success, scheduled]);

  return confetti ? <Confetti /> : null;
};
