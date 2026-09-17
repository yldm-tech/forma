import "server-only";

/**
 * The exact string the v1 management routes have always returned for a missing contacts
 * entitlement — kept verbatim for API consumers that match on it.
 */
export const CONTACTS_API_V1_NOT_ENABLED_MESSAGE =
  "Contacts are only enabled for Enterprise Edition, please upgrade.";
