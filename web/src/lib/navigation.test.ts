import { expect, test } from "vite-plus/test";
import {
  loginHref,
  organizationSwitchPath,
  sortByName,
  workspaceSettingsPath,
  workspaceSwitchPath,
} from "./navigation";

test("loginHref carries the absolute address to come back to", () => {
  expect(loginHref("/organizations/o1/settings/teams?x=1", "https://app.example.com")).toBe(
    "/auth/login?callbackUrl=https%3A%2F%2Fapp.example.com%2Forganizations%2Fo1%2Fsettings%2Fteams%3Fx%3D1"
  );
});

test("choosing the current organization opens its settings, another opens it", () => {
  expect(organizationSwitchPath("o1", "o1")).toBe("/organizations/o1/settings/general");
  expect(organizationSwitchPath("o2", "o1")).toBe("/organizations/o2/");
});

test("sortByName does not reorder its input", () => {
  const items = [{ name: "b" }, { name: "a" }];
  expect(sortByName(items).map((i) => i.name)).toEqual(["a", "b"]);
  expect(items[0]?.name).toBe("b");
});

test("workspace settings live under the workspace", () => {
  expect(workspaceSettingsPath("w1", "languages")).toBe("/workspaces/w1/settings/workspace/languages");
});

test("switching workspace keeps the settings page", () => {
  expect(workspaceSwitchPath("/workspaces/w1/settings/workspace/look", "w1", "w2")).toBe(
    "/workspaces/w2/settings/workspace/look"
  );
});
