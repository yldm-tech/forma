import { type BackendModule, type i18n as I18n, createInstance } from "i18next";
import ICU from "i18next-icu";
import { initReactI18next } from "react-i18next";
import { DEFAULT_LOCALE, type Locale, matchLocale } from "../../lib/i18n";

/** The namespaces of the catalogue a respondent's page reads: forma2's public namespaces, never the admin UI's. */
export const SURVEY_PAGE_NAMESPACES = ["common", "s", "c"] as const;

type Namespaces = Record<(typeof SURVEY_PAGE_NAMESPACES)[number], Record<string, unknown>>;

/** One small chunk per shipped locale, each carrying only the public namespaces (see the survey page locale plugin in apps/web/vite.config.ts). */
export const surveyPageCatalogues: Record<Locale, () => Promise<Namespaces>> = {
  "en-US": () => import("virtual:survey-page-locale/en-US"),
  "ja-JP": () => import("virtual:survey-page-locale/ja-JP"),
  "zh-Hans-CN": () => import("virtual:survey-page-locale/zh-Hans-CN"),
  "zh-Hant-TW": () => import("virtual:survey-page-locale/zh-Hant-TW"),
};

/**
 * Loads a locale's namespaces on demand, so switching language (the in-survey picker, a gate's own locale) fetches its catalogue first. A locale the app does not ship reads the closest one it does, as forma2's catalogue lookup fell back.
 */
const backend: BackendModule = {
  type: "backend",
  init: () => undefined,
  read: (language, namespace, callback) => {
    surveyPageCatalogues[matchLocale([language])]().then(
      (bundle) => callback(null, bundle[namespace as keyof Namespaces] ?? {}),
      (error: unknown) => callback(error instanceof Error ? error : new Error(String(error)), false)
    );
  },
};

/** Creates the page's i18n instance in `locale`, with en-US loaded as the fallback for missing keys. Keys are forma2's `<namespace>.<key>`. */
export const initSurveyPageI18n = async (locale: string): Promise<I18n> => {
  const instance = createInstance();
  await instance
    .use(backend)
    .use(ICU)
    .use(initReactI18next)
    .init({
      lng: locale,
      fallbackLng: DEFAULT_LOCALE,
      ns: [...SURVEY_PAGE_NAMESPACES],
      defaultNS: false,
      nsSeparator: ".",
      keySeparator: ".",
      interpolation: { escapeValue: false },
      react: { useSuspense: false },
    });
  return instance;
};
