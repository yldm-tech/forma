/**
 * A date element's answer is `YYYY-MM-DD`, or `YYYY-MM-DDTHH:mm` when the element includes the time of day: the local date and time the respondent picked, with no time zone. Kept free of zod so the survey runtime can import it; the Go side (`apps/server/internal/response/answers`, `apps/server/internal/survey/surveylogic`) applies the same rules.
 */

/** A date-and-time answer as the date element stores it. */
export const DATE_TIME_ANSWER_PATTERN = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/;

/** Whether a date answer carries a time of day. */
export const hasTimeOfDay = (answer: string): boolean => answer.length > 10 && answer[10] === "T";

/** The day of a date answer, `YYYY-MM-DD`, with any time of day dropped. */
export const getAnswerDay = (answer: string): string => (hasTimeOfDay(answer) ? answer.slice(0, 10) : answer);

/**
 * The two sides of a comparison with a date answer. A day is compared with a date and time by the day alone ("on 2026-03-01" holds for 2026-03-01T09:30), so a rule or condition written with a calendar date keeps meaning that day once the element asks for the time as well; two values of the same kind are compared as they are.
 */
export const getComparableDateAnswers = (left: string, right: string): [string, string] =>
  hasTimeOfDay(left) === hasTimeOfDay(right) ? [left, right] : [getAnswerDay(left), getAnswerDay(right)];
