import { cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vite-plus/test";

// forma2's PostHogIdentify and PostHogGroupIdentify.

const posthog = vi.hoisted(() => ({
  __loaded: false,
  capture: vi.fn(),
  group: vi.fn(),
  identify: vi.fn(),
  init: vi.fn(),
  reset: vi.fn(),
}));

vi.mock("posthog-js", () => ({ default: posthog }));

const CONFIG = { key: "phc_test", apiHost: "/ingest", uiHost: "https://eu.i.posthog.com" };

const load = async () => {
  vi.resetModules();
  return import("./posthog-identify");
};

beforeEach(() => {
  vi.clearAllMocks();
  posthog.__loaded = false;
});

afterEach(() => {
  cleanup();
  delete window.__formaPostHogConfig;
  vi.useRealTimers();
});

describe("PostHogIdentify", () => {
  test("starts PostHog with the server's key and identifies the user by id and email domain only", async () => {
    window.__formaPostHogConfig = CONFIG;
    const { PostHogIdentify } = await load();

    render(<PostHogIdentify userId="u1" email="Jane.Doe@Example.COM" />);

    await waitFor(() => expect(posthog.identify).toHaveBeenCalledWith("u1", { email_domain: "example.com" }));
    expect(posthog.init).toHaveBeenCalledWith(
      "phc_test",
      expect.objectContaining({ api_host: "/ingest", ui_host: "https://eu.i.posthog.com" })
    );
    expect(JSON.stringify(posthog.identify.mock.calls)).not.toContain("Jane");
    expect(posthog.reset).not.toHaveBeenCalled();
  });

  test("starts a fresh person when another user signs in without a reload", async () => {
    window.__formaPostHogConfig = CONFIG;
    const { PostHogIdentify } = await load();

    const first = render(<PostHogIdentify userId="u1" email="a@example.com" />);
    await waitFor(() => expect(posthog.identify).toHaveBeenCalledTimes(1));
    first.unmount();
    render(<PostHogIdentify userId="u1" email="a@example.com" />);
    await waitFor(() => expect(posthog.identify).toHaveBeenCalledTimes(2));
    expect(posthog.reset).not.toHaveBeenCalled();

    cleanup();
    render(<PostHogIdentify userId="u2" email="b@example.org" />);

    await waitFor(() =>
      expect(posthog.identify).toHaveBeenLastCalledWith("u2", { email_domain: "example.org" })
    );
    expect(posthog.reset).toHaveBeenCalledTimes(1);
  });

  test("does nothing, and loads nothing, without a key", async () => {
    const { PostHogIdentify } = await load();

    render(<PostHogIdentify userId="u1" email="a@example.com" />);
    await Promise.resolve();

    expect(posthog.init).not.toHaveBeenCalled();
    expect(posthog.identify).not.toHaveBeenCalled();
  });

  test("emailDomain is the lowercased part after the @, empty without one", async () => {
    const { emailDomain } = await load();

    expect(emailDomain("Jane@Example.COM")).toBe("example.com");
    expect(emailDomain("no-at-sign")).toBe("");
    expect(emailDomain("a@")).toBe("");
  });
});

describe("PostHogGroupIdentify", () => {
  test("attaches the organization and workspace once PostHog has started", async () => {
    vi.useFakeTimers();
    window.__formaPostHogConfig = CONFIG;
    const { PostHogGroupIdentify, PostHogIdentify } = await load();

    render(
      <>
        <PostHogIdentify userId="u1" email="a@example.com" />
        <PostHogGroupIdentify
          organizationId="o1"
          organizationName="Acme"
          workspaceId="w1"
          workspaceName="Website"
        />
      </>
    );
    await vi.advanceTimersByTimeAsync(100);

    expect(posthog.group).toHaveBeenCalledWith("organization", "o1", { name: "Acme" });
    expect(posthog.group).toHaveBeenCalledWith("workspace", "w1", { name: "Website" });
    expect(posthog.group).toHaveBeenCalledTimes(2);
  });

  test("gives up on an install without a key", async () => {
    vi.useFakeTimers();
    const { PostHogGroupIdentify } = await load();

    render(
      <PostHogGroupIdentify
        organizationId="o1"
        organizationName="Acme"
        workspaceId="w1"
        workspaceName="Website"
      />
    );
    await vi.advanceTimersByTimeAsync(10_000);

    expect(posthog.group).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });
});
