import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import type { TSurvey } from "@forma/types/surveys/types";
import type { i18n as I18n } from "i18next";
import type { ReactNode } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { toast } from "../../../lib/toast";
import { EXAMPLE_STATUS_CADENCE_MS, useExampleResponsesAction } from "./survey-analysis-cta";

const WORKSPACE_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f";
const SURVEY_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  for (const { id } of toast.getSnapshot()) toast.dismiss(id);
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const survey = (archived = false) =>
  ({
    id: SURVEY_ID,
    workspaceId: WORKSPACE_ID,
    archivedAt: archived ? new Date() : null,
  }) as unknown as TSurvey;

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

/** The server: the workspace's AI availability, and the generation's answer, which the test may hold back. */
const stubServer = (ai: unknown, generation: () => Promise<Response>) => {
  const posts: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      if (path === `/api/app/workspaces/${WORKSPACE_ID}/ai`) return json(200, ai);
      if (init?.method === "POST") {
        posts.push(path);
        return generation();
      }
      return json(404, { code: "not_found", message: "Not found" });
    })
  );
  return posts;
};

const renderAction = (props: { responseCount?: number; isReadOnly?: boolean; archived?: boolean } = {}) => {
  const onGenerated = vi.fn(async () => {});
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    </I18nextProvider>
  );
  const hook = renderHook(
    () =>
      useExampleResponsesAction({
        survey: survey(props.archived),
        workspaceId: WORKSPACE_ID,
        responseCount: props.responseCount ?? 0,
        isReadOnly: props.isReadOnly ?? false,
        onGenerated,
      }),
    { wrapper }
  );
  return { ...hook, onGenerated };
};

const shown = () => toast.getSnapshot().map(({ kind, message }) => ({ kind, message }));

// forma2's SurveyAnalysisCTA: the example-responses button and its states.
describe("useExampleResponsesAction", () => {
  test("is offered to writers of a survey that is not archived", () => {
    stubServer({ available: true }, async () => json(200, { createdCount: 20 }));
    expect(renderAction().result.current.isVisible).toBe(true);
    expect(renderAction({ isReadOnly: true }).result.current.isVisible).toBe(false);
    expect(renderAction({ archived: true }).result.current.isVisible).toBe(false);
  });

  test("is disabled once the survey has responses", async () => {
    stubServer({ available: true }, async () => json(200, { createdCount: 20 }));
    const { result } = renderAction({ responseCount: 3 });
    await waitFor(() =>
      expect(result.current.tooltip).toBe(
        "You can only generate example responses for surveys without responses. Reset survey to try."
      )
    );
    expect(result.current.disabled).toBe(true);
  });

  test("is disabled while AI is unavailable, saying why", async () => {
    stubServer({ available: false, reason: "not_enabled" }, async () => json(200, { createdCount: 20 }));
    const { result } = renderAction();
    await waitFor(() => expect(result.current.tooltip).toBe(i18n.t("common.ai_unavailable.not_enabled")));
    expect(result.current.disabled).toBe(true);
  });

  test("shows status lines while generating, then the result, and refreshes the analysis", async () => {
    let answer: (response: Response) => void = () => {};
    const posts = stubServer(
      { available: true },
      () => new Promise<Response>((resolve) => (answer = resolve))
    );
    const { result, onGenerated } = renderAction();
    await waitFor(() => expect(result.current.disabled).toBe(false));
    expect(result.current.tooltip).toBe("Generate example responses");

    vi.useFakeTimers();
    act(() => result.current.onClick?.());
    expect(result.current.disabled).toBe(true);
    expect(result.current.tooltip).toBe("Generating example responses…");
    expect(shown()).toEqual([{ kind: "loading", message: "Reading your questions…" }]);
    act(() => {
      vi.advanceTimersByTime(EXAMPLE_STATUS_CADENCE_MS);
    });
    expect(shown()).toEqual([{ kind: "loading", message: "Drafting realistic answers…" }]);
    act(() => {
      vi.advanceTimersByTime(EXAMPLE_STATUS_CADENCE_MS * 4);
    });
    // The last line holds: cycling back would read as the work restarting.
    expect(shown()).toEqual([{ kind: "loading", message: "Saving the responses…" }]);
    vi.useRealTimers();

    await act(async () => answer(json(200, { createdCount: 20 })));
    await waitFor(() =>
      expect(shown()).toEqual([{ kind: "success", message: "Generated 20 example responses." }])
    );
    expect(posts).toEqual([`/api/app/surveys/${SURVEY_ID}/ai/example-responses`]);
    expect(onGenerated).toHaveBeenCalledTimes(1);
    expect(result.current.disabled).toBe(false);
  });

  test("a refusal shows the server's reason; a failure forma2's message", async () => {
    let status = 403;
    stubServer({ available: true }, async () =>
      status === 403
        ? json(403, {
            code: "operation_not_allowed",
            message: "Example responses can only be generated for a survey that has no responses yet.",
          })
        : json(500, { code: "internal_server_error", message: "Something went wrong" })
    );
    const { result, onGenerated } = renderAction();
    await waitFor(() => expect(result.current.disabled).toBe(false));
    await act(async () => result.current.onClick?.());
    await waitFor(() =>
      expect(shown()).toEqual([
        {
          kind: "error",
          message: "Example responses can only be generated for a survey that has no responses yet.",
        },
      ])
    );
    status = 500;
    await act(async () => result.current.onClick?.());
    await waitFor(() =>
      expect(shown()).toContainEqual({
        kind: "error",
        message: "Couldn't generate example responses. Please try again.",
      })
    );
    expect(onGenerated).not.toHaveBeenCalled();
  });
});
