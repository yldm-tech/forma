import { Suspense, lazy, useLayoutEffect, useState } from "react";
import { LinkSurveyNotFound } from "./components/not-found";
import { PageError } from "./components/page-error";
import { PinScreen } from "./components/pin-screen";
import { SurveyClientWrapper } from "./components/survey-client-wrapper";
import { SurveyCompletedMessage } from "./components/survey-completed-message";
import { SurveyInactive } from "./components/survey-inactive";
import { hasRespondedInBrowser } from "./lib/browser-response";
import { removeFirstPaint } from "./lib/first-paint";
import type { TSurveyPageState } from "./lib/state";

// Only the email gate reads the survey's questions (recall, every date-fns locale) and raises toasts, so it is a chunk of its own that every other page skips.
const VerifyEmail = lazy(async () => ({ default: (await import("./components/verify-email")).VerifyEmail }));

/** The screen the server chose for this page (forma2's link survey page and its renderSurvey). */
export const SurveyPageApp = ({ state }: { state: TSurveyPageState }) => {
  // One response per browser: the browser, not the server, knows whether it answered. Read once, before anything renders, so the survey never flashes up.
  const browserLimit = "browserLimit" in state ? state.browserLimit : undefined;
  const [browserResponded] = useState(() => !!browserLimit && hasRespondedInBrowser(browserLimit.surveyId));
  // The server paints the survey's title over a survey view (lib/first-paint.ts); the survey's loading overlay fades it out. Any other screen takes it away before it is first shown.
  const showsSurvey = !browserResponded && state.view === "survey" && !!state.survey.survey;
  useLayoutEffect(() => {
    if (!showsSurvey) removeFirstPaint();
  }, [showsSurvey]);
  if (browserResponded && browserLimit) {
    return (
      <SurveyInactive
        status="browser responded"
        isScheduled={false}
        surveyClosedMessage={null}
        workspace={browserLimit.workspace}
      />
    );
  }
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
