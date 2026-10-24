import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createRouter } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { I18nextProvider } from "react-i18next";
import "./index.css";
import { APP_BASE_PATH } from "./lib/base-path";
import { initI18n } from "./lib/i18n";
import { initialLocale } from "./lib/initial-locale";
import { createQueryClient } from "./lib/query-client";
import { initSentry } from "./lib/telemetry/sentry";
import { routeTree } from "./routeTree.gen";

const queryClient = createQueryClient();

const router = createRouter({
  routeTree,
  // Routes, links and redirects are written without the base; the router adds it to every URL and strips it from the address bar.
  basepath: APP_BASE_PATH,
  context: { queryClient },
  defaultPreload: "intent",
  // Route loaders read through TanStack Query, which owns caching; the router must not cache a second copy.
  defaultPreloadStaleTime: 0,
  scrollRestoration: true,
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("#root element missing from index.html");

// The catalogue loads before the first render so no screen ever shows raw keys. A ?lang= hint in the address, then the browser's languages, pick it until the signed-in user's saved locale is known (see changeLocale). Sentry, where the server configured it, starts alongside, so errors thrown while rendering are reported.
const bootstrap = async () => {
  const [i18n] = await Promise.all([initI18n(initialLocale(window.location.search, navigator.languages)), initSentry()]);
  createRoot(rootElement).render(
    <StrictMode>
      <I18nextProvider i18n={i18n}>
        <QueryClientProvider client={queryClient}>
          <RouterProvider router={router} />
        </QueryClientProvider>
      </I18nextProvider>
    </StrictMode>
  );
};

void bootstrap();
