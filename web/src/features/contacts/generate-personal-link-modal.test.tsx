import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import { toast } from "../../lib/toast";
import { GeneratePersonalLinkModal } from "./generate-personal-link-modal";

const CONTACT = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d10";
const SURVEY = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d12";

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
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  vi.unstubAllGlobals();
});

/** The personal-link route answering every request with status and body. */
const stubLinkRoute = (status: number, body: unknown) => {
  const requests: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_path: string, init: RequestInit = {}) => {
      requests.push(JSON.parse(init.body as string));
      return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
    })
  );
  return requests;
};

const renderModal = (surveys = [{ id: SURVEY, name: "Onboarding" }]) =>
  render(
    <I18nextProvider i18n={i18n}>
      <GeneratePersonalLinkModal open setOpen={() => {}} contactId={CONTACT} publishedLinkSurveys={surveys} />
    </I18nextProvider>
  );

const generate = () => {
  fireEvent.change(screen.getByLabelText(i18n.t("common.select_survey")), { target: { value: SURVEY } });
  fireEvent.click(screen.getByRole("button", { name: i18n.t("common.generate") }));
};

const toasts = () => toast.getSnapshot().map((shown) => shown.message);

test("without a published link survey there is nothing to generate", () => {
  stubLinkRoute(200, {});
  renderModal([]);
  expect(screen.getByText(i18n.t("workspace.contacts.no_published_link_surveys_available"))).toBeTruthy();
  expect((screen.getByLabelText(i18n.t("common.select_survey")) as HTMLSelectElement).disabled).toBe(true);
  expect(
    (screen.getByRole("button", { name: i18n.t("common.generate") }) as HTMLButtonElement).disabled
  ).toBe(true);
});

test("the link is copied, and shown when the clipboard refuses", async () => {
  const requests = stubLinkRoute(200, { surveyUrl: "https://surveys.example.com/c/token" });
  vi.stubGlobal("navigator", {
    ...navigator,
    clipboard: {
      writeText: vi.fn(async () => {
        throw new Error("denied");
      }),
    },
  });
  renderModal();
  generate();
  expect(await screen.findByDisplayValue("https://surveys.example.com/c/token")).toBeTruthy();
  expect(requests).toEqual([{ surveyId: SURVEY }]);
  expect(toasts()).toContain(
    i18n.t("workspace.contacts.personal_link_generated_but_clipboard_failed", {
      url: "https://surveys.example.com/c/token",
    })
  );
});

test("a survey of another workspace is refused in the user's language", async () => {
  stubLinkRoute(400, {
    code: "contact_survey_workspace_mismatch",
    message: "contact_survey_workspace_mismatch",
  });
  renderModal();
  generate();
  await waitFor(() =>
    expect(toasts()).toContain(i18n.t("workspace.contacts.contact_survey_workspace_mismatch"))
  );
  // The dialog stays usable after a failure.
  expect((screen.getByRole("button", { name: i18n.t("common.cancel") }) as HTMLButtonElement).disabled).toBe(
    false
  );
});
