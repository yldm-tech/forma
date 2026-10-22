import { useTranslation } from "react-i18next";
import type { TSurveyFollowUp } from "@forma/types/surveys/follow-up";
import type { TSurvey } from "@forma/types/surveys/types";
import type { TUserLocale } from "@forma/types/user";
import { Alert, AlertButton, AlertDescription, AlertTitle } from "../../../../components/ui/alert";
import type { TFollowUpEmailToUser } from "../../types/survey-follow-up";
import { WORKFLOWS_DOCS_URL, formatSurveyFollowUpsSunsetDate } from "../lib/deprecation";
import { FollowUpItem } from "./follow-up-item";

interface FollowUpsViewProps {
  localSurvey: TSurvey;
  setLocalSurvey: React.Dispatch<React.SetStateAction<TSurvey>>;
  selectedLanguageCode: string;
  userEmail: string;
  teamMemberDetails: TFollowUpEmailToUser[];
  locale: TUserLocale;
}

/**
 * forma2's Follow-ups tab with Workflows available: follow-ups are deprecated, and a survey's existing ones can still be read, fixed and deleted. forma2 offered a "New workflow" button here; the Workflows pages are not ported to this app yet, so it is left out rather than leading nowhere, and the deprecation notice links to the Workflows documentation.
 */
export const FollowUpsView = ({
  localSurvey,
  setLocalSurvey,
  selectedLanguageCode,
  userEmail,
  teamMemberDetails,
  locale,
}: Readonly<FollowUpsViewProps>) => {
  const { t } = useTranslation();

  const surveyFollowUps: TSurveyFollowUp[] = localSurvey.followUps.filter((f) => !f.deleted);
  const sunsetDate = formatSurveyFollowUpsSunsetDate(locale);

  return (
    <div className="mt-12 space-y-4 p-5">
      <Alert variant="warning" role="status">
        <AlertTitle>{t("workspace.surveys.edit.follow_ups_deprecation_title")}</AlertTitle>
        <AlertDescription>
          {t("workspace.surveys.edit.follow_ups_deprecation_description", { date: sunsetDate })}
        </AlertDescription>
        <AlertButton asChild>
          <a href={WORKFLOWS_DOCS_URL} target="_blank" rel="noopener noreferrer">
            {t("common.learn_more")}
          </a>
        </AlertButton>
      </Alert>

      {surveyFollowUps.length > 0 && (
        <div className="flex flex-col gap-y-2">
          {surveyFollowUps.map((followUp) => (
            <FollowUpItem
              key={followUp.id}
              followUp={followUp}
              localSurvey={localSurvey}
              setLocalSurvey={setLocalSurvey}
              selectedLanguageCode={selectedLanguageCode}
              userEmail={userEmail}
              teamMemberDetails={teamMemberDetails}
              locale={locale}
            />
          ))}
        </div>
      )}
    </div>
  );
};
