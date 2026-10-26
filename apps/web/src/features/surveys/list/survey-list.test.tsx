import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { toast } from "../../../lib/toast";
import type { SurveyListItem, SurveySdkState } from "../api";
import { surveyFiltersKey } from "./filters";
import { SurveysList } from "./survey-list";

const WORKSPACE_A = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0a";
const WORKSPACE_B = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0b";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  // jsdom has the <dialog> element but not its modal methods, which the confirmations open with.
  HTMLDialogElement.prototype.showModal ??= function showModal(this: HTMLDialogElement) {
    this.open = true;
  };
  HTMLDialogElement.prototype.close ??= function close(this: HTMLDialogElement) {
    this.open = false;
  };
});

beforeEach(() => localStorage.clear());

afterEach(() => {
  cleanup();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  vi.unstubAllGlobals();
});

const survey = (n: number, overrides: Partial<SurveyListItem> = {}): SurveyListItem => ({
  id: `survey-${n.toString()}`,
  name: `Survey ${n.toString()}`,
  workspaceId: WORKSPACE_A,
  type: "link",
  status: "inProgress",
  publishOn: null,
  archivedAt: null,
  createdAt: "2026-10-01T09:00:00.000Z",
  updatedAt: "2026-10-02T09:00:00.000Z",
  responseCount: 4,
  completedResponseCount: 3,
  creator: { name: "Ada" },
  ...overrides,
});

const json = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });

const SDK_STATE_WITHIN_CAP: SurveySdkState = {
  limit: 100,
  liveAppSurveyCount: 3,
  excludedCount: 0,
  excluded: [],
};

/** The list endpoint answering the pages in order (by cursor), with the given counts on page one; every list request's query is recorded. The SDK state endpoint answers sdkState. */
const stubApi = ({
  pages,
  workspaceSurveyCount = pages.flat().length,
  sdkState = SDK_STATE_WITHIN_CAP,
}: {
  pages: SurveyListItem[][];
  workspaceSurveyCount?: number;
  sdkState?: SurveySdkState;
}) => {
  const queries: URLSearchParams[] = [];
  const mutations: string[] = [];
  const total = pages.flat().length;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      const [base, query] = path.split("?");
      const method = init?.method ?? "GET";
      if (method !== "GET") {
        mutations.push(`${method} ${path} ${typeof init?.body === "string" ? init.body : ""}`.trim());
        const id = base?.split("/")[4] ?? "";
        return json({ data: { id, status: "paused", archivedAt: null } });
      }
      if (base === "/api/app/public-domain") return json({ publicDomain: "http://localhost" });
      if (base?.endsWith("/ai")) return json({ available: false, reason: "not_enabled" });
      if (base?.endsWith("/sdk-state")) return json(sdkState);
      if (base !== "/api/v3/surveys") throw new Error(`unexpected ${path}`);
      const params = new URLSearchParams(query);
      queries.push(params);
      const cursor = params.get("cursor");
      const index = cursor ? Number(cursor) : 0;
      const isFirst = index === 0;
      return json({
        data: pages[index] ?? [],
        meta: {
          limit: 12,
          nextCursor: index + 1 < pages.length ? String(index + 1) : null,
          totalCount: isFirst ? total : null,
          workspaceSurveyCount: isFirst ? workspaceSurveyCount : null,
        },
      });
    })
  );
  return Object.assign(queries, { mutations });
};

const renderList = (
  workspaceId = WORKSPACE_A,
  channel: "link" | "app" | null = null,
  { canSaveAsTemplate = false }: { canSaveAsTemplate?: boolean } = {}
) => {
  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/workspaces/$workspaceId/surveys",
      component: () => (
        <SurveysList
          workspaceId={workspaceId}
          organizationId="org-1"
          isReadOnly={false}
          canSaveAsTemplate={canSaveAsTemplate}
          channel={channel}
          surveyType="link"
          defaultLanguage="default"
        />
      ),
    }),
  ]);
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [`/workspaces/${workspaceId}/surveys`] }),
  });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
};

describe("SurveysList filters", () => {
  test("each workspace keeps its own filters, and a name search is not carried over", async () => {
    localStorage.setItem(
      surveyFiltersKey(WORKSPACE_A),
      JSON.stringify({ name: "zzzz", status: ["draft"], type: [], sortBy: "name" })
    );
    const queries = stubApi({ pages: [[survey(1)]] });
    renderList(WORKSPACE_B);

    expect(await screen.findByRole("link", { name: "Survey 1" })).toBeTruthy();
    expect(queries[0]?.has("filter[status][in]")).toBe(false);
    expect(queries[0]?.has("filter[name][contains]")).toBe(false);
    expect(queries[0]?.get("sortBy")).toBe("relevance");
    cleanup();

    const queriesA = stubApi({ pages: [[survey(1, { status: "draft" })]] });
    renderList(WORKSPACE_A);
    expect(await screen.findByRole("link", { name: "Survey 1" })).toBeTruthy();
    expect(queriesA[0]?.getAll("filter[status][in]")).toEqual(["draft"]);
    expect(queriesA[0]?.has("filter[name][contains]")).toBe(false);
    expect(queriesA[0]?.get("sortBy")).toBe("name");
    // What is stored again leaves the name out.
    expect(JSON.parse(localStorage.getItem(surveyFiltersKey(WORKSPACE_A)) ?? "null")).toEqual({
      status: ["draft"],
      type: [],
      sortBy: "name",
    });
  });

  test("the status trigger says how many statuses are on", async () => {
    localStorage.setItem(
      surveyFiltersKey(WORKSPACE_A),
      JSON.stringify({ status: ["draft", "paused"], type: [], sortBy: "relevance" })
    );
    stubApi({ pages: [[survey(1, { status: "draft" })]] });
    renderList();

    const trigger = await screen.findByRole("button", { name: /^Status/ });
    expect(trigger.textContent).toContain("2");
    expect(trigger.textContent).toContain(i18n.t("workspace.surveys.filter_selected_count", { count: 2 }));
  });

  test("filters that empty the list say so, and Clear filters asks for the whole list again", async () => {
    localStorage.setItem(
      surveyFiltersKey(WORKSPACE_A),
      JSON.stringify({ status: ["completed"], type: [], sortBy: "relevance" })
    );
    const queries = stubApi({ pages: [[]], workspaceSurveyCount: 5 });
    renderList();

    expect(await screen.findByText(i18n.t("workspace.surveys.no_surveys_match_filters"))).toBeTruthy();
    // The toolbar's Clear filters and the empty state's; either resets.
    const clear = screen.getAllByRole("button", { name: i18n.t("common.clear_filters") });
    fireEvent.click(clear.at(-1)!);
    await waitFor(() => expect(queries.at(-1)?.has("filter[status][in]")).toBe(false));
    // The pressed button is gone; the focus is in the search box rather than on the page.
    expect(document.activeElement).toBe(
      screen.getByRole("searchbox", { name: i18n.t("workspace.surveys.search_by_survey_name") })
    );
  });
});

describe("SurveysList rows", () => {
  test("a row's link is named by its survey alone, and its menu names the survey too", async () => {
    stubApi({ pages: [[survey(1, { name: "Product Feedback" })]] });
    renderList();

    const table = await screen.findByRole("table", { name: i18n.t("common.surveys") });
    const link = within(table).getByRole("link", { name: "Product Feedback" });
    expect(link.getAttribute("href")).toBe(`/workspaces/${WORKSPACE_A}/surveys/survey-1/summary`);
    expect(link.getAttribute("title")).toBe("Product Feedback");
    expect(
      within(table).getByRole("button", {
        name: i18n.t("workspace.surveys.open_options_for", { name: "Product Feedback" }),
      })
    ).toBeTruthy();
    expect(within(table).getAllByRole("columnheader").length).toBeGreaterThan(1);
    // The completed column is named by its label; the hint is the tooltip, not part of the name a screen reader repeats with every cell.
    expect(
      within(table).getByRole("columnheader", { name: i18n.t("workspace.surveys.completed_responses") })
    ).toBeTruthy();
    expect(within(table).getAllByRole("row")).toHaveLength(2);
  });

  test("Load more hands the focus to the first survey it added and the count follows", async () => {
    const firstPage = Array.from({ length: 12 }, (_, i) => survey(i + 1));
    stubApi({ pages: [firstPage, [survey(13)]] });
    renderList();

    expect(
      await screen.findByText(i18n.t("workspace.surveys.showing_count", { shown: 12, total: 13 }))
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: i18n.t("common.load_more") }));
    await waitFor(() => expect(document.activeElement?.textContent).toBe("Survey 13"));
    expect(
      screen.getByText(i18n.t("workspace.surveys.showing_count", { shown: 13, total: 13 }))
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: i18n.t("common.load_more") })).toBeNull();
  });
});

describe("SurveysList archive", () => {
  test("Undo in the archive toast restores the survey and runs it again if it was running", async () => {
    const api = stubApi({ pages: [[survey(1, { name: "Live survey", status: "inProgress" })]] });
    renderList();

    const trigger = await screen.findByRole("button", {
      name: i18n.t("workspace.surveys.open_options_for", { name: "Live survey" }),
    });
    fireEvent.keyDown(trigger, { key: "Enter" });
    fireEvent.click(await screen.findByRole("menuitem", { name: i18n.t("workspace.surveys.archive") }));
    fireEvent.click(
      await screen.findByRole("button", { name: i18n.t("workspace.surveys.stop_and_archive") })
    );

    await waitFor(() => expect(toast.getSnapshot().some((item) => item.action)).toBe(true));
    const archived = toast.getSnapshot().find((item) => item.action);
    expect(archived?.action?.label).toBe(i18n.t("workspace.surveys.undo_archive"));
    archived?.action?.onClick();

    await waitFor(() =>
      expect(api.mutations).toEqual([
        "POST /api/v3/surveys/survey-1/archive",
        "POST /api/v3/surveys/survey-1/restore",
        'PATCH /api/v3/surveys/survey-1 {"status":"inProgress"}',
      ])
    );
  });
});

/** Whether the stubbed fetch has been asked for the SDK state. */
const sdkStateRequested = () =>
  vi.mocked(fetch).mock.calls.some(([path]) => typeof path === "string" && path.endsWith("/sdk-state"));

describe("SurveysList SDK state", () => {
  test("live app surveys past the SDK state's cap are named, linked to their summaries, and counted", async () => {
    stubApi({
      pages: [[survey(1)]],
      sdkState: {
        limit: 100,
        liveAppSurveyCount: 103,
        excludedCount: 3,
        excluded: [
          { id: "old-1", name: "Old checkout survey" },
          { id: "old-2", name: "Old onboarding survey" },
        ],
      },
    });
    renderList();

    const warning = await screen.findByTestId("sdk-state-truncated");
    expect(warning.textContent).toContain(
      i18n.t("workspace.surveys.list.state_truncated_title", { count: 3 })
    );
    expect(warning.textContent).toContain(i18n.t("workspace.surveys.list.state_truncated", { limit: 100 }));
    expect(within(warning).getByRole("link", { name: "Old checkout survey" }).getAttribute("href")).toBe(
      `/workspaces/${WORKSPACE_A}/surveys/old-1/summary`
    );
    expect(within(warning).getByRole("link", { name: "Old onboarding survey" })).toBeTruthy();
    expect(warning.textContent).toContain(
      i18n.t("workspace.surveys.list.state_truncated_more", { count: 1 })
    );
  });

  test("nothing is said while every live app survey reaches the SDK, nor in a link-only workspace", async () => {
    stubApi({ pages: [[survey(1)]] });
    renderList();
    await screen.findByText("Survey 1");
    await waitFor(() => expect(sdkStateRequested()).toBe(true));
    expect(screen.queryByTestId("sdk-state-truncated")).toBeNull();
    cleanup();

    stubApi({
      pages: [[survey(1)]],
      sdkState: {
        limit: 100,
        liveAppSurveyCount: 101,
        excludedCount: 1,
        excluded: [{ id: "old", name: "Old" }],
      },
    });
    renderList(WORKSPACE_A, "link");
    await screen.findByText("Survey 1");
    expect(screen.queryByTestId("sdk-state-truncated")).toBeNull();
    expect(sdkStateRequested()).toBe(false);
  });
});

describe("SurveysList save as template", () => {
  test("an owner or a manager saves a survey as a template under the name and description given", async () => {
    const api = stubApi({ pages: [[survey(1, { name: "Release NPS" })]] });
    renderList(WORKSPACE_A, null, { canSaveAsTemplate: true });

    const trigger = await screen.findByRole("button", {
      name: i18n.t("workspace.surveys.open_options_for", { name: "Release NPS" }),
    });
    fireEvent.keyDown(trigger, { key: "Enter" });
    fireEvent.click(
      await screen.findByRole("menuitem", {
        name: i18n.t("workspace.surveys.organization_templates.save_as_template"),
      })
    );
    const name = await screen.findByRole<HTMLInputElement>("textbox", { name: i18n.t("common.name") });
    expect(name.value).toBe("Release NPS");
    fireEvent.change(name, { target: { value: "  " } });
    fireEvent.click(
      screen.getByRole("button", { name: i18n.t("workspace.surveys.organization_templates.save_template") })
    );
    expect(
      await screen.findByText(i18n.t("workspace.surveys.organization_templates.name_required"))
    ).toBeTruthy();
    expect(api.mutations).toEqual([]);

    fireEvent.change(name, { target: { value: " Quarterly NPS " } });
    fireEvent.change(screen.getByRole("textbox", { name: i18n.t("common.description") }), {
      target: { value: "Run after every release" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: i18n.t("workspace.surveys.organization_templates.save_template") })
    );
    await waitFor(() =>
      expect(api.mutations).toEqual([
        'POST /api/app/surveys/survey-1/save-as-template {"name":"Quarterly NPS","description":"Run after every release"}',
      ])
    );
    await waitFor(() =>
      expect(
        toast
          .getSnapshot()
          .some((item) => item.message === i18n.t("workspace.surveys.organization_templates.saved"))
      ).toBe(true)
    );
  });

  test("a member who is not an owner or a manager is not offered it", async () => {
    stubApi({ pages: [[survey(1, { name: "Release NPS" })]] });
    renderList();

    const trigger = await screen.findByRole("button", {
      name: i18n.t("workspace.surveys.open_options_for", { name: "Release NPS" }),
    });
    fireEvent.keyDown(trigger, { key: "Enter" });
    expect(await screen.findByRole("menuitem", { name: i18n.t("workspace.surveys.archive") })).toBeTruthy();
    expect(
      screen.queryByRole("menuitem", {
        name: i18n.t("workspace.surveys.organization_templates.save_as_template"),
      })
    ).toBeNull();
  });
});
