import { createFileRoute } from "@tanstack/react-router";
import { tokenSearch } from "../features/auth/search";
import { SharedResultsPage } from "../features/survey-analysis/components/shared-results/shared-results-page";

// A survey's read-only results link, /results?token=<key>: open to anyone holding the key, signed in or not. The key is a query parameter so telemetry masks it (SENSITIVE_QUERY_PARAMS).
export const Route = createFileRoute("/results")({
  validateSearch: tokenSearch,
  component: SharedResultsRoute,
});

function SharedResultsRoute() {
  const { token } = Route.useSearch();
  return <SharedResultsPage token={token} />;
}
