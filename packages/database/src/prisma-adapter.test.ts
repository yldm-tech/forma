import type { PoolConfig } from "pg";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { createPrismaPgAdapter } from "./prisma-adapter";

const { prismaPgMock } = vi.hoisted(() => ({
  prismaPgMock: vi.fn(),
}));

vi.mock("@prisma/adapter-pg", () => ({
  PrismaPg: class {
    constructor(...args: unknown[]) {
      prismaPgMock(...args);
    }
  },
}));

const BASE_URL = "postgresql://forma:secret@localhost:5432/forma";

const poolConfigFor = (url: string, options?: { applyQueryTimeouts?: boolean }): PoolConfig => {
  createPrismaPgAdapter(url, options);
  const [lastCall] = prismaPgMock.mock.calls.slice(-1);
  return lastCall[0] as PoolConfig;
};

beforeEach(() => {
  prismaPgMock.mockClear();
});

describe("createPrismaPgAdapter query timeouts", () => {
  test("bounds idle-in-transaction sessions at 60s by default and leaves statement timeouts opt-in", () => {
    const poolConfig = poolConfigFor(BASE_URL);

    expect(poolConfig.idle_in_transaction_session_timeout).toBe(60_000);
    expect(poolConfig.statement_timeout).toBeUndefined();
    expect(poolConfig.query_timeout).toBeUndefined();
  });

  test("reads all three bounds from the URL in seconds", () => {
    const poolConfig = poolConfigFor(
      `${BASE_URL}?statement_timeout=45&query_timeout=50&idle_in_transaction_session_timeout=120`
    );

    expect(poolConfig.statement_timeout).toBe(45_000);
    expect(poolConfig.query_timeout).toBe(50_000);
    expect(poolConfig.idle_in_transaction_session_timeout).toBe(120_000);
  });

  test("treats 0 as disabled rather than falling back to the default", () => {
    const poolConfig = poolConfigFor(`${BASE_URL}?idle_in_transaction_session_timeout=0`);

    expect(poolConfig.idle_in_transaction_session_timeout).toBe(0);
  });

  test("strips the timeout params from the connection string pg is handed", () => {
    // pg merges the parsed connection string over the explicit PoolConfig, so a leftover param
    // would override the computed number with a string in the wrong unit.
    const url = `${BASE_URL}?statement_timeout=45&query_timeout=50&idle_in_transaction_session_timeout=120&sslmode=require`;
    const { connectionString } = createPrismaPgAdapter(url);

    expect(connectionString).not.toContain("statement_timeout");
    expect(connectionString).not.toContain("query_timeout");
    expect(connectionString).not.toContain("idle_in_transaction_session_timeout");
    expect(connectionString).toContain("sslmode=require");
  });

  test("applyQueryTimeouts: false ignores the defaults and the URL params", () => {
    const poolConfig = poolConfigFor(
      `${BASE_URL}?statement_timeout=45&idle_in_transaction_session_timeout=120`,
      { applyQueryTimeouts: false }
    );

    expect(poolConfig.idle_in_transaction_session_timeout).toBeUndefined();
    expect(poolConfig.statement_timeout).toBeUndefined();
    expect(poolConfig.query_timeout).toBeUndefined();
  });

  test("leaves the pool settings it already computed untouched", () => {
    const poolConfig = poolConfigFor(`${BASE_URL}?connection_limit=7&connect_timeout=3`);

    expect(poolConfig.max).toBe(7);
    expect(poolConfig.connectionTimeoutMillis).toBe(3_000);
  });
});
