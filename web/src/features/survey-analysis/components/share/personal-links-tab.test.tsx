import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../../lib/i18n";
import { toast } from "../../../../lib/toast";
import { PersonalLinksTab, expirationDaysUntil } from "./personal-links-tab";

const WS = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f";
const SURVEY = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d12";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const segment = (id: string, title: string, isPrivate: boolean) => ({
  id,
  title,
  isPrivate,
  createdAt: "",
  updatedAt: "",
  description: null,
  filters: [],
  workspaceId: WS,
  surveys: [],
  activeSurveys: [],
  inactiveSurveys: [],
});

/** The Go server: the workspace's segments, and the CSV route answering with status and body. */
const stubServer = (status: number, body: unknown) => {
  const requests: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      const json = (value: unknown, code = 200) =>
        new Response(JSON.stringify(value), {
          status: code,
          headers: { "content-type": "application/json" },
        });
      if (path === `/api/app/workspaces/${WS}/segments`) {
        return json([
          segment("seg-public", "Power users", false),
          segment("seg-private", "Survey targeting", true),
        ]);
      }
      if (path === `/api/app/surveys/${SURVEY}/personal-links`) {
        requests.push(JSON.parse(init.body as string));
        return json(body, status);
      }
      return json({ code: "not_found", message: path }, 404);
    })
  );
  return requests;
};

const renderTab = () =>
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <PersonalLinksTab surveyId={SURVEY} workspaceId={WS} />
      </QueryClientProvider>
    </I18nextProvider>
  );

const generate = async () => {
  const select = screen.getByLabelText(i18n.t("common.recipients"));
  await waitFor(() => expect(within(select).getByRole("option", { name: "Power users" })).toBeTruthy());
  fireEvent.change(select, { target: { value: "seg-public" } });
  fireEvent.click(
    screen.getByRole("button", {
      name: i18n.t("workspace.surveys.share.personal_links.generate_and_download_links"),
    })
  );
};

const toasts = () => toast.getSnapshot().map((shown) => shown.message);

test("only public segments are offered", async () => {
  stubServer(200, {});
  renderTab();
  const select = screen.getByLabelText(i18n.t("common.recipients"));
  await waitFor(() => expect(within(select).getByRole("option", { name: "Power users" })).toBeTruthy());
  expect(within(select).queryByRole("option", { name: "Survey targeting" })).toBeNull();
});

test("the segment's links download as the CSV the server wrote", async () => {
  const csv = `"Forma Contact ID","User ID","First Name","Last Name","Email","Personal Link"\n"c1","","","","a@example.com","https://s/c/t"`;
  const requests = stubServer(200, { fileName: "personal-links-s-1.csv", csvContent: csv });
  const saved: { name: string; content: Promise<string> }[] = [];
  vi.stubGlobal("URL", {
    ...URL,
    createObjectURL: (file: File) => {
      saved.push({ name: file.name, content: file.text() });
      return "blob:links";
    },
    revokeObjectURL: () => {},
  });
  const clicked: string[] = [];
  vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function click(this: HTMLAnchorElement) {
    clicked.push(this.download);
  });
  renderTab();
  await generate();

  await waitFor(() =>
    expect(toasts()).toContain(i18n.t("workspace.surveys.share.personal_links.links_generated_success_toast"))
  );
  expect(requests).toEqual([{ segmentId: "seg-public" }]);
  expect(clicked).toEqual(["personal-links-s-1.csv"]);
  expect(saved.map((file) => file.name)).toEqual(["personal-links-s-1.csv"]);
  expect(await saved[0]?.content).toBe(csv);
});

test("a segment without contacts is reported in the user's language", async () => {
  stubServer(400, { code: "no_contacts_in_segment", message: "no_contacts_in_segment" });
  renderTab();
  await generate();
  await waitFor(() =>
    expect(toasts()).toContain(i18n.t("workspace.surveys.share.personal_links.no_contacts_in_segment"))
  );
});

test("the expiry counts the days to the chosen day, a started day as a whole one", () => {
  const now = new Date(2026, 9, 23, 10, 0);
  expect(expirationDaysUntil(new Date(2026, 9, 24), now)).toBe(1);
  expect(expirationDaysUntil(new Date(2026, 9, 26), now)).toBe(3);
  // Never less than a day.
  expect(expirationDaysUntil(new Date(2026, 9, 23, 10, 0), now)).toBe(1);
});
