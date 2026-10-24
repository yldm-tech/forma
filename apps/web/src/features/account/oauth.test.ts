import { describe, expect, test } from "vite-plus/test";
import { consentRedirectUrl, hostOf, isLocalhostHost, scopeLabel } from "./oauth";

// forma2's oauth-client-metadata.test.ts and account/authorize consent-redirect.test.ts.

const t = (key: string) => `translated:${key}`;

describe("OAuth client metadata helpers", () => {
  test("extracts hosts from valid URLs", () => {
    expect(hostOf("https://client.example.com/callback")).toBe("client.example.com");
    expect(hostOf("http://localhost:6274/callback")).toBe("localhost:6274");
    expect(hostOf("not a url")).toBeNull();
    expect(hostOf(null)).toBeNull();
    expect(hostOf(undefined)).toBeNull();
  });

  test("matches only exact localhost hosts", () => {
    expect(isLocalhostHost("localhost")).toBe(true);
    expect(isLocalhostHost("localhost:6274")).toBe(true);
    expect(isLocalhostHost("127.0.0.1")).toBe(true);
    expect(isLocalhostHost("127.0.0.1:6274")).toBe(true);
    expect(isLocalhostHost("[::1]")).toBe(true);
    expect(isLocalhostHost("[::1]:6274")).toBe(true);
    expect(isLocalhostHost("localhost.example.com")).toBe(false);
    expect(isLocalhostHost("127.0.0.1.example.com")).toBe(false);
    expect(isLocalhostHost("[::1].example.com")).toBe(false);
    expect(isLocalhostHost(null)).toBe(false);
  });

  test("maps known OAuth scopes to localized labels", () => {
    expect(scopeLabel("openid", t)).toBe("translated:auth.oauth.scopes.openid");
    expect(scopeLabel("profile", t)).toBe("translated:auth.oauth.scopes.profile");
    expect(scopeLabel("email", t)).toBe("translated:auth.oauth.scopes.email");
    expect(scopeLabel("offline_access", t)).toBe("translated:auth.oauth.scopes.offline_access");
    expect(scopeLabel("surveys:read", t)).toBe("translated:auth.oauth.scopes.surveys_read");
    expect(scopeLabel("surveys:write", t)).toBe("translated:auth.oauth.scopes.surveys_write");
    expect(scopeLabel("workflows:read", t)).toBe("translated:auth.oauth.scopes.workflows_read");
    expect(scopeLabel("workflows:write", t)).toBe("translated:auth.oauth.scopes.workflows_write");
  });

  test("keeps unknown OAuth scopes readable", () => {
    expect(scopeLabel("custom:scope", t)).toBe("custom:scope");
  });
});

describe("consentRedirectUrl", () => {
  test("reads url from the accept answer", () => {
    expect(
      consentRedirectUrl({ redirect: true, url: "https://client.example/callback?code=abc&state=xyz" })
    ).toBe("https://client.example/callback?code=abc&state=xyz");
  });

  test("reads the error url from the deny answer", () => {
    expect(
      consentRedirectUrl({ redirect: true, url: "https://client.example/callback?error=access_denied" })
    ).toBe("https://client.example/callback?error=access_denied");
  });

  test("allows loopback and custom-scheme redirect targets (native MCP clients)", () => {
    expect(consentRedirectUrl({ url: "http://127.0.0.1:52765/callback?code=abc" })).toBe(
      "http://127.0.0.1:52765/callback?code=abc"
    );
    expect(consentRedirectUrl({ url: "cursor://anysphere.cursor-mcp/callback?code=abc" })).toBe(
      "cursor://anysphere.cursor-mcp/callback?code=abc"
    );
  });

  test("falls back to redirect_uri when only that is present", () => {
    expect(consentRedirectUrl({ redirect_uri: "https://client.example/cb?code=abc" })).toBe(
      "https://client.example/cb?code=abc"
    );
  });

  test("prefers url over redirect_uri when both are present", () => {
    expect(consentRedirectUrl({ url: "https://a.example/cb", redirect_uri: "https://b.example/cb" })).toBe(
      "https://a.example/cb"
    );
  });

  test("returns null for missing, empty, non-object and non-string values", () => {
    expect(consentRedirectUrl({})).toBeNull();
    expect(consentRedirectUrl({ url: "" })).toBeNull();
    expect(consentRedirectUrl({ url: "   " })).toBeNull();
    expect(consentRedirectUrl({ url: 123 })).toBeNull();
    expect(consentRedirectUrl(null)).toBeNull();
    expect(consentRedirectUrl(undefined)).toBeNull();
    expect(consentRedirectUrl("https://client.example/cb")).toBeNull();
  });

  test("rejects dangerous schemes", () => {
    expect(consentRedirectUrl({ url: "javascript:alert(1)" })).toBeNull();
    expect(consentRedirectUrl({ url: "  JavaScript:alert(1)" })).toBeNull();
    expect(consentRedirectUrl({ url: "java\tscript:alert(1)" })).toBeNull();
    expect(consentRedirectUrl({ url: "data:text/html,<script>alert(1)</script>" })).toBeNull();
    expect(consentRedirectUrl({ url: "file:///etc/passwd" })).toBeNull();
    expect(consentRedirectUrl({ url: "blob:https://x/1" })).toBeNull();
    expect(consentRedirectUrl({ url: "javascript:1", redirect_uri: "data:x" })).toBeNull();
  });

  test("rejects malformed and protocol-relative values", () => {
    expect(consentRedirectUrl({ url: "//evil.com/path" })).toBeNull();
    expect(consentRedirectUrl({ url: "not a url" })).toBeNull();
    expect(consentRedirectUrl({ url: "/relative/path" })).toBeNull();
  });
});
