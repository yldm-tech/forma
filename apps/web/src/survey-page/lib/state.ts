import type {
  TSurvey,
  TSurveyClosedMessage,
  TSurveySingleUse,
  TSurveyStyling,
} from "@forma/types/surveys/types";
import type { TWorkspace, TWorkspaceStyling } from "@forma/types/workspace";

/**
 * The page state the Go server embeds in every survey page (server/internal/linksurvey): which screen to show and everything that screen needs. The server has already applied every gate it can decide; the bundle only renders.
 */

/** The survey as it reaches the browser: forma2's TSurvey without its PIN, follow-ups or targeting rules, its segment reduced to what the runtime reads. */
export type TPublicSurvey = Omit<TSurvey, "segment"> & {
  segment: { id: string; hasFilters: boolean } | null;
};

export type TInactiveStatus = "paused" | "completed" | "link invalid" | "response submitted" | "link expired";

/** The workspace's branding switch; null is no workspace, which shows Forma's branding. */
export type TBranding = { linkSurveyBranding: boolean } | null;

export interface TInactiveState {
  status: TInactiveStatus;
  isScheduled: boolean;
  surveyClosedMessage: TSurveyClosedMessage | null;
  workspace: TBranding;
}

export interface TCompletedState {
  singleUse: TSurveySingleUse | null;
  workspace: TBranding;
}

export interface TVerifyEmailState {
  fishy: boolean;
  survey: TPublicSurvey;
  languageCode: string;
  styling: TWorkspaceStyling;
  locale: string;
  singleUseId: string;
  singleUseToken: string | null;
}

export interface TSurveyWorkspace {
  styling: TWorkspaceStyling;
  logo: TWorkspace["logo"];
  linkSurveyBranding: boolean;
  customHeadScripts: string | null;
}

/** forma2's SurveyClientWrapper props. Behind a PIN, survey is null until the PIN is entered and locale is the gate's. */
export interface TSurveyProps {
  survey: TPublicSurvey | null;
  surveyId: string;
  workspace: TSurveyWorkspace;
  styling: TWorkspaceStyling | TSurveyStyling;
  publicDomain: string;
  responseCount: number | null;
  languageCode: string;
  locale: string;
  isEmbed: boolean;
  isPreview: boolean;
  singleUseId?: string;
  singleUseResponseId?: string;
  contactId?: string;
  canReadUserIdFromUrl: boolean;
  recaptchaSiteKey?: string;
  isSpamProtectionEnabled: boolean;
  verifiedEmail?: string;
  imprintUrl?: string;
  privacyUrl?: string;
  termsUrl?: string;
  isFormaCloud: boolean;
}

export type TSurveyPageState =
  | { view: "notFound" | "error"; locale: string }
  | { view: "inactive"; locale: string; inactive: TInactiveState }
  | { view: "completed"; locale: string; completed: TCompletedState }
  | { view: "verifyEmail"; locale: string; verifyEmail: TVerifyEmailState }
  | { view: "pin" | "survey"; locale: string; survey: TSurveyProps };

/** The id of the script element that carries the state (stateElementID in server/internal/linksurvey/shell.go). */
export const SURVEY_PAGE_STATE_ID = "forma-survey-page";

/** Reads the state the server embedded; a page without one (the bundle opened on its own) is the not-found screen. */
export const readSurveyPageState = (doc: Document): TSurveyPageState => {
  const text = doc.getElementById(SURVEY_PAGE_STATE_ID)?.textContent;
  if (!text) return { view: "notFound", locale: "en-US" };
  return JSON.parse(text) as TSurveyPageState;
};
