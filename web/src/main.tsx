import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createRouter } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { I18nextProvider } from "react-i18next";
import "./index.css";
import { initI18n, matchLocale } from "./lib/i18n";
import { createQueryClient } from "./lib/query-client";
import { routeTree } from "./routeTree.gen";

const queryClient = createQueryClient();

const router = createRouter({
  routeTree,
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

// The catalogue loads before the first render so no screen ever shows raw keys. The browser's languages pick it until the signed-in user's saved locale is known (see changeLocale).
const bootstrap = async () => {
  const i18n = await initI18n(matchLocale(navigator.languages));
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
