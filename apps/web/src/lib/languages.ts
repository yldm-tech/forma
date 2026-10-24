/** The languages a user may choose for the app (forma2's appLanguages, the ZUserLocale set). The app is translated into en-US, ja-JP, zh-Hans-CN and zh-Hant-TW; any other choice is shown in English. */
export const APP_LANGUAGES = [
  { code: "de-DE", english: "German", native: "Deutsch" },
  { code: "en-US", english: "English (US)", native: "English (US)" },
  { code: "es-ES", english: "Spanish", native: "Español" },
  { code: "fr-FR", english: "French", native: "Français" },
  { code: "hu-HU", english: "Hungarian", native: "Magyar" },
  { code: "ja-JP", english: "Japanese", native: "日本語" },
  { code: "nl-NL", english: "Dutch", native: "Nederlands" },
  { code: "pt-BR", english: "Portuguese (Brazil)", native: "Português (Brasil)" },
  { code: "pt-PT", english: "Portuguese (Portugal)", native: "Português (Portugal)" },
  { code: "ro-RO", english: "Romanian", native: "Română" },
  { code: "ru-RU", english: "Russian", native: "Русский" },
  { code: "sv-SE", english: "Swedish", native: "Svenska" },
  { code: "tr-TR", english: "Turkish", native: "Türkçe" },
  { code: "zh-Hans-CN", english: "Chinese (Simplified)", native: "简体中文" },
  { code: "zh-Hant-TW", english: "Chinese (Traditional)", native: "繁體中文" },
] as const;

/** "Japanese (日本語)", or the English name alone where it is the native one. */
export const languageLabel = (language: { english: string; native: string }): string =>
  language.native === language.english ? language.english : `${language.english} (${language.native})`;

export const sortedAppLanguages = [...APP_LANGUAGES].sort((a, b) => a.english.localeCompare(b.english));
