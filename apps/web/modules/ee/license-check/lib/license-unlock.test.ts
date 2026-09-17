import { beforeEach, describe, expect, test, vi } from "vitest";

// The unlock is the no-key branch, so the env mock has no key. Everything else here exists to keep `getEnterpriseLicense` from reaching a cache, a database or a licence server — none of which the branch under test touches.
const { envMock, constantsMock } = vi.hoisted(() => ({
  envMock: {
    ENTERPRISE_LICENSE_KEY: undefined as string | undefined,
    ENVIRONMENT: "development",
    FORMA_COM_URL: "https://app.forma.ylam.ai",
    HTTPS_PROXY: undefined,
    HTTP_PROXY: undefined,
    NODE_ENV: "test",
  },
  constantsMock: { IS_DEVELOPMENT: false, E2E_TESTING: false },
}));

vi.mock("@/lib/env", () => ({ env: envMock }));

vi.mock("@/lib/constants", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(typeof actual === "object" && actual !== null ? actual : {}),
    IS_FORMA_CLOUD: false,
    get IS_DEVELOPMENT() {
      return constantsMock.IS_DEVELOPMENT;
    },
    get E2E_TESTING() {
      return constantsMock.E2E_TESTING;
    },
  };
});

vi.mock("@/lib/cache", () => ({
  cache: { get: vi.fn(), set: vi.fn(), del: vi.fn(), withCache: vi.fn(), getRedisClient: vi.fn() },
}));
vi.mock("@forma/cache", () => ({ createCacheKey: { custom: () => "k" } }));
vi.mock("@forma/database", () => ({ prisma: { organization: { count: vi.fn() } } }));
vi.mock("@forma/logger", () => ({
  logger: { error: vi.fn(), warn: vi.fn(), info: vi.fn(), debug: vi.fn() },
}));
vi.mock("@/lib/instance", () => ({ getInstanceId: vi.fn(), getInstanceInfo: vi.fn() }));

const { getEnterpriseLicense } = await import("./license");

describe("licence with no key", () => {
  beforeEach(() => {
    constantsMock.IS_DEVELOPMENT = false;
    constantsMock.E2E_TESTING = false;
  });

  test("grants every feature, because features are not sold separately here", async () => {
    const result = await getEnterpriseLicense();

    expect(result.active).toBe(true);
    expect(result.status).toBe("active");
    expect(result.features?.contacts).toBe(true);
    expect(result.features?.workflows).toBe(true);
    expect(result.features?.quotas).toBe(true);
    expect(result.features?.sso).toBe(true);
    // `workspaces` is a limit rather than a flag; null means unlimited.
    expect(result.features?.workspaces).toBeNull();
  });

  test("does not depend on the environment, so a production build behaves like a local one", async () => {
    const production = await getEnterpriseLicense();

    constantsMock.IS_DEVELOPMENT = true;
    const development = await getEnterpriseLicense();

    constantsMock.E2E_TESTING = true;
    const e2e = await getEnterpriseLicense();

    expect(development.features).toEqual(production.features);
    expect(e2e.features).toEqual(production.features);
    expect([production.active, development.active, e2e.active]).toEqual([true, true, true]);
  });

  test("leaves no feature flagged off, so an instance is never half-featured", async () => {
    const { features } = await getEnterpriseLicense();

    const off = Object.entries(features ?? {}).filter(([, v]) => v === false);
    expect(off).toEqual([]);
  });

  test("reports no pending downgrade, so nothing schedules one", async () => {
    const result = await getEnterpriseLicense();

    expect(result.isPendingDowngrade).toBe(false);
  });
});
