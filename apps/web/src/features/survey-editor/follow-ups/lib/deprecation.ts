import { formatDateForDisplay } from "../../../../lib/survey/datetime";

/**
 * Survey Follow-ups are deprecated in favour of Workflows, which cover the same
 * `response completed → send email` automation through a workspace-level surface
 * (`packages/workflows` documents the 1:1 field parity of the two send-email configs).
 *
 * Nothing is removed yet: this module only decides who still gets shown the editor tab, so the
 * migration debt stops growing while everyone who relies on the feature keeps it.
 */

/** Announced removal date, ISO-8601 so the stored value stays non-localized. */
export const SURVEY_FOLLOW_UPS_SUNSET_DATE = new Date("2026-12-01T00:00:00.000Z");

/**
 * The sunset date as the announcement means it: a calendar day, not an instant.
 *
 * `formatDateForDisplay` builds an `Intl.DateTimeFormat` with no `timeZone`, so it renders in the viewer's own zone — and midnight UTC on 1 December is still 30 November anywhere west of it. Left alone, a reader in New York would be told follow-ups stop working on "Nov 30, 2026" while the deprecation announcement says 1 December. Locale still controls the formatting; only the represented calendar day is pinned, which is exactly the split AGENTS.md asks for.
 *
 * Formatting lives here rather than at the call site so a second consumer of the date cannot
 * reintroduce the shift by forgetting the zone.
 */
export const formatSurveyFollowUpsSunsetDate = (locale: string): string =>
  formatDateForDisplay(SURVEY_FOLLOW_UPS_SUNSET_DATE, locale, {
    year: "numeric",
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  });

/**
 * Whether the survey editor still offers the Follow-ups tab: only to a survey that already has follow-ups. They are live automation and their owner has to be able to read, fix and eventually migrate them; with none, new automation starts as a Workflow.
 *
 * forma2 also kept an empty tab where Workflows were unavailable; every deployment has them, so that case is gone.
 *
 * Callers pass the *persisted* count rather than the editor's working copy on purpose: the working copy soft-deletes with a `deleted` flag, so counting it would pull the tab out from under someone who just deleted their last follow-up and has not saved yet.
 */
export const shouldShowFollowUpsTab = (followUpCount: number): boolean => followUpCount > 0;
