import { SURVEY_SCHEDULING_CONFIG } from "./constants";

export interface TSurveySchedulingConfig {
  timeZone: string;
  localHour: number;
  localMinute: number;
}

const padSchedulingTimePart = (value: number): string => value.toString().padStart(2, "0");

export const getSurveySchedulingTimeLabel = (config: TSurveySchedulingConfig): string =>
  `${padSchedulingTimePart(config.localHour)}:${padSchedulingTimePart(config.localMinute)}`;

/** The schedule the server sends with the workspace (its organization's zone, or the instance's); the built-in default only when it sends none. The server's object is returned as it is, so it keeps its identity across renders. */
export const resolveSurveySchedulingConfig = (
  surveyScheduling: TSurveySchedulingConfig | undefined
): TSurveySchedulingConfig => surveyScheduling ?? SURVEY_SCHEDULING_CONFIG;

/** A scheduled moment written out in a time zone. */
export interface TScheduledMomentLabel {
  moment: string;
  timeZone: string;
}

const formatMoment = (instant: Date, timeZone: string, locale: string): string =>
  new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short", timeZone }).format(instant);

/** The instant a scheduled survey opens or closes, in the scheduling zone and, when the clock there reads differently, in the browser's zone too. */
export const describeScheduledMoment = (
  instant: Date,
  schedulingTimeZone: string,
  browserTimeZone: string,
  locale: string
): { scheduled: TScheduledMomentLabel; browser: TScheduledMomentLabel | null } => {
  const scheduled = { moment: formatMoment(instant, schedulingTimeZone, locale), timeZone: schedulingTimeZone };
  const browserMoment = formatMoment(instant, browserTimeZone, locale);
  return {
    scheduled,
    browser:
      browserTimeZone === schedulingTimeZone || browserMoment === scheduled.moment
        ? null
        : { moment: browserMoment, timeZone: browserTimeZone },
  };
};

/** The browser's IANA time zone. */
export const browserTimeZone = (): string => Intl.DateTimeFormat().resolvedOptions().timeZone;
