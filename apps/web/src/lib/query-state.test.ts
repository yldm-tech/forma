import { expect, test } from "vite-plus/test";
import { combinedQueryState } from "./query-state";

const pending = { isPending: true, isError: false, error: null };
const done = { isPending: false, isError: false, error: null };
const failed = (error: unknown) => ({ isPending: false, isError: true, error });

test("a failed query is shown, not dropped, even while another is still loading", () => {
  const error = new Error("members failed");
  expect(combinedQueryState([failed(error), done, done])).toEqual({ status: "error", error });
  expect(combinedQueryState([done, pending, failed(error)])).toEqual({ status: "error", error });
});

test("waits for every query, then succeeds", () => {
  expect(combinedQueryState([done, pending])).toEqual({ status: "pending" });
  expect(combinedQueryState([done, done])).toEqual({ status: "success" });
});
