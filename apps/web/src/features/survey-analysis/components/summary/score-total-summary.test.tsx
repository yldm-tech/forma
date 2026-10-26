import { cleanup, render, screen, within } from "@testing-library/react";
import type { TSurveyScoreSummary } from "@forma/types/surveys/types";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test } from "vite-plus/test";
import { initI18n } from "../../../../lib/i18n";
import { ScoreTotalSummary, scoreBinLabel } from "./score-total-summary";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
});

const score: TSurveyScoreSummary = {
  variableId: "total",
  name: "score",
  responseCount: 3,
  average: 1.17,
  median: 1.5,
  min: -3,
  max: 5,
  bins: [
    { from: -3, to: -3, count: 1, percentage: 33.33 },
    { from: 1.5, to: 1.5, count: 1, percentage: 33.33 },
    { from: 5, to: 5, count: 1, percentage: 33.33 },
  ],
};

const renderCard = (s: TSurveyScoreSummary) =>
  render(
    <I18nextProvider i18n={i18n}>
      <ScoreTotalSummary score={s} />
    </I18nextProvider>
  );

describe("ScoreTotalSummary", () => {
  test("states the scored responses, the average, the extremes and a bar per score", () => {
    renderCard(score);
    const card = screen.getByRole("region", { name: "Total score" });
    expect(within(card).getByText("3 responses")).toBeTruthy();
    expect(within(card).getByText(/Average/).textContent).toBe("Average: 1.17");
    expect(within(card).getByText("Lowest").nextElementSibling?.textContent).toBe("-3");
    expect(within(card).getByText("Highest").nextElementSibling?.textContent).toBe("5");
    expect(within(card).getAllByRole("listitem")).toHaveLength(3);
    expect(within(card).getByText(/the score variable/)).toBeTruthy();
  });

  test("says so when no response under the filter has a score", () => {
    renderCard({ ...score, responseCount: 0, bins: [] });
    expect(screen.getByText("No completed response under this filter has a score yet.")).toBeTruthy();
  });

  test("labels a grouped bin by its range", () => {
    expect(scoreBinLabel({ from: 0, to: 10, count: 2, percentage: 50 })).toBe("0 – 10");
    expect(scoreBinLabel({ from: 4, to: 4, count: 2, percentage: 50 })).toBe("4");
  });
});
