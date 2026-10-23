import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurveyQuota } from "@forma/types/quota";
import type { TSurvey } from "@forma/types/surveys/types";
import { initI18n } from "../../../../lib/i18n";
import { toast } from "../../../../lib/toast";
import { QuotaModal } from "./quota-modal";

// A survey and a quota as the Go server returns them: UUIDs for the records it creates, cuid2 for the cards and conditions the editor creates.
const SURVEY = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e";
const QUOTA = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d30";
const ENDING = "endingone000000000000000";

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
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  vi.unstubAllGlobals();
});

const survey = {
  id: SURVEY,
  workspaceId: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f",
  name: "Feedback",
  type: "link",
  status: "draft",
  blocks: [
    {
      id: "blockone0000000000000000",
      name: "Block 1",
      elements: [
        {
          id: "q1",
          type: TSurveyElementTypeEnum.OpenText,
          headline: { default: "How was it?" },
          required: true,
          inputType: "text",
          charLimit: { enabled: false },
        },
      ],
    },
  ],
  endings: [{ id: ENDING, type: "endScreen", headline: { default: "Thanks" } }],
  hiddenFields: { enabled: true, fieldIds: [] },
  variables: [],
  languages: [],
} as unknown as TSurvey;

const quota: TSurveyQuota = {
  id: QUOTA,
  createdAt: new Date("2026-10-23T00:00:00.000Z"),
  updatedAt: new Date("2026-10-23T00:00:00.000Z"),
  surveyId: SURVEY,
  name: "Adults",
  limit: 100,
  logic: {
    connector: "and",
    conditions: [
      {
        id: "conditionone000000000000",
        leftOperand: { type: "element", value: "q1" },
        operator: "isSubmitted",
      },
    ],
  },
  action: "endSurvey",
  endingCardId: ENDING,
  countPartialSubmissions: false,
};

/** The quota route answering a save with the stored quota, recording each request. */
const stubQuotaRoute = () => {
  const requests: { path: string; method?: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      const body = JSON.parse(init.body as string) as Record<string, unknown>;
      requests.push({ path, method: init.method, body });
      const stored = {
        ...body,
        id: QUOTA,
        createdAt: "2026-10-23T00:00:00.000Z",
        updatedAt: "2026-10-23T00:00:00.000Z",
      };
      return new Response(JSON.stringify(stored), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    })
  );
  return requests;
};

test("saves a quota of a survey whose id is a UUID", async () => {
  const requests = stubQuotaRoute();
  const onClose = vi.fn();

  render(
    <QueryClientProvider client={new QueryClient()}>
      <I18nextProvider i18n={i18n}>
        <QuotaModal
          open
          onOpenChange={() => {}}
          survey={survey}
          quota={quota}
          setQuotaToDelete={() => {}}
          onClose={onClose}
          duplicateQuota={() => {}}
          hasResponses={false}
          quotaResponseCount={0}
        />
      </I18nextProvider>
    </QueryClientProvider>
  );

  fireEvent.change(screen.getByLabelText(i18n.t("common.label")), { target: { value: "Adults only" } });
  fireEvent.click(screen.getByRole("button", { name: i18n.t("common.save") }));

  await waitFor(() => expect(onClose).toHaveBeenCalled());
  expect(requests).toEqual([
    {
      path: `/api/app/quotas/${QUOTA}`,
      method: "PATCH",
      body: expect.objectContaining({
        name: "Adults only",
        surveyId: SURVEY,
        action: "endSurvey",
        endingCardId: ENDING,
      }),
    },
  ]);
  expect(toast.getSnapshot().map((shown) => shown.message)).toContain(
    i18n.t("workspace.surveys.edit.quotas.quota_updated_successfull_toast")
  );
});
