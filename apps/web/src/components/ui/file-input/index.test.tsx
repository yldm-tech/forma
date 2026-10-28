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
import { initI18n } from "../../../lib/i18n";
import { toast } from "../../../lib/toast";
import { FileInput } from "./index";

const convert = vi.hoisted(() => vi.fn());
vi.mock("heic-convert/browser", () => ({ default: convert }));

const WS = "0190a000-0000-7000-8000-000000000002";
const BUCKET = "https://bucket.example.com/uploads";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  // jsdom has no object URLs; the preview only needs one to exist.
  URL.createObjectURL = () => "blob:preview";
});

afterEach(() => {
  cleanup();
  convert.mockReset();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  vi.unstubAllGlobals();
});

interface Call {
  key: string;
  body: unknown;
}

/** The server and the bucket: the instance says whether storage is configured, every upload is signed and accepted. Requests are recorded. */
const stubServer = (storageConfigured: boolean, signStatus = 200) => {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      const key = `${init.method ?? "GET"} ${path}`;
      calls.push({ key, body: init.body });
      const json = (status: number, body: unknown) =>
        new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
      if (key === "GET /api/app/instance") return json(200, { storageConfigured, version: "0.2.10" });
      if (key === "POST /api/v1/management/storage") {
        const { fileName } = JSON.parse(init.body as string) as { fileName: string };
        return signStatus === 200
          ? json(200, {
              data: {
                signedUrl: BUCKET,
                presignedFields: { key: fileName },
                fileUrl: `/storage/${WS}/public/${fileName}`,
              },
            })
          : json(signStatus, { code: "bad_request", message: "Invalid input", details: { fileName } });
      }
      if (key === `POST ${BUCKET}`) return new Response(null, { status: 204 });
      throw new Error(`unexpected ${key}`);
    })
  );
  return calls;
};

const renderAt = (element: ReactNode) => {
  const rootRoute = createRootRoute({ component: Outlet });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/workspaces/$workspaceId",
        component: () => element,
      }),
    ]),
    history: createMemoryHistory({ initialEntries: [`/workspaces/${WS}`] }),
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
};

const image = (name: string, type = "image/png") => new File(["img"], name, { type });

describe("FileInput", () => {
  test("a picked file is uploaded to the route's workspace and handed back by its URL", async () => {
    const calls = stubServer(true);
    const onFileUpload = vi.fn();
    renderAt(<FileInput id="logo" onFileUpload={onFileUpload} maxSizeInMB={5} />);

    const input = await screen.findByTestId("upload-file-input");
    fireEvent.change(input, { target: { files: [image("logo.png")] } });

    await waitFor(() =>
      expect(onFileUpload).toHaveBeenCalledWith([`/storage/${WS}/public/logo.png`], "image")
    );
    const sign = calls.find((call) => call.key === "POST /api/v1/management/storage");
    expect(JSON.parse(sign?.body as string)).toEqual({
      fileName: "logo.png",
      fileType: "image/png",
      fileSize: 3,
      allowedFileExtensions: ["png", "jpeg", "jpg", "webp", "heic"],
      workspaceId: WS,
    });
    expect(calls.map((call) => call.key)).toContain(`POST ${BUCKET}`);
  });

  test("dropped files are uploaded after the ones the field has", async () => {
    stubServer(true);
    const onFileUpload = vi.fn();
    renderAt(
      <FileInput
        id="choices"
        onFileUpload={onFileUpload}
        fileUrl={["/storage/a/public/one.png"]}
        multiple
        workspaceId={WS}
      />
    );

    fireEvent.drop(await screen.findByTestId("upload-file-label"), {
      dataTransfer: { files: [image("two.png"), image("three.webp", "image/webp")] },
    });

    await waitFor(() =>
      expect(onFileUpload).toHaveBeenCalledWith(
        ["/storage/a/public/one.png", `/storage/${WS}/public/two.png`, `/storage/${WS}/public/three.webp`],
        "image"
      )
    );
  });

  test("a HEIC photo is uploaded as the JPEG it converts to", async () => {
    convert.mockResolvedValue(new Uint8Array([0xff, 0xd8]));
    const calls = stubServer(true);
    const onFileUpload = vi.fn();
    renderAt(<FileInput id="logo" onFileUpload={onFileUpload} workspaceId={WS} />);

    fireEvent.change(await screen.findByTestId("upload-file-input"), {
      target: { files: [image("photo.heic", "image/heic")] },
    });

    await waitFor(() =>
      expect(onFileUpload).toHaveBeenCalledWith([`/storage/${WS}/public/photo.jpg`], "image")
    );
    const sign = calls.find((call) => call.key === "POST /api/v1/management/storage");
    expect(JSON.parse(sign?.body as string)).toMatchObject({ fileName: "photo.jpg", fileType: "image/jpeg" });
  });

  test("a refused upload says why and changes nothing", async () => {
    stubServer(true, 400);
    const onFileUpload = vi.fn();
    renderAt(<FileInput id="logo" onFileUpload={onFileUpload} workspaceId={WS} />);

    fireEvent.change(await screen.findByTestId("upload-file-input"), { target: { files: [image("x.png")] } });

    await waitFor(() =>
      expect(toast.getSnapshot().map((shown) => shown.message)).toEqual([i18n.t("common.invalid_file_name")])
    );
    expect(onFileUpload).not.toHaveBeenCalled();
    expect(await screen.findByTestId("upload-file-label")).toBeTruthy();
  });

  test("without file storage a click or a drop explains, and nothing is uploaded", async () => {
    const calls = stubServer(false);
    const onFileUpload = vi.fn();
    renderAt(<FileInput id="logo" onFileUpload={onFileUpload} workspaceId={WS} />);

    await waitFor(() => expect(screen.queryByTestId("upload-file-input")).toBeNull());
    fireEvent.click(screen.getByTestId("upload-file-label"));
    expect(toast.getSnapshot().map((shown) => shown.message)).toEqual([
      i18n.t("common.file_storage_not_set_up"),
    ]);
    for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);

    fireEvent.drop(screen.getByTestId("upload-file-label"), { dataTransfer: { files: [image("x.png")] } });
    expect(toast.getSnapshot().map((shown) => shown.message)).toEqual([
      i18n.t("common.file_storage_not_set_up"),
    ]);
    expect(calls.map((call) => call.key)).toEqual(["GET /api/app/instance"]);
    expect(onFileUpload).not.toHaveBeenCalled();
  });
});
