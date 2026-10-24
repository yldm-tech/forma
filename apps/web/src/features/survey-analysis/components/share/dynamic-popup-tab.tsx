import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Alert, AlertButton, AlertDescription, AlertTitle } from "../../../../components/ui/alert";
import { DocumentationLinks } from "./documentation-links";

/** forma2's DynamicPopupTab: a link survey can also pop up on a website, set up in the editor. */
export const DynamicPopupTab = ({ surveyId, workspaceId }: { surveyId: string; workspaceId: string }) => {
  const { t } = useTranslation();
  return (
    <div className="flex h-full flex-col justify-between gap-y-4" data-testid="dynamic-popup-container">
      <Alert variant="info" role="status">
        <AlertTitle>{t("workspace.surveys.share.dynamic_popup.alert_title")}</AlertTitle>
        <AlertDescription>{t("workspace.surveys.share.dynamic_popup.alert_description")}</AlertDescription>
        <AlertButton asChild>
          <Link to="/workspaces/$workspaceId/surveys/$surveyId/edit" params={{ workspaceId, surveyId }}>
            {t("workspace.surveys.share.dynamic_popup.alert_button")}
          </Link>
        </AlertButton>
      </Alert>
      <DocumentationLinks
        links={[
          {
            title: t("workspace.surveys.share.dynamic_popup.attribute_based_targeting"),
            href: "https://forma.yldm.ai/docs/surveys/website-app-surveys/advanced-targeting",
          },
          {
            title: t("workspace.surveys.share.dynamic_popup.code_no_code_triggers"),
            href: "https://forma.yldm.ai/docs/surveys/website-app-surveys/actions",
          },
          {
            title: t("workspace.surveys.share.dynamic_popup.recontact_options"),
            href: "https://forma.yldm.ai/docs/surveys/website-app-surveys/recontact",
          },
        ]}
      />
    </div>
  );
};
