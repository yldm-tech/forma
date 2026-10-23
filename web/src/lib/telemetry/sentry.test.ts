// @vitest-environment jsdom
import type { Event, EventHint } from "@sentry/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { MASKED } from "./runtime-config";

// forma2's init-client-sentry.test.ts, plus the masking of tokens in what the browser reports.

const sentry = vi.hoisted(() => ({
  init: vi.fn(),
  replayIntegration: vi.fn((options: unknown) => ({ name: "Replay", options })),
  addEventProcessor: vi.fn(),
  captureException: vi.fn(),
}));

vi.mock("@sentry/react", () => sentry);

const CONFIG = { dsn: "https://key@sentry.example.com/1", release: "1.2.3", environment: "production" };

// The module is loaded afresh for every test, and the ApiError it recognises with it.
const load = async () => {
  vi.resetModules();
  const [sentryModule, { ApiError }] = await Promise.all([import("./sentry"), import("../api")]);
  return { ...sentryModule, ApiError };
};

type InitOptions = {
  beforeSend: (event: Event, hint: EventHint) => Event | null;
  beforeBreadcrumb: (breadcrumb: unknown) => unknown;
};
const initOptions = () => sentry.init.mock.calls[0]?.[0] as InitOptions;

beforeEach(() => {
  vi.clearAllMocks();
});

afterEach(() => {
  delete window.__formaSentryClientConfig;
});

describe("initSentry", () => {
  test("initializes once with the configuration the server wrote into the page", async () => {
    window.__formaSentryClientConfig = CONFIG;
    const { initSentry } = await load();

    await Promise.all([initSentry(), initSentry()]);
    await initSentry();

    expect(sentry.init).toHaveBeenCalledTimes(1);
    expect(sentry.init).toHaveBeenCalledWith(
      expect.objectContaining({ dsn: CONFIG.dsn, release: CONFIG.release, environment: CONFIG.environment })
    );
    expect(sentry.addEventProcessor).toHaveBeenCalledTimes(1);
  });

  test("stays off, without loading the SDK, while no DSN is configured", async () => {
    const { initSentry, reportError } = await load();

    await initSentry();
    reportError(new Error("boom"));

    expect(sentry.init).not.toHaveBeenCalled();
    expect(sentry.captureException).not.toHaveBeenCalled();
  });

  test("ignores a configuration without a DSN", async () => {
    window.__formaSentryClientConfig = { dsn: "" };
    const { initSentry } = await load();

    await initSentry();

    expect(sentry.init).not.toHaveBeenCalled();
  });

  test("lets the app start without error reporting when the SDK cannot be fetched", async () => {
    window.__formaSentryClientConfig = CONFIG;
    const warn = vi.spyOn(console, "warn").mockImplementation(() => undefined);
    vi.doMock("@sentry/react", () => {
      throw new Error("Failed to fetch dynamically imported module");
    });
    try {
      const { initSentry, reportError } = await load();

      await expect(initSentry()).resolves.toBeUndefined();
      reportError(new Error("boom"));

      expect(warn).toHaveBeenCalledWith("Sentry could not be loaded", expect.anything());
    } finally {
      vi.doMock("@sentry/react", () => sentry);
      warn.mockRestore();
    }
  });

  test("keeps forma2's tracing, replay sampling and privacy options", async () => {
    window.__formaSentryClientConfig = CONFIG;
    const { initSentry } = await load();

    await initSentry();

    expect(sentry.init).toHaveBeenCalledWith(
      expect.objectContaining({
        tracesSampleRate: 0,
        replaysOnErrorSampleRate: 1.0,
        replaysSessionSampleRate: 0.1,
        debug: false,
        sendDefaultPii: false,
        sendClientReports: false,
      })
    );
    expect(sentry.replayIntegration).toHaveBeenCalledWith(
      expect.objectContaining({ maskAllText: true, blockAllMedia: true })
    );
  });

  test("drops errors the app expects and keeps everything else", async () => {
    window.__formaSentryClientConfig = CONFIG;
    const { initSentry, ApiError } = await load();
    await initSentry();
    const { beforeSend } = initOptions();
    const event = { message: "boom" };

    expect(beforeSend(event, { originalException: new ApiError(404, "not_found", "Not found") })).toBeNull();
    expect(
      beforeSend(event, { originalException: new ApiError(403, "forbidden", "Not authorized") })
    ).toBeNull();
    expect(beforeSend(event, { originalException: new ApiError(500, "internal_server_error", "Oops") })).toBe(
      event
    );
    expect(beforeSend(event, { originalException: new Error("boom") })).toBe(event);
    expect(beforeSend(event, {})).toBe(event);
  });

  test("reports what a route's error screen caught, unless the app expects it", async () => {
    window.__formaSentryClientConfig = CONFIG;
    const { initSentry, reportError, ApiError } = await load();
    await initSentry();
    const error = new Error("render failed");

    reportError(error);
    reportError(new ApiError(404, "not_found", "Not found"));

    expect(sentry.captureException).toHaveBeenCalledTimes(1);
    expect(sentry.captureException).toHaveBeenCalledWith(error);
  });
});

describe("scrubbing", () => {
  test("masks tokens and addresses in the page URL, the referrer, breadcrumbs and a replay's URLs", async () => {
    const { scrubEvent } = await load();

    const scrubbed = scrubEvent({
      request: {
        url: "https://forma.example.com/invite?token=secret",
        headers: { Referer: "https://forma.example.com/auth/login?callbackUrl=%2Finvite%3Ftoken%3Dsecret" },
      },
      breadcrumbs: [
        { category: "navigation", data: { from: "/auth/verify-email?token=a", to: "/workspaces/w1" } },
        { category: "fetch", data: { url: "/api/app/auth/me", method: "GET" } },
      ],
      urls: ["https://forma.example.com/auth/forgot-password/reset?token=b"],
    } as Event & { urls: string[] });

    expect(JSON.stringify(scrubbed)).not.toContain("secret");
    expect(scrubbed.request?.url).toBe(`https://forma.example.com/invite?token=${MASKED}`);
    expect(scrubbed.request?.headers?.Referer).toBe(
      `https://forma.example.com/auth/login?callbackUrl=${MASKED}`
    );
    expect(scrubbed.breadcrumbs?.[0]?.data).toEqual({
      from: `/auth/verify-email?token=${MASKED}`,
      to: "/workspaces/w1",
    });
    expect(scrubbed.breadcrumbs?.[1]?.data).toEqual({ url: "/api/app/auth/me", method: "GET" });
    expect((scrubbed as { urls: string[] }).urls).toEqual([
      `https://forma.example.com/auth/forgot-password/reset?token=${MASKED}`,
    ]);
  });

  test("leaves an event without URLs alone", async () => {
    const { scrubEvent } = await load();

    expect(scrubEvent({ message: "boom" })).toEqual({ message: "boom" });
  });

  test("masks breadcrumbs before they are kept, and the URLs of a replay's navigation spans", async () => {
    window.__formaSentryClientConfig = CONFIG;
    const { initSentry, scrubRecordingEvent } = await load();
    await initSentry();

    expect(
      initOptions().beforeBreadcrumb({ category: "navigation", data: { to: "/invite?token=a" } })
    ).toEqual({
      category: "navigation",
      data: { to: `/invite?token=${MASKED}` },
    });
    const span = {
      type: 5,
      timestamp: 1,
      data: {
        tag: "performanceSpan",
        payload: {
          op: "navigation.push",
          description: "/invite?token=a",
          data: { previous: "/auth/login?email=x" },
        },
      },
    };
    expect(scrubRecordingEvent(span).data.payload).toEqual({
      op: "navigation.push",
      description: `/invite?token=${MASKED}`,
      data: { previous: `/auth/login?email=${MASKED}` },
    });
    const breadcrumb = {
      type: 5,
      timestamp: 1,
      data: { tag: "breadcrumb", payload: { category: "ui.click" } },
    };
    expect(scrubRecordingEvent(breadcrumb)).toBe(breadcrumb);
    expect(sentry.replayIntegration).toHaveBeenCalledWith(
      expect.objectContaining({ beforeAddRecordingEvent: scrubRecordingEvent })
    );
  });
});
