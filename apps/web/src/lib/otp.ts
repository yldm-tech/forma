const DIGITS = /^\d+$/;

/** The boxes of a one-time code input: each position's digit, or empty. */
export const otpDigits = (value: string, length: number): string[] =>
  Array.from({ length }, (_, i) => {
    const char = value[i] ?? "";
    return DIGITS.test(char) ? char : "";
  });

/**
 * forma2's OTPInput change rule for the box at index: a digit replaces that position and moves on, clearing it leaves a space (only when the next box is empty, so a code is never left with a hole), and a whole code pasted into any box replaces the value. Anything else is ignored (null).
 */
export const setOtpDigit = (
  value: string,
  length: number,
  index: number,
  input: string
): { value: string; advance: "next" | "done" | "stay" } | null => {
  const typed = input.trim();
  const isDigits = DIGITS.test(typed);
  if (!isDigits && typed !== "") return null;
  if (isDigits && typed.length === length) return { value: typed, advance: "done" };
  if (isDigits && typed.length !== 1) return null;
  if (!isDigits && (value[index + 1] ?? " ").trim() !== "") return null;
  const padded = value.padEnd(length, " ");
  const char = isDigits ? typed : " ";
  return {
    value: padded.slice(0, index) + char + padded.slice(index + 1),
    advance: isDigits ? "next" : "stay",
  };
};
