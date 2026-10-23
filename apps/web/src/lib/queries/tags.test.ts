import { expect, test } from "vite-plus/test";
import { ApiError } from "../api";
import { isDuplicateTagNameError } from "./tags";

test("a duplicate rename is recognised by its invalid_params reason only", () => {
  const duplicate = new ApiError(422, "http_error", "Unable to update tag", undefined, undefined, [
    { name: "name", reason: "tag_name_already_exists" },
  ]);
  expect(isDuplicateTagNameError(duplicate)).toBe(true);
  expect(isDuplicateTagNameError(new ApiError(422, "http_error", "Unable to update tag"))).toBe(false);
  expect(isDuplicateTagNameError(new Error("tag_name_already_exists"))).toBe(false);
});
