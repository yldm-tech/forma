import { type Locale, formatDistance } from "date-fns";
import { de, enUS, es, fr, hu, ja, nl, pt, ptBR, ro, ru, sv, tr, zhCN, zhTW } from "date-fns/locale";
import { TUserLocale } from "@forma/types/user";
import { formatDateForDisplay } from "./utils/datetime";

const DEFAULT_LOCALE: TUserLocale = "en-US";
const TIME_SINCE_LOCALES: Record<TUserLocale, Locale> = {
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

const isUserLocale = (locale: string): locale is TUserLocale => Object.hasOwn(TIME_SINCE_LOCALES, locale);

/** Maps locale strings to date-fns locales and falls back to English for unsupported inputs. */
const getLocaleForTimeSince = (locale: string): Locale =>
  isUserLocale(locale) ? TIME_SINCE_LOCALES[locale] : enUS;

export const timeSince = (dateString: string, locale: string = DEFAULT_LOCALE) => {
  const date = new Date(dateString);
  return formatDistance(date, new Date(), {
    addSuffix: true,
    locale: getLocaleForTimeSince(locale),
  });
};

export const timeSinceDate = (date: Date, locale: string = DEFAULT_LOCALE) => {
  return formatDistance(date, new Date(), {
    addSuffix: true,
    locale: getLocaleForTimeSince(locale),
  });
};

export const formatDate = (date: Date, locale: string = DEFAULT_LOCALE) => {
  return formatDateForDisplay(date, locale, {
    year: "numeric",
    month: "long",
    day: "numeric",
  });
};

/**
 * A UTC date and clock time joined by `seperator`, for machine-facing names such as the response export's filename.
 *
 * Both halves come out of one ISO string, so they always describe the same moment. They used not to: the date was read from `toISOString()` (UTC) while the time came from `toTimeString()` (the process's local zone), so on a server running ahead of UTC an export taken at 08:30 JST was named `…-2026-09-19-08-30-00` — a calendar day behind the clock time printed beside it, and out of name order against an export taken minutes earlier on the other side of UTC midnight.
 *
 * `date` is injectable so the format can be asserted against a fixed instant instead of recomputed the same way the implementation computes it.
 */
export const getTodaysDateTimeFormatted = (seperator: string, date: Date = new Date()) => {
  const [isoDate, isoTime] = date.toISOString().split("T");
  const formattedDate = isoDate.split("-").join(seperator);
  const formattedTime = isoTime.slice(0, 8).split(":").join(seperator);

  return [formattedDate, formattedTime].join(seperator);
};

export const convertDatesInObject = <T>(obj: T, keysToIgnore?: Set<string>): T => {
  if (obj === null || typeof obj !== "object") {
    return obj; // Return if obj is not an object
  }
  if (Array.isArray(obj)) {
    // Handle arrays by mapping each element through the function
    return obj.map((item) => convertDatesInObject(item, keysToIgnore)) as unknown as T;
  }
  const newObj: Record<string, unknown> = {};
  for (const key in obj) {
    if (keysToIgnore?.has(key)) {
      newObj[key] = obj[key];
      continue;
    }
    if (
      (key === "createdAt" || key === "updatedAt") &&
      typeof obj[key] === "string" &&
      !isNaN(Date.parse(obj[key] as unknown as string))
    ) {
      newObj[key] = new Date(obj[key] as unknown as string);
    } else if (typeof obj[key] === "object" && obj[key] !== null) {
      newObj[key] = convertDatesInObject(obj[key], keysToIgnore);
    } else {
      newObj[key] = obj[key];
    }
  }
  return newObj as T;
};
