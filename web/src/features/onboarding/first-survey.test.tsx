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

const renderStep = () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      if (path === `/api/app/workspaces/${WS}/ai`) {
        return new Response(JSON.stringify({ available: true }), {
          status: 200,
          headers: { "content-type": "application/json" },
        });
      }
      throw new Error(`unexpected ${path}`);
    })
  );
  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/organizations/$organizationId/workspaces/new/survey",
      component: () => <CreateFirstSurvey organizationId={ORG} workspaceId={WS} defaultLanguage="en" />,
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
  return router;
};

describe("CreateFirstSurvey", () => {
  test("choosing templates reports the path, the organization and the workspace", async () => {
    const router = renderStep();

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
});
