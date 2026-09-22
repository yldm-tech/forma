import { beforeEach, describe, expect, test, vi } from "vitest";
import { prisma } from "@forma/database";
import { parseApiKeyV2, verifySecret } from "@/lib/crypto";
import { applyRateLimit } from "@/modules/core/rate-limit/helpers";
import { rateLimitConfigs } from "@/modules/core/rate-limit/rate-limit-configs";
import { GET } from "./route";

const mockConstants = vi.hoisted(() => ({ gatewayRateLimiting: false }));
const mockHeaders = vi.hoisted(() => ({ apiKey: null as string | null }));

vi.mock("next/headers", () => ({
  headers: async () => ({ get: (_name: string) => mockHeaders.apiKey }),
}));

vi.mock("@/lib/constants", () => ({
  CONTROL_HASH: "control-hash",
  get GATEWAY_RATE_LIMITING() {
    return mockConstants.gatewayRateLimiting;
  },
}));

vi.mock("@/lib/crypto", () => ({
  hashSha256: vi.fn(() => "lookup-hash"),
  parseApiKeyV2: vi.fn(),
  verifySecret: vi.fn(),
}));

vi.mock("@forma/database", () => ({
  prisma: {
    apiKey: {
      findUnique: vi.fn(),
      findFirst: vi.fn(),
      update: vi.fn(),
    },
    user: { findUnique: vi.fn() },
  },
}));

vi.mock("@/modules/core/rate-limit/helpers", () => ({
  applyRateLimit: vi.fn(),
}));

vi.mock("@/app/api/v1/management/me/lib/utils", () => ({
  getSessionUser: vi.fn(async () => null),
}));

const apiKeyId = "api-key-id";
const organizationId = "organization-id";

const mockApiKeyData = {
  id: apiKeyId,
  hashedKey: "hashed-key",
  organizationId,
  lastUsedAt: new Date(),
  apiKeyWorkspaces: [
    {
      permission: "manage",
      workspace: {
        id: "workspace-id",
        organizationId,
        legacyEnvironmentId: "legacy-environment-id",
        createdAt: new Date(),
        updatedAt: new Date(),
        name: "Workspace",
        appSetupCompleted: true,
      },
    },
  ],
};

describe("GET /api/v1/management/me", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockConstants.gatewayRateLimiting = false;
    mockHeaders.apiKey = "fbk_secret";
    vi.mocked(parseApiKeyV2).mockReturnValue({ secret: "secret" } as ReturnType<typeof parseApiKeyV2>);
    vi.mocked(verifySecret).mockResolvedValue(true);
    vi.mocked(prisma.apiKey.findUnique).mockResolvedValue(mockApiKeyData as never);
  });

  test("rate limits an api key request when no gateway is declared", async () => {
    const response = await GET();

    expect(applyRateLimit).toHaveBeenCalledWith(rateLimitConfigs.api.v1, apiKeyId);
    expect(response.status).toBe(200);
  });

  test("returns 429 when the api key exceeds its limit", async () => {
    vi.mocked(applyRateLimit).mockRejectedValue(new Error("Maximum number of requests reached"));

    const response = await GET();

    expect(response.status).toBe(429);
  });

  test("defers to the gateway when GATEWAY_RATE_LIMITING is on", async () => {
    mockConstants.gatewayRateLimiting = true;

    const response = await GET();

    expect(applyRateLimit).not.toHaveBeenCalled();
    expect(response.status).toBe(200);
  });

  test("does not rate limit an api key that fails authentication", async () => {
    vi.mocked(verifySecret).mockResolvedValue(false);

    const response = await GET();

    expect(applyRateLimit).not.toHaveBeenCalled();
    expect(response.status).toBe(401);
  });
});
