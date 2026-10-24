import { LinkSurveyNotFound } from "./components/not-found";
import { PageError } from "./components/page-error";
import { PinScreen } from "./components/pin-screen";
import { SurveyClientWrapper } from "./components/survey-client-wrapper";
import { SurveyCompletedMessage } from "./components/survey-completed-message";
import { SurveyInactive } from "./components/survey-inactive";
import { VerifyEmail } from "./components/verify-email";
import type { TSurveyPageState } from "./lib/state";

/** The screen the server chose for this page (forma2's link survey page and its renderSurvey). */
export const SurveyPageApp = ({ state }: { state: TSurveyPageState }) => {
  switch (state.view) {
    case "inactive":
      return <SurveyInactive {...state.inactive} />;
    case "completed":
      return <SurveyCompletedMessage {...state.completed} />;
    case "verifyEmail":
      return <VerifyEmail {...state.verifyEmail} />;
    case "pin":
      return <PinScreen {...state.survey} />;
    case "survey":
      return state.survey.survey ? (
        <SurveyClientWrapper {...state.survey} survey={state.survey.survey} />
      ) : (
        <PageError />
      );
    case "error":
      return <PageError />;
    default:
      return <LinkSurveyNotFound />;
  }
};
