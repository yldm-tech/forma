import { expect, test } from "vite-plus/test";
import { homeTarget } from "./home";

test("the server's path wins", () => {
  expect(homeTarget({ path: "/auth/login", workspaceIds: [] }, "w1")).toBe("/auth/login");
});

test("the remembered workspace is reopened only while the user can open it", () => {
  const home = { workspaceIds: ["w1", "w2"] };
  expect(homeTarget(home, "w2")).toBe("/workspaces/w2/");
  expect(homeTarget(home, "gone")).toBe("/workspaces/w1/");
  expect(homeTarget(home, null)).toBe("/workspaces/w1/");
  expect(homeTarget({ workspaceIds: [] }, null)).toBeNull();
});
