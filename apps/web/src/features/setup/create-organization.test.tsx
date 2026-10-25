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
import { CreateOrganization } from "./create-organization";

const NEW_ORG = "0190a000-0000-7000-8000-0000000000cc";

let i18n: I18n;
const t = (key: string) => i18n.t(key);

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

/** The server right after a create: the new organization is refused (404, as its existence is hidden) until the creator's permissions arrive, which the test releases. Returns the bodies of the POSTs and the release. */
const stubServer = () => {
  const posts: unknown[] = [];
  let authorized = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      const key = `${init.method ?? "GET"} ${path}`;
      if (key === "POST /api/app/organizations") {
        posts.push(JSON.parse(init.body as string));
        return json({ id: NEW_ORG, name: "Acme", workspaceId: "w" }, 201);
      }
      if (key === `GET /api/app/organizations/${NEW_ORG}`) {
        return authorized
          ? json({ id: NEW_ORG, name: "Acme" })
          : json({ code: "not_found", message: "Not found" }, 404);
      }
      if (key === "GET /api/app/organizations") return json({ organizations: [] });
      throw new Error(`unexpected ${key}`);
    })
  );
  return { posts, authorize: () => (authorized = true) };
};

const renderStep = () => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute({ component: Outlet });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/", component: CreateOrganization }),
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/setup/organization/$organizationId/invite",
        component: () => <p>invite step</p>,
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

describe("CreateOrganization (setup step)", () => {
  test("creates one organization, in the browser's time zone, however often it is submitted while the creator's permissions arrive", async () => {
    const server = stubServer();
    renderStep();
    fireEvent.change(await screen.findByLabelText(t("workspace.settings.general.organization_name")), {
      target: { value: "  Acme  " },
    });
    const next = screen.getByRole("button", { name: t("setup.organization.create.continue") });
    const form = next.closest("form");
    if (!form) throw new Error("no form");
    // A double click: the second submit lands in the same tick, before the button has re-rendered.
    fireEvent.submit(form);
    fireEvent.submit(form);
    await waitFor(() => expect(server.posts).toHaveLength(1));
    expect(server.posts[0]).toEqual({
      name: "Acme",
      timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    });

    // The POST has returned and the new organization is still refused: the step is still submitting, and neither the button nor a submit by Enter sends it again.
    await waitFor(() => expect(next.getAttribute("aria-busy")).toBe("true"));
    expect((next as HTMLButtonElement).disabled).toBe(true);
    fireEvent.submit(form);
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(server.posts).toHaveLength(1);
    expect(next.getAttribute("aria-busy")).toBe("true");

    server.authorize();
    expect(await screen.findByText("invite step", undefined, { timeout: 3000 })).toBeTruthy();
    expect(server.posts).toHaveLength(1);
  });
});
