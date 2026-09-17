import { type Dispatch, type SetStateAction } from "react";
import { ActionClass, OrganizationRole } from "@forma/database/prisma-browser";
import { TContactAttributeKey } from "@forma/types/contact-attribute-key";
import { TSurveyQuota } from "@forma/types/quota";
import { TSegment } from "@forma/types/segment";
import { TSurvey } from "@forma/types/surveys/types";
import { TUserLocale } from "@forma/types/user";
import { TargetingCard } from "@/modules/contacts/segments/components/targeting-card";
import { QuotasCard } from "@/modules/quotas/components/quotas-card";
import { HowToSendCard } from "@/modules/survey/editor/components/how-to-send-card";
import { RecontactOptionsCard } from "@/modules/survey/editor/components/recontact-options-card";
import { ResponseOptionsCard } from "@/modules/survey/editor/components/response-options-card";
import { SurveyPlacementCard } from "@/modules/survey/editor/components/survey-placement-card";
import { WhenToSendCard } from "@/modules/survey/editor/components/when-to-send-card";
import { type TSurveySchedulingConfig } from "@/modules/survey/scheduling/lib/config";
import { TTeamPermission } from "@/modules/teams/workspace-teams/types/team";

interface SettingsViewProps {
  localSurvey: TSurvey;
  setLocalSurvey: Dispatch<SetStateAction<TSurvey>>;
  actionClasses: ActionClass[];
  contactAttributeKeys: TContactAttributeKey[];
  segments: TSegment[];
  responseCount: number;
  finishedResponseCount: number;
  membershipRole?: OrganizationRole;
  isSpamProtectionAllowed: boolean;
  workspacePermission: TTeamPermission | null;
  quotas: TSurveyQuota[];
  surveySchedulingConfig: TSurveySchedulingConfig;
  locale: TUserLocale;
  appSetupCompleted: boolean;
  /** A save or publish was blocked because the survey has no trigger (ENG-2581). */
  hasTriggerError?: boolean;
}

export const SettingsView = ({
  localSurvey,
  setLocalSurvey,
  actionClasses,
  contactAttributeKeys,
  segments,
  responseCount,
  finishedResponseCount,
  membershipRole,
  isSpamProtectionAllowed,
  workspacePermission,
  quotas,
  surveySchedulingConfig,
  locale,
  appSetupCompleted,
  hasTriggerError = false,
}: Readonly<SettingsViewProps>) => {
  const isAppSurvey = localSurvey.type === "app";

  return (
    <div className="mt-12 space-y-3 p-5">
      <HowToSendCard
        localSurvey={localSurvey}
        setLocalSurvey={setLocalSurvey}
        appSetupCompleted={appSetupCompleted}
      />

      {localSurvey.type === "app" ? (
        <TargetingCard
          key={localSurvey.segment?.id}
          localSurvey={localSurvey}
          setLocalSurvey={setLocalSurvey}
          contactAttributeKeys={contactAttributeKeys}
          segments={segments}
          initialSegment={segments.find((segment) => segment.id === localSurvey.segment?.id)}
        />
      ) : null}

      <WhenToSendCard
        localSurvey={localSurvey}
        setLocalSurvey={setLocalSurvey}
        workspaceId={localSurvey.workspaceId}
        propActionClasses={actionClasses}
        membershipRole={membershipRole}
        workspacePermission={workspacePermission}
        hasError={hasTriggerError}
      />
      <QuotasCard localSurvey={localSurvey} quotas={quotas} hasResponses={responseCount > 0} />

      <ResponseOptionsCard
        localSurvey={localSurvey}
        setLocalSurvey={setLocalSurvey}
        finishedResponseCount={finishedResponseCount}
        isSpamProtectionAllowed={isSpamProtectionAllowed}
        surveySchedulingConfig={surveySchedulingConfig}
        locale={locale}
      />

      <RecontactOptionsCard localSurvey={localSurvey} setLocalSurvey={setLocalSurvey} />

      {isAppSurvey && <SurveyPlacementCard localSurvey={localSurvey} setLocalSurvey={setLocalSurvey} />}
    </div>
  );
};
