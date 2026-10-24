import { type Locale } from "date-fns";
import { de, enUS, es, fr, hu, ja, nl, pt, ptBR, ro, ru, sv, tr, zhCN, zhTW } from "date-fns/locale";

// A module of its own because it imports every date-fns locale the app maps (about 18 KB gzipped): the date formatters in ./datetime are shared with the public survey page's email gate, which must not download them, while only the calendar needs these.

/**
 * Maps an app locale code to the date-fns locale the calendar needs for month names, weekday headers and the first day of the week.
 *
 * `Intl` (which the formatters in ./datetime use) takes the BCP 47 tag directly, but `react-day-picker` wants a date-fns `Locale` object, so the app's locale codes have to be mapped explicitly. The keys mirror `apps/web/locales/*.json`; anything else — including a survey language code that never became an app locale — falls back to en-US rather than throwing.
 */
export const getDateFnsLocale = (localeCode?: string): Locale => {
  if (!localeCode) return enUS;

  const normalized = localeCode.toLowerCase();

  // Script-and-region-specific tags first: these do not survive being cut down to a base language (pt-BR and pt-PT differ, and zh-Hans/zh-Hant are different scripts, not different regions).
  if (normalized.startsWith("pt-br")) return ptBR;
  if (normalized.startsWith("pt-pt")) return pt;
  if (normalized.startsWith("zh-hans") || normalized === "zh-cn") return zhCN;
  if (normalized.startsWith("zh-hant") || normalized === "zh-tw" || normalized === "zh-hk") return zhTW;

  const localeMap: Record<string, Locale> = {
    de,
    en: enUS,
    es,
    fr,
    hu,
    ja,
    nl,
    pt: ptBR, // Bare "pt" is ambiguous; Brazilian is the larger audience and matches the survey packages.
    ro,
    ru,
    sv,
    tr,
    zh: zhCN,
  };

  return localeMap[normalized.split("-")[0] ?? ""] ?? enUS;
};
