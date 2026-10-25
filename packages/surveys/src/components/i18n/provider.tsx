import { ComponentChildren } from "preact";
import { useEffect, useRef } from "preact/hooks";
import { I18nextProvider } from "react-i18next";
import i18n, { switchSurveyLanguage } from "../../lib/i18n.config";

export const I18nProvider = ({ language, children }: { language: string; children?: ComponentChildren }) => {
  const isFirstRender = useRef(true);
  const prevLanguage = useRef(language);

  // Set language synchronously on initial render so children get the correct translations immediately. This is synchronous when the language's translations are in memory, which renderSurvey (src/index.ts) waits for, up to a deadline (English is bundled, the others are fetched first; see i18n.config.ts). Past the deadline the survey renders in the language i18next is on, and switches when the fetch it already started arrives.
  // On subsequent renders, skip this to avoid overriding language changes made by the user via LanguageSwitch.
  if (isFirstRender.current) {
    switchSurveyLanguage(language);
    isFirstRender.current = false;
  }

  // Only update language when the prop itself changes, not when i18n was changed internally by user action
  useEffect(() => {
    if (prevLanguage.current !== language) {
      switchSurveyLanguage(language);
      prevLanguage.current = language;
    }
  }, [language]);

  // work around for react-i18next not supporting preact
  return <I18nextProvider i18n={i18n}>{children as unknown as React.ReactNode}</I18nextProvider>;
};
