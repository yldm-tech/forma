import { type Locale, formatDistance } from "date-fns";
import { de, enUS, es, fr, hu, ja, nl, pt, ptBR, ro, ru, sv, tr, zhCN, zhTW } from "date-fns/locale";

// forma2's lib/time.ts timeSince and lib/utils/datetime.ts formatDateForDisplay, which the survey list's date columns use.

const TIME_SINCE_LOCALES: Record<string, Locale> = {
  "de-DE": de,
  "en-US": enUS,
  "es-ES": es,
  "fr-FR": fr,
  "hu-HU": hu,
  "ja-JP": ja,
  "nl-NL": nl,
  "pt-BR": ptBR,
  "pt-PT": pt,
  "ro-RO": ro,
  "ru-RU": ru,
  "sv-SE": sv,
  "tr-TR": tr,
  "zh-Hans-CN": zhCN,
  "zh-Hant-TW": zhTW,
};

/** "5 minutes ago" in the dashboard language; English for a locale date-fns is not set up for. */
export const timeSince = (iso: string, locale: string, now: Date = new Date()): string =>
  formatDistance(new Date(iso), now, {
    addSuffix: true,
    locale: Object.hasOwn(TIME_SINCE_LOCALES, locale) ? TIME_SINCE_LOCALES[locale] : enUS,
  });

/** A calendar date such as "Oct 22, 2026" in the dashboard language. */
export const formatDateForDisplay = (iso: string, locale: string): string =>
  new Intl.DateTimeFormat(locale, { year: "numeric", month: "short", day: "numeric" }).format(new Date(iso));
