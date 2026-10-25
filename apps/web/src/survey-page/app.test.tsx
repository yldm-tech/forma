/**
 * @vitest-environment jsdom
 */
import type { SurveyContainerProps } from "@forma/types/forma-surveys";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { SurveyPageApp } from "./app";
import { initSurveyPageI18n } from "./lib/i18n";
import {
  SURVEY_PAGE_STATE_ID,
  type TPublicSurvey,
  type TSurveyPageState,
  type TSurveyProps,
  readSurveyPageState,
} from "./lib/state";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initSurveyPageI18n("en-US");
});

const renderPage = (state: TSurveyPageState) =>
  render(
    <I18nextProvider i18n={i18n}>
      <SurveyPageApp state={state} />
    </I18nextProvider>
  );

const renderSurvey = vi.fn<(props: SurveyContainerProps) => void>();

beforeEach(() => {
  renderSurvey.mockReset();
  (globalThis as unknown as { formaSurveys: unknown }).formaSurveys = { renderSurvey };
  globalThis.history.replaceState({}, "", "/s/survey-1");
  document.head.innerHTML = "";
  document.title = "Feedback | Forma";
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const publicSurvey = (overrides: Partial<TPublicSurvey> = {}): TPublicSurvey =>
  ({
    id: "survey-1",
    name: "Feedback",
    type: "link",
    status: "inProgress",
    workspaceId: "ws-1",
    welcomeCard: { enabled: false, timeToFinish: false, showResponseCount: false },
    blocks: [
      {
        id: "block-1",
        name: "Block 1",
        elements: [
          {
            id: "feedback",
            type: "openText",
            headline: { default: "What do you think?" },
            required: false,
            inputType: "text",
            charLimit: { enabled: false },
          },
        ],
      },
    ],
    endings: [],
    hiddenFields: { enabled: true, fieldIds: ["campaign"] },
    embeddedFields: [
      {
        field: {
          name: "campaign",
          source: "ingested",
          dataType: "string",
          defaultValue: null,
          locked: false,
        },
        link: { storageKey: "campaign" },
      },
    ],
    variables: [],
    languages: [],
    styling: null,
    segment: null,
    isVerifyEmailEnabled: false,
    customHeadScripts: null,
    customHeadScriptsMode: "add",
    ...overrides,
  }) as unknown as TPublicSurvey;

const surveyProps = (overrides: Partial<TSurveyProps> = {}): TSurveyProps => ({
  survey: publicSurvey(),
  surveyId: "survey-1",
  workspace: {
    styling: { allowStyleOverwrite: true },
    logo: null,
    linkSurveyBranding: true,
    customHeadScripts: null,
  },
  styling: { allowStyleOverwrite: true },
  publicDomain: "https://surveys.example.com",
  responseCount: null,
  languageCode: "default",
  locale: "en-US",
  isEmbed: false,
  isPreview: false,
  canReadUserIdFromUrl: false,
  isSpamProtectionEnabled: false,
  isFormaCloud: false,
  ...overrides,
});

const lastRender = (): SurveyContainerProps => {
  const call = renderSurvey.mock.calls.at(-1);
  if (!call) throw new Error("the survey was never rendered");
  return call[0];
};

describe("readSurveyPageState", () => {
  test("reads the state the server embedded", () => {
    const doc = document.implementation.createHTMLDocument();
    doc.body.innerHTML = `<script id="${SURVEY_PAGE_STATE_ID}" type="application/json">{"view":"inactive","locale":"ja-JP","inactive":{"status":"paused","isScheduled":false,"surveyClosedMessage":null,"workspace":null}}</script>`;
    expect(readSurveyPageState(doc)).toEqual({
      view: "inactive",
      locale: "ja-JP",
      inactive: { status: "paused", isScheduled: false, surveyClosedMessage: null, workspace: null },
    });
  });

  test("a page without state is the not-found screen", () => {
    expect(readSurveyPageState(document.implementation.createHTMLDocument())).toEqual({
      view: "notFound",
      locale: "en-US",
    });
  });
});

describe("SurveyPageApp", () => {
  test("not found, in the visitor's language", async () => {
    renderPage({ view: "notFound", locale: "en-US" });
    expect(screen.getByRole("main")).toBeTruthy();
    expect(screen.getByRole("heading", { level: 1, name: "This survey isn't available" })).toBeTruthy();
    expect(
      screen.getByText("The link may be wrong, or the survey isn't accepting responses yet.")
    ).toBeTruthy();
    cleanup();
    await i18n.changeLanguage("ja-JP");
    try {
      renderPage({ view: "notFound", locale: "ja-JP" });
      expect(screen.getByRole("heading", { level: 1, name: "このアンケートは利用できません" })).toBeTruthy();
    } finally {
      await i18n.changeLanguage("en-US");
    }
  });

  // forma2's SurveyInactive: headings, descriptions, the create-your-own CTA and the footer logo.
  test.each([
    {
      inactive: {
        status: "paused",
        isScheduled: false,
        surveyClosedMessage: null,
        workspace: { linkSurveyBranding: true },
      },
      heading: "Survey Paused",
      description: "This survey is temporarily paused.",
      cta: true,
      logo: true,
    },
    {
      inactive: {
        status: "paused",
        isScheduled: true,
        surveyClosedMessage: null,
        workspace: { linkSurveyBranding: false },
      },
      heading: "Survey scheduled.",
      description: "This survey is scheduled to go live soon.",
      cta: false,
      logo: false,
    },
    {
      inactive: { status: "completed", isScheduled: false, surveyClosedMessage: null, workspace: null },
      heading: "Survey Completed",
      description: "This survey is closed.",
      cta: true,
      logo: true,
    },
    {
      inactive: {
        status: "completed",
        isScheduled: false,
        surveyClosedMessage: { heading: "Thanks!", subheading: "We have enough answers." },
        workspace: { linkSurveyBranding: true },
      },
      heading: "Thanks!",
      description: "We have enough answers.",
      cta: false,
      logo: true,
    },
    {
      inactive: { status: "link invalid", isScheduled: false, surveyClosedMessage: null, workspace: null },
      heading: "s.this_looks_fishy",
      description: "This survey can only be taken by invitation.",
      cta: false,
      logo: true,
    },
    {
      inactive: { status: "link expired", isScheduled: false, surveyClosedMessage: null, workspace: null },
      heading: "Your link is expired.",
      description: "The link you used is no longer valid.",
      cta: false,
      logo: true,
    },
    {
      inactive: {
        status: "response submitted",
        isScheduled: false,
        surveyClosedMessage: null,
        workspace: { linkSurveyBranding: false },
      },
      heading: "The survey has already been answered.",
      description: "A response linked to this survey and contact already exists",
      cta: false,
      logo: false,
    },
  ] as const)(
    "inactive: $inactive.status, scheduled $inactive.isScheduled",
    ({ inactive, heading, description, cta, logo }) => {
      renderPage({ view: "inactive", locale: "en-US", inactive });
      // A heading given as a key is read from the catalogue, so a reworded value does not break this test.
      const name = heading.startsWith("s.") ? i18n.t(heading) : heading;
      expect(screen.getByRole("heading", { name })).toBeTruthy();
      expect(screen.getByText(description)).toBeTruthy();
      expect(Boolean(screen.queryByRole("link", { name: "Create your own open-source survey" }))).toBe(cta);
      // Forma's wordmark, not the upstream logo, linked with the accessible name Forma.
      expect(Boolean(screen.queryByRole("link", { name: "Forma" }))).toBe(logo);
    }
  );

  test("completed single-use link, with and without its own message", () => {
    renderPage({
      view: "completed",
      locale: "en-US",
      completed: {
        singleUse: { enabled: true, isEncrypted: false, heading: "Done", subheading: "Once only" },
        workspace: { linkSurveyBranding: false },
      },
    });
    expect(screen.getByRole("heading", { name: "Done" })).toBeTruthy();
    expect(screen.getByText("Once only")).toBeTruthy();
    expect(screen.queryByRole("link", { name: "Forma" })).toBeNull();
    cleanup();
    renderPage({ view: "completed", locale: "en-US", completed: { singleUse: null, workspace: null } });
    expect(screen.getByRole("heading", { name: "The survey has already been answered." })).toBeTruthy();
    expect(screen.getByText("You can only use this link once.")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Forma" })).toBeTruthy();
  });

  test("the error screen", () => {
    renderPage({ view: "error", locale: "en-US" });
    expect(screen.getByRole("heading", { name: "Something went wrong. Please try again." })).toBeTruthy();
  });
});

describe("the survey", () => {
  test("renders with the runtime, inline in forma2's container, with what the server decided", async () => {
    renderPage({
      view: "survey",
      locale: "en-US",
      survey: surveyProps({
        isPreview: true,
        isEmbed: true,
        singleUseId: "su-1",
        singleUseResponseId: "resp-1",
        contactId: "contact-1",
        responseCount: 12,
        recaptchaSiteKey: "site-key",
        isSpamProtectionEnabled: false,
      }),
    });
    await waitFor(() => expect(renderSurvey).toHaveBeenCalled());
    const props = lastRender();
    expect(props).toMatchObject({
      containerId: "forma-survey-container",
      mode: "inline",
      appUrl: "https://surveys.example.com",
      workspaceId: "ws-1",
      isPreviewMode: true,
      fullSizeCards: true,
      languageCode: "default",
      singleUseId: "su-1",
      singleUseResponseId: "resp-1",
      contactId: "contact-1",
      responseCount: 12,
      recaptchaSiteKey: "site-key",
      isSpamProtectionEnabled: false,
      isBrandingEnabled: true,
      shouldResetQuestionId: false,
      skipPrefilled: false,
      offlineSupport: false,
    });
    expect(props.survey.id).toBe("survey-1");
    expect(typeof props.getRecaptchaToken).toBe("function");
    expect(document.getElementById("forma-survey-container")).toBeTruthy();
  });

  // An embedded survey fills its frame; on its own page a card may use the viewport rather than the runtime's 60dvh widget default, which stays the floor on a short (landscape) viewport.
  test("caps a card by the viewport on its own page, not when embedded", async () => {
    renderPage({ view: "survey", locale: "en-US", survey: surveyProps({ isEmbed: false }) });
    await waitFor(() => expect(renderSurvey).toHaveBeenCalled());
    expect(lastRender().cardMaxHeight).toBe("max(60dvh, 100dvh - 12rem)");
    expect(lastRender().fullSizeCards).toBe(false);
    cleanup();
    renderSurvey.mockReset();
    renderPage({ view: "survey", locale: "en-US", survey: surveyProps({ isEmbed: true }) });
    await waitFor(() => expect(renderSurvey).toHaveBeenCalled());
    expect(lastRender().cardMaxHeight).toBeUndefined();
  });

  test("reads prefill, hidden fields, the start position and the user id from the URL", async () => {
    globalThis.history.replaceState(
      {},
      "",
      "/s/survey-1?feedback=great&CAMPAIGN=spring&userId=u1&startAt=feedback&skipPrefilled=true&verify=x"
    );
    renderPage({
      view: "survey",
      locale: "en-US",
      survey: surveyProps({
        canReadUserIdFromUrl: true,
        verifiedEmail: "user1@example.com",
        survey: publicSurvey({ isVerifyEmailEnabled: true }),
      }),
    });
    await waitFor(() => expect(renderSurvey).toHaveBeenCalled());
    expect(lastRender()).toMatchObject({
      prefillResponseData: { feedback: "great" },
      hiddenFieldsRecord: { campaign: "spring", verifiedEmail: "user1@example.com" },
      userId: "u1",
      startAtQuestionId: "feedback",
      skipPrefilled: true,
    });
  });

  test("ignores the user id unless the server allows it, and drops an invalid start position from the URL", async () => {
    globalThis.history.replaceState({}, "", "/s/survey-1?userId=u1&startAt=nowhere");
    renderPage({ view: "survey", locale: "en-US", survey: surveyProps() });
    await waitFor(() => expect(renderSurvey).toHaveBeenCalled());
    expect(lastRender().userId).toBeUndefined();
    expect(lastRender().startAtQuestionId).toBeUndefined();
    expect(globalThis.location.search).toBe("?userId=u1");
  });

  test("injects the custom head scripts, survey scripts replacing the workspace's in replace mode", async () => {
    renderPage({
      view: "survey",
      locale: "en-US",
      survey: surveyProps({
        workspace: {
          styling: { allowStyleOverwrite: true },
          logo: null,
          linkSurveyBranding: true,
          customHeadScripts: '<script data-from="workspace">1</script>',
        },
        survey: publicSurvey({
          customHeadScripts: '<script data-from="survey">2</script><meta name="custom" content="1">',
          customHeadScriptsMode: "replace",
        }),
      }),
    });
    await waitFor(() => expect(document.head.querySelector('script[data-from="survey"]')).toBeTruthy());
    expect(document.head.querySelector('script[data-from="workspace"]')).toBeNull();
    expect(document.head.querySelector('meta[name="custom"]')).toBeTruthy();
  });

  test.each([
    { name: "preview", props: { isPreview: true } },
    { name: "Forma Cloud", props: { isFormaCloud: true } },
  ])("injects no custom head scripts on $name", async ({ props }) => {
    renderPage({
      view: "survey",
      locale: "en-US",
      survey: surveyProps({
        ...props,
        workspace: {
          styling: { allowStyleOverwrite: true },
          logo: null,
          linkSurveyBranding: true,
          customHeadScripts: '<script data-from="workspace">1</script>',
        },
      }),
    });
    await waitFor(() => expect(renderSurvey).toHaveBeenCalled());
    expect(document.head.querySelector('script[data-from="workspace"]')).toBeNull();
  });

  // The page re-renders on every card (its title follows the card). Rendering the survey again on each of those handed the runtime fresh callbacks, which replaced its response queue and lost the answers waiting in the old one (R-01).
  test("does not render the survey again when the page re-renders for a new card", async () => {
    globalThis.history.replaceState({}, "", "/s/survey-1?CAMPAIGN=spring");
    renderPage({ view: "survey", locale: "en-US", survey: surveyProps() });
    await waitFor(() => expect(renderSurvey).toHaveBeenCalled());
    const renders = renderSurvey.mock.calls.length;

    act(() => lastRender().onPageChange?.({ index: 1, total: 3, label: "Question 1 of 3" }));
    act(() => lastRender().onPageChange?.({ index: 2, total: 3, label: "Question 2 of 3" }));
    expect(document.title).toBe("Feedback | Forma — Question 2 of 3");

    expect(renderSurvey).toHaveBeenCalledTimes(renders);
  });

  test("titles the document with the card the respondent is on, and follows the survey's language", async () => {
    renderPage({
      view: "survey",
      locale: "en-US",
      survey: surveyProps({
        survey: publicSurvey({
          languages: [
            {
              default: true,
              enabled: true,
              language: {
                id: "l1",
                code: "en",
                alias: null,
                workspaceId: "ws-1",
                createdAt: new Date(),
                updatedAt: new Date(),
              },
            },
            {
              default: false,
              enabled: true,
              language: {
                id: "l2",
                code: "ar",
                alias: null,
                workspaceId: "ws-1",
                createdAt: new Date(),
                updatedAt: new Date(),
              },
            },
          ],
        }),
      }),
    });
    await waitFor(() => expect(renderSurvey).toHaveBeenCalled());
    expect(document.documentElement.getAttribute("lang")).toBe("en");
    act(() => lastRender().onPageChange?.({ index: 0, total: 2, label: "Question 1 of 2" }));
    expect(document.title).toBe("Feedback | Forma — Question 1 of 2");
    act(() => lastRender().onLanguageChange?.("ar"));
    await waitFor(() => expect(document.documentElement.getAttribute("dir")).toBe("rtl"));
    expect(document.documentElement.getAttribute("lang")).toBe("ar");
  });
});

describe("the PIN gate", () => {
  const typePin = (pin: string) => {
    const boxes = screen.getAllByRole("textbox");
    fireEvent.change(boxes[0] as HTMLInputElement, { target: { value: pin } });
  };

  test("asks for the PIN, and renders the survey the server returns with its token", async () => {
    const fetchMock = vi.fn(async () =>
      Response.json({ survey: publicSurvey(), pinAuthToken: "pin-token" }, { status: 200 })
    );
    vi.stubGlobal("fetch", fetchMock);
    renderPage({ view: "pin", locale: "en-US", survey: surveyProps({ survey: null }) });
    expect(
      await screen.findByRole("heading", { level: 1, name: "This survey is protected. Enter the PIN below." })
    ).toBeTruthy();
    expect(screen.getByRole("main")).toBeTruthy();
    // The gate exists only to take the PIN, so typing works at once.
    await waitFor(() => expect(document.activeElement).toBe(screen.getAllByRole("textbox")[0]));
    expect(renderSurvey).not.toHaveBeenCalled();

    typePin("1234");
    await waitFor(() => expect(renderSurvey).toHaveBeenCalled());
    expect(fetchMock).toHaveBeenCalledWith(
      "/s/survey-1/pin",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ pin: "1234" }) })
    );
    expect(lastRender().pinAuthToken).toBe("pin-token");
    expect(lastRender().survey.id).toBe("survey-1");
  });

  test("a refused PIN turns the boxes red, then clears them", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json({ code: "INVALID_PIN", message: "INVALID_PIN" }, { status: 400 }))
    );
    renderPage({ view: "pin", locale: "en-US", survey: surveyProps({ survey: null }) });
    await screen.findByText("This survey is protected. Enter the PIN below.");
    typePin("0000");
    await waitFor(() => expect(screen.getAllByRole("textbox")[0]?.className).toContain("border-red-400"));
    expect((screen.getAllByRole("textbox")[0] as HTMLInputElement).disabled).toBe(true);
    expect(screen.getByRole("status").textContent).toBe("Incorrect PIN. Try again.");
    await act(async () => {
      vi.advanceTimersByTime(2000);
    });
    // The reset timer is armed by a passive effect, which can run after the advance above when the machine is busy; the clock keeps moving (shouldAdvanceTime), so waiting longer than the 2 s reset covers that case.
    await waitFor(() => expect((screen.getAllByRole("textbox")[0] as HTMLInputElement).value).toBe(""), {
      timeout: 4000,
    });
    expect(screen.getAllByRole("textbox")[0]?.className).not.toContain("border-red-400");
    // The boxes are cleared and enabled again, focus is back in the first one, and the reason stays until the next keystroke.
    await waitFor(() => expect(document.activeElement).toBe(screen.getAllByRole("textbox")[0]));
    expect(screen.getByRole("status").textContent).toBe("Incorrect PIN. Try again.");
    typePin("1");
    expect(screen.getByRole("status").textContent).toBe("");
    expect(renderSurvey).not.toHaveBeenCalled();
    vi.useRealTimers();
  });

  test("a rate-limited PIN says to wait instead of calling it wrong", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json({ code: "too_many_requests", message: "Too many requests" }, { status: 429 })
      )
    );
    renderPage({ view: "pin", locale: "en-US", survey: surveyProps({ survey: null }) });
    await screen.findByText("This survey is protected. Enter the PIN below.");
    typePin("0000");
    await waitFor(() =>
      expect(screen.getByRole("status").textContent).toBe("Too many attempts. Wait a minute and try again.")
    );
  });
});

describe("the email verification gate", () => {
  // The gate is a lazy chunk; loading it once up front keeps the first test's wait about the gate, not the module transform.
  beforeAll(async () => {
    await import("./components/verify-email");
  });

  const verifyState = (fishy: boolean): TSurveyPageState => ({
    view: "verifyEmail",
    locale: "en-US",
    verifyEmail: {
      fishy,
      survey: publicSurvey(),
      languageCode: "de",
      styling: { allowStyleOverwrite: true },
      locale: "en-US",
      singleUseId: "su-1",
      singleUseToken: "tok",
    },
  });

  test("a verification link that does not verify says so in its heading", async () => {
    renderPage(verifyState(true));
    expect(await screen.findByRole("heading", { level: 1, name: i18n.t("s.this_looks_fishy") })).toBeTruthy();
  });

  test("mails the survey link to the lowercased address", async () => {
    const fetchMock = vi.fn(async () => Response.json({ success: true }, { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    renderPage(verifyState(false));
    expect(
      await screen.findByRole("heading", { level: 1, name: "Verify your email to respond" })
    ).toBeTruthy();
    // A visible label names the field; the placeholder is a localized example, not a company address.
    const input = screen.getByLabelText("Email address");
    expect(input.getAttribute("placeholder")).toBe("you@example.com");
    fireEvent.change(input, { target: { value: "Not an email" } });
    fireEvent.click(screen.getByRole("button", { name: "Verify" }));
    // Zod's English message is replaced by the catalogue's, and the field points at it.
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toBe("Please enter a valid email address");
    expect(input.getAttribute("aria-describedby")).toBe(alert.id);
    expect(fetchMock).not.toHaveBeenCalled();

    fireEvent.change(input, { target: { value: "User1@Example.com" } });
    fireEvent.click(screen.getByRole("button", { name: "Verify" }));
    expect(await screen.findByText("Survey sent to User1@Example.com")).toBeTruthy();
    expect(fetchMock).toHaveBeenCalledWith(
      "/s/survey-1/verify-email",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          email: "user1@example.com",
          suId: "su-1",
          suToken: "tok",
          locale: "en-US",
          surveyLanguageCode: "de",
        }),
      })
    );
  });

  test("lists the questions to the just curious, in the survey's language", async () => {
    const state = verifyState(false);
    if (state.view === "verifyEmail") state.verifyEmail.languageCode = "default";
    renderPage(state);
    fireEvent.click(await screen.findByRole("button", { name: /Just curious\?/ }));
    expect(screen.getByText("1. What do you think?")).toBeTruthy();
  });
});
