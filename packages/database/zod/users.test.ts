import { describe, expect, test } from "vitest";
import { ZUser } from "./users";

const baseUser = {
  id: "clv1abcd2efgh3ijkl4mnop5",
  createdAt: new Date("2024-01-01T00:00:00.000Z"),
  updatedAt: new Date("2024-01-01T00:00:00.000Z"),
  isActive: true,
  name: "John Doe",
  email: "john@example.com",
  role: "member",
  teams: [],
};

describe("ZUser", () => {
  test("accepts a user who has never logged in", () => {
    const result = ZUser.safeParse({ ...baseUser, lastLoginAt: null });

    expect(result.success).toBe(true);
    expect(result.data?.lastLoginAt).toBeNull();
  });

  test("still accepts a last login date", () => {
    const result = ZUser.safeParse({ ...baseUser, lastLoginAt: new Date("2024-06-01T00:00:00.000Z") });

    expect(result.success).toBe(true);
    expect(result.data?.lastLoginAt).toEqual(new Date("2024-06-01T00:00:00.000Z"));
  });

  test("requires the field to be present", () => {
    expect(ZUser.safeParse(baseUser).success).toBe(false);
  });
});
