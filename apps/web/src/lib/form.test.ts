import { describe, expect, test } from "vite-plus/test";
import { type Validators, hasErrors, validateValues, visibleErrors } from "./form";

interface Values {
  password: string;
  confirm: string;
}

const rules: Validators<Values> = {
  password: (value) => (value.length < 3 ? "too short" : undefined),
  confirm: (value, values) => (value !== values.password ? "mismatch" : undefined),
};

describe("validateValues", () => {
  test("keeps only the failing fields, and a rule sees every value", () => {
    expect(validateValues(rules, { password: "ab", confirm: "ab" })).toEqual({ password: "too short" });
    expect(validateValues(rules, { password: "abc", confirm: "abd" })).toEqual({ confirm: "mismatch" });
    expect(validateValues(rules, { password: "abc", confirm: "abc" })).toEqual({});
  });

  test("ignores fields without a rule", () => {
    expect(validateValues<{ a: string; b: string }>({ a: () => "bad" }, { a: "", b: "" })).toEqual({
      a: "bad",
    });
  });
});

test("hasErrors", () => {
  expect(hasErrors({})).toBe(false);
  expect(hasErrors({ password: undefined })).toBe(false);
  expect(hasErrors({ password: "too short" })).toBe(true);
});

describe("visibleErrors", () => {
  const validation = { password: "too short", confirm: "mismatch" };

  test("hides rule failures until a submit has been attempted", () => {
    expect(visibleErrors(validation, {}, false)).toEqual({});
    expect(visibleErrors(validation, {}, true)).toEqual(validation);
  });

  test("shows a server-reported error at once, ahead of the field's own rule", () => {
    expect(visibleErrors<Values>({}, { password: "breached" }, false)).toEqual({ password: "breached" });
    expect(visibleErrors<Values>(validation, { password: "breached" }, true)).toEqual({
      password: "breached",
      confirm: "mismatch",
    });
  });
});
