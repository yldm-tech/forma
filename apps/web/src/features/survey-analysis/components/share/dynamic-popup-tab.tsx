import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Alert, AlertButton, AlertDescription, AlertTitle } from "../../../../components/ui/alert";

/**
 * forma2's DynamicPopupTab: a survey that pops up on its own, for the visitors and the actions its targeting picks, is an in-app survey, set up in the editor. A link survey can still open over any page as a popup, slider or side tab through the website embed; onUsePopupEmbed, when given, goes there.
 */
export const DynamicPopupTab = ({
  surveyId,
  workspaceId,
  onUsePopupEmbed,
}: {
  surveyId: string;
  workspaceId: string;
  onUsePopupEmbed?: () => void;
}) => {
  const { t } = useTranslation();
  return (
    <div className="flex h-full flex-col gap-y-4" data-testid="dynamic-popup-container">
      {onUsePopupEmbed && (
        <Alert variant="default" role="status">
          <AlertTitle>{t("workspace.surveys.share.dynamic_popup.embed_popup_title")}</AlertTitle>
          <AlertDescription>
            {t("workspace.surveys.share.dynamic_popup.embed_popup_description")}
          </AlertDescription>
          <AlertButton onClick={onUsePopupEmbed}>
            {t("workspace.surveys.share.dynamic_popup.embed_popup_button")}
          </AlertButton>
        </Alert>
      )}
      <Alert variant="info" role="status">
        <AlertTitle>{t("workspace.surveys.share.dynamic_popup.alert_title")}</AlertTitle>
        <AlertDescription>{t("workspace.surveys.share.dynamic_popup.alert_description")}</AlertDescription>
        <AlertButton asChild>
          <Link to="/workspaces/$workspaceId/surveys/$surveyId/edit" params={{ workspaceId, surveyId }}>
            {t("workspace.surveys.share.dynamic_popup.alert_button")}
          </Link>
        </AlertButton>
      </Alert>
    </div>
  );
};
