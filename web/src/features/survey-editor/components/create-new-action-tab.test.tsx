import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import type { TSurvey } from "@forma/types/surveys/types";
import { initI18n } from "../../../lib/i18n";
import { toast } from "../../../lib/toast";
import { CreateNewActionTab } from "./create-new-action-tab";

// A workspace id as the Go server hands it out: a UUID, where forma2's were cuid2.
const WORKSPACE = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f";
const ACTION = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d20";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  vi.unstubAllGlobals();
});

/** The action-class route answering a create with the stored action, recording each request. */
const stubCreateRoute = () => {
  const requests: { path: string; method?: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      const body = JSON.parse(init.body as string) as Record<string, unknown>;
      requests.push({ path, method: init.method, body });
      const created = {
        ...body,
        id: ACTION,
        key: null,
        workspaceId: WORKSPACE,
        createdAt: "2026-10-23T00:00:00.000Z",
        updatedAt: "2026-10-23T00:00:00.000Z",
      };
      return new Response(JSON.stringify(created), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    })
  );
  return requests;
};

test("captures a new page-view action in a workspace whose id is a UUID and adds it to the survey's triggers", async () => {
  const requests = stubCreateRoute();
  const setOpen = vi.fn();
  let survey = { triggers: [] } as unknown as TSurvey;
  const setLocalSurvey = vi.fn((update: TSurvey | ((prev: TSurvey) => TSurvey)) => {
    survey = typeof update === "function" ? update(survey) : update;
  });

  render(
    <QueryClientProvider client={new QueryClient()}>
      <I18nextProvider i18n={i18n}>
        <CreateNewActionTab
          actionClasses={[]}
          isReadOnly={false}
          setLocalSurvey={setLocalSurvey}
          setOpen={setOpen}
          workspaceId={WORKSPACE}
        />
      </I18nextProvider>
    </QueryClientProvider>
  );

  fireEvent.click(screen.getByLabelText(i18n.t("workspace.actions.page_view")));
  fireEvent.change(screen.getByLabelText(i18n.t("workspace.actions.what_did_your_user_do")), {
    target: { value: "New Session" },
  });
  fireEvent.click(screen.getByRole("button", { name: i18n.t("workspace.actions.create_action") }));

  await waitFor(() => expect(setOpen).toHaveBeenCalledWith(false));
  expect(requests).toEqual([
    {
      path: `/api/app/workspaces/${WORKSPACE}/action-classes`,
      method: "POST",
      body: expect.objectContaining({
        name: "New Session",
        type: "noCode",
        workspaceId: WORKSPACE,
        noCodeConfig: expect.objectContaining({ type: "pageView", urlFilters: [] }),
      }),
    },
  ]);
  expect(survey.triggers.map((trigger) => trigger.actionClass.id)).toEqual([ACTION]);
  expect(toast.getSnapshot().map((shown) => shown.message)).toContain(
    i18n.t("workspace.actions.action_created_successfully")
  );
});
