import { beforeEach, describe, expect, test, vi } from "vitest";
import {
  TIntegrationAirtableConfig,
  TIntegrationAirtableConfigData,
} from "@forma/types/integration/airtable";

const integrationServiceMock = vi.hoisted(() => ({
  createOrUpdateIntegration: vi.fn(),
  getIntegrationByType: vi.fn(),
}));
vi.mock("@/lib/integration/service", () => integrationServiceMock);

const cacheMock = vi.hoisted(() => ({ cache: { tryLock: vi.fn(), del: vi.fn() } }));
vi.mock("@/lib/cache", () => cacheMock);

const { getAirtableToken, resolveAirtableCredential, writeData } = await import("@/lib/airtable/service");

const jsonResponse = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

const fetchMock = vi.fn();

const configData = {
  baseId: "base_1",
  tableId: "tbl_1",
  tableName: "Responses",
  createdAt: new Date(),
  questionIds: [],
  questions: "",
  surveyId: "survey_1",
  surveyName: "Survey",
} as unknown as TIntegrationAirtableConfigData;

const credential = (expiryDate: Date) => ({
  access_token: "at_1",
  refresh_token: "rt_1",
  expiry_date: expiryDate.toISOString(),
});

const config = (expiryDate: Date): TIntegrationAirtableConfig => ({
  key: credential(expiryDate),
  data: [],
  email: "owner@example.com",
});

const tablesResponse = jsonResponse(200, {
  tables: [{ id: "tbl_1", name: "Responses", fields: [{ id: "fld_1", name: "What is your name?" }] }],
});

beforeEach(() => {
  vi.stubGlobal("fetch", fetchMock);
  // Uncontended by default: every test that is not about the lock behaves as the sole refresher.
  cacheMock.cache.tryLock.mockResolvedValue({ ok: true, data: true });
  cacheMock.cache.del.mockResolvedValue({ ok: true, data: undefined });
});

// The record POST discarded its response, so a rejected write — a 422 from a field Airtable refuses, a
// 429 from the per-base rate limit — resolved exactly like a delivered response and the pipeline logged
// nothing. These assert the failure now reaches the caller.
describe("writeData", () => {
  const callWriteData = () =>
    writeData(credential(new Date(Date.now() + 3_600_000)), configData, ["Ada"], ["What is your name?"]);

  test("resolves when Airtable accepts the record", async () => {
    fetchMock
      .mockResolvedValueOnce(tablesResponse.clone())
      .mockResolvedValueOnce(jsonResponse(200, { id: "rec_1" }));

    await expect(callWriteData()).resolves.toBeUndefined();
  });

  test("rejects when Airtable refuses the record with a 422", async () => {
    fetchMock.mockResolvedValueOnce(tablesResponse.clone()).mockResolvedValueOnce(
      jsonResponse(422, {
        error: { type: "INVALID_VALUE_FOR_COLUMN", message: "Field cannot accept the value" },
      })
    );

    await expect(callWriteData()).rejects.toThrow(
      /Airtable API error creating record: 422 .*INVALID_VALUE_FOR_COLUMN/
    );
  });

  test("rejects when Airtable rate-limits the record with a 429", async () => {
    fetchMock
      .mockResolvedValueOnce(tablesResponse.clone())
      .mockResolvedValueOnce(jsonResponse(429, { error: "RATE_LIMIT_REACHED" }));

    await expect(callWriteData()).rejects.toThrow(/Airtable API error creating record: 429/);
  });
});

// The delivery path read `integration.config.key` straight off the record while only the settings UI
// refreshed, so an integration went dark at its stored expiry. One resolver now owns the lifecycle.
describe("resolveAirtableCredential", () => {
  test("returns the stored credential untouched while it is comfortably valid", async () => {
    const stored = config(new Date(Date.now() + 3_600_000));

    await expect(resolveAirtableCredential("ws_1", stored)).resolves.toEqual(stored.key);
    expect(fetchMock).not.toHaveBeenCalled();
    expect(integrationServiceMock.createOrUpdateIntegration).not.toHaveBeenCalled();
  });

  test("refreshes a token that expires inside the buffer, before it can fail mid-delivery", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(200, { access_token: "at_2", refresh_token: "rt_2", expires_in: 3600 })
    );

    const resolved = await resolveAirtableCredential("ws_1", config(new Date(Date.now() + 120_000)));

    expect(resolved.access_token).toBe("at_2");
    expect(resolved.refresh_token).toBe("rt_2");
  });

  test("persists the rotated refresh token, since Airtable invalidates the old one on use", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(200, { access_token: "at_2", refresh_token: "rt_2", expires_in: 3600 })
    );

    await resolveAirtableCredential("ws_1", config(new Date(Date.now() - 1000)));

    expect(integrationServiceMock.createOrUpdateIntegration).toHaveBeenCalledWith("ws_1", {
      type: "airtable",
      config: {
        data: [],
        email: "owner@example.com",
        key: expect.objectContaining({ access_token: "at_2", refresh_token: "rt_2" }),
      },
    });
  });

  test("surfaces a failure to persist the rotation rather than returning an unrecoverable token", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(200, { access_token: "at_2", refresh_token: "rt_2", expires_in: 3600 })
    );
    integrationServiceMock.createOrUpdateIntegration.mockRejectedValue(new Error("write failed"));

    await expect(resolveAirtableCredential("ws_1", config(new Date(Date.now() - 1000)))).rejects.toThrow(
      "write failed"
    );
  });

  // The worker runs at DEFAULT_WORKER_CONCURRENCY (2-4), so two deliveries for one workspace can reach
  // an expired token together. Airtable rotates the refresh token on first use, so the second POST
  // would come back invalid_grant and lose its response.
  test("reads back the winner's rotation instead of spending the single-use refresh token twice", async () => {
    cacheMock.cache.tryLock.mockResolvedValue({ ok: true, data: false });
    integrationServiceMock.getIntegrationByType.mockResolvedValue({
      config: {
        ...config(new Date(Date.now() + 3_600_000)),
        key: {
          access_token: "at_rotated",
          refresh_token: "rt_rotated",
          expiry_date: new Date(Date.now() + 3_600_000).toISOString(),
        },
      },
    });

    const resolved = await resolveAirtableCredential("ws_1", config(new Date(Date.now() - 1000)));

    expect(resolved.access_token).toBe("at_rotated");
    expect(fetchMock).not.toHaveBeenCalled();
    expect(integrationServiceMock.createOrUpdateIntegration).not.toHaveBeenCalled();
  });

  test("refreshes unserialised when Redis cannot hand out the lock", async () => {
    cacheMock.cache.tryLock.mockResolvedValue({ ok: false, error: { code: "redis_connection_error" } });
    fetchMock.mockResolvedValue(
      jsonResponse(200, { access_token: "at_2", refresh_token: "rt_2", expires_in: 3600 })
    );

    const resolved = await resolveAirtableCredential("ws_1", config(new Date(Date.now() - 1000)));

    expect(resolved.access_token).toBe("at_2");
    expect(cacheMock.cache.del).not.toHaveBeenCalled();
  });

  test("refreshes anyway when the lock holder never publishes a rotation", async () => {
    vi.useFakeTimers();
    cacheMock.cache.tryLock.mockResolvedValue({ ok: true, data: false });
    integrationServiceMock.getIntegrationByType.mockResolvedValue({
      config: config(new Date(Date.now() - 1000)),
    });
    fetchMock.mockResolvedValue(
      jsonResponse(200, { access_token: "at_2", refresh_token: "rt_2", expires_in: 3600 })
    );

    try {
      const pending = resolveAirtableCredential("ws_1", config(new Date(Date.now() - 1000)));
      await vi.advanceTimersByTimeAsync(5_000);

      await expect(pending).resolves.toMatchObject({ access_token: "at_2" });
    } finally {
      vi.useRealTimers();
    }
  });
});

describe("getAirtableToken", () => {
  test("keeps the underlying failure as the error cause", async () => {
    integrationServiceMock.getIntegrationByType.mockResolvedValue(null);

    await expect(getAirtableToken("ws_1")).rejects.toThrow("Failed to get Airtable token");
    await expect(getAirtableToken("ws_1")).rejects.toMatchObject({
      cause: expect.objectContaining({ message: "No Airtable integration found for workspace ws_1" }),
    });
  });

  test("returns the resolved access token for a valid credential", async () => {
    integrationServiceMock.getIntegrationByType.mockResolvedValue({
      config: config(new Date(Date.now() + 3_600_000)),
    });

    await expect(getAirtableToken("ws_1")).resolves.toBe("at_1");
  });
});
