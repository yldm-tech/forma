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
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import type { InstanceInfo } from "../../lib/queries/instance";
import type { WorkspaceDetail } from "../../lib/queries/workspaces";
import { toast } from "../../lib/toast";
import { WorkspaceLookPage } from "./look";

const ORG = "0190a000-0000-7000-8000-000000000001";
const WS = "0190a000-0000-7000-8000-000000000002";
const BUCKET = "https://bucket.example.com/uploads";
const LOGO = `/storage/${WS}/public/old.png`;

let i18n: I18n;
const t = (key: string, options?: Record<string, unknown>) => i18n.t(key, options);

beforeAll(async () => {
  i18n = await initI18n("en-US");
  // jsdom has no object URLs; the upload preview only needs one to exist.
  URL.createObjectURL = () => "blob:preview";
  // jsdom has the <dialog> element but not its modal methods, which the delete dialog opens with.
  HTMLDialogElement.prototype.showModal ??= function showModal(this: HTMLDialogElement) {
    this.open = true;
  };
  HTMLDialogElement.prototype.close ??= function close(this: HTMLDialogElement) {
    this.open = false;
  };
});

afterEach(() => {
  cleanup();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  vi.unstubAllGlobals();
});

const workspace = (logo: WorkspaceDetail["logo"] = null): WorkspaceDetail => ({
  id: WS,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  name: "Main",
  organizationId: ORG,
  styling: { allowStyleOverwrite: true },
  recontactDays: 7,
  inAppSurveyBranding: true,
  linkSurveyBranding: true,
  config: { channel: null, industry: null },
  placement: "bottomRight",
  clickOutsideClose: true,
  overlay: "none",
  languages: [],
  appSetupCompleted: false,
  logo,
  customHeadScripts: null,
  access: { role: "owner", canWrite: true, canManage: true, isReadOnly: false },
});

interface Call {
  key: string;
  body?: string;
}

/** The server and the bucket: the instance answers when `instance` resolves, uploads are signed for the workspace they name (a file called bad.png is refused by name) and accepted, and workspace updates are stored. Requests are recorded. */
const stubServer = (instance: Promise<InstanceInfo>) => {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      const key = `${init.method ?? "GET"} ${path}`;
      calls.push({ key, body: typeof init.body === "string" ? init.body : undefined });
      const json = (body: unknown, status = 200) =>
        new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
      switch (key) {
        case "GET /api/app/instance":
          return json(await instance);
        case "POST /api/v1/management/storage": {
          const { fileName, workspaceId } = JSON.parse(init.body as string) as {
            fileName: string;
            workspaceId: string;
          };
          if (fileName === "bad.png")
            return json({ code: "bad_request", message: "Invalid input", details: { fileName } }, 400);
          return json({
            data: {
              signedUrl: BUCKET,
              presignedFields: { key: fileName },
              fileUrl: `/storage/${workspaceId}/public/${fileName}`,
            },
          });
        }
        case `POST ${BUCKET}`:
          return new Response(null, { status: 204 });
        case `PATCH /api/app/workspaces/${WS}`:
          return json(workspace());
        default:
          throw new Error(`unexpected ${key}`);
      }
    })
  );
  return calls;
};

const storage = (storageConfigured: boolean) => Promise.resolve({ storageConfigured, version: "0.2.10" });

/** The page on a route without a workspace parameter, so an upload reaches the workspace only if the page names it. */
const renderPage = (detail: WorkspaceDetail) => {
  const rootRoute = createRootRoute({ component: Outlet });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/look",
        component: () => <WorkspaceLookPage workspace={detail} />,
      }),
    ]),
    history: createMemoryHistory({ initialEntries: ["/look"] }),
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
};

const png = (name: string) => new File(["img"], name, { type: "image/png" });
const toastMessages = () => toast.getSnapshot().map((shown) => shown.message);
/** The logo card; the theme card has its own Save button. */
const logoCard = () => within(document.getElementById("edit-logo")!);
/** Once the page is up, Edit on a saved logo offers replacing, removing and the background color. */
const startEditingLogo = async () => {
  await screen.findByText(t("workspace.look.theme"));
  fireEvent.click(logoCard().getByRole("button", { name: t("common.edit") }));
};
const logoImage = () => screen.queryByRole("img", { name: t("common.logo") });
const signedFor = (calls: Call[]) =>
  calls
    .filter((call) => call.key === "POST /api/v1/management/storage")
    .map((call) => JSON.parse(call.body ?? "") as { fileName: string; workspaceId: string });
const savedLogos = (calls: Call[]) =>
  calls
    .filter((call) => call.key === `PATCH /api/app/workspaces/${WS}`)
    .map((call) => (JSON.parse(call.body ?? "") as { logo: unknown }).logo);

describe("WorkspaceLookPage storage warning", () => {
  test("is shown once the instance reports no file storage, and not while that is unknown", async () => {
    let answer: (info: InstanceInfo) => void = () => {};
    const calls = stubServer(new Promise((resolve) => (answer = resolve)));
    renderPage(workspace());
    expect(await screen.findByText(t("workspace.look.theme"))).toBeTruthy();
    await waitFor(() => expect(calls.map((call) => call.key)).toContain("GET /api/app/instance"));
    expect(screen.queryByText(t("common.storage_not_configured"))).toBeNull();

    answer({ storageConfigured: false, version: "0.2.10" });
    expect(await screen.findByText(t("common.storage_not_configured"))).toBeTruthy();
  });
});

describe("WorkspaceLookPage logo", () => {
  test("a logo is uploaded to the workspace's storage and saved", async () => {
    const calls = stubServer(storage(true));
    renderPage(workspace());

    fireEvent.change(await screen.findByTestId("upload-file-input"), {
      target: { files: [png("brand.png")] },
    });
    await waitFor(() => expect(logoImage()?.getAttribute("src")).toBe(`/storage/${WS}/public/brand.png`));
    expect(signedFor(calls)).toEqual([expect.objectContaining({ fileName: "brand.png", workspaceId: WS })]);

    fireEvent.click(logoCard().getByRole("button", { name: t("common.save") }));
    await waitFor(() => expect(toastMessages()).toContain(t("workspace.look.logo_updated_successfully")));
    expect(savedLogos(calls)).toEqual([{ url: `/storage/${WS}/public/brand.png` }]);
  });

  test("replacing the logo uploads the new file and saves it with the background color", async () => {
    const calls = stubServer(storage(true));
    renderPage(workspace({ url: LOGO, bgColor: "#123456" }));

    await startEditingLogo();
    expect(logoCard().getByRole("button", { name: t("workspace.look.replace_logo") })).toBeTruthy();
    fireEvent.change(screen.getByTestId("logo-replace-input"), { target: { files: [png("new.png")] } });
    await waitFor(() => expect(logoImage()?.getAttribute("src")).toBe(`/storage/${WS}/public/new.png`));
    expect(signedFor(calls)).toEqual([expect.objectContaining({ fileName: "new.png", workspaceId: WS })]);

    fireEvent.click(logoCard().getByRole("button", { name: t("common.save") }));
    await waitFor(() => expect(toastMessages()).toContain(t("workspace.look.logo_updated_successfully")));
    expect(savedLogos(calls)).toEqual([{ url: `/storage/${WS}/public/new.png`, bgColor: "#123456" }]);
  });

  test("a refused replacement shows why and keeps the current logo", async () => {
    const calls = stubServer(storage(true));
    renderPage(workspace({ url: LOGO }));

    await startEditingLogo();
    fireEvent.change(screen.getByTestId("logo-replace-input"), { target: { files: [png("bad.png")] } });
    await waitFor(() => expect(toastMessages()).toEqual([t("common.invalid_file_name")]));
    expect(logoImage()?.getAttribute("src")).toBe(LOGO);
    expect(signedFor(calls)).toHaveLength(1);
  });

  test("without file storage a replacement is refused with the storage toast and nothing is uploaded", async () => {
    const calls = stubServer(storage(false));
    renderPage(workspace({ url: LOGO }));
    expect(await screen.findByText(t("common.storage_not_configured"))).toBeTruthy();

    fireEvent.click(logoCard().getByRole("button", { name: t("common.edit") }));
    fireEvent.click(logoCard().getByRole("button", { name: t("workspace.look.replace_logo") }));
    expect(toastMessages()).toEqual([t("common.file_storage_not_set_up")]);
    for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);

    fireEvent.change(screen.getByTestId("logo-replace-input"), { target: { files: [png("new.png")] } });
    expect(toastMessages()).toEqual([t("common.file_storage_not_set_up")]);
    expect(signedFor(calls)).toEqual([]);
    expect(logoImage()?.getAttribute("src")).toBe(LOGO);
  });

  test("removing the logo clears it and offers the upload again", async () => {
    const calls = stubServer(storage(true));
    renderPage(workspace({ url: LOGO }));

    await startEditingLogo();
    fireEvent.click(logoCard().getByRole("button", { name: t("workspace.look.remove_logo") }));
    fireEvent.click(await screen.findByRole("button", { name: t("common.delete") }));
    await waitFor(() => expect(toastMessages()).toContain(t("workspace.look.logo_removed_successfully")));
    expect(savedLogos(calls)).toEqual([{}]);
    expect(logoImage()).toBeNull();
    expect(screen.getByTestId("upload-file-input")).toBeTruthy();
  });
});
