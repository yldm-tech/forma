/**
 * ReDoS-safe validation utilities
 * These functions avoid using Zod to keep the surveys package lightweight
 */

/**
 * Validate email address using a ReDoS-safe regex
 * Matches Zod's/HTML5's permissive behavior while avoiding nested quantifiers
 */
export const validateEmail = (email: string): boolean => {
  // ReDoS-safe email validation regex that closely matches Zod's/HTML5's permissive behavior.
  // This avoids "super-linear runtime" warnings by sticking to specific allowed characters
  // rather than negated character classes where possible, and avoiding nested quantifiers.
  const emailRegex = /^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$/;
  return emailRegex.test(email);
};

/**
 * Validate URL using the URL constructor
 */
export const validateUrl = (url: string): boolean => {
  try {
    // Use URL constructor for validation
    new URL(url);
    return true;
  } catch {
    return false;
  }
};

// Characters people write between the digits of a phone number, after NFKC (which already folds full-width forms and the ideographic space): space, - . ( ) /, the Unicode hyphens and dashes U+2010 to U+2015, the minus sign, and the katakana prolonged sound mark a Japanese IME types for a hyphen. The server's isValidPhone (apps/server/internal/response/answers/rules.go) strips the same set.
const PHONE_SEPARATORS = /[ \-.()/\u2010-\u2015\u2212\u30fc]/g;

// What is left must be an optional leading + and 7 to 15 digits of any script (E.164 allows at most 15).
const PHONE_DIGITS = /^\+?\p{Nd}{7,15}$/u;

/**
 * Validate a phone number the way the server does: NFKC-normalize, drop the separators, then require an optional leading + and 7 to 15 digits. The answer itself is stored as typed.
 *
 * The cases both sides must agree on are in apps/server/internal/response/answers/testdata/phone.json.
 */
export const validatePhone = (phone: string): boolean => {
  return PHONE_DIGITS.test(phone.normalize("NFKC").replace(PHONE_SEPARATORS, ""));
};
