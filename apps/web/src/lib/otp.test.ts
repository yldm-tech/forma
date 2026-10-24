import { describe, expect, test } from "vite-plus/test";
import { otpDigits, setOtpDigit } from "./otp";

test("otpDigits keeps digits in place and blanks anything else", () => {
  expect(otpDigits("12 4", 6)).toEqual(["1", "2", "", "4", "", ""]);
  expect(otpDigits("", 3)).toEqual(["", "", ""]);
});

describe("setOtpDigit", () => {
  test("a digit fills its box and moves on", () => {
    expect(setOtpDigit("", 6, 0, "1")).toEqual({ value: "1     ", advance: "next" });
    expect(setOtpDigit("12", 6, 2, "3")).toEqual({ value: "123   ", advance: "next" });
  });
  test("a whole pasted code replaces the value", () => {
    expect(setOtpDigit("", 6, 3, "123456")).toEqual({ value: "123456", advance: "done" });
  });
  test("letters and partial pastes are ignored", () => {
    expect(setOtpDigit("", 6, 0, "a")).toBeNull();
    expect(setOtpDigit("", 6, 0, "123")).toBeNull();
  });
  test("a box is cleared only when the next one is empty", () => {
    expect(setOtpDigit("123", 6, 2, "")).toEqual({ value: "12    ", advance: "stay" });
    expect(setOtpDigit("123", 6, 1, "")).toBeNull();
  });
});
