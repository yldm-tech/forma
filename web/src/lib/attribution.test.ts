import { describe, expect, test } from "vite-plus/test";
import {
  attributionCookie,
  buildAttributionQuerySuffix,
  pickAttributionParams,
  withAttributionQuery,
} from "./attribution";

// forma2's attribution.ts and AttributionTracker: only whitelisted, non-blank values, first touch wins, nothing oversized is written.

describe("pickAttributionParams", () => {
  test("keeps the whitelisted keys, trimmed and capped", () => {
    const params = new URLSearchParams({
      utm_source: " newsletter ",
      utm_term: "  ",
      email: "ada@example.com",
      page: "x".repeat(300),
    });
    expect(pickAttributionParams(params)).toEqual({ utm_source: "newsletter", page: "x".repeat(256) });
  });

  test("is empty without parameters", () => {
    expect(pickAttributionParams(null)).toEqual({});
    expect(buildAttributionQuerySuffix(new URLSearchParams("inviteToken=t"))).toBe("");
  });
});

test("withAttributionQuery appends to a path with or without a query", () => {
  const params = new URLSearchParams("utm_campaign=spring&callbackUrl=x");
  expect(withAttributionQuery("/auth/signup", params)).toBe("/auth/signup?utm_campaign=spring");
  expect(withAttributionQuery("/auth/signup?inviteToken=t", params)).toBe(
    "/auth/signup?inviteToken=t&utm_campaign=spring"
  );
  expect(withAttributionQuery("/auth/signup", new URLSearchParams())).toBe("/auth/signup");
});

describe("attributionCookie", () => {
  test("records the first touch as encoded JSON for half an hour", () => {
    expect(attributionCookie("", "?utm_source=ads&utm_medium=cpc&x=1", "http:")).toBe(
      `fb_attribution=${encodeURIComponent('{"utm_source":"ads","utm_medium":"cpc"}')}; path=/; max-age=1800; SameSite=Lax`
    );
  });

  test("is Secure over HTTPS", () => {
    expect(attributionCookie("", "?page=pricing", "https:")).toMatch(/; Secure$/);
  });

  test("never overwrites an earlier touch", () => {
    expect(attributionCookie("a=1; fb_attribution=%7B%7D", "?utm_source=ads", "http:")).toBeNull();
  });

  test("writes nothing without attribution or when it would be too large to keep", () => {
    expect(attributionCookie("", "?inviteToken=t", "http:")).toBeNull();
    const keys = ["page", "utm_source", "utm_medium", "utm_campaign", "utm_term", "utm_content", "initial_source", "entry_page"];
    const huge = new URLSearchParams(keys.map((key) => [key, "é".repeat(256)]));
    expect(attributionCookie("", `?${huge.toString()}`, "http:")).toBeNull();
  });
});
