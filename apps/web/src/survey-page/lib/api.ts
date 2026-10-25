import { api } from "../../lib/api";
import type { TPublicSurvey } from "./state";

// The gates' endpoints live under the survey's own path, where forma2 posted their server actions (apps/server/internal/linksurvey/handler.go).

export interface TPinResult {
  survey: TPublicSurvey;
  pinAuthToken: string;
}

/** forma2's validateSurveyPinAction: the survey and its PIN token, or an ApiError (INVALID_PIN, rate limited). */
export const validateSurveyPin = (surveyId: string, pin: string): Promise<TPinResult> =>
  api.post<TPinResult>(`/s/${encodeURIComponent(surveyId)}/pin`, { pin });

/** Opens the PIN gate again with a token a correct PIN returned (pin-session.ts): the survey and the same token, or an ApiError when the token has expired or the survey no longer saves progress. */
export const resumeSurveyPin = (surveyId: string, pinAuthToken: string): Promise<TPinResult> =>
  api.post<TPinResult>(`/s/${encodeURIComponent(surveyId)}/pin/resume`, { pinAuthToken });

export interface TLinkSurveyEmailInput {
  email: string;
  suId: string;
  suToken?: string;
  locale: string;
  surveyLanguageCode: string;
}

/** forma2's sendLinkSurveyEmailAction. */
export const sendLinkSurveyEmail = (
  surveyId: string,
  input: TLinkSurveyEmailInput
): Promise<{ success: true }> =>
  api.post<{ success: true }>(`/s/${encodeURIComponent(surveyId)}/verify-email`, input);
