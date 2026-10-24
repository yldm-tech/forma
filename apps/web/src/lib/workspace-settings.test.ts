import { expect, test } from "vite-plus/test";
import {
  cooldownError,
  hasMatchingWorkspaceDeleteConfirmation,
  truncate,
  workspaceNameError,
} from "./workspace-settings";

test("a workspace name must not be blank", () => {
  expect(workspaceNameError("  ")).toBe("Workspace name cannot be empty");
  expect(workspaceNameError(" Acme ")).toBeUndefined();
});

test("the cooldown period is 0 to 365 days", () => {
  expect(cooldownError("0")).toBeUndefined();
  expect(cooldownError("365")).toBeUndefined();
  expect(cooldownError("-1")).toBe("Must be a positive number");
  expect(cooldownError("366")).toBe("Must be less than 365");
  expect(cooldownError("")).toBeDefined();
});

test("the delete confirmation ignores case and surrounding space", () => {
  expect(hasMatchingWorkspaceDeleteConfirmation("  my WORKSPACE ", "My workspace")).toBe(true);
  expect(hasMatchingWorkspaceDeleteConfirmation("My workspac", "My workspace")).toBe(false);
});

test("truncate keeps short values and shortens long ones", () => {
  expect(truncate("abc", 3)).toBe("abc");
  expect(truncate("abcd", 3)).toBe("abc...");
});
