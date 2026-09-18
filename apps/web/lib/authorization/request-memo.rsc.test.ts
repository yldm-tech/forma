import * as React from "react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import type { TAuthorizationActor, TAuthorizationResource } from "./contract";
import { memoizeAuthorizationDecision } from "./request-memo";

/**
 * Runs in the `rsc` project because that is the only build where React's `cache` is implemented. In
 * the default build it is a permanent no-op, so outside this project the memo degrades to no
 * memoization — which is the documented behaviour, and also means these assertions could not fail
 * there for the right reason.
 *
 * The request scope is installed the same way `context.rsc.test.ts` installs it: React's `cache`
 * reads `ReactSharedInternals.A` and calls `getCacheForType`, and that is the whole contract Next.js
 * satisfies per request.
 */
const reactServerInternals = (
  React as unknown as {
    __SERVER_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE: { A: unknown };
  }
).__SERVER_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE;

const enterRequestScope = (): void => {
  const roots = new Map<() => unknown, unknown>();
  reactServerInternals.A = {
    getCacheForType<T>(resourceType: () => T): T {
      if (!roots.has(resourceType)) roots.set(resourceType, resourceType());
      return roots.get(resourceType) as T;
    },
  };
};

beforeEach(enterRequestScope);
afterEach(() => {
  reactServerInternals.A = null;
});

const user = (id: string): TAuthorizationActor => ({ type: "user", id });
const workspace = (id: string): TAuthorizationResource => ({ type: "workspace", id });

describe("memoizeAuthorizationDecision", () => {
  test("asks the evaluator once for a repeated question", async () => {
    const evaluate = vi.fn().mockResolvedValue(true);

    const first = memoizeAuthorizationDecision(user("u1"), "workspace.read", workspace("w1"), evaluate);
    const second = memoizeAuthorizationDecision(user("u1"), "workspace.read", workspace("w1"), evaluate);

    expect(await first).toBe(true);
    expect(await second).toBe(true);
    expect(evaluate).toHaveBeenCalledTimes(1);
  });

  test("shares one round trip between concurrent callers", async () => {
    // The common case: a layout and the page inside it render together and ask the same question.
    // Caching the promise rather than the value is what stops them starting two gRPC calls.
    let resolveEvaluate: ((value: boolean) => void) | undefined;
    const evaluate = vi.fn(
      () =>
        new Promise<boolean>((resolve) => {
          resolveEvaluate = resolve;
        })
    );

    const both = Promise.all([
      memoizeAuthorizationDecision(user("u1"), "workspace.read", workspace("w1"), evaluate),
      memoizeAuthorizationDecision(user("u1"), "workspace.read", workspace("w1"), evaluate),
    ]);
    resolveEvaluate?.(true);

    expect(await both).toEqual([true, true]);
    expect(evaluate).toHaveBeenCalledTimes(1);
  });

  test.each([
    ["a different actor", () => user("u2"), () => "workspace.read" as const, () => workspace("w1")],
    ["a different action", () => user("u1"), () => "workspace.write" as const, () => workspace("w1")],
    ["a different resource", () => user("u1"), () => "workspace.read" as const, () => workspace("w2")],
  ])("does not reuse a decision for %s", async (_label, actor, action, resource) => {
    // The key has to carry every input. Collapsing any of them would hand one user another's answer,
    // which is the one way this optimisation could become a security bug rather than a slow page.
    const evaluate = vi.fn().mockResolvedValue(true);

    await memoizeAuthorizationDecision(user("u1"), "workspace.read", workspace("w1"), evaluate);
    await memoizeAuthorizationDecision(actor(), action(), resource(), evaluate);

    expect(evaluate).toHaveBeenCalledTimes(2);
  });

  test("denials are cached as readily as grants", async () => {
    const evaluate = vi.fn().mockResolvedValue(false);

    expect(
      await memoizeAuthorizationDecision(user("u1"), "survey.read", { type: "survey", id: "s1" }, evaluate)
    ).toBe(false);
    expect(
      await memoizeAuthorizationDecision(user("u1"), "survey.read", { type: "survey", id: "s1" }, evaluate)
    ).toBe(false);
    expect(evaluate).toHaveBeenCalledTimes(1);
  });

  test("a failure is not cached, so the next caller may try again", async () => {
    // An operational error is not a decision. Caching it would turn one SpiceDB blip into a page
    // that cannot recover for the rest of the request.
    const evaluate = vi
      .fn()
      .mockRejectedValueOnce(new Error("spicedb unavailable"))
      .mockResolvedValueOnce(true);

    await expect(
      memoizeAuthorizationDecision(user("u1"), "survey.write", { type: "survey", id: "s9" }, evaluate)
    ).rejects.toThrow("spicedb unavailable");

    expect(
      await memoizeAuthorizationDecision(user("u1"), "survey.write", { type: "survey", id: "s9" }, evaluate)
    ).toBe(true);
    expect(evaluate).toHaveBeenCalledTimes(2);
  });

  test("a decision does not survive into the next request", async () => {
    // The store is request-scoped for a reason: a process-wide cache would hand the next visitor
    // the previous one's answer.
    const evaluate = vi.fn().mockResolvedValue(true);

    await memoizeAuthorizationDecision(user("u1"), "workspace.read", workspace("w1"), evaluate);
    enterRequestScope();
    await memoizeAuthorizationDecision(user("u1"), "workspace.read", workspace("w1"), evaluate);

    expect(evaluate).toHaveBeenCalledTimes(2);
  });
});
