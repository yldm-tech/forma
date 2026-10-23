/// <reference types="node" />
import { defineConfig, devices } from "@playwright/test";

/**
 * The browser suite in e2e/, forma2's playwright.config.ts. It drives a running server rather than starting one: scripts/ci-stack.sh brings up `forma start` with its dependencies (see .github/workflows/e2e.yml), and E2E_BASE_URL points elsewhere when the server runs somewhere else. Every spec creates the account it works in, so specs run in parallel and in any order against one instance.
 */
const baseURL = process.env.E2E_BASE_URL ?? "http://localhost:3000";

export default defineConfig({
  testDir: ".",
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  // One retry on CI, with a trace of it, so a failure that only happens once is still visible in the report.
  retries: process.env.CI ? 1 : 0,
  timeout: 90_000,
  expect: { timeout: 15_000 },
  // The GitHub runner has four cores and also runs the server and its dependencies.
  workers: process.env.CI ? 3 : undefined,
  reporter: process.env.CI
    ? [["github"], ["html", { outputFolder: "playwright-report", open: "never" }]]
    : [["list"]],
  use: {
    baseURL,
    trace: "on-first-retry",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
