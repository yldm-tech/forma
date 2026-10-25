import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurvey } from "@forma/types/surveys/types";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../../lib/i18n";
import type { SurveyTrends } from "../../api";
import { TrendsCard } from "./trends-card";

const SURVEY_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e";

vi.mock("../filter/response-filter-context", () => ({
  useResponseFilter: () => ({ filterCriteria: { finished: true } }),
}));

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const survey = {
  id: SURVEY_ID,
  workspaceId: "ws",
  blocks: [
    {
      id: "b1",
      name: "B1",
      elements: [
        {
          id: "nps",
          type: TSurveyElementTypeEnum.NPS,
          headline: { default: "How likely are you to recommend us?" },
          required: false,
        },
      ],
    },
  ],
} as unknown as TSurvey;

const trends = (interval: SurveyTrends["interval"]): SurveyTrends => ({
  interval,
  timeZone: "Asia/Tokyo",
  buckets: [
    { start: "2026-03-01", end: "2026-03-01", responses: 3, completed: 2, completionPercentage: 66.67 },
    { start: "2026-03-02", end: "2026-03-02", responses: 0, completed: 0, completionPercentage: 0 },
  ],
  elements: [
    {
      elementId: "nps",
      type: "nps",
      headline: "How likely are you to recommend us?",
      metric: "nps",
      points: [
        { answered: 3, value: 33.33 },
        { answered: 0, value: null },
      ],
    },
  ],
});

/** The trend endpoint, recording each request body; answer builds the reply from it. */
const stubTrends = (answer: (body: { interval?: string }) => Response) => {
  const bodies: { filterCriteria: unknown; interval?: string }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit) => {
      if (path !== `/api/app/surveys/${SURVEY_ID}/trends`) throw new Error(`unexpected ${path}`);
      const body = JSON.parse(init.body as string) as { filterCriteria: unknown; interval?: string };
      bodies.push(body);
      return answer(body);
    })
  );
  return bodies;
};

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

const renderCard = () =>
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <TrendsCard survey={survey} locale="en-US" />
      </QueryClientProvider>
    </I18nextProvider>
  );

describe("TrendsCard", () => {
  test("asks for the current filter and lets the server pick the interval", async () => {
    const bodies = stubTrends((body) => json(trends((body.interval as SurveyTrends["interval"]) ?? "day")));
    renderCard();
    await waitFor(() => expect(screen.getAllByTestId("trend-bucket")).toHaveLength(2));
    expect(bodies[0]).toEqual({ filterCriteria: { finished: true } });
    expect(screen.getByText("Responses over time in Asia/Tokyo, following the current filter")).toBeTruthy();
    expect(screen.getByRole("radio", { name: "Day" }).getAttribute("aria-checked")).toBe("true");
    // The table for screen readers carries the bars' numbers.
    const table = screen.getByRole("table");
    expect(within(table).getByRole("rowheader", { name: "Mar 1, 2026" })).toBeTruthy();

    fireEvent.click(screen.getByRole("radio", { name: "Week" }));
    await waitFor(() =>
      expect(bodies.at(-1)).toEqual({ filterCriteria: { finished: true }, interval: "week" })
    );
    await waitFor(() =>
      expect(screen.getByRole("radio", { name: "Week" }).getAttribute("aria-checked")).toBe("true")
    );
  });

  test("shows a score element's metric, with a gap where nobody answered", async () => {
    stubTrends(() => json(trends("day")));
    renderCard();
    const select = await screen.findByRole("combobox", { name: "Metric" });
    await waitFor(() =>
      expect(
        within(select).getByRole("option", {
          name: "Net Promoter Score (NPS): How likely are you to recommend us?",
        })
      ).toBeTruthy()
    );
    fireEvent.change(select, { target: { value: "element:nps" } });
    const cells = within(screen.getByRole("table")).getAllByRole("cell");
    expect(cells.map((cell) => cell.textContent)).toEqual(["+33", "3", "No data", "0"]);
  });

  test("says when the range is too long for the interval", async () => {
    stubTrends(() => json({ code: "trend_too_many_buckets", message: "too many" }, 400));
    renderCard();
    expect(
      await screen.findByText("This range has too many periods to show at once. Group by week or month.")
    ).toBeTruthy();
  });

  test("says when there are no responses to chart", async () => {
    stubTrends(() => json({ ...trends("day"), buckets: [], elements: [] }));
    renderCard();
    expect(await screen.findByText("No responses in this range yet.")).toBeTruthy();
  });
});
