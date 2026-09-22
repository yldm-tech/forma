import { checkDatabaseHealth } from "@/modules/api/v2/health/lib/health-checks";

// Readiness is a different question from liveness: /health answers "is this process serving HTTP", which
// must stay true during a dependency outage so kubelet does not restart every pod, while this route answers
// "can this pod serve a request that needs its data" and is allowed to say no. Deliberately Postgres only —
// Redis backs the cache and the job queue, and draining every replica out of the Service on a Redis blip
// costs more than it saves. SpiceDB stays out for the same reason, as templates/NOTES.txt already states.
export const dynamic = "force-dynamic";

export const GET = async (): Promise<Response> => {
  const databaseResult = await checkDatabaseHealth();

  return Response.json(
    { status: databaseResult.ok ? "ok" : "error", main_database: databaseResult.ok },
    {
      status: databaseResult.ok ? 200 : 503,
      headers: { "Cache-Control": "no-store" },
    }
  );
};
