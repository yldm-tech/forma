import type { QueryClient } from "@tanstack/react-query";
import { Outlet, createRootRouteWithContext } from "@tanstack/react-router";
import { NotFound, RouteError } from "../components/shell/route-states";
import { Toaster } from "../components/ui/toaster";

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
      <Outlet />
      <Toaster />
    </div>
  );
}
