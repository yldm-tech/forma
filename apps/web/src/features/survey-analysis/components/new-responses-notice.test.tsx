import {
  QueryClient,
  QueryClientProvider,
  focusManager,
  useInfiniteQuery,
  useQuery,
} from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { TSurvey } from "@forma/types/surveys/types";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { useState } from "react";
import {
  NEW_RESPONSES_POLL_MS,
  RESPONSES_PER_PAGE,
  surveyResponseCountQuery,
  surveyResponsesQuery,
  surveySummaryQuery,
} from "../api";
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

/** The server's response count, which a test raises as responses arrive, and how often it was asked; the summary and the response list answer too, a full page of responses at a time. */
const stubCount = (initial: number) => {
  const server = { count: initial, reads: 0 };
  const json = (body: unknown) =>
    new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      switch (path) {
        case `/api/app/surveys/${SURVEY_ID}/responses/count`:
          server.reads++;
          return json({ count: server.count });
        case `/api/app/surveys/${SURVEY_ID}/summary`:
          return json({ meta: {}, dropOff: [], quotas: [], summary: [] });
        case `/api/app/surveys/${SURVEY_ID}/responses/search`:
          return json(
            Array.from({ length: RESPONSES_PER_PAGE }, (_, i) => ({
              id: String(i),
              createdAt: "2026-10-25T00:00:00Z",
              updatedAt: "2026-10-25T00:00:00Z",
              tags: [],
            }))
          );
        default:
          throw new Error(`unexpected ${path}`);
      }
    })
  );
  return server;
};

/** The notice under the count the analysis layout loads with the page. */
const Harness = ({ survey }: { survey: TSurvey }) => {
  const loaded = useQuery(surveyResponseCountQuery(survey.id, {}));
  return <NewResponsesNotice survey={survey} loadedCount={loaded.data} />;
};

/** The notice over a summary page whose filter a button changes, as the filter bar does. */
const SummaryHarness = ({ survey }: { survey: TSurvey }) => {
  const [filtered, setFiltered] = useState(false);
  useQuery(surveySummaryQuery(survey.id, filtered ? { finished: true } : {}));
  return (
    <>
      <Harness survey={survey} />
      <button type="button" onClick={() => setFiltered(true)}>
        Completed only
      </button>
    </>
  );
};

/** The notice over a response list whose next page a button loads, as scrolling to its end does. */
const ResponsesHarness = ({ survey }: { survey: TSurvey }) => {
  const list = useInfiniteQuery(surveyResponsesQuery(survey.id, {}));
  return (
    <>
      <Harness survey={survey} />
      <button type="button" onClick={() => void list.fetchNextPage()}>
        Load more
      </button>
      <output>{list.data?.pages.length ?? 0} pages</output>
    </>
  );
};

const renderNotice = (status: TSurvey["status"], Page = Harness) => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 30_000 } } });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <Page survey={{ id: SURVEY_ID, status } as unknown as TSurvey} />
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

  test("stops offering the responses a new read of the page's data brought in", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const server = stubCount(3);
    renderNotice("inProgress", SummaryHarness);
    await waitFor(() => expect(server.reads).toBeGreaterThanOrEqual(2));
    server.count = 5;
    await nextPoll();
    await screen.findByRole("button", { name: /^2 new responses/ });

    // Another filter reads the summary again, with the two responses in it.
    fireEvent.click(screen.getByRole("button", { name: "Completed only" }));
    await waitFor(() => expect(screen.queryByRole("button", { name: /new response/ })).toBeNull());
    server.count = 6;
    await nextPoll();
    await screen.findByRole("button", { name: /^1 new response\s*Refresh results$/ });
  });

  test("keeps offering new responses when a further page of the list loads", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const server = stubCount(3);
    renderNotice("inProgress", ResponsesHarness);
    await screen.findByText("1 pages");
    await waitFor(() => expect(server.reads).toBeGreaterThanOrEqual(2));
    server.count = 5;
    await nextPoll();
    await screen.findByRole("button", { name: /^2 new responses/ });

    const reads = server.reads;
    fireEvent.click(screen.getByRole("button", { name: "Load more" }));
    await screen.findByText("2 pages");
    await act(() => vi.advanceTimersByTimeAsync(100));
    expect(server.reads).toBe(reads);
    expect(screen.getByRole("button", { name: /^2 new responses/ })).toBeTruthy();
  });
});
