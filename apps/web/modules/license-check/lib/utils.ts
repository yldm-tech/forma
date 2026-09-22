import "server-only";
import { AUDIT_LOG_ENABLED, IS_RECAPTCHA_CONFIGURED } from "@/lib/constants";

/**
 * What this installation can do.
 *
 * Every answer here used to be computed: a licence server told the instance which features it had, a Stripe subscription told an organization which entitlements it had, and seventeen helpers combined the two. None of that survives — features are not sold separately, so every one of those questions had a single possible answer.
 *
 * Two are still real, and neither is about entitlement. Audit logging is a deployment choice, and spam protection cannot work without reCAPTCHA credentials, so both read their own constant.
 *
 * The rest are kept as functions, rather than deleted outright, only until their call sites stop asking. Each one is a `true` that some component still awaits before rendering something it would render anyway.
 */

export const getIsMultiOrgEnabled = async (): Promise<boolean> => true;

export const getIsContactsEnabled = async (): Promise<boolean> => true;

export const getIsTwoFactorAuthEnabled = async (): Promise<boolean> => true;

export const getIsSsoEnabled = async (): Promise<boolean> => true;

export const getIsSamlSsoEnabled = async (): Promise<boolean> => true;

export const getIsQuotasEnabled = async (): Promise<boolean> => true;

export const getIsAISmartToolsEnabled = async (): Promise<boolean> => true;

export const getIsWorkflowsEnabled = async (): Promise<boolean> => true;

export const getAccessControlPermission = async (): Promise<boolean> => true;

export const getRemoveBrandingPermission = async (): Promise<boolean> => true;

export const getWhiteLabelPermission = async (): Promise<boolean> => true;

export const getBulkInvitePermission = async (): Promise<boolean> => true;

/** No licence caps the count, so nothing here does either. */
export const getOrganizationWorkspacesLimit = async (): Promise<number> => Infinity;

/** A deployment choice rather than an entitlement: audit events cost storage. */
export const getIsAuditLogsEnabled = async (): Promise<boolean> => AUDIT_LOG_ENABLED;

/** Not a gate but a prerequisite — without reCAPTCHA credentials there is nothing to call. */
export const getIsSpamProtectionEnabled = async (): Promise<boolean> => IS_RECAPTCHA_CONFIGURED;
