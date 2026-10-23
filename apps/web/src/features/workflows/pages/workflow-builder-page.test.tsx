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
import { useState } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { authKeys } from "../../../lib/queries/auth";
import { initPostHogClient } from "../../../lib/telemetry/posthog";
import type { TWorkflowResource } from "../api";
import type { TWorkflowDefinition } from "../definition";
import { WorkflowBuilderPage } from "./workflow-builder-page";

const posthog = vi.hoisted(() => ({ __loaded: false, capture: vi.fn(), init: vi.fn() }));

vi.mock("posthog-js", () => ({ default: posthog }));

const WORKSPACE_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f";
const WORKFLOW_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d01";
const SURVEY_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  // jsdom has the <dialog> element but not its modal methods, which the problems dialog opens with.
  HTMLDialogElement.prototype.showModal ??= function showModal(this: HTMLDialogElement) {
    this.open = true;
  };
  HTMLDialogElement.prototype.close ??= function close(this: HTMLDialogElement) {
    this.open = false;
  };
  Element.prototype.scrollIntoView ??= function scrollIntoView() {};
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const stubLayout = () => {
  // React Flow measures its viewport and nodes; jsdom has no layout, so it observes nothing.
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    }
  );
};

const definition: TWorkflowDefinition = {
  schemaVersion: 1,
  trigger: {
    id: "trigger-1",
    type: "trigger",
    triggerType: "response.completed",
    config: { surveyId: SURVEY_ID, endingCardIds: [] },
    ui: { position: { x: 220, y: 80 } },
  },
  nodes: [
    {
      id: "email-1",
      type: "action",
      actionType: "send_email",
      config: {
        to: "",
        from: "team@example.com",
        replyTo: [],
        subject: "",
        body: "",
        attachResponseData: false,
      },
      ui: { position: { x: 220, y: 200 } },
    },
  ],
  edges: [{ id: "edge-1", source: "trigger-1", target: "email-1" }],
  entryNodeId: "trigger-1",
};

const workflow: TWorkflowResource = {
  id: WORKFLOW_ID,
  workspaceId: WORKSPACE_ID,
  name: "Thank respondents",
  description: null,
  status: "draft",
  triggerType: "response.completed",
  surveyId: SURVEY_ID,
  createdBy: null,
  creator: null,
  createdAt: "2026-10-01T00:00:00.000Z",
  updatedAt: "2026-10-01T00:00:00.000Z",
  lastRun: null,
  runCount: 0,
  definition,
};

const json = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });

/** The server the builder reads: the workflow, its authoring context, the workspace's surveys and the bound survey. */
const stubServer = () =>
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      if (path === `/api/v3/workflows/${WORKFLOW_ID}`) return json({ data: workflow });
      if (path === `/api/app/workspaces/${WORKSPACE_ID}/workflows/authoring-context`) {
        return json({
          teamMemberDetails: [{ name: "Ada", email: "ada@example.com" }],
          mailFrom: "noreply@forma.test",
        });
      }
      if (path.startsWith("/api/v3/surveys?")) {
        return json({
          data: [{ id: SURVEY_ID, name: "Onboarding" }],
          meta: { limit: 100, nextCursor: null, totalCount: null, workspaceSurveyCount: null },
        });
      }
      if (path === `/api/app/surveys/${SURVEY_ID}`) {
        return json({
          id: SURVEY_ID,
          workspaceId: WORKSPACE_ID,
          name: "Onboarding",
          createdAt: "2026-10-01T00:00:00.000Z",
          updatedAt: "2026-10-01T00:00:00.000Z",
          publishOn: null,
          closeOn: null,
          blocks: [],
          endings: [{ id: "endingone000000000000000", type: "endScreen", headline: { default: "Thanks" } }],
          hiddenFields: { enabled: true, fieldIds: ["email"] },
          variables: [],
          triggers: [],
          segment: null,
          languages: [],
          followUps: [],
          isVerifyEmailEnabled: false,
        });
      }
      throw new Error(`unexpected GET ${path}`);
    })
  );

const renderPage = (initialNode: string | null) => {
  const Page = () => {
    const [node, setNode] = useState<string | null>(initialNode);
    return (
      <>
        <WorkflowBuilderPage
          workspaceId={WORKSPACE_ID}
          workflowId={WORKFLOW_ID}
          isReadOnly={false}
          nodeParam={node}
          setNodeParam={setNode}
        />
        <output data-testid="node-param">{node ?? ""}</output>
      </>
    );
  };
  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/workspaces/$workspaceId/workflows/$workflowId",
      component: Page,
    }),
  ]);
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({
      initialEntries: [`/workspaces/${WORKSPACE_ID}/workflows/${WORKFLOW_ID}`],
    }),
  });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  queryClient.setQueryData(authKeys.me, {
    id: "u1",
    name: "Me",
    email: "me@example.com",
    emailVerified: true,
    locale: "en-US",
    identityProvider: "email",
    twoFactorEnabled: false,
    createdAt: "2026-01-01T00:00:00.000Z",
    lastLoginAt: null,
  });
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

describe("WorkflowBuilderPage", () => {
  test("draws the trigger and its email step, flags the unfinished step, and opens the ?node= step in the inspector", async () => {
    stubLayout();
    stubServer();
    renderPage("email-1");

    expect(await screen.findByText(i18n.t("workspace.workflows.response_completed"))).toBeTruthy();
    // The step's title shows on its canvas card and as the inspector's heading.
    await waitFor(() => expect(screen.getAllByText(i18n.t("workspace.workflows.send_email")).length).toBe(2));
    // The bound survey resolves, so only the email step is flagged, and the problem count says so.
    expect(await screen.findByText(i18n.t("workspace.workflows.node_needs_email_content"))).toBeTruthy();
    expect(screen.queryByText(i18n.t("workspace.workflows.node_needs_survey"))).toBeNull();
    expect(
      await screen.findByRole("button", {
        name: i18n.t("workspace.workflows.validation_problems_count", { count: 1 }),
      })
    ).toBeTruthy();
    // The inspector authors against the bound survey: the sender is the instance's MAIL_FROM.
    expect(await screen.findByText("noreply@forma.test")).toBeTruthy();
    expect(screen.getByTestId("node-param").textContent).toBe("email-1");
  });

  test("the problems dialog sends the user to the first blank field of the unfinished step", async () => {
    stubLayout();
    stubServer();
    renderPage(null);

    fireEvent.click(
      await screen.findByRole("button", {
        name: i18n.t("workspace.workflows.validation_problems_count", { count: 1 }),
      })
    );
    const fix = await screen.findByRole("button", {
      name: i18n.t("workspace.workflows.validation_problem_fix_label", {
        problem: i18n.t("workspace.workflows.validation_problem_step_incomplete"),
      }),
    });
    fireEvent.click(fix);

    // The jump opens the step and reveals the errors of every blank field.
    expect(await screen.findByText(i18n.t("workspace.workflows.email_to_required"))).toBeTruthy();
    expect(screen.getByText(i18n.t("workspace.workflows.email_subject_required"))).toBeTruthy();
    await waitFor(() => expect(screen.getByTestId("node-param").textContent).toBe("email-1"));
    // The first blank field in form order takes the focus.
    await waitFor(() => expect(document.activeElement?.id).toBe("workflow-email-to"));
  });
});

describe("WorkflowBuilderPage analytics", () => {
  beforeAll(async () => {
    await initPostHogClient("phc_test", {});
  });

  test("reports the builder, the opened step, the problems and the fix, without node ids or field paths", async () => {
    posthog.capture.mockClear();
    stubLayout();
    stubServer();
    renderPage("email-1");

    await waitFor(() =>
      expect(posthog.capture).toHaveBeenCalledWith("workflow_surface_viewed", {
        surface: "builder",
        workflow_id: WORKFLOW_ID,
        workflow_status: "draft",
        is_read_only: false,
      })
    );
    await waitFor(() =>
      expect(posthog.capture).toHaveBeenCalledWith("workflow_inspector_opened", {
        node_type: "send_email",
        has_config_form: true,
        is_editable: true,
      })
    );

    fireEvent.click(
      await screen.findByRole("button", {
        name: i18n.t("workspace.workflows.validation_problems_count", { count: 1 }),
      })
    );
    expect(posthog.capture).toHaveBeenCalledWith("workflow_validation_problems_viewed", {
      problem_count: 1,
      problem_codes: ["step_incomplete"],
    });
    fireEvent.click(
      await screen.findByRole("button", {
        name: i18n.t("workspace.workflows.validation_problem_fix_label", {
          problem: i18n.t("workspace.workflows.validation_problem_step_incomplete"),
        }),
      })
    );
    expect(posthog.capture).toHaveBeenCalledWith("workflow_validation_problem_fix_clicked", {
      problem_code: "step_incomplete",
    });
    expect(posthog.capture.mock.calls.filter(([event]) => event === "workflow_surface_viewed")).toHaveLength(
      1
    );
    expect(JSON.stringify(posthog.capture.mock.calls)).not.toContain("email-1");
  });
});
