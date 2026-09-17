import "server-only";
import { logger } from "@forma/logger";
import { E2E_TESTING, IS_DEVELOPMENT } from "@/lib/constants";
import {
  getDefaultOrganizationBilling,
  getOrganizationBillingWithReadThroughSync,
} from "@/modules/ee/billing/lib/organization-billing";
import { getEnterpriseLicense } from "@/modules/ee/license-check/lib/license";
import {
  KNOWN_ENTITLEMENT_FEATURES,
  type TEntitlementFeature,
  type TEntitlementLimits,
  type TOrganizationEntitlementsContext,
  isEntitlementFeature,
} from "./types";

const toDateOrNull = (value: Date | string | null | undefined): Date | null => {
  if (!value) return null;
  const parsed = value instanceof Date ? value : new Date(value);
  return Number.isNaN(parsed.getTime()) ? null : parsed;
};

/**
 * `IS_FORMA_CLOUD=1` moves the entitlement source from the enterprise licence to the Stripe subscription, so a developer who sets it to work on the billing surface otherwise loses every licensed feature at once: contacts, quotas, RBAC, AI smart tools, workflows, follow-ups, custom links and bulk invite all resolve their entitlement from a subscription that does not exist on a local install. `license.ts` already takes this permission for the licence half of the same decision — the EE licence allows copying and modifying the software for development and testing — and this is the cloud half of it.
 *
 * Two details mirror that branch deliberately. The gate is positive on development or E2E rather than negative on production, so an unset NODE_ENV stays locked instead of accidentally unlocking. And it only fires when Stripe resolved no features at all, so a real subscription in development still runs the ordinary path and the cloud entitlement logic itself stays testable.
 *
 * The full known set is granted rather than the licence-mapped subset that `self-hosted-provider.ts` builds, because follow-ups, custom links, custom redirect URLs and bulk invite have no licence feature behind them and self-hosted grants them unconditionally. Granting only the mapped subset would leave a local cloud instance behaving like neither of the two real modes.
 */
const isDevelopmentUnlock = (stripeFeatures: readonly TEntitlementFeature[]): boolean =>
  stripeFeatures.length === 0 && (IS_DEVELOPMENT || E2E_TESTING);

const unlockedLimits: TEntitlementLimits = {
  workspaces: null,
  monthlyResponses: null,
  monthlyWorkflowRuns: null,
};

export const getCloudOrganizationEntitlementsContext = async (
  organizationId: string
): Promise<TOrganizationEntitlementsContext> => {
  const [billing, license] = await Promise.all([
    getOrganizationBillingWithReadThroughSync(organizationId),
    getEnterpriseLicense(),
  ]);

  if (!billing) {
    logger.warn({ organizationId }, "Organization billing not found, using default entitlements");
    const defaultBilling = getDefaultOrganizationBilling();
    const unlocked = isDevelopmentUnlock([]);

    return {
      organizationId,
      source: "cloud_stripe",
      features: unlocked ? [...KNOWN_ENTITLEMENT_FEATURES] : [],
      limits: unlocked
        ? unlockedLimits
        : {
            workspaces: defaultBilling.limits?.workspaces ?? null,
            monthlyResponses: defaultBilling.limits?.monthly?.responses ?? null,
            monthlyWorkflowRuns: defaultBilling.limits?.monthly?.workflowRuns ?? null,
          },
      licenseActive: license.active,
      licenseStatus: license.status,
      licenseFeatures: license.features,
      stripeCustomerId: null,
      subscriptionStatus: null,
      usageCycleAnchor: null,
    };
  }

  const stripeFeatures = (billing.stripe?.features ?? []).filter(isEntitlementFeature);
  const unlocked = isDevelopmentUnlock(stripeFeatures);

  return {
    organizationId,
    source: "cloud_stripe",
    features: unlocked ? [...KNOWN_ENTITLEMENT_FEATURES] : stripeFeatures,
    limits: unlocked
      ? unlockedLimits
      : {
          workspaces: billing.limits?.workspaces ?? null,
          monthlyResponses: billing.limits?.monthly?.responses ?? null,
          monthlyWorkflowRuns: billing.limits?.monthly?.workflowRuns ?? null,
        },
    licenseActive: license.active,
    licenseStatus: license.status,
    licenseFeatures: license.features,
    stripeCustomerId: billing.stripeCustomerId ?? null,
    subscriptionStatus: billing.stripe?.subscriptionStatus ?? null,
    usageCycleAnchor: toDateOrNull(billing.usageCycleAnchor),
  };
};
