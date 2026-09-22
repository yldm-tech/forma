import { type CacheKey, type CustomCacheNamespace } from "@/types/keys";
import { makeCacheKey } from "./utils/key";

/**
 * Enterprise-grade cache key generator following industry best practices
 * Pattern: fb:\{resource\}:\{identifier\}:\{subResource\}
 *
 * Benefits:
 * - Clear namespace hierarchy (fb = forma)
 * - Collision-proof across workspaces
 * - Easy debugging and monitoring
 * - Predictable invalidation patterns
 * - Multi-tenant safe
 * - Type-safe with branded CacheKey type
 */

export const createCacheKey = {
  // Workspace-related keys
  workspace: {
    state: (workspaceId: string): CacheKey => makeCacheKey("env", workspaceId, "state"),
    config: (workspaceId: string): CacheKey => makeCacheKey("env", workspaceId, "config"),
    segments: (workspaceId: string): CacheKey => makeCacheKey("env", workspaceId, "segments"),
    languages: (workspaceId: string): CacheKey => makeCacheKey("env", workspaceId, "languages"),
    // Keyed on the raw URL id, which may be a workspace id *or* a legacy environment id, so both forms of
    // the same public request get their own entry. Do not reuse this key for anything keyed on the
    // resolved workspace id — the whole point is that the input side is not yet known to be either.
    idResolution: (rawId: string): CacheKey => makeCacheKey("env", rawId, "id-resolution"),
  },

  // Organization-related keys
  organization: {
    billing: (organizationId: string): CacheKey => makeCacheKey("org", organizationId, "billing"),
    // Single-flight lock so only one process refreshes an org's stale billing snapshot at a time.
    billingSyncLock: (organizationId: string): CacheKey =>
      makeCacheKey("org", organizationId, "billing-sync-lock"),
  },

  // License and enterprise features
  license: {
    status: (organizationId: string): CacheKey => makeCacheKey("license", organizationId, "status"),
    previous_result: (organizationId: string): CacheKey =>
      makeCacheKey("license", organizationId, "previous_result"),
    fetch_lock: (organizationId: string): CacheKey => makeCacheKey("license", organizationId, "fetch_lock"),
  },

  // Response-related keys
  response: {
    countBySurveyId: (surveyId: string): CacheKey => makeCacheKey("response", surveyId, "count"),
  },

  // Rate limiting and security
  rateLimit: {
    core: (namespace: string, identifier: string, windowStart: number): CacheKey =>
      makeCacheKey("rate_limit", namespace, identifier, String(windowStart)),
  },

  // Custom keys with validation
  custom: (namespace: CustomCacheNamespace, identifier: string, subResource?: string): CacheKey => {
    return subResource !== undefined
      ? makeCacheKey(namespace, identifier, subResource)
      : makeCacheKey(namespace, identifier);
  },
};
