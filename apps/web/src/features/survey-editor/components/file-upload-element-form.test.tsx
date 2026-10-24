import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import type { TSurveyFileUploadElement } from "@forma/types/surveys/elements";
import type { TSurvey } from "@forma/types/surveys/types";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { billingKeys } from "../../../lib/queries/billing";
import { toast } from "../../../lib/toast";
import { FileUploadElementForm } from "./file-upload-element-form";

const WS = "0190a000-0000-7000-8000-000000000002";
const ORG = "0190a000-0000-7000-8000-000000000001";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  vi.unstubAllGlobals();
});

/** The Go server: the survey's workspace and its organization's billing summary on Forma Cloud with the given workspace limit. */
const stubServer = (workspaces: number | null) =>
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), { headers: { "content-type": "application/json" } });
      if (path === `/api/app/workspaces/${WS}`) return json({ id: WS, organizationId: ORG, name: "Website" });
      if (path === `/api/app/organizations/${ORG}/billing/summary`)
        return json({
          isFormaCloud: true,
          plan: "hobby",
          limits: { workspaces, monthly: { responses: 250 } },
        });
      throw new Error(`unexpected ${path}`);
    })
  );

const renderForm = (maxSizeInMB?: number) => {
  const updates: Partial<TSurveyFileUploadElement>[] = [];
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const element = {
    id: "upload",
    type: "fileUpload",
    headline: { default: "Your CV" },
    required: true,
    allowMultipleFiles: false,
    maxSizeInMB,
  } as TSurveyFileUploadElement;
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <FileUploadElementForm
          localSurvey={
            {
              workspaceId: WS,
              languages: [],
              blocks: [{ id: "block", name: "Block 1", elements: [element] }],
              endings: [],
            } as unknown as TSurvey
          }
          element={element}
          elementIdx={0}
          updateElement={(_idx, attributes) => updates.push(attributes as Partial<TSurveyFileUploadElement>)}
          isInvalid={false}
          locale="en-US"
        />
      </QueryClientProvider>
    </I18nextProvider>
  );
  // The cap applies once the organization's plan is known.
  const planKnown = () =>
    waitFor(() => expect(queryClient.getQueryData(billingKeys.summary(ORG))).toBeDefined());
  return { updates, planKnown };
};

test("on a one-workspace Forma Cloud plan a larger file size is capped at 10 MB, with the way to upgrade", async () => {
  stubServer(1);
  const { updates, planKnown } = renderForm(5);
  const input = await screen.findByRole("spinbutton");
  await planKnown();
  fireEvent.change(input, { target: { value: "50" } });
  expect(updates.at(-1)).toEqual({ maxSizeInMB: 10 });
  expect(toast.getSnapshot().map((shown) => shown.message)).toContain("Max file size limit is 10 MB.");
  expect(screen.getByRole("link", { name: "upgrade your plan" }).getAttribute("href")).toBe(
    `/app/organizations/${ORG}/settings/billing`
  );
});

test("a plan with more workspaces may allow up to 1 GB", async () => {
  stubServer(3);
  const { updates, planKnown } = renderForm(5);
  const input = await screen.findByRole("spinbutton");
  await planKnown();
  fireEvent.change(input, { target: { value: "500" } });
  expect(updates.at(-1)).toEqual({ maxSizeInMB: 500 });
  expect(toast.getSnapshot()).toEqual([]);
});
