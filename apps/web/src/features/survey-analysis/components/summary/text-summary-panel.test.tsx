import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { TSurvey } from "@forma/types/surveys/types";
import type { i18n as I18n } from "i18next";
import type { ReactElement } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../../lib/i18n";
import type { TextSummary, TextSummaryState } from "../../api";
import { ResponseFilterProvider, useResponseFilter } from "../filter/response-filter-context";
import { SharedSummaryContext } from "./summary-parts";
import { TextSummaryPanel } from "./text-summary-panel";

const SURVEY_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e";

const survey = {
  id: SURVEY_ID,
  workspaceId: "workspace1",
  name: "Feedback",
  type: "link",
  status: "inProgress",
  blocks: [],
  variables: [],
  languages: [],
  welcomeCard: { enabled: false },
  endings: [],
} as unknown as TSurvey;

const summary: TextSummary = {
  elementId: "q1",
  target: "answers",
  language: "en-US",
  generatedAt: new Date(Date.now() - 5 * 60_000).toISOString(),
  answerCount: 120,
  sampledCount: 80,
  readLimit: 0,
  sentiment: { overall: "mixed", positive: 40, neutral: 20, negative: 40 },
  themes: [
    {
      title: "Pricing",
      description: "Respondents find the plans expensive.",
      count: 30,
      percentage: 37.5,
      quotes: [{ responseId: "r1", text: "Too pricey for a small team" }],
    },
  ],
};

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

/** The server: the lookup answers with lookup, a generation with generated (or what the promise it is given resolves to); every request body is recorded by path. */
const stubServer = (lookup: TextSummaryState, generated?: TextSummaryState | Promise<TextSummaryState>) => {
  const requests: { path: string; body: Record<string, unknown> }[] = [];
  const json = (body: unknown) =>
    new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      requests.push({
        path,
        body: JSON.parse(typeof init?.body === "string" ? init.body : "{}") as Record<string, unknown>,
      });
      switch (path) {
        case `/api/app/surveys/${SURVEY_ID}/text-summaries/lookup`:
          return json(lookup);
        case `/api/app/surveys/${SURVEY_ID}/text-summaries`:
          return json(await generated);
        default:
          throw new Error(`unexpected ${path}`);
      }
    })
  );
  return requests;
};

const renderPanel = async (panel: ReactElement, shared = false) => {
  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/",
      component: () => (
        <ResponseFilterProvider survey={survey}>
          <SharedSummaryContext.Provider value={shared}>{panel}</SharedSummaryContext.Provider>
        </ResponseFilterProvider>
      ),
    }),
  ]);
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/"] }) });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <RouterProvider router={router} />
      </I18nextProvider>
    </QueryClientProvider>
  );
  await router.load();
};

const panel = (answerCount = 120) => (
  <TextSummaryPanel
    survey={survey}
    elementId="q1"
    target="answers"
    answerCount={answerCount}
    locale="en-US"
    isReadOnly
  />
);

/** Switches the analysis filter between all responses and completed ones. */
const FilterSwitch = () => {
  const { selectedFilter, setSelectedFilter } = useResponseFilter();
  const complete = selectedFilter.responseStatus === "complete";
  return (
    <button
      type="button"
      onClick={() => setSelectedFilter({ ...selectedFilter, responseStatus: complete ? "all" : "complete" })}>
      {complete ? "All responses" : "Completed responses"}
    </button>
  );
};

describe("TextSummaryPanel", () => {
  test("offers a summary, says where the answers go, and shows the themes once generated", async () => {
    const requests = stubServer({ available: true, summary: null }, { available: true, summary });
    await renderPanel(panel());

    const button = await screen.findByRole("button", { name: "Summarize answers" });
    expect(screen.getByText(/to the AI provider configured for this instance/)).toBeTruthy();
    fireEvent.click(button);

    expect(await screen.findByText("Pricing")).toBeTruthy();
    expect(screen.getByText("Overall sentiment: Mixed")).toBeTruthy();
    expect(screen.getByText(/Based on a sample of 80 of 120 answers/)).toBeTruthy();
    expect(screen.getByText("37.5% of answers (30 answers)")).toBeTruthy();
    expect(screen.getByText("Generated 5 minutes ago")).toBeTruthy();
    expect(screen.getByRole("button", { name: "“Too pricey for a small team”" })).toBeTruthy();

    const generation = requests.find((r) => r.path.endsWith("/text-summaries"));
    expect(generation?.body).toMatchObject({
      elementId: "q1",
      target: "answers",
      language: "en-US",
      refresh: false,
    });
    expect(generation?.body.filterCriteria).toBeDefined();
  });

  test("refresh asks for a new summary", async () => {
    const requests = stubServer({ available: true, summary }, { available: true, summary });
    await renderPanel(panel());
    fireEvent.click(await screen.findByRole("button", { name: "Refresh summary" }));
    await vi.waitFor(() =>
      expect(requests.some((r) => r.path.endsWith("/text-summaries") && r.body.refresh === true)).toBe(true)
    );
  });

  test("a summary generated for one filter stays with that filter when the reader switches while it runs", async () => {
    let finish: (state: TextSummaryState) => void = () => {};
    const requests = stubServer(
      { available: true, summary: null },
      new Promise<TextSummaryState>((resolve) => {
        finish = resolve;
      })
    );
    await renderPanel(
      <>
        <FilterSwitch />
        {panel()}
      </>
    );

    fireEvent.click(await screen.findByRole("button", { name: "Summarize answers" }));
    expect(await screen.findByRole("status")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Completed responses" }));
    await vi.waitFor(() =>
      expect(
        requests.filter((r) => r.path.endsWith("/lookup")).map((r) => r.body.filterCriteria)
      ).toContainEqual(expect.objectContaining({ finished: true }))
    );
    // The other filter's card is not the one generating.
    const summarize = await screen.findByRole("button", { name: "Summarize answers" });
    expect((summarize as HTMLButtonElement).disabled).toBe(false);
    expect(screen.queryByRole("status")).toBeNull();

    finish({ available: true, summary });
    await vi.waitFor(() => expect(requests.filter((r) => r.path.endsWith("/text-summaries")).length).toBe(1));
    // It lands under the filter it was asked for, not the one on screen.
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(screen.queryByText("Pricing")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "All responses" }));
    expect(await screen.findByText("Pricing")).toBeTruthy();
    const generation = requests.find((r) => r.path.endsWith("/text-summaries"));
    expect(generation?.body.filterCriteria).not.toMatchObject({ finished: true });
  });

  test("a member without edit access reads the current summary but cannot refresh it", async () => {
    stubServer({ available: false, reason: "read_only", summary });
    await renderPanel(panel());
    expect(await screen.findByText("Pricing")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Refresh summary" })).toBeNull();
  });

  test("a member without edit access is told why there is nothing to generate", async () => {
    stubServer({ available: false, reason: "read_only", summary: null });
    await renderPanel(panel());
    const button = await screen.findByRole("button", { name: "Summarize answers" });
    expect((button as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText("You need edit access to this workspace to use AI smart tools.")).toBeTruthy();
  });

  test("explains why AI is unavailable and offers nothing to click", async () => {
    stubServer({ available: false, reason: "instance_not_configured", summary: null });
    await renderPanel(panel());
    const button = await screen.findByRole("button", { name: "Summarize answers" });
    expect((button as HTMLButtonElement).disabled).toBe(true);
    expect(
      screen.getByText("AI is not configured on this instance. Contact your administrator.")
    ).toBeTruthy();
    expect(screen.queryByText(/to the AI provider configured for this instance/)).toBeNull();
  });

  // Decision D1: AI is on, but the organization has not allowed it to read responses.
  test("says the organization does not allow AI to read responses, and where that is decided", async () => {
    stubServer({ available: false, reason: "response_reading_disabled", summary: null });
    await renderPanel(panel());
    const button = await screen.findByRole("button", { name: "Summarize answers" });
    expect((button as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText(/does not allow AI to read responses/)).toBeTruthy();
    expect(screen.getByText(/Allow AI to read responses/)).toBeTruthy();
  });

  test("a results link and a question nobody answered ask nothing", async () => {
    const requests = stubServer({ available: true, summary: null });
    await renderPanel(panel(), true);
    await renderPanel(panel(0));
    expect(screen.queryByRole("button", { name: "Summarize answers" })).toBeNull();
    expect(requests).toEqual([]);
  });
});
