import type { TBaseFilters } from "@forma/types/segment";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, render, screen } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeEach, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { PREVIEW_COUNT_DEBOUNCE_MS, SegmentPreviewCount } from "./segment-preview-count";

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const phone = (id: string): TBaseFilters => [
  {
    id: `node${id}`,
    connector: null,
    resource: {
      id,
      root: { type: "device", deviceType: "phone" },
      qualifier: { operator: "equals" },
      value: "phone",
    },
  },
];

const plan = (value: string): TBaseFilters => [
  {
    id: "nodeplan",
    connector: null,
    resource: {
      id: "plan",
      root: { type: "attribute", contactAttributeKey: "plan" },
      qualifier: { operator: "equals" },
      value,
    },
  },
];

/** The server's preview-count answer, recording the trees it was asked about. */
const stubCount = (answer: { count: number; total: number; approximate: boolean }) => {
  const asked: TBaseFilters[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      expect(`${init.method} ${path}`).toBe("POST /api/app/workspaces/ws/segments/preview-count");
      asked.push((JSON.parse(init.body as string) as { filters: TBaseFilters }).filters);
      return new Response(JSON.stringify(answer), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    })
  );
  return asked;
};

const renderCount = (i18n: I18n, filters: TBaseFilters) => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = (next: TBaseFilters) => (
    <QueryClientProvider client={client}>
      <I18nextProvider i18n={i18n}>
        <SegmentPreviewCount workspaceId="ws" filters={next} />
      </I18nextProvider>
    </QueryClientProvider>
  );
  const { rerender } = render(view(filters));
  return { rerender: (next: TBaseFilters) => rerender(view(next)) };
};

const settle = () => act(() => vi.advanceTimersByTimeAsync(PREVIEW_COUNT_DEBOUNCE_MS));

test("counts the tree it opens with at once, then an edited tree once it stops changing", async () => {
  const i18n = await initI18n("en-US");
  const asked = stubCount({ count: 1234, total: 5000, approximate: false });
  const { rerender } = renderCount(i18n, phone("a"));
  expect(await screen.findByText("Matches 1,234 of 5,000 contacts")).toBeTruthy();
  expect(asked).toEqual([phone("a")]);
  rerender(plan("p"));
  rerender(plan("pr"));
  rerender(plan("pro"));
  expect(asked).toEqual([phone("a")]);
  await settle();
  await vi.waitFor(() => expect(asked).toEqual([phone("a"), plan("pro")]));
  expect(screen.queryByText(i18n.t("workspace.segments.preview_count_approximate"))).toBeNull();
});

test("a tree the schema refuses is not counted", async () => {
  const i18n = await initI18n("en-US");
  const asked = stubCount({ count: 1, total: 1, approximate: false });
  renderCount(i18n, plan(""));
  await settle();
  expect(asked).toEqual([]);
  expect(screen.queryByRole("status")).toBeNull();
});

test("an approximate count says so, and no match is a warning", async () => {
  const i18n = await initI18n("en-US");
  stubCount({ count: 0, total: 1, approximate: true });
  renderCount(i18n, phone("a"));
  await settle();
  const status = await screen.findByRole("status");
  expect(status.textContent).toContain("Matches 0 of 1 contact");
  expect(status.textContent).toContain(i18n.t("workspace.segments.preview_count_approximate"));
  expect(status.className).toContain("amber");
});

test("the count is translated with the locale's number format", async () => {
  const i18n = await initI18n("ja-JP");
  stubCount({ count: 12, total: 3400, approximate: false });
  renderCount(i18n, phone("a"));
  await settle();
  expect(await screen.findByText("3,400 件中 12 件の連絡先が一致")).toBeTruthy();
});

test("a failed count says it could not count", async () => {
  const i18n = await initI18n("en-US");
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(JSON.stringify({ code: "internal_server_error", message: "x" }), { status: 500 })
    )
  );
  renderCount(i18n, phone("a"));
  await settle();
  expect(await screen.findByText(i18n.t("workspace.segments.preview_count_failed"))).toBeTruthy();
});
