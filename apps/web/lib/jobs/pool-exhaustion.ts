import { Prisma } from "@forma/database/prisma";
import { DatabaseError } from "@forma/types/errors";

/**
 * True when an error is a transient database connection-pool exhaustion. These are retryable: a
 * background job that hits one should propagate the error so it is retried, rather than swallow it
 * and silently drop work.
 *
 * Which error that is depends on who owns the pool. Prisma 6 owned it in its Rust engine and
 * reported exhaustion as `P2024`; Prisma 7 with the pg driver adapter does not — `P2024` is not in
 * the client at all any more — and the pool is `pg-pool`, which fails a queued acquisition with a
 * plain `Error('timeout exceeded when trying to connect')` (node_modules/pg-pool/index.js) that the
 * adapter rethrows unchanged. Matching only the Prisma shapes meant every real exhaustion on this
 * version classified as non-retryable, which is the silent-drop this function exists to prevent.
 *
 * The Prisma branches stay: they cost nothing and keep the classification right if the adapter is
 * swapped back.
 *
 * Shared by the response-pipeline job and the workflow runner enqueue so both classify retryable
 * DB exhaustion the same way (and so the runner can rethrow it without importing the pipeline).
 */
export const isDatabasePoolExhaustionError = (error: unknown): boolean => {
  if (error instanceof Prisma.PrismaClientKnownRequestError && error.code === "P2024") {
    return true;
  }

  if (error instanceof DatabaseError || error instanceof Error) {
    return /Timed out fetching a new connection from the connection pool|connection pool timeout|timeout exceeded when trying to connect/i.test(
      error.message
    );
  }

  return false;
};
