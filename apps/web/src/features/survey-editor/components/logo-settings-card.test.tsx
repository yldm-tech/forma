import type { TSurveyStyling } from "@forma/types/surveys/types";
import type { TWorkspaceStyling } from "@forma/types/workspace";
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
import { FormProvider, type UseFormReturn, useForm, useWatch } from "react-hook-form";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { instanceQuery } from "../../../lib/queries/instance";
import { toast } from "../../../lib/toast";
import { LogoSettingsCard } from "./logo-settings-card";

const WS = "0190a000-0000-7000-8000-000000000002";
const SURVEY = "0190a000-0000-7000-8000-000000000003";
const BUCKET = "https://bucket.example.com/uploads";
const LOGO = `/storage/${WS}/public/old.png`;

let i18n: I18n;
const t = (key: string, options?: Record<string, unknown>) => i18n.t(key, options);

beforeAll(async () => {
  i18n = await initI18n("en-US");
  // jsdom has no object URLs; the upload preview only needs one to exist.
  URL.createObjectURL = () => "blob:preview";
});

afterEach(() => {
  cleanup();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  vi.unstubAllGlobals();
});

interface Call {
  key: string;
  body?: string;
}

/** The server and the bucket: uploads are signed for the workspace they name (a file called bad.png is refused by name) and accepted. Requests are recorded. */
const stubServer = () => {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      const key = `${init.method ?? "GET"} ${path}`;
      calls.push({ key, body: typeof init.body === "string" ? init.body : undefined });
      const json = (body: unknown, status = 200) =>
        new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
      switch (key) {
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
        default:
          throw new Error(`unexpected ${key}`);
      }
    })
  );
  return calls;
};

type StylingForm = UseFormReturn<TWorkspaceStyling | TSurveyStyling>;

/** The card in the survey editor's route, open, on a styling form holding `logo`, which the harness prints so a test can read what the card set; the instance's storage answer is already known. */
const renderCard = (logo: TSurveyStyling["logo"], storageConfigured = true) => {
  const Harness = () => {
    const styling = useForm<TSurveyStyling>({ defaultValues: { logo } }) as StylingForm;
    const current = useWatch({ control: styling.control, name: "logo" });
    return (
      <FormProvider {...styling}>
        <LogoSettingsCard open setOpen={() => {}} form={styling} />
        <output data-testid="form-logo">{JSON.stringify(current ?? null)}</output>
      </FormProvider>
    );
  };
  const rootRoute = createRootRoute({ component: Outlet });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/workspaces/$workspaceId/surveys/$surveyId/edit",
        component: Harness,
      }),
    ]),
    history: createMemoryHistory({ initialEntries: [`/workspaces/${WS}/surveys/${SURVEY}/edit`] }),
  });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  queryClient.setQueryData(instanceQuery.queryKey, {
    storageConfigured,
    customScripts: "requires_public_url",
    version: "0.2.10",
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  return { logo: () => JSON.parse(screen.getByTestId("form-logo").textContent ?? "") as unknown };
};

const png = (name: string) => new File(["img"], name, { type: "image/png" });
const toastMessages = () => toast.getSnapshot().map((shown) => shown.message);
const logoImage = () => screen.queryByRole("img", { name: "Survey Logo" });
const signedFor = (calls: Call[]) =>
  calls
    .filter((call) => call.key === "POST /api/v1/management/storage")
    .map((call) => JSON.parse(call.body ?? "") as { fileName: string; workspaceId: string });

describe("LogoSettingsCard", () => {
  test("a logo is uploaded to the survey's workspace and set on the form", async () => {
    const calls = stubServer();
    const card = renderCard(undefined);

    fireEvent.change(await screen.findByTestId("upload-file-input"), {
      target: { files: [png("brand.png")] },
    });
    await waitFor(() => expect(card.logo()).toEqual({ url: `/storage/${WS}/public/brand.png` }));
    expect(signedFor(calls)).toEqual([expect.objectContaining({ fileName: "brand.png", workspaceId: WS })]);
    expect(logoImage()?.getAttribute("src")).toBe(`/storage/${WS}/public/brand.png`);
  });

  test("replacing the logo uploads the new file and keeps its background color", async () => {
    const calls = stubServer();
    const card = renderCard({ url: LOGO, bgColor: "#123456" });

    const input = (await screen.findByTestId("survey-logo-replace-input")) as HTMLInputElement;
    fireEvent.change(input, { target: { files: [png("new.png")] } });
    await waitFor(() =>
      expect(card.logo()).toEqual({ url: `/storage/${WS}/public/new.png`, bgColor: "#123456" })
    );
    expect(signedFor(calls)).toEqual([expect.objectContaining({ fileName: "new.png", workspaceId: WS })]);
    expect(logoImage()?.getAttribute("src")).toBe(`/storage/${WS}/public/new.png`);
    expect(
      screen.getByRole("button", { name: t("workspace.look.replace_logo") }).hasAttribute("disabled")
    ).toBe(false);
  });

  test("a refused replacement shows why and keeps the current logo", async () => {
    const calls = stubServer();
    const card = renderCard({ url: LOGO });

    fireEvent.change(await screen.findByTestId("survey-logo-replace-input"), {
      target: { files: [png("bad.png")] },
    });
    await waitFor(() => expect(toastMessages()).toEqual([t("common.invalid_file_name")]));
    expect(signedFor(calls)).toHaveLength(1);
    expect(card.logo()).toEqual({ url: LOGO });
    expect(
      screen.getByRole("button", { name: t("workspace.look.replace_logo") }).hasAttribute("disabled")
    ).toBe(false);
  });

  test("without file storage a replacement is refused with the storage toast and nothing is uploaded", async () => {
    const calls = stubServer();
    const card = renderCard({ url: LOGO }, false);

    fireEvent.click(await screen.findByRole("button", { name: t("workspace.look.replace_logo") }));
    expect(toastMessages()).toEqual([t("common.file_storage_not_set_up")]);
    for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);

    fireEvent.change(screen.getByTestId("survey-logo-replace-input"), {
      target: { files: [png("new.png")] },
    });
    expect(toastMessages()).toEqual([t("common.file_storage_not_set_up")]);
    expect(signedFor(calls)).toEqual([]);
    expect(card.logo()).toEqual({ url: LOGO });
  });

  test("removing the logo clears it from the form and offers the upload again", async () => {
    stubServer();
    const card = renderCard({ url: LOGO, bgColor: "#123456" });

    fireEvent.click(await screen.findByRole("button", { name: t("workspace.look.remove_logo") }));
    expect(card.logo()).toBeNull();
    expect(logoImage()).toBeNull();
    expect(screen.getByTestId("upload-file-input")).toBeTruthy();
  });
});
