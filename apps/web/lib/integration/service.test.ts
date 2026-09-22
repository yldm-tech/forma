import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { prisma } from "@forma/database";
import { IntegrationType, Prisma } from "@forma/database/prisma";
import { DatabaseError } from "@forma/types/errors";
import { TIntegrationInput } from "@forma/types/integration";
import { ITEMS_PER_PAGE } from "../constants";
import { symmetricDecrypt } from "../crypto";
import {
  createOrUpdateIntegration,
  deleteIntegration,
  getIntegration,
  getIntegrationByType,
  getIntegrations,
} from "./service";

// The global setup pins `ENCRYPTION_KEY` to a value `createCipheriv` rejects, and these tests exercise
// the real credential encryption the service now applies on write.
const { TEST_ENCRYPTION_KEY } = vi.hoisted(() => ({
  TEST_ENCRYPTION_KEY: "0123456789abcdef0123456789abcdef",
}));

vi.mock("@/lib/constants", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/constants")>()),
  ENCRYPTION_KEY: TEST_ENCRYPTION_KEY,
}));

vi.mock("@forma/database", () => ({
  prisma: {
    integration: {
      upsert: vi.fn(),
      findMany: vi.fn(),
      findUnique: vi.fn(),
      findFirst: vi.fn(),
      delete: vi.fn(),
    },
  },
}));

/** `iv:ciphertext:tag`, the shape `symmetricEncrypt` produces. */
const CIPHERTEXT = /^[0-9a-f]{32}:[0-9a-f]+:[0-9a-f]{32}$/;

describe("Integration Service", () => {
  beforeEach(() => {
    vi.resetAllMocks();
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  const mockIntegrationConfig = {
    email: "test@example.com",
    key: {
      scope: "https://www.googleapis.com/auth/spreadsheets",
      token_type: "Bearer" as const,
      expiry_date: 1234567890,
      access_token: "mock-access-token",
      refresh_token: "mock-refresh-token",
    },
    data: [
      {
        spreadsheetId: "spreadsheet123",
        spreadsheetName: "Test Spreadsheet",
        surveyId: "survey123",
        surveyName: "Test Survey",
        elementIds: ["q1", "q2"],
        elements: "Question 1, Question 2",
        createdAt: new Date(),
        includeHiddenFields: false,
        includeMetadata: true,
        includeCreatedAt: true,
        includeVariables: false,
      },
    ],
  };

  describe("createOrUpdateIntegration", () => {
    const mockWorkspaceId = "clg123456789012345678901234";
    const mockIntegrationData: TIntegrationInput = {
      type: "googleSheets",
      config: mockIntegrationConfig,
    };

    test("should create a new integration", async () => {
      const mockIntegration = {
        id: "int_123",
        workspaceId: mockWorkspaceId,
        ...mockIntegrationData,
      };

      vi.mocked(prisma.integration.upsert).mockResolvedValue(mockIntegration);

      const result = await createOrUpdateIntegration(mockWorkspaceId, mockIntegrationData);

      const expectedConfig = {
        ...mockIntegrationConfig,
        key: {
          ...mockIntegrationConfig.key,
          access_token: expect.stringMatching(CIPHERTEXT),
          refresh_token: expect.stringMatching(CIPHERTEXT),
        },
      };

      expect(prisma.integration.upsert).toHaveBeenCalledWith({
        where: {
          type_workspaceId: {
            workspaceId: mockWorkspaceId,
            type: mockIntegrationData.type,
          },
        },
        update: {
          type: mockIntegrationData.type,
          config: expectedConfig,
          workspace: { connect: { id: mockWorkspaceId } },
        },
        create: {
          type: mockIntegrationData.type,
          config: expectedConfig,
          workspace: { connect: { id: mockWorkspaceId } },
        },
      });

      expect(result).toEqual(mockIntegration);
    });

    // ENG: Slack, Google Sheets and Airtable wrote their OAuth credentials to Postgres in cleartext —
    // only Notion encrypted, and it did so in its own callback. The store now owns it for every
    // provider, so a dump of `Integration.config` carries no usable token.
    test("encrypts the credentials on the way into Postgres and leaves the rest of the key alone", async () => {
      vi.mocked(prisma.integration.upsert).mockResolvedValue({
        id: "int_123",
        workspaceId: mockWorkspaceId,
        ...mockIntegrationData,
      });

      await createOrUpdateIntegration(mockWorkspaceId, mockIntegrationData);

      const written = vi.mocked(prisma.integration.upsert).mock.calls[0][0].create.config as {
        key: Record<string, unknown>;
      };

      expect(written.key.access_token).not.toBe("mock-access-token");
      expect(written.key.refresh_token).not.toBe("mock-refresh-token");
      expect(symmetricDecrypt(written.key.access_token as string, TEST_ENCRYPTION_KEY)).toBe(
        "mock-access-token"
      );
      expect(symmetricDecrypt(written.key.refresh_token as string, TEST_ENCRYPTION_KEY)).toBe(
        "mock-refresh-token"
      );
      // `token_type` names a scheme and `scope` a grant; neither is a credential, and blanking or
      // encrypting them would break the Zod literals the provider types declare.
      expect(written.key.token_type).toBe("Bearer");
      expect(written.key.scope).toBe(mockIntegrationConfig.key.scope);
      expect(written.key.expiry_date).toBe(mockIntegrationConfig.key.expiry_date);
    });

    test("should throw DatabaseError when Prisma throws an error", async () => {
      const prismaError = new Prisma.PrismaClientKnownRequestError("Test error", {
        code: "P2002",
        clientVersion: "5.0.0",
      });

      vi.mocked(prisma.integration.upsert).mockRejectedValue(prismaError);

      await expect(createOrUpdateIntegration(mockWorkspaceId, mockIntegrationData)).rejects.toThrow(
        DatabaseError
      );
    });
  });

  describe("getIntegrations", () => {
    const mockWorkspaceId = "clg123456789012345678901234";
    const mockIntegrations = [
      {
        id: "int_123",
        workspaceId: mockWorkspaceId,
        type: IntegrationType.googleSheets,
        config: mockIntegrationConfig,
      },
    ];

    test("should get all integrations for a workspace", async () => {
      vi.mocked(prisma.integration.findMany).mockResolvedValue(mockIntegrations);

      const result = await getIntegrations(mockWorkspaceId);

      expect(prisma.integration.findMany).toHaveBeenCalledWith({
        where: {
          workspaceId: mockWorkspaceId,
        },
      });

      expect(result).toEqual(mockIntegrations);
    });

    test("should get paginated integrations", async () => {
      const page = 2;
      vi.mocked(prisma.integration.findMany).mockResolvedValue(mockIntegrations);

      const result = await getIntegrations(mockWorkspaceId, page);

      expect(prisma.integration.findMany).toHaveBeenCalledWith({
        where: {
          workspaceId: mockWorkspaceId,
        },
        take: ITEMS_PER_PAGE,
        skip: ITEMS_PER_PAGE * (page - 1),
      });

      expect(result).toEqual(mockIntegrations);
    });

    test("should throw DatabaseError when Prisma throws an error", async () => {
      const prismaError = new Prisma.PrismaClientKnownRequestError("Test error", {
        code: "P2002",
        clientVersion: "5.0.0",
      });

      vi.mocked(prisma.integration.findMany).mockRejectedValue(prismaError);

      await expect(getIntegrations(mockWorkspaceId)).rejects.toThrow(DatabaseError);
    });
  });

  describe("getIntegration", () => {
    const mockIntegrationId = "int_123";
    const mockIntegration = {
      id: mockIntegrationId,
      workspaceId: "clg123456789012345678901234",
      type: IntegrationType.googleSheets,
      config: mockIntegrationConfig,
    };

    test("should get an integration by ID", async () => {
      vi.mocked(prisma.integration.findUnique).mockResolvedValue(mockIntegration);

      const result = await getIntegration(mockIntegrationId);

      expect(prisma.integration.findUnique).toHaveBeenCalledWith({
        where: {
          id: mockIntegrationId,
        },
      });

      expect(result).toEqual(mockIntegration);
    });

    test("should return null when integration is not found", async () => {
      vi.mocked(prisma.integration.findUnique).mockResolvedValue(null);

      const result = await getIntegration(mockIntegrationId);

      expect(result).toBeNull();
    });

    test("should throw DatabaseError when Prisma throws an error", async () => {
      const prismaError = new Prisma.PrismaClientKnownRequestError("Test error", {
        code: "P2002",
        clientVersion: "5.0.0",
      });

      vi.mocked(prisma.integration.findUnique).mockRejectedValue(prismaError);

      await expect(getIntegration(mockIntegrationId)).rejects.toThrow(DatabaseError);
    });
  });

  describe("getIntegrationByType", () => {
    const mockWorkspaceId = "clg123456789012345678901234";
    const mockType = IntegrationType.googleSheets;
    const mockIntegration = {
      id: "int_123",
      workspaceId: mockWorkspaceId,
      type: mockType,
      config: mockIntegrationConfig,
    };

    test("should get an integration by type", async () => {
      vi.mocked(prisma.integration.findFirst).mockResolvedValue(mockIntegration);

      const result = await getIntegrationByType(mockWorkspaceId, mockType);

      expect(prisma.integration.findFirst).toHaveBeenCalledWith({
        where: {
          workspaceId: mockWorkspaceId,
          type: mockType,
        },
      });

      expect(result).toEqual(mockIntegration);
    });

    test("should return null when integration is not found", async () => {
      vi.mocked(prisma.integration.findFirst).mockResolvedValue(null);

      const result = await getIntegrationByType(mockWorkspaceId, mockType);

      expect(result).toBeNull();
    });

    test("should throw DatabaseError when Prisma throws an error", async () => {
      const prismaError = new Prisma.PrismaClientKnownRequestError("Test error", {
        code: "P2002",
        clientVersion: "5.0.0",
      });

      vi.mocked(prisma.integration.findFirst).mockRejectedValue(prismaError);

      await expect(getIntegrationByType(mockWorkspaceId, mockType)).rejects.toThrow(DatabaseError);
    });
  });

  describe("deleteIntegration", () => {
    const mockIntegrationId = "int_123";
    const mockIntegration = {
      id: mockIntegrationId,
      workspaceId: "clg123456789012345678901234",
      type: IntegrationType.googleSheets,
      config: mockIntegrationConfig,
    };

    test("should delete an integration", async () => {
      vi.mocked(prisma.integration.delete).mockResolvedValue(mockIntegration);

      const result = await deleteIntegration(mockIntegrationId);

      expect(prisma.integration.delete).toHaveBeenCalledWith({
        where: {
          id: mockIntegrationId,
        },
      });

      expect(result).toEqual(mockIntegration);
    });

    test("should throw DatabaseError when Prisma throws an error", async () => {
      const prismaError = new Prisma.PrismaClientKnownRequestError("Test error", {
        code: "P2002",
        clientVersion: "5.0.0",
      });

      vi.mocked(prisma.integration.delete).mockRejectedValue(prismaError);

      await expect(deleteIntegration(mockIntegrationId)).rejects.toThrow(DatabaseError);
    });
  });
});
