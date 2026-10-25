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
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import type { WorkspaceDetail } from "../../lib/queries/workspaces";
import { AppConnectionPage } from "./app-connection";

const ORG = "0190a000-0000-7000-8000-000000000001";
const WS = "0190a000-0000-7000-8000-000000000002";
const APP_URL = "https://forma.example.com";

let i18n: I18n;
const t = (key: string) => i18n.t(key);

beforeAll(async () => {
  i18n = await initI18n("en-US");
  // jsdom has the <dialog> element but not its modal methods, which the secret dialog opens with.
  HTMLDialogElement.prototype.showModal ??= function showModal(this: HTMLDialogElement) {
    this.open = true;
  };
  HTMLDialogElement.prototype.close ??= function close(this: HTMLDialogElement) {
    this.open = false;
  };
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const workspaceWith = (canManage: boolean) =>
  ({
    id: WS,
    organizationId: ORG,
    access: { role: canManage ? "owner" : "member", canWrite: canManage, canManage, isReadOnly: !canManage },
  }) as WorkspaceDetail;

const json = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });

const attributeKey = (key: string) => ({ id: key, key, name: null, type: "custom", dataType: "string" });

/** The server's answers: the app connection, the workspace's attribute keys, and its SDK identity settings, which mutations change. Every request is recorded. */
const stubServer = (
  identity = { hasSecret: false, required: false, protectedAttributeKeys: [] as string[] }
) => {
  const requests: { method: string; path: string; body: unknown }[] = [];
  let state = identity;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const body = typeof init?.body === "string" ? (JSON.parse(init.body) as unknown) : undefined;
      requests.push({ method, path, body });
      if (path === `/api/app/workspaces/${WS}/app-connection`) {
        return json({ workspaceId: WS, appUrl: APP_URL, appSetupCompleted: false });
      }
      if (path === `/api/app/workspaces/${WS}/contact-attribute-keys`) {
        return json([
          attributeKey("userId"),
          attributeKey("email"),
          attributeKey("plan"),
          attributeKey("tier"),
        ]);
      }
      if (path === `/api/app/workspaces/${WS}/sdk-identity`) {
        if (method === "PATCH") state = { ...state, ...(body as object) };
        return json(state);
      }
      if (path === `/api/app/workspaces/${WS}/sdk-identity/secret` && method === "POST") {
        state = { ...state, hasSecret: true };
        return json({ ...state, secret: "the-new-secret" });
      }
      throw new Error(`unexpected ${method} ${path}`);
    })
  );
  return requests;
};

const renderPage = (workspace = workspaceWith(true)) => {
  stubServer();
  renderWith(workspace);
};

const renderWith = (workspace: WorkspaceDetail) => {
  const rootRoute = createRootRoute({ component: Outlet });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/sdk",
        component: () => <AppConnectionPage workspace={workspace} />,
      }),
    ]),
    history: createMemoryHistory({ initialEntries: ["/sdk"] }),
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
};

const methodButton = async (key: string) =>
  screen.findByRole("button", { name: new RegExp(t(`workspace.app-connection.install_method_${key}`)) });

// Every framework option used to start with "npm install @forma/js", a package that does not exist.
test("the website methods load the SDK with the script tag, and none installs a package", async () => {
  renderPage();
  fireEvent.click(await methodButton("website"));
  for (const option of ["html", "react", "nextjs", "vue"]) {
    fireEvent.click(
      screen.getByRole("radio", { name: t(`workspace.app-connection.install_method_${option}`) })
    );
    const text = document.body.textContent ?? "";
    expect(text).not.toMatch(/npm install|@forma\//);
    expect(text).toContain(WS);
    expect(text).toContain('"/js/forma.umd.js"');
  }
  expect(document.body.textContent).not.toMatch(/useSearchParams|usePathname/);
});

// The mobile SDKs, and the WordPress plugin the page sent people to (somebody else's), are not published.
test("mobile and WordPress say coming soon and offer nothing to install", async () => {
  renderPage();
  await methodButton("website");
  for (const key of ["mobile", "wordpress"]) {
    const title = screen.getByText(t(`workspace.app-connection.install_method_${key}`));
    expect(title.closest("button")).toBeNull();
    const card = title.closest("div.rounded-lg");
    expect(card).not.toBeNull();
    expect(within(card as HTMLElement).getByText(t("workspace.app-connection.coming_soon"))).toBeDefined();
  }
  expect(document.body.textContent).not.toMatch(/official plugin|@forma\/react-native|com\.forma:android/);
});

test("the page links to the SDK reference", async () => {
  renderPage();
  await methodButton("website");
  const link = screen.getByRole("link", { name: new RegExp(t("workspace.app-connection.loader_docs_link")) });
  expect(link.getAttribute("href")).toBe("https://forma.yldm.ai/developers/sdk/");
  expect(link.getAttribute("rel")).toContain("noopener");
});

// The workspace id in the snippet is public: the page lets a manager generate the identity secret (shown once), require verified identities only once there is one, and protect attributes from the SDK.
test("a manager generates the identity secret, then requires verified identities and protects attributes", async () => {
  const requests = stubServer();
  renderWith(workspaceWith(true));

  await screen.findByText(t("workspace.sdk_identity.secret_none"));
  const required = screen.getByRole("switch", { name: t("workspace.sdk_identity.required_label") });
  expect(required.hasAttribute("disabled")).toBe(true);
  expect(document.body.textContent).toContain("setUserId(user.id, { userHash })");

  fireEvent.click(screen.getByRole("button", { name: new RegExp(t("workspace.sdk_identity.generate")) }));
  const dialog = await screen.findByRole("dialog");
  expect((within(dialog).getByRole("textbox") as HTMLInputElement).value).toBe("the-new-secret");
  fireEvent.click(within(dialog).getByRole("button", { name: t("common.done") }));

  await screen.findByText(t("workspace.sdk_identity.secret_set"));
  expect(document.body.textContent).not.toContain("the-new-secret");
  const enabled = screen.getByRole("switch", { name: t("workspace.sdk_identity.required_label") });
  expect(enabled.hasAttribute("disabled")).toBe(false);

  // Requiring verification waits for a confirmation that says what else it turns off.
  fireEvent.click(enabled);
  const confirmation = await screen.findByRole("dialog");
  expect(confirmation.textContent).toContain(t("workspace.sdk_identity.required_confirm_description"));
  expect(requests.some((request) => request.method === "PATCH")).toBe(false);
  fireEvent.click(within(confirmation).getByRole("button", { name: t("common.cancel") }));
  await vi.waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(requests.some((request) => request.method === "PATCH")).toBe(false);

  fireEvent.click(enabled);
  fireEvent.click(
    within(await screen.findByRole("dialog")).getByRole("button", {
      name: t("workspace.sdk_identity.required_confirm_button"),
    })
  );
  await vi.waitFor(() =>
    expect(requests).toContainEqual({
      method: "PATCH",
      path: `/api/app/workspaces/${WS}/sdk-identity`,
      body: { required: true },
    })
  );
  await vi.waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

  // Turning it off again needs no confirmation.
  fireEvent.click(screen.getByRole("switch", { name: t("workspace.sdk_identity.required_label") }));
  await vi.waitFor(() =>
    expect(requests).toContainEqual({
      method: "PATCH",
      path: `/api/app/workspaces/${WS}/sdk-identity`,
      body: { required: false },
    })
  );
});

test("userId and email cannot be protected", async () => {
  stubServer();
  renderWith(workspaceWith(true));
  await screen.findByText(t("workspace.sdk_identity.secret_none"));
  fireEvent.keyDown(screen.getByLabelText(t("workspace.sdk_identity.protected_label")), { key: "Enter" });
  await vi.waitFor(() =>
    expect(screen.getAllByRole("menuitemcheckbox").map((option) => option.textContent)).toEqual([
      "plan",
      "tier",
    ])
  );
});

test("someone who does not manage the workspace sees the settings but cannot change them", async () => {
  stubServer({ hasSecret: true, required: true, protectedAttributeKeys: ["plan"] });
  renderWith(workspaceWith(false));
  await screen.findByText(t("workspace.sdk_identity.secret_set"));
  expect(
    screen.queryByRole("button", { name: new RegExp(t("workspace.sdk_identity.regenerate")) })
  ).toBeNull();
  const required = screen.getByRole("switch", { name: t("workspace.sdk_identity.required_label") });
  expect(required.getAttribute("aria-checked")).toBe("true");
  expect(required.hasAttribute("disabled")).toBe(true);
  expect(screen.getByText(t("workspace.sdk_identity.read_only"))).toBeDefined();
});
