import {
  Outlet,
  RouterProvider,
  type RouterHistory,
  createBrowserHistory,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { useState } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import { UnsavedChangesGuard, useUnsavedChanges } from "./unsaved-changes";

let i18n: I18n;
const t = (key: string) => i18n.t(key);

beforeAll(async () => {
  i18n = await initI18n("en-US");
  // jsdom has the <dialog> element but not its modal methods; these stand in for what the Dialog reads of them.
  HTMLDialogElement.prototype.showModal ??= function (this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close ??= function (this: HTMLDialogElement) {
    this.removeAttribute("open");
  };
});

afterEach(cleanup);

/** A settings form: dirty while its field differs from the stored "Acme". */
const NameForm = ({ label }: { label: string }) => {
  const [name, setName] = useState("Acme");
  useUnsavedChanges(name !== "Acme");
  return (
    <label>
      {label}
      <input value={name} onChange={(event) => setName(event.target.value)} />
    </label>
  );
};

/** The settings page with two forms, and another page. The app's links and switchers navigate by href, as the test does. */
const renderPage = (history: RouterHistory = createMemoryHistory({ initialEntries: ["/"] })) => {
  const rootRoute = createRootRoute({
    component: () => (
      <UnsavedChangesGuard>
        <Outlet />
      </UnsavedChangesGuard>
    ),
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/",
        component: () => (
          <>
            <NameForm label="Workspace name" />
            <NameForm label="Organization name" />
          </>
        ),
      }),
      createRoute({ getParentRoute: () => rootRoute, path: "/other", component: () => <h1>Other</h1> }),
    ]),
    history,
  });
  render(
    <I18nextProvider i18n={i18n}>
      <RouterProvider router={router} />
    </I18nextProvider>
  );
  // Not awaited: a navigation the guard holds back settles only when the user answers.
  const go = (href: string) => {
    void router.navigate({ href });
  };
  return { router, go };
};

/** Whether the "Discard changes?" dialog is open. */
const isAsking = () =>
  screen.queryByText(t("common.discard_changes_title"))?.closest("dialog")?.hasAttribute("open") ?? false;

const edit = async (label: string, value: string) =>
  fireEvent.change(await screen.findByLabelText(label), { target: { value } });

/** Fires the event a reload or a closed tab sends, and says whether the page asked the browser to confirm. */
const unloadIsHeldBack = () => {
  const event = new Event("beforeunload", { cancelable: true });
  window.dispatchEvent(event);
  return event.defaultPrevented;
};

describe("UnsavedChangesGuard", () => {
  test("lets the user leave a page whose forms hold nothing unsaved", async () => {
    const { router, go } = renderPage();
    await screen.findByLabelText("Workspace name");
    act(() => go("/other"));
    expect(await screen.findByText("Other")).toBeTruthy();
    expect(router.state.location.pathname).toBe("/other");
    expect(isAsking()).toBe(false);
  });

  test("asks before leaving with unsaved edits, and stays when the user keeps editing", async () => {
    const { router, go } = renderPage();
    await edit("Workspace name", "Acme Labs");
    act(() => go("/other"));
    await screen.findByText(t("common.discard_changes_title"));
    expect(isAsking()).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: t("common.discard_changes_keep_editing") }));
    await act(async () => {});
    expect(isAsking()).toBe(false);
    expect(router.state.location.pathname).toBe("/");
    expect((screen.getByLabelText("Workspace name") as HTMLInputElement).value).toBe("Acme Labs");
  });

  test("leaves when the user discards, asking once for a page with two edited forms", async () => {
    const { router, go } = renderPage();
    await edit("Workspace name", "Acme Labs");
    await edit("Organization name", "Acme Inc");
    act(() => go("/other"));
    await screen.findByText(t("common.discard_changes_title"));

    fireEvent.click(screen.getByRole("button", { name: t("common.discard_changes_confirm") }));
    expect(await screen.findByText("Other")).toBeTruthy();
    expect(router.state.location.pathname).toBe("/other");
    expect(isAsking()).toBe(false);
  });

  test("stops asking once the edits are undone", async () => {
    const { router, go } = renderPage();
    await edit("Workspace name", "Acme Labs");
    await edit("Workspace name", "Acme");
    act(() => go("/other"));
    expect(await screen.findByText("Other")).toBeTruthy();
    expect(router.state.location.pathname).toBe("/other");
  });

  test("does not ask about a navigation that stays on the page", async () => {
    const { router, go } = renderPage();
    await edit("Workspace name", "Acme Labs");
    act(() => go("/?tab=second"));
    await act(async () => {});
    expect(router.state.location.search).toEqual({ tab: "second" });
    expect(isAsking()).toBe(false);
  });

  test("has the browser confirm a reload or a closed tab only while edits are unsaved", async () => {
    // Only the browser's history listens for the unload; the page starts at its own address.
    window.history.replaceState(null, "", "/");
    renderPage(createBrowserHistory());
    await screen.findByLabelText("Workspace name");
    expect(unloadIsHeldBack()).toBe(false);
    await edit("Workspace name", "Acme Labs");
    expect(unloadIsHeldBack()).toBe(true);
  });
});
