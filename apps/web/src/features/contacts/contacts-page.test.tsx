import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { Suspense } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import { toast } from "../../lib/toast";
import { ContactsPage } from "./contacts-page";

const WS = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f";
const ALICE = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d10";
const BOB = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d11";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  window.scrollTo = () => {};
});

afterEach(() => {
  cleanup();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  vi.unstubAllGlobals();
  localStorage.clear();
});

const key = (k: string, name: string | null = null) => ({
  id: `key-${k}`,
  key: k,
  name,
  description: null,
  isUnique: false,
  type: ["email", "userId", "firstName", "lastName"].includes(k) ? "default" : "custom",
  dataType: "string",
  workspaceId: WS,
  createdAt: "",
  updatedAt: "",
});

const contacts = [
  {
    id: ALICE,
    createdAt: "",
    updatedAt: "",
    workspaceId: WS,
    attributes: {
      email: "alice@example.com",
      userId: "u-1001",
      firstName: "Alice",
      lastName: "Anders",
      plan: "enterprise",
      company: "Acme Corp",
    },
  },
  {
    id: BOB,
    createdAt: "",
    updatedAt: "",
    workspaceId: WS,
    attributes: {
      email: "bob@acme.test",
      userId: "u-1002",
      firstName: "Bob",
      lastName: "Brown",
      plan: "starter",
      company: "Acme Labs",
    },
  },
];

/** The Go server: the viewer, the workspace's attribute keys, and its contacts, filtered as the server searches them (any value containing the search, ignoring case). */
const stubServer = () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });
      const url = new URL(path, "http://localhost");
      if (url.pathname === "/api/app/auth/me")
        return json({ id: "u1", email: "me@example.com", locale: "en-US" });
      if (url.pathname === `/api/app/workspaces/${WS}/contact-attribute-keys`)
        return json([
          key("email"),
          key("userId"),
          key("firstName"),
          key("lastName"),
          key("plan", "Plan"),
          key("company", "Company"),
        ]);
      if (url.pathname === `/api/app/workspaces/${WS}/contacts`) {
        const search = (url.searchParams.get("search") ?? "").toLowerCase();
        return json(
          contacts.filter((contact) =>
            Object.values(contact.attributes).some((value) => value.toLowerCase().includes(search))
          )
        );
      }
      return new Response(JSON.stringify({ code: "not_found", message: path }), {
        status: 404,
        headers: { "content-type": "application/json" },
      });
    })
  );
};

const renderPage = () => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/",
      component: () => (
        <Suspense fallback={null}>
          <ContactsPage workspaceId={WS} isReadOnly={false} />
        </Suspense>
      ),
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

const search = async (value: string) => {
  fireEvent.change(await screen.findByRole("searchbox"), { target: { value } });
};

/** The table row that shows a text, found once the debounced search has reloaded the list. */
const rowWith = async (text: string) => (await screen.findByText(text, {}, { timeout: 3000 })).closest("tr")!;

const hintLabel = (column: string) => i18n.t("workspace.contacts.search_matched_in", { column });

test("a row found only through a hidden column says which column matched, with the match marked", async () => {
  stubServer();
  renderPage();
  await screen.findByText("alice@example.com");
  await search("corp");
  await vi.waitFor(() => expect(screen.queryByText("bob@acme.test")).toBeNull(), { timeout: 3000 });
  const row = await rowWith("alice@example.com");
  const hint = within(row).getByText(hintLabel("Company"), { exact: false });
  expect(hint.textContent).toBe(`${hintLabel("Company")} Acme Corp`);
  expect(within(hint).getByText("Corp").tagName).toBe("MARK");
  // The hint sits under the contact's link, in the email column.
  expect(hint.closest("td")?.querySelector("a")?.textContent).toBe("alice@example.com");
});

test("a row whose visible column already contains the search needs no hint", async () => {
  stubServer();
  renderPage();
  await screen.findByText("alice@example.com");
  await search("acme");
  await vi.waitFor(
    () => expect(screen.getAllByText(hintLabel("Company"), { exact: false })).toHaveLength(1),
    {
      timeout: 3000,
    }
  );
  // Bob's email shows "acme" itself; only Alice's row, matched through the hidden company, explains itself.
  const bob = await rowWith("Bob");
  expect(within(bob).queryByText(hintLabel("Company"), { exact: false })).toBeNull();
  const alice = await rowWith("Alice");
  expect(within(alice).getByText(hintLabel("Company"), { exact: false })).toBeTruthy();
});

test("with every linking column hidden, the hint moves to the first visible column instead of disappearing", async () => {
  // The stored choice covers exactly the table's columns, so it applies: only the user id and the plan are shown.
  localStorage.setItem(
    `${WS}-columnVisibility`,
    JSON.stringify({
      select: true,
      contactsTableUser: false,
      userId: true,
      email: false,
      firstName: false,
      lastName: false,
      plan: true,
      company: false,
    })
  );
  stubServer();
  renderPage();
  await screen.findByText("u-1001");
  expect(screen.queryByText("alice@example.com")).toBeNull();
  await search("corp");
  const hint = await screen.findByText(hintLabel("Company"), { exact: false }, { timeout: 3000 });
  expect(hint.textContent).toBe(`${hintLabel("Company")} Acme Corp`);
  // It sits in the user id cell, the first visible column after the selection box.
  expect(within(hint.closest("td")!).getByText("u-1001")).toBeTruthy();
  // Showing the hint does not change the stored column choice.
  expect(JSON.parse(localStorage.getItem(`${WS}-columnVisibility`)!)).toMatchObject({
    email: false,
    company: false,
  });
});

/** The server's export endpoint on top of stubServer: records each request's body and answers a CSV, or fails with status. */
const stubExport = (status = 200) => {
  stubServer();
  const list = fetch;
  const bodies: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      if (path !== `/api/app/workspaces/${WS}/contacts/export`) return list(path, init);
      expect(init?.method).toBe("POST");
      bodies.push(JSON.parse(init?.body as string));
      if (status !== 200) {
        return new Response(JSON.stringify({ code: "internal_server_error", message: "x" }), {
          status,
          headers: { "content-type": "application/json" },
        });
      }
      return new Response("\ufeffemail\nalice@example.com\n", {
        status: 200,
        headers: {
          "content-type": "text/csv;charset=utf-8",
          "content-disposition": 'attachment; filename="contacts-2026-10-26-00-00-00-utc.csv"',
        },
      });
    })
  );
  URL.createObjectURL = vi.fn(() => "blob:contacts");
  URL.revokeObjectURL = vi.fn();
  const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => undefined);
  click.mockClear();
  return { bodies, click };
};

test("Export CSV downloads every contact, and with a search, the contacts it matches", async () => {
  const { bodies, click } = stubExport();
  renderPage();
  await screen.findByText("alice@example.com");

  fireEvent.click(screen.getByRole("button", { name: "Export CSV" }));
  await vi.waitFor(() => expect(click).toHaveBeenCalledTimes(1));
  expect(bodies).toEqual([{ search: "" }]);
  expect((click.mock.contexts[0] as HTMLAnchorElement).download).toBe("contacts-2026-10-26-00-00-00-utc.csv");

  await search("corp");
  fireEvent.click(await screen.findByRole("button", { name: "Export matches as CSV" }, { timeout: 3000 }));
  await vi.waitFor(() => expect(click).toHaveBeenCalledTimes(2));
  expect(bodies[1]).toEqual({ search: "corp" });
});

test("a failed export says so", async () => {
  const { click } = stubExport(500);
  renderPage();
  await screen.findByText("alice@example.com");

  fireEvent.click(screen.getByRole("button", { name: "Export CSV" }));
  await vi.waitFor(() =>
    expect(
      toast
        .getSnapshot()
        .some((shown) => shown.message === "The contacts could not be exported. Please try again.")
    ).toBe(true)
  );
  expect(click).not.toHaveBeenCalled();
});
