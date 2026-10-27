import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../../lib/i18n";
import { SharedResultsPage } from "./shared-results-page";

// The page a read-only results link opens: the summary under the link's filter, with nothing that filters, opens a response or names a respondent, and the cards the link withholds showing their counts only.

const TOKEN = "k".repeat(43);

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const label = (text: string) => ({ default: text });

const elements = {
  q1: {
    id: "q1",
    type: TSurveyElementTypeEnum.OpenText,
    headline: label("What could be better?"),
    required: false,
    inputType: "text",
  },
  nps: {
    id: "nps",
    type: TSurveyElementTypeEnum.NPS,
    headline: label("How likely are you to recommend us?"),
    required: false,
    lowerLabel: label("Low"),
    upperLabel: label("High"),
    isColorCodingEnabled: false,
  },
  ci: {
    id: "ci",
    type: TSurveyElementTypeEnum.ContactInfo,
    headline: label("Your details"),
    required: false,
  },
};

const sharedResults = {
  link: { name: "Board", expiresAt: null, filtered: true, segmentMissing: false },
  survey: {
    id: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e",
    name: "CSAT 2026",
    type: "link",
    blocks: [{ id: "b1", name: "B1", elements: Object.values(elements) }],
    endings: [],
    hiddenFields: { enabled: true, fieldIds: ["utm"] },
    variables: [],
  },
  summary: {
    meta: {
      displayCount: 4,
      totalResponses: 2,
      startsPercentage: 50,
      completedResponses: 2,
      completedPercentage: 50,
      dropOffCount: 0,
      dropOffPercentage: 0,
      ttcAverage: 0,
      quotasCompleted: 0,
      quotasCompletedPercentage: 0,
    },
    dropOff: [],
    quotas: [],
    summary: [
      {
        type: "openText",
        element: elements.q1,
        responseCount: 2,
        samples: [
          { updatedAt: "2026-01-01T12:02:00Z", value: "Cheaper", contact: null, contactAttributes: null },
          {
            updatedAt: "2026-01-01T12:01:00Z",
            value: "Faster support",
            contact: null,
            contactAttributes: null,
          },
        ],
      },
      {
        type: "nps",
        element: elements.nps,
        responseCount: 2,
        total: 2,
        answered: 2,
        score: 0,
        promoters: { count: 1, percentage: 50 },
        passives: { count: 0, percentage: 0 },
        detractors: { count: 1, percentage: 50 },
        dismissed: { count: 0, percentage: 0 },
        choices: Array.from({ length: 11 }, (_, rating) => ({ rating, count: 0, percentage: 0 })),
      },
      { type: "contactInfo", element: elements.ci, responseCount: 1, samples: [] },
      {
        type: "hiddenField",
        id: "utm",
        responseCount: 2,
        samples: [],
        breakdown: { values: [], restCount: 0, restPercentage: 0 },
      },
    ],
  },
  withheldIds: ["ci", "utm"],
};

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

/** The two link endpoints, recording each request body. */
const stubLink = (summary: () => Response) => {
  const calls: { path: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit) => {
      const body: unknown = JSON.parse(init.body as string);
      calls.push({ path, body });
      if (path === "/api/app/shared-results/summary") return summary();
      if (path === "/api/app/shared-results/trends") {
        return json({ interval: "day", timeZone: "UTC", buckets: [], elements: [] });
      }
      throw new Error(`unexpected ${path}`);
    })
  );
  return calls;
};

const renderPage = async (token: string | undefined) => {
  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/results",
      component: () => <SharedResultsPage token={token} />,
    }),
  ]);
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: ["/results"] }),
  });
  const view = render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  await router.load();
  return view;
};

describe("SharedResultsPage", () => {
  test("shows the summary through the link, without filters, responses or respondents", async () => {
    const calls = stubLink(() => json(sharedResults));
    await renderPage(TOKEN);
    expect(await screen.findByRole("heading", { name: "CSAT 2026" })).toBeTruthy();
    expect(screen.getByText("Board")).toBeTruthy();
    expect(screen.getByText("Filtered view")).toBeTruthy();
    expect(screen.queryByText(/segment that no longer exists/)).toBeNull();
    expect(calls[0]).toEqual({ path: "/api/app/shared-results/summary", body: { token: TOKEN } });

    // The revealed answers are listed without a user column.
    expect(screen.getByText("Faster support")).toBeTruthy();
    expect(screen.queryByText("User")).toBeNull();
    expect(screen.queryByText("Anonymous")).toBeNull();

    // The withheld cards keep their headers and counts, and say why the answers are missing.
    const withheld = screen.getAllByTestId("withheld-summary");
    expect(withheld).toHaveLength(2);
    expect(withheld[0]!.textContent).toContain("Your details");
    expect(withheld[1]!.textContent).toContain("utm");
    expect(screen.getAllByText("Individual answers are not shown in this shared view.")).toHaveLength(2);

    // Nothing filters: the NPS card's rows are not buttons, and the impressions are a plain number.
    const nps = screen
      .getByRole("heading", { name: "How likely are you to recommend us?" })
      .closest(".rounded-xl");
    expect(nps?.querySelectorAll('button:not([role="tab"]):not([aria-label])')).toHaveLength(0);
    expect(screen.queryByTestId("impressions-toggle")).toBeNull();

    // The trend reads through the link, and offers no download.
    await waitFor(() =>
      expect(calls.some((call) => call.path === "/api/app/shared-results/trends")).toBe(true)
    );
    expect(calls.find((call) => call.path === "/api/app/shared-results/trends")?.body).toEqual({
      token: TOKEN,
    });
    expect(screen.queryByRole("button", { name: "Download trend" })).toBeNull();
  });

  test("a link whose segment was deleted says its results may not be the ones shared", async () => {
    stubLink(() => json({ ...sharedResults, link: { ...sharedResults.link, segmentMissing: true } }));
    await renderPage(TOKEN);
    expect(await screen.findByRole("heading", { name: "CSAT 2026" })).toBeTruthy();
    expect(
      screen.getByText(
        "This link filters by a segment that no longer exists, or by one that uses a segment that no longer exists, so these results may not be the ones that were shared."
      )
    ).toBeTruthy();
  });

  test("a link that expired, was revoked or never existed says so", async () => {
    stubLink(() => json({ code: "result_share_not_found", message: "gone" }, 404));
    await renderPage(TOKEN);
    expect(await screen.findByRole("heading", { name: "Results not available" })).toBeTruthy();
    expect(
      screen.getByText(
        "This results link does not exist, has expired or was revoked. Ask the person who shared it for a new link."
      )
    ).toBeTruthy();
  });

  test("an address without a token asks for nothing", async () => {
    const calls = stubLink(() => json(sharedResults));
    await renderPage(undefined);
    expect(await screen.findByRole("heading", { name: "Results not available" })).toBeTruthy();
    expect(calls).toHaveLength(0);
  });
});
