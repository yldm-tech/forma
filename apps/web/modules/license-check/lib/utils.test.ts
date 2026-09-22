import { beforeEach, describe, expect, test, vi } from "vitest";

const { constantsMock } = vi.hoisted(() => ({
  constantsMock: { AUDIT_LOG_ENABLED: true, IS_RECAPTCHA_CONFIGURED: true },
}));

vi.mock("server-only", () => ({}));

vi.mock("@/lib/constants", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(typeof actual === "object" && actual !== null ? actual : {}),
    get AUDIT_LOG_ENABLED() {
      return constantsMock.AUDIT_LOG_ENABLED;
    },
    get IS_RECAPTCHA_CONFIGURED() {
      return constantsMock.IS_RECAPTCHA_CONFIGURED;
    },
  };
});

const utils = await import("./utils");

const ALWAYS_ON = [
  "getIsMultiOrgEnabled",
  "getIsContactsEnabled",
  "getIsTwoFactorAuthEnabled",
  "getIsSsoEnabled",
  "getIsSamlSsoEnabled",
  "getIsQuotasEnabled",
  "getIsAISmartToolsEnabled",
  "getIsWorkflowsEnabled",
  "getAccessControlPermission",
  "getRemoveBrandingPermission",
  "getWhiteLabelPermission",
  "getBulkInvitePermission",
] as const;

beforeEach(() => {
  constantsMock.AUDIT_LOG_ENABLED = true;
  constantsMock.IS_RECAPTCHA_CONFIGURED = true;
});

describe("what this installation can do", () => {
  test.each(ALWAYS_ON)("%s is on, because nothing sells it separately", async (name) => {
    await expect(utils[name]()).resolves.toBe(true);
  });

  test("the workspace count is unlimited rather than a number to compare against", async () => {
    await expect(utils.getOrganizationWorkspacesLimit()).resolves.toBe(Infinity);
  });

  test("audit logging follows its own deployment switch, not an entitlement", async () => {
    await expect(utils.getIsAuditLogsEnabled()).resolves.toBe(true);

    constantsMock.AUDIT_LOG_ENABLED = false;
    await expect(utils.getIsAuditLogsEnabled()).resolves.toBe(false);
  });

  test("spam protection follows whether reCAPTCHA is configured, since without keys there is nothing to call", async () => {
    await expect(utils.getIsSpamProtectionEnabled()).resolves.toBe(true);

    constantsMock.IS_RECAPTCHA_CONFIGURED = false;
    await expect(utils.getIsSpamProtectionEnabled()).resolves.toBe(false);
  });
});
