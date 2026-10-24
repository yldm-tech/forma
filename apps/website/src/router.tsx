import {
  type RouteComponent,
  Outlet,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  useLocation,
} from "@tanstack/react-router";
import { Document, type DocumentAssets } from "./components/Document";
import { HomePage } from "./pages/Home";
import {
  DevelopersPage,
  IntegrationsPage,
  NotFoundPage,
  SecurityPage,
  SelfHostingPage,
} from "./pages/MorePages";
import { FormBuilderPage, InAppSurveyPage, WebsiteSurveyPage } from "./pages/SurveyPages";
import { type PageId, ROUTES, localeOfPath } from "./site";

// The site's routes as a TanStack Router tree: one route per page and locale, all generated from ROUTES. The router runs only at build time (src/prerender), on a memory history, and every page ships as static HTML with no hydration; the browser gets client/main.ts and nothing of React or the router.

const PAGE_COMPONENTS: Record<PageId, RouteComponent> = {
  home: HomePage,
  formBuilder: FormBuilderPage,
  websiteSurvey: WebsiteSurveyPage,
  inAppSurvey: InAppSurveyPage,
  integrations: IntegrationsPage,
  developers: DevelopersPage,
  selfHosting: SelfHostingPage,
  security: SecurityPage,
};

/** The root: the whole HTML document, with the matched page (or the 404 page) in <main>. */
const Root = ({ assets }: { assets: DocumentAssets }) => {
  const { pathname } = useLocation();
  // Looked up by the exact path rather than read from the router's matches: an unknown path such as /ja/nope still matches its closest route (/ja), whose match reports success while the not-found component renders in its place.
  const route = ROUTES.find((candidate) => candidate.path === pathname);
  const page = route?.page;
  const locale = route?.locale ?? localeOfPath(pathname);
  return (
    <Document locale={locale} page={page} assets={assets}>
      <Outlet />
    </Document>
  );
};

/** A router positioned at `path`. Paths outside ROUTES render the 404 page in the locale of their prefix. */
export const createSiteRouter = (path: string, assets: DocumentAssets) => {
  const rootRoute = createRootRoute({
    component: () => <Root assets={assets} />,
    notFoundComponent: NotFoundPage,
  });
  const pageRoutes = ROUTES.map((route) =>
    createRoute({
      getParentRoute: () => rootRoute,
      path: route.path === "/" ? "/" : route.path.replace(/\/$/, ""),
      component: PAGE_COMPONENTS[route.page],
    })
  );
  return createRouter({
    routeTree: rootRoute.addChildren(pageRoutes),
    history: createMemoryHistory({ initialEntries: [path] }),
    trailingSlash: "always",
    isServer: true,
  });
};
