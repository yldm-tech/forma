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
import type { ReactNode } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { SettingsNavigation } from "../../../components/shell/settings-navigation";
import { initI18n } from "../../../lib/i18n";
import type { OrganizationDetail } from "../../../lib/queries/organizations";
import type { OrganizationRole } from "../../../lib/roles";
import { toast } from "../../../lib/toast";
import type { PrettyUrlSurvey } from "./api";
import { DomainSettingsPage } from "./domain-page";
import { EmailCustomizationCard } from "./email-customization-card";
import { FaviconCustomizationCard } from "./favicon-customization-card";

const ORG = "0190a000-0000-7000-8000-000000000001";
const WS = "0190a000-0000-7000-8000-000000000002";
const BUCKET = "https://bucket.example.com/uploads";
const API = `/api/app/organizations/${ORG}`;

let i18n: I18n;
const t = (key: string, options?: Record<string, unknown>) => i18n.t(key, options);

beforeAll(async () => {
  i18n = await initI18n("en-US");
  URL.createObjectURL = () => "blob:preview";
  URL.revokeObjectURL = () => {};
  window.scrollTo = () => {};
});

afterEach(() => {
  cleanup();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  vi.unstubAllGlobals();
});

const organization = (
  role: OrganizationRole,
  whitelabel: OrganizationDetail["whitelabel"] = {}
): OrganizationDetail => ({
  id: ORG,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  name: "Acme",
  isAISmartToolsEnabled: false,
  displayTimeZone: null,
  whitelabel,
  membership: { organizationId: ORG, userId: "u1", accepted: true, role },
  userManagementAllowed: role !== "member",
});

interface Call {
  key: string;
  body?: string;
}

/** The server: the instance, the caller, the organization's workspaces, file uploads and the whitelabel writes. Requests are recorded. */
const stubServer = (storageConfigured = true) => {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      const key = `${init.method ?? "GET"} ${path}`;
      calls.push({ key, body: typeof init.body === "string" ? init.body : undefined });
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });
      switch (key) {
        case "GET /api/app/instance":
          return json({ storageConfigured, version: "0.2.10" });
        case "GET /api/app/auth/me":
          return json({ id: "u1", name: "Grace", email: "grace@example.com", locale: "en-US" });
        case `GET ${API}/workspaces`:
          return json({ workspaces: [{ id: WS, name: "Main" }] });
        case "POST /api/v1/management/storage": {
          const { fileName } = JSON.parse(init.body as string) as { fileName: string };
          return json({
            data: {
              signedUrl: BUCKET,
              presignedFields: { key: fileName },
              fileUrl: `/storage/${WS}/public/${fileName}`,
            },
          });
        }
        case `POST ${BUCKET}`:
        case `PUT ${API}/whitelabel/email-logo`:
        case `DELETE ${API}/whitelabel/email-logo`:
        case `PUT ${API}/whitelabel/favicon`:
        case `DELETE ${API}/whitelabel/favicon`:
        case `POST ${API}/whitelabel/test-email`:
          return new Response(null, { status: 204 });
        case `GET ${API}`:
          return json(organization("owner"));
        default:
          throw new Error(`unexpected ${key}`);
      }
    })
  );
  return calls;
};

const renderAt = (element: ReactNode, path = `/organizations/${ORG}/settings/general`) => {
  const rootRoute = createRootRoute({ component: Outlet });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path, component: () => element }),
      createRoute({
        getParentRoute: () => rootRoute,
        path: "$",
        component: () => <h1>{`page ${router.state.location.pathname}`}</h1>,
      }),
    ]),
    history: createMemoryHistory({ initialEntries: [path] }),
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

/** The upload input, once the workspace that receives organization uploads is known and it is enabled. */
const enabledUploadInput = async () => {
  const input = (await screen.findByTestId("upload-file-input")) as HTMLInputElement;
  await waitFor(() => expect(input.disabled).toBe(false));
  return input;
};

const toastMessages = () => toast.getSnapshot().map((shown) => shown.message);
const png = (name: string) => new File(["img"], name, { type: "image/png" });

describe("EmailCustomizationCard", () => {
  test("an owner uploads a logo, saves it, and sends the test email", async () => {
    const calls = stubServer();
    renderAt(<EmailCustomizationCard organization={organization("owner")} />);
    expect(
      await screen.findByText(
        t("workspace.settings.general.email_customization_preview_email_heading", { userName: "Grace" })
      )
    ).toBeTruthy();
    await waitFor(() => expect(calls.map((call) => call.key)).toContain(`GET ${API}/workspaces`));

    // Forma's own logo is shown until one is chosen; replacing it picks a file.
    fireEvent.change(screen.getByTestId("upload-file-input"), { target: { files: [png("brand.png")] } });
    fireEvent.click(screen.getByTestId("send-test-email-button"));
    expect(toastMessages()).toEqual([
      t("workspace.settings.general.please_save_logo_before_sending_test_email"),
    ]);

    fireEvent.click(screen.getByRole("button", { name: t("common.save") }));
    await waitFor(() =>
      expect(toastMessages()).toContain(t("workspace.settings.general.logo_saved_successfully"))
    );
    const sign = calls.find((call) => call.key === "POST /api/v1/management/storage");
    expect(JSON.parse(sign?.body ?? "")).toMatchObject({ fileName: "brand.png", workspaceId: WS });
    const save = calls.find((call) => call.key === `PUT ${API}/whitelabel/email-logo`);
    expect(JSON.parse(save?.body ?? "")).toEqual({ logoUrl: `/storage/${WS}/public/brand.png` });
  });

  test("the saved logo is sent in a test email, and removing it clears it", async () => {
    const calls = stubServer();
    const logoUrl = `/storage/${WS}/public/brand.png`;
    renderAt(<EmailCustomizationCard organization={organization("manager", { logoUrl })} />);

    fireEvent.click(await screen.findByTestId("send-test-email-button"));
    await waitFor(() =>
      expect(toastMessages()).toContain(t("workspace.settings.general.test_email_sent_successfully"))
    );
    expect(calls.map((call) => call.key)).toContain(`POST ${API}/whitelabel/test-email`);

    fireEvent.click(screen.getByTestId("remove-logo-button"));
    await waitFor(() =>
      expect(toastMessages()).toContain(t("workspace.settings.general.logo_removed_successfully"))
    );
    expect(calls.map((call) => call.key)).toContain(`DELETE ${API}/whitelabel/email-logo`);
    fireEvent.click(screen.getByTestId("send-test-email-button"));
    expect(toastMessages()).toContain(t("workspace.settings.general.please_add_a_logo"));
  });

  test.each(["member", "billing"] as const)("a %s can only look", async (role) => {
    stubServer();
    renderAt(
      <EmailCustomizationCard organization={organization(role, { logoUrl: "/storage/a/public/l.png" })} />
    );
    expect(
      await screen.findByText(
        t("common.only_owners_managers_and_manage_access_members_can_perform_this_action")
      )
    ).toBeTruthy();
    for (const id of ["replace-logo-button", "remove-logo-button", "send-test-email-button"]) {
      expect((screen.getByTestId(id) as HTMLButtonElement).disabled).toBe(true);
    }
  });
});

describe("FaviconCustomizationCard", () => {
  test("an uploaded favicon is saved, and a saved one removed", async () => {
    const calls = stubServer();
    renderAt(<FaviconCustomizationCard organization={organization("owner")} />);
    await waitFor(() => expect(calls.map((call) => call.key)).toContain(`GET ${API}/workspaces`));

    fireEvent.change(await enabledUploadInput(), {
      target: { files: [png("icon.png")] },
    });
    fireEvent.click(await screen.findByRole("button", { name: t("common.save") }));
    await waitFor(() =>
      expect(toastMessages()).toContain(t("workspace.settings.domain.favicon_saved_successfully"))
    );
    const save = calls.find((call) => call.key === `PUT ${API}/whitelabel/favicon`);
    expect(JSON.parse(save?.body ?? "")).toEqual({ faviconUrl: `/storage/${WS}/public/icon.png` });
    cleanup();

    renderAt(
      <FaviconCustomizationCard
        organization={organization("owner", { faviconUrl: "/storage/a/public/f.png" })}
      />
    );
    fireEvent.click(await screen.findByRole("button", { name: t("common.edit") }));
    fireEvent.click(screen.getByRole("button", { name: t("common.remove") }));
    await waitFor(() =>
      expect(toastMessages()).toContain(t("workspace.settings.domain.favicon_removed_successfully"))
    );
    expect(calls.map((call) => call.key)).toContain(`DELETE ${API}/whitelabel/favicon`);
  });

  test("a favicon over 100 KB is refused before it is uploaded", async () => {
    const calls = stubServer();
    renderAt(<FaviconCustomizationCard organization={organization("owner")} />);
    const big = new File([new Uint8Array(200_000)], "icon.png", { type: "image/png" });
    fireEvent.change(await enabledUploadInput(), { target: { files: [big] } });
    await waitFor(() => expect(toastMessages()).toEqual(["File exceeds 0.1 MB size limit."]));
    expect(calls.map((call) => call.key)).not.toContain("POST /api/v1/management/storage");
  });
});

describe("DomainSettingsPage", () => {
  const surveys: PrettyUrlSurvey[] = [
    {
      id: "s1",
      name: "Onboarding",
      slug: "welcome",
      status: "inProgress",
      createdAt: "2026-01-01T00:00:00Z",
      workspace: { id: WS, name: "Main", organizationId: ORG },
    },
  ];

  test("lists the pretty URLs with their survey and workspace", async () => {
    stubServer();
    renderAt(<DomainSettingsPage organization={organization("owner")} surveys={surveys} />);
    const link = await screen.findByRole("link", { name: "Onboarding" });
    expect(link.getAttribute("href")).toBe(`/workspaces/${WS}/surveys/s1/summary`);
    expect(screen.getByText("Main")).toBeTruthy();
    expect(screen.getByText("welcome")).toBeTruthy();
    expect(screen.getByText(t("workspace.settings.domain.favicon_customization"))).toBeTruthy();
    expect(screen.queryByText(t("common.storage_not_configured"))).toBeNull();
  });

  test("warns without file storage and says when there are no pretty URLs", async () => {
    stubServer(false);
    renderAt(<DomainSettingsPage organization={organization("owner")} surveys={[]} />);
    expect(await screen.findByText(t("common.storage_not_configured"))).toBeTruthy();
    expect(screen.getByText(t("workspace.settings.domain.no_pretty_urls"))).toBeTruthy();
  });
});

describe("settings navigation", () => {
  const navigation = (isFormaCloud: boolean) => (
    <SettingsNavigation
      organizationId={ORG}
      organizationName="Acme"
      membershipRole="owner"
      backUrl="/"
      pathname={`/organizations/${ORG}/settings/general`}
      isFormaCloud={isFormaCloud}
    />
  );

  test("self-hosted, the organization settings include the domain page", async () => {
    stubServer();
    renderAt(navigation(false));
    const domain = await screen.findByRole("link", { name: t("common.domain") });
    expect(domain.getAttribute("href")).toBe(`/organizations/${ORG}/settings/domain`);
  });

  test("on Forma Cloud there is no domain page", async () => {
    stubServer();
    renderAt(navigation(true));
    expect(await screen.findByRole("link", { name: t("common.billing") })).toBeTruthy();
    expect(screen.queryByRole("link", { name: t("common.domain") })).toBeNull();
  });
});
