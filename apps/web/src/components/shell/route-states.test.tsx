import {
  type ErrorComponentProps,
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import type { ReactNode } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { ApiError } from "../../lib/api";
import { initI18n } from "../../lib/i18n";
import { NoMobileOverlay, useIsPhone } from "./no-mobile-overlay";
import { NotFound, RootNotFound, RouteError } from "./route-states";
import { UnsavedChangesGuard, useUnsavedChanges } from "./unsaved-changes";

// forma2's app/error.tsx, app/not-found.tsx and NoMobileOverlay.

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(async () => {
  cleanup();
  vi.unstubAllGlobals();
  if (i18n.language !== "en-US") await i18n.changeLanguage("en-US");
});

const renderInRouter = async (node: ReactNode, queryClient = new QueryClient()) => {
  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({ getParentRoute: () => rootRoute, path: "/", component: () => node }),
    createRoute({ getParentRoute: () => rootRoute, path: "/auth/login", component: () => <p>login page</p> }),
  ]);
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/"] }) });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  await router.load();
  return router;
};

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

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

test("the root not-found page carries the wordmark and the signed-in user's language", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => json(200, { id: "u1", name: "Ada", email: "ada@example.com", locale: "ja-JP" }))
  );
  await renderInRouter(<RootNotFound />, new QueryClient());
  expect(screen.getByRole("img", { name: "Forma" })).toBeTruthy();
  await waitFor(() => expect(i18n.language).toBe("ja-JP"));
  expect(screen.getByRole("heading", { name: i18n.t("common.page_not_found") })).toBeTruthy();
});

test("the root not-found page keeps the browser's language without a session", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => json(401, { code: "not_authenticated", message: "Not authenticated" }))
  );
  await renderInRouter(<RootNotFound />);
  expect(await screen.findByRole("heading", { name: i18n.t("common.page_not_found") })).toBeTruthy();
  expect(i18n.language).toBe("en-US");
});

test("the mobile overlay is a modal dialog that names the account and lets the user out", async () => {
  const signOut = vi.fn(async () => json(200, {}));
  vi.stubGlobal("fetch", signOut);
  const router = await renderInRouter(<NoMobileOverlay email="ada@example.com" />);
  const dialog = screen.getByRole("dialog", { name: i18n.t("common.mobile_overlay_title") });
  expect(dialog.getAttribute("aria-modal")).toBe("true");
  expect(dialog.className).toContain("sm:hidden");
  expect(document.activeElement).toBe(dialog);
  // The overlay's title is the one h1 left to assistive technology, the app behind being inert.
  expect(screen.getAllByRole("heading", { level: 1 }).map((h) => h.textContent)).toEqual([
    i18n.t("common.mobile_overlay_title"),
  ]);
  expect(
    screen.getByText(i18n.t("common.mobile_overlay_signed_in_as", { email: "ada@example.com" }))
  ).toBeTruthy();

  const writeText = vi.fn(async () => {});
  Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
  fireEvent.click(screen.getByRole("button", { name: i18n.t("common.mobile_overlay_copy_link") }));
  await waitFor(() => expect(writeText).toHaveBeenCalledWith(window.location.href));
  expect(screen.getByRole("status").textContent).toBe(i18n.t("common.copied_to_clipboard"));

  fireEvent.click(screen.getByRole("button", { name: i18n.t("common.logout") }));
  await waitFor(() => expect(router.state.location.pathname).toBe("/auth/login"));
  expect(signOut).toHaveBeenCalledWith("/api/app/auth/sign-out", expect.anything());
});

test("logging out of the mobile overlay with unsaved edits behind it goes straight to the sign-in page", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => json(200, {}))
  );
  // A page whose form holds edits; the session ends before the page changes, so there is nothing left to keep editing.
  const DirtyForm = () => {
    useUnsavedChanges(true);
    return null;
  };
  const router = await renderInRouter(
    <UnsavedChangesGuard>
      <DirtyForm />
      <NoMobileOverlay email="ada@example.com" />
    </UnsavedChangesGuard>
  );
  fireEvent.click(screen.getByRole("button", { name: i18n.t("common.logout") }));
  await waitFor(() => expect(router.state.location.pathname).toBe("/auth/login"));
  expect(await screen.findByText("login page")).toBeTruthy();
});

test("useIsPhone follows the sm breakpoint", () => {
  const Probe = () => <p>{useIsPhone() ? "phone" : "wide"}</p>;
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: query === "(max-width: 639px)",
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
  render(<Probe />);
  expect(screen.getByText("phone")).toBeTruthy();
  cleanup();
  vi.unstubAllGlobals();
  render(<Probe />);
  expect(screen.getByText("wide")).toBeTruthy();
});
