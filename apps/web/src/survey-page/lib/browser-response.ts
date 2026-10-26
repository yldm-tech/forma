/**
 * "One response per browser" (isSingleResponsePerBrowserEnabled, owner decision D-FG03): once a respondent's final answer reaches the server, the page remembers in this browser that it answered the survey, and a later visit shows "already responded" instead of the survey.
 *
 * Best effort, as the editor says: the mark is kept twice, in localStorage and in a cookie under the survey's own path (/s/{surveyId}, so the server and every other page never receive it), so clearing only one of them does not forget it, and either one found restores the other. A private window, clearing all site data, another browser or device answers again; a frame on another site may be refused both (third-party storage), and then the survey is not limited there. On a shared device the next person cannot answer.
 */

const MARK = "1";
/** How long the mark lasts: a year, about as long as a browser keeps a cookie it was given. */
const MAX_AGE_SECONDS = 60 * 60 * 24 * 365;

const storageKey = (surveyId: string) => `forma:responded:${surveyId}`;
const cookieName = (surveyId: string) => `forma_responded_${surveyId}`;

const readStorage = (surveyId: string): boolean => {
  try {
    return globalThis.localStorage.getItem(storageKey(surveyId)) === MARK;
  } catch {
    return false;
  }
};

const writeStorage = (surveyId: string): void => {
  try {
    globalThis.localStorage.setItem(storageKey(surveyId), MARK);
  } catch {
    // Storage refused (a sandboxed frame, storage blocked): the cookie alone remembers.
  }
};

const readCookie = (surveyId: string): boolean => {
  try {
    const name = cookieName(surveyId);
    return globalThis.document.cookie.split(";").some((pair) => pair.trim() === `${name}=${MARK}`);
  } catch {
    return false;
  }
};

const writeCookie = (surveyId: string): void => {
  try {
    const secure = globalThis.location.protocol === "https:" ? "; Secure" : "";
    globalThis.document.cookie = `${cookieName(surveyId)}=${MARK}; Max-Age=${String(MAX_AGE_SECONDS)}; Path=/s/${encodeURIComponent(surveyId)}; SameSite=Lax${secure}`;
  } catch {
    // Cookies refused: localStorage alone remembers.
  }
};

/** Whether this browser has finished a response to the survey. Finding the mark in one place writes it back to the other. */
export const hasRespondedInBrowser = (surveyId: string): boolean => {
  const inStorage = readStorage(surveyId);
  const inCookie = readCookie(surveyId);
  if (inStorage && !inCookie) writeCookie(surveyId);
  if (inCookie && !inStorage) writeStorage(surveyId);
  return inStorage || inCookie;
};

/** Remembers that this browser finished a response to the survey. */
export const markRespondedInBrowser = (surveyId: string): void => {
  writeStorage(surveyId);
  writeCookie(surveyId);
};
