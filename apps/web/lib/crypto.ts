import { compare, hash } from "bcryptjs";
import {
  createCipheriv,
  createDecipheriv,
  createHash,
  createHmac,
  randomBytes,
  timingSafeEqual,
} from "node:crypto";
import { logger } from "@forma/logger";
import { ENCRYPTION_KEY } from "@/lib/constants";

const ALGORITHM_V1 = "aes256";
const ALGORITHM_V2 = "aes-256-gcm";
const INPUT_ENCODING = "utf8";
const OUTPUT_ENCODING = "hex";
const BUFFER_ENCODING = ENCRYPTION_KEY.length === 32 ? "latin1" : "hex";
const IV_LENGTH = 16; // AES blocksize

/**
 *
 * @param text Value to be encrypted
 * @param key Key used to encrypt value must be 32 bytes for AES256 encryption algorithm
 *
 * @returns Encrypted value using key
 */
export const symmetricEncrypt = (text: string, key: string) => {
  const _key = Buffer.from(key, BUFFER_ENCODING);
  const iv = randomBytes(IV_LENGTH);
  const cipher = createCipheriv(ALGORITHM_V2, _key, iv);
  let ciphered = cipher.update(text, INPUT_ENCODING, OUTPUT_ENCODING);
  ciphered += cipher.final(OUTPUT_ENCODING);
  const tag = cipher.getAuthTag().toString(OUTPUT_ENCODING);
  return `${iv.toString(OUTPUT_ENCODING)}:${ciphered}:${tag}`;
};

/**
 * Legacy AES-256-CBC. Unauthenticated: the IV is read verbatim out of the payload and there is no auth tag, so an attacker who holds one valid ciphertext can mint unlimited variants whose first plaintext block they choose, and every one of them decrypts without error. Never reachable from a caller that has not opted in — see `symmetricDecrypt`.
 *
 * @param text Value to decrypt
 * @param key Key used to decrypt value must be 32 bytes for AES256 encryption algorithm
 */

const symmetricDecryptV1 = (text: string, key: string): string => {
  const _key = Buffer.from(key, BUFFER_ENCODING);

  const components = text.split(":");
  const iv_from_ciphertext = Buffer.from(components.shift() ?? "", OUTPUT_ENCODING);
  const decipher = createDecipheriv(ALGORITHM_V1, _key, iv_from_ciphertext);
  let deciphered = decipher.update(components.join(":"), OUTPUT_ENCODING, INPUT_ENCODING);
  deciphered += decipher.final(INPUT_ENCODING);

  return deciphered;
};

/**
 *
 * @param text Value to decrypt
 * @param key Key used to decrypt value must be 32 bytes for AES256 encryption algorithm
 */

const symmetricDecryptV2 = (text: string, key: string): string => {
  // split into [ivHex, encryptedHex, tagHex]
  const [ivHex, encryptedHex, tagHex] = text.split(":");
  const _key = Buffer.from(key, BUFFER_ENCODING);
  const iv = Buffer.from(ivHex, OUTPUT_ENCODING);
  const decipher = createDecipheriv(ALGORITHM_V2, _key, iv);
  decipher.setAuthTag(Buffer.from(tagHex, OUTPUT_ENCODING));
  let decrypted = decipher.update(encryptedHex, OUTPUT_ENCODING, INPUT_ENCODING);
  decrypted += decipher.final(INPUT_ENCODING);
  return decrypted;
};

export type TSymmetricDecryptOptions = {
  /**
   * Accept the legacy two-part `iv:ciphertext` form and decrypt it with unauthenticated AES-256-CBC. Off by default, and the default is the security boundary: a payload in that form carries no auth tag, so any string a caller was handed decrypts to *something* rather than being rejected.
   *
   * Only pass `true` when the ciphertext's integrity is already established by something other than this function — it came out of our own database and no request can influence it. A value that arrives on a request, in a URL parameter, a body field, or an unverified token, never qualifies, however deeply nested it is.
   */
  allowLegacyCbc?: boolean;
};

/**
 * Decrypt a payload produced by `symmetricEncrypt` (V2, `iv:ciphertext:tag`, AES-256-GCM).
 *
 * A two-part `iv:ciphertext` payload is the pre-GCM V1 format. It is rejected unless the caller passes `allowLegacyCbc: true`, because V1 is unauthenticated: routing attacker-supplied strings there turns "decryption failed" into "decrypted to a value the attacker steered", which is how a single-use link could be forged. There is no fallback in the other direction — a V2 payload that fails its auth tag throws, and is never retried as CBC.
 *
 * @param payload - The encrypted string to decrypt.
 * @param key - The secret key used for decryption.
 * @param options - See `TSymmetricDecryptOptions`.
 * @returns The decrypted plaintext.
 */

export function symmetricDecrypt(
  payload: string,
  key: string,
  options: TSymmetricDecryptOptions = {}
): string {
  // Two parts means legacy V1 — hex has no colons, so the part count is an exact discriminator.
  if (payload.split(":").length === 2) {
    if (!options.allowLegacyCbc) {
      logger.warn(
        "Rejected a legacy AES-256-CBC payload: this caller does not accept unauthenticated ciphertext"
      );

      throw new Error("Unsupported encrypted payload format");
    }

    return symmetricDecryptV1(payload, key);
  }

  try {
    return symmetricDecryptV2(payload, key);
  } catch (err) {
    logger.warn({ err }, "AES-GCM decryption failed; refusing to fall back to insecure CBC");

    throw err;
  }
}

/**
 * General bcrypt hashing utility for secrets (passwords, API keys, etc.)
 */
export const hashSecret = async (secret: string, cost: number = 12): Promise<string> => {
  return await hash(secret, cost);
};

/**
 * General bcrypt verification utility for secrets (passwords, API keys, etc.)
 */
export const verifySecret = async (secret: string, hashedSecret: string): Promise<boolean> => {
  try {
    const isValid = await compare(secret, hashedSecret);
    return isValid;
  } catch (error) {
    // Log warning for debugging purposes, but don't throw to maintain security
    logger.warn({ error }, "Secret verification failed due to invalid hash format");
    // Return false for invalid hashes or other bcrypt errors
    return false;
  }
};

/**
 * SHA-256 hashing utility (deterministic, for legacy support)
 */
export const hashSha256 = (input: string): string => {
  return createHash("sha256").update(input).digest("hex");
};

/**
 * Compare two secrets — MACs, signatures, token fingerprints — without leaking how far the match got.
 * `timingSafeEqual` throws on length mismatch, so the lengths are checked first; that check is not
 * itself constant-time, which is fine because the length of a fixed-width digest is not the secret.
 *
 * Use this for anything an attacker supplies and can vary between attempts. Plain `===` on a secret is
 * the bug this exists to prevent.
 */
export const constantTimeEqual = (a: string, b: string, encoding: BufferEncoding = "utf8"): boolean => {
  const aBytes = Buffer.from(a, encoding);
  const bBytes = Buffer.from(b, encoding);

  // `Buffer.from` silently drops input that is invalid for the encoding — `Buffer.from("zz", "hex")` is
  // an empty buffer — so without this guard two malformed values would compare equal at length 0.
  if (aBytes.length === 0 || bBytes.length === 0) {
    return false;
  }

  return aBytes.length === bBytes.length && timingSafeEqual(aBytes, bBytes);
};

/**
 * Parse a v2 API key format: fbk_{secret}
 * Returns null if the key doesn't match the expected format
 */
export const parseApiKeyV2 = (key: string): { secret: string } | null => {
  // Check if it starts with fbk_
  if (!key.startsWith("fbk_")) {
    return null;
  }

  const secret = key.slice(4); // Skip 'fbk_' prefix

  // Validate that secret contains only allowed characters and is not empty
  // Secrets are base64url-encoded and can contain underscores, hyphens, and alphanumeric chars
  if (!secret || !/^[A-Za-z0-9_-]+$/.test(secret)) {
    return null;
  }

  return { secret };
};

// Standard Webhooks secret prefix
const WEBHOOK_SECRET_PREFIX = "whsec_";

/**
 * Generate a Standard Webhooks compliant secret
 * Following: https://github.com/standard-webhooks/standard-webhooks/blob/main/spec/standard-webhooks.md
 *
 * Format: whsec_ + base64(32 random bytes)
 * @returns A webhook secret in format "whsec_{base64_encoded_random_bytes}"
 */
export const generateWebhookSecret = (): string => {
  const secretBytes = randomBytes(32); // 256 bits of entropy
  return `${WEBHOOK_SECRET_PREFIX}${secretBytes.toString("base64")}`;
};

/**
 * Decode a Standard Webhooks secret to get the raw bytes
 * Strips the whsec_ prefix and base64 decodes the rest
 *
 * @param secret The webhook secret (with or without whsec_ prefix)
 * @returns Buffer containing the raw secret bytes
 */
export const getWebhookSecretBytes = (secret: string): Buffer => {
  const base64Part = secret.startsWith(WEBHOOK_SECRET_PREFIX)
    ? secret.slice(WEBHOOK_SECRET_PREFIX.length)
    : secret;
  return Buffer.from(base64Part, "base64");
};

/**
 * Generate Standard Webhooks compliant signature
 * Following: https://github.com/standard-webhooks/standard-webhooks/blob/main/spec/standard-webhooks.md
 *
 * @param webhookId Unique message identifier
 * @param timestamp Unix timestamp in seconds
 * @param payload The request body as a string
 * @param secret The shared secret (whsec_ prefixed)
 * @returns The signature in format "v1,{base64_signature}"
 */
export const generateStandardWebhookSignature = (
  webhookId: string,
  timestamp: number,
  payload: string,
  secret: string
): string => {
  const signedContent = `${webhookId}.${timestamp}.${payload}`;
  const secretBytes = getWebhookSecretBytes(secret);
  const signature = createHmac("sha256", secretBytes).update(signedContent).digest("base64");
  return `v1,${signature}`;
};
