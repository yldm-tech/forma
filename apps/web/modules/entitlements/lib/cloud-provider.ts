import "server-only";
import { logger } from "@forma/logger";
import { getOrganizationBillingWithReadThroughSync } from "@/modules/billing/lib/organization-billing";
import { getEnterpriseLicense } from "@/modules/license-check/lib/license";
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
 * `IS_FORMA_CLOUD=1` moves the entitlement source from the licence to the Stripe subscription. Features are not sold separately on this fork, so an organization with no subscription gets the whole product rather than nothing: without this, setting the switch to reach the billing surface would turn off contacts, quotas, RBAC, AI smart tools, workflows, follow-ups, custom links and bulk invite in one go, and cap the organization at one workspace. This is the cloud half of the same decision `license.ts` makes for the licence half.
 *
 * It fires only when Stripe resolved no features at all, so a real subscription still wins and the cloud entitlement path — sync, cache, trial handling — stays reachable and testable rather than becoming dead code.
 *
 * The full known set is granted rather than the licence-mapped subset that `self-hosted-provider.ts` builds, because follow-ups, custom links, custom redirect URLs and bulk invite have no licence feature behind them and self-hosted grants them unconditionally. Granting only the mapped subset would leave a cloud instance behaving like neither.
 */
const hasNoStripeEntitlements = (stripeFeatures: readonly TEntitlementFeature[]): boolean =>
  stripeFeatures.length === 0;

const ungatedLimits: TEntitlementLimits = {
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
    // No billing record means no subscription to read, which is the ungated case by definition.
    logger.warn({ organizationId }, "Organization billing not found, granting ungated entitlements");

    return {
      organizationId,
      source: "cloud_stripe",
      features: [...KNOWN_ENTITLEMENT_FEATURES],
      limits: ungatedLimits,
      licenseActive: license.active,
      licenseStatus: license.status,
      licenseFeatures: license.features,
      stripeCustomerId: null,
      subscriptionStatus: null,
      usageCycleAnchor: null,
    };
  }

  const stripeFeatures = (billing.stripe?.features ?? []).filter(isEntitlementFeature);
  const ungated = hasNoStripeEntitlements(stripeFeatures);

  return {
    organizationId,
    source: "cloud_stripe",
    features: ungated ? [...KNOWN_ENTITLEMENT_FEATURES] : stripeFeatures,
    limits: ungated
      ? ungatedLimits
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
