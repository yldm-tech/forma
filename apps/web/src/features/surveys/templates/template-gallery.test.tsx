import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { SurveyContainerProps } from "@forma/types/forma-surveys";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { type WorkspaceDetail, workspaceQuery } from "../../../lib/queries/workspaces";
import { type SurveyTemplate, surveyTemplatesInLanguageQuery, surveyTemplatesQuery } from "../api";
import {
  type OrganizationTemplate,
  type OrganizationTemplateList,
  organizationTemplateQuery,
  organizationTemplatesQuery,
} from "./organization-templates";
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

/** Stubs the viewport as wide (from the lg breakpoint) or not; `resize` crosses the breakpoint as a window resize would. */
const stubScreen = (wide: boolean) => {
  let isWide = wide;
  const listeners = new Set<() => void>();
  vi.stubGlobal("matchMedia", (query: string) => ({
    get matches() {
      return isWide && query === "(min-width: 1024px)";
    },
    media: query,
    addEventListener: (_type: string, listener: () => void) => listeners.add(listener),
    removeEventListener: (_type: string, listener: () => void) => listeners.delete(listener),
  }));
  return {
    resize: (nextWide: boolean) =>
      act(() => {
        isWide = nextWide;
        for (const listener of listeners) listener();
      }),
  };
};

const renderGallery = (
  surveyType: "link" | "app" = "link",
  {
    defaultLanguage = "en-US",
    templatesInSurveyLanguage = templates,
    ownTemplates = { templates: [], canManage: false },
  }: {
    defaultLanguage?: string;
    templatesInSurveyLanguage?: SurveyTemplate[];
    ownTemplates?: OrganizationTemplateList;
  } = {}
) => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  queryClient.setQueryData(surveyTemplatesQuery(WS, i18n.language).queryKey, {
    templates,
    xmTemplateIds: ["nps"],
  });
  queryClient.setQueryData(surveyTemplatesInLanguageQuery(WS, defaultLanguage).queryKey, {
    templates: templatesInSurveyLanguage,
    xmTemplateIds: ["nps"],
  });
  queryClient.setQueryData(workspaceQuery(WS).queryKey, workspace);
  queryClient.setQueryData(organizationTemplatesQuery(ORG).queryKey, ownTemplates);
  for (const template of ownTemplates.templates) {
    queryClient.setQueryData(organizationTemplateQuery(template.id).queryKey, {
      ...template,
      preset: ownPreset,
    });
  }
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
            defaultLanguage={defaultLanguage}
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
  test("previews the template in the survey's default language, which the created survey uses, not the dashboard's", async () => {
    stubScreen(true);
    const renderSurvey = stubRuntime();
    const jaBlock = {
      ...npsBlock,
      elements: [
        {
          ...npsBlock.elements[0],
          headline: { default: "Mainを友人に勧める可能性はどのくらいありますか？" },
        },
      ],
    };
    const jaTemplates = templates.map((template): SurveyTemplate =>
      template.id === "nps"
        ? {
            ...template,
            name: "ネットプロモータースコア (NPS)",
            preset: { ...template.preset, blocks: [jaBlock] } as SurveyTemplate["preset"],
          }
        : template
    );
    renderGallery("link", { defaultLanguage: "ja-JP", templatesInSurveyLanguage: jaTemplates });

    // The cards stay in the dashboard's language.
    await chooseTemplate("Net Promoter Score (NPS)");

    await waitFor(() => expect(renderSurvey).toHaveBeenCalled());
    expect(renderedSurvey(renderSurvey)?.survey.blocks).toEqual([jaBlock]);
  });

  test("a preview dialog closed by widening the window does not reopen when it narrows again", async () => {
    const screenSize = stubScreen(false);
    stubRuntime();
    renderGallery();

    await chooseTemplate("Net Promoter Score (NPS)");
    fireEvent.click(screen.getByRole("button", { name: i18n.t("common.preview") }));
    await screen.findByRole("dialog", { name: "Net Promoter Score (NPS)" });

    // The pane takes the preview over.
    screenSize.resize(true);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(
      screen.getByRole("complementary", { name: i18n.t("workspace.surveys.templates.template_preview") })
    ).toBeTruthy();

    screenSize.resize(false);
    expect(screen.queryByRole("complementary")).toBeNull();
    expect(screen.queryByRole("dialog")).toBeNull();
    // The card's Preview button still opens it.
    fireEvent.click(screen.getByRole("button", { name: i18n.t("common.preview") }));
    expect(await screen.findByRole("dialog", { name: "Net Promoter Score (NPS)" })).toBeTruthy();
  });
});

const OWN_TEMPLATE_ID = "0190a000-0000-7000-8000-000000000010";

const ownBlock = {
  id: "blk-own",
  name: "Block 1",
  elements: [
    {
      id: "el-own",
      type: "openText",
      headline: { default: "What shipped well this release?", "ja-JP": "今回のリリースで良かった点は？" },
      required: true,
    },
  ],
};

const ownPreset = {
  welcomeCard: { enabled: false, timeToFinish: false, showResponseCount: false },
  blocks: [ownBlock],
  endings: [{ id: "end-own", type: "endScreen", headline: { default: "Thanks, team" } }],
  hiddenFields: { enabled: true, fieldIds: [] },
  variables: [],
  metadata: {},
  styling: { brandColor: { light: "#123456" } },
  surveyClosedMessage: null,
} as unknown as OrganizationTemplate["preset"];

const ownTemplate: OrganizationTemplate = {
  id: OWN_TEMPLATE_ID,
  createdAt: "2026-10-20T00:00:00Z",
  updatedAt: "2026-10-20T00:00:00Z",
  organizationId: ORG,
  name: "Release retro",
  description: "What we ask after every release.",
  creator: { name: "Ada" },
  questionCount: 1,
  languages: [
    { code: "en-US", alias: null, default: true, enabled: true },
    { code: "ja-JP", alias: null, default: false, enabled: true },
  ],
};

describe("TemplateGallery organization templates", () => {
  test("lists the organization's templates above the catalog, previews one as saved and creates a survey from it", async () => {
    stubScreen(true);
    const renderSurvey = stubRuntime();
    const posts: { path: string; body: unknown }[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (path: string, init: RequestInit = {}) => {
        if (init.method !== "POST") throw new Error(`unexpected ${init.method ?? "GET"} ${path}`);
        posts.push({ path, body: JSON.parse(init.body as string) });
        return Response.json({ id: "0190a000-0000-7000-8000-000000000011" }, { status: 201 });
      })
    );
    renderGallery("app", { ownTemplates: { templates: [ownTemplate], canManage: false } });

    const headings = (await screen.findAllByRole("heading", { level: 2 })).map((h) => h.textContent);
    expect(headings).toEqual([
      i18n.t("workspace.surveys.organization_templates.section_title"),
      i18n.t("workspace.surveys.organization_templates.catalog_title"),
    ]);
    expect(
      screen.getByText(i18n.t("workspace.surveys.organization_templates.saved_by", { name: "Ada" }))
    ).toBeTruthy();
    expect(
      screen.getByText(i18n.t("workspace.surveys.organization_templates.language_count", { count: 2 }))
    ).toBeTruthy();

    await chooseTemplate("Release retro");
    const pane = screen.getByRole("complementary", {
      name: i18n.t("workspace.surveys.templates.template_preview"),
    });
    expect(within(pane).getByRole("heading", { name: "Release retro" })).toBeTruthy();
    await waitFor(() => expect(renderSurvey).toHaveBeenCalled());
    const props = renderedSurvey(renderSurvey);
    expect(props?.survey.blocks).toEqual([ownBlock]);
    expect(props?.survey.type).toBe("app");
    expect(props?.survey.styling).toEqual({ brandColor: { light: "#123456" } });
    // Using it is open to whoever may create surveys here; managing it is not.
    expect(
      screen.queryByRole("button", {
        name: i18n.t("workspace.surveys.organization_templates.delete_template_named", {
          name: "Release retro",
        }),
      })
    ).toBeNull();

    fireEvent.click(
      within(pane).getByRole("button", { name: i18n.t("workspace.surveys.templates.use_this_template") })
    );
    await waitFor(() =>
      expect(posts).toEqual([
        {
          path: `/api/app/survey-templates/${OWN_TEMPLATE_ID}/surveys`,
          body: { workspaceId: WS, surveyType: "app" },
        },
      ])
    );
  });

  test("a filter leaves them out, a search finds them by name, and an owner or a manager may edit and delete them", async () => {
    stubScreen(true);
    stubRuntime();
    renderGallery("link", { ownTemplates: { templates: [ownTemplate], canManage: true } });

    expect(await screen.findByRole("button", { name: "Release retro" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: i18n.t("common.customer_success") }));
    expect(screen.queryByRole("button", { name: "Release retro" })).toBeNull();
    expect(screen.queryByRole("heading", { level: 2 })).toBeNull();

    fireEvent.change(screen.getByRole("searchbox", { name: i18n.t("common.search") }), {
      target: { value: "retro" },
    });
    expect(screen.getByRole("button", { name: "Release retro" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Net Promoter Score (NPS)" })).toBeNull();

    await chooseTemplate("Release retro");
    fireEvent.click(
      screen.getByRole("button", {
        name: i18n.t("workspace.surveys.organization_templates.delete_template_named", {
          name: "Release retro",
        }),
      })
    );
    const dialog = await screen.findByRole("alertdialog");
    expect(
      within(dialog).getByText(i18n.t("workspace.surveys.organization_templates.delete_template_description"))
    ).toBeTruthy();
    expect(
      screen.getByRole("button", {
        name: i18n.t("workspace.surveys.organization_templates.edit_template_named", {
          name: "Release retro",
        }),
      })
    ).toBeTruthy();
  });
});
