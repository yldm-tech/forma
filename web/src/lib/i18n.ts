import { type i18n as I18n, createInstance } from "i18next";
import ICU from "i18next-icu";
import { initReactI18next } from "react-i18next";

export const DEFAULT_LOCALE = "en-US";
export const AVAILABLE_LOCALES = ["en-US", "ja-JP", "zh-Hans-CN", "zh-Hant-TW"] as const;
export type Locale = (typeof AVAILABLE_LOCALES)[number];

/** One locale's catalogue keyed by namespace: the catalogue's top-level keys ("common", "auth", "workspace", ...). */
type Resources = Record<string, Record<string, unknown>>;

/** Rendered only into server-side email; the browser never needs it. */
const SERVER_ONLY_NAMESPACES = new Set(["emails"]);

// One lazily loaded chunk per locale: a browser downloads only the catalogues it uses.
const catalogues = import.meta.glob<Resources>("../../locales/*.json", { import: "default" });

const loadResources = async (locale: Locale): Promise<Resources> => {
  const load = catalogues[`../../locales/${locale}.json`];
  if (!load) throw new Error(`No translation catalogue for ${locale}`);
  const catalogue = await load();
  return Object.fromEntries(
    Object.entries(catalogue).filter(([namespace]) => !SERVER_ONLY_NAMESPACES.has(namespace))
  );
};

/** Chinese regions conventionally written in Traditional Han. Every other Chinese region is Simplified. */
const TRADITIONAL_CHINESE_REGIONS = new Set(["tw", "hk", "mo"]);

interface LocaleParts {
  language: string;
  script?: string;
  region?: string;
}

// A 4-letter subtag is a script, a 2-letter or 3-digit one a region. A Chinese tag with a region but no script (what browsers send: zh-TW) gets the script the region implies, because that is what tells the two Chinese catalogues apart.
const parseLocaleTag = (tag: string): LocaleParts => {
  const [language = "", ...rest] = tag.toLowerCase().split("-");
  let script: string | undefined;
  let region: string | undefined;
  for (const subtag of rest) {
    if (!script && subtag.length === 4 && /^[a-z]+$/.test(subtag)) script = subtag;
    else if (!region && /^([a-z]{2}|\d{3})$/.test(subtag)) region = subtag;
  }
  if (!script && language === "zh" && region) {
    script = TRADITIONAL_CHINESE_REGIONS.has(region) ? "hant" : "hans";
  }
  return { language, script, region };
};

// Script outranks region, so zh-HK reaches the Traditional catalogue rather than the Simplified one listed first.
const scoreMatch = (available: LocaleParts, requested: LocaleParts): number => {
  if (available.language !== requested.language) return 0;
  if (available.script && requested.script && available.script !== requested.script) return 0;
  if (available.region === requested.region && available.script === requested.script) return 4;
  if (available.script && available.script === requested.script) return 3;
  if (available.region && available.region === requested.region) return 2;
  return 1;
};

/**
 * Picks the catalogue for the user's preferred languages (navigator.languages, best first), as forma2 matched Accept-Language: the first requested language with any match wins, so a user who ranks French over Japanese gets Japanese rather than a closer match to a language ranked lower.
 */
export const matchLocale = (requested: readonly string[]): Locale => {
  const availableParts = AVAILABLE_LOCALES.map(parseLocaleTag);
  for (const tag of requested) {
    if (!tag || tag === "*") continue;
    const parts = parseLocaleTag(tag);
    let best = -1;
    let bestScore = 0;
    availableParts.forEach((available, index) => {
      const score = scoreMatch(available, parts);
      if (score > bestScore) {
        bestScore = score;
        best = index;
      }
    });
    const match = AVAILABLE_LOCALES[best];
    if (match) return match;
  }
  return DEFAULT_LOCALE;
};

export const isLocale = (value: string): value is Locale =>
  (AVAILABLE_LOCALES as readonly string[]).includes(value);

/**
 * Creates the i18next instance with en-US (the fallback for any missing key) and `locale` loaded. Keys are written `<namespace>.<key>` (`t("common.loading")`), the shape every forma2 call site uses, so screens port with their keys unchanged.
 */
export const initI18n = async (locale: Locale): Promise<I18n> => {
  const locales: Locale[] = locale === DEFAULT_LOCALE ? [DEFAULT_LOCALE] : [DEFAULT_LOCALE, locale];
  const loaded = await Promise.all(locales.map(loadResources));
  const resources = Object.fromEntries(locales.map((l, i) => [l, loaded[i] ?? {}]));
  const instance = createInstance();
  await instance
    .use(ICU)
    .use(initReactI18next)
    .init({
      lng: locale,
      fallbackLng: DEFAULT_LOCALE,
      supportedLngs: [...AVAILABLE_LOCALES],
      nsSeparator: ".",
      keySeparator: ".",
      defaultNS: false,
      ns: Object.keys(loaded[0] ?? {}),
      resources,
      interpolation: { escapeValue: false },
    });
  return instance;
};

/** Switches to `locale`, loading its catalogue first if needed; used once the signed-in user's saved locale is known. */
export const changeLocale = async (instance: I18n, locale: Locale): Promise<void> => {
  if (!instance.hasResourceBundle(locale, "common")) {
    const resources = await loadResources(locale);
    for (const [namespace, bundle] of Object.entries(resources)) {
      instance.addResourceBundle(locale, namespace, bundle, true, true);
    }
  }
  await instance.changeLanguage(locale);
};
