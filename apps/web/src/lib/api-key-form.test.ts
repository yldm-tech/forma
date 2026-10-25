import { expect, test } from "vite-plus/test";
import {
  apiKeyExpiry,
  apiKeyExpiryState,
  apiKeyScope,
  canSubmitApiKey,
  effectiveAccess,
  firstCustomExpiryDay,
  hasDuplicateWorkspace,
  maskedApiKey,
} from "./api-key-form";

const none = { accessControl: { read: false, write: false } };

test("canSubmitApiKey needs a label that is not blank and at least one grant", () => {
  const row = { workspaceId: "w1", permission: "read" as const };
  expect(canSubmitApiKey("CI", [row], none)).toBe(true);
  expect(canSubmitApiKey("CI", [], { accessControl: { read: true, write: false } })).toBe(true);
  expect(canSubmitApiKey("CI", [], { accessControl: { read: false, write: true } })).toBe(true);
  expect(canSubmitApiKey("CI", [], none)).toBe(false);
  expect(canSubmitApiKey("   ", [row], none)).toBe(false);
});

test("hasDuplicateWorkspace", () => {
  expect(
    hasDuplicateWorkspace([
      { workspaceId: "w1", permission: "read" },
      { workspaceId: "w1", permission: "write" },
    ])
  ).toBe(true);
  expect(
    hasDuplicateWorkspace([
      { workspaceId: "w1", permission: "read" },
      { workspaceId: "w2", permission: "read" },
    ])
  ).toBe(false);
});

test("effectiveAccess: write implies read", () => {
  expect(effectiveAccess({ accessControl: { read: false, write: true } })).toEqual({
    accessControl: { read: true, write: true },
  });
  expect(effectiveAccess(none)).toEqual(none);
});

test("apiKeyScope lists the first workspace grants, counts the rest and reads write as including read", () => {
  const grant = (name: string) => ({ workspaceId: name, workspaceName: name, permission: "read" as const });
  expect(
    apiKeyScope({ workspacePermissions: [grant("A"), grant("B"), grant("C")], organizationAccess: none })
  ).toEqual({ shown: [grant("A"), grant("B")], more: 1, organization: null });
  expect(
    apiKeyScope({
      workspacePermissions: [],
      organizationAccess: { accessControl: { read: false, write: true } },
    })
  ).toEqual({ shown: [], more: 0, organization: "write" });
  expect(
    apiKeyScope({
      workspacePermissions: [grant("A")],
      organizationAccess: { accessControl: { read: true, write: false } },
    })
  ).toEqual({ shown: [grant("A")], more: 0, organization: "read" });
});

test("apiKeyExpiry: none, a number of days from now, or the end of a chosen day after today", () => {
  const now = new Date(2026, 9, 26, 15, 30);
  const day = 24 * 60 * 60 * 1000;
  expect(apiKeyExpiry("never", "", now)).toBeNull();
  expect(apiKeyExpiry("7", "", now)?.getTime()).toBe(now.getTime() + 7 * day);
  expect(apiKeyExpiry("365", "2027-01-01", now)?.getTime()).toBe(now.getTime() + 365 * day);
  expect(apiKeyExpiry("custom", "2026-10-27", now)).toEqual(new Date(2026, 9, 27, 23, 59, 59));
  expect(apiKeyExpiry("custom", "2028-02-29", now)).toEqual(new Date(2028, 1, 29, 23, 59, 59));
  // Missing, malformed, impossible, today or earlier: nothing to send.
  for (const bad of ["", "2026-13-01", "2027-02-30", "27-01-01", "2026-10-26", "2025-12-31"]) {
    expect(apiKeyExpiry("custom", bad, now)).toBeUndefined();
  }
});

test("firstCustomExpiryDay is tomorrow, across a month and a year", () => {
  expect(firstCustomExpiryDay(new Date(2026, 9, 26, 23, 59))).toBe("2026-10-27");
  expect(firstCustomExpiryDay(new Date(2026, 9, 31, 0, 0))).toBe("2026-11-01");
  expect(firstCustomExpiryDay(new Date(2026, 11, 31, 12, 0))).toBe("2027-01-01");
});

test("apiKeyExpiryState: never, active, within a week, or expired from the instant of the expiry", () => {
  const now = new Date("2026-10-26T00:00:00Z");
  const at = (ms: number) => ({ expiresAt: new Date(now.getTime() + ms).toISOString() });
  const day = 24 * 60 * 60 * 1000;
  expect(apiKeyExpiryState({ expiresAt: null }, now)).toBe("never");
  expect(apiKeyExpiryState(at(8 * day), now)).toBe("active");
  expect(apiKeyExpiryState(at(7 * day), now)).toBe("soon");
  expect(apiKeyExpiryState(at(1), now)).toBe("soon");
  expect(apiKeyExpiryState(at(0), now)).toBe("expired");
  expect(apiKeyExpiryState(at(-day), now)).toBe("expired");
});

test("maskedApiKey shows the prefix and the last four characters", () => {
  expect(maskedApiKey("Ab-_")).toBe("fbk_…Ab-_");
});
