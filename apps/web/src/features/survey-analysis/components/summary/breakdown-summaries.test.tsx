import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type {
  TSurvey,
  TSurveyElementSummaryAddress,
  TSurveyElementSummaryHiddenFields,
  TSurveyElementSummaryMultipleChoice,
} from "@forma/types/surveys/types";
import type { i18n as I18n } from "i18next";
import type { ReactElement } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test } from "vite-plus/test";
import { initI18n } from "../../../../lib/i18n";
import { MultipleChoiceSummary } from "./choice-summaries";
import { ArraySamplesSummary, HiddenFieldsSummary } from "./sample-summaries";

// insights-10: a hidden field's values and the values typed into Other are counted into bars, with the answers one by one behind the toggle, and the address and contact cards page like every other sample card.

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(cleanup);

const survey = {
  id: "survey1",
  workspaceId: "workspace1",
  name: "Breakdowns",
  type: "link",
  status: "inProgress",
  blocks: [],
  variables: [],
  languages: [],
  welcomeCard: { enabled: false },
  endings: [],
} as unknown as TSurvey;

const renderCard = async (card: ReactElement) => {
  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({ getParentRoute: () => rootRoute, path: "/", component: () => card }),
  ]);
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/"] }) });
  render(
    <I18nextProvider i18n={i18n}>
      <RouterProvider router={router} />
    </I18nextProvider>
  );
  await router.load();
};

const sample = (value: string, minute: number) => ({
  updatedAt: new Date(Date.UTC(2026, 9, 1, 12, minute)),
  value,
  contact: null,
  contactAttributes: {},
});

const hiddenSummary = (
  breakdown: TSurveyElementSummaryHiddenFields["breakdown"],
  values: string[]
): TSurveyElementSummaryHiddenFields => ({
  type: "hiddenField",
  id: "utm_source",
  responseCount: values.length,
  samples: values.map(sample),
  breakdown,
});

/** The bars of a breakdown, each as "value | count | share". */
const bars = () =>
  screen.getAllByRole("listitem").map((item) =>
    Array.from(item.querySelectorAll("p"))
      .map((p) => p.textContent)
      .join(" | ")
  );

describe("HiddenFieldsSummary", () => {
  test("counts repeating values into bars, the rest added up, and lists the answers behind the toggle", async () => {
    await renderCard(
      <HiddenFieldsSummary
        elementSummary={hiddenSummary(
          {
            values: [
              { value: "twitter", count: 3, percentage: 50 },
              { value: "newsletter", count: 2, percentage: 33.33 },
            ],
            restCount: 1,
            restPercentage: 16.67,
          },
          ["twitter", "newsletter", "twitter", "partner", "newsletter", "twitter"]
        )}
        workspaceId="workspace1"
        locale="en-US"
      />
    );
    expect(bars()).toEqual([
      "twitter | 3 responses | 50%",
      "newsletter | 2 responses | 33.33%",
      "All other values | 1 response | 16.67%",
    ]);

    fireEvent.click(screen.getByRole("tab", { name: "Individual" }));
    expect(screen.queryAllByRole("listitem")).toHaveLength(0);
    expect(screen.getAllByText("twitter")).toHaveLength(3);
    expect(screen.getByText("partner")).toBeTruthy();
  });

  test("opens on the answers when no value repeats", async () => {
    await renderCard(
      <HiddenFieldsSummary
        elementSummary={hiddenSummary(
          {
            values: [
              { value: "a1", count: 1, percentage: 50 },
              { value: "b2", count: 1, percentage: 50 },
            ],
            restCount: 0,
            restPercentage: 0,
          },
          ["a1", "b2"]
        )}
        workspaceId="workspace1"
        locale="en-US"
      />
    );
    expect(screen.getByRole("tab", { name: "Individual" }).getAttribute("aria-selected")).toBe("true");
    expect(screen.queryAllByRole("listitem")).toHaveLength(0);
    expect(screen.getByText("a1")).toBeTruthy();
  });
});

describe("MultipleChoiceSummary", () => {
  test("groups the values typed into Other, the list one toggle away", async () => {
    const element = {
      id: "plan",
      type: TSurveyElementTypeEnum.MultipleChoiceSingle,
      headline: { default: "Which plan are you on?" },
      required: true,
      choices: [
        { id: "free", label: { default: "Free" } },
        { id: "other", label: { default: "Other" } },
      ],
      shuffleOption: "none",
    };
    const typed = ["Education", "Nonprofit", "Education", "Nonprofit", "Nonprofit", "Startup program"];
    const summary = {
      type: TSurveyElementTypeEnum.MultipleChoiceSingle,
      element,
      responseCount: 8,
      selectionCount: 8,
      choices: [
        { value: "Free", count: 2, percentage: 25 },
        {
          value: "Other",
          count: 6,
          percentage: 75,
          others: typed.map((value) => ({ value, contact: null, contactAttributes: {} })),
          othersBreakdown: {
            values: [
              { value: "Nonprofit", count: 3, percentage: 50 },
              { value: "Education", count: 2, percentage: 33.33 },
              { value: "Startup program", count: 1, percentage: 16.67 },
            ],
            restCount: 0,
            restPercentage: 0,
          },
        },
      ],
    } as unknown as TSurveyElementSummaryMultipleChoice;
    await renderCard(<MultipleChoiceSummary elementSummary={summary} survey={survey} setFilter={() => {}} />);
    expect(bars()).toEqual([
      "Nonprofit | 3 selections | 50%",
      "Education | 2 selections | 33.33%",
      "Startup program | 1 selection | 16.67%",
    ]);

    fireEvent.click(screen.getByRole("tab", { name: "Individual" }));
    expect(screen.queryAllByRole("listitem")).toHaveLength(0);
    expect(screen.getAllByText("Nonprofit")).toHaveLength(3);
  });
});

describe("ArraySamplesSummary", () => {
  test("pages the answers ten at a time", async () => {
    const element = {
      id: "address",
      type: TSurveyElementTypeEnum.Address,
      headline: { default: "Where do you live?" },
      required: true,
    };
    const summary = {
      type: TSurveyElementTypeEnum.Address,
      element,
      responseCount: 12,
      samples: Array.from({ length: 12 }, (_, i) => ({
        id: `r${i}`,
        updatedAt: new Date(Date.UTC(2026, 9, 1, 12, i)),
        value: [`${i} Main Street`],
        contact: null,
        contactAttributes: {},
      })),
    } as unknown as TSurveyElementSummaryAddress;
    await renderCard(<ArraySamplesSummary elementSummary={summary} survey={survey} locale="en-US" />);
    expect(screen.getAllByText(/Main Street$/)).toHaveLength(10);
    fireEvent.click(screen.getByRole("button", { name: "Load more" }));
    expect(screen.getAllByText(/Main Street$/)).toHaveLength(12);
    expect(within(document.body).queryByRole("button", { name: "Load more" })).toBeNull();
  });
});
