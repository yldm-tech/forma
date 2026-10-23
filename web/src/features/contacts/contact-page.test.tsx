import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurvey } from "@forma/types/surveys/types";
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
import type { i18n as I18n } from "i18next";
import { Suspense } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import { toast } from "../../lib/toast";
import { analysisSurveyQuery } from "../survey-analysis/api";
import { ContactPage } from "./contact-page";

const WS = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f";
const CONTACT = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d10";
const VIEWED = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d11";
const ANSWERED = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d12";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  // jsdom has no modal dialogs; showing one is setting its open attribute.
  HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
    this.removeAttribute("open");
  };
  window.scrollTo = () => {};
});

afterEach(() => {
  cleanup();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  vi.unstubAllGlobals();
});

const answeredSurvey = {
  id: ANSWERED,
  createdAt: new Date("2026-09-01T00:00:00.000Z"),
  updatedAt: new Date("2026-09-01T00:00:00.000Z"),
  name: "Onboarding",
  type: "link",
  workspaceId: WS,
  status: "inProgress",
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
  endings: [],
  hiddenFields: { enabled: true, fieldIds: [] },
  variables: [],
  followUps: [],
  languages: [],
  isVerifyEmailEnabled: false,
} as unknown as TSurvey;

const activity = {
  responses: [
    {
      id: "r1",
      createdAt: "2026-10-02T10:00:00.000Z",
      updatedAt: "2026-10-02T10:00:00.000Z",
      surveyId: ANSWERED,
      displayId: null,
      contact: { id: CONTACT, userId: "U1" },
      contactAttributes: null,
      finished: true,
      endingId: null,
      data: { q1: "Smooth sailing" },
      variables: {},
      ttc: {},
      tags: [],
      meta: {},
      singleUseId: null,
      language: null,
      quotas: [],
    },
  ],
  displays: [
    { id: "d1", createdAt: "2026-10-01T10:00:00.000Z", surveyId: VIEWED },
    // A display of a survey that is gone is left out of the timeline.
    { id: "d2", createdAt: "2026-10-03T10:00:00.000Z", surveyId: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9dff" },
  ],
  surveys: [
    { id: VIEWED, name: "Pricing poll" },
    { id: ANSWERED, name: "Onboarding" },
  ],
  responseCount: 1,
  displayCount: 2,
};

/** The Go server: the contact, its activity, the viewer, and a personal link for any survey. */
const stubServer = (contactActivity: unknown = activity) => {
  const links: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });
      switch (path) {
        case `/api/app/workspaces/${WS}/contacts/${CONTACT}`:
          return json({
            contact: { id: CONTACT, createdAt: "", updatedAt: "", workspaceId: WS, attributes: {} },
            attributes: [
              { key: "email", name: "Email", type: "default", value: "ada@example.com", dataType: "string" },
            ],
          });
        case `/api/app/contacts/${CONTACT}/activity`:
          return json(contactActivity);
        case "/api/app/auth/me":
          return json({ id: "u1", email: "me@example.com", locale: "en-US" });
        case `/api/app/workspaces/${WS}/contact-attribute-keys`:
          return json([]);
        case `/api/v3/tags?workspaceId=${WS}`:
          return json({ data: [] });
        case `/api/app/workspaces/${WS}/published-link-surveys`:
          return json([{ id: ANSWERED, name: "Onboarding" }]);
        case `/api/app/contacts/${CONTACT}/personal-link`:
          links.push(JSON.parse(init.body as string));
          return json({ surveyUrl: "https://surveys.example.com/c/token" });
      }
      return new Response(JSON.stringify({ code: "not_found", message: path }), {
        status: 404,
        headers: { "content-type": "application/json" },
      });
    })
  );
  return links;
};

const renderPage = (isReadOnly = false) => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  queryClient.setQueryData(analysisSurveyQuery(ANSWERED).queryKey, answeredSurvey);
  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/",
      component: () => (
        <Suspense fallback={null}>
          <ContactPage workspaceId={WS} contactId={CONTACT} isReadOnly={isReadOnly} />
        </Suspense>
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

const counts = () => {
  const term = (label: string) =>
    screen.getAllByText(label).find((el) => el.tagName === "DT")?.nextElementSibling?.textContent;
  return {
    responses: term(i18n.t("common.responses")),
    displays: term(i18n.t("workspace.contacts.displays")),
  };
};

test("the attributes list how many responses and displays the contact has", async () => {
  stubServer();
  renderPage();
  await screen.findAllByText("ada@example.com");
  expect(counts()).toEqual({ responses: "1", displays: "2" });
});

test("the activity lists the contact's responses and displays newest first, and can be reversed", async () => {
  stubServer();
  renderPage();
  const viewed = await screen.findByText(i18n.t("workspace.contacts.survey_viewed"));
  const answered = screen.getByText(i18n.t("workspace.contacts.survey_response_created"));
  // The response (2 October) comes before the display (1 October); the display of a deleted survey is not shown.
  expect(answered.compareDocumentPosition(viewed) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  expect(screen.getAllByText(i18n.t("workspace.contacts.survey_viewed"))).toHaveLength(1);
  expect(screen.getByRole("link", { name: "Pricing poll" }).getAttribute("href")).toBe(
    `/workspaces/${WS}/surveys/${VIEWED}/summary`
  );
  // The response is the survey analysis' response card.
  expect(screen.getByText("Smooth sailing")).toBeTruthy();

  fireEvent.click(screen.getByRole("button", { name: i18n.t("common.activity") }));
  const reversedViewed = screen.getByText(i18n.t("workspace.contacts.survey_viewed"));
  const reversedAnswered = screen.getByText(i18n.t("workspace.contacts.survey_response_created"));
  expect(
    reversedViewed.compareDocumentPosition(reversedAnswered) & Node.DOCUMENT_POSITION_FOLLOWING
  ).toBeTruthy();
});

test("a contact without activity says so", async () => {
  stubServer({ responses: [], displays: [], surveys: [], responseCount: 0, displayCount: 0 });
  renderPage();
  expect(await screen.findByText(i18n.t("workspace.contacts.no_activity_yet"))).toBeTruthy();
  expect(counts()).toEqual({ responses: "0", displays: "0" });
});

test("a writer generates a personal link from the control bar", async () => {
  const links = stubServer();
  vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText: vi.fn(async () => {}) } });
  renderPage();
  fireEvent.click(
    await screen.findByRole("button", { name: i18n.t("workspace.contacts.generate_personal_link") })
  );
  const dialog = await screen.findByRole("dialog");
  const select = within(dialog).getByLabelText(i18n.t("common.select_survey"));
  await waitFor(() => expect(within(select).getByRole("option", { name: "Onboarding" })).toBeTruthy());
  fireEvent.change(select, { target: { value: ANSWERED } });
  fireEvent.click(within(dialog).getByRole("button", { name: i18n.t("common.generate") }));

  await waitFor(() => expect(links).toEqual([{ surveyId: ANSWERED }]));
  expect(
    await within(dialog).findByDisplayValue("https://surveys.example.com/c/token", undefined, {
      timeout: 2000,
    })
  ).toBeTruthy();
});

test("a reader is offered no personal link", async () => {
  stubServer();
  renderPage(true);
  await screen.findAllByText("ada@example.com");
  expect(
    screen.queryByRole("button", { name: i18n.t("workspace.contacts.generate_personal_link") })
  ).toBeNull();
});
