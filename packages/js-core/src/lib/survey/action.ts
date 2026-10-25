import { Config } from "@/lib/common/config";
import { FORMA_EVENTS, emitFormaEvent } from "@/lib/common/events";
import { Logger } from "@/lib/common/logger";
import { getSurveyExclusionReason } from "@/lib/common/utils";
import { triggerSurvey } from "@/lib/survey/widget";
import { type InvalidCodeError, type NetworkError, type Result, err, okVoid } from "@/types/error";
import { type TTrackProperties } from "@/types/survey";

/**
 * Tracks an action name and triggers associated surveys
 * @param name - The name of the action to track
 * @param alias - Optional alias for the action name
 * @param properties - Optional properties to set, like the hidden fields (deprecated, hidden fields will be removed in a future version)
 * @returns Result indicating success or network error
 */
export const trackAction = async (
  name: string,
  alias?: string,
  properties?: TTrackProperties
): Promise<Result<void, NetworkError>> => {
  const logger = Logger.getInstance();
  const appConfig = Config.getInstance();

  const aliasName = alias ?? name;

  logger.debug(`Forma: Action "${aliasName}" tracked`);

  // The shared path under both code and no-code actions, so every tracked action reaches the host
  // exactly once — whether or not it triggers a survey (funnel analytics wants the misses too).
  emitFormaEvent(FORMA_EVENTS.actionTracked, { action: aliasName });

  // get a list of surveys that are collecting insights
  const activeSurveys = appConfig.get().filteredSurveys;

  explainUntriggeredSurveys(name, aliasName);

  if (Boolean(activeSurveys) && activeSurveys.length > 0) {
    for (const survey of activeSurveys) {
      for (const trigger of survey.triggers) {
        if (trigger.actionClass.name === name) {
          await triggerSurvey(survey, name, properties);
        }
      }
    }
  } else {
    logger.debug("No active surveys to display");
  }

  return okVoid();
};

/**
 * Debug output for a tracked action: each survey it is a trigger of that the user cannot be shown, with the reason, or that no survey in the state uses it at all. Without this, debug mode only lists the surveys that can show, and a survey that silently never appears (an "ignore cooldown" survey reset the cooldown, it was already shown once, the user is not in its segment) leaves nothing to go on.
 */
const explainUntriggeredSurveys = (name: string, aliasName: string): void => {
  const logger = Logger.getInstance();
  const { workspace, user, filteredSurveys } = Config.getInstance().get();
  const triggered = workspace.data.surveys.filter((survey) =>
    survey.triggers.some((trigger) => trigger.actionClass.name === name)
  );

  if (!triggered.length) {
    const { truncated } = workspace.data;
    logger.debug(
      `No survey uses action "${aliasName}"${truncated ? ` (${truncated.toString()} old surveys not in state)` : ""}`
    );
    return;
  }

  for (const survey of triggered) {
    if (filteredSurveys.some((eligible) => eligible.id === survey.id)) continue;
    logger.debug(
      `Survey ${survey.id} left out: ${getSurveyExclusionReason(survey, workspace.data.settings, user) ?? "not re-evaluated yet"}`
    );
  }
};

/**
 * Tracks an action by its code and triggers associated surveys (used for code actions only)
 * @param code - The action code to track
 * @param properties - Optional properties to set, like the hidden fields (deprecated, hidden fields will be removed in a future version)
 * @returns Result indicating success, network error, or invalid code error
 */
export const trackCodeAction = async (
  code: string,
  properties?: TTrackProperties
): Promise<Result<void, NetworkError> | Result<void, InvalidCodeError>> => {
  const appConfig = Config.getInstance();

  const {
    workspace: {
      data: { actionClasses = [] },
    },
  } = appConfig.get();

  const codeActionClasses = actionClasses.filter((action) => action.type === "code");
  const actionClass = codeActionClasses.find((action) => action.key === code);

  if (!actionClass) {
    return err({
      code: "invalid_code",
      message: `Action with identifier '${code}' is unknown. Please add this action in Forma in order to use it via the SDK action tracking.`,
    });
  }

  return trackAction(actionClass.name, code, properties);
};

export const trackNoCodeAction = (name: string): Promise<Result<void, NetworkError>> => {
  return trackAction(name);
};
