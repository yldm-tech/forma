import { describe, expect, test } from "vite-plus/test";
import {
  callbackPath,
  inviteTokenFromCallbackUrl,
  inviteUrl,
  ssoReturnToUrl,
  validatedCallbackUrl,
} from "./callback-url";

// forma2's lib/utils/url.test.ts (getValidatedCallbackUrl) and modules/auth/lib/callback-url.test.ts.

describe("validatedCallbackUrl", () => {
  const origin = "https://webapp.example.com";

  test.each([
    ["https://webapp.example.com/callback", "https://webapp.example.com/callback"],
    ["https://webapp.example.com/dashboard", "https://webapp.example.com/dashboard"],
    ["/invite?token=abc", "https://webapp.example.com/invite?token=abc"],
  ])("accepts %s", (url, expected) => {
    expect(validatedCallbackUrl(url, origin)).toBe(expected);
  });

  test.each([
    ["an unsupported scheme", "ftp://webapp.example.com/callback"],
    ["another origin", "https://malicious.com/callback"],
    ["another port", "https://webapp.example.com:8443/callback"],
    ["a value that is neither a URL nor a root-relative path", "not-a-valid-url"],
    ["a javascript: URL", "javascript:alert(1)"],
    ["credentials", "https://user:pass@webapp.example.com/callback"],
    ["a scheme-relative pathname", "https://webapp.example.com//evil.example/path?x=1#frag"],
    ["the dot-segment variant of a // pathname", "https://webapp.example.com/.//evil.example/path"],
    ["the backslash variant of a // pathname", "https://webapp.example.com/\\evil.example"],
    ["a protocol-relative path", "//evil.example/path"],
    ["nothing", ""],
  ])("rejects %s", (_, url) => {
    expect(validatedCallbackUrl(url, origin)).toBeNull();
  });

  test("rejects a missing value", () => {
    expect(validatedCallbackUrl(undefined, origin)).toBeNull();
    expect(validatedCallbackUrl(null, origin)).toBeNull();
  });
});

describe("callbackPath", () => {
  const origin = "http://localhost:3000";

  test("returns the in-app path of a valid callback", () => {
    expect(callbackPath("http://localhost:3000/invite?token=abc123&source=signin", origin)).toBe(
      "/invite?token=abc123&source=signin"
    );
    expect(callbackPath("/workspaces/1#settings", origin)).toBe("/workspaces/1#settings");
  });

  test("falls back to the root page for a missing or invalid callback", () => {
    expect(callbackPath(undefined, origin)).toBe("/");
    expect(callbackPath("https://evil.example/x", origin)).toBe("/");
  });

  test("falls back to the root page for a same-origin URL with a // pathname, which a router would resolve off-origin", () => {
    expect(callbackPath("http://localhost:3000//evil.example/path", origin)).toBe("/");
  });
});

describe("inviteTokenFromCallbackUrl", () => {
  const origin = "http://localhost:3000";

  test("extracts the token of a validated callback", () => {
    expect(
      inviteTokenFromCallbackUrl("http://localhost:3000/invite?token=abc123&source=signin", origin)
    ).toBe("abc123");
  });

  test("returns null for an invalid callback or one without a token", () => {
    expect(inviteTokenFromCallbackUrl("https://evil.example/invite?token=bad", origin)).toBeNull();
    expect(inviteTokenFromCallbackUrl("http://localhost:3000/invite?source=signin", origin)).toBeNull();
  });
});

test("inviteUrl encodes the token", () => {
  expect(inviteUrl("a+b/c=", "http://localhost:3000")).toBe(
    "http://localhost:3000/invite?token=a%2Bb%2Fc%3D"
  );
});

describe("ssoReturnToUrl", () => {
  test("tags an absolute callback with its source", () => {
    expect(ssoReturnToUrl("http://localhost:3000/invite?token=abc", "signup")).toBe(
      "http://localhost:3000/invite?token=abc&source=signup"
    );
  });

  test("keeps a relative callback relative", () => {
    expect(ssoReturnToUrl("/workspaces/1", "signin")).toBe("/workspaces/1?source=signin");
    expect(ssoReturnToUrl(undefined, "signin")).toBe("/?source=signin");
  });
});
