import type { TFunction } from "i18next";
import { beforeAll, describe, expect, test } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import {
  MESSAGES,
  emailError,
  nameError,
  passwordChecks,
  passwordError,
  serverFieldMessage,
  signInPasswordError,
  translatedRules,
  validationMessage,
} from "./validation";

describe("emailError", () => {
  // The cases of the Go server's userinput.ValidEmailFormat test, which reproduces the same zod pattern: both sides must agree.
  test.each([
    ["ada@example.com", true],
    ["ada.lovelace+x@ex-ample.co", true],
    ["o'brien@example.ie", true],
    ["ADA@EXAMPLE.COM", true],
    ["ada@example", false],
    [".ada@example.com", false],
    ["ada..x@example.com", false],
    ["ada.@example.com", false],
    ["ada@example.com\n", false],
    [" ada@example.com", false],
    ["ada@-example.com", false],
    ["not-an-email", false],
    ["", false],
  ])("%j is valid: %s", (email, valid) => {
    expect(emailError(email)).toBe(valid ? undefined : MESSAGES.invalidEmail);
  });
});

describe("signInPasswordError", () => {
  test.each([
    ["1234567", MESSAGES.passwordTooShort],
    ["", MESSAGES.passwordTooShort],
    ["a".repeat(129), MESSAGES.passwordTooLong],
    ["12345678", undefined],
    ["a".repeat(128), undefined],
  ])("%j", (password, expected) => {
    expect(signInPasswordError(password)).toBe(expected);
  });

  test("counts UTF-16 code units, as the Go server does", () => {
    expect(signInPasswordError("😀😀😀😀")).toBeUndefined();
  });
});

describe("passwordError", () => {
  test.each([
    ["Short1", MESSAGES.passwordTooShort],
    [`A1${"a".repeat(127)}`, MESSAGES.passwordTooLong],
    ["alllowercase1", MESSAGES.passwordPattern],
    ["NoDigitsHere", MESSAGES.passwordPattern],
    ["Password1", undefined],
    ["PASSWORD1", undefined],
    ["Pass\nword1", MESSAGES.passwordPattern],
  ])("%j", (password, expected) => {
    expect(passwordError(password)).toBe(expected);
  });
});

describe("nameError", () => {
  test.each([
    ["Ada Lovelace", undefined],
    ["Zoë O'Brien-Smith", undefined],
    ["山田 太郎", undefined],
    ["Acme & Co. (2)", undefined],
    ["  Ada  ", undefined],
    ["", MESSAGES.nameTooShort],
    ["   ", MESSAGES.nameTooShort],
    ["<script>", MESSAGES.nameFormat],
    ["ada@example.com", MESSAGES.nameFormat],
    ["Ada_Lovelace", MESSAGES.nameFormat],
  ])("%j", (name, expected) => {
    expect(nameError(name)).toBe(expected);
  });
});

describe("passwordChecks", () => {
  const passed = (password: string) => passwordChecks(password).map((check) => check.passed);

  test("reports each line of the checklist", () => {
    expect(passed("")).toEqual([false, false, false]);
    expect(passed("Password1")).toEqual([true, true, true]);
    expect(passed("Pa1")).toEqual([true, false, true]);
  });

  test("asks for a lowercase letter the rule does not require, as forma2's checklist did", () => {
    expect(passwordError("PASSWORD1")).toBeUndefined();
    expect(passed("PASSWORD1")).toEqual([false, true, true]);
  });
});

describe("serverFieldMessage", () => {
  test.each([
    [{ field: "email", rule: "email" }, MESSAGES.invalidEmail],
    [{ field: "email", rule: "required" }, MESSAGES.invalidEmail],
    [{ field: "name", rule: "min", param: "1" }, MESSAGES.nameTooShort],
    [{ field: "name", rule: "name_format" }, MESSAGES.nameFormat],
    [{ field: "password", rule: "min", param: "8" }, MESSAGES.passwordTooShort],
    [{ field: "password", rule: "required" }, MESSAGES.passwordTooShort],
    [{ field: "newPassword", rule: "max", param: "128" }, MESSAGES.passwordTooLong],
    [{ field: "password", rule: "password_composition" }, MESSAGES.passwordPattern],
    [{ field: "turnstileToken", rule: "required" }, MESSAGES.captchaRequired],
    [{ field: "userLocale", rule: "oneof" }, undefined],
  ])("%j", (error, expected) => {
    expect(serverFieldMessage(error)).toBe(expected);
  });
});

describe("the messages", () => {
  let t: TFunction;
  beforeAll(async () => {
    t = (await initI18n("ja-JP")).t;
  });

  test("are translated, never the raw pattern forma2 showed", async () => {
    const en = (await initI18n("en-US")).t;
    expect(validationMessage(en, MESSAGES.passwordPattern)).toBe(
      "Use at least one uppercase letter and one number."
    );
    expect(validationMessage(en, MESSAGES.passwordTooShort)).toBe("Use at least 8 characters.");
    for (const key of Object.values(MESSAGES)) {
      expect(validationMessage(t, key)).not.toBe(key);
      expect(validationMessage(t, key)).not.toBe(validationMessage(en, key));
    }
  });

  test("replace each rule's key in translatedRules and leave a passing value alone", () => {
    const rules = translatedRules<{ email: string }>(t, { email: emailError });
    expect(rules.email?.("bad", { email: "bad" })).toBe(validationMessage(t, MESSAGES.invalidEmail));
    expect(rules.email?.("ada@example.com", { email: "ada@example.com" })).toBeUndefined();
  });
});
