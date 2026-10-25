import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import { expect, test } from "vite-plus/test";
import { SDK_STUB_METHODS, sdkReferenceUrl, sdkSnippets } from "./sdk-snippets";

const WORKSPACE_ID = "0190a000-0000-7000-8000-000000000002";
const APP_URL = "https://forma.example.com";

/** The JavaScript inside the HTML snippet's <script> tag. */
const htmlScript = (html: string): string =>
  html.replace(/<!--[^>]*-->/g, "").replace(/<\/?script[^>]*>/g, "");

/** The JavaScript the Next.js component hands to next/script, between the template literal's backticks. */
const nextScript = (nextjs: string): string => {
  const match = /\{`([\s\S]*)`\}/.exec(nextjs);
  if (!match?.[1]) throw new Error("no inline script in the Next.js snippet");
  return match[1];
};

interface FakeScript {
  onload?: () => void;
  src?: string;
}

/** Runs a loader against a fake page and returns the inserted script tags and the page's window. */
const runLoader = (source: string, window: { forma?: unknown } = {}) => {
  const inserted: FakeScript[] = [];
  const document = {
    createElement: () => {
      const element: FakeScript = {};
      inserted.push(element);
      return element;
    },
    getElementsByTagName: () => [{ parentNode: { insertBefore: () => undefined } }],
  };
  runInNewContext(source, { window, document });
  return { inserted, window };
};

/** A stand-in for the SDK's globalThis.forma that records every call in order. */
const fakeSdk = (calls: unknown[][]) => {
  const sdk: Record<string, (...args: unknown[]) => void> = {
    setup: (...args) => calls.push(["setup", ...args]),
  };
  for (const method of SDK_STUB_METHODS) sdk[method] = (...args) => calls.push([method, ...args]);
  return sdk;
};

test("every loader names the workspace and the app URL", () => {
  const snippets = sdkSnippets(WORKSPACE_ID, APP_URL);
  for (const snippet of [snippets.html, snippets.nextjs]) {
    expect(snippet).toContain(WORKSPACE_ID);
    expect(snippet).toContain(APP_URL);
  }
});

// No Forma package is published on npm, for mobile or for WordPress; a snippet that installs or imports one fails at its first line.
test("no snippet imports or installs a package that does not exist", () => {
  const snippets = sdkSnippets(WORKSPACE_ID, APP_URL);
  for (const snippet of Object.values(snippets)) {
    expect(snippet).not.toMatch(/@forma\//);
    expect(snippet).not.toMatch(/npm install|pub add|implementation\(|import FormaSDK/);
  }
});

// .cjs is not on Cloudflare's cacheable-extension list, so the old URL was fetched from origin on every first visit. The .cjs copy stays for snippets already installed.
test("the loader fetches the CDN-cacheable bundle", () => {
  const { html } = sdkSnippets(WORKSPACE_ID, APP_URL);
  const { inserted } = runLoader(htmlScript(html));
  expect(inserted).toHaveLength(1);
  expect(inserted[0]?.src).toBe(`${APP_URL}/js/forma.umd.js`);
});

test("the HTML snippet sets the SDK up once its script has loaded, not after a fixed delay", () => {
  const { html } = sdkSnippets("ws-1", APP_URL);
  expect(html).not.toContain("setTimeout");
  const { inserted, window } = runLoader(htmlScript(html));
  const calls: unknown[][] = [];
  // The script arrives late: until then nothing has called setup.
  window.forma = fakeSdk(calls);
  expect(calls).toEqual([]);
  inserted[0]?.onload?.();
  expect(calls).toEqual([["setup", { workspaceId: "ws-1", appUrl: APP_URL }]]);
});

test("calls made before the SDK has loaded are queued, not thrown, and replayed in order after setup", () => {
  const { html } = sdkSnippets("ws-1", APP_URL);
  const { inserted, window } = runLoader(htmlScript(html));
  const stub = window.forma as Record<string, (...args: unknown[]) => unknown>;
  const handler = () => undefined;
  stub.setUserId?.("user-1");
  stub.on?.("forma_setup_successful", handler);
  stub.setAttributes?.({ plan: "pro" });
  stub.setNonce?.("n0nce");
  stub.track?.("upgrade_clicked", { hiddenFields: { source: "pricing" } });

  const calls: unknown[][] = [];
  window.forma = fakeSdk(calls);
  inserted[0]?.onload?.();

  // on() and setNonce() run first, so the handler sees forma_setup_successful; the command-queue methods run after setup, which would otherwise drop them as "Setup not complete".
  expect(calls).toEqual([
    ["on", "forma_setup_successful", handler],
    ["setNonce", "n0nce"],
    ["setup", { workspaceId: "ws-1", appUrl: APP_URL }],
    ["setUserId", "user-1"],
    ["setAttributes", { plan: "pro" }],
    ["track", "upgrade_clicked", { hiddenFields: { source: "pricing" } }],
  ]);
});

test("a second copy of the snippet, or one running after the SDK is loaded, adds no second script", () => {
  const { html } = sdkSnippets("ws-1", APP_URL);
  const first = runLoader(htmlScript(html));
  const again = runLoader(htmlScript(html), first.window);
  expect(again.inserted).toHaveLength(0);

  const loaded = runLoader(htmlScript(html), { forma: fakeSdk([]) });
  expect(loaded.inserted).toHaveLength(0);
});

// A GTM Custom HTML tag rejects ES2015 syntax, and the same loader is what GTM users paste.
test("the loader is ES5", () => {
  const source = htmlScript(sdkSnippets(WORKSPACE_ID, APP_URL).html);
  expect(source).not.toMatch(/=>|\blet\b|\bconst\b|`|\bclass\b/);
});

// useSearchParams in the root layout without a Suspense boundary fails `next build`, and the route-change effect it fed is redundant: the SDK watches history itself.
test("the Next.js snippet uses no routing hooks and runs the same loader", () => {
  const { nextjs, html } = sdkSnippets("ws-1", APP_URL);
  expect(nextjs).toContain('import Script from "next/script"');
  expect(nextjs).not.toMatch(/usePathname|useSearchParams|registerRouteChange\(\)|useEffect|"use client"/);
  expect(nextScript(nextjs)).toBe(htmlScript(html).trim());

  const { inserted, window } = runLoader(nextScript(nextjs));
  const calls: unknown[][] = [];
  window.forma = fakeSdk(calls);
  inserted[0]?.onload?.();
  expect(calls).toEqual([["setup", { workspaceId: "ws-1", appUrl: APP_URL }]]);
});

test("the usage snippet calls the global the loader defines", () => {
  const { usage } = sdkSnippets(WORKSPACE_ID, APP_URL);
  expect(usage).toContain("interface Window");
  for (const call of ["window.forma.setUserId(", "window.forma.track(", "window.forma.logout("]) {
    expect(usage).toContain(call);
  }
});

// The website shows the same snippet with placeholders (apps/website/src/site.ts JS_SNIPPET); a fix to one that misses the other would teach visitors a loader the app no longer gives them.
test("the website's snippet is the same loader", () => {
  const site = readFileSync(new URL("../../../website/src/site.ts", import.meta.url), "utf8");
  const lines = htmlScript(sdkSnippets(WORKSPACE_ID, APP_URL).html)
    .split("\n")
    .filter((line) => line.trim() !== "" && !line.includes(WORKSPACE_ID) && !line.includes(APP_URL));
  expect(lines.length).toBeGreaterThan(3);
  expect(lines.filter((line) => !site.includes(line))).toEqual([]);
});

test("the SDK reference link follows the app's language to the website's closest locale", () => {
  expect(sdkReferenceUrl("en-US")).toBe("https://forma.yldm.ai/developers/sdk/");
  expect(sdkReferenceUrl("ja-JP")).toBe("https://forma.yldm.ai/ja/developers/sdk/");
  expect(sdkReferenceUrl("zh-Hans-CN")).toBe("https://forma.yldm.ai/zh/developers/sdk/");
  expect(sdkReferenceUrl("zh-Hant-TW")).toBe("https://forma.yldm.ai/zh/developers/sdk/");
});
