export const getNPSOptionColor = (idx: number): string => {
  if (idx > 8) return "bg-emerald-100";
  if (idx > 6) return "bg-orange-100";
  return "bg-rose-100";
};

export const getRatingNumberOptionColor = (range: number, idx: number): string => {
  if (range > 5) {
    if (range - idx < 2) return "emerald-100";
    if (range - idx < 4) return "orange-100";
    return "rose-100";
  } else if (range < 5) {
    if (range - idx < 1) return "emerald-100";
    if (range - idx < 2) return "orange-100";
    return "rose-100";
  }
  if (range - idx < 2) return "emerald-100";
  if (range - idx < 3) return "orange-100";
  return "rose-100";
};

const defaultLocale = "en-US";

const getMessages = (locale: string): Record<string, string> => {
  // A stored locale can outlive its translation file — the app ships Chinese, Japanese and English,
  // and a user who picked one of the locales that used to be offered still carries that value. A
  // bare `require` would throw while sending their mail, so fall back to English instead.
  try {
    const messages = require(`@/locales/${locale}.json`) as {
      emails: Record<string, string>;
    };
    return messages.emails;
  } catch {
    const fallback = require(`@/locales/${defaultLocale}.json`) as {
      emails: Record<string, string>;
    };
    return fallback.emails;
  }
};

export const translateEmailText = (
  text: string,
  locale: string,
  replacements?: Record<string, string>
): string => {
  const messages = getMessages(locale || defaultLocale);
  let translatedText = messages[text] || text;

  if (replacements) {
    Object.entries(replacements).forEach(([key, value]) => {
      translatedText = translatedText.replace(new RegExp(`\\{${key}\\}`, "g"), value);
    });
  }

  return translatedText;
};
