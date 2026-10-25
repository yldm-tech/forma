/**
 * The PIN token a correct PIN returned, kept for the tab's session while the survey saves respondents' progress, so a reload goes on where the respondent left off instead of asking for the PIN again. sessionStorage, not localStorage: it ends with the tab, and the server refuses the token an hour after the PIN was entered anyway (POST /s/{id}/pin/resume).
 *
 * Storage can be refused (a sandboxed frame, storage blocked by the browser); the gate then asks for the PIN on every visit, as it did before.
 */
const storageKey = (surveyId: string) => `forma:pin-token:${surveyId}`;

export const readPinToken = (surveyId: string): string | null => {
  try {
    return globalThis.sessionStorage.getItem(storageKey(surveyId));
  } catch {
    return null;
  }
};

export const keepPinToken = (surveyId: string, token: string): void => {
  try {
    globalThis.sessionStorage.setItem(storageKey(surveyId), token);
  } catch {
    // Storage refused: the PIN is asked for again after a reload.
  }
};

export const forgetPinToken = (surveyId: string): void => {
  try {
    globalThis.sessionStorage.removeItem(storageKey(surveyId));
  } catch {
    // Storage refused: nothing was kept.
  }
};
