import { readFileSync } from "node:fs";
import { describe, expect, test } from "vite-plus/test";
import { z } from "zod";
// Used for parity check in tests only
import { validateEmail, validatePhone, validateUrl } from "./validation-utils";

describe("Validation Logic Parity", () => {
  describe("Email Validation", () => {
    const zodEmail = z.email();

    const testCases = [
      "test@example.com",
      "user.name+tag@example.co.uk",
      "invalid-email",
      "no-at-sign.com",
      "@domain.com",
      "user@",
      "user@domain", // Zod might accept this or not depending on TLD requirement. Our regex requires TLD {2,}.
      "user@domain.c", // Our regex requires 2 chars TLD.
    ];

    testCases.forEach((email) => {
      test(`should match Zod behavior for email: "${email}"`, () => {
        const zodResult = zodEmail.safeParse(email).success;
        const myResult = validateEmail(email);

        // We aim for parity with Zod's default email validator.
        // Our custom ReDoS-safe regex handles standard email formats correctly.
        expect(myResult).toBe(zodResult);
      });
    });
  });

  describe("URL Validation", () => {
    const zodUrl = z.url();

    const testCases = [
      "https://example.com",
      "http://localhost:3000",
      "ftp://files.com",
      "invalid-url",
      "examples",
      "https://",
    ];

    testCases.forEach((url) => {
      test(`should match Zod behavior for URL: "${url}"`, () => {
        const zodResult = zodUrl.safeParse(url).success;
        const myResult = validateUrl(url);

        if (zodResult) {
          expect(myResult).toBe(true);
        } else {
          expect(myResult).toBe(false);
        }
      });
    });
  });

  // The server's isValidPhone runs the same check on the same cases; the table lives with the Go test so neither side can drift alone.
  describe("Phone Validation", () => {
    const cases = JSON.parse(
      readFileSync(
        new URL(
          "../../../../../../apps/server/internal/response/answers/testdata/phone.json",
          import.meta.url
        ),
        "utf8"
      )
    ) as { name: string; input: string; valid: boolean }[];

    test("the shared table is not empty", () => {
      expect(cases.length).toBeGreaterThan(0);
    });

    cases.forEach(({ name, input, valid }) => {
      test(`${name}: "${input}" is ${valid ? "valid" : "invalid"}`, () => {
        expect(validatePhone(input)).toBe(valid);
      });
    });
  });
});
