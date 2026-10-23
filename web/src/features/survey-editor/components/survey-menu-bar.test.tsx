import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { useState } from "react";
import { I18nextProvider } from "react-i18next";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSegment } from "@forma/types/segment";
import type { TSurvey, TSurveyEditorTabs } from "@forma/types/surveys/types";
import type { TWorkspace } from "@forma/types/workspace";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { instanceKeys } from "../../../lib/queries/instance";
import { analysisKeys } from "../../survey-analysis/api";
import { surveyEditorKeys } from "../api";
import { SurveyMenuBar } from "./survey-menu-bar";

const WORKSPACE_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f";
const SURVEY_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e";
const EDITOR_PATH = `/workspaces/${WORKSPACE_ID}/surveys/${SURVEY_ID}/edit`;
const SUMMARY_PATH = `/workspaces/${WORKSPACE_ID}/surveys/${SURVEY_ID}/summary`;
const LOADED_AT = new Date("2026-10-01T00:00:00.000Z");
const SAVED_AT = "2026-10-02T09:30:00.123Z";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const survey = (overrides: Partial<TSurvey> = {}): TSurvey =>
  ({
    id: SURVEY_ID,
    createdAt: LOADED_AT,
    updatedAt: LOADED_AT,
    name: "Feedback",
    type: "link",
    workspaceId: WORKSPACE_ID,
    createdBy: null,
    status: "draft",
    displayOption: "displayOnce",
    autoClose: null,
    triggers: [],
    recontactDays: null,
    displayLimit: null,
    welcomeCard: { enabled: false, timeToFinish: true, showResponseCount: false },
    questions: [],
    blocks: [
      {
        id: "blockone0000000000000000",
        name: "Block 1",
        elements: [
          {
            id: "q1",
            type: TSurveyElementTypeEnum.OpenText,
            headline: { default: "How was it?" },
            required: true,
            inputType: "text",
            charLimit: { enabled: false },
          },
        ],
      },
    ],
    endings: [{ id: "endingone000000000000000", type: "endScreen", headline: { default: "Thanks" } }],
    hiddenFields: { enabled: true, fieldIds: [] },
    variables: [],
    followUps: [],
    delay: 0,
    publishOn: null,
    closeOn: null,
    autoComplete: null,
    workspaceOverwrites: null,
    styling: null,
    showLanguageSwitch: null,
    surveyClosedMessage: null,
    segment: null,
    singleUse: null,
    isVerifyEmailEnabled: false,
    recaptcha: null,
    isBackButtonHidden: false,
    isAutoProgressingEnabled: false,
    isCaptureIpEnabled: false,
    isAnonymizeResponsesEnabled: false,
    pin: null,
    displayPercentage: null,
    languages: [],
    metadata: {},
    slug: null,
    ...overrides,
  }) as TSurvey;

interface SaveRequest {
  path: string;
  body: Record<string, unknown>;
}

/** The Go server's survey saves: every PUT is answered with the survey as sent, stamped with a new version, unless `failWith` sets the status of the answer. */
const stubSaveEndpoint = (failWith?: number): SaveRequest[] => {
  const saves: SaveRequest[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit) => {
      if (init.method !== "PUT") throw new Error(`unexpected ${init.method ?? "GET"} ${path}`);
      const body = JSON.parse(init.body as string) as Record<string, unknown>;
      saves.push({ path, body });
      if (failWith) {
        return new Response(JSON.stringify({ code: "internal_error", message: "boom" }), {
          status: failWith,
          headers: { "content-type": "application/json" },
        });
      }
      return new Response(JSON.stringify({ ...body, updatedAt: SAVED_AT }), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    })
  );
  return saves;
};

/** An app survey as the editor holds it after "Website & App" was picked for a survey without a segment: the placeholder segment how-to-send-card gives it, with the targeting the user set, and one trigger. */
const appSurvey = (filters: TSegment["filters"]): TSurvey =>
  survey({
    type: "app",
    triggers: [
      {
        actionClass: {
          id: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d10",
          name: "New Session",
          description: null,
          type: "code",
          key: "new-session",
          noCodeConfig: null,
          workspaceId: WORKSPACE_ID,
          createdAt: LOADED_AT,
          updatedAt: LOADED_AT,
        },
      },
    ],
    segment: {
      id: "temp",
      isPrivate: true,
      title: SURVEY_ID,
      workspaceId: WORKSPACE_ID,
      surveys: [SURVEY_ID],
      filters,
      createdAt: LOADED_AT,
      updatedAt: LOADED_AT,
      description: "",
    },
  });

const PRIVATE_SEGMENT_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d11";

/** The Go server's survey saves and segment creation: a PUT is answered with the survey as sent, given the private segment an app survey saved without one gets; a POST to the segments creates the private segment it was sent. */
const stubAppSurveyEndpoints = (): SaveRequest[] => {
  const requests: SaveRequest[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit) => {
      const body = JSON.parse(init.body as string) as Record<string, unknown>;
      requests.push({ path, body });
      const stamp = { createdAt: SAVED_AT, updatedAt: SAVED_AT };
      if (init.method === "POST" && path === `/api/app/workspaces/${WORKSPACE_ID}/segments`) {
        const created = { ...body, ...stamp, id: PRIVATE_SEGMENT_ID, workspaceId: WORKSPACE_ID };
        return Response.json(
          { ...created, surveys: [SURVEY_ID], activeSurveys: [], inactiveSurveys: [] },
          { status: 201 }
        );
      }
      if (init.method !== "PUT") throw new Error(`unexpected ${init.method ?? "GET"} ${path}`);
      const segment = body.segment ?? {
        ...stamp,
        id: PRIVATE_SEGMENT_ID,
        title: SURVEY_ID,
        description: null,
        isPrivate: true,
        filters: [],
        workspaceId: WORKSPACE_ID,
        surveys: [SURVEY_ID],
      };
      return Response.json({ ...body, segment, updatedAt: SAVED_AT });
    })
  );
  return requests;
};

/** The menu bar inside a router that has the editor and the survey summary it leaves for, opened on the editor with no history behind it, as a new tab or a pasted URL opens it. */
const renderMenuBar = (persisted: TSurvey) => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  queryClient.setQueryData(surveyEditorKeys.survey(persisted.id), persisted);

  const Editor = () => {
    const [localSurvey, setLocalSurvey] = useState(persisted);
    const [activeId, setActiveId] = useState<TSurveyEditorTabs>("elements");
    const [, setInvalidElements] = useState<string[] | null>(null);
    const [, setHasTriggerError] = useState(false);
    return (
      <SurveyMenuBar
        localSurvey={localSurvey}
        survey={persisted}
        setLocalSurvey={setLocalSurvey}
        activeId={activeId}
        setActiveId={setActiveId}
        setInvalidElements={setInvalidElements}
        setHasTriggerError={setHasTriggerError}
        workspace={{ id: WORKSPACE_ID, name: "Acme" } as TWorkspace}
        responseCount={0}
        finishedResponseCount={0}
        selectedLanguageCode="default"
        isCxMode={false}
        locale="en-US"
        setIsCautionDialogOpen={() => {}}
      />
    );
  };

  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/workspaces/$workspaceId/surveys/$surveyId/edit",
      component: Editor,
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/workspaces/$workspaceId/surveys/$surveyId/summary",
      component: () => <h1>Survey summary</h1>,
    }),
  ]);
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [EDITOR_PATH] }),
  });

  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  return { queryClient, router };
};

describe("SurveyMenuBar", () => {
  test("Publish saves the survey as in progress, caches it for the editor, drops its cached analysis and leaves for its summary, which announces the publish", async () => {
    const saves = stubSaveEndpoint();
    const { queryClient, router } = renderMenuBar(survey());
    const staleAnalysisKey = analysisKeys.displays(SURVEY_ID);
    queryClient.setQueryData(staleAnalysisKey, []);

    fireEvent.click(await screen.findByRole("button", { name: "Publish" }));

    expect(await screen.findByRole("heading", { name: "Survey summary" })).toBeTruthy();
    expect(router.state.location.pathname).toBe(SUMMARY_PATH);
    expect(router.state.location.search).toEqual({ success: true });
    expect(queryClient.getQueryData(staleAnalysisKey)).toBeUndefined();
    expect(saves).toHaveLength(1);
    expect(saves[0]?.path).toBe(`/api/app/surveys/${SURVEY_ID}`);
    expect(saves[0]?.body.status).toBe("inProgress");
    const cached = queryClient.getQueryData<TSurvey>(surveyEditorKeys.survey(SURVEY_ID));
    expect(cached?.status).toBe("inProgress");
    expect(cached?.updatedAt).toEqual(new Date(SAVED_AT));
  });

  test("Publish of an app survey with no targeting saves it without a segment, so the server gives it its empty private segment, as forma2's editor did", async () => {
    const requests = stubAppSurveyEndpoints();
    const { router } = renderMenuBar(appSurvey([]));

    // A draft app survey first leads to its settings, where its audience is set; Publish follows.
    fireEvent.click(await screen.findByRole("button", { name: "Continue to Settings" }));
    fireEvent.click(await screen.findByRole("button", { name: "Publish" }));

    expect(await screen.findByRole("heading", { name: "Survey summary" })).toBeTruthy();
    expect(router.state.location.search).toEqual({ success: true });
    // forma2's segment create refuses an empty filter tree, private or not: nothing is created up front.
    expect(requests.map(({ path }) => path)).toEqual([`/api/app/surveys/${SURVEY_ID}`]);
    expect(requests[0]?.body).toMatchObject({ type: "app", status: "inProgress", segment: null });
  });

  test("Publish of an app survey with targeting creates its private segment first and saves the survey on it", async () => {
    const requests = stubAppSurveyEndpoints();
    const filters: TSegment["filters"] = [
      {
        id: "filternode00000000000000",
        connector: null,
        resource: {
          id: "filterleaf00000000000000",
          root: { type: "attribute", contactAttributeKey: "email" },
          value: "user@example.com",
          qualifier: { operator: "equals" },
        },
      },
    ];
    renderMenuBar(appSurvey(filters));

    fireEvent.click(await screen.findByRole("button", { name: "Continue to Settings" }));
    fireEvent.click(await screen.findByRole("button", { name: "Publish" }));

    expect(await screen.findByRole("heading", { name: "Survey summary" })).toBeTruthy();
    expect(requests.map(({ path }) => path)).toEqual([
      `/api/app/workspaces/${WORKSPACE_ID}/segments`,
      `/api/app/surveys/${SURVEY_ID}`,
    ]);
    expect(requests[0]?.body).toEqual({ filters, isPrivate: true, surveyId: SURVEY_ID, title: SURVEY_ID });
    expect(requests[1]?.body).toMatchObject({
      status: "inProgress",
      segment: { id: PRIVATE_SEGMENT_ID, isPrivate: true, filters },
    });
  });

  test("a draft save of an app survey with no targeting saves it without a segment", async () => {
    const requests = stubAppSurveyEndpoints();
    renderMenuBar(appSurvey([]));

    fireEvent.click(await screen.findByRole("button", { name: "Save as draft" }));

    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Save as draft" })).toHaveProperty("disabled", false)
    );
    expect(requests.map(({ path }) => path)).toEqual([`/api/app/surveys/${SURVEY_ID}/draft`]);
    expect(requests[0]?.body).toMatchObject({ type: "app", status: "draft", segment: null });
  });

  test("a refused publish stays in the editor", async () => {
    const saves = stubSaveEndpoint(500);
    const { router } = renderMenuBar(survey());

    fireEvent.click(await screen.findByRole("button", { name: "Publish" }));

    await waitFor(() => expect(saves).toHaveLength(1));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Publish" })).toHaveProperty("disabled", false)
    );
    expect(router.state.location.pathname).toBe(EDITOR_PATH);
    expect(screen.queryByRole("heading", { name: "Survey summary" })).toBeNull();
  });

  test("Save & Close saves a live survey in full and returns to its summary even with no history behind the editor", async () => {
    const saves = stubSaveEndpoint();
    const { queryClient, router } = renderMenuBar(survey({ status: "inProgress" }));

    fireEvent.change(await screen.findByDisplayValue("Feedback"), { target: { value: "Feedback 2026" } });
    fireEvent.click(screen.getByRole("button", { name: "Save & Close" }));

    expect(await screen.findByRole("heading", { name: "Survey summary" })).toBeTruthy();
    expect(router.state.location.pathname).toBe(SUMMARY_PATH);
    expect(saves).toHaveLength(1);
    expect(saves[0]?.path).toBe(`/api/app/surveys/${SURVEY_ID}`);
    expect(saves[0]?.body).toMatchObject({ name: "Feedback 2026", status: "inProgress" });
    expect(queryClient.getQueryData<TSurvey>(surveyEditorKeys.survey(SURVEY_ID))?.name).toBe("Feedback 2026");
  });

  test("a refused Save & Close stays in the editor", async () => {
    const saves = stubSaveEndpoint(500);
    const { router } = renderMenuBar(survey({ status: "inProgress" }));

    fireEvent.click(await screen.findByRole("button", { name: "Save & Close" }));

    await waitFor(() => expect(saves).toHaveLength(1));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Save & Close" })).toHaveProperty("disabled", false)
    );
    expect(router.state.location.pathname).toBe(EDITOR_PATH);
  });

  test("autosave writes the saved draft into the editor's cached survey, so the editor reopens on it", async () => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
    const saves = stubSaveEndpoint();
    const { queryClient, router } = renderMenuBar(survey());

    fireEvent.change(await screen.findByDisplayValue("Feedback"), { target: { value: "Feedback 2026" } });
    await act(async () => {
      vi.advanceTimersByTime(10_000);
    });

    expect(await screen.findByText("Progress saved")).toBeTruthy();
    expect(saves).toHaveLength(1);
    expect(saves[0]?.path).toBe(`/api/app/surveys/${SURVEY_ID}/draft`);
    expect(saves[0]?.body).toMatchObject({ name: "Feedback 2026", updatedAt: LOADED_AT.toISOString() });
    const cached = queryClient.getQueryData<TSurvey>(surveyEditorKeys.survey(SURVEY_ID));
    expect(cached?.name).toBe("Feedback 2026");
    expect(cached?.updatedAt).toEqual(new Date(SAVED_AT));
    // Autosave stays in the editor.
    expect(router.state.location.pathname).toBe(EDITOR_PATH);
  });

  test("autosave saves nothing while the draft has no unsaved change", async () => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
    const saves = stubSaveEndpoint();
    const { queryClient } = renderMenuBar(survey());

    await screen.findByDisplayValue("Feedback");
    await act(async () => {
      vi.advanceTimersByTime(10_000);
    });

    expect(saves).toHaveLength(0);
    expect(queryClient.getQueryData<TSurvey>(surveyEditorKeys.survey(SURVEY_ID))?.updatedAt).toEqual(
      LOADED_AT
    );
  });

  test.each([
    [false, true],
    [true, false],
  ])("with file storage configured %s, the storage warning shows: %s", async (storageConfigured, shown) => {
    stubSaveEndpoint();
    const { queryClient } = renderMenuBar(survey());
    await screen.findByDisplayValue("Feedback");
    act(() => {
      queryClient.setQueryData(instanceKeys.info, { storageConfigured, version: "0.2.10" });
    });
    await waitFor(() =>
      expect(screen.queryByText(i18n.t("common.storage_not_configured")) !== null).toBe(shown)
    );
  });
});
