import { describe, expect, test } from "vite-plus/test";
import { ApiError } from "./api";
import { createQueryClient, shouldRetry } from "./query-client";
import { authKeys } from "./queries/auth";

describe("shouldRetry", () => {
  test.each([400, 401, 403, 404, 409, 429])("never retries a %i", (status) => {
    expect(shouldRetry(0, new ApiError(status, "x", "x"))).toBe(false);
  });

  test("retries server and network failures twice", () => {
    for (const error of [new ApiError(503, "x", "x"), new TypeError("Failed to fetch")]) {
      expect(shouldRetry(0, error)).toBe(true);
      expect(shouldRetry(1, error)).toBe(true);
      expect(shouldRetry(2, error)).toBe(false);
    }
  });
});

test("mutations are never retried", () => {
  expect(createQueryClient().getDefaultOptions().mutations?.retry).toBe(false);
});

describe("an ended session", () => {
  const notAuthenticated = () => new ApiError(401, "not_authenticated", "Not authenticated");

  const signedIn = () => {
    let ended = 0;
    const client = createQueryClient(() => ended++);
    client.setQueryData(authKeys.me, { id: "u1" });
    return { client, ended: () => ended };
  };

  test("a query that finds the session gone sends a signed-in browser to sign-in and drops its data", async () => {
    const { client, ended } = signedIn();
    await client
      .fetchQuery({ queryKey: ["organizations"], queryFn: () => Promise.reject(notAuthenticated()) })
      .catch(() => undefined);
    expect(ended()).toBe(1);
    expect(client.getQueryData(authKeys.me)).toBeUndefined();
  });

  test("so does a mutation", async () => {
    const { client, ended } = signedIn();
    const mutation = client.getMutationCache().build(client, {
      mutationFn: () => Promise.reject(notAuthenticated()),
    });
    await mutation.execute(undefined).catch(() => undefined);
    expect(ended()).toBe(1);
  });

  test("a refused credential or another error is the screen's to show", async () => {
    const { client, ended } = signedIn();
    for (const error of [
      new ApiError(401, "invalid_backup_code", "Invalid backup code"),
      new ApiError(403, "forbidden", "Not authorized"),
    ]) {
      await client
        .fetchQuery({ queryKey: [error.code], queryFn: () => Promise.reject(error) })
        .catch(() => undefined);
    }
    expect(ended()).toBe(0);
  });

  test("a browser that is not signed in is left to the signed-out screens", async () => {
    let ended = 0;
    const client = createQueryClient(() => ended++);
    await client
      .fetchQuery({ queryKey: authKeys.me, queryFn: () => Promise.reject(notAuthenticated()) })
      .catch(() => undefined);
    expect(ended).toBe(0);
  });
});
