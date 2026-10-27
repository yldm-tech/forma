import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createMemoryHistory, createRouter } from "@tanstack/react-router";
import { cleanup, render, screen } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../../../../lib/i18n";
import { routeTree } from "../../../../../../routeTree.gen";

// The editor route offers spam protection exactly when the instance has reCAPTCHA keys, which GET /api/app/auth/options reports; it used to pass false on every instance. The editor itself (a module graph that takes half a minute to load in jsdom) is replaced by one that prints the prop, since what is under test is what the route hands it.
vi.mock("../../../../../../features/survey-editor/components/survey-editor", () => ({
  SurveyEditor: ({ isSpamProtectionAllowed }: { isSpamProtectionAllowed: boolean }) => (
    <output data-testid="spam-protection">{String(isSpamProtectionAllowed)}</output>
  ),
}));

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const ORG = "0190a000-0000-7000-8000-000000000001";
const WS = "0190a000-0000-7000-8000-000000000002";
const SURVEY = "0190a000-0000-7000-8000-000000000003";
const STAMP = "2026-10-27T08:00:00Z";

const language = {
  id: "0190a000-0000-7000-8000-000000000004",
  createdAt: STAMP,
  updatedAt: STAMP,
  code: "en-US",
  alias: null,
  workspaceId: WS,
};

const workspace = {
  id: WS,
  createdAt: STAMP,
  updatedAt: STAMP,
  name: "Acme",
  organizationId: ORG,
  styling: { allowStyleOverwrite: true },
  recontactDays: 7,
  inAppSurveyBranding: true,
  linkSurveyBranding: true,
  config: { channel: null, industry: null },
  placement: "bottomRight",
  clickOutsideClose: true,
  overlay: "none",
  languages: [language],
  appSetupCompleted: false,
  logo: null,
  customHeadScripts: null,
  access: { role: "owner", canWrite: true, canManage: true, isReadOnly: false },
  surveyScheduling: { timeZone: "UTC", localHour: 0, localMinute: 0, instanceTimeZone: "UTC" },
};

// The parts of GET /api/app/surveys/{id} the route reads before it renders the editor.
const survey = {
  id: SURVEY,
  createdAt: STAMP,
  updatedAt: STAMP,
  name: "Event registration",
  type: "link",
  workspaceId: WS,
  status: "draft",
  triggers: [],
  segment: null,
  languages: [{ language, default: true, enabled: true }],
  followUps: [],
  publishOn: null,
  closeOn: null,
  archivedAt: null,
  recaptcha: { enabled: false, threshold: 0.1 },
};

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

/** The server the editor route loads from; spamProtectionAvailable is what GET /api/app/auth/options reports. */
const stubServer = (spamProtectionAvailable: boolean) => {
  const answers: Record<string, unknown> = {
    "/api/app/auth/me": {
      id: "u1",
      name: "Ada",
      email: "ada@example.com",
      emailVerified: true,
      locale: "en-US",
      identityProvider: "email",
      hasPassword: true,
      twoFactorEnabled: false,
      createdAt: STAMP,
      lastLoginAt: null,
    },
    "/api/app/auth/options": {
      emailAuthEnabled: true,
      signupEnabled: false,
      passwordResetEnabled: true,
      emailVerificationRequired: true,
      smtpConfigured: true,
      ssoProviders: [],
      isFormaCloud: false,
      spamProtectionAvailable,
    },
    [`/api/app/workspaces/${WS}`]: workspace,
    [`/api/app/surveys/${SURVEY}`]: survey,
    [`/api/app/workspaces/${WS}/action-classes`]: { actionClasses: [] },
    [`/api/app/workspaces/${WS}/contact-attribute-keys`]: [],
    [`/api/app/workspaces/${WS}/segments`]: [],
    [`/api/app/surveys/${SURVEY}/quotas`]: { quotas: [] },
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      if (path in answers) return json(200, answers[path]);
      if (path.startsWith("/api/v3/surveys?")) return json(200, { data: [], meta: { nextCursor: null } });
      throw new Error(`unexpected ${path}`);
    })
  );
};

const renderEditorRoute = () => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createRouter({
    routeTree,
    context: { queryClient },
    history: createMemoryHistory({ initialEntries: [`/workspaces/${WS}/surveys/${SURVEY}/edit`] }),
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
};

test.each([
  { spamProtectionAvailable: true, offered: "true" },
  { spamProtectionAvailable: false, offered: "false" },
])(
  "spam protection is offered: $offered when the server reports spamProtectionAvailable: $spamProtectionAvailable",
  async ({ spamProtectionAvailable, offered }) => {
    stubServer(spamProtectionAvailable);
    renderEditorRoute();
    expect((await screen.findByTestId("spam-protection", {}, { timeout: 10_000 })).textContent).toBe(offered);
  },
  20_000
);
