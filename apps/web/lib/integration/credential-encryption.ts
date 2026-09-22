import { logger } from "@forma/logger";
import { ENCRYPTION_KEY } from "@/lib/constants";
import { symmetricDecrypt, symmetricEncrypt } from "@/lib/crypto";
import { TIntegrationWithConfig, isSecretKeyField } from "./redact-credentials";

/**
 * Encrypt-on-write / decrypt-on-read for the OAuth credentials stored in `Integration.config.key`.
 *
 * Notion encrypted its access token in its own OAuth callback and decrypted it in its own service, so the property held for exactly one destination: Slack, Google Sheets and Airtable wrote theirs in cleartext, including the two indefinite-lifetime `refresh_token`s that grant access to the connected Google and Airtable accounts long after the member's Forma access is revoked. Making it a property of the store instead means the fifth provider is covered the day it arrives, which is the same reason {@link isSecretKeyField} keys off a pattern rather than a list.
 *
 * What this defends is the at-rest surface — a database dump, a read replica, a backup snapshot, a logged query, SELECT access to one JSON column — and not an application compromise, because the process holds `ENCRYPTION_KEY` either way. That is the same boundary the repo already accepted for 2FA secrets.
 *
 * Rotating `ENCRYPTION_KEY` invalidates every stored integration credential and every workspace has to reconnect. Nothing here can detect that case as distinct from corruption, so it is an operational constraint, not a code path.
 */

// 16-byte IV and 16-byte GCM auth tag, hex-encoded by `symmetricEncrypt`.
const AES_IV_HEX_LENGTH = 32;
const AES_GCM_TAG_HEX_LENGTH = 32;
const HEX_ONLY = /^[0-9a-f]*$/i;

type TCredentialForm = "gcm" | "legacy-cbc" | "cleartext";

/**
 * Decide which of the three forms a stored value is in.
 *
 * `crypto.ts` discriminates on colon count alone, which is safe there because its callers only ever hand it something this codebase encrypted. Here the same value may be a provider's raw token, so the shape is checked as well: every token these four providers issue (`xoxb-…`, `ya29.…`, `ntn_…`, `oaa…`) is colon-free, and requiring a 32-hex IV on top of the colon count means no plaintext can be mistaken for ciphertext even if a provider starts issuing colons.
 */
const classifyCredential = (value: string): TCredentialForm => {
  const parts = value.split(":");
  const iv = parts[0];
  if (iv === undefined || iv.length !== AES_IV_HEX_LENGTH || !HEX_ONLY.test(iv)) {
    return "cleartext";
  }

  if (
    parts.length === 3 &&
    HEX_ONLY.test(parts[1]) &&
    parts[2].length === AES_GCM_TAG_HEX_LENGTH &&
    HEX_ONLY.test(parts[2])
  ) {
    return "gcm";
  }

  // Pre-GCM AES-256-CBC, which is what a Notion integration connected before that switch still holds.
  if (parts.length === 2 && parts[1].length > 0 && HEX_ONLY.test(parts[1])) {
    return "legacy-cbc";
  }

  return "cleartext";
};

const encryptCredential = (value: string): string => {
  // An empty string carries nothing to protect, and `googleSheet/service.ts` branches on a falsy
  // `refresh_token` to raise the reconnect error — encrypting it would turn that branch off.
  if (value === "") {
    return value;
  }

  // Already ciphertext: a Notion row written by the callback's own call, or any row read through
  // `decryptIntegrationCredentials` whose decryption failed and which was handed back unchanged.
  // Re-encrypting it would bury the real value under a second layer nothing knows to peel off.
  if (classifyCredential(value) !== "cleartext") {
    return value;
  }

  return symmetricEncrypt(value, ENCRYPTION_KEY);
};

const decryptCredential = (value: string): string => {
  const form = classifyCredential(value);

  // Cleartext is what every Slack, Google Sheets and Airtable row holds until its next write. Returning
  // it as-is is the whole migration: the row is re-encrypted the next time anything saves it, so no
  // backfill runs and no integration disconnects on deploy.
  if (form === "cleartext") {
    return value;
  }

  try {
    // `allowLegacyCbc` only for the two-part form, and only because the value is at rest in
    // `Integration.config`, written by this app, with nothing on a request able to influence it.
    return symmetricDecrypt(value, ENCRYPTION_KEY, { allowLegacyCbc: form === "legacy-cbc" });
  } catch (err) {
    // A wrong or rotated `ENCRYPTION_KEY`. Throwing here would take down the whole integrations
    // settings page, because one unreadable row travels with every other row the list query returned.
    // Handing the value back instead fails at the provider, as an expired token does, and the way out
    // of both is the same: reconnect.
    logger.warn({ err }, "Could not decrypt a stored integration credential; leaving it as stored");
    return value;
  }
};

const mapSecretFields = (
  key: Record<string, unknown>,
  transform: (value: string) => string
): Record<string, unknown> => {
  const next: Record<string, unknown> = { ...key };
  for (const field of Object.keys(next)) {
    const value = next[field];
    // String values only, for the same reason the redaction pass restricts itself to them: every
    // credential these providers issue is a string, and Slack's `team` or Google's `expiry_date` would
    // stop matching its schema if it came back as one.
    if (isSecretKeyField(field) && typeof value === "string") {
      next[field] = transform(value);
    }
  }
  return next;
};

const mapIntegrationKey = <T extends TIntegrationWithConfig>(
  integration: T,
  transform: (value: string) => string
): T => {
  if (!integration?.config?.key || typeof integration.config.key !== "object") {
    return integration;
  }

  return {
    ...integration,
    config: { ...integration.config, key: mapSecretFields(integration.config.key, transform) },
  };
};

/** Applied on every write, so a credential reaches Postgres as `iv:ciphertext:tag` whatever wrote it. */
export const encryptIntegrationCredentials = <T extends TIntegrationWithConfig>(integration: T): T =>
  mapIntegrationKey(integration, encryptCredential);

/** Applied on every read, so callers see the plaintext token regardless of which form the row holds. */
export const decryptIntegrationCredentials = <T extends TIntegrationWithConfig>(integration: T): T =>
  mapIntegrationKey(integration, decryptCredential);
