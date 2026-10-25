import i18n, { type BackendModule } from "i18next";
import ICU from "i18next-icu";
import { initReactI18next } from "react-i18next";
import localeFiles from "virtual:survey-locale-files";
import {
  DEFAULT_SURVEY_LANGUAGE_CODE,
  SURVEY_RUNTIME_LANGUAGE_CODES,
  resolveSurveyLanguageDefaultTag,
} from "@forma/i18n-utils/survey-runtime-languages";
import enUSTranslations from "../../locales/en-US.json";

/**
 * Map any requested language tag to the bundle we actually ship, then English.
 *
 * Bundles are keyed by each language's canonical CLDR-default tag (`de-DE`, `ar-EG`, `zh-Hans-CN`), and
 * `resolveSurveyLanguageDefaultTag` is what turns a requested tag into that key — `de-AT`/`de` -> `de-DE`,
 * `pt-PT` -> `pt-BR`, while preserving script so `zh-Hant`/`zh-TW` resolve to Traditional. It is shared
 * with the workspace default-language picker, which uses it to decide whether a language has strings at
 * all, so the two can never disagree about what this runtime serves.
 */
export const resolveFallbackBundles = (code: string): string[] => {
  const defaultBundle = resolveSurveyLanguageDefaultTag(code);
  return defaultBundle && defaultBundle !== code
    ? [defaultBundle, DEFAULT_SURVEY_LANGUAGE_CODE]
    : [DEFAULT_SURVEY_LANGUAGE_CODE];
};

/**
 * The URL the locale files are resolved against: the script this runtime was loaded from, which they sit beside. `document.currentScript` names the script only while it runs, so it is read here, as the bundle evaluates. Without one (a host that put the runtime's code into its page instead of loading the script, a test), loadSurveyLanguage falls back to the app URL the host renders the survey with; with neither, the other languages render in English.
 */
let localeBaseUrl: string | undefined =
  typeof document === "undefined"
    ? undefined
    : (document.currentScript as HTMLScriptElement | null)?.src || undefined;

/** A fetch that failed before any answer arrived; i18next retries those, and not an answer that says the file is missing. */
const isNetworkFailure = (error: unknown): boolean => error instanceof TypeError;

/**
 * Fetches the languages the bundle does not carry from `virtual:survey-locale-files`. i18next keeps rendering the previous language until the requested one has arrived, so a language switch never shows untranslated keys, and falls back to English when the fetch fails.
 */
const localeFilesBackend: BackendModule = {
  type: "backend",
  init: () => undefined,
  read(language, _namespace, callback) {
    const file = localeFiles[language];
    if (!file || !localeBaseUrl) {
      callback(new Error(`No translation file for ${language}`), false);
      return;
    }
    fetch(new URL(file, localeBaseUrl))
      .then((response) => {
        if (!response.ok) throw new Error(`Translation file ${file} answered ${String(response.status)}`);
        return response.json() as Promise<Record<string, unknown>>;
      })
      .then(
        (translations) => callback(null, translations),
        (error: unknown) => callback(error as Error, isNetworkFailure(error))
      );
  },
};

i18n
  .use(ICU)
  .use(initReactI18next)
  .use(localeFilesBackend)
  .init({
    lng: DEFAULT_SURVEY_LANGUAGE_CODE,
    fallbackLng: resolveFallbackBundles,
    supportedLngs: [...SURVEY_RUNTIME_LANGUAGE_CODES],

    // English is bundled, so it is there from the first render and is what every other language falls back to; partialBundledLanguages lets the backend fetch the rest.
    resources: {
      [DEFAULT_SURVEY_LANGUAGE_CODE]: { translation: enUSTranslations },
    },
    partialBundledLanguages: true,

    interpolation: { escapeValue: false },
    // i18next otherwise logs an advertisement for its vendor's product in the console of every customer site that loads the widget.
    showSupportNotice: false,
  });

/**
 * The bundles a language renders with that are not in memory yet, which renderSurvey fetches before it shows a survey so the first card is not drawn in English and redrawn: the language's own bundle when it ships one, and its fallbacks.
 */
const missingBundles = (language: string): string[] =>
  [language, ...resolveFallbackBundles(language)].filter(
    (code) =>
      (code === DEFAULT_SURVEY_LANGUAGE_CODE || code in localeFiles) &&
      !i18n.hasResourceBundle(code, "translation")
  );

/**
 * Loads the translations `language` renders with and switches to it (`appUrl` is the app the survey talks to, see localeBaseUrl), as the I18nProvider does on its first render. Undefined when they are all in memory already (English always is), so a caller can render at once; otherwise a promise that resolves when they have arrived or failed to, in which case the survey renders in English. changeLanguage rather than loadLanguages: it resolves a legacy code (`de`) to its bundle the way rendering will, and it waits for a fetch another call already started.
 */
export const loadSurveyLanguage = (language: string, appUrl?: string): Promise<void> | undefined => {
  if (missingBundles(language).length === 0) return undefined;
  if (!localeBaseUrl && appUrl) localeBaseUrl = `${appUrl.replace(/\/+$/, "")}/js/`;
  return i18n.changeLanguage(language).then(() => undefined);
};

export default i18n;
