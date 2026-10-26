import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { I18nextProvider } from "react-i18next";
import "./index.css";
import { SurveyPageApp } from "./app";
import { readEmbedFrameMode, reportEmbedHeight } from "./lib/embed-frame";
import { initSurveyPageI18n } from "./lib/i18n";
import { readSurveyPageState } from "./lib/state";

// The public survey page (/s, /c, /p), a bundle of its own so respondents never download the admin app. The server renders the head and embeds the state; this renders the screen it names.
const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("#root element missing from survey.html");

const bootstrap = async () => {
  const state = readSurveyPageState(document);
  const i18n = await initSurveyPageI18n(state.locale);
  const embedFrame = readEmbedFrameMode(globalThis.location.search);
  createRoot(rootElement).render(
    <StrictMode>
      <I18nextProvider i18n={i18n}>
        {/* A survey that takes its content's height (the embed script's frames) does not fill the viewport: its frame follows its height, so a page that filled the frame would keep the frame from ever shrinking. */}
        <div className={embedFrame.isAutoHeight ? undefined : "h-dvh"}>
          <SurveyPageApp state={state} />
        </div>
      </I18nextProvider>
    </StrictMode>
  );
  // In another page's frame, the survey tells that page how tall it is; the embed script sizes the frame to it.
  if (embedFrame.isEmbed && globalThis.parent !== globalThis.self)
    reportEmbedHeight(rootElement, globalThis.parent);
};

void bootstrap();
