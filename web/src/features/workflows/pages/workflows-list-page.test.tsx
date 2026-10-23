import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { initPostHogClient } from "../../../lib/telemetry/posthog";
import type { TWorkflowListItem } from "../api";
import { WORKFLOW_FILTERS_KEY } from "../lib/list-filters";
import { WorkflowsListPage } from "./workflows-list-page";

const posthog = vi.hoisted(() => ({ __loaded: false, capture: vi.fn(), init: vi.fn() }));

vi.mock("posthog-js", () => ({ default: posthog }));

const WORKSPACE_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

beforeEach(() => localStorage.clear());

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const item = (id: string, name: string): TWorkflowListItem => ({
  id,
  workspaceId: WORKSPACE_ID,
  name,
  description: null,
  status: "draft",
  triggerType: null,
  surveyId: null,
  createdBy: null,
  creator: { name: "Ada" },
  createdAt: new Date().toISOString(),
  updatedAt: new Date().toISOString(),
  lastRun: null,
  runCount: 3,
});

/** GET /api/v3/workflows answering every page with the given workflows; the requested query strings are recorded. */
const stubList = (workflows: TWorkflowListItem[]) => {
  const queries: URLSearchParams[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      const [base, query] = path.split("?");
      if (base !== "/api/v3/workflows") throw new Error(`unexpected ${path}`);
      queries.push(new URLSearchParams(query));
      return new Response(JSON.stringify({ data: workflows, meta: { limit: 12, nextCursor: null } }), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    })
  );
  return queries;
};

const renderList = (isReadOnly = false) => {
  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/workspaces/$workspaceId/workflows",
      component: () => (
        <WorkflowsListPage workspaceId={WORKSPACE_ID} isReadOnly={isReadOnly} workflowsPerPage={12} />
      ),
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/workspaces/$workspaceId/workflows/$workflowId",
      component: () => <h1>Builder</h1>,
    }),
  ]);
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [`/workspaces/${WORKSPACE_ID}/workflows`] }),
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

describe("WorkflowsListPage", () => {
  test("the first request already carries the filters this browser stored", async () => {
    localStorage.setItem(
      WORKFLOW_FILTERS_KEY,
      JSON.stringify({ searchValue: "welcome", selectedStatuses: ["archived"], sortBy: "name" })
    );
    const queries = stubList([item("wf-1", "Welcome flow")]);
    renderList();

    expect(await screen.findByText("Welcome flow")).toBeTruthy();
    expect(queries).toHaveLength(1);
    expect(queries[0]?.get("filter[name][contains]")).toBe("welcome");
    expect(queries[0]?.getAll("filter[status][in]")).toEqual(["archived"]);
    expect(queries[0]?.get("sortBy")).toBe("name");
    expect(queries[0]?.get("limit")).toBe("12");
  });

  test("a malformed stored value gives way to the defaults, which are stored over it", async () => {
    localStorage.setItem(WORKFLOW_FILTERS_KEY, "{not json");
    const queries = stubList([]);
    renderList();

    expect(await screen.findByText(i18n.t("workspace.workflows.no_workflows_title"))).toBeTruthy();
    expect(queries[0]?.get("sortBy")).toBe("updatedAt");
    expect(queries[0]?.has("filter[status][in]")).toBe(false);
    expect(JSON.parse(localStorage.getItem(WORKFLOW_FILTERS_KEY) ?? "null")).toEqual({
      searchValue: "",
      selectedStatuses: [],
      sortBy: "updatedAt",
    });
  });

  test("clearing the filters asks for the default list and an emptied filter says so", async () => {
    localStorage.setItem(
      WORKFLOW_FILTERS_KEY,
      JSON.stringify({ searchValue: "nothing", selectedStatuses: ["enabled"], sortBy: "updatedAt" })
    );
    const queries = stubList([]);
    renderList();

    expect(await screen.findByText(i18n.t("workspace.workflows.no_results_title"))).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: i18n.t("common.clear_filters") }));
    // The status applies at once, the cleared search after its debounce.
    await waitFor(() =>
      expect([
        queries.at(-1)?.has("filter[status][in]"),
        queries.at(-1)?.has("filter[name][contains]"),
      ]).toEqual([false, false])
    );
    expect(await screen.findByText(i18n.t("workspace.workflows.no_workflows_title"))).toBeTruthy();
  });

  test("rows open the builder and show their run count; a read-only member gets no row actions", async () => {
    stubList([item("wf-1", "Welcome flow")]);
    renderList(true);

    const row = (await screen.findByText("Welcome flow")).closest("a");
    expect(row?.getAttribute("href")).toBe(`/workspaces/${WORKSPACE_ID}/workflows/wf-1`);
    expect(row?.textContent).toContain("3");
    expect(screen.queryByRole("button", { name: i18n.t("common.open_options") })).toBeNull();
  });
});

describe("WorkflowsListPage analytics", () => {
  beforeAll(async () => {
    await initPostHogClient("phc_test", {});
  });

  test("reports the list once, and not the filters it started with", async () => {
    posthog.capture.mockClear();
    stubList([item("wf-1", "Welcome flow")]);
    renderList();

    await waitFor(() =>
      expect(posthog.capture).toHaveBeenCalledWith("workflow_surface_viewed", {
        surface: "list",
        workflow_id: undefined,
        workflow_status: undefined,
        is_read_only: undefined,
      })
    );
    expect(posthog.capture).toHaveBeenCalledTimes(1);
  });

  test("an emptied filter is its own surface, and clearing it is one settled filter change", async () => {
    posthog.capture.mockClear();
    localStorage.setItem(
      WORKFLOW_FILTERS_KEY,
      JSON.stringify({ searchValue: "nothing", selectedStatuses: ["archived", "enabled"], sortBy: "name" })
    );
    stubList([]);
    renderList();

    await waitFor(() =>
      expect(posthog.capture).toHaveBeenCalledWith(
        "workflow_surface_viewed",
        expect.objectContaining({ surface: "list_empty_filtered" })
      )
    );
    expect(posthog.capture.mock.calls.some(([event]) => event === "workflow_list_filtered")).toBe(false);

    fireEvent.click(screen.getByRole("button", { name: i18n.t("common.clear_filters") }));

    // The status clears at once, the search after its debounce; only the settled state is reported.
    await waitFor(() =>
      expect(posthog.capture).toHaveBeenCalledWith("workflow_list_filtered", {
        has_search: false,
        status_filter: [],
        sort_by: "name",
        includes_archived: false,
      })
    );
    expect(posthog.capture.mock.calls.filter(([event]) => event === "workflow_list_filtered")).toHaveLength(
      1
    );
    expect(JSON.stringify(posthog.capture.mock.calls)).not.toContain("nothing");
  });
});
