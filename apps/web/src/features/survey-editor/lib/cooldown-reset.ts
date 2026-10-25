import type { TSurvey } from "@forma/types/surveys/types";

/** Whether the survey can show more than once; js-core puts no limit on a "displaySome" survey whose displayLimit is null. */
const canShowMoreThanOnce = ({ displayOption, displayLimit }: TSurvey): boolean => {
  if (displayOption === "displayOnce") return false;
  if (displayOption === "displaySome") return displayLimit === null || displayLimit > 1;
  return true;
};

/**
 * Whether an app survey keeps restarting the workspace cooldown: it ignores the cooldown, can show more than once, and one of its triggers is a page view on any page (no URL filter). Every display restarts the cooldown that surveys set to "Use Cooldown Period" wait out, from the last display of any survey, so such a survey can keep them from ever showing; the SDK's debug output reports them as left out for "workspace cooldown".
 */
export const resetsCooldownOnEveryPageView = (survey: TSurvey): boolean =>
  survey.type === "app" &&
  survey.recontactDays === 0 &&
  canShowMoreThanOnce(survey) &&
  survey.triggers.some(
    ({ actionClass }) =>
      actionClass.noCodeConfig?.type === "pageView" && actionClass.noCodeConfig.urlFilters.length === 0
  );
