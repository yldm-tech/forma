// @vitest-environment happy-dom
import { afterEach, beforeEach, expect, test, vi } from "vite-plus/test";
import { FORMA_SURVEY_PAGE_ATTRIBUTE } from "./zod-jitless";

type ZodGlobal = { __zod_globalConfig?: Record<string, unknown> };

// Runs the module again as the script tag `script` would, the way the browser runs the runtime's bundle.
const loadRuntimeFrom = async (script: HTMLScriptElement | null) => {
  vi.resetModules();
  Object.defineProperty(document, "currentScript", { configurable: true, get: () => script });
  await import("./zod-jitless");
};

const runtimeScript = (attributes: Record<string, string> = {}) => {
  const script = document.createElement("script");
  script.src = "https://forma.example/js/surveys.umd.js";
  for (const [name, value] of Object.entries(attributes)) script.setAttribute(name, value);
  return script;
};

const zodGlobal = () => (globalThis as ZodGlobal).__zod_globalConfig ?? {};

beforeEach(() => {
  delete zodGlobal().jitless;
});

afterEach(() => {
  delete (document as { currentScript?: unknown }).currentScript;
  delete zodGlobal().jitless;
});

// The SDK appends the runtime to the customer's own document, where zod's global config also drives the customer's schemas.
test("leaves zod's page-wide config alone on a customer's site", async () => {
  await loadRuntimeFrom(runtimeScript());
  expect(zodGlobal().jitless).toBeUndefined();
});

test("keeps a customer's own jitless setting", async () => {
  zodGlobal().jitless = false;
  await loadRuntimeFrom(runtimeScript());
  expect(zodGlobal().jitless).toBe(false);
});

// Loaded as a module (no current script), as in the admin app's bundle or a test.
test("leaves the config alone without a script tag", async () => {
  await loadRuntimeFrom(null);
  expect(zodGlobal().jitless).toBeUndefined();
});

// Forma's survey page, whose Content-Security-Policy refuses zod's eval probe.
test("turns off zod's eval probe on Forma's survey page", async () => {
  await loadRuntimeFrom(runtimeScript({ [FORMA_SURVEY_PAGE_ATTRIBUTE]: "" }));
  expect(zodGlobal().jitless).toBe(true);
});
