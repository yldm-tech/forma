import "server-only";
import { cache as reactCache } from "react";
import { type CacheKey, type CacheService, createCacheKey, getCacheService } from "@forma/cache";
import { prisma } from "@forma/database";
import { logger } from "@forma/logger";

export type TResolvedClientIds = {
  workspaceId: string;
};

type TWorkspaceIdResolution = { id: string; organizationId: string };

/**
 * The raw-id → workspace mapping is immutable for a workspace's lifetime: `id` and
 * `legacyEnvironmentId` are both assigned at creation and a workspace never moves between
 * organizations, so the only event that invalidates an entry is a workspace deletion. Every caller reads
 * the real resource after resolving, which makes a stale positive cost one extra failing query rather
 * than serve another tenant's data — so the TTL is set by how long a deleted workspace may keep
 * resolving, not by how fresh the mapping needs to be. An hour keeps that window short while still
 * absorbing sustained public client-API traffic.
 */
const ID_RESOLUTION_TTL_MS = 60 * 60 * 1000;

/**
 * Builds the resolution key, or null when the id cannot form one. `makeCacheKey` throws on empty parts,
 * and an id that cannot be keyed cannot match a workspace either, so the caller falls through to the
 * database.
 */
const buildIdResolutionKey = (id: string): CacheKey | null => {
  try {
    return createCacheKey.workspace.idResolution(id);
  } catch {
    return null;
  }
};

/** Returns the cache service, or null when it is unavailable. Never throws — the cache is optional here. */
const getOptionalCacheService = async (): Promise<CacheService | null> => {
  try {
    const result = await getCacheService();
    return result.ok ? result.data : null;
  } catch (error) {
    logger.warn({ error }, "Cache unavailable while resolving a client id");
    return null;
  }
};

/**
 * Finds a workspace by its primary id or by legacyEnvironmentId in a single query.
 * Both columns have unique indexes so the query planner will use index scans.
 * Returns the workspace id and its owning organizationId if found, null otherwise.
 *
 * Successful resolutions are cached in Redis so the public client API — which runs this on every request
 * — stops checking out a Postgres connection just to map a URL id onto a workspace. Misses are
 * deliberately *not* cached: the ids reaching this function come straight off public URLs, so caching
 * negatives would let anyone grow the Redis keyspace with one key per bogus id they send.
 */
export const findWorkspaceByIdOrLegacyEnvId = async (id: string): Promise<TWorkspaceIdResolution | null> => {
  const cacheKey = buildIdResolutionKey(id);
  const cacheService = cacheKey ? await getOptionalCacheService() : null;

  if (cacheKey && cacheService) {
    const cached = await cacheService.get<TWorkspaceIdResolution>(cacheKey);
    if (cached.ok && cached.data) {
      return cached.data;
    }
  }

  const workspace = await prisma.workspace.findFirst({
    where: { OR: [{ id }, { legacyEnvironmentId: id }] },
    select: { id: true, organizationId: true },
  });

  if (workspace && cacheKey && cacheService) {
    await cacheService.set(cacheKey, workspace, ID_RESOLUTION_TTL_MS);
  }

  return workspace;
};

/**
 * Resolves a URL parameter that may be a workspaceId or a legacy environmentId.
 *
 * - Looks up the id in the Workspace table by primary key first.
 * - Falls back to a lookup by legacyEnvironmentId for backward compatibility.
 * - Returns null if both lookups fail.
 *
 * The `reactCache` wrapper dedupes within a request, so a route that resolves the same id twice pays
 * neither the database nor the Redis round trip the second time.
 */
export const resolveClientApiIds = reactCache(async (id: string): Promise<TResolvedClientIds | null> => {
  const workspace = await findWorkspaceByIdOrLegacyEnvId(id);

  if (workspace) {
    return { workspaceId: workspace.id };
  }

  return null;
});
