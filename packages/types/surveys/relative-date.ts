/**
 * The limit on a relative date bound of a date element's validation rules. Kept free of zod so the survey runtime, which resolves relative bounds as a respondent answers, can import it without the schema library; `validation-rules.ts` builds its schema with it and re-exports it.
 */

/**
 * Ten years. addWorkingDays steps one day at a time and these validators run on the server for every response, so an unbounded amount from a typo or an API-created survey would block the event loop. Exported so the editor's input caps at the same number the schema enforces, and so the resolver can clamp params saved by paths that skip this schema (draft saves).
 */
export const MAX_RELATIVE_DATE_AMOUNT = 3650;
