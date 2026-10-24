import { Suspense, lazy } from "react";
import { LinkSurveyNotFound } from "./components/not-found";
import { PageError } from "./components/page-error";
import { PinScreen } from "./components/pin-screen";
import { SurveyClientWrapper } from "./components/survey-client-wrapper";
import { SurveyCompletedMessage } from "./components/survey-completed-message";
import { SurveyInactive } from "./components/survey-inactive";
import type { TSurveyPageState } from "./lib/state";

// Only the email gate reads the survey's questions (recall, every date-fns locale) and raises toasts, so it is a chunk of its own that every other page skips.
const VerifyEmail = lazy(async () => ({ default: (await import("./components/verify-email")).VerifyEmail }));

/** The screen the server chose for this page (forma2's link survey page and its renderSurvey). */
export const SurveyPageApp = ({ state }: { state: TSurveyPageState }) => {
  switch (state.view) {
    case "inactive":
      return <SurveyInactive {...state.inactive} />;
    case "completed":
      return <SurveyCompletedMessage {...state.completed} />;
    case "verifyEmail":
      return (
        <Suspense fallback={null}>
          <VerifyEmail {...state.verifyEmail} />
        </Suspense>
      );
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
