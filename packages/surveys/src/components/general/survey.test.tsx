import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/preact";
import { afterEach, beforeEach, describe, expect, test, vi } from "vite-plus/test";
import type { TJsWorkspaceStateSurvey } from "@forma/types/js";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/constants";
import { Survey } from "./survey";

const apiClientMocks = vi.hoisted(() => ({
  createDisplay: vi.fn(),
  createResponse: vi.fn(),
  updateResponse: vi.fn(),
  getResponseIdByDisplayId: vi.fn(),
}));

const offlineStorageMocks = vi.hoisted(() => ({
  addPendingResponse: vi.fn(),
  countPendingResponses: vi.fn(),
  getPendingResponses: vi.fn(),
  removePendingResponse: vi.fn(),
  clearSurveyProgress: vi.fn(),
  getSurveyProgress: vi.fn(),
  patchSurveyProgressSnapshot: vi.fn(),
  saveSurveyProgress: vi.fn(),
}));

vi.mock("@/lib/api-client", () => ({
  ApiClient: vi.fn(function ApiClient() {
    return apiClientMocks;
  }),
}));

vi.mock("@/lib/offline-storage", () => ({
  addPendingResponse: offlineStorageMocks.addPendingResponse,
  countPendingResponses: offlineStorageMocks.countPendingResponses,
  countPendingResponsesStrict: offlineStorageMocks.countPendingResponses,
  getPendingResponses: offlineStorageMocks.getPendingResponses,
  getPendingResponsesStrict: offlineStorageMocks.getPendingResponses,
  removePendingResponse: offlineStorageMocks.removePendingResponse,
  removePendingResponseStrict: offlineStorageMocks.removePendingResponse,
  clearSurveyProgress: offlineStorageMocks.clearSurveyProgress,
  getSurveyProgress: offlineStorageMocks.getSurveyProgress,
  patchSurveyProgressSnapshot: offlineStorageMocks.patchSurveyProgressSnapshot,
  saveSurveyProgress: offlineStorageMocks.saveSurveyProgress,
}));

vi.mock("@/lib/use-online-status", () => ({
  useOnlineStatus: () => true,
}));

vi.mock("@/lib/recall", () => ({
  parseRecallInformation: (element: unknown) => element,
}));

vi.mock("@/components/general/block-conditional", () => ({
  BlockConditional: ({ block, onSubmit }: any) => (
    <button
      data-testid={`submit-${block.id}`}
      onClick={() =>
        onSubmit(
          { [block.elements[0].id]: `${block.id}-answer` },
          {
            [block.elements[0].id]: 123,
          }
        )
      }>
      Submit {block.id}
    </button>
  ),
}));

vi.mock("@/components/wrappers/stacked-cards-container", () => ({
  StackedCardsContainer: ({ currentBlockId, getCardContent, survey }: any) => {
    const blockIndex =
      currentBlockId === "start" ? -1 : survey.blocks.findIndex((block: any) => block.id === currentBlockId);
    return <div data-testid="survey-root">{getCardContent(blockIndex, 0)}</div>;
  },
}));

vi.mock("@/components/wrappers/auto-close-wrapper", () => ({
  AutoCloseWrapper: ({ children }: any) => <>{children}</>,
}));

vi.mock("@/components/general/ending-card", () => ({
  EndingCard: () => <div data-testid="ending-card">Ending</div>,
}));

vi.mock("@/components/general/welcome-card", () => ({
  WelcomeCard: () => <div data-testid="welcome-card">Welcome</div>,
}));

vi.mock("@/components/general/error-component", () => ({
  ErrorComponent: () => <div>Error</div>,
}));

vi.mock("@/components/general/response-error-component", () => ({
  ResponseErrorComponent: ({ onRetry }: any) => (
    <div>
      Response Error
      <button onClick={() => onRetry?.()}>Retry</button>
    </div>
  ),
  ResponseSendingDelayedNotice: () => <div>Sending delayed</div>,
}));

vi.mock("@/components/general/forma-branding", () => ({
  FormaBranding: () => null,
}));

vi.mock("@/components/general/language-switch", () => ({
  LanguageSwitch: () => null,
}));

vi.mock("@/components/general/progress-bar", () => ({
  ProgressBar: () => null,
}));

vi.mock("@/components/general/recaptcha-branding", () => ({
  RecaptchaBranding: () => null,
}));

vi.mock("@/components/general/survey-close-button", () => ({
  SurveyCloseButton: () => null,
}));

const defaultLanguage = {
  default: true,
  enabled: true,
  language: {
    id: "lang123456789012345678901",
    code: "en",
    alias: null,
    createdAt: new Date(),
    updatedAt: new Date(),
    workspaceId: "workspace12345678901234567",
  },
};

const baseSurvey: TJsWorkspaceStateSurvey = {
  id: "survey12345678901234567890",
  name: "Offline Resume Survey",
  type: "link",
  status: "inProgress",
  questions: [],
  blocks: [
    {
      id: "block-1",
      elements: [
        {
          id: "q1",
          type: TSurveyElementTypeEnum.OpenText,
          required: false,
        },
      ],
      logic: [],
    },
    {
      id: "block-2",
      elements: [
        {
          id: "q2",
          type: TSurveyElementTypeEnum.OpenText,
          required: false,
        },
      ],
      logic: [],
    },
  ],
  endings: [{ id: "ending-1" }],
  welcomeCard: { enabled: false, timeToFinish: false, showResponseCount: false },
  variables: [],
  styling: { overwriteThemeStyling: false },
  recontactDays: null,
  displayLimit: null,
  displayPercentage: null,
  languages: [defaultLanguage],
  segment: null,
  hiddenFields: { enabled: false, fieldIds: [] },
  workspaceOverwrites: null,
  triggers: [],
  displayOption: "displayOnce",
  showLanguageSwitch: false,
  isBackButtonHidden: false,
  isAutoProgressingEnabled: false,
  recaptcha: {
    enabled: false,
  },
} as unknown as TJsWorkspaceStateSurvey;

const makeProgress = (overrides: Record<string, unknown> = {}) => ({
  surveyId: baseSurvey.id,
  blockId: "block-2",
  responseData: { q1: "saved-answer" },
  ttc: { q1: 111 },
  currentVariables: { savedVar: "saved" },
  history: ["block-1"],
  selectedLanguage: "en",
  surveyStateSnapshot: {
    responseId: null,
    displayId: "display1234567890123456789",
    surveyId: baseSurvey.id,
    singleUseId: null,
    userId: null,
    contactId: null,
    responseAcc: {
      finished: false,
      data: { q1: "saved-answer" },
      ttc: { q1: 111 },
      variables: { savedVar: "saved" },
    },
  },
  updatedAt: Date.now(),
  ...overrides,
});

const renderSurvey = () =>
  render(
    <Survey
      appUrl="http://localhost:3000"
      workspaceId="ws1234567890123456789012"
      survey={baseSurvey}
      styling={{} as any}
      isBrandingEnabled={false}
      languageCode="en"
      offlineSupport
      isSpamProtectionEnabled={false}
    />
  );

describe("Survey offline restore", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    offlineStorageMocks.addPendingResponse.mockResolvedValue(1);
    offlineStorageMocks.countPendingResponses.mockResolvedValue(0);
    offlineStorageMocks.getPendingResponses.mockResolvedValue([]);
    offlineStorageMocks.removePendingResponse.mockResolvedValue(undefined);
    offlineStorageMocks.clearSurveyProgress.mockResolvedValue(undefined);
    offlineStorageMocks.patchSurveyProgressSnapshot.mockResolvedValue(undefined);
    offlineStorageMocks.saveSurveyProgress.mockResolvedValue(undefined);
    apiClientMocks.createDisplay.mockResolvedValue({ ok: true, data: { id: "display-created" } });
    apiClientMocks.createResponse.mockResolvedValue({ ok: true, data: { id: "response-created" } });
    apiClientMocks.updateResponse.mockResolvedValue({ ok: true, data: { quotaFull: false } });
    apiClientMocks.getResponseIdByDisplayId.mockResolvedValue({ ok: true, data: { responseId: null } });
    window.parent.postMessage = vi.fn();
  });

  afterEach(() => {
    cleanup();
  });

  test("recovers responseId from displayId and continues on the update path", async () => {
    offlineStorageMocks.getSurveyProgress.mockResolvedValue(makeProgress());
    apiClientMocks.getResponseIdByDisplayId.mockResolvedValue({
      ok: true,
      data: { responseId: "response-recovered" },
    });

    renderSurvey();

    await waitFor(() => {
      expect(apiClientMocks.getResponseIdByDisplayId).toHaveBeenCalledWith("display1234567890123456789");
    });

    fireEvent.click(await screen.findByTestId("submit-block-2"));

    await waitFor(() => {
      expect(apiClientMocks.updateResponse).toHaveBeenCalledWith(
        expect.objectContaining({
          responseId: "response-recovered",
          data: { q2: "block-2-answer" },
        })
      );
    });

    expect(apiClientMocks.createResponse).not.toHaveBeenCalled();
    expect(apiClientMocks.createDisplay).not.toHaveBeenCalled();
    expect(offlineStorageMocks.patchSurveyProgressSnapshot).toHaveBeenCalledWith(baseSurvey.id, {
      responseId: "response-recovered",
    });
  });

  test("bootstraps create from restored progress when display lookup returns no response", async () => {
    offlineStorageMocks.getSurveyProgress.mockResolvedValue(makeProgress());

    renderSurvey();

    fireEvent.click(await screen.findByTestId("submit-block-2"));

    await waitFor(() => {
      expect(apiClientMocks.createResponse).toHaveBeenCalledWith(
        expect.objectContaining({
          displayId: "display1234567890123456789",
          finished: true,
          data: { q1: "saved-answer", q2: "block-2-answer" },
          ttc: { q1: 111, q2: 123 },
          variables: { savedVar: "saved" },
        })
      );
    });

    expect(apiClientMocks.updateResponse).not.toHaveBeenCalled();
    expect(apiClientMocks.createDisplay).not.toHaveBeenCalled();
    expect(offlineStorageMocks.patchSurveyProgressSnapshot).toHaveBeenCalledWith(baseSurvey.id, {
      responseId: "response-created",
    });
  });

  test("creates a new display and bootstraps from restored progress when no ids are saved", async () => {
    offlineStorageMocks.getSurveyProgress.mockResolvedValue(
      makeProgress({
        surveyStateSnapshot: {
          responseId: null,
          displayId: null,
          surveyId: baseSurvey.id,
          singleUseId: null,
          userId: null,
          contactId: null,
          responseAcc: {
            finished: false,
            data: { q1: "saved-answer" },
            ttc: { q1: 111 },
            variables: { savedVar: "saved" },
          },
        },
      })
    );

    renderSurvey();

    await waitFor(() => {
      expect(apiClientMocks.createDisplay).toHaveBeenCalled();
    });

    fireEvent.click(await screen.findByTestId("submit-block-2"));

    await waitFor(() => {
      expect(apiClientMocks.createResponse).toHaveBeenCalledWith(
        expect.objectContaining({
          displayId: "display-created",
          data: { q1: "saved-answer", q2: "block-2-answer" },
        })
      );
    });

    expect(offlineStorageMocks.patchSurveyProgressSnapshot).toHaveBeenCalledWith(baseSurvey.id, {
      displayId: "display-created",
    });
  });

  test("sends the first ending id when the survey falls off the last block without a jump target", async () => {
    offlineStorageMocks.getSurveyProgress.mockResolvedValue(makeProgress());

    renderSurvey();

    fireEvent.click(await screen.findByTestId("submit-block-2"));

    await waitFor(() => {
      expect(apiClientMocks.createResponse).toHaveBeenCalledWith(
        expect.objectContaining({
          finished: true,
          endingId: "ending-1",
        })
      );
    });
  });

  test("skips responseId recovery while pending offline entries exist", async () => {
    offlineStorageMocks.getSurveyProgress.mockResolvedValue(makeProgress());
    offlineStorageMocks.countPendingResponses.mockResolvedValue(2);

    renderSurvey();

    await waitFor(() => {
      expect(offlineStorageMocks.countPendingResponses).toHaveBeenCalled();
    });

    expect(apiClientMocks.getResponseIdByDisplayId).not.toHaveBeenCalled();
  });

  test("clears stale finished progress instead of restoring the ending card", async () => {
    offlineStorageMocks.getSurveyProgress.mockResolvedValue(
      makeProgress({
        blockId: "ending-1",
        surveyStateSnapshot: {
          responseId: "response-finished",
          displayId: "display1234567890123456789",
          surveyId: baseSurvey.id,
          singleUseId: null,
          userId: null,
          contactId: null,
          responseAcc: {
            finished: true,
            data: { q1: "saved-answer", q2: "done" },
            ttc: { q1: 111, q2: 123 },
            variables: { savedVar: "saved" },
          },
        },
      })
    );

    renderSurvey();

    await waitFor(() => {
      expect(offlineStorageMocks.clearSurveyProgress).toHaveBeenCalledWith(baseSurvey.id);
    });

    expect(apiClientMocks.getResponseIdByDisplayId).not.toHaveBeenCalled();
  });
});

// F1 (R-03): a link survey that saves progress goes on where the respondent left off after a reload, without the offline mode's prompts.
describe("Survey progress persistence", () => {
  const renderWith = (props: Partial<Parameters<typeof Survey>[0]> = {}) =>
    render(
      <Survey
        appUrl="http://localhost:3000"
        workspaceId="ws1234567890123456789012"
        survey={baseSurvey}
        styling={{} as any}
        isBrandingEnabled={false}
        languageCode="en"
        isSpamProtectionEnabled={false}
        getSetIsResponseSendingFinished={() => {}}
        {...props}
      />
    );

  const leavePage = () => {
    const event = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(event);
    return event.defaultPrevented;
  };

  beforeEach(() => {
    vi.clearAllMocks();
    offlineStorageMocks.addPendingResponse.mockResolvedValue(1);
    offlineStorageMocks.countPendingResponses.mockResolvedValue(0);
    offlineStorageMocks.getPendingResponses.mockResolvedValue([]);
    offlineStorageMocks.removePendingResponse.mockResolvedValue(undefined);
    offlineStorageMocks.clearSurveyProgress.mockResolvedValue(undefined);
    offlineStorageMocks.getSurveyProgress.mockResolvedValue(undefined);
    offlineStorageMocks.patchSurveyProgressSnapshot.mockResolvedValue(undefined);
    offlineStorageMocks.saveSurveyProgress.mockResolvedValue(undefined);
    apiClientMocks.createDisplay.mockResolvedValue({ ok: true, data: { id: "display-created" } });
    apiClientMocks.createResponse.mockResolvedValue({ ok: true, data: { id: "response-created" } });
    apiClientMocks.updateResponse.mockResolvedValue({ ok: true, data: { quotaFull: false } });
    apiClientMocks.getResponseIdByDisplayId.mockResolvedValue({ ok: true, data: { responseId: null } });
    window.parent.postMessage = vi.fn();
  });

  afterEach(() => {
    cleanup();
  });

  test("goes on in the same response and display after a reload, and never asks before the page is left", async () => {
    offlineStorageMocks.getSurveyProgress.mockResolvedValue(
      makeProgress({
        surveyStateSnapshot: { ...makeProgress().surveyStateSnapshot, responseId: "response-saved" },
      })
    );

    renderWith({ persistProgress: true });

    fireEvent.click(await screen.findByTestId("submit-block-2"));
    await waitFor(() =>
      expect(apiClientMocks.updateResponse).toHaveBeenCalledWith(
        expect.objectContaining({ responseId: "response-saved", data: { q2: "block-2-answer" } })
      )
    );
    expect(apiClientMocks.createDisplay).not.toHaveBeenCalled();
    expect(apiClientMocks.createResponse).not.toHaveBeenCalled();
    expect(leavePage()).toBe(false);
  });

  test("offline mode still asks before a survey half answered is left", async () => {
    offlineStorageMocks.getSurveyProgress.mockResolvedValue(makeProgress());

    renderWith({ persistProgress: true, offlineSupport: true });

    await screen.findByTestId("submit-block-2");
    await waitFor(() => expect(leavePage()).toBe(true));
  });

  test("keeps nothing in the browser when the survey does not save progress", async () => {
    renderWith({ persistProgress: false });

    fireEvent.click(await screen.findByTestId("submit-block-1"));
    await waitFor(() => expect(apiClientMocks.createResponse).toHaveBeenCalled());

    expect(offlineStorageMocks.getSurveyProgress).not.toHaveBeenCalled();
    expect(offlineStorageMocks.saveSurveyProgress).not.toHaveBeenCalled();
    expect(offlineStorageMocks.addPendingResponse).not.toHaveBeenCalled();
  });

  test("starts afresh when the stored progress belongs to another single-use link", async () => {
    offlineStorageMocks.getSurveyProgress.mockResolvedValue(
      makeProgress({
        surveyStateSnapshot: {
          ...makeProgress().surveyStateSnapshot,
          responseId: "someone-elses-response",
          singleUseId: "single-use-1",
        },
      })
    );

    renderWith({ persistProgress: true, mode: "inline", singleUseId: "single-use-2" });

    await waitFor(() => expect(offlineStorageMocks.clearSurveyProgress).toHaveBeenCalledWith(baseSurvey.id));
    await waitFor(() => expect(apiClientMocks.createDisplay).toHaveBeenCalled());
    fireEvent.click(await screen.findByTestId("submit-block-1"));
    await waitFor(() =>
      expect(apiClientMocks.createResponse).toHaveBeenCalledWith(
        expect.objectContaining({ singleUseId: "single-use-2", data: { q1: "block-1-answer" } })
      )
    );
    expect(apiClientMocks.updateResponse).not.toHaveBeenCalled();
  });

  test("forgets the stored progress once the server has the final answer", async () => {
    renderWith({ persistProgress: true });

    fireEvent.click(await screen.findByTestId("submit-block-1"));
    await waitFor(() => expect(apiClientMocks.createResponse).toHaveBeenCalled());
    expect(offlineStorageMocks.clearSurveyProgress).not.toHaveBeenCalled();
    expect(offlineStorageMocks.saveSurveyProgress).toHaveBeenCalledWith(
      expect.objectContaining({ surveyId: baseSurvey.id, blockId: "block-2" })
    );

    fireEvent.click(await screen.findByTestId("submit-block-2"));
    await waitFor(() =>
      expect(apiClientMocks.updateResponse).toHaveBeenCalledWith(expect.objectContaining({ finished: true }))
    );
    await waitFor(() => expect(offlineStorageMocks.clearSurveyProgress).toHaveBeenCalledWith(baseSurvey.id));
  });

  // An IndexedDB that keeps what is stored, seeded with rows an earlier page left.
  const storeRows = (seed: object[]) => {
    const store = { rows: seed.map((row, index) => ({ ...row, id: index + 1 })) as { id: number }[] };
    let nextId = store.rows.length;
    offlineStorageMocks.addPendingResponse.mockImplementation(async (entry: object) => {
      nextId += 1;
      store.rows.push({ ...entry, id: nextId });
      return nextId;
    });
    offlineStorageMocks.countPendingResponses.mockImplementation(async () => store.rows.length);
    offlineStorageMocks.getPendingResponses.mockImplementation(async () =>
      store.rows.map((row) => ({ ...row }))
    );
    offlineStorageMocks.removePendingResponse.mockImplementation(async (id: number) => {
      store.rows = store.rows.filter((row) => row.id !== id);
    });
    return store;
  };

  const earlierRow = (snapshot: Record<string, unknown>, data: Record<string, string>, ageMs = 60_000) => ({
    surveyId: baseSurvey.id,
    responseUpdate: { data, finished: false },
    surveyStateSnapshot: { ...makeProgress().surveyStateSnapshot, ...snapshot },
    createdAt: Date.now() - ageMs,
  });

  test("an unsent answer of another single-use link goes to that link's response, not into this respondent's", async () => {
    const otherRespondent = { responseId: "response-A", singleUseId: "single-use-1" };
    offlineStorageMocks.getSurveyProgress.mockResolvedValue(
      makeProgress({ surveyStateSnapshot: { ...makeProgress().surveyStateSnapshot, ...otherRespondent } })
    );
    const store = storeRows([earlierRow(otherRespondent, { q1: "A-answer" })]);

    renderWith({ persistProgress: true, mode: "inline", singleUseId: "single-use-2" });

    await waitFor(() => expect(offlineStorageMocks.clearSurveyProgress).toHaveBeenCalledWith(baseSurvey.id));
    await waitFor(() =>
      expect(apiClientMocks.updateResponse).toHaveBeenCalledWith(
        expect.objectContaining({ responseId: "response-A", data: { q1: "A-answer" } })
      )
    );
    fireEvent.click(await screen.findByTestId("submit-block-1"));
    await waitFor(() =>
      expect(apiClientMocks.createResponse).toHaveBeenCalledWith(
        expect.objectContaining({ singleUseId: "single-use-2", data: { q1: "block-1-answer" } })
      )
    );
    expect(apiClientMocks.updateResponse).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(store.rows).toHaveLength(0));
  });

  test("an unsent answer older than the progress it belonged to does not pull a fresh start into the old response", async () => {
    const day = 25 * 60 * 60 * 1000;
    const stale = { responseId: "response-old", displayId: "display-old" };
    offlineStorageMocks.getSurveyProgress.mockResolvedValue(
      makeProgress({
        updatedAt: Date.now() - day,
        surveyStateSnapshot: { ...makeProgress().surveyStateSnapshot, ...stale },
      })
    );
    storeRows([earlierRow(stale, { q1: "old-answer" }, day)]);

    renderWith({ persistProgress: true });

    await waitFor(() =>
      expect(apiClientMocks.updateResponse).toHaveBeenCalledWith(
        expect.objectContaining({ responseId: "response-old", data: { q1: "old-answer" } })
      )
    );
    await waitFor(() => expect(apiClientMocks.createDisplay).toHaveBeenCalled());
    fireEvent.click(await screen.findByTestId("submit-block-1"));
    await waitFor(() =>
      expect(apiClientMocks.createResponse).toHaveBeenCalledWith(
        expect.objectContaining({ displayId: "display-created", data: { q1: "block-1-answer" } })
      )
    );
    expect(apiClientMocks.updateResponse).toHaveBeenCalledTimes(1);
  });

  test("an answer from the middle of the survey sent after a reload does not count as the end", async () => {
    const resumed = { responseId: "response-A" };
    offlineStorageMocks.getSurveyProgress.mockResolvedValue(
      makeProgress({ surveyStateSnapshot: { ...makeProgress().surveyStateSnapshot, ...resumed } })
    );
    const store = storeRows([earlierRow(resumed, { q1: "A-answer" })]);
    const onFinished = vi.fn();

    renderWith({ persistProgress: true, onFinished });

    await waitFor(() => expect(store.rows).toHaveLength(0));
    expect(apiClientMocks.updateResponse).toHaveBeenCalledWith(
      expect.objectContaining({ responseId: "response-A", data: { q1: "A-answer" } })
    );
    // The position just restored survives another reload.
    expect(offlineStorageMocks.clearSurveyProgress).not.toHaveBeenCalled();

    // The final answer is still in flight.
    apiClientMocks.updateResponse.mockImplementation(() => new Promise(() => {}));
    fireEvent.click(await screen.findByTestId("submit-block-2"));
    await waitFor(() =>
      expect(apiClientMocks.updateResponse).toHaveBeenCalledWith(
        expect.objectContaining({ responseId: "response-A", finished: true })
      )
    );
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(onFinished).not.toHaveBeenCalled();
  });

  test("a survey that stopped saving progress removes what earlier respondents left, after sending their answers once", async () => {
    const store = storeRows([earlierRow({ responseId: "response-A" }, { q1: "A-answer" })]);
    apiClientMocks.updateResponse.mockResolvedValue({
      ok: false,
      error: { code: "internal_server_error", message: "Server error", status: 500 },
    });

    renderWith({ persistProgress: false });

    await waitFor(() => expect(offlineStorageMocks.clearSurveyProgress).toHaveBeenCalledWith(baseSurvey.id));
    await waitFor(() => expect(store.rows).toHaveLength(0));
    expect(apiClientMocks.updateResponse).toHaveBeenCalledTimes(1);
    expect(apiClientMocks.updateResponse).toHaveBeenCalledWith(
      expect.objectContaining({ responseId: "response-A", data: { q1: "A-answer" } })
    );

    fireEvent.click(await screen.findByTestId("submit-block-1"));
    await waitFor(() =>
      expect(apiClientMocks.createResponse).toHaveBeenCalledWith(
        expect.objectContaining({ data: { q1: "block-1-answer" } })
      )
    );
    expect(offlineStorageMocks.addPendingResponse).not.toHaveBeenCalled();
  });

  test("keeps no progress for a survey shown on a customer's site", async () => {
    renderWith({ persistProgress: true, survey: { ...baseSurvey, type: "app" } as TJsWorkspaceStateSurvey });

    fireEvent.click(await screen.findByTestId("submit-block-1"));
    await waitFor(() => expect(apiClientMocks.createResponse).toHaveBeenCalled());
    expect(offlineStorageMocks.getSurveyProgress).not.toHaveBeenCalled();
    expect(offlineStorageMocks.saveSurveyProgress).not.toHaveBeenCalled();
  });
});

// R-01 and R-02: the answers a respondent gives reach the server even when the host re-renders the survey or the connection drops for a while.
describe("Survey response delivery", () => {
  const serverError = {
    ok: false,
    error: { code: "internal_server_error", message: "Server error", status: 500 },
  };

  const surveyElement = (getSetIsResponseSendingFinished: (f: (value: boolean) => void) => void) => (
    <Survey
      appUrl="http://localhost:3000"
      workspaceId="ws1234567890123456789012"
      survey={baseSurvey}
      styling={{} as any}
      isBrandingEnabled={false}
      languageCode="en"
      isSpamProtectionEnabled={false}
      getSetIsResponseSendingFinished={getSetIsResponseSendingFinished}
    />
  );

  beforeEach(() => {
    vi.clearAllMocks();
    apiClientMocks.createDisplay.mockResolvedValue({ ok: true, data: { id: "display-created" } });
    apiClientMocks.createResponse.mockResolvedValue({ ok: true, data: { id: "response-created" } });
    apiClientMocks.updateResponse.mockResolvedValue({ ok: true, data: { quotaFull: false } });
    window.parent.postMessage = vi.fn();
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  test("an answer given while the previous one is still being sent is not lost when the host re-renders", async () => {
    let releaseCreate!: () => void;
    apiClientMocks.createResponse.mockImplementation(
      () =>
        new Promise((resolve) => {
          releaseCreate = () => resolve({ ok: true, data: { id: "response-created" } });
        })
    );

    // A host that passes a fresh callback on every render, as the link page did.
    const { rerender } = render(surveyElement(() => {}));
    fireEvent.click(await screen.findByTestId("submit-block-1"));
    await waitFor(() => expect(apiClientMocks.createResponse).toHaveBeenCalledTimes(1));

    rerender(surveyElement(() => {}));
    fireEvent.click(await screen.findByTestId("submit-block-2"));
    rerender(surveyElement(() => {}));

    releaseCreate();

    await waitFor(() =>
      expect(apiClientMocks.updateResponse).toHaveBeenCalledWith(
        expect.objectContaining({
          responseId: "response-created",
          data: { q2: "block-2-answer" },
          finished: true,
        })
      )
    );
    expect(apiClientMocks.createResponse).toHaveBeenCalledWith(
      expect.objectContaining({ data: { q1: "block-1-answer" }, finished: false })
    );
  });

  test("a card that cannot be sent shows a notice and lets the respondent carry on", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    apiClientMocks.createResponse.mockResolvedValue(serverError);

    render(surveyElement(() => {}));
    fireEvent.click(await screen.findByTestId("submit-block-1"));

    expect(await screen.findByText("Sending delayed")).toBeTruthy();
    expect(await screen.findByTestId("submit-block-2")).toBeTruthy();

    // The whole burst of retries fails; the survey still is not replaced by the error card.
    await vi.advanceTimersByTimeAsync(16_000);
    expect(screen.queryByText("Response Error")).toBeNull();
    expect(screen.getByTestId("submit-block-2")).toBeTruthy();

    apiClientMocks.createResponse.mockResolvedValue({ ok: true, data: { id: "response-created" } });
    window.dispatchEvent(new Event("online"));

    await waitFor(() => expect(screen.queryByText("Sending delayed")).toBeNull());
    expect(apiClientMocks.createResponse).toHaveBeenLastCalledWith(
      expect.objectContaining({ data: { q1: "block-1-answer" } })
    );
  });

  test("a final submission that cannot be sent shows the error card, and Retry sends it", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    render(surveyElement(() => {}));
    fireEvent.click(await screen.findByTestId("submit-block-1"));
    await waitFor(() => expect(apiClientMocks.createResponse).toHaveBeenCalledTimes(1));

    apiClientMocks.updateResponse.mockResolvedValue(serverError);
    fireEvent.click(await screen.findByTestId("submit-block-2"));
    await vi.advanceTimersByTimeAsync(16_000);
    expect(await screen.findByText("Response Error")).toBeTruthy();

    apiClientMocks.updateResponse.mockClear();
    apiClientMocks.updateResponse.mockResolvedValue({ ok: true, data: { quotaFull: false } });
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    // The ack clears the error card (this file's card stack mock cannot show the ending itself).
    await waitFor(() => expect(screen.queryByText("Response Error")).toBeNull());
    expect(apiClientMocks.updateResponse).toHaveBeenCalledWith(
      expect.objectContaining({
        responseId: "response-created",
        data: { q2: "block-2-answer" },
        finished: true,
      })
    );
  });

  test("with offline support, finishing after a card that could not be sent shows the ending once the server is back", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    // An IndexedDB that keeps what is stored, so the queue hands the sending to its sync of stored answers.
    let rows: { id: number }[] = [];
    let nextId = 0;
    offlineStorageMocks.addPendingResponse.mockImplementation(async (entry: object) => {
      nextId += 1;
      rows.push({ ...entry, id: nextId });
      return nextId;
    });
    offlineStorageMocks.countPendingResponses.mockImplementation(async () => rows.length);
    offlineStorageMocks.getPendingResponses.mockImplementation(async () => rows.map((row) => ({ ...row })));
    offlineStorageMocks.removePendingResponse.mockImplementation(async (id: number) => {
      rows = rows.filter((row) => row.id !== id);
    });
    offlineStorageMocks.getSurveyProgress.mockResolvedValue(undefined);
    offlineStorageMocks.saveSurveyProgress.mockResolvedValue(undefined);
    offlineStorageMocks.clearSurveyProgress.mockResolvedValue(undefined);
    offlineStorageMocks.patchSurveyProgressSnapshot.mockResolvedValue(undefined);
    apiClientMocks.createResponse.mockResolvedValue(serverError);
    const onFinished = vi.fn();

    render(
      <Survey
        appUrl="http://localhost:3000"
        workspaceId="ws1234567890123456789012"
        survey={baseSurvey}
        styling={{} as any}
        isBrandingEnabled={false}
        languageCode="en"
        offlineSupport
        isSpamProtectionEnabled={false}
        getSetIsResponseSendingFinished={() => {}}
        onFinished={onFinished}
      />
    );
    fireEvent.click(await screen.findByTestId("submit-block-1"));
    expect(await screen.findByText("Sending delayed")).toBeTruthy();
    await vi.advanceTimersByTimeAsync(16_000);
    expect(apiClientMocks.createResponse).toHaveBeenCalledTimes(5);

    // The server is back, and the respondent finishes before the background retry is due.
    apiClientMocks.createResponse.mockResolvedValue({ ok: true, data: { id: "response-created" } });
    fireEvent.click(screen.getByTestId("submit-block-2"));

    await waitFor(() => expect(onFinished).toHaveBeenCalledWith("response-created"));
    expect(screen.queryByText("Sending delayed")).toBeNull();
    expect(screen.queryByText("Response Error")).toBeNull();
    expect(apiClientMocks.updateResponse).toHaveBeenCalledWith(
      expect.objectContaining({
        responseId: "response-created",
        data: { q2: "block-2-answer" },
        finished: true,
      })
    );
    expect(rows).toHaveLength(0);
  });
});
