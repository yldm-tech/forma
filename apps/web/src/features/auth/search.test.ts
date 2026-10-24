import { expect, test } from "vite-plus/test";
import { callbackSearch, stringParam, tokenSearch } from "./search";

test.each([
  ["plain", "plain"],
  [["first", "second"], "first"],
  [[], undefined],
  [undefined, undefined],
  ["", undefined],
  [123456, "123456"],
  [true, "true"],
  [{ nested: "x" }, undefined],
])("stringParam(%j) is %j", (value, expected) => {
  expect(stringParam(value)).toBe(expected);
});

test("route search parsers keep only their own parameters", () => {
  expect(callbackSearch({ callbackUrl: "/x", other: "y" })).toEqual({ callbackUrl: "/x" });
  expect(tokenSearch({ token: "abc", callbackUrl: "/x" })).toEqual({ token: "abc" });
  expect(tokenSearch({})).toEqual({ token: undefined });
});
