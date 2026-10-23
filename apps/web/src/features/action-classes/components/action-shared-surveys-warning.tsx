import { useQuery } from "@tanstack/react-query";
import { useEffect } from "react";
import { toast } from "../../../lib/toast";
import { useTranslation } from "react-i18next";
import { errorMessage } from "../../../lib/errors";
import { Alert, AlertDescription, AlertTitle } from "../../../components/ui/alert";
import { actionClassSurveysQuery } from "../api";

interface ActionSharedSurveysWarningProps {
  actionClassId: string;
  currentSurveyId: string;
}

export const ActionSharedSurveysWarning = ({
  actionClassId,
  currentSurveyId,
}: Readonly<ActionSharedSurveysWarningProps>) => {
  const { t } = useTranslation();
  const { data, error } = useQuery(actionClassSurveysQuery(actionClassId, currentSurveyId));
  const otherSurveyNames = data ? [...data.activeSurveys, ...data.inactiveSurveys] : null;

  useEffect(() => {
    if (error) toast.error(errorMessage(t, error));
  }, [error, t]);

  if (!otherSurveyNames?.length) {
    return null;
  }

  return (
    <Alert variant="warning">
      <AlertTitle>{t("workspace.actions.edit_shared_action_warning_title")}</AlertTitle>
      <AlertDescription>
        {t("workspace.actions.edit_shared_action_warning_description", {
          surveys: otherSurveyNames.join(", "),
        })}
      </AlertDescription>
    </Alert>
  );
};
