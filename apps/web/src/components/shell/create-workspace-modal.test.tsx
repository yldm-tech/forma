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
import { useState } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import { CreateWorkspaceModal } from "./create-workspace-modal";

const ORG = "0190a000-0000-7000-8000-000000000001";
const NEW_WS = "0190a000-0000-7000-8000-0000000000bb";

let i18n: I18n;
const t = (key: string) => i18n.t(key);

beforeAll(async () => {
  i18n = await initI18n("en-US");
  HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
    this.removeAttribute("open");
  };
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

/** The server right after a create: the new workspace is refused (403) until the creator's permissions arrive, which the test releases. With failing, the POST itself hangs until the test releases it and then fails. Returns the bodies of the POSTs and the releases. */
const stubServer = ({ failing = false }: { failing?: boolean } = {}) => {
  const posts: unknown[] = [];
  let authorized = false;
  let fail = () => {};
  const failed = new Promise<void>((resolve) => (fail = resolve));
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      const key = `${init.method ?? "GET"} ${path}`;
      if (key === `POST /api/app/organizations/${ORG}/workspaces`) {
        posts.push(JSON.parse(init.body as string));
        if (failing) {
          await failed;
          return json({ code: "internal", message: "boom" }, 500);
        }
        // A second POST for the same name is what the double click produced.
        return posts.length > 1
          ? json({ code: "validation", message: "duplicate name" }, 400)
          : json({ id: NEW_WS, name: "Shop" }, 201);
      }
      if (key === `GET /api/app/workspaces/${NEW_WS}`) {
        return authorized
          ? json({ id: NEW_WS, name: "Shop" })
          : json({ code: "forbidden", message: "" }, 403);
      }
      if (key === `GET /api/app/organizations/${ORG}/teams`) return json({ userTeams: [], otherTeams: [] });
      if (key === `GET /api/app/organizations/${ORG}`) return json({ id: ORG, name: "Acme" });
      throw new Error(`unexpected ${key}`);
    })
  );
  return { posts, authorize: () => (authorized = true), fail };
};

const Host = () => {
  const [open, setOpen] = useState(true);
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        reopen
      </button>
      <CreateWorkspaceModal open={open} setOpen={setOpen} organizationId={ORG} />
    </>
  );
};

const renderModal = () => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute({ component: Outlet });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/", component: Host }),
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/workspaces/$workspaceId/",
        component: () => <p>new workspace</p>,
      }),
    ]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
};

describe("CreateWorkspaceModal", () => {
  test("sends one POST however often it is submitted while the creator's permissions arrive", async () => {
    const server = stubServer();
    renderModal();
    fireEvent.change(await screen.findByLabelText(t("common.workspace_name")), { target: { value: "Shop" } });
    const create = screen.getByRole("button", { name: t("common.create_workspace") });
    const form = create.closest("form");
    if (!form) throw new Error("no form");
    fireEvent.submit(form);
    fireEvent.submit(form);
    await waitFor(() => expect(server.posts).toHaveLength(1));

    // The POST has returned and the workspace is still refused: the button stays busy and a further submit sends nothing.
    await waitFor(() => expect(create.getAttribute("aria-busy")).toBe("true"));
    expect((create as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: t("common.cancel") }) as HTMLButtonElement).disabled).toBe(
      true
    );
    fireEvent.submit(form);
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(server.posts).toHaveLength(1);

    server.authorize();
    expect(await screen.findByText("new workspace", undefined, { timeout: 3000 })).toBeTruthy();
    expect(server.posts).toHaveLength(1);
  });

  // Chrome closes the native dialog on a repeated Escape however its cancel event is handled. The close held back while the create ran must take effect when it fails, or the open state stays true and "Add new workspace" can never open the dialog again.
  test("a dialog the browser closed while the create ran can be opened again after the create fails", async () => {
    const server = stubServer({ failing: true });
    renderModal();
    const field = t("common.workspace_name");
    fireEvent.change(await screen.findByLabelText(field), { target: { value: "Shop" } });
    const form = screen.getByLabelText(field).closest("form");
    if (!form) throw new Error("no form");
    fireEvent.submit(form);
    await waitFor(() => expect(server.posts).toHaveLength(1));

    const dialog = document.querySelector("dialog");
    if (!dialog) throw new Error("no dialog");
    fireEvent(dialog, new Event("cancel", { cancelable: true }));
    dialog.close();
    fireEvent(dialog, new Event("close"));
    server.fail();
    await waitFor(() => expect(screen.queryByLabelText(field)).toBeNull());

    fireEvent.click(screen.getByRole("button", { name: "reopen" }));
    expect(dialog.hasAttribute("open")).toBe(true);
    expect(screen.getByLabelText(field)).toBeTruthy();
  });
});
