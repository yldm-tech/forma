import { useTranslation } from "react-i18next";
import type { TSurveyFollowUp } from "@forma/types/surveys/follow-up";
import type { TSurvey } from "@forma/types/surveys/types";
import type { TUserLocale } from "@forma/types/user";
import { Alert, AlertDescription, AlertTitle } from "../../../../components/ui/alert";
import { Button } from "../../../../components/ui/button";
import type { TFollowUpEmailToUser } from "../../types/survey-follow-up";
import { formatSurveyFollowUpsSunsetDate } from "../lib/deprecation";
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
 * forma2's Follow-ups tab with Workflows available: follow-ups are deprecated, a survey's existing ones can still be read, fixed and deleted, and "New workflow" leads to the workspace's workflows, where their replacement is built.
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
      </Alert>

      <div className="flex justify-end">
        <Button size="sm" asChild>
          {/* A new tab on purpose: the editor autosaves drafts only, so leaving a published survey here would drop what the author has not saved. */}
          <a
            href={`/workspaces/${localSurvey.workspaceId}/workflows`}
            target="_blank"
            rel="noopener noreferrer">
            {t("common.new_workflow")}
          </a>
        </Button>
      </div>

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
