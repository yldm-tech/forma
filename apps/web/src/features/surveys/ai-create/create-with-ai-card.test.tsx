import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { cleanup, render, screen } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { type AIAvailability, aiKeys } from "../../ai/availability";
import { CreateWithAICard } from "./create-with-ai-card";

// The template gallery's create-with-AI card says why AI is unavailable instead of opening a dialog that only says so, as the onboarding step does.

const ORG = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d01";
const WS = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(cleanup);

const renderCard = (ai: AIAvailability) => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  queryClient.setQueryData(aiKeys.workspace(WS), ai);
  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/",
      component: () => <CreateWithAICard workspaceId={WS} organizationId={ORG} surveyType="link" />,
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/organizations/$organizationId/settings/general",
      component: () => null,
    }),
  ]);
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/"] }) });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
};

const card = async () =>
  (await screen.findByText(i18n.t("workspace.surveys.ai_create.card_title"))).closest(
    "button"
  ) as HTMLButtonElement;

describe("CreateWithAICard", () => {
  test("is enabled where AI is available", async () => {
    renderCard({ available: true });
    expect((await card()).disabled).toBe(false);
    expect(screen.queryByText(i18n.t("common.ai_unavailable.instance_not_configured"))).toBeNull();
  });

  test("is disabled with the reason under it where the instance has no AI", async () => {
    renderCard({ available: false, reason: "instance_not_configured" });
    const button = await card();
    expect(button.disabled).toBe(true);
    const reason = screen.getByText(i18n.t("common.ai_unavailable.instance_not_configured"));
    expect(button.getAttribute("aria-describedby")).toBe(reason.id);
    expect(
      screen.queryByRole("link", { name: i18n.t("common.ai_unavailable.enable_in_settings") })
    ).toBeNull();
  });

  test("links to the organization settings when AI only has to be switched on", async () => {
    renderCard({ available: false, reason: "not_enabled" });
    expect((await card()).disabled).toBe(true);
    const link = screen.getByRole("link", { name: i18n.t("common.ai_unavailable.enable_in_settings") });
    expect(link.getAttribute("href")).toBe(`/organizations/${ORG}/settings/general`);
  });
});
