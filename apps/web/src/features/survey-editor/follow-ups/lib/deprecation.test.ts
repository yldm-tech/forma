import { describe, expect, test } from "vite-plus/test";
import {
  SURVEY_FOLLOW_UPS_SUNSET_DATE,
  formatSurveyFollowUpsSunsetDate,
  shouldShowFollowUpsTab,
} from "./deprecation";

describe("shouldShowFollowUpsTab", () => {
  test("keeps the tab for a survey that already has follow-ups", () => {
    expect(shouldShowFollowUpsTab(1)).toBe(true);
    expect(shouldShowFollowUpsTab(2)).toBe(true);
  });

  test("hides the empty tab, since new automation starts as a Workflow", () => {
    expect(shouldShowFollowUpsTab(0)).toBe(false);
  });
});

describe("SURVEY_FOLLOW_UPS_SUNSET_DATE", () => {
  test("is the announced removal date", () => {
    expect(SURVEY_FOLLOW_UPS_SUNSET_DATE.toISOString()).toBe("2026-12-01T00:00:00.000Z");
  });
});

describe("formatSurveyFollowUpsSunsetDate", () => {
  test("renders the announced calendar day, not the viewer's local one", () => {
    // Midnight UTC on 1 December is still 30 November west of UTC. Without an explicit `timeZone`
    // the notice would tell a reader in New York the feature stops on Nov 30 while the docs and the
    // announcement both say 1 December.
    expect(formatSurveyFollowUpsSunsetDate("en-US")).toBe("Dec 1, 2026");
  });

  test("keeps the same calendar day in a negative-offset zone", () => {
    // Belt and braces: the assertion above already pins the rendered string, but this proves the
    // zone is the reason rather than whatever TZ the test runner happens to use.
    const formatted = new Intl.DateTimeFormat("en-US", {
      year: "numeric",
      month: "short",
      day: "numeric",
      timeZone: "America/Los_Angeles",
    }).format(SURVEY_FOLLOW_UPS_SUNSET_DATE);

    expect(formatted).toBe("Nov 30, 2026");
    expect(formatSurveyFollowUpsSunsetDate("en-US")).toBe("Dec 1, 2026");
  });

  test("still localizes the formatting", () => {
    // Only the calendar day is pinned; locale keeps controlling how it is written.
    expect(formatSurveyFollowUpsSunsetDate("de-DE")).toBe("1. Dez. 2026");
  });
});
