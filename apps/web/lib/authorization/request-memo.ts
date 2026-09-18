import "server-only";
import { cache as reactCache } from "react";
import type { TAuthorizationAction, TAuthorizationActor, TAuthorizationResource } from "./contract";

/**
 * Memoizes an authorization decision for the life of one request.
 *
 * A single document load asks SpiceDB the same question more than once — the workspace layout
 * resolves `workspace.read` and then `resolveWorkspaceAuth` asks for it again a few frames later —
 * and each repeat is a gRPC round trip the answer for which cannot have changed.
 *
 * **Staleness, explicitly.** Inside one request a `can()` that follows a permission change returns
 * the answer from before it. That is not a new failure mode: `AUTHZED_CONSISTENCY` is
 * `minimize_latency` on this deployment, so SpiceDB may already serve a read-after-write from an
 * older revision. What changes is that the staleness becomes deterministic within a request instead
 * of depending on replication timing. Code that grants access and then re-checks it in the same
 * request must not rely on either behaviour; it should act on the grant it just made.
 *
 * The counter that feeds the N+1 histogram deliberately stays *outside* this, in `can()`. The metric
 * answers "how many decisions did this page attempt", and a page that attempts the same one twenty
 * times still has an N+1 problem worth seeing even when nineteen of them are free.
 *
 * `reactCache` is used for its request scope, the same way `context.ts` uses it, and the wrapper is
 * pinned to `globalThis` so duplicated Next.js server bundles resolve the same store. Outside a
 * request scope — scripts, unit tests, the non-RSC bundle — `cache()` returns a fresh object every
 * call, so this degrades to no memoization rather than to a process-wide cache that would leak
 * decisions between users.
 */

type TDecisionStore = Map<string, Promise<boolean>>;

const globalForAuthorizationMemo = globalThis as unknown as {
  formaAuthorizationDecisionStore?: () => TDecisionStore;
};

const getDecisionStore =
  globalForAuthorizationMemo.formaAuthorizationDecisionStore ?? reactCache((): TDecisionStore => new Map());

globalForAuthorizationMemo.formaAuthorizationDecisionStore = getDecisionStore;

const decisionKey = (
  actor: TAuthorizationActor,
  action: TAuthorizationAction,
  resource: TAuthorizationResource
): string => `${actor.type}:${actor.id}|${action}|${resource.type}:${resource.id}`;

/**
 * Runs `evaluate` unless this exact question was already asked in this request.
 *
 * The promise is stored rather than the resolved value, so two callers that ask concurrently — which
 * is the common case, since a layout and its page render together — share one round trip instead of
 * racing to start two. A rejection is not cached: an operational failure is not a decision, and the
 * next caller should be free to try again.
 */
export const memoizeAuthorizationDecision = (
  actor: TAuthorizationActor,
  action: TAuthorizationAction,
  resource: TAuthorizationResource,
  evaluate: () => Promise<boolean>
): Promise<boolean> => {
  const store = getDecisionStore();
  const key = decisionKey(actor, action, resource);

  const cached = store.get(key);
  if (cached) return cached;

  const pending = evaluate().catch((error: unknown) => {
    store.delete(key);
    throw error;
  });
  store.set(key, pending);
  return pending;
};
