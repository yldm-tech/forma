import { expect, test } from "vite-plus/test";
import type { BillingSummary } from "../../../lib/queries/billing";
import { maxUploadSizeMB } from "./limits";

const summary = (isFormaCloud: boolean, workspaces: number | null): BillingSummary => ({
  isFormaCloud,
  plan: "hobby",
  subscriptionStatus: null,
  trialEnd: null,
  trialDaysRemaining: null,
  limits: { workspaces, monthly: { responses: 250 } },
  usage: null,
  usageCycle: null,
});

test("Forma Cloud caps uploads at 10 MB for a one-workspace plan and 1 GB above it", () => {
  expect(maxUploadSizeMB(summary(true, 1))).toBe(10);
  expect(maxUploadSizeMB(summary(true, 3))).toBe(1024);
  expect(maxUploadSizeMB(summary(true, null))).toBe(1024);
});

test("self-hosted, or before the summary is known, nothing is capped", () => {
  expect(maxUploadSizeMB(summary(false, 1))).toBeNull();
  expect(maxUploadSizeMB(undefined)).toBeNull();
});
