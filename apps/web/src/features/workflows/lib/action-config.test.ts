import { describe, expect, test } from "vite-plus/test";
import type { TWorkflowHttpRequestActionNode } from "../definition";
import { clearSecretsOnOriginChange, generateSigningSecret, originOf } from "./action-config";

const config = (overrides: Partial<TWorkflowHttpRequestActionNode["config"]> = {}) => ({
  method: "POST" as const,
  url: "https://api.example.com/a",
  headers: [
    { name: "Authorization", secret: { sealed: "s1", hint: "…ijkl" } },
    { name: "X-Plain", value: "1" },
    { name: "X-New", secret: { value: "typed" } },
  ],
  body: "",
  signingSecret: { sealed: "s2" },
  ...overrides,
});

describe("clearSecretsOnOriginChange", () => {
  test("keeps sealed secrets while the URL stays on its origin", () => {
    expect(clearSecretsOnOriginChange(config(), "https://API.example.com/other?x=1")).toEqual({
      config: { url: "https://API.example.com/other?x=1" },
      cleared: false,
    });
  });

  test("empties sealed secrets, and only those, when the host, scheme or port changes", () => {
    for (const url of [
      "https://evil.example/a",
      "http://api.example.com/a",
      "https://api.example.com:8443/a",
    ]) {
      const next = clearSecretsOnOriginChange(config(), url);
      expect(next.cleared).toBe(true);
      expect(next.config).toEqual({
        url,
        headers: [
          { name: "Authorization", secret: {} },
          { name: "X-Plain", value: "1" },
          { name: "X-New", secret: { value: "typed" } },
        ],
        signingSecret: {},
      });
    }
  });

  test("changes nothing else without a sealed secret", () => {
    const plain = config({ headers: [{ name: "X-Plain", value: "1" }], signingSecret: null });
    expect(clearSecretsOnOriginChange(plain, "https://elsewhere.example")).toEqual({
      config: { url: "https://elsewhere.example" },
      cleared: false,
    });
  });
});

test("originOf", () => {
  expect(originOf("https://API.example.com:8443/a?b")).toBe("https://api.example.com:8443");
  expect(originOf("not a url")).toBe("");
});

test("generateSigningSecret is a Standard Webhooks secret of 32 random bytes", () => {
  const secret = generateSigningSecret();
  expect(secret).toMatch(/^whsec_[A-Za-z0-9+/]{43}=$/);
  expect(atob(secret.slice("whsec_".length))).toHaveLength(32);
  expect(generateSigningSecret()).not.toBe(secret);
});
