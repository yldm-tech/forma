// @vitest-environment jsdom
import { afterEach, describe, expect, test } from "vite-plus/test";
import { MASKED, maskSensitiveQuery, postHogClientConfig, sentryClientConfig } from "./runtime-config";

afterEach(() => {
  delete window.__formaSentryClientConfig;
  delete window.__formaPostHogConfig;
});

describe("runtime config", () => {
  test("is absent until the server writes it into the page", () => {
    expect(sentryClientConfig()).toBeUndefined();
    expect(postHogClientConfig()).toBeUndefined();
  });

  test("is read from the globals the server writes", () => {
    window.__formaSentryClientConfig = { dsn: "https://key@sentry.example.com/1", environment: "production" };
    window.__formaPostHogConfig = { key: "phc_test", apiHost: "/ingest", uiHost: "https://eu.i.posthog.com" };

    expect(sentryClientConfig()).toEqual({
      dsn: "https://key@sentry.example.com/1",
      environment: "production",
    });
    expect(postHogClientConfig()?.key).toBe("phc_test");
  });

  test("an empty DSN or key leaves the SDK off", () => {
    window.__formaSentryClientConfig = { dsn: "" };
    window.__formaPostHogConfig = { key: "", apiHost: "/ingest", uiHost: "https://eu.i.posthog.com" };

    expect(sentryClientConfig()).toBeUndefined();
    expect(postHogClientConfig()).toBeUndefined();
  });
});

describe("maskSensitiveQuery", () => {
  test("masks the one-time tokens of the auth pages", () => {
    expect(maskSensitiveQuery("https://forma.example.com/invite?token=abc.def")).toBe(
      `https://forma.example.com/invite?token=${MASKED}`
    );
    expect(maskSensitiveQuery("/auth/forgot-password/reset?token=t1")).toBe(
      `/auth/forgot-password/reset?token=${MASKED}`
    );
    expect(maskSensitiveQuery("/auth/signup?inviteToken=t2&email=jane%40example.com")).toBe(
      `/auth/signup?inviteToken=${MASKED}&email=${MASKED}`
    );
  });

  test("masks a sign-in return path, which can carry any of them", () => {
    expect(maskSensitiveQuery("/auth/login?callbackUrl=%2Finvite%3Ftoken%3Dabc&error=x")).toBe(
      `/auth/login?callbackUrl=${MASKED}&error=x`
    );
  });

  test("masks the pending authorization of the consent page and keeps the client", () => {
    expect(
      maskSensitiveQuery(
        "/account/authorize?request_id=r1&client_id=c1&scope=openid&redirect_uri=http%3A%2F%2F127.0.0.1%3A6274%2Fcb"
      )
    ).toBe(`/account/authorize?request_id=${MASKED}&client_id=c1&scope=openid&redirect_uri=${MASKED}`);
  });

  test("leaves everything else, the fragment included, as it was written", () => {
    expect(maskSensitiveQuery("/workspaces/w1/surveys?filter=a%20b&sort=name#top")).toBe(
      "/workspaces/w1/surveys?filter=a%20b&sort=name#top"
    );
    expect(maskSensitiveQuery("/workspaces/w1")).toBe("/workspaces/w1");
    expect(maskSensitiveQuery("/x?token=a#token=b")).toBe(`/x?token=${MASKED}#token=b`);
  });

  test("matches encoded and repeated keys, and keys without a value", () => {
    expect(maskSensitiveQuery("/x?%74oken=a&token=b&token")).toBe(
      `/x?%74oken=${MASKED}&token=${MASKED}&token=${MASKED}`
    );
    expect(maskSensitiveQuery("/x?%E0%A4%A=1")).toBe("/x?%E0%A4%A=1");
  });
});
