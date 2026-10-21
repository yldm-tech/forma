import { expect, test } from "vite-plus/test";
import { ApiError } from "./api";
import { untilAuthorized } from "./consistency";

const clock = () => {
  let t = 0;
  return { now: () => t, wait: async (ms: number) => void (t += ms) };
};

test("retries a refusal until the permission arrives", async () => {
  let calls = 0;
  const result = await untilAuthorized(async () => {
    calls++;
    if (calls < 3) throw new ApiError(403, "forbidden", "Not authorized");
    return "ok";
  }, clock());
  expect(result).toBe("ok");
  expect(calls).toBe(3);
});

test("gives up after the window", async () => {
  const c = clock();
  const error = await untilAuthorized(
    () => Promise.reject(new ApiError(404, "not_found", "Workspace not found")),
    { ...c, timeoutMs: 2000, intervalMs: 500 }
  ).catch((e: unknown) => e);
  expect(error).toMatchObject({ status: 404 });
  expect(c.now()).toBe(2000);
});

test("any other failure is not retried", async () => {
  let calls = 0;
  const error = await untilAuthorized(() => {
    calls++;
    return Promise.reject(new ApiError(500, "internal_server_error", "boom"));
  }, clock()).catch((e: unknown) => e);
  expect(error).toMatchObject({ status: 500 });
  expect(calls).toBe(1);
});
