import { beforeEach, describe, expect, test, vi } from "vitest";

const checkDatabaseHealth = vi.fn();

vi.mock("@/modules/api/v2/health/lib/health-checks", () => ({
  checkDatabaseHealth: () => checkDatabaseHealth(),
}));

const { GET } = await import("./route");

describe("readiness route", () => {
  beforeEach(() => {
    checkDatabaseHealth.mockReset();
  });

  test("reports ready while the database answers", async () => {
    checkDatabaseHealth.mockResolvedValue({ ok: true, data: true });

    const response = await GET();

    expect(response.status).toBe(200);
    expect(response.headers.get("Cache-Control")).toBe("no-store");
    await expect(response.json()).resolves.toEqual({ status: "ok", main_database: true });
  });

  // The point of the route: a probe that cannot return a non-2xx lets a rollout whose new pods cannot
  // reach Postgres go Ready and replace every serving pod.
  test("reports not ready with 503 when the database check fails", async () => {
    checkDatabaseHealth.mockResolvedValue({
      ok: false,
      error: { type: "internal_server_error", details: [] },
    });

    const response = await GET();

    expect(response.status).toBe(503);
    expect(response.headers.get("Cache-Control")).toBe("no-store");
    await expect(response.json()).resolves.toEqual({ status: "error", main_database: false });
  });
});
