import {
  type ErrorComponentProps,
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { cleanup, render, screen } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import type { ReactNode } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test } from "vite-plus/test";
import { ApiError } from "../../lib/api";
import { initI18n } from "../../lib/i18n";
import { NoMobileOverlay } from "./no-mobile-overlay";
import { NotFound, RouteError } from "./route-states";

// forma2's app/error.tsx, app/not-found.tsx and NoMobileOverlay.

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(cleanup);

const renderInRouter = async (node: ReactNode) => {
  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({ getParentRoute: () => rootRoute, path: "/", component: () => node }),
  ]);
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/"] }) });
  render(
    <I18nextProvider i18n={i18n}>
      <RouterProvider router={router} />
    </I18nextProvider>
  );
  await router.load();
};

const errorProps = (error: Error): ErrorComponentProps => ({ error, reset: () => {} });

test("a rate limit gets its own wording and no buttons", async () => {
  await renderInRouter(<RouteError {...errorProps(new ApiError(429, "too_many_requests", "Slow down"))} />);
  expect(await screen.findByText(i18n.t("common.error_rate_limit_title"))).toBeTruthy();
  expect(screen.getByText(i18n.t("common.error_rate_limit_description"))).toBeTruthy();
  expect(screen.queryByRole("button")).toBeNull();
});

test("any other error offers a retry and the dashboard, without the error's own message", async () => {
  await renderInRouter(<RouteError {...errorProps(new ApiError(404, "not_found", "Survey not found"))} />);
  expect(await screen.findByText(i18n.t("common.error_component_title"))).toBeTruthy();
  expect(screen.getByText(i18n.t("common.error_component_description"))).toBeTruthy();
  expect(screen.queryByText("Survey not found")).toBeNull();
  expect(screen.getByRole("button", { name: i18n.t("common.try_again") })).toBeTruthy();
  expect(screen.getByRole("button", { name: i18n.t("common.go_to_dashboard") })).toBeTruthy();
});

test("the not-found page says 404 and links home", async () => {
  await renderInRouter(<NotFound />);
  expect((await screen.findByTestId("error-code")).textContent).toBe("404");
  expect(screen.getByRole("heading", { name: i18n.t("common.page_not_found") })).toBeTruthy();
  expect(screen.getByText(i18n.t("common.page_not_found_description"))).toBeTruthy();
  expect(screen.getByRole("link", { name: i18n.t("common.back_to_home") }).getAttribute("href")).toBe("/");
});

test("the mobile overlay shows below the sm breakpoint only", () => {
  render(
    <I18nextProvider i18n={i18n}>
      <NoMobileOverlay />
    </I18nextProvider>
  );
  expect(screen.getByTestId("no-mobile-overlay").className).toContain("sm:hidden");
  expect(screen.getByRole("heading", { name: i18n.t("common.mobile_overlay_title") })).toBeTruthy();
});
