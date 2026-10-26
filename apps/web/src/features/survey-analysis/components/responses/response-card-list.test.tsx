import type { TResponseWithQuotas } from "@forma/types/responses";
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
import type { ComponentProps } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../../lib/i18n";
import { ResponseCardList } from "./response-card-list";

// The responses on a phone: the cards the table opens, stacked, read-only.

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => cleanup());

const survey = {
  id: "s1",
  workspaceId: "ws1",
  blocks: [
    {
      id: "b1",
      name: "Block 1",
      elements: [
        {
          id: "q1",
          type: "openText",
          headline: { default: "What could we do better?" },
          required: false,
          inputType: "text",
          charLimit: { enabled: false },
        },
      ],
    },
  ],
  welcomeCard: { enabled: false },
  isVerifyEmailEnabled: false,
  hiddenFields: { enabled: true, fieldIds: [] },
  variables: [],
  languages: [],
} as unknown as TSurvey;

const response = (id: string, answer: string) =>
  ({
    id,
    createdAt: new Date("2026-10-25T00:00:00Z"),
    updatedAt: new Date("2026-10-25T00:00:00Z"),
    surveyId: survey.id,
    contact: null,
    contactAttributes: null,
    finished: true,
    data: { q1: answer },
    variables: {},
    ttc: {},
    tags: [],
    meta: {},
    singleUseId: null,
    language: null,
    displayId: null,
    endingId: null,
    quotas: [],
  }) as unknown as TResponseWithQuotas;

type ListProps = ComponentProps<typeof ResponseCardList>;

const renderList = async (props: Partial<ListProps>) => {
  const list = () => (
    <ResponseCardList
      survey={survey}
      responses={[]}
      environmentTags={[]}
      isReadOnly
      locale="en-US"
      isFetchingFirstPage={false}
      hasMore={false}
      isFetchingMore={false}
      fetchNextPage={() => {}}
      countSummary={<p>2 of 2</p>}
      emptyState={<p>No responses yet</p>}
      onResponseChanged={() => {}}
      onResponsesDeleted={() => {}}
      {...props}
    />
  );
  const rootRoute = createRootRoute({ component: Outlet });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/", component: list }),
    ]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  await waitFor(() => expect(router.state.status).toBe("idle"));
};

test("while the first page loads, placeholder cards stand in", async () => {
  await renderList({ isFetchingFirstPage: true });
  expect(screen.getByRole("status", { name: i18n.t("common.loading") }).getAttribute("aria-busy")).toBe(
    "true"
  );
  expect(screen.queryByRole("list")).toBeNull();
  expect(screen.queryByText("No responses yet")).toBeNull();
});

test("with nothing to show, the page's empty state", async () => {
  await renderList({});
  expect(screen.getByText("No responses yet")).toBeTruthy();
  expect(screen.queryByText("2 of 2")).toBeNull();
});

test("each response is a card with its answers, under the count, and read-only cards offer no delete", async () => {
  await renderList({ responses: [response("r1", "Faster replies"), response("r2", "Nothing")] });
  const list = screen.getByRole("list", { name: i18n.t("common.responses") });
  const cards = within(list).getAllByRole("listitem");
  expect(cards).toHaveLength(2);
  expect(within(cards[0]!).getByText("What could we do better?")).toBeTruthy();
  expect(within(cards[0]!).getByText("Faster replies")).toBeTruthy();
  expect(within(cards[1]!).getByText("Nothing")).toBeTruthy();
  expect(screen.getByText("2 of 2")).toBeTruthy();
  expect(
    screen.queryByRole("button", { name: i18n.t("workspace.surveys.responses.delete_response") })
  ).toBeNull();
  expect(screen.queryByRole("button", { name: i18n.t("common.load_more") })).toBeNull();
});

test("Load more asks for the next page while there is one", async () => {
  const fetchNextPage = vi.fn();
  await renderList({ responses: [response("r1", "Faster replies")], hasMore: true, fetchNextPage });
  fireEvent.click(screen.getByRole("button", { name: i18n.t("common.load_more") }));
  expect(fetchNextPage).toHaveBeenCalledTimes(1);
});
