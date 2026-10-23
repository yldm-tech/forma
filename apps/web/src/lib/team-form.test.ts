import { expect, test } from "vite-plus/test";
import { remainingOptions, toTeamUpdate, validateTeamForm } from "./team-form";

const valid = {
  name: " Support ",
  members: [{ userId: "u1", role: "admin" as const }],
  workspaces: [{ workspaceId: "w1", permission: "read" as const }],
};

test("validateTeamForm carries forma2's messages", () => {
  expect(validateTeamForm(valid)).toEqual({});
  expect(validateTeamForm({ ...valid, name: "  " })).toEqual({ name: "Team name is required" });
  expect(validateTeamForm({ ...valid, members: [] }).members).toBe("Please add at least one member");
  expect(validateTeamForm({ ...valid, members: [{ userId: "", role: "contributor" }] }).members).toBe(
    "Please select a member"
  );
  expect(validateTeamForm({ ...valid, workspaces: [] }).workspaces).toBe("Please add at least one workspace");
  expect(
    validateTeamForm({ ...valid, workspaces: [{ workspaceId: "", permission: "read" }] }).workspaces
  ).toBe("Please select a workspace");
});

test("toTeamUpdate trims the name and drops empty rows", () => {
  expect(
    toTeamUpdate({ ...valid, members: [...valid.members, { userId: "", role: "contributor" }] })
  ).toEqual({ name: "Support", members: valid.members, workspaces: valid.workspaces });
});

test("remainingOptions leaves out what other rows chose, keeps this row's choice, sorts by name", () => {
  const all = [
    { id: "b", name: "beta" },
    { id: "a", name: "Alpha" },
    { id: "c", name: "gamma" },
  ];
  expect(remainingOptions(all, ["a", "c"], "c")).toEqual([
    { value: "b", label: "beta" },
    { value: "c", label: "gamma" },
  ]);
});
