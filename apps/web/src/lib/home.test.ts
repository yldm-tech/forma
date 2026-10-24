// @vitest-environment jsdom
import { describe, expect, test } from "vite-plus/test";
import { homeTarget, isOnboardingSkipped, skipOnboarding } from "./home";

test("the server's path wins", () => {
  expect(homeTarget({ path: "/auth/login", workspaceIds: [] }, "w1", () => false)).toBe("/auth/login");
});

test("the remembered workspace is reopened only while the user can open it", () => {
  const home = { workspaceIds: ["w1", "w2"] };
  expect(homeTarget(home, "w2")).toBe("/workspaces/w2/");
  expect(homeTarget(home, "gone")).toBe("/workspaces/w1/");
  expect(homeTarget(home, null)).toBe("/workspaces/w1/");
  expect(homeTarget({ workspaceIds: [] }, null)).toBeNull();
});

describe("a skipped onboarding", () => {
  const onboarding = { path: "/organizations/o1/workspaces/new/survey", workspaceIds: ["w1", "w2"] };

  test("is followed until the user skips it for that organization", () => {
    expect(homeTarget(onboarding, null, () => false)).toBe(onboarding.path);
    expect(homeTarget(onboarding, null, (id) => id === "other")).toBe(onboarding.path);
    expect(homeTarget(onboarding, "w2", (id) => id === "o1")).toBe("/workspaces/w2/");
  });

  test("leads to the organization when it has no workspace the user can open", () => {
    expect(homeTarget({ ...onboarding, workspaceIds: [] }, null, () => true)).toBe("/organizations/o1/");
  });

  test("never passes over any other path the server names", () => {
    expect(homeTarget({ path: "/setup/organization/create", workspaceIds: [] }, null, () => true)).toBe(
      "/setup/organization/create"
    );
  });

  test("is remembered per organization in this browser", () => {
    expect(isOnboardingSkipped("o9")).toBe(false);
    skipOnboarding("o9");
    expect(isOnboardingSkipped("o9")).toBe(true);
    expect(isOnboardingSkipped("o8")).toBe(false);
    expect(homeTarget({ ...onboarding, path: "/organizations/o9/workspaces/new/plan" }, null)).toBe("/workspaces/w1/");
    globalThis.localStorage.removeItem("forma.onboardingSkipped.o9");
  });
});
