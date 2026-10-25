import { type Mock, beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { ApiClient } from "@/lib/common/api";
import { Config } from "@/lib/common/config";
import { Logger } from "@/lib/common/logger";
import { prefetchSurveysScript } from "@/lib/survey/widget";
import {
  mockAppUrl,
  mockAttributes,
  mockUserId,
  mockWorkspaceId,
} from "@/lib/user/tests/__mocks__/update.mock";
import { getSkippedAttributeKeys, sendUpdates, sendUpdatesToBackend } from "@/lib/user/update";
import { type TUpdates } from "@/types/config";

vi.mock("@/lib/common/config", () => ({
  Config: {
    getInstance: vi.fn(() => ({
      get: vi.fn(),
      update: vi.fn(),
    })),
  },
}));

vi.mock("@/lib/common/logger", () => ({
  Logger: {
    getInstance: vi.fn(() => ({
      debug: vi.fn(),
    })),
  },
}));

vi.mock("@/lib/common/utils", () => ({
  filterSurveys: vi.fn(),
  getIsDebug: vi.fn(),
}));

vi.mock("@/lib/common/api", () => ({
  ApiClient: vi.fn(function MockApiClient(this: { createOrUpdateUser: ReturnType<typeof vi.fn> }) {
    this.createOrUpdateUser = vi.fn();
  }),
}));

vi.mock("@/lib/survey/widget", () => ({
  prefetchSurveysScript: vi.fn(),
}));

describe("sendUpdatesToBackend", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  test("sends user updates to backend and returns updated state", async () => {
    const mockResponse = {
      ok: true,
      data: {
        state: {
          data: {
            userId: mockUserId,
            attributes: mockAttributes,
          },
        },
      },
    };

    (ApiClient as Mock).mockImplementation(function MockApiClient() {
      return {
        createOrUpdateUser: vi.fn().mockResolvedValue(mockResponse),
      };
    });

    const result = await sendUpdatesToBackend({
      appUrl: mockAppUrl,
      workspaceId: mockWorkspaceId,
      updates: { userId: mockUserId, attributes: mockAttributes },
    });

    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.data.state.data).toEqual({ userId: mockUserId, attributes: mockAttributes });
    }
  });

  test("returns network error if API call fails", async () => {
    const mockUpdates: TUpdates = { userId: mockUserId, attributes: mockAttributes };

    (ApiClient as Mock).mockImplementation(function MockApiClient() {
      return {
        createOrUpdateUser: vi.fn().mockResolvedValue({
          ok: false,
          error: { code: "network_error", message: "Request failed", status: 500 },
        }),
      };
    });

    const result = await sendUpdatesToBackend({
      appUrl: mockAppUrl,
      workspaceId: mockWorkspaceId,
      updates: mockUpdates,
    });

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.code).toBe("network_error");
      expect(result.error.message).toBe("Error updating user with userId user_123");
    }
  });

  test("returns error if network request fails", async () => {
    const mockUpdates: TUpdates = { userId: mockUserId, attributes: { plan: "premium" } };

    (ApiClient as Mock).mockImplementation(function MockApiClient() {
      return {
        createOrUpdateUser: vi.fn().mockRejectedValue(new Error("Network error")),
      };
    });

    const result = await sendUpdatesToBackend({
      appUrl: mockAppUrl,
      workspaceId: mockWorkspaceId,
      updates: mockUpdates,
    });

    expect(result.ok).toBe(false);

    if (!result.ok) {
      expect(result.error.code).toBe("network_error");
    }
  });
});

describe("sendUpdates", () => {
  beforeEach(() => {
    (Config.getInstance as Mock).mockImplementation(() => ({
      get: vi.fn().mockReturnValue({
        appUrl: mockAppUrl,
        workspaceId: mockWorkspaceId,
        workspace: {
          data: {
            surveys: [],
          },
        },
      }),
      update: vi.fn(),
    }));

    (Logger.getInstance as Mock).mockImplementation(() => ({
      debug: vi.fn(),
    }));
  });

  test("successfully processes updates", async () => {
    const mockResponse = {
      ok: true,
      data: {
        state: {
          data: {
            userId: mockUserId,
            attributes: mockAttributes,
          },
          expiresAt: new Date(Date.now() + 1000 * 60 * 30),
        },
      },
    };

    (ApiClient as Mock).mockImplementation(function MockApiClient() {
      return {
        createOrUpdateUser: vi.fn().mockResolvedValue(mockResponse),
      };
    });

    const result = await sendUpdates({ updates: { userId: mockUserId, attributes: mockAttributes } });

    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.data.skippedAttributeKeys).toEqual(new Set());
    }

    // Identification is one of the two points where an empty survey set can become non-empty, so the
    // prefetch setup skipped has to be retried here.
    expect(prefetchSurveysScript).toHaveBeenCalledWith(mockAppUrl);
  });

  test("reports the attribute keys the server skipped, from both messages and errors", async () => {
    const logger = { debug: vi.fn(), error: vi.fn() };
    (Logger.getInstance as Mock).mockImplementation(() => logger);

    // Server text as built by apps/server/internal/contact/identify.go messageTemplates. The type mismatch arrives in `messages` from servers that have not moved it to `errors` yet.
    const typeMismatch =
      "Attribute 'seats' expects a number but received a string (attribute 'seats' has dataType: number)";
    const invalidKeys =
      "Skipped creating attribute(s) with invalid key(s): Bad Key, 1st. Keys must only contain lowercase letters, numbers, and underscores, and must start with a letter.";
    (ApiClient as Mock).mockImplementation(function MockApiClient() {
      return {
        createOrUpdateUser: vi.fn().mockResolvedValue({
          ok: true,
          data: {
            state: { data: { userId: mockUserId }, expiresAt: new Date(Date.now() + 60_000) },
            messages: [typeMismatch, "Created new attribute 'plan' with type 'string'"],
            errors: [invalidKeys],
          },
        }),
      };
    });

    const result = await sendUpdates({
      updates: { userId: mockUserId, attributes: { seats: "30", plan: "pro", "Bad Key": "x", "1st": "y" } },
    });

    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.data.skippedAttributeKeys).toEqual(new Set(["seats", "Bad Key", "1st"]));
    }
    expect(logger.error).toHaveBeenCalledWith(invalidKeys);
  });

  test("handles backend errors", async () => {
    const mockErrorResponse = {
      ok: false,
      error: {
        code: "invalid_request",
        status: 400,
        message: "Invalid request",
      },
    };

    (ApiClient as Mock).mockImplementation(function MockApiClient() {
      return {
        createOrUpdateUser: vi.fn().mockResolvedValue(mockErrorResponse),
      };
    });

    const result = await sendUpdates({ updates: { userId: mockUserId, attributes: mockAttributes } });

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.code).toBe("invalid_request");
    }
    expect(prefetchSurveysScript).not.toHaveBeenCalled();
  });

  test("handles unexpected errors", async () => {
    (ApiClient as Mock).mockImplementation(function MockApiClient() {
      return {
        createOrUpdateUser: vi.fn().mockRejectedValue(new Error("Unexpected error")),
      };
    });

    const result = await sendUpdates({ updates: { userId: mockUserId, attributes: mockAttributes } });

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.code).toBe("network_error");
      expect(result.error.status).toBe(500);
    }
  });
});

describe("getSkippedAttributeKeys", () => {
  test("reads a type mismatch whether it arrives in messages or in errors", () => {
    const text =
      "Attribute 'seats' expects a number but received a string (attribute 'seats' has dataType: number)";
    expect(getSkippedAttributeKeys([text], [])).toEqual(new Set(["seats"]));
    expect(getSkippedAttributeKeys([], [text])).toEqual(new Set(["seats"]));
  });

  test("reads reserved keys and an ignored language", () => {
    const reserved =
      "Reserved attribute key(s): firstname, lastname. These keys are reserved for the v5.1 safe-identifier default attribute migration and cannot be created as custom attributes.";
    expect(
      getSkippedAttributeKeys(
        ["Ignored invalid language code 'xx'. The existing value was preserved."],
        [reserved]
      )
    ).toEqual(new Set(["firstname", "lastname", "language"]));
  });

  test("does not treat informational messages as skipped keys", () => {
    expect(
      getSkippedAttributeKeys(
        [
          "Created new attribute 'plan' with type 'string'",
          "Either email or userId is required. The existing values were preserved.",
        ],
        undefined
      )
    ).toEqual(new Set());
  });
});
