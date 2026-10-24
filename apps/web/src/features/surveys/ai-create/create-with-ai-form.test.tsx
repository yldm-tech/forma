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
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { CreateWithAIForm } from "./create-with-ai-form";

const WS = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f";
const ORG = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d00";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  window.scrollTo = () => {};
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

/** An NDJSON body delivered in the given chunks, which need not end on a line boundary. */
const ndjson = (chunks: string[]) =>
  new Response(
    new ReadableStream<Uint8Array>({
      start(controller) {
        const encoder = new TextEncoder();
        for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
        controller.close();
      },
    }),
    { status: 200, headers: { "content-type": "application/x-ndjson; charset=utf-8" } }
  );

const renderForm = (form: ReactNode) => {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({ getParentRoute: () => rootRoute, path: "/", component: () => form }),
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

const payload = {
  name: "Onboarding check-in",
  blocks: [
    {
      name: "Block 1",
      elements: [{ id: "q_1_abc", type: "openText", headline: { "en-US": "How was your first week?" } }],
    },
  ],
};

test("the generation stream fills the draft as it arrives, and saving creates the survey from the final payload", async () => {
  const requests: { path: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit) => {
      requests.push({ path, body: JSON.parse(init.body as string) as unknown });
      if (path === "/api/app/surveys/generate/stream") {
        const partial = JSON.stringify({
          type: "partial",
          seq: 1,
          draft: {
            name: "Onboarding check-in",
            blocks: [
              { name: "Block 1", questions: [{ type: "openText", headline: "How was your first week?" }] },
            ],
          },
        });
        const done = JSON.stringify({
          type: "done",
          language: "en-US",
          payload,
          validation: {
            valid: true,
            invalid_params: [],
            languages: [{ code: "en-US", default: true, enabled: true }],
          },
        });
        // Lines split across chunks, as a network delivers them.
        return ndjson([
          `{"type":"start","requestId":"r1"}\n${partial.slice(0, 20)}`,
          `${partial.slice(20)}\n${done}`,
          "\n",
        ]);
      }
      return new Response(JSON.stringify({ data: { id: "new-survey" } }), {
        status: 201,
        headers: { "content-type": "application/json" },
      });
    })
  );
  const onSuccess = vi.fn();
  renderForm(
    <CreateWithAIForm
      workspaceId={WS}
      organizationId={ORG}
      locale="en-US"
      isAIAvailable
      onSuccess={onSuccess}
    />
  );

  fireEvent.change(await screen.findByLabelText(i18n.t("workspace.surveys.ai_create.prompt_label")), {
    target: { value: "An onboarding check-in for new hires after their first week" },
  });
  fireEvent.click(screen.getByRole("button", { name: i18n.t("workspace.surveys.ai_create.create") }));

  expect(await screen.findByText("How was your first week?")).toBeTruthy();
  fireEvent.click(
    await screen.findByRole("button", { name: i18n.t("workspace.surveys.ai_create.open_in_editor") })
  );

  await waitFor(() => expect(onSuccess).toHaveBeenCalledWith("new-survey"));
  expect(requests[0]).toEqual({
    path: "/api/app/surveys/generate/stream",
    body: {
      workspaceId: WS,
      prompt: "An onboarding check-in for new hires after their first week",
      type: "link",
      languages: ["en-US"],
    },
  });
  expect(requests[1]).toEqual({ path: "/api/v3/surveys?createdFrom=ai", body: payload });
});

test("a refusal before the stream opens is shown, and the prompt is kept to try again", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            code: "too_many_requests",
            message: "Too many requests",
            details: { retryAfter: 30 },
          }),
          { status: 429, headers: { "content-type": "application/json" } }
        )
    )
  );
  renderForm(<CreateWithAIForm workspaceId={WS} locale="en-US" isAIAvailable onSuccess={vi.fn()} />);

  const prompt = await screen.findByLabelText(i18n.t("workspace.surveys.ai_create.prompt_label"));
  fireEvent.change(prompt, {
    target: { value: "An onboarding check-in for new hires after their first week" },
  });
  fireEvent.click(screen.getByRole("button", { name: i18n.t("workspace.surveys.ai_create.create") }));

  expect(await screen.findByText(i18n.t("common.error"))).toBeTruthy();
  expect(
    (screen.getByLabelText(i18n.t("workspace.surveys.ai_create.prompt_label")) as HTMLTextAreaElement).value
  ).toBe("An onboarding check-in for new hires after their first week");
});

test("AI switched off for the organization says so and links to where it is switched on", async () => {
  renderForm(
    <CreateWithAIForm
      workspaceId={WS}
      organizationId={ORG}
      locale="en-US"
      isAIAvailable={false}
      aiUnavailableReason="not_enabled"
      onSuccess={vi.fn()}
    />
  );

  expect(await screen.findByText(i18n.t("common.ai_unavailable.not_enabled"))).toBeTruthy();
  expect(
    screen
      .getByRole("link", { name: i18n.t("common.ai_unavailable.enable_in_settings") })
      .getAttribute("href")
  ).toBe(`/organizations/${ORG}/settings/general`);
  expect(screen.queryByLabelText(i18n.t("workspace.surveys.ai_create.prompt_label"))).toBeNull();
});
