/**
 * The languages a user may choose for the app, forma2's appLanguages. Kept free of zod so the public survey page, which maps a survey's language to one of them, can import the list without the schema library; `user.ts` builds `ZUserLocale` from it.
 */

export const USER_LOCALES = [
  "de-DE",
  "en-US",
  "es-ES",
  "fr-FR",
  "hu-HU",
  "ja-JP",
  "nl-NL",
  "pt-BR",
  "pt-PT",
  "ro-RO",
  "ru-RU",
  "sv-SE",
  "tr-TR",
  "zh-Hans-CN",
  "zh-Hant-TW",
] as const;

export type TUserLocale = (typeof USER_LOCALES)[number];
