import { beforeEach, describe, expect, test, vi } from "vitest";
import { OperationNotAllowedError } from "@forma/types/errors";
import { can } from "@/lib/authorization";
import { assertCanManageOrganizationUsers } from "./user-management-access";

vi.mock("@/lib/authorization", () => ({ can: vi.fn() }));

const mockedCan = vi.mocked(can);

describe("assertCanManageOrganizationUsers", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  test("asks for organization.manage_access, the action carrying USER_MANAGEMENT_MINIMUM_ROLE", async () => {
    mockedCan.mockResolvedValue(true);

    await assertCanManageOrganizationUsers("user-1", "org-1");

    // Asking `organization.manage` instead is what made the deployment policy unreachable: that
    // permission is owner+manager fixed in the schema and carries no policy input.
    expect(mockedCan).toHaveBeenCalledWith({ type: "user", id: "user-1" }, "organization.manage_access", {
      type: "organization",
      id: "org-1",
    });
  });

  test("refuses when the policy denies, even for a role holding organization.manage", async () => {
    mockedCan.mockResolvedValue(false);

    await expect(assertCanManageOrganizationUsers("user-1", "org-1")).rejects.toThrow(
      OperationNotAllowedError
    );
  });

  test("allows when the policy permits", async () => {
    mockedCan.mockResolvedValue(true);

    await expect(assertCanManageOrganizationUsers("user-1", "org-1")).resolves.toBeUndefined();
  });
});
