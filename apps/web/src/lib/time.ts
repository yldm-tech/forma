import { type Locale, formatDistance } from "date-fns";
import { de, enUS, es, fr, hu, ja, nl, pt, ptBR, ro, ru, sv, tr, zhCN, zhTW } from "date-fns/locale";

// forma2's time display helpers (apps/web/lib/time.ts, lib/utils/datetime.ts).

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

/** forma2's timeSince: "3 days ago" in the user's locale, English for any other. */
export const timeSince = (dateString: string, locale: string, now: Date = new Date()): string =>
  formatDistance(new Date(dateString), now, {
    addSuffix: true,
    locale: Object.hasOwn(TIME_SINCE_LOCALES, locale) ? TIME_SINCE_LOCALES[locale] : enUS,
  });

/** forma2's formatDateForDisplay: "Oct 22, 2026". */
export const formatDateForDisplay = (
  date: Date | string,
  locale: string,
  options: Intl.DateTimeFormatOptions = { year: "numeric", month: "short", day: "numeric" }
): string => new Intl.DateTimeFormat(locale, options).format(new Date(date));

/** forma2's formatDateTimeForDisplay: "October 22, 2026 at 9:05 AM". */
export const formatDateTimeForDisplay = (date: Date | string, locale: string): string =>
  formatDateForDisplay(date, locale, {
    year: "numeric",
    month: "long",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
