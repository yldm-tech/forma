import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
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
    {
      start: "2026-03-01",
      end: "2026-03-01",
      partial: false,
      responses: 3,
      completed: 2,
      completionPercentage: 66.67,
    },
    {
      start: "2026-03-02",
      end: "2026-03-02",
      partial: false,
      responses: 0,
      completed: 0,
      completionPercentage: 0,
    },
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
  score: null,
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

const radio = (name: string) => screen.getByRole("radio", { name }) as HTMLInputElement;

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
    expect(radio("Day").checked).toBe(true);
    // The table for screen readers carries the bars' numbers.
    const table = screen.getByRole("table");
    expect(within(table).getByRole("rowheader", { name: "Mar 1, 2026" })).toBeTruthy();

    fireEvent.click(radio("Week"));
    // The pick shows at once, before the week's trend arrives.
    expect(radio("Week").checked).toBe(true);
    await waitFor(() =>
      expect(bodies.at(-1)).toEqual({ filterCriteria: { finished: true }, interval: "week" })
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

  test("says when the responses span too many buckets, with advice that fits the interval picked", async () => {
    stubTrends((body) =>
      body.interval
        ? json({ code: "trend_too_many_buckets", message: "too many" }, 400)
        : json(trends("week"))
    );
    renderCard();
    await waitFor(() => expect(screen.getAllByTestId("trend-bucket")).toHaveLength(2));
    fireEvent.click(radio("Day"));
    expect(
      await screen.findByText("These responses span too many days to show at once. Group by week or month.")
    ).toBeTruthy();
    fireEvent.click(radio("Month"));
    // Month is the longest interval: the advice is a shorter range, not another interval.
    expect(
      await screen.findByText(
        "These responses span too many months to show at once. Choose a shorter date range."
      )
    ).toBeTruthy();
  });

  test("the plot shows a bucket's numbers from the keyboard", async () => {
    stubTrends(() => json(trends("day")));
    renderCard();
    // The plot is one tab stop, a slider over the buckets.
    const plot = await screen.findByRole("slider", { name: "Responses, grouped by Day" });
    expect(plot.tabIndex).toBe(0);
    expect(plot.getAttribute("aria-valuetext")).toBe("Mar 2, 2026: 0 responses");
    // Focus opens the latest bucket's tooltip; the arrow keys move it.
    act(() => plot.focus());
    expect(screen.getByRole("tooltip").textContent).toContain("Mar 2, 2026");
    fireEvent.keyDown(plot, { key: "ArrowLeft" });
    expect(screen.getByRole("tooltip").textContent).toContain("Mar 1, 2026");
    expect(screen.getByRole("tooltip").textContent).toContain("3 responses");
    expect(plot.getAttribute("aria-valuenow")).toBe("0");
    expect(plot.getAttribute("aria-valuetext")).toBe("Mar 1, 2026: 3 responses");
    fireEvent.keyDown(plot, { key: "End" });
    expect(screen.getByRole("tooltip").textContent).toContain("Mar 2, 2026");
    act(() => plot.blur());
    expect(screen.queryByRole("tooltip")).toBeNull();
  });

  test("marks a partial period", async () => {
    const partial = trends("day");
    partial.buckets[0] = { ...partial.buckets[0]!, partial: true };
    stubTrends(() => json(partial));
    renderCard();
    expect(await screen.findByText("Partial period")).toBeTruthy();
    expect(
      within(screen.getByRole("table")).getByRole("rowheader", { name: "Mar 1, 2026 (partial)" })
    ).toBeTruthy();
    fireEvent.mouseEnter(screen.getAllByTestId("trend-bucket")[0]!);
    expect(screen.getByRole("tooltip").textContent).toContain("only part of it is in the range");
  });

  test("draws a slider's average on its own scale, and the average total score of the completed responses", async () => {
    const scored: SurveyTrends = {
      ...trends("day"),
      elements: [
        {
          elementId: "temp",
          type: "slider",
          headline: "How warm?",
          metric: "average",
          points: [
            { answered: 3, value: 70 },
            { answered: 0, value: null },
          ],
        },
      ],
      score: {
        variableId: "total",
        name: "score",
        points: [
          { answered: 2, value: 7.5 },
          { answered: 0, value: null },
        ],
      },
    };
    stubTrends(() => json(scored));
    render(
      <I18nextProvider i18n={i18n}>
        <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
          <TrendsCard
            survey={
              {
                ...survey,
                blocks: [
                  {
                    id: "b1",
                    name: "B1",
                    elements: [
                      {
                        id: "temp",
                        type: TSurveyElementTypeEnum.Slider,
                        headline: { default: "How warm?" },
                        required: false,
                        min: 0,
                        max: 100,
                        step: 1,
                      },
                    ],
                  },
                ],
              } as unknown as TSurvey
            }
            locale="en-US"
          />
        </QueryClientProvider>
      </I18nextProvider>
    );
    const select = await screen.findByRole("combobox", { name: "Metric" });
    await waitFor(() =>
      expect(within(select).getByRole("option", { name: "Total score: score" })).toBeTruthy()
    );

    // The slider's axis runs over its scale, 0 to 100, not from 1 to the largest average as a rating's does.
    fireEvent.change(select, { target: { value: "element:temp" } });
    expect(within(screen.getByRole("table")).getAllByRole("cell")[0]!.textContent).toBe("70.00");
    expect(screen.getByText("100")).toBeTruthy();
    expect(screen.getByText("50")).toBeTruthy();

    fireEvent.change(select, { target: { value: "score" } });
    const table = screen.getByRole("table");
    expect(
      within(table)
        .getAllByRole("cell")
        .map((cell) => cell.textContent)
    ).toEqual(["7.50", "2", "No data", "0"]);
    expect(within(table).getByRole("columnheader", { name: "Scored responses" })).toBeTruthy();
    expect(
      screen.getByText("Average total score in each period, over the completed responses that have a score.")
    ).toBeTruthy();
    fireEvent.mouseEnter(screen.getAllByTestId("trend-bucket")[0]!);
    expect(screen.getByRole("tooltip").textContent).toContain("2 scored responses");
  });

  test("says when there are no responses to chart", async () => {
    stubTrends(() => json({ ...trends("day"), buckets: [], elements: [] }));
    renderCard();
    expect(await screen.findByText("No responses in this range yet.")).toBeTruthy();
  });
});
