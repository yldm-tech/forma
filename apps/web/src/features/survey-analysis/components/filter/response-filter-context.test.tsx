import {
  Link,
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { TSurvey } from "@forma/types/surveys/types";
import { afterEach, describe, expect, test } from "vite-plus/test";
import { ResponseFilterProvider, useResponseFilter } from "./response-filter-context";

const WORKSPACE_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f";
const SURVEY_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e";
const SURVEY_PATH = `/workspaces/${WORKSPACE_ID}/surveys/${SURVEY_ID}`;
const SURVEY_LIST_PATH = `/workspaces/${WORKSPACE_ID}/surveys`;
const EDITOR_PATH = `${SURVEY_PATH}/edit`;

afterEach(() => {
  cleanup();
});

const survey = {
  id: SURVEY_ID,
  workspaceId: WORKSPACE_ID,
  name: "Feedback",
  type: "link",
  status: "inProgress",
  blocks: [],
  questions: [],
  endings: [],
  hiddenFields: { enabled: true, fieldIds: [] },
  variables: [],
  languages: [],
} as unknown as TSurvey;

/** A deferred loader result: the page a navigation leaves stays mounted until it resolves. */
const deferred = () => {
  let resolve: () => void = () => {};
  const promise = new Promise<void>((r) => {
    resolve = r;
  });
  return { promise, resolve };
};

const AnalysisView = ({ name }: { name: string }) => {
  const { setSelectedFilter } = useResponseFilter();
  return (
    <div>
      <h1>{name}</h1>
      <button type="button" onClick={() => setSelectedFilter({ filter: [], responseStatus: "complete" })}>
        Completed only
      </button>
      <Link
        to="/workspaces/$workspaceId/surveys/$surveyId/responses"
        params={{ workspaceId: WORKSPACE_ID, surveyId: SURVEY_ID }}>
        Responses
      </Link>
      <Link to="/workspaces/$workspaceId/surveys" params={{ workspaceId: WORKSPACE_ID }}>
        Survey list
      </Link>
      <Link
        to="/workspaces/$workspaceId/surveys/$surveyId/edit"
        params={{ workspaceId: WORKSPACE_ID, surveyId: SURVEY_ID }}>
        Edit
      </Link>
    </div>
  );
};

/** The provider in the survey's layout route with its two analysis views, and the pages one leaves them for, whose loaders wait until the test lets them finish. */
const renderAnalysis = () => {
  const leaving = deferred();
  const rootRoute = createRootRoute({ component: Outlet });
  const layout = createRoute({
    getParentRoute: () => rootRoute,
    path: "/workspaces/$workspaceId/surveys/$surveyId",
    component: () => (
      <ResponseFilterProvider survey={survey}>
        <Outlet />
      </ResponseFilterProvider>
    ),
  });
  const routeTree = rootRoute.addChildren([
    layout.addChildren([
      createRoute({
        getParentRoute: () => layout,
        path: "summary",
        component: () => <AnalysisView name="Summary" />,
      }),
      createRoute({
        getParentRoute: () => layout,
        path: "responses",
        component: () => <AnalysisView name="Responses" />,
      }),
    ]),
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/workspaces/$workspaceId/surveys",
      loader: () => leaving.promise,
      component: () => <h1>Surveys</h1>,
    }),
    // The editor lives outside the analysis layout, as $workspaceId_ puts it in the app.
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/workspaces/$workspaceId/surveys/$surveyId/edit",
      loader: () => leaving.promise,
      component: () => <h1>Editor</h1>,
    }),
  ]);
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [`${SURVEY_PATH}/summary`] }),
  });
  render(<RouterProvider router={router} />);
  return { router, leaving };
};

describe("ResponseFilterProvider", () => {
  test("keeps the filter in the URL of the analysis views and carries it from the summary to the responses", async () => {
    const { router } = renderAnalysis();

    fireEvent.click(await screen.findByRole("button", { name: "Completed only" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ filter: { s: "complete" } }));

    fireEvent.click(screen.getByRole("link", { name: "Responses" }));
    expect(await screen.findByRole("heading", { name: "Responses" })).toBeTruthy();
    await waitFor(() => expect(router.state.location.search).toEqual({ filter: { s: "complete" } }));
    expect(router.state.location.pathname).toBe(`${SURVEY_PATH}/responses`);
  });

  test.each([
    ["Survey list", "Surveys", SURVEY_LIST_PATH],
    ["Edit", "Editor", EDITOR_PATH],
  ])(
    "leaving for the %s does not take the filter along while the next page loads",
    async (link, heading, path) => {
      const { router, leaving } = renderAnalysis();
      fireEvent.click(await screen.findByRole("button", { name: "Completed only" }));
      await waitFor(() => expect(router.state.location.search).toEqual({ filter: { s: "complete" } }));

      fireEvent.click(screen.getByRole("link", { name: link }));
      // The router already shows the target's location while the analysis view is still mounted.
      await waitFor(() => expect(router.state.location.pathname).toBe(path));
      expect(screen.getByRole("heading", { name: "Summary" })).toBeTruthy();
      // Let the still-mounted provider's effects run against the pending location before the target finishes loading.
      await act(async () => {
        await new Promise((r) => setTimeout(r, 50));
      });
      await act(async () => {
        leaving.resolve();
      });

      expect(await screen.findByRole("heading", { name: heading })).toBeTruthy();
      expect(router.state.location.pathname).toBe(path);
      expect(router.state.location.search).toEqual({});
      expect(router.state.location.href).toBe(path);
    }
  );
});
