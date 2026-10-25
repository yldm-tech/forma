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
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import { initPostHogClient } from "../../lib/telemetry/posthog";
import { CreateFirstSurvey } from "./first-survey";

// forma2's create-first-survey.tsx reports which way the user chose to create their first survey.

const posthog = vi.hoisted(() => ({ __loaded: false, capture: vi.fn(), init: vi.fn() }));

vi.mock("posthog-js", () => ({ default: posthog }));

const ORG = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d01";
const WS = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  await initPostHogClient("phc_test", {});
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  posthog.capture.mockClear();
});

interface RecordedRequest {
  method: string;
  path: string;
  body?: unknown;
}

/** The step for a workspace still called "My workspace", for a user with a personal address unless `email` says otherwise. PATCH /api/app/workspaces/{id} answers with `rename` (the stored workspace, or an error status); every request is recorded. */
const renderStep = ({
  workspaceName = "My workspace",
  email = "ada@gmail.com",
  rename = 200,
}: { workspaceName?: string; email?: string; rename?: 200 | 400 } = {}) => {
  const requests: RecordedRequest[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      const method = init.method ?? "GET";
      requests.push({ method, path, body: init.body ? JSON.parse(init.body as string) : undefined });
      const json = (body: unknown, status = 200) =>
        new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
      if (method === "GET" && path === `/api/app/workspaces/${WS}/ai`) return json({ available: true });
      if (method === "PATCH" && path === `/api/app/workspaces/${WS}`) {
        return rename === 200
          ? json({ id: WS, name: (JSON.parse(init.body as string) as { name: string }).name })
          : json(
              {
                code: "workspace_name_taken",
                message: "A workspace with this name already exists in your organization",
              },
              400
            );
      }
      throw new Error(`unexpected ${method} ${path}`);
    })
  );
  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/organizations/$organizationId/workspaces/new/survey",
      component: () => (
        <CreateFirstSurvey
          organizationId={ORG}
          workspaceId={WS}
          workspaceName={workspaceName}
          email={email}
          defaultLanguage="en"
        />
      ),
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/organizations/$organizationId/workspaces/new/$step",
      component: () => <h1>Next step</h1>,
    }),
  ]);
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [`/organizations/${ORG}/workspaces/new/survey`] }),
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  return { router, requests };
};

describe("CreateFirstSurvey", () => {
  test("choosing templates reports the path, the organization and the workspace", async () => {
    const { router } = renderStep();

    fireEvent.click(await screen.findByText(i18n.t("organizations.workspaces.new.survey.use_template")));

    expect(posthog.capture).toHaveBeenCalledWith("onboarding_survey_path_selected", {
      path: "template",
      organization_id: ORG,
      workspace_id: WS,
    });
    await waitFor(() =>
      expect(router.state.location.pathname).toBe(`/organizations/${ORG}/workspaces/new/templates`)
    );
  });

  test("choosing AI reports the ai path", async () => {
    renderStep();

    const option = (await screen.findByText(i18n.t("workspace.surveys.ai_create.create_with_ai"))).closest(
      "button"
    );
    await waitFor(() => expect(option?.hasAttribute("disabled")).toBe(false));
    fireEvent.click(option as HTMLButtonElement);

    expect(posthog.capture).toHaveBeenCalledWith("onboarding_survey_path_selected", {
      path: "ai",
      organization_id: ORG,
      workspace_id: WS,
    });
  });

  test("a product name typed in the field renames the workspace before the next step", async () => {
    const { router, requests } = renderStep();
    const field = await screen.findByRole("textbox", { name: i18n.t("onboarding.product_name_label") });
    // A personal address suggests nothing.
    expect((field as HTMLInputElement).value).toBe("");

    fireEvent.change(field, { target: { value: "  Acme  " } });
    fireEvent.click(screen.getByText(i18n.t("organizations.workspaces.new.survey.use_template")));

    await waitFor(() =>
      expect(router.state.location.pathname).toBe(`/organizations/${ORG}/workspaces/new/templates`)
    );
    expect(requests.filter((request) => request.method === "PATCH")).toEqual([
      { method: "PATCH", path: `/api/app/workspaces/${WS}`, body: { name: "Acme" } },
    ]);
  });

  test("a work address suggests the name, and an untouched field that matches the workspace sends nothing", async () => {
    const { router, requests } = renderStep({ workspaceName: "Acme", email: "ada@acme.com" });
    const field = await screen.findByRole("textbox", { name: i18n.t("onboarding.product_name_label") });
    expect((field as HTMLInputElement).value).toBe("Acme");

    fireEvent.click(screen.getByText(i18n.t("organizations.workspaces.new.survey.use_template")));

    await waitFor(() =>
      expect(router.state.location.pathname).toBe(`/organizations/${ORG}/workspaces/new/templates`)
    );
    expect(requests.some((request) => request.method === "PATCH")).toBe(false);
  });

  test("a name the server refuses stays on the step with the reason under the field", async () => {
    const { router } = renderStep({ rename: 400 });
    const field = await screen.findByRole("textbox", { name: i18n.t("onboarding.product_name_label") });
    fireEvent.change(field, { target: { value: "Taken" } });
    fireEvent.click(screen.getByText(i18n.t("organizations.workspaces.new.survey.use_template")));

    expect((await screen.findByRole("alert")).textContent).toBe(
      "A workspace with this name already exists in your organization"
    );
    expect(field.getAttribute("aria-invalid")).toBe("true");
    expect(router.state.location.pathname).toBe(`/organizations/${ORG}/workspaces/new/survey`);
  });
});
