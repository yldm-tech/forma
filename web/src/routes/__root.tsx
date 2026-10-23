import type { QueryClient } from "@tanstack/react-query";
import { Outlet, createRootRouteWithContext, useRouterState } from "@tanstack/react-router";
import { useCallback, useEffect, useSyncExternalStore } from "react";
import { useTranslation } from "react-i18next";
import { NotFound, RouteError } from "../components/shell/route-states";
import { Toaster } from "../components/ui/toaster";
import { documentTitle } from "../lib/page-titles";

export interface RouterContext {
  queryClient: QueryClient;
}

export const Route = createRootRouteWithContext<RouterContext>()({
  component: RootLayout,
  notFoundComponent: NotFound,
  errorComponent: RouteError,
});

function RootLayout() {
  return (
    <div className="min-h-screen bg-slate-50 text-slate-900">
      <DocumentTitle />
      <Outlet />
      <Toaster />
    </div>
  );
}

/** Keeps the tab title on the current page's, from the central map (lib/page-titles.ts). A title made from loaded data follows the query cache, so it appears once the data does. */
function DocumentTitle() {
  const { t } = useTranslation();
  const { queryClient } = Route.useRouteContext();
  // The matches array keeps its identity until the matched routes change.
  const matches = useRouterState({ select: (state) => state.matches });
  const subscribe = useCallback(
    (onChange: () => void) => queryClient.getQueryCache().subscribe(onChange),
    [queryClient]
  );
  const title = useSyncExternalStore(subscribe, () =>
    documentTitle(
      matches.map((match) => ({ routeId: match.routeId, params: match.params as Record<string, string> })),
      { t, queryClient }
    )
  );
  useEffect(() => {
    document.title = title;
  }, [title]);
  return null;
}
