import { type Mock, afterEach, beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { TURNSTILE_SCRIPT_ID, TURNSTILE_SCRIPT_URL } from "@/lib/common/constants";
import { loadTurnstileScript } from "../turnstile";

vi.mock("../logger", () => ({
  Logger: { getInstance: () => ({ debug: vi.fn() }) },
}));

// A script tag as the loader uses it: listeners a test can fire, and remove().
interface FakeScript {
  id: string;
  src: string;
  async: boolean;
  addEventListener: Mock;
  remove: Mock;
  fire: (type: "load" | "error") => void;
}

const fakeScript = (): FakeScript => {
  const listeners: Record<string, (() => void)[]> = {};
  return {
    id: "",
    src: "",
    async: false,
    addEventListener: vi.fn((type: string, listener: () => void) => {
      (listeners[type] ??= []).push(listener);
    }),
    remove: vi.fn(),
    fire: (type) => {
      for (const listener of listeners[type] ?? []) listener();
    },
  };
};

type TurnstileWindow = { turnstile?: unknown };

describe("loadTurnstileScript", () => {
  // The document as far as the loader sees it: the tags appended to head, minus the ones removed again.
  let appended: FakeScript[];
  beforeEach(() => {
    appended = [];
    vi.mocked(document.createElement).mockImplementation(() => fakeScript() as unknown as HTMLElement);
    vi.mocked(document.head.appendChild).mockImplementation(<T extends Node>(node: T) => {
      appended.push(node as unknown as FakeScript);
      return node;
    });
    vi.mocked(document.getElementById).mockImplementation(
      (id: string) =>
        (appended.find((script) => script.id === id && script.remove.mock.calls.length === 0) ??
          null) as unknown as HTMLElement | null
    );
  });
  afterEach(() => {
    delete (window as unknown as TurnstileWindow).turnstile;
  });

  test("resolves at once when Turnstile is already on the page", async () => {
    (window as unknown as TurnstileWindow).turnstile = {};
    await expect(loadTurnstileScript()).resolves.toBeUndefined();
    expect(appended).toHaveLength(0);
  });

  test("adds the explicit-render script the surveys runtime looks for, once", async () => {
    const loading = loadTurnstileScript();
    const second = loadTurnstileScript();
    expect(appended).toHaveLength(1);
    expect(appended[0]).toMatchObject({ id: TURNSTILE_SCRIPT_ID, src: TURNSTILE_SCRIPT_URL, async: true });
    (window as unknown as TurnstileWindow).turnstile = {};
    appended[0].fire("load");
    await expect(loading).resolves.toBeUndefined();
    await expect(second).resolves.toBeUndefined();
  });

  test("rejects and drops the tag when the script fails, so the next survey tries again", async () => {
    const loading = loadTurnstileScript();
    appended[0].fire("error");
    await expect(loading).rejects.toThrow("Error loading Turnstile script");
    expect(appended[0].remove).toHaveBeenCalled();
    void loadTurnstileScript().catch(() => {});
    expect(appended).toHaveLength(2);
  });

  test("a script that loads without defining Turnstile counts as failed", async () => {
    const loading = loadTurnstileScript();
    appended[0].fire("load");
    await expect(loading).rejects.toThrow("Error loading Turnstile script");
  });
});
