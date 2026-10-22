import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { I18nextProvider } from "react-i18next";
import "./index.css";
import { SurveyPageApp } from "./app";
import { initSurveyPageI18n } from "./lib/i18n";
import { readSurveyPageState } from "./lib/state";

// The public survey page (/s, /c, /p), a bundle of its own so respondents never download the admin app. The server renders the head and embeds the state; this renders the screen it names.
const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("#root element missing from survey.html");

const bootstrap = async () => {
  const state = readSurveyPageState(document);
  const i18n = await initSurveyPageI18n(state.locale);
  createRoot(rootElement).render(
    <StrictMode>
      <I18nextProvider i18n={i18n}>
        <div className="h-dvh">
          <SurveyPageApp state={state} />
        </div>
      </I18nextProvider>
    </StrictMode>
  );
};

void bootstrap();
