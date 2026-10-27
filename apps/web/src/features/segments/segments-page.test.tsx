import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import type { Segment, SegmentResultShare } from "../../lib/queries/contacts";
import { SegmentSettings } from "./segments-page";

// Deleting a segment names the results links it breaks, and is not confirmed before that list has been read.

const SEGMENT_ID = "0192f7a8-1c3e-7b2a-9d4f-00000000000a";

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
  vi.unstubAllGlobals();
});

const segment: Segment = {
  id: SEGMENT_ID,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  title: "Pro users",
  description: null,
  isPrivate: false,
  filters: [],
  workspaceId: "ws",
  surveys: [],
  activeSurveys: [],
  inactiveSurveys: [],
};

/** The segment's results-links endpoint, answering once release is called. */
const stubLinks = () => {
  let release: (response: Response) => void = () => {};
  const answer = new Promise<Response>((resolve) => {
    release = resolve;
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      expect(`${init.method ?? "GET"} ${path}`).toBe(`GET /api/app/segments/${SEGMENT_ID}/result-shares`);
      return answer;
    })
  );
  return (body: unknown, status = 200) =>
    release(new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } }));
};

const openDeleteDialog = () => {
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <SegmentSettings
          workspaceId="ws"
          segment={segment}
          segments={[segment]}
          attributeKeys={[]}
          activity={{ activeSurveys: [], inactiveSurveys: [] }}
          isReadOnly={false}
          close={() => {}}
        />
      </QueryClientProvider>
    </I18nextProvider>
  );
  fireEvent.click(screen.getByRole("button", { name: "Delete" }));
  const dialog = screen.getByRole("dialog");
  return { dialog, confirm: () => within(dialog).getByRole("button", { name: "Delete" }) };
};

test("delete waits for the links the segment's deletion breaks, then names them", async () => {
  const answer = stubLinks();
  const { dialog, confirm } = openDeleteDialog();
  expect((confirm() as HTMLButtonElement).disabled).toBe(true);
  const links: SegmentResultShare[] = [{ id: "l1", name: "Board", surveyId: "s1", surveyName: "CSAT 2026" }];
  answer(links);
  await waitFor(() => expect((confirm() as HTMLButtonElement).disabled).toBe(false));
  expect(within(dialog).getByTestId("segment-result-shares").textContent).toContain("Board (CSAT 2026)");
});

test("links that could not be listed are said to be unchecked, and delete stays possible", async () => {
  const answer = stubLinks();
  const { dialog, confirm } = openDeleteDialog();
  answer({ code: "internal", message: "boom" }, 500);
  await waitFor(() => expect((confirm() as HTMLButtonElement).disabled).toBe(false));
  expect(within(dialog).getByTestId("segment-result-shares-unchecked").textContent).toBe(
    "The results links that filter by this segment couldn’t be checked. Deleting it may stop some of them from showing the responses they were created for."
  );
});
