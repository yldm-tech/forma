import "server-only";
import { cache as reactCache } from "react";
import { TEnterpriseLicenseFeatures, TLicenseStatus } from "@/modules/license-check/types/enterprise-license";

/**
 * Every feature, for every install.
 *
 * Features are not sold separately here, so there is nothing to unlock and nothing to check. What used to live in this file — a call to a licence server, a Redis-backed cache with a single-flight lock, a three-day grace period, a pending-downgrade schedule and a set of fallback levels — existed to answer a question this build no longer asks.
 *
 * The shape is kept so callers read the same object they always did. `status` and `active` describe a licence that is permanently present; `lastChecked` is the current time because nothing is fetched; `isPendingDowngrade` is false because nothing can downgrade.
 */
const UNGATED_FEATURES: TEnterpriseLicenseFeatures = {
  isMultiOrgEnabled: true,
  contacts: true,
  workspaces: null,
  whitelabel: true,
  removeBranding: true,
  twoFactorAuth: true,
  sso: true,
  saml: true,
  spamProtection: true,
  aiSmartTools: true,
  auditLogs: true,
  accessControl: true,
  quotas: true,
  feedbackDirectories: true,
  dashboards: true,
  workflows: true,
};

export type TEnterpriseLicenseResult = {
  active: boolean;
  features: TEnterpriseLicenseFeatures | null;
  lastChecked: Date;
  isPendingDowngrade: boolean;
  status: TLicenseStatus;
};

export const getEnterpriseLicense = reactCache(async (): Promise<TEnterpriseLicenseResult> => ({
  active: true,
  features: UNGATED_FEATURES,
  lastChecked: new Date(),
  isPendingDowngrade: false,
  status: "active" as const,
}));

export const getLicenseFeatures = async (): Promise<TEnterpriseLicenseFeatures | null> => {
  const licenseState = await getEnterpriseLicense();
  return licenseState.active ? licenseState.features : null;
};
