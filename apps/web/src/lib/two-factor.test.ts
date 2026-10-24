import { expect, test } from "vite-plus/test";
import { disableTwoFactorFactor, formatBackupCode } from "./two-factor";

test("formatBackupCode splits the ten characters in two", () => {
  expect(formatBackupCode("abcde12345")).toBe("abcde-12345");
});

test("disableTwoFactorFactor requires exactly one second factor", () => {
  expect(disableTwoFactorFactor("123456", "")).toEqual({ code: "123456" });
  expect(disableTwoFactorFactor("", " abcde-12345 ")).toEqual({ backupCode: "abcde-12345" });
  expect(disableTwoFactorFactor("123456", "abcde-12345")).toBeNull();
  expect(disableTwoFactorFactor("", "")).toBeNull();
  // An incomplete authenticator code (the OTP boxes keep a space for an empty position) is no code.
  expect(disableTwoFactorFactor("12 456", "")).toBeNull();
});
