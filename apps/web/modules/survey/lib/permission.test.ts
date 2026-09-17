import { beforeEach, describe, expect, test, vi } from "vitest";
import { OperationNotAllowedError } from "@forma/types/errors";
import { getIsSpamProtectionEnabled } from "@/modules/license-check/lib/utils";
import { checkSpamProtectionPermission, getExternalUrlsPermission } from "./permission";

vi.mock("@/modules/license-check/lib/utils", () => ({
  getIsSpamProtectionEnabled: vi.fn(),
}));

beforeEach(() => {
  vi.clearAllMocks();
});

describe("checkSpamProtectionPermission", () => {
  test("passes when reCAPTCHA is configured", async () => {
    vi.mocked(getIsSpamProtectionEnabled).mockResolvedValue(true);

    await expect(checkSpamProtectionPermission()).resolves.toBeUndefined();
  });

  test("throws when it is not, because there are no credentials to call with", async () => {
    vi.mocked(getIsSpamProtectionEnabled).mockResolvedValue(false);

    await expect(checkSpamProtectionPermission()).rejects.toBeInstanceOf(OperationNotAllowedError);
  });
});

describe("getExternalUrlsPermission", () => {
  test("is on, because custom redirects and links are part of the product", async () => {
    await expect(getExternalUrlsPermission()).resolves.toBe(true);
  });
});
