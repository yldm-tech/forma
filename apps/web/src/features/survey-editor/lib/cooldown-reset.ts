import type { TSurvey } from "@forma/types/surveys/types";

/**
 * Whether an app survey keeps restarting the workspace cooldown: it ignores the cooldown, can show more than once, and one of its triggers is a page view on any page (no URL filter). Every display restarts the cooldown that surveys set to "Use Cooldown Period" wait out, from the last display of any survey, so such a survey can keep them from ever showing; the SDK's debug output reports them as left out for "workspace cooldown".
 */
export const resetsCooldownOnEveryPageView = (survey: TSurvey): boolean =>
  survey.type === "app" &&
  survey.recontactDays === 0 &&
  survey.displayOption !== "displayOnce" &&
  survey.triggers.some(
    ({ actionClass }) =>
      actionClass.noCodeConfig?.type === "pageView" && actionClass.noCodeConfig.urlFilters.length === 0
  );
