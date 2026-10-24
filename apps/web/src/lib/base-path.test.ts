import { expect, test } from "vite-plus/test";
import { APP_BASE_PATH, appHref } from "./base-path";

test("the base path is the Go server's config.AppBasePath", () => {
  expect(APP_BASE_PATH).toBe("/app");
});

test("an in-app path becomes the browser's path under the base", () => {
  expect(appHref("/")).toBe("/app/");
  expect(appHref("/workspaces/w1/surveys?x=1")).toBe("/app/workspaces/w1/surveys?x=1");
});
