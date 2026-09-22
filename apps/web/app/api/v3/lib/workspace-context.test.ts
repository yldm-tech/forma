import { describe, expect, test, vi } from "vitest";
import { prisma } from "@forma/database";
import type { Workspace } from "@forma/database/prisma";
import { ResourceNotFoundError } from "@forma/types/errors";
import { resolveV3WorkspaceContext } from "./workspace-context";

// The real `getWorkspace` runs here on purpose: the point of these tests is how many database reads one
// resolution costs, which a mock of the service layer would hide.
vi.mock("@forma/database", () => ({
  prisma: {
    workspace: {
      findUnique: vi.fn(),
    },
  },
}));

vi.mock("@/lib/authorization/resource-list", () => ({
  lookupAuthorizedOrganizationIds: vi.fn(),
  lookupAuthorizedWorkspaceIds: vi.fn(),
}));

const mockWorkspace = {
  id: "ws_abc",
  createdAt: new Date(),
  updatedAt: new Date(),
  name: "Test Workspace",
  organizationId: "org_123",
  languages: [],
  recontactDays: 0,
  linkSurveyBranding: false,
  inAppSurveyBranding: false,
  config: { channel: "link", industry: "saas" },
  placement: "bottomRight",
  clickOutsideClose: false,
  overlay: "none",
  appSetupCompleted: false,
  styling: { allowStyleOverwrite: false },
  logo: null,
} as unknown as Workspace;

describe("resolveV3WorkspaceContext", () => {
  test("returns workspaceId and organizationId when workspace exists", async () => {
    vi.mocked(prisma.workspace.findUnique).mockResolvedValue(mockWorkspace);

    const result = await resolveV3WorkspaceContext("ws_abc");

    expect(result).toEqual({
      workspaceId: "ws_abc",
      organizationId: "org_123",
    });
  });

  test("reads the workspace row once, taking organizationId from the row it already fetched", async () => {
    vi.mocked(prisma.workspace.findUnique).mockResolvedValue(mockWorkspace);

    await resolveV3WorkspaceContext("ws_abc");

    expect(prisma.workspace.findUnique).toHaveBeenCalledExactlyOnceWith({
      where: { id: "ws_abc" },
      select: expect.any(Object),
    });
  });

  test("throws when workspace does not exist", async () => {
    vi.mocked(prisma.workspace.findUnique).mockResolvedValue(null);

    await expect(resolveV3WorkspaceContext("ws_nonexistent")).rejects.toThrow(ResourceNotFoundError);
  });
});
