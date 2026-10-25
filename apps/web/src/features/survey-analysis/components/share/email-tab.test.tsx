import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../../lib/i18n";
import { toast } from "../../../../lib/toast";
import { EmailTab, stripPreviewParam } from "./email-tab";

const SURVEY = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d12";

const EMBED =
  '<table bgcolor="#ffffff"><tbody><tr><td><a href="https://s.example.com/s/x?preview=true&amp;q1=yes&amp;skipPrefilled=true" target="_blank">Yes</a>' +
  '<a href="https://s.example.com/s/x?preview=true" target="_blank">Open</a><script>alert(1)</script></td></tr></tbody></table>';

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  vi.unstubAllGlobals();
});

/** The Go server: the instance's auth options, the survey's email embed, and the preview mail answering sent. */
const stubServer = (sent: boolean, smtpConfigured = true) => {
  const previews: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      const json = (value: unknown) =>
        new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
      if (path === "/api/app/auth/options") return json({ smtpConfigured });
      if (path === `/api/app/surveys/${SURVEY}/email-html`) return json({ html: EMBED });
      if (path === `/api/app/surveys/${SURVEY}/email-preview` && init.method === "POST") {
        previews.push(path);
        return json({ sent });
      }
      return new Response(JSON.stringify({ code: "not_found", message: path }), {
        status: 404,
        headers: { "content-type": "application/json" },
      });
    })
  );
  return previews;
};

const renderTab = () =>
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <EmailTab surveyId={SURVEY} email="ada@example.com" />
      </QueryClientProvider>
    </I18nextProvider>
  );

const toasts = () => toast.getSnapshot().map((shown) => shown.message);

test("the preview shows the embed sanitized, addressed to the member", async () => {
  stubServer(true);
  renderTab();
  const frame = (await screen.findByTestId("survey-email-preview-frame")) as HTMLIFrameElement;
  const doc = frame.getAttribute("srcdoc") ?? "";
  expect(doc).toContain('bgcolor="#ffffff"');
  expect(doc).toContain(">Yes</a>");
  expect(doc).not.toContain("<script");
  expect(screen.getByTestId("survey-email-preview-shell").textContent).toContain("ada@example.com");
});

test("the embed code drops the preview mark from the survey's links, and copies as shown", async () => {
  stubServer(true);
  const writeText = vi.fn(async () => {});
  vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
  renderTab();
  await screen.findByTestId("survey-email-preview-frame");
  fireEvent.click(
    screen.getByRole("button", { name: i18n.t("workspace.surveys.share.send_email.embed_code_tab") })
  );
  const code = screen.getByText((_, el) => el?.tagName === "CODE");
  expect(code.textContent).toBe(stripPreviewParam(EMBED));
  expect(code.textContent).toContain('href="https://s.example.com/s/x?q1=yes&amp;skipPrefilled=true"');
  expect(code.textContent).toContain('href="https://s.example.com/s/x"');

  fireEvent.click(
    screen.getByRole("button", { name: i18n.t("workspace.surveys.share.send_email.copy_embed_code") })
  );
  await waitFor(() => expect(writeText).toHaveBeenCalledWith(stripPreviewParam(EMBED)));
  expect(toasts()).toContain(i18n.t("workspace.surveys.share.send_email.embed_code_copied_to_clipboard"));
});

test("the preview is mailed to the member", async () => {
  const previews = stubServer(true);
  renderTab();
  fireEvent.click(
    screen.getByRole("button", { name: i18n.t("workspace.surveys.share.send_email.send_preview_email") })
  );
  await waitFor(() => expect(toasts()).toContain(i18n.t("workspace.surveys.share.send_email.email_sent")));
  expect(previews).toHaveLength(1);
});

test("a preview the server could not send is reported", async () => {
  stubServer(false);
  renderTab();
  fireEvent.click(
    screen.getByRole("button", { name: i18n.t("workspace.surveys.share.send_email.send_preview_email") })
  );
  await waitFor(() => expect(toasts()).toContain(i18n.t("common.something_went_wrong_please_try_again")));
});

test("without email on the instance the preview cannot be sent, and the tab says why", async () => {
  const previews = stubServer(false, false);
  renderTab();
  const button = screen.getByRole("button", {
    name: i18n.t("workspace.surveys.share.send_email.send_preview_email"),
  });
  await waitFor(() => expect(button).toHaveProperty("disabled", true));
  const reason = screen.getByText(i18n.t("workspace.settings.general.test_email_requires_smtp"));
  expect(button.getAttribute("aria-describedby")).toBe(reason.id);
  fireEvent.click(button);
  expect(previews).toHaveLength(0);
  expect(toasts()).not.toContain(i18n.t("common.something_went_wrong_please_try_again"));
});
