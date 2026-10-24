import { type Dispatch, type SetStateAction } from "react";
import type { TActionClass as ActionClass } from "@forma/types/action-classes";
import { TContactAttributeKey } from "@forma/types/contact-attribute-key";
import { TSurveyQuota } from "@forma/types/quota";
import { TSegment } from "@forma/types/segment";
import { TSurvey } from "@forma/types/surveys/types";
import { TUserLocale } from "@forma/types/user";
import { TargetingCard } from "../../segments/components/targeting-card";
import { QuotasCard } from "../quotas/components/quotas-card";
import { HowToSendCard } from "./how-to-send-card";
import { RecontactOptionsCard } from "./recontact-options-card";
import { ResponseOptionsCard } from "./response-options-card";
import { SurveyPlacementCard } from "./survey-placement-card";
import { WhenToSendCard } from "./when-to-send-card";
import { type TSurveySchedulingConfig } from "../lib/scheduling-config";

interface SettingsViewProps {
  localSurvey: TSurvey;
  setLocalSurvey: Dispatch<SetStateAction<TSurvey>>;
  actionClasses: ActionClass[];
  contactAttributeKeys: TContactAttributeKey[];
  segments: TSegment[];
  responseCount: number;
  finishedResponseCount: number;
  isReadOnly: boolean;
  isSpamProtectionAllowed: boolean;
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
  isReadOnly,
  isSpamProtectionAllowed,
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
        actionClasses={actionClasses}
        isReadOnly={isReadOnly}
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
