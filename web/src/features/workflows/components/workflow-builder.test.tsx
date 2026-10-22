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
import { Provider as JotaiProvider } from "jotai";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { toast } from "../../../lib/toast";
import type { TWorkflowResource } from "../api";
import type { TWorkflowDefinition } from "../definition";
import { useWorkflowBuilder } from "../hooks/use-workflow-builder";
import { WorkflowHeaderCta } from "./workflow-header-cta";
import { WorkflowPageTitle } from "./workflow-page-title";

const WORKSPACE_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f";
const WORKFLOW_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d01";
const SURVEY_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e";
const BUILDER_PATH = `/workspaces/${WORKSPACE_ID}/workflows/${WORKFLOW_ID}`;

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  // Radix's popper measures its anchor; jsdom has no layout to observe.
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    }
  );
});

beforeEach(() => {
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const executableDefinition: TWorkflowDefinition = {
  schemaVersion: 1,
  trigger: {
    id: "trigger-1",
    type: "trigger",
    triggerType: "response.completed",
    config: { surveyId: SURVEY_ID, endingCardIds: [] },
  },
  nodes: [
    {
      id: "email-1",
      type: "action",
      actionType: "send_email",
      config: {
        to: "q1",
        from: "team@example.com",
        replyTo: [],
        subject: "Thanks",
        body: "<p>Thanks for your answer</p>",
        attachResponseData: false,
      },
    },
  ],
  edges: [{ id: "edge-1", source: "trigger-1", target: "email-1" }],
  entryNodeId: "trigger-1",
};

const emptyDefinition: TWorkflowDefinition = {
  schemaVersion: 1,
  trigger: null,
  nodes: [],
  edges: [],
  entryNodeId: null,
};

const workflowResource = (definition: TWorkflowDefinition): TWorkflowResource => ({
  id: WORKFLOW_ID,
  workspaceId: WORKSPACE_ID,
  name: "Thank respondents",
  description: null,
  status: "draft",
  triggerType: definition.trigger ? "response.completed" : null,
  surveyId: definition.trigger?.config.surveyId ?? null,
  createdBy: null,
  creator: null,
  createdAt: "2026-10-01T00:00:00.000Z",
  updatedAt: "2026-10-01T00:00:00.000Z",
  lastRun: null,
  runCount: 0,
  definition,
});

interface Recorded {
  method: string;
  path: string;
  body: Record<string, unknown> | null;
}

const problem = (status: number, detail: string) =>
  new Response(JSON.stringify({ title: "Bad Request", status, detail, code: "bad_request" }), {
    status,
    headers: { "content-type": "application/problem+json" },
  });

/** /api/v3/workflows as the Go server answers it: GET returns the workflow, PATCH applies the body, the lifecycle endpoints move the status. patchStatus makes every PATCH fail with that status. */
const stubWorkflowsApi = (initial: TWorkflowResource, { patchStatus }: { patchStatus?: number } = {}) => {
  let workflow = initial;
  const requests: Recorded[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      const method = init.method ?? "GET";
      const body = typeof init.body === "string" ? (JSON.parse(init.body) as Record<string, unknown>) : null;
      requests.push({ method, path, body });
      const ok = (resource: TWorkflowResource) =>
        new Response(JSON.stringify({ data: resource }), {
          status: 200,
          headers: { "content-type": "application/json" },
        });
      if (method === "GET" && path === `/api/v3/workflows/${WORKFLOW_ID}`) return ok(workflow);
      if (method === "PATCH" && path === `/api/v3/workflows/${WORKFLOW_ID}`) {
        if (patchStatus) return problem(patchStatus, "The request payload is invalid.");
        workflow = {
          ...workflow,
          ...(body as Partial<TWorkflowResource>),
          updatedAt: new Date().toISOString(),
        };
        return ok(workflow);
      }
      if (method === "POST" && path === `/api/v3/workflows/${WORKFLOW_ID}/enable`) {
        workflow = { ...workflow, status: "enabled" };
        return ok(workflow);
      }
      throw new Error(`unexpected ${method} ${path}`);
    })
  );
  return requests;
};

/** The builder page's header (title and status menu) over a builder that loads and autosaves, as the workflow's layout and editor tab mount them. */
const renderBuilder = () => {
  const Builder = () => {
    useWorkflowBuilder({ workspaceId: WORKSPACE_ID, workflowId: WORKFLOW_ID, isReadOnly: false });
    return (
      <>
        <WorkflowPageTitle
          workflowId={WORKFLOW_ID}
          isReadOnly={false}
          isEditTab
          isNew={false}
          onNewConsumed={() => {}}
        />
        <WorkflowHeaderCta workflowId={WORKFLOW_ID} isReadOnly={false} isEditTab />
      </>
    );
  };
  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/workspaces/$workspaceId/workflows/$workflowId",
      component: Builder,
    }),
  ]);
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [BUILDER_PATH] }),
  });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <JotaiProvider>
          <RouterProvider router={router} />
        </JotaiProvider>
      </QueryClientProvider>
    </I18nextProvider>
  );
};

const openStatusMenu = async (statusLabel: string) => {
  const trigger = await screen.findByRole("button", { name: statusLabel });
  fireEvent.keyDown(trigger, { key: "Enter" });
};

const toastMessages = () => toast.getSnapshot().map((shown) => `${shown.kind}: ${shown.message}`);

describe("workflow builder save and publish", () => {
  test("autosaves a renamed workflow once the debounce passes, sending the trimmed name and the definition, and reports it saved", async () => {
    const requests = stubWorkflowsApi(workflowResource(executableDefinition));
    renderBuilder();

    const title = await screen.findByRole("textbox", { name: i18n.t("common.workflow_name") });
    fireEvent.change(title, { target: { value: "  Welcome flow  " } });
    expect(screen.getByRole("status").textContent).toBe(i18n.t("workspace.workflows.saving_changes"));

    await waitFor(() => expect(requests.some((request) => request.method === "PATCH")).toBe(true), {
      timeout: 5000,
    });
    const patches = requests.filter((request) => request.method === "PATCH");
    expect(patches).toHaveLength(1);
    expect(patches[0]?.body).toEqual({
      name: "Welcome flow",
      description: null,
      definition: executableDefinition,
    });
    await waitFor(() =>
      expect(screen.getByRole("status").textContent).toBe(i18n.t("workspace.workflows.all_changes_saved"))
    );
    // An autosave never toasts; the pill is its report.
    expect(toastMessages()).toEqual([]);
  });

  test("publishing flushes unsaved edits before enabling, then shows the workflow enabled", async () => {
    const requests = stubWorkflowsApi(workflowResource(executableDefinition));
    renderBuilder();

    const title = await screen.findByRole("textbox", { name: i18n.t("common.workflow_name") });
    fireEvent.change(title, { target: { value: "Welcome flow" } });
    await openStatusMenu(i18n.t("common.draft"));
    fireEvent.click(await screen.findByRole("menuitem", { name: i18n.t("common.enable") }));

    await waitFor(() =>
      expect(toastMessages()).toContain(`success: ${i18n.t("workspace.workflows.enable_success")}`)
    );
    expect(
      requests.filter((request) => request.method !== "GET").map((r) => `${r.method} ${r.path}`)
    ).toEqual([`PATCH /api/v3/workflows/${WORKFLOW_ID}`, `POST /api/v3/workflows/${WORKFLOW_ID}/enable`]);
    expect(requests.find((request) => request.method === "PATCH")?.body).toMatchObject({
      name: "Welcome flow",
    });
    // Enabled locks the definition, so the header says read-only and the menu offers Disable.
    expect(await screen.findByText(i18n.t("workspace.workflows.read_only"))).toBeTruthy();
    await openStatusMenu(i18n.t("common.enabled"));
    expect(await screen.findByRole("menuitem", { name: i18n.t("common.disable") })).toBeTruthy();
  });

  test("publishing is refused, without an enable request, when the unsaved edits cannot be saved", async () => {
    const requests = stubWorkflowsApi(workflowResource(executableDefinition), { patchStatus: 400 });
    renderBuilder();

    const title = await screen.findByRole("textbox", { name: i18n.t("common.workflow_name") });
    fireEvent.change(title, { target: { value: "Welcome flow" } });
    await openStatusMenu(i18n.t("common.draft"));
    fireEvent.click(await screen.findByRole("menuitem", { name: i18n.t("common.enable") }));

    await waitFor(() =>
      expect(toastMessages()).toContain(
        `error: ${i18n.t("workspace.workflows.enable_blocked_unsaved_changes")}`
      )
    );
    expect(requests.some((request) => request.path.endsWith("/enable"))).toBe(false);
    // The refused draft is reported by the pill and not sent again by the autosave.
    await waitFor(() =>
      expect(screen.getByRole("status").textContent).toContain(i18n.t("workspace.workflows.autosave_failed"))
    );
    expect(requests.filter((request) => request.method === "PATCH")).toHaveLength(1);
  });

  test("a draft that is not ready cannot be enabled", async () => {
    const requests = stubWorkflowsApi(workflowResource(emptyDefinition));
    renderBuilder();

    await openStatusMenu(i18n.t("common.draft"));
    const enable = await screen.findByRole("menuitem", { name: i18n.t("common.enable") });
    expect(enable.getAttribute("aria-disabled")).toBe("true");
    fireEvent.click(enable);
    expect(requests.some((request) => request.path.endsWith("/enable"))).toBe(false);
  });

  test("an empty name is never sent and the field says why", async () => {
    const requests = stubWorkflowsApi(workflowResource(executableDefinition));
    renderBuilder();

    const title = await screen.findByRole("textbox", { name: i18n.t("common.workflow_name") });
    fireEvent.change(title, { target: { value: "   " } });
    expect((await screen.findByRole("alert")).textContent).toBe(
      i18n.t("workspace.workflows.validation_problem_name_missing")
    );
    expect(screen.getByRole("status").textContent).toContain(i18n.t("workspace.workflows.autosave_blocked"));
    fireEvent.keyDown(title, { key: "Enter" });
    await waitFor(() =>
      expect(toastMessages()).toContain(`error: ${i18n.t("workspace.workflows.name_required")}`)
    );
    expect(requests.some((request) => request.method === "PATCH")).toBe(false);
  });
});
