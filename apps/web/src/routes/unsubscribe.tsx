import { createFileRoute } from "@tanstack/react-router";
import { TokenUnsubscribePage } from "../features/account/unsubscribe";
import { tokenSearch } from "../features/auth/search";

// Where a notification email's signed unsubscribe link lands, /unsubscribe?token=<token>: open to whoever holds the email, signed in or not, on any screen. The token is a query parameter so telemetry masks it (SENSITIVE_QUERY_PARAMS).
export const Route = createFileRoute("/unsubscribe")({
  validateSearch: tokenSearch,
  component: Unsubscribe,
});

function Unsubscribe() {
  const { token = "" } = Route.useSearch();
  return <TokenUnsubscribePage token={token} />;
}
