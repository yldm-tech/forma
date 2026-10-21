import { queryOptions, useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { getJSON } from "../lib/api";

interface Readiness {
  status: "ok" | "error";
  main_database: boolean;
}

const readinessQuery = queryOptions({
  queryKey: ["health", "ready"],
  queryFn: ({ signal }) => getJSON<Readiness>("/health/ready", signal),
  retry: false,
});

export const Route = createFileRoute("/")({
  component: Home,
});

// The admin UI arrives module by module (see the migration plan); until sign-in lands, the root page reports whether this deployment can reach its database.
function Home() {
  const readiness = useQuery(readinessQuery);
  const label = readiness.isPending
    ? "Checking server…"
    : readiness.isSuccess
      ? "Server and database are reachable"
      : "Server is not ready";

  return (
    <main className="mx-auto max-w-xl p-8">
      <h1 className="text-2xl font-semibold">Forma</h1>
      <p className="mt-4 text-sm text-slate-600" role="status">
        {label}
      </p>
    </main>
  );
}
