/** The PINs a survey can be protected with: exactly four digits. */
export const SURVEY_PIN_PATTERN = /^\d{4}$/;

// The largest multiple of 10 000 that fits in 32 bits: values at or above it are drawn again, so every PIN is equally likely.
const UNBIASED_LIMIT = 4_294_960_000;

/** Fills an array with random values, as crypto.getRandomValues does. */
type RandomFill = (array: Uint32Array<ArrayBuffer>) => Uint32Array<ArrayBuffer>;

/**
 * A random four-digit PIN, zero-padded, for a survey whose PIN protection was just switched on. forma2 filled in "1234", the PIN anyone tries first. Drawn from the Web Crypto generator rather than Math.random.
 */
export const generateSurveyPin = (
  getRandomValues: RandomFill = (array) => crypto.getRandomValues(array)
): string => {
  const draw = new Uint32Array(1);
  let value: number;
  do {
    value = getRandomValues(draw)[0] ?? 0;
  } while (value >= UNBIASED_LIMIT);
  return String(value % 10_000).padStart(4, "0");
};
