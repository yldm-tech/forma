import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { prisma } from "@forma/database";
import { Prisma } from "@forma/database/prisma";
import { DatabaseError, ResourceNotFoundError } from "@forma/types/errors";
import { lookupAuthorizedOrganizationIds } from "@/lib/authorization/resource-list";
import { reconcileApiKeyRelationships } from "@/lib/authzed/api-key";
import { deleteOrganizationRelationships } from "@/lib/authzed/organization-membership";
import { reconcileTeamWorkspaceRelationships } from "@/lib/authzed/team-workspace";
import { IS_FORMA_CLOUD } from "@/lib/constants";
import { updateUser } from "@/lib/user/service";
import { getWorkspaces } from "@/lib/workspace/service";
import {
  cleanupStripeCustomer,
  ensureCloudStripeSetupForOrganization,
} from "@/modules/billing/lib/organization-billing";
import {
  createOrganization,
  deleteOrganization,
  getMonthlyOrganizationWorkflowRunCount,
  getOrganization,
  getOrganizationByWorkspaceId,
  getOrganizationsByUserId,
  select as organizationSelect,
  subscribeOrganizationMembersToSurveyResponses,
  updateOrganization,
} from "./service";

vi.mock("@forma/database", () => ({
  prisma: {
    $transaction: vi.fn(),
    organization: {
      findUnique: vi.fn(),
      findFirst: vi.fn(),
      findMany: vi.fn(),
      create: vi.fn(),
      update: vi.fn(),
      delete: vi.fn(),
    },
    organizationBilling: {
      upsert: vi.fn(),
    },
    user: {
      findUnique: vi.fn(),
    },
    workflowRun: {
      aggregate: vi.fn(),
    },
  },
}));

vi.mock("@/lib/user/service", () => ({
  updateUser: vi.fn(),
}));

vi.mock("@/lib/workspace/service", () => ({
  getWorkspaces: vi.fn(),
}));
vi.mock("@/lib/authorization/resource-list", () => ({ lookupAuthorizedOrganizationIds: vi.fn() }));

vi.mock("@/lib/authzed/organization-membership", () => ({
  deleteOrganizationRelationships: vi.fn(),
}));
vi.mock("@/lib/authzed/api-key", () => ({
  reconcileApiKeyRelationships: vi.fn(),
}));
vi.mock("@/lib/authzed/team-workspace", () => ({
  reconcileTeamWorkspaceRelationships: vi.fn(),
}));

vi.mock("@/modules/billing/lib/organization-billing", () => ({
  ensureCloudStripeSetupForOrganization: vi.fn().mockResolvedValue(undefined),
  cleanupStripeCustomer: vi.fn().mockResolvedValue(undefined),
}));

describe("Organization Service", () => {
  beforeEach(() => {
    vi.mocked(ensureCloudStripeSetupForOrganization).mockResolvedValue(undefined);
    vi.mocked(lookupAuthorizedOrganizationIds).mockResolvedValue(["org1"]);
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  describe("getOrganization", () => {
    test("should return organization when found", async () => {
      const mockOrganization = {
        id: "org1",
        name: "Test Org",
        createdAt: new Date(),
        updatedAt: new Date(),
        billing: {
          limits: {
            workspaces: 3,
            monthly: {
              responses: 1500,
            },
          },
          stripeCustomerId: null,
          usageCycleAnchor: new Date(),
        },
        isAISmartToolsEnabled: false,
        displayTimeZone: null,
        whitelabel: false,
      };

      vi.mocked(prisma.organization.findUnique).mockResolvedValue(mockOrganization);

      const result = await getOrganization("org1");

      expect(result).toEqual(mockOrganization);
      expect(prisma.organization.findUnique).toHaveBeenCalledWith({
        where: { id: "org1" },
        select: expect.any(Object),
      });
    });

    test("should return null when organization not found", async () => {
      vi.mocked(prisma.organization.findUnique).mockResolvedValue(null);

      const result = await getOrganization("nonexistent");

      expect(result).toBeNull();
    });

    test("should throw DatabaseError on prisma error", async () => {
      const prismaError = new Prisma.PrismaClientKnownRequestError("Database error", {
        code: "P2002",
        clientVersion: "5.0.0",
      });
      vi.mocked(prisma.organization.findUnique).mockRejectedValue(prismaError);

      await expect(getOrganization("org1")).rejects.toThrow(DatabaseError);
    });
  });

  describe("getOrganizationByWorkspaceId", () => {
    const workspaceId = "clzabc123def456ghi789jkl";

    const mockOrganization = {
      id: "org1",
      name: "Test Org",
      createdAt: new Date(),
      updatedAt: new Date(),
      billing: {
        limits: {
          workspaces: 3,
          monthly: {
            responses: 1500,
          },
        },
        stripeCustomerId: null,
        usageCycleAnchor: new Date(),
      },
      isAISmartToolsEnabled: false,
      displayTimeZone: null,
      whitelabel: false,
    };

    test("should select only the mapped organization columns and load no relations beyond billing", async () => {
      vi.mocked(prisma.organization.findFirst).mockResolvedValue(mockOrganization);

      const result = await getOrganizationByWorkspaceId(workspaceId);

      expect(result).toEqual(mockOrganization);
      expect(prisma.organization.findFirst).toHaveBeenCalledWith({
        where: { workspaces: { some: { id: workspaceId } } },
        select: organizationSelect,
      });
      // Guard against re-adding a relation the mapper drops: `memberships` cost one extra query plus a row per member on every call site.
      const passedSelect = vi.mocked(prisma.organization.findFirst).mock.calls[0][0]?.select;
      expect(passedSelect).not.toHaveProperty("memberships");
    });

    test("should return null when no organization matches the workspace", async () => {
      vi.mocked(prisma.organization.findFirst).mockResolvedValue(null);

      await expect(getOrganizationByWorkspaceId(workspaceId)).resolves.toBeNull();
    });

    test("should throw DatabaseError on prisma error", async () => {
      const prismaError = new Prisma.PrismaClientKnownRequestError("Database error", {
        code: "P2002",
        clientVersion: "5.0.0",
      });
      vi.mocked(prisma.organization.findFirst).mockRejectedValue(prismaError);

      await expect(getOrganizationByWorkspaceId(workspaceId)).rejects.toThrow(DatabaseError);
    });
  });

  describe("getOrganizationsByUserId", () => {
    test("should return organizations for user", async () => {
      const mockOrganizations = [
        {
          id: "org1",
          name: "Test Org 1",
          createdAt: new Date(),
          updatedAt: new Date(),
          billing: {
            limits: {
              workspaces: 3,
              monthly: {
                responses: 1500,
              },
            },
            stripeCustomerId: null,
            usageCycleAnchor: new Date(),
          },
          isAISmartToolsEnabled: false,
          displayTimeZone: null,
          whitelabel: false,
        },
      ];

      vi.mocked(prisma.organization.findMany).mockResolvedValue(mockOrganizations);

      const result = await getOrganizationsByUserId("user1");

      expect(result).toEqual(mockOrganizations);
      expect(prisma.organization.findMany).toHaveBeenCalledWith({
        where: {
          id: { in: ["org1"] },
        },
        select: expect.any(Object),
        // Callers index into this list as if it were ordered, so the query has to be.
        orderBy: [{ createdAt: "asc" }, { id: "asc" }],
      });
    });

    test("should throw DatabaseError on prisma error", async () => {
      const prismaError = new Prisma.PrismaClientKnownRequestError("Database error", {
        code: "P2002",
        clientVersion: "5.0.0",
      });
      vi.mocked(prisma.organization.findMany).mockRejectedValue(prismaError);

      await expect(getOrganizationsByUserId("user1")).rejects.toThrow(DatabaseError);
    });
  });

  describe("createOrganization", () => {
    test("should create organization with default billing settings", async () => {
      const expectedBilling = {
        limits: {
          workspaces: IS_FORMA_CLOUD ? 1 : 3,
          monthly: {
            responses: IS_FORMA_CLOUD ? 250 : 1500,
          },
        },
        stripeCustomerId: null,
        usageCycleAnchor: null,
      };

      const mockOrganization = {
        id: "org1",
        name: "Test Org",
        createdAt: new Date(),
        updatedAt: new Date(),
        billing: expectedBilling,
        isAISmartToolsEnabled: false,
        displayTimeZone: null,
        whitelabel: false,
      };

      vi.mocked(prisma.organization.create).mockResolvedValue(mockOrganization);

      const result = await createOrganization({ name: "Test Org" });

      expect(result).toEqual(mockOrganization);
      expect(prisma.organization.create).toHaveBeenCalledWith({
        data: {
          name: "Test Org",
          billing: {
            create: {
              limits: {
                workspaces: IS_FORMA_CLOUD ? 1 : 3,
                monthly: {
                  responses: IS_FORMA_CLOUD ? 250 : 1500,
                  workflowRuns: null,
                },
              },
              stripeCustomerId: null,
              usageCycleAnchor: null,
            },
          },
        },
        select: organizationSelect,
      });
      // Stripe setup is now handled by the caller after membership creation
      expect(ensureCloudStripeSetupForOrganization).not.toHaveBeenCalled();
    });

    test("should throw DatabaseError on prisma error", async () => {
      const prismaError = new Prisma.PrismaClientKnownRequestError("Database error", {
        code: "P2002",
        clientVersion: "5.0.0",
      });
      vi.mocked(prisma.organization.create).mockRejectedValue(prismaError);

      await expect(createOrganization({ name: "Test Org" })).rejects.toThrow(DatabaseError);
    });
  });

  describe("updateOrganization", () => {
    test("should update organization and revalidate cache", async () => {
      const mockOrganization = {
        id: "org1",
        name: "Updated Org",
        createdAt: new Date(),
        updatedAt: new Date(),
        billing: {
          limits: {
            workspaces: 3,
            monthly: {
              responses: 1500,
            },
          },
          stripeCustomerId: null,
          usageCycleAnchor: new Date(),
        },
        isAISmartToolsEnabled: false,
        displayTimeZone: null,
        whitelabel: false,
        memberships: [{ userId: "user1" }, { userId: "user2" }],
        workspaces: [
          {
            environments: [{ id: "env1" }, { id: "env2" }],
          },
        ],
      };

      vi.mocked(prisma.organization.update).mockResolvedValue(mockOrganization);
      vi.mocked(prisma.$transaction).mockImplementation(
        async (fn: any) =>
          await fn({
            organization: {
              update: prisma.organization.update,
              findUnique: vi.fn().mockResolvedValue(mockOrganization),
            },
            organizationBilling: {
              upsert: prisma.organizationBilling.upsert,
            },
          })
      );

      const result = await updateOrganization("org1", { name: "Updated Org" });

      expect(result).toMatchObject({
        id: "org1",
        name: "Updated Org",
        createdAt: expect.any(Date),
        updatedAt: expect.any(Date),
        billing: {
          limits: {
            workspaces: 3,
            monthly: {
              responses: 1500,
            },
          },
          stripeCustomerId: null,
          usageCycleAnchor: expect.any(Date),
        },
        isAISmartToolsEnabled: false,
        whitelabel: false,
      });
      expect(prisma.organization.update).toHaveBeenCalledWith({
        where: { id: "org1" },
        data: { name: "Updated Org" },
      });
    });

    test("should throw ResourceNotFoundError when the update targets a missing organization (P2025)", async () => {
      const prismaError = new Prisma.PrismaClientKnownRequestError("Record to update not found", {
        code: "P2025",
        clientVersion: "5.0.0",
      });

      vi.mocked(prisma.$transaction).mockImplementation(
        async (fn: any) =>
          await fn({
            organization: {
              update: vi.fn().mockRejectedValue(prismaError),
              findUnique: vi.fn().mockResolvedValue({ id: "org1" }),
            },
            organizationBilling: {
              upsert: prisma.organizationBilling.upsert,
            },
          })
      );

      await expect(updateOrganization("org1", { name: "Updated Org" })).rejects.toThrow(
        ResourceNotFoundError
      );
    });
  });

  describe("subscribeOrganizationMembersToSurveyResponses", () => {
    test("should subscribe user to survey responses when not unsubscribed", async () => {
      const mockUser = {
        id: "user-123",
        notificationSettings: {
          alert: { "existing-survey-id": true },
          unsubscribedOrganizationIds: [], // User is subscribed to all organizations
        },
      } as any;

      const surveyId = "survey-123";
      const userId = "user-123";
      const organizationId = "org-123";

      vi.mocked(prisma.user.findUnique).mockResolvedValueOnce(mockUser);
      vi.mocked(updateUser).mockResolvedValueOnce({} as any);

      await subscribeOrganizationMembersToSurveyResponses(surveyId, userId, organizationId);

      expect(prisma.user.findUnique).toHaveBeenCalledWith({
        where: { id: userId },
      });
      expect(updateUser).toHaveBeenCalledWith(userId, {
        notificationSettings: {
          alert: {
            "existing-survey-id": true,
            "survey-123": true,
          },

          unsubscribedOrganizationIds: [],
        },
      });
    });

    test("should not subscribe user when unsubscribed from organization", async () => {
      const mockUser = {
        id: "user-123",
        notificationSettings: {
          alert: { "existing-survey-id": true },
          unsubscribedOrganizationIds: ["org-123"], // User has unsubscribed from this organization
        },
      } as any;

      const surveyId = "survey-123";
      const userId = "user-123";
      const organizationId = "org-123";

      vi.mocked(prisma.user.findUnique).mockResolvedValueOnce(mockUser);

      await subscribeOrganizationMembersToSurveyResponses(surveyId, userId, organizationId);

      // Should not call updateUser because user is unsubscribed from this organization
      expect(updateUser).not.toHaveBeenCalled();
    });
  });

  describe("deleteOrganization", () => {
    test("should call cleanupStripeCustomer when cloud and stripeCustomerId exists", async () => {
      vi.mocked(prisma.organization.delete).mockResolvedValue({
        id: "org1",
        name: "Test Org",
        billing: { stripeCustomerId: "cus_123" },
        memberships: [],
        workspaces: [],
        teams: [],
        apiKeys: [{ id: "api-key-1" }],
      } as any);

      await deleteOrganization("org1");

      expect(deleteOrganizationRelationships).toHaveBeenCalledWith("org1");
      expect(reconcileTeamWorkspaceRelationships).toHaveBeenCalledWith({ teamIds: [], workspaceIds: [] });
      expect(reconcileApiKeyRelationships).toHaveBeenCalledWith({
        apiKeyIds: ["api-key-1"],
      });
      if (IS_FORMA_CLOUD) {
        expect(cleanupStripeCustomer).toHaveBeenCalledWith("cus_123");
      }
    });
  });

  describe("getMonthlyOrganizationWorkflowRunCount", () => {
    const mockOrganization = {
      id: "org_1",
      name: "Test Org",
      createdAt: new Date(),
      updatedAt: new Date(),
      billing: {
        stripeCustomerId: "cus_1",
        limits: { workspaces: 5, monthly: { responses: 5000, workflowRuns: 1000 } },
        usageCycleAnchor: null,
        stripe: null,
      },
      isAISmartToolsEnabled: false,
      whitelabel: null,
    };

    test("counts non-dry workflow runs across the organization's workspaces in the billing cycle", async () => {
      vi.mocked(prisma.organization.findUnique).mockResolvedValue(mockOrganization as never);
      vi.mocked(getWorkspaces).mockResolvedValue([{ id: "ws_1" }, { id: "ws_2" }] as never);
      vi.mocked(prisma.workflowRun.aggregate).mockResolvedValue({ _count: { id: 42 } } as never);

      const result = await getMonthlyOrganizationWorkflowRunCount("cms634kob000001uzrelh0qeb");

      expect(result).toBe(42);
      const aggregateArgs = vi.mocked(prisma.workflowRun.aggregate).mock.calls[0][0];
      expect(aggregateArgs.where?.AND).toEqual(
        expect.arrayContaining([
          { workspaceId: { in: ["ws_1", "ws_2"] } },
          { isDryRun: false },
          expect.objectContaining({ createdAt: expect.any(Object) }),
        ])
      );
    });

    test("throws ResourceNotFoundError when the organization does not exist", async () => {
      vi.mocked(prisma.organization.findUnique).mockResolvedValue(null);

      await expect(getMonthlyOrganizationWorkflowRunCount("cmmissingorg00000000000a")).rejects.toThrow(
        ResourceNotFoundError
      );
    });

    test("wraps a known Prisma error in DatabaseError", async () => {
      vi.mocked(prisma.organization.findUnique).mockResolvedValue(mockOrganization as never);
      vi.mocked(getWorkspaces).mockResolvedValue([{ id: "ws_1" }] as never);
      vi.mocked(prisma.workflowRun.aggregate).mockRejectedValue(
        new Prisma.PrismaClientKnownRequestError("db down", { code: "P2002", clientVersion: "1.0.0" })
      );

      await expect(getMonthlyOrganizationWorkflowRunCount("cms634kob000001uzrelh0qeb")).rejects.toThrow(
        DatabaseError
      );
    });
  });
});
