/**
 * What stored progress records of the email verification token of the page that stored it (SerializedSurveyState.verifyTokenHash): the token's SHA-256 as lowercase hex, rather than the token, which is a credential for the address it proves. Two pages opened from the same verification link get the same value; pages opened from different links never do.
 *
 * A page the browser gives no SubtleCrypto (served over plain HTTP from a host other than localhost) gets a random value instead, which matches no other page: a verified respondent's progress then does not survive a reload, rather than ever being resumed by someone else.
 */
export const hashVerifyToken = async (token: string): Promise<string> => {
  const subtle = globalThis.crypto?.subtle as SubtleCrypto | undefined;
  if (!subtle) {
    const random = new Uint32Array(4);
    globalThis.crypto.getRandomValues(random);
    return `unhashed-${Array.from(random, (n) => n.toString(16).padStart(8, "0")).join("")}`;
  }
  const digest = await subtle.digest("SHA-256", new TextEncoder().encode(token));
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
};
