import { type Locale, matchLocale } from "./i18n";

/**
 * The catalogue the app starts in, before the signed-in user's saved locale is known: the `lang` the address asks for (the website's Sign in links pass the language the visitor chose there), then the browser's languages. The hint is not stored, so it lasts only for this load; a signed-in user's saved locale replaces it (see the authenticated layout).
 */
export const initialLocale = (search: string, languages: readonly string[]): Locale =>
  matchLocale([new URLSearchParams(search).get("lang") ?? "", ...languages]);
