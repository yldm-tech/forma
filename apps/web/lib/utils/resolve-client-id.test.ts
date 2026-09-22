import { beforeEach, describe, expect, test, vi } from "vitest";
import { getCacheService } from "@forma/cache";
import { prisma } from "@forma/database";
import { findWorkspaceByIdOrLegacyEnvId, resolveClientApiIds } from "./resolve-client-id";

vi.mock("server-only", () => ({}));

vi.mock("@forma/database", () => ({
  prisma: {
    workspace: {
      findFirst: vi.fn(),
    },
  },
}));

vi.mock("@forma/logger", () => ({
  logger: { warn: vi.fn(), error: vi.fn(), info: vi.fn(), debug: vi.fn() },
}));

vi.mock("@forma/cache", () => ({
  getCacheService: vi.fn(),
  createCacheKey: {
    workspace: {
      idResolution: (rawId: string) => {
        // Mirrors makeCacheKey's refusal to build a key from an empty part.
        if (rawId.length === 0) {
          throw new Error("Invalid Cache key: Parts cannot be empty");
        }
        return `fb:env:${rawId}:id-resolution`;
      },
    },
  },
}));

type TCacheServiceResult = Awaited<ReturnType<typeof getCacheService>>;
type TWorkspaceFindFirstResult = Awaited<ReturnType<typeof prisma.workspace.findFirst>>;

const cacheGet = vi.fn();
const cacheSet = vi.fn();

/** Points `getCacheService` at a working cache whose GET returns `cached` (null meaning a miss). */
const givenCache = (cached: unknown): void => {
  cacheGet.mockResolvedValue({ ok: true, data: cached });
  cacheSet.mockResolvedValue({ ok: true, data: undefined });
  vi.mocked(getCacheService).mockResolvedValue({
    ok: true,
    data: { get: cacheGet, set: cacheSet },
  } as unknown as TCacheServiceResult);
};

const givenWorkspaceRow = (row: { id: string; organizationId: string } | null): void => {
  vi.mocked(prisma.workspace.findFirst).mockResolvedValue(row as TWorkspaceFindFirstResult);
};

describe("resolveClientApiIds", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    givenCache(null);
  });

  test("resolves a workspaceId to workspaceId", async () => {
    vi.mocked(prisma.workspace.findFirst).mockResolvedValue({
      id: "ws-456",
    } as any);

    const result = await resolveClientApiIds("ws-456");

    expect(result).toEqual({
      workspaceId: "ws-456",
    });
    expect(prisma.workspace.findFirst).toHaveBeenCalledWith({
      where: { OR: [{ id: "ws-456" }, { legacyEnvironmentId: "ws-456" }] },
      select: { id: true, organizationId: true },
    });
  });

  test("falls back to legacyEnvironmentId when primary lookup fails", async () => {
    vi.mocked(prisma.workspace.findFirst).mockResolvedValue({ id: "ws-456" } as any);

    const result = await resolveClientApiIds("env-old-123");

    expect(result).toEqual({ workspaceId: "ws-456" });
    expect(prisma.workspace.findFirst).toHaveBeenCalledTimes(1);
    expect(prisma.workspace.findFirst).toHaveBeenCalledWith({
      where: { OR: [{ id: "env-old-123" }, { legacyEnvironmentId: "env-old-123" }] },
      select: { id: true, organizationId: true },
    });
  });

  test("returns null when both lookups fail", async () => {
    vi.mocked(prisma.workspace.findFirst).mockResolvedValue(null);

    const result = await resolveClientApiIds("unknown-id");

    expect(result).toBeNull();
    expect(prisma.workspace.findFirst).toHaveBeenCalledTimes(1);
  });
});

describe("findWorkspaceByIdOrLegacyEnvId", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    givenCache(null);
  });

  test("returns workspace when found by primary id", async () => {
    vi.mocked(prisma.workspace.findFirst).mockResolvedValue({ id: "ws-123" } as any);

    const result = await findWorkspaceByIdOrLegacyEnvId("ws-123");

    expect(result).toEqual({ id: "ws-123" });
    expect(prisma.workspace.findFirst).toHaveBeenCalledTimes(1);
  });

  test("returns workspace when found by legacyEnvironmentId", async () => {
    vi.mocked(prisma.workspace.findFirst).mockResolvedValue({ id: "ws-123" } as any);

    const result = await findWorkspaceByIdOrLegacyEnvId("env-old");

    expect(result).toEqual({ id: "ws-123" });
    expect(prisma.workspace.findFirst).toHaveBeenCalledTimes(1);
  });

  test("returns null when not found by either lookup", async () => {
    vi.mocked(prisma.workspace.findFirst).mockResolvedValue(null);

    const result = await findWorkspaceByIdOrLegacyEnvId("nonexistent");

    expect(result).toBeNull();
  });
});

describe("findWorkspaceByIdOrLegacyEnvId caching", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  test("serves a cached resolution without querying the database", async () => {
    givenCache({ id: "ws-123", organizationId: "org-1" });

    const result = await findWorkspaceByIdOrLegacyEnvId("env-old");

    expect(result).toEqual({ id: "ws-123", organizationId: "org-1" });
    expect(prisma.workspace.findFirst).not.toHaveBeenCalled();
    expect(cacheGet).toHaveBeenCalledWith("fb:env:env-old:id-resolution");
  });

  test("caches a hit under the raw id it was asked about, with a one-hour TTL", async () => {
    givenCache(null);
    givenWorkspaceRow({ id: "ws-123", organizationId: "org-1" });

    await findWorkspaceByIdOrLegacyEnvId("env-old");

    // Keyed on the raw input, never on the resolved workspace id: keying on the resolution would let
    // one workspace's entry answer for an id belonging to another.
    expect(cacheSet).toHaveBeenCalledWith(
      "fb:env:env-old:id-resolution",
      { id: "ws-123", organizationId: "org-1" },
      60 * 60 * 1000
    );
  });

  test("does not cache a miss, so unknown public ids cannot grow the keyspace", async () => {
    givenCache(null);
    givenWorkspaceRow(null);

    const result = await findWorkspaceByIdOrLegacyEnvId("bogus-id");

    expect(result).toBeNull();
    expect(cacheSet).not.toHaveBeenCalled();
  });

  test("falls through to the database when the cache service is unavailable", async () => {
    vi.mocked(getCacheService).mockResolvedValue({
      ok: false,
      error: { code: "redis_connection_error" },
    } as unknown as TCacheServiceResult);
    givenWorkspaceRow({ id: "ws-123", organizationId: "org-1" });

    const result = await findWorkspaceByIdOrLegacyEnvId("env-old");

    expect(result).toEqual({ id: "ws-123", organizationId: "org-1" });
    expect(prisma.workspace.findFirst).toHaveBeenCalledTimes(1);
  });

  test("falls through to the database when the cache service throws", async () => {
    vi.mocked(getCacheService).mockRejectedValue(new Error("boom"));
    givenWorkspaceRow({ id: "ws-123", organizationId: "org-1" });

    const result = await findWorkspaceByIdOrLegacyEnvId("env-old");

    expect(result).toEqual({ id: "ws-123", organizationId: "org-1" });
    expect(prisma.workspace.findFirst).toHaveBeenCalledTimes(1);
  });

  test("still resolves when the id cannot form a cache key", async () => {
    givenCache(null);
    givenWorkspaceRow(null);

    const result = await findWorkspaceByIdOrLegacyEnvId("");

    expect(result).toBeNull();
    expect(cacheGet).not.toHaveBeenCalled();
    expect(prisma.workspace.findFirst).toHaveBeenCalledTimes(1);
  });
});
