import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import { meQuery } from "../../lib/queries/auth";
import type { WorkspaceDetail, WorkspaceLanguage } from "../../lib/queries/workspaces";
import { toast } from "../../lib/toast";
import { WorkspaceLanguagesPage } from "./languages";

// The survey languages form: an alias may name its own language (ja for ja-JP), a problem names the row it is on, and saving writes only what changed, so an alias stored under an older rule never blocks the other rows.

const WS = "0190a000-0000-7000-8000-000000000002";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
});

const language = (id: string, code: string, alias: string | null = null): WorkspaceLanguage => ({
  id,
  code,
  alias,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  workspaceId: WS,
});

// Seed's languages: Japanese and Chinese carry the base-language aliases the API accepted.
const seedLanguages = [
  language("l-en", "en-US"),
  language("l-ja", "ja-JP", "ja"),
  language("l-zh", "zh-Hans-CN", "zh"),
];

const workspaceWith = (languages: WorkspaceLanguage[]) =>
  ({
    id: WS,
    organizationId: "0190a000-0000-7000-8000-000000000001",
    languages,
    config: { defaultSurveyLanguage: "en-US" },
    access: { role: "owner", canWrite: true, canManage: true, isReadOnly: false },
  }) as unknown as WorkspaceDetail;

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

/** The language endpoints, answering every write as stored; each request is recorded. */
const stubServer = () => {
  const requests: { method: string; path: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const body = typeof init?.body === "string" ? (JSON.parse(init.body) as unknown) : undefined;
      requests.push({ method, path, body });
      if (path === `/api/app/workspaces/${WS}/languages` && method === "POST") {
        return json(language("l-new", (body as { code: string }).code), 201);
      }
      if (path.startsWith(`/api/app/workspaces/${WS}/languages/`) && method === "PATCH") {
        return json(
          language(path.split("/").at(-1) ?? "", "en-US", (body as { alias: string | null }).alias)
        );
      }
      if (path === `/api/app/workspaces/${WS}` && method === "GET") return json(workspaceWith(seedLanguages));
      throw new Error(`unexpected ${method} ${path}`);
    })
  );
  return requests;
};

const renderPage = (languages: WorkspaceLanguage[]) => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  queryClient.setQueryData(meQuery.queryKey, { locale: "en-US" } as never);
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <WorkspaceLanguagesPage workspace={workspaceWith(languages)} />
      </QueryClientProvider>
    </I18nextProvider>
  );
};

const aliasInputs = () => screen.getAllByRole("textbox", { name: "Alias" });

const toastMessages = () => toast.getSnapshot().map((shown) => shown.message);

test("a language can be added beside stored base-language aliases, and only it is written", async () => {
  const requests = stubServer();
  renderPage(seedLanguages);
  fireEvent.click(screen.getByRole("button", { name: "Edit languages" }));
  fireEvent.click(screen.getByRole("button", { name: "Add language" }));
  const pickers = screen.getAllByRole("button", { name: "Language" });
  fireEvent.click(pickers.at(-1)!);
  fireEvent.change(screen.getByRole("textbox", { name: "Search items" }), { target: { value: "Arabic" } });
  fireEvent.click(screen.getByRole("button", { name: "Arabic" }));
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() => expect(toastMessages()).toContain("Languages updated successfully"));
  expect(requests.filter((request) => request.method !== "GET")).toEqual([
    { method: "POST", path: `/api/app/workspaces/${WS}/languages`, body: { code: "ar-EG", alias: "" } },
  ]);
});

test("an alias naming a language the workspace does not have is refused on its row", async () => {
  const requests = stubServer();
  renderPage(seedLanguages);
  fireEvent.click(screen.getByRole("button", { name: "Edit languages" }));
  fireEvent.change(aliasInputs()[1]!, { target: { value: "fr" } });
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
  expect(toastMessages()).toEqual([
    "The alias “fr” of Japanese is the code of French, which this workspace doesn’t have. Add French as a language, or choose an alias that isn’t a language code.",
  ]);
  expect(requests).toEqual([]);
});

test("an alias naming another row is refused, naming both", async () => {
  stubServer();
  renderPage(seedLanguages);
  fireEvent.click(screen.getByRole("button", { name: "Edit languages" }));
  fireEvent.change(aliasInputs()[0]!, { target: { value: "ja-JP" } });
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
  expect(toastMessages()).toEqual([
    "The alias “ja-JP” of English refers to Japanese, which already has its own row. Choose an alias that isn’t another language’s code.",
  ]);
});

test("changing one alias writes only that row", async () => {
  const requests = stubServer();
  renderPage([...seedLanguages, language("l-legacy", "de-DE", "nl")]);
  fireEvent.click(screen.getByRole("button", { name: "Edit languages" }));
  const row = aliasInputs()[1]!;
  fireEvent.change(row, { target: { value: "japanese" } });
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() => expect(toastMessages()).toContain("Languages updated successfully"));
  expect(requests.filter((request) => request.method !== "GET")).toEqual([
    { method: "PATCH", path: `/api/app/workspaces/${WS}/languages/l-ja`, body: { alias: "japanese" } },
  ]);
  expect(within(document.body).queryByText(/doesn’t have/)).toBeNull();
});
