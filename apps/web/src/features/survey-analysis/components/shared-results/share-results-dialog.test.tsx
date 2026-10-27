import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TResponseFilterCriteria } from "@forma/types/responses";
import type { TSurvey } from "@forma/types/surveys/types";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../../lib/i18n";
import { toast } from "../../../../lib/toast";
import type { ResultShare, ResultShares } from "../../result-shares";
import { ShareResultsDialog, hasCriteria } from "./share-results-dialog";

// "Share results": a new link freezes the summary's current filter and shows only the counts of cards with individual answers unless they are ticked; a link can be revoked.

const SURVEY_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e";

vi.mock("../filter/response-filter-context", () => ({
  useResponseFilter: () => ({ filterCriteria: { finished: true } }),
}));

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
    this.removeAttribute("open");
  };
  // Radix's checkbox measures itself; jsdom lays nothing out.
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
});

const survey = {
  id: SURVEY_ID,
  workspaceId: "ws",
  name: "CSAT 2026",
  type: "link",
  blocks: [
    {
      id: "b1",
      name: "B1",
      elements: [
        {
          id: "q1",
          type: TSurveyElementTypeEnum.OpenText,
          headline: { default: "What could be better?" },
          required: false,
        },
      ],
    },
  ],
  hiddenFields: { enabled: true, fieldIds: ["utm"] },
} as unknown as TSurvey;

const existing: ResultShare = {
  id: "0192f7a8-1c3e-7b2a-9d4f-000000000001",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  surveyId: SURVEY_ID,
  key: "a".repeat(43),
  name: "Board",
  filterCriteria: {},
  revealedElementIds: [],
  expiresAt: null,
  creator: { id: "u1", name: "Ada" },
  missingSegmentIds: [],
};

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

const stubApi = (
  share: ResultShare = existing,
  failCreate?: { status: number; code: string; message: string }
) => {
  const calls: { method: string; path: string; body?: unknown }[] = [];
  let list: ResultShares = {
    shares: [share],
    revealable: [
      { id: "q1", kind: "answers", type: "openText" },
      { id: "utm", kind: "hiddenField" },
    ],
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit) => {
      const method = init.method ?? "GET";
      const body: unknown = init.body ? JSON.parse(init.body as string) : undefined;
      calls.push({ method, path, body });
      const base = `/api/app/surveys/${SURVEY_ID}/result-shares`;
      if (path === base && method === "GET") return json(list);
      if (path === base && method === "POST" && failCreate) {
        return json({ code: failCreate.code, message: failCreate.message }, failCreate.status);
      }
      if (path === base && method === "POST") {
        const created = { ...existing, id: "new", key: "b".repeat(43), ...(body as object) };
        list = { ...list, shares: [created as ResultShare, ...list.shares] };
        return json(created, 201);
      }
      if (path === `${base}/${existing.id}` && method === "PUT") {
        const updated = { ...share, ...(body as object) } as ResultShare;
        list = { ...list, shares: [updated] };
        return json(updated);
      }
      if (path === `${base}/${existing.id}` && method === "DELETE") {
        list = { ...list, shares: list.shares.filter((share) => share.id !== existing.id) };
        return new Response(null, { status: 204 });
      }
      throw new Error(`unexpected ${method} ${path}`);
    })
  );
  return calls;
};

const renderDialog = () =>
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ShareResultsDialog survey={survey} open setOpen={() => {}} />
      </QueryClientProvider>
    </I18nextProvider>
  );

describe("ShareResultsDialog", () => {
  test("lists the links with their addresses", async () => {
    stubApi();
    renderDialog();
    const row = await screen.findByTestId("result-share");
    expect(within(row).getByText("Board")).toBeTruthy();
    const address = within(row).getByRole("textbox", { name: "Address of Board" }) as HTMLInputElement;
    expect(address.value).toBe(`${window.location.origin}/app/results?token=${"a".repeat(43)}`);
    expect(row.textContent).toContain("All responses");
    expect(row.textContent).toContain("Counts only");
    expect(row.textContent).toContain("Created by Ada");
  });

  test("a new link freezes the current filter and reveals only the cards ticked", async () => {
    const calls = stubApi();
    renderDialog();
    await screen.findByTestId("result-share");
    expect(
      screen.getByText(
        "The link shows the responses matching the summary's current filter. The filter cannot be changed later."
      )
    ).toBeTruthy();
    // Nothing is revealed until it is ticked.
    const answers = screen.getByRole("checkbox", { name: "What could be better?" });
    const hidden = screen.getByRole("checkbox", { name: "utm (hidden field)" });
    expect(answers.getAttribute("aria-checked")).toBe("false");
    expect(hidden.getAttribute("aria-checked")).toBe("false");
    fireEvent.click(answers);
    fireEvent.change(screen.getByLabelText("Name (optional)"), { target: { value: " Investors " } });
    fireEvent.click(screen.getByRole("button", { name: "Create link" }));
    await waitFor(() => expect(calls.some((call) => call.method === "POST")).toBe(true));
    expect(calls.find((call) => call.method === "POST")?.body).toEqual({
      name: "Investors",
      expiresAt: null,
      revealedElementIds: ["q1"],
      filterCriteria: { finished: true },
    });
    await waitFor(() => expect(screen.getAllByTestId("result-share")).toHaveLength(2));
  });

  test("editing a link keeps its expiry, even a past one, and drops cards the survey no longer offers", async () => {
    // The link expired, and reveals q1 and a card since removed from the survey.
    const expiresAt = "2020-01-01T10:00:00.123456Z";
    const calls = stubApi({ ...existing, expiresAt, revealedElementIds: ["q1", "removed_q"] });
    renderDialog();
    await screen.findByTestId("result-share");
    fireEvent.click(screen.getByRole("button", { name: "Edit Board" }));
    expect(screen.getByRole("checkbox", { name: "What could be better?" }).getAttribute("aria-checked")).toBe(
      "true"
    );
    fireEvent.change(screen.getByLabelText("Name (optional)"), { target: { value: "Board renamed" } });
    const save = screen.getByRole("button", { name: "Save" });
    expect(save.hasAttribute("disabled")).toBe(false);
    fireEvent.click(save);
    await waitFor(() => expect(calls.some((call) => call.method === "PUT")).toBe(true));
    expect(calls.find((call) => call.method === "PUT")?.body).toEqual({
      name: "Board renamed",
      expiresAt,
      revealedElementIds: ["q1"],
    });
  });

  test("changing an edited link's expiry replaces it", async () => {
    const calls = stubApi({ ...existing, expiresAt: "2020-01-01T10:00:00.123456Z" });
    renderDialog();
    await screen.findByTestId("result-share");
    fireEvent.click(screen.getByRole("button", { name: "Edit Board" }));
    fireEvent.change(screen.getByLabelText("Expiration"), { target: { value: "never" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(calls.some((call) => call.method === "PUT")).toBe(true));
    expect(calls.find((call) => call.method === "PUT")?.body).toEqual({
      name: "Board",
      expiresAt: null,
      revealedElementIds: [],
    });
  });

  test("a link whose segment was deleted is marked", async () => {
    stubApi({ ...existing, missingSegmentIds: ["0192f7a8-1c3e-7b2a-9d4f-00000000000a"] });
    renderDialog();
    const row = await screen.findByTestId("result-share");
    expect(within(row).getByText("Segment deleted")).toBeTruthy();
    expect(row.textContent).toContain("filters by a segment that was deleted");
  });

  test("a link from an older server, without missingSegmentIds, is listed unmarked", async () => {
    // During a rolling deploy the new bundle can be answered by a pod that predates the field.
    const { missingSegmentIds: _absent, ...older } = existing;
    stubApi(older as ResultShare);
    renderDialog();
    const row = await screen.findByTestId("result-share");
    expect(within(row).getByText("Board")).toBeTruthy();
    expect(within(row).queryByText("Segment deleted")).toBeNull();
  });

  test("a new link whose filter names a deleted segment is refused in the user's words", async () => {
    stubApi(existing, {
      status: 400,
      code: "result_share_segment_not_found",
      message: "The filter uses a segment that does not exist in this workspace: x.",
    });
    renderDialog();
    await screen.findByTestId("result-share");
    fireEvent.click(screen.getByRole("button", { name: "Create link" }));
    await waitFor(() =>
      expect(toast.getSnapshot().map((shown) => shown.message)).toContain(
        "The filter uses a segment that no longer exists, or one that uses a deleted segment. Remove it from the filter and try again."
      )
    );
    expect(screen.getAllByTestId("result-share")).toHaveLength(1);
  });

  test("revoking asks first, then deletes the link", async () => {
    const calls = stubApi();
    renderDialog();
    await screen.findByTestId("result-share");
    fireEvent.click(screen.getByRole("button", { name: "Revoke Board" }));
    fireEvent.click(await screen.findByRole("button", { name: "Revoke link" }));
    await waitFor(() => expect(calls.some((call) => call.method === "DELETE")).toBe(true));
    expect(await screen.findByText("No results links yet.")).toBeTruthy();
  });
});

describe("hasCriteria", () => {
  test("is set only by a criterion with a value", () => {
    expect(hasCriteria({})).toBe(false);
    expect(hasCriteria({ finished: undefined })).toBe(false);
    // The server stores criteria as the Go type marshals them, with null for every member not set.
    expect(hasCriteria({ finished: null } as unknown as TResponseFilterCriteria)).toBe(false);
    expect(hasCriteria({ finished: true })).toBe(true);
  });
});
