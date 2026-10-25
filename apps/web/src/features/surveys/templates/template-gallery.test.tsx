import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { SurveyContainerProps } from "@forma/types/forma-surveys";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { type WorkspaceDetail, workspaceQuery } from "../../../lib/queries/workspaces";
import { type SurveyTemplate, surveyTemplatesQuery } from "../api";
import { TemplateGallery } from "./template-gallery";

// The gallery previews the chosen template with the respondent runtime: beside the cards on a wide screen, in a dialog from the card's Preview button on a narrower one.

const ORG = "0190a000-0000-7000-8000-000000000001";
const WS = "0190a000-0000-7000-8000-000000000002";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  // jsdom has the <dialog> element but not its modal methods, which the preview dialog opens with.
  HTMLDialogElement.prototype.showModal ??= function showModal(this: HTMLDialogElement) {
    this.open = true;
  };
  HTMLDialogElement.prototype.close ??= function close(this: HTMLDialogElement) {
    this.open = false;
  };
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const workspace: WorkspaceDetail = {
  id: WS,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  name: "Main",
  organizationId: ORG,
  styling: { allowStyleOverwrite: true },
  recontactDays: 7,
  inAppSurveyBranding: true,
  linkSurveyBranding: false,
  config: { channel: null, industry: null },
  placement: "bottomRight",
  clickOutsideClose: true,
  overlay: "none",
  languages: [],
  appSetupCompleted: false,
  logo: null,
  customHeadScripts: null,
  access: { role: "owner", canWrite: true, canManage: true, isReadOnly: false },
};

const npsBlock = {
  id: "blk-nps",
  name: "Block 1",
  elements: [
    {
      id: "el-nps",
      type: "nps",
      headline: { default: "How likely are you to recommend Main?" },
      required: true,
      lowerLabel: { default: "Not likely" },
      upperLabel: { default: "Very likely" },
    },
  ],
};

const templates: SurveyTemplate[] = [
  {
    id: "custom",
    name: "Start from scratch",
    description: "Create a survey without a template.",
    preset: { blocks: [] },
  },
  {
    id: "nps",
    name: "Net Promoter Score (NPS)",
    description: "Measure customer loyalty.",
    role: "customerSuccess",
    preset: {
      name: "Net Promoter Score (NPS)",
      welcomeCard: { enabled: false, timeToFinish: false, showResponseCount: false },
      blocks: [npsBlock],
      endings: [{ id: "end-1", type: "endScreen", headline: { default: "Thank you!" } }],
      hiddenFields: { enabled: true, fieldIds: [] },
    } as SurveyTemplate["preset"],
  },
];

/** Stubs the respondent runtime the preview loads, so the test sees what it was asked to render. */
const stubRuntime = () => {
  const renderSurvey = vi.fn<(props: SurveyContainerProps) => void>();
  vi.stubGlobal("formaSurveys", { renderSurvey });
  return renderSurvey;
};

const stubScreen = (wide: boolean) =>
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: wide && query === "(min-width: 1024px)",
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  }));

const renderGallery = (surveyType: "link" | "app" = "link") => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  queryClient.setQueryData(surveyTemplatesQuery(WS, i18n.language).queryKey, {
    templates,
    xmTemplateIds: ["nps"],
  });
  queryClient.setQueryData(workspaceQuery(WS).queryKey, workspace);
  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/",
      component: () => (
        <div style={{ height: 800 }}>
          <TemplateGallery
            workspaceId={WS}
            organizationId={ORG}
            surveyType={surveyType}
            defaultLanguage="en-US"
            isTemplatePage
          />
        </div>
      ),
    }),
  ]);
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/"] }) });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
};

const chooseTemplate = async (name: string) => fireEvent.click(await screen.findByRole("button", { name }));

/** The survey the runtime was last asked to render. */
const renderedSurvey = (renderSurvey: ReturnType<typeof stubRuntime>) => renderSurvey.mock.calls.at(-1)?.[0];

describe("TemplateGallery preview", () => {
  test("on a wide screen, previews the chosen template beside the cards in the runtime's preview mode", async () => {
    stubScreen(true);
    const renderSurvey = stubRuntime();
    renderGallery();

    const pane = await screen.findByRole("complementary", {
      name: i18n.t("workspace.surveys.templates.template_preview"),
    });
    expect(
      within(pane).getByText(i18n.t("workspace.surveys.templates.select_a_template_to_preview"))
    ).toBeTruthy();

    await chooseTemplate("Net Promoter Score (NPS)");

    expect(within(pane).getByRole("heading", { name: "Net Promoter Score (NPS)" })).toBeTruthy();
    expect(
      within(pane).getByRole("button", { name: i18n.t("workspace.surveys.templates.use_this_template") })
    ).toBeTruthy();
    await waitFor(() => expect(renderSurvey).toHaveBeenCalled());
    const props = renderedSurvey(renderSurvey);
    expect(props?.isPreviewMode).toBe(true);
    expect(props?.isRedirectDisabled).toBe(true);
    expect(props?.survey.type).toBe("link");
    expect(props?.survey.blocks).toEqual([npsBlock]);
    expect(props?.survey.endings.map((ending) => ending.id)).toEqual(["end-1"]);
    // The workspace's link branding, as the draft's own preview would show it.
    expect(props?.isBrandingEnabled).toBe(false);
    // The pane already shows the preview, so the card offers no button for it.
    expect(screen.queryByRole("button", { name: i18n.t("common.preview") })).toBeNull();

    // Another template replaces the one previewed.
    await chooseTemplate("Start from scratch");
    expect(within(pane).getByRole("heading", { name: "Start from scratch" })).toBeTruthy();
    expect(within(pane).getByRole("button", { name: i18n.t("common.create_survey") })).toBeTruthy();
    await waitFor(() => expect(renderedSurvey(renderSurvey)?.survey.id).toBe("custom"));
  });

  test("an app survey is previewed in the mock web app with the in-app branding", async () => {
    stubScreen(true);
    const renderSurvey = stubRuntime();
    renderGallery("app");

    await chooseTemplate("Net Promoter Score (NPS)");

    await waitFor(() => expect(renderSurvey).toHaveBeenCalled());
    const props = renderedSurvey(renderSurvey);
    expect(props?.survey.type).toBe("app");
    expect(props?.isBrandingEnabled).toBe(true);
    expect(screen.getByText(i18n.t("workspace.surveys.edit.your_web_app"))).toBeTruthy();
  });

  test("on a narrower screen, the chosen card opens the preview in a dialog that can create the survey", async () => {
    stubScreen(false);
    const renderSurvey = stubRuntime();
    renderGallery();

    expect(screen.queryByRole("complementary")).toBeNull();
    await chooseTemplate("Net Promoter Score (NPS)");
    expect(renderSurvey).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: i18n.t("common.preview") }));

    const dialog = await screen.findByRole("dialog", { name: "Net Promoter Score (NPS)" });
    expect(
      within(dialog).getByRole("button", { name: i18n.t("workspace.surveys.templates.use_this_template") })
    ).toBeTruthy();
    await waitFor(() => expect(renderSurvey).toHaveBeenCalled());
    expect(renderedSurvey(renderSurvey)?.survey.blocks).toEqual([npsBlock]);

    fireEvent.click(within(dialog).getByRole("button", { name: i18n.t("common.close") }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });
});
