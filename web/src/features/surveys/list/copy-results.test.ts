import { describe, expect, test } from "vite-plus/test";
import { copyTargets, summarizeCopies } from "./copy-results";

const workspaces = [
  { id: "w2", name: "Beta" },
  { id: "w1", name: "Alpha" },
  { id: "w3", name: "Gamma" },
];

describe("copyTargets", () => {
  test("leaves out the survey's own workspace and sorts the rest by name", () => {
    expect(copyTargets(workspaces, "w3").map((w) => w.name)).toEqual(["Alpha", "Beta"]);
  });
});

describe("summarizeCopies", () => {
  test("counts the successes and names each failed workspace with its error", () => {
    const error = new Error("Source and target workspaces must be in the same organization");
    const outcome = summarizeCopies(
      ["w1", "w2", "unknown"],
      [
        { status: "fulfilled", value: {} },
        { status: "rejected", reason: error },
        { status: "rejected", reason: "boom" },
      ],
      workspaces
    );
    expect(outcome.successCount).toBe(1);
    expect(outcome.failures).toEqual([
      { workspaceId: "w2", workspaceName: "Beta", error },
      // A workspace that is no longer listed is named by its id.
      { workspaceId: "unknown", workspaceName: "unknown", error: "boom" },
    ]);
  });
});
