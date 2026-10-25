import { QueryClient, QueryClientProvider, focusManager, useQuery } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { TSurvey } from "@forma/types/surveys/types";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { NEW_RESPONSES_POLL_MS, surveyResponseCountQuery } from "../api";
import { NewResponsesNotice } from "./new-responses-notice";

const SURVEY_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  focusManager.setFocused(undefined);
});

/** The server's response count, which a test raises as responses arrive, and how often it was asked. */
const stubCount = (initial: number) => {
  const server = { count: initial, reads: 0 };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      if (path !== `/api/app/surveys/${SURVEY_ID}/responses/count`) throw new Error(`unexpected ${path}`);
      server.reads++;
      return new Response(JSON.stringify({ count: server.count }), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    })
  );
  return server;
};

/** The notice under the count the analysis layout loads with the page. */
const Harness = ({ survey }: { survey: TSurvey }) => {
  const loaded = useQuery(surveyResponseCountQuery(survey.id, {}));
  return <NewResponsesNotice survey={survey} loadedCount={loaded.data} />;
};

const renderNotice = (status: TSurvey["status"]) => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 30_000 } } });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <Harness survey={{ id: SURVEY_ID, status } as unknown as TSurvey} />
      </QueryClientProvider>
    </I18nextProvider>
  );
};

const nextPoll = () => act(() => vi.advanceTimersByTimeAsync(NEW_RESPONSES_POLL_MS));

describe("NewResponsesNotice", () => {
  test("offers the responses that arrived since the page loaded, and reloads them on click", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const server = stubCount(3);
    renderNotice("inProgress");
    await waitFor(() => expect(server.reads).toBe(2));
    expect(screen.queryByRole("button")).toBeNull();

    server.count = 5;
    await nextPoll();
    const pill = await screen.findByRole("button", { name: /^2 new responses\s*Refresh results$/ });
    expect(screen.getByRole("status").contains(pill)).toBe(true);

    fireEvent.click(pill);
    await waitFor(() => expect(screen.queryByRole("button")).toBeNull());
  });

  test("does not poll a survey that is not collecting responses", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const server = stubCount(3);
    renderNotice("completed");
    await waitFor(() => expect(server.reads).toBe(1));
    server.count = 5;
    await nextPoll();
    await nextPoll();
    expect(server.reads).toBe(1);
    expect(screen.queryByRole("button")).toBeNull();
  });

  test("does not poll from a background tab", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const server = stubCount(3);
    renderNotice("inProgress");
    await waitFor(() => expect(server.reads).toBe(2));
    focusManager.setFocused(false);
    server.count = 5;
    await nextPoll();
    await nextPoll();
    expect(server.reads).toBe(2);
    expect(screen.queryByRole("button")).toBeNull();
  });
});
