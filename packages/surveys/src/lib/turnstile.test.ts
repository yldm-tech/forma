// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, test, vi } from "vite-plus/test";
import {
  TURNSTILE_SCRIPT_ID,
  TURNSTILE_RETRY_WAIT_MS,
  TURNSTILE_SCRIPT_URL,
  TurnstileChallenge,
  type TurnstileApi,
  type TurnstileRenderOptions,
  loadTurnstile,
  turnstileLanguage,
} from "./turnstile";

// A stand-in for window.turnstile that records the widget's options, so a test can play Cloudflare's callbacks.
const fakeTurnstile = () => {
  let options: TurnstileRenderOptions | undefined;
  let expired = false;
  const api: TurnstileApi = {
    render: vi.fn((_container: HTMLElement, o: TurnstileRenderOptions) => {
      options = o;
      return "widget-1";
    }),
    getResponse: vi.fn(),
    isExpired: vi.fn(() => expired),
    reset: vi.fn(),
    remove: vi.fn(),
  };
  return {
    api,
    options: () => options!,
    expire: () => {
      expired = true;
    },
    refresh: () => {
      expired = false;
    },
  };
};

describe("turnstileLanguage", () => {
  test.each([
    ["en-US", "en"],
    ["de", "de"],
    ["zh-Hans-CN", "zh-cn"],
    ["zh-Hans", "zh-cn"],
    ["zh-Hant-TW", "zh-tw"],
    ["zh-TW", "zh-tw"],
    ["zh-HK", "zh-tw"],
    ["pt-BR", "pt-br"],
    ["pt-PT", "pt"],
    ["ja-JP", "ja"],
    ["km-KH", "auto"],
    [null, "auto"],
    ["", "auto"],
  ])("%s shows Turnstile in %s", (tag, want) => {
    expect(turnstileLanguage(tag)).toBe(want);
  });
});

describe("TurnstileChallenge", () => {
  test("renders an interaction-only widget in the survey's language", async () => {
    const t = fakeTurnstile();
    const challenge = new TurnstileChallenge("site-key", "de", async () => t.api);
    const container = document.createElement("div");
    await challenge.mount(container);
    expect(t.api.render).toHaveBeenCalledWith(container, expect.anything());
    expect(t.options()).toMatchObject({
      sitekey: "site-key",
      language: "de",
      appearance: "interaction-only",
      size: "flexible",
      action: "submit_response",
    });
  });

  test("waits for the challenge, then hands out its token once", async () => {
    const t = fakeTurnstile();
    const challenge = new TurnstileChallenge("site-key", "en", async () => t.api);
    await challenge.mount(document.createElement("div"));
    const pending = challenge.getToken();
    t.options().callback!("token-1");
    await expect(pending).resolves.toBe("token-1");
    expect(t.api.reset).not.toHaveBeenCalled();

    // Cloudflare accepts a token once: the next create (a retry of one that failed) gets a fresh challenge, not the spent token.
    let second: string | null | undefined;
    const secondCall = challenge.getToken().then((token) => (second = token));
    await Promise.resolve();
    expect(second).toBeUndefined();
    expect(t.api.reset).toHaveBeenCalledWith("widget-1");
    t.options().callback!("token-2");
    await secondCall;
    expect(second).toBe("token-2");
  });

  test("two creates waiting at once get a token each", async () => {
    const t = fakeTurnstile();
    const challenge = new TurnstileChallenge("site-key", "en", async () => t.api);
    await challenge.mount(document.createElement("div"));
    const first = challenge.getToken();
    const second = challenge.getToken();
    t.options().callback!("token-1");
    await expect(first).resolves.toBe("token-1");
    await vi.waitFor(() => expect(t.api.reset).toHaveBeenCalledTimes(1));
    t.options().callback!("token-2");
    await expect(second).resolves.toBe("token-2");
  });

  test("whenReady waits for a token without taking it", async () => {
    const t = fakeTurnstile();
    const challenge = new TurnstileChallenge("site-key", "en", async () => t.api);
    await challenge.mount(document.createElement("div"));
    const ready = challenge.whenReady();
    t.options().callback!("token-1");
    await expect(ready).resolves.toBe(true);
    await expect(challenge.whenReady()).resolves.toBe(true);
    await expect(challenge.getToken()).resolves.toBe("token-1");

    // After the token went with a create, readiness means a fresh one.
    const readyAgain = challenge.whenReady();
    expect(t.api.reset).toHaveBeenCalledWith("widget-1");
    t.options().callback!("token-2");
    await expect(readyAgain).resolves.toBe(true);
    await expect(challenge.getToken()).resolves.toBe("token-2");
  });

  test("an expired token is not sent: the next one Turnstile issues is", async () => {
    const t = fakeTurnstile();
    const challenge = new TurnstileChallenge("site-key", "en", async () => t.api);
    await challenge.mount(document.createElement("div"));
    t.options().callback!("token-1");
    t.expire();
    const next = challenge.getToken();
    t.refresh();
    t.options().callback!("token-2");
    await expect(next).resolves.toBe("token-2");
  });

  test("the expired callback drops the token until Turnstile issues another", async () => {
    const t = fakeTurnstile();
    const challenge = new TurnstileChallenge("site-key", "en", async () => t.api);
    await challenge.mount(document.createElement("div"));
    t.options().callback!("token-1");
    t.options()["expired-callback"]!();
    const next = challenge.getToken();
    t.options().callback!("token-2");
    await expect(next).resolves.toBe("token-2");
  });

  test("a configuration error fails the waiting submit and the next ones, until a later success", async () => {
    const t = fakeTurnstile();
    const challenge = new TurnstileChallenge("site-key", "en", async () => t.api);
    await challenge.mount(document.createElement("div"));
    const pending = challenge.getToken();
    expect(t.options()["error-callback"]!("110200")).toBe(true);
    await expect(pending).resolves.toBeNull();
    await expect(challenge.getToken()).resolves.toBeNull();
    t.options().callback!("token-after-retry");
    await expect(challenge.getToken()).resolves.toBe("token-after-retry");
  });

  test("an error Turnstile retries keeps the submit waiting for the retry", async () => {
    vi.useFakeTimers();
    try {
      const t = fakeTurnstile();
      const challenge = new TurnstileChallenge("site-key", "en", async () => t.api);
      await challenge.mount(document.createElement("div"));
      const pending = challenge.getToken();
      expect(t.options()["error-callback"]!("300010")).toBe(true);
      let settled = false;
      void pending.then(() => (settled = true));
      await vi.advanceTimersByTimeAsync(8_000);
      expect(settled).toBe(false);
      t.options().callback!("token-after-retry");
      await expect(pending).resolves.toBe("token-after-retry");
    } finally {
      vi.useRealTimers();
    }
  });

  test("an error Turnstile retries fails the submit when no retry succeeds in time", async () => {
    vi.useFakeTimers();
    try {
      const t = fakeTurnstile();
      const challenge = new TurnstileChallenge("site-key", "en", async () => t.api);
      await challenge.mount(document.createElement("div"));
      const pending = challenge.getToken();
      t.options()["error-callback"]!("600010");
      await vi.advanceTimersByTimeAsync(8_000);
      // Turnstile's own retry fails again: the wait is still counted from the first error.
      t.options()["error-callback"]!("600010");
      await vi.advanceTimersByTimeAsync(TURNSTILE_RETRY_WAIT_MS - 8_000);
      await expect(pending).resolves.toBeNull();
      await expect(challenge.getToken()).resolves.toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  test("a script that does not load fails every submit", async () => {
    const challenge = new TurnstileChallenge("site-key", "en", () => Promise.reject(new Error("blocked")));
    await challenge.mount(document.createElement("div"));
    await expect(challenge.getToken()).resolves.toBeNull();
  });

  test("an unsupported browser fails the submit", async () => {
    const t = fakeTurnstile();
    const challenge = new TurnstileChallenge("site-key", "en", async () => t.api);
    await challenge.mount(document.createElement("div"));
    t.options()["unsupported-callback"]!();
    await expect(challenge.getToken()).resolves.toBeNull();
  });

  test("tells the widget when Cloudflare needs the respondent", async () => {
    const t = fakeTurnstile();
    const onInteractive = vi.fn();
    const challenge = new TurnstileChallenge("site-key", "en", async () => t.api);
    await challenge.mount(document.createElement("div"), onInteractive);
    t.options()["before-interactive-callback"]!();
    t.options()["after-interactive-callback"]!();
    expect(onInteractive.mock.calls).toEqual([[true], [false]]);
  });

  test("remove takes the widget away and releases a waiting submit", async () => {
    const t = fakeTurnstile();
    const challenge = new TurnstileChallenge("site-key", "en", async () => t.api);
    await challenge.mount(document.createElement("div"));
    const pending = challenge.getToken();
    challenge.remove();
    expect(t.api.remove).toHaveBeenCalledWith("widget-1");
    await expect(pending).resolves.toBeNull();
  });
});

describe("loadTurnstile", () => {
  // The document as far as the loader sees it, without happy-dom fetching Cloudflare's script: tags appended to head, and the ones removed again.
  let appended: HTMLScriptElement[];
  let removed: Set<Element>;
  beforeEach(() => {
    appended = [];
    removed = new Set();
    vi.spyOn(document.head, "appendChild").mockImplementation(<T extends Node>(node: T) => {
      appended.push(node as unknown as HTMLScriptElement);
      return node;
    });
    vi.spyOn(document, "getElementById").mockImplementation(
      (id: string) => appended.find((script) => script.id === id && !removed.has(script)) ?? null
    );
    vi.spyOn(Element.prototype, "remove").mockImplementation(function (this: Element) {
      removed.add(this);
    });
  });
  afterEach(() => {
    vi.restoreAllMocks();
    delete window.turnstile;
  });

  test("adds Cloudflare's explicit-render script once and resolves with the API it defines", async () => {
    const loading = loadTurnstile();
    const second = loadTurnstile();
    expect(appended).toHaveLength(1);
    const [script] = appended;
    expect(script.id).toBe(TURNSTILE_SCRIPT_ID);
    expect(script.src).toBe(TURNSTILE_SCRIPT_URL);
    const api = fakeTurnstile().api;
    window.turnstile = api;
    script.dispatchEvent(new Event("load"));
    await expect(loading).resolves.toBe(api);
    await expect(second).resolves.toBe(api);
    await expect(loadTurnstile()).resolves.toBe(api);
    expect(appended).toHaveLength(1);
  });

  test("rejects and drops the tag when the script fails, so a later call tries again", async () => {
    const loading = loadTurnstile();
    appended[0].dispatchEvent(new Event("error"));
    await expect(loading).rejects.toThrow("Turnstile did not load");
    expect(removed.has(appended[0])).toBe(true);
    void loadTurnstile().catch(() => {});
    expect(appended).toHaveLength(2);
  });
});
