import type { InitOptions } from "i18next";

/**
 * The i18next options shared by the browser provider, so the configuration is one testable value
 * rather than an object literal buried in a component.
 *
 * The catalogue's top-level keys are treated as namespaces. Every `t()` call in this repo already
 * writes `<namespace>.<rest>` — `common.welcome`, `workspace.settings.general.title` — and i18next
 * splits the namespace off the first `nsSeparator` before applying `keySeparator` to the remainder,
 * so sharing `.` between the two is what lets the namespaces exist without touching a single call
 * site.
 *
 * `defaultNS: false` is deliberate: there is no namespace to fall back to, so a key written without
 * one is a bug that shows up as the key rather than silently resolving somewhere.
 */
export const buildI18nInitOptions = ({
  language,
  defaultLanguage,
  resources,
}: {
  language: string;
  defaultLanguage: string;
  resources: Record<string, Record<string, unknown>>;
}): InitOptions => ({
  lng: language,
  fallbackLng: defaultLanguage,
  nsSeparator: ".",
  keySeparator: ".",
  defaultNS: false,
  ns: Object.keys(resources),
  resources: { [language]: resources },
  // The server already read these, so initialisation has nothing to wait for. Both flags keep
  // i18next from deferring work to a microtask or a backend round trip, which is what makes the
  // provider's first render — including the one on the server — already translated.
  initImmediate: false,
  partialBundledLanguages: true,
  interpolation: { escapeValue: false },
});
