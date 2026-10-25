import type { HistoryState } from "@tanstack/react-router";

// A survey created from scratch opens with its name field focused and selected, since the default name ("Untitled survey") is a placeholder the author is expected to replace. The request travels in the history entry's state, as the sign-up address does (features/auth/history-state.ts), so it marks only the navigation that created the survey, never a link to the editor.
const FOCUS_SURVEY_NAME = "focusSurveyName";

export const withSurveyNameFocus = (previous: HistoryState): HistoryState =>
  ({ ...previous, [FOCUS_SURVEY_NAME]: true }) as HistoryState;

export const surveyNameFocusFrom = (state: HistoryState | undefined): boolean =>
  (state as Record<string, unknown> | undefined)?.[FOCUS_SURVEY_NAME] === true;
