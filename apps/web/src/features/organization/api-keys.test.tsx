import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import type { ReactNode } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import type { ApiKey, CreatedApiKey } from "../../lib/queries/api-keys";
import type { OrganizationDetail } from "../../lib/queries/organizations";
import { toast } from "../../lib/toast";
import { ApiKeysPage, ConnectAppBanner, permissionLabel, scopeSummary } from "./api-keys";

// forma2's API keys page (modules/organization/settings/api-keys/page.tsx): a website or app survey needs no key, so the page points to the first workspace's SDK page while there is a workspace.

let i18n: I18n;

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
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
});

const renderBanner = async (workspaces: { id: string; name: string }[]) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      if (path !== "/api/app/organizations/o1/workspaces") throw new Error(`unexpected ${path}`);
      return new Response(JSON.stringify({ workspaces }), {
        headers: { "content-type": "application/json" },
      });
    })
  );
  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/",
      component: () => <ConnectAppBanner organizationId="o1" />,
    }),
  ]);
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/"] }) });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  await router.load();
};

test("points to the first workspace's SDK page", async () => {
  await renderBanner([
    { id: "w1", name: "Website" },
    { id: "w2", name: "App" },
  ]);
  expect(
    await screen.findByText(i18n.t("workspace.settings.api_keys.connect_app_banner_title"))
  ).toBeTruthy();
  expect(
    screen
      .getByRole("link", { name: i18n.t("workspace.settings.api_keys.connect_app_banner_link") })
      .getAttribute("href")
  ).toBe("/workspaces/w1/integrations/app-connection");
});

test("is absent while the organization has no workspace", async () => {
  await renderBanner([]);
  await waitFor(() => expect(vi.mocked(fetch)).toHaveBeenCalled());
  expect(screen.queryByText(i18n.t("workspace.settings.api_keys.connect_app_banner_title"))).toBeNull();
});

test("names a workspace permission in the user's language, never by its enum value", async () => {
  expect((["read", "write", "manage"] as const).map((p) => permissionLabel(i18n.t, p))).toEqual([
    "Read",
    "Read & Write",
    "Manage",
  ]);
  const ja = await initI18n("ja-JP");
  const labels = (["read", "write", "manage"] as const).map((p) => permissionLabel(ja.t, p));
  expect(labels).toEqual([
    ja.t("workspace.settings.teams.read"),
    ja.t("workspace.settings.teams.read_write"),
    ja.t("workspace.settings.teams.manage"),
  ]);
  for (const label of labels) expect(label).not.toMatch(/^(read|write|manage)$/i);
});

const ORG = "0190a000-0000-7000-8000-000000000001";
const API = `/api/app/organizations/${ORG}`;

const owner: OrganizationDetail = {
  id: ORG,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  name: "Acme",
  isAISmartToolsEnabled: false,
  displayTimeZone: null,
  surveySchedulingTimeZone: null,
  surveyScheduling: { timeZone: "UTC", localHour: 0, localMinute: 0, instanceTimeZone: "UTC" },
  whitelabel: {},
  membership: { organizationId: ORG, userId: "u1", accepted: true, role: "owner" },
  userManagementAllowed: true,
};

const existing: ApiKey = {
  id: "k1",
  label: "Existing",
  createdAt: "2026-01-01T00:00:00Z",
  organizationAccess: { accessControl: { read: true, write: false } },
  workspacePermissions: [],
  lastUsedAt: null,
  createdBy: { id: "u1", name: "Ada Owner", formerMember: false },
  disabledAt: null,
  disabledReason: null,
  keyHint: null,
  expiresAt: null,
};

/** The API keys endpoints over an in-memory list, so a create or delete shows up in the next list read. The bodies of the creates are kept in order. */
const stubKeysServer = (listed: ApiKey[] = [existing]) => {
  const keys: ApiKey[] = [...listed];
  const creates: Record<string, unknown>[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      const key = `${init.method ?? "GET"} ${path}`;
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), { headers: { "content-type": "application/json" } });
      if (key === `GET ${API}/workspaces`) return json({ workspaces: [] });
      if (key === `GET ${API}/api-keys`) return json(keys);
      if (key === `POST ${API}/api-keys`) {
        const body = JSON.parse(init.body as string) as Record<string, unknown>;
        creates.push(body);
        const created: CreatedApiKey = {
          id: "k2",
          label: "CI",
          createdAt: "2026-01-02T00:00:00Z",
          organizationAccess: { accessControl: { read: true, write: false } },
          workspacePermissions: [],
          lastUsedAt: null,
          createdBy: { id: "u1", name: "Ada Owner", formerMember: false },
          disabledAt: null,
          disabledReason: null,
          keyHint: "text",
          expiresAt: typeof body.expiresAt === "string" ? body.expiresAt : null,
          key: "fbk_plaintext",
        };
        keys.push(created);
        return json(created);
      }
      if (key === `DELETE ${API}/api-keys/k2`) {
        keys.splice(
          keys.findIndex((k) => k.id === "k2"),
          1
        );
        return new Response(null, { status: 204 });
      }
      throw new Error(`unexpected ${key}`);
    })
  );
  return creates;
};

const renderPage = async (node: ReactNode) => {
  const rootRoute = createRootRoute({ component: Outlet });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/", component: () => node }),
    ]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  await router.load();
};

test("tells the creator of a key that v3 and MCP can lag, and how to check what it reaches, only while that key is listed", async () => {
  const creates = stubKeysServer();
  await renderPage(<ApiKeysPage organization={owner} />);
  const note = i18n.t("workspace.api_keys.v3_propagation_note");
  const hint = i18n.t("workspace.api_keys.check_key_hint");
  const command = `curl -H "x-api-key: $FORMA_API_KEY" ${window.location.origin}/api/v3/me`;
  await screen.findByText("Existing");
  expect(screen.queryByText(note)).toBeNull();

  fireEvent.click(screen.getByRole("button", { name: i18n.t("workspace.settings.api_keys.add_api_key") }));
  const dialog = await screen.findByRole("dialog");
  fireEvent.change(within(dialog).getByLabelText(i18n.t("workspace.api_keys.api_key_label")), {
    target: { value: "CI" },
  });
  fireEvent.click(
    within(dialog).getByRole("switch", { name: i18n.t("workspace.api_keys.organization_read") })
  );
  fireEvent.click(within(dialog).getByRole("button", { name: i18n.t("workspace.api_keys.add_api_key") }));

  expect((await screen.findByText(note)).getAttribute("role")).toBe("status");
  expect(await screen.findByText("fbk_plaintext")).toBeTruthy();
  // No expiry unless one is chosen.
  expect(creates).toEqual([expect.objectContaining({ label: "CI", expiresAt: null })]);
  // The command keeps the key in an environment variable, never in the shell history; the hint says to set it first, and the command copies with one click.
  expect(screen.getByText(hint).textContent).toContain("FORMA_API_KEY");
  const shown = screen.getByText(command);
  expect(shown.tagName).toBe("CODE");
  const writeText = vi.fn(async () => {});
  vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
  const commandBox = shown.parentElement;
  if (!commandBox) throw new Error("the command has no container");
  fireEvent.click(within(commandBox).getByRole("button", { name: i18n.t("common.copy") }));
  await waitFor(() => expect(writeText).toHaveBeenCalledWith(command));
  // The OpenAPI document is a link to this instance's copy.
  const openapi = screen.getByRole("link", { name: `${window.location.origin}/api/v3/openapi.json` });
  expect(openapi.getAttribute("href")).toBe(`${window.location.origin}/api/v3/openapi.json`);

  // Deleting the key just created takes the note and the plaintext with it: no listed key is new any more. Until then the label shows twice, in the notice and in the key's row.
  const row = within(screen.getByRole("table")).getByText("CI").closest("tr");
  if (!row) throw new Error("the new key has no row");
  fireEvent.click(within(row).getByRole("button", { name: i18n.t("common.delete") }));
  const confirm = await screen.findByRole("alertdialog");
  fireEvent.click(within(confirm).getByRole("button", { name: i18n.t("common.delete") }));
  await waitFor(() => expect(screen.queryAllByText("CI")).toEqual([]));
  expect(screen.queryByText("fbk_plaintext")).toBeNull();
  expect(screen.queryByText(note)).toBeNull();
  expect(screen.queryByText(hint)).toBeNull();
  expect(screen.getByText("Existing")).toBeTruthy();
});

test("summarises a key's scope in one line: the first workspaces, how many more, and its organization access", () => {
  const grant = (name: string, permission: "read" | "write" | "manage") => ({
    workspaceId: name,
    workspaceName: name,
    permission,
  });
  expect(
    scopeSummary(i18n.t, {
      workspacePermissions: [grant("Growth", "manage"), grant("Ops", "read"), grant("Web", "write")],
      organizationAccess: { accessControl: { read: false, write: true } },
    })
  ).toBe("Growth: Manage · Ops: Read · +1 more · Organization: Read & Write");
  expect(
    scopeSummary(i18n.t, {
      workspacePermissions: [],
      organizationAccess: { accessControl: { read: false, write: false } },
    })
  ).toBe("No access");
});

test("lists when each key was last used, who created it, and which keys a departed creator left disabled", async () => {
  const usedAt = new Date(Date.now() - 3 * 60 * 60 * 1000).toISOString();
  stubKeysServer([
    { ...existing, lastUsedAt: usedAt },
    {
      ...existing,
      id: "k2",
      label: "Left behind",
      createdBy: { id: "u2", name: "Grace Gone", formerMember: true },
      disabledAt: "2026-02-01T00:00:00Z",
      disabledReason: "creator_removed",
    },
    { ...existing, id: "k3", label: "Orphan", createdBy: null },
  ]);
  await renderPage(<ApiKeysPage organization={owner} />);

  const row = (label: string) => {
    const found = screen.getByText(label).closest("tr");
    if (!found) throw new Error(`no row for ${label}`);
    return within(found);
  };
  await screen.findByText("Left behind");
  expect(row("Existing").getByText("Ada Owner")).toBeTruthy();
  expect(row("Existing").getByText("about 3 hours ago")).toBeTruthy();
  expect(row("Existing").queryByText(i18n.t("common.disabled"))).toBeNull();
  expect(row("Existing").getByText("Organization: Read")).toBeTruthy();

  const formerMember = i18n.t("workspace.api_keys.former_member");
  expect(row("Left behind").getByText("Grace Gone")).toBeTruthy();
  expect(row("Left behind").getByText(formerMember)).toBeTruthy();
  expect(row("Left behind").getByText(i18n.t("workspace.api_keys.never_used"))).toBeTruthy();
  const badge = row("Left behind").getByText(i18n.t("common.disabled"));
  expect(badge.getAttribute("title")).toBe(i18n.t("workspace.api_keys.disabled_reason_creator_removed"));
  expect(row("Orphan").getByText(formerMember)).toBeTruthy();

  // The key's dialog says why it stopped working and what to do.
  fireEvent.click(
    screen.getByRole("button", {
      name: i18n.t("workspace.api_keys.view_permissions_for", { label: "Left behind" }),
    })
  );
  const dialog = await screen.findByRole("dialog");
  expect(within(dialog).getByText(i18n.t("workspace.api_keys.disabled_reason_creator_removed"))).toBeTruthy();
  expect(within(dialog).getByText(/can't be turned back on/)).toBeTruthy();
  expect(within(dialog).getByText("Grace Gone")).toBeTruthy();
});

test("links the API reference on the website from the page header", async () => {
  stubKeysServer();
  await renderPage(<ApiKeysPage organization={owner} />);
  const link = await screen.findByRole("link", { name: i18n.t("workspace.api_keys.api_reference") });
  expect(link.getAttribute("href")).toBe("https://forma.yldm.ai/developers/#reference");
  expect(link.getAttribute("target")).toBe("_blank");
});

/** Opens the add dialog with a label and organization read access, enough to create a key. */
const openAddDialog = async () => {
  fireEvent.click(
    await screen.findByRole("button", { name: i18n.t("workspace.settings.api_keys.add_api_key") })
  );
  const dialog = await screen.findByRole("dialog");
  fireEvent.change(within(dialog).getByLabelText(i18n.t("workspace.api_keys.api_key_label")), {
    target: { value: "CI" },
  });
  fireEvent.click(
    within(dialog).getByRole("switch", { name: i18n.t("workspace.api_keys.organization_read") })
  );
  return within(dialog);
};

test("creates a key that expires after the chosen number of days", async () => {
  const creates = stubKeysServer();
  await renderPage(<ApiKeysPage organization={owner} />);
  const dialog = await openAddDialog();
  expect(dialog.getByText(i18n.t("workspace.api_keys.expiration_never_description"))).toBeTruthy();

  fireEvent.change(dialog.getByLabelText(i18n.t("workspace.api_keys.expiration")), {
    target: { value: "30" },
  });
  expect(dialog.getByText(/^The key will stop working on /)).toBeTruthy();
  const before = Date.now();
  fireEvent.click(dialog.getByRole("button", { name: i18n.t("workspace.api_keys.add_api_key") }));
  await waitFor(() => expect(creates).toHaveLength(1));
  const sent = Date.parse(String(creates[0]?.expiresAt));
  const day = 24 * 60 * 60 * 1000;
  expect(sent).toBeGreaterThanOrEqual(before + 30 * day);
  expect(sent).toBeLessThanOrEqual(Date.now() + 30 * day);
});

test("creates a key that expires at the end of a chosen day, and needs the day first", async () => {
  const creates = stubKeysServer();
  await renderPage(<ApiKeysPage organization={owner} />);
  const dialog = await openAddDialog();
  const submit = dialog.getByRole("button", { name: i18n.t("workspace.api_keys.add_api_key") });

  fireEvent.change(dialog.getByLabelText(i18n.t("workspace.api_keys.expiration")), {
    target: { value: "custom" },
  });
  expect(submit.hasAttribute("disabled")).toBe(true);
  const day = dialog.getByLabelText(i18n.t("workspace.api_keys.expiration_date"));
  const now = new Date();
  const year = now.getFullYear() + 1;
  fireEvent.change(day, { target: { value: `${year}-03-15` } });
  expect(submit.hasAttribute("disabled")).toBe(false);
  fireEvent.click(submit);
  await waitFor(() => expect(creates).toHaveLength(1));
  expect(creates[0]?.expiresAt).toBe(new Date(year, 2, 15, 23, 59, 59).toISOString());
});

test("lists each key by its last four characters, when it expires, and which keys expired or expire within a week", async () => {
  const day = 24 * 60 * 60 * 1000;
  const soon = new Date(Date.now() + 2 * day).toISOString();
  const later = new Date(Date.now() + 60 * day).toISOString();
  const past = new Date(Date.now() - day).toISOString();
  stubKeysServer([
    { ...existing, keyHint: "Ab-_" },
    { ...existing, id: "k2", label: "Soon", keyHint: "soon", expiresAt: soon },
    { ...existing, id: "k3", label: "Later", keyHint: "late", expiresAt: later },
    { ...existing, id: "k4", label: "Gone", keyHint: "gone", expiresAt: past },
  ]);
  await renderPage(<ApiKeysPage organization={owner} />);
  const row = (label: string) => {
    const found = screen.getByText(label).closest("tr");
    if (!found) throw new Error(`no row for ${label}`);
    return within(found);
  };
  await screen.findByText("Gone");
  const formatDay = new Intl.DateTimeFormat("en-US", { dateStyle: "medium" });

  expect(row("Existing").getByText("fbk_…Ab-_")).toBeTruthy();
  expect(row("Existing").getByText(i18n.t("workspace.api_keys.never_expires"))).toBeTruthy();
  expect(row("Existing").queryByText(i18n.t("common.expired"))).toBeNull();
  expect(row("Existing").queryByText(i18n.t("workspace.api_keys.expires_soon"))).toBeNull();

  expect(row("Soon").getByText(i18n.t("workspace.api_keys.expires_soon"))).toBeTruthy();
  expect(row("Soon").getByText(formatDay.format(new Date(soon)))).toBeTruthy();
  expect(row("Later").queryByText(i18n.t("workspace.api_keys.expires_soon"))).toBeNull();
  expect(row("Later").getByText(formatDay.format(new Date(later)))).toBeTruthy();
  expect(row("Gone").getByText(i18n.t("common.expired"))).toBeTruthy();

  // The expired key's dialog says when it stopped working and what to do, with its hint and expiry.
  fireEvent.click(
    screen.getByRole("button", { name: i18n.t("workspace.api_keys.view_permissions_for", { label: "Gone" }) })
  );
  const dialog = within(await screen.findByRole("dialog"));
  expect(dialog.getByText(/^This key expired on /)).toBeTruthy();
  expect(dialog.getByText(i18n.t("workspace.api_keys.expired_description"))).toBeTruthy();
  expect(dialog.getByText("fbk_…gone")).toBeTruthy();
});

test("a key created before hints were kept shows a dash for its hint, in the list and in its dialog", async () => {
  stubKeysServer();
  await renderPage(<ApiKeysPage organization={owner} />);
  await screen.findByText("Existing");
  expect(screen.queryByText(/^fbk_…/)).toBeNull();
  const row = screen.getByText("Existing").closest("tr");
  if (!row) throw new Error("no row for Existing");
  expect(within(row).getByText("—")).toBeTruthy();

  fireEvent.click(
    screen.getByRole("button", {
      name: i18n.t("workspace.api_keys.view_permissions_for", { label: "Existing" }),
    })
  );
  const dialog = within(await screen.findByRole("dialog"));
  const term = dialog.getByText(i18n.t("workspace.api_keys.api_key"), { selector: "dt" });
  expect(term.nextElementSibling?.textContent).toBe("—");
});
