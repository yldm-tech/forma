import type { TSurveySchedulingConfig } from "./scheduling-config";

/** forma2's SURVEY_BG_COLORS: the swatches the background color picker offers. */
export const SURVEY_BG_COLORS = [
  "#FFFFFF",
  "#FFF2D8",
  "#EAD7BB",
  "#BCA37F",
  "#113946",
  "#04364A",
  "#176B87",
  "#64CCC5",
  "#DAFFFB",
  "#132043",
  "#1F4172",
  "#F1B4BB",
  "#FDF0F0",
  "#001524",
  "#445D48",
  "#D6CC99",
  "#FDE5D4",
  "#BEADFA",
  "#D0BFFF",
  "#DFCCFB",
  "#FFF8C9",
  "#FF8080",
  "#FFCF96",
  "#F6FDC3",
  "#CDFAD5",
];

/**
 * When scheduled surveys open and close when the server does not say: the defaults of SURVEY_SCHEDULING_TIME_ZONE, _LOCAL_HOUR and _LOCAL_MINUTE, which forma2 and the Go server share. The server sends the schedule in effect with the workspace (the organization's zone, or the instance's) and snaps every publish and close date to it itself; the editor uses it to show the day and time a date stands for.
 */
export const SURVEY_SCHEDULING_CONFIG: TSurveySchedulingConfig = {
  timeZone: "Europe/Berlin",
  localHour: 0,
  localMinute: 0,
};
