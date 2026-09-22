import { describe, expect, test, vi } from "vitest";

// The global setup pins `ENCRYPTION_KEY` to "mock-encryption-key", which is neither 32 characters nor
// valid hex, so `createCipheriv` would reject it. These tests run the real AES, so they need a real key.
const { TEST_ENCRYPTION_KEY } = vi.hoisted(() => ({
  TEST_ENCRYPTION_KEY: "0123456789abcdef0123456789abcdef",
}));

vi.mock("@/lib/constants", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/constants")>()),
  ENCRYPTION_KEY: TEST_ENCRYPTION_KEY,
}));

const { symmetricEncrypt } = await import("@/lib/crypto");
const { decryptIntegrationCredentials, encryptIntegrationCredentials } =
  await import("./credential-encryption");

const withKey = (key: Record<string, unknown>) => ({ config: { key, data: [] } });

/** The pre-GCM `iv:ciphertext` form, produced the way the removed `symmetricDecryptV1` reads it. */
const legacyCbcEncrypt = async (text: string): Promise<string> => {
  const { createCipheriv, randomBytes } = await import("node:crypto");
  const iv = randomBytes(16);
  const cipher = createCipheriv("aes256", Buffer.from(TEST_ENCRYPTION_KEY, "latin1"), iv);
  return `${iv.toString("hex")}:${cipher.update(text, "utf8", "hex") + cipher.final("hex")}`;
};

describe("encryptIntegrationCredentials", () => {
  test("encrypts every secret field and leaves the display fields alone", () => {
    const encrypted = encryptIntegrationCredentials(
      withKey({
        access_token: "xoxb-slack-secret",
        refresh_token: "1//long-lived-secret",
        token_type: "bot",
        expiry_date: 1234567890,
        team: { id: "T1", name: "Acme" },
      })
    );

    expect(encrypted.config.key.access_token).not.toBe("xoxb-slack-secret");
    expect(encrypted.config.key.access_token).toMatch(/^[0-9a-f]{32}:[0-9a-f]+:[0-9a-f]{32}$/);
    expect(encrypted.config.key.refresh_token).toMatch(/^[0-9a-f]{32}:[0-9a-f]+:[0-9a-f]{32}$/);
    expect(encrypted.config.key.token_type).toBe("bot");
    expect(encrypted.config.key.expiry_date).toBe(1234567890);
    expect(encrypted.config.key.team).toEqual({ id: "T1", name: "Acme" });
  });

  test("round-trips through decrypt", () => {
    const original = withKey({ access_token: "ya29.super-secret", refresh_token: "1//secret" });
    const restored = decryptIntegrationCredentials(encryptIntegrationCredentials(original));

    expect(restored.config.key).toEqual(original.config.key);
  });

  // The write pass runs over rows the read pass may have handed back untouched, so it has to be a no-op
  // on ciphertext or the real token ends up buried under a second layer.
  test("does not re-encrypt a value that is already ciphertext", () => {
    const ciphertext = symmetricEncrypt("ntn_notion-secret", TEST_ENCRYPTION_KEY);
    const encrypted = encryptIntegrationCredentials(withKey({ access_token: ciphertext }));

    expect(encrypted.config.key.access_token).toBe(ciphertext);
  });

  // `googleSheet/service.ts` raises the reconnect error on a falsy `refresh_token`; encrypting "" would
  // make every such row look like it still held a token.
  test("leaves an empty credential empty", () => {
    const encrypted = encryptIntegrationCredentials(withKey({ refresh_token: "" }));

    expect(encrypted.config.key.refresh_token).toBe("");
  });

  test("passes through an integration with no key", () => {
    const integration = { config: { key: null, data: [] } };

    expect(encryptIntegrationCredentials(integration)).toBe(integration);
  });
});

describe("decryptIntegrationCredentials", () => {
  test("decrypts the current AES-GCM form", () => {
    const decrypted = decryptIntegrationCredentials(
      withKey({ access_token: symmetricEncrypt("xoxb-slack-secret", TEST_ENCRYPTION_KEY) })
    );

    expect(decrypted.config.key.access_token).toBe("xoxb-slack-secret");
  });

  // Notion rows written before the GCM switch hold this form. Omitting it would disconnect those
  // installs on the deploy that moved decryption out of `lib/notion/service.ts`.
  test("decrypts the legacy AES-CBC form Notion rows may still hold", async () => {
    const decrypted = decryptIntegrationCredentials(
      withKey({ access_token: await legacyCbcEncrypt("ntn_legacy-secret") })
    );

    expect(decrypted.config.key.access_token).toBe("ntn_legacy-secret");
  });

  // Every Slack, Google Sheets and Airtable row in an existing install is cleartext. This is the whole
  // migration: if it threw or returned a mangled value, the first deploy would disconnect all of them.
  test("returns a cleartext credential unchanged", () => {
    const decrypted = decryptIntegrationCredentials(
      withKey({ access_token: "xoxb-written-before-this-landed", refresh_token: "1//also-cleartext" })
    );

    expect(decrypted.config.key.access_token).toBe("xoxb-written-before-this-landed");
    expect(decrypted.config.key.refresh_token).toBe("1//also-cleartext");
  });

  test("re-encrypts a cleartext credential on the next write", () => {
    const stored = withKey({ access_token: "xoxb-written-before-this-landed" });
    const rewritten = encryptIntegrationCredentials(decryptIntegrationCredentials(stored));

    expect(rewritten.config.key.access_token).toMatch(/^[0-9a-f]{32}:[0-9a-f]+:[0-9a-f]{32}$/);
    expect(decryptIntegrationCredentials(rewritten).config.key.access_token).toBe(
      "xoxb-written-before-this-landed"
    );
  });

  // One unreadable row travels with every other row a list query returned, so throwing here would take
  // the whole integrations settings page down rather than the one integration that has to be reconnected.
  test("hands back a credential it cannot decrypt instead of throwing", () => {
    const foreign = symmetricEncrypt("xoxb-secret", "fedcba9876543210fedcba9876543210");
    const decrypted = decryptIntegrationCredentials(withKey({ access_token: foreign }));

    expect(decrypted.config.key.access_token).toBe(foreign);
  });
});

describe("classification of stored values", () => {
  // A token that happens to carry colons must not be read as ciphertext, which is why the check is the
  // 32-hex IV and not the colon count `crypto.ts` uses on values it knows it produced.
  test.each([
    ["a colon-bearing plaintext token", "basic:auth:token"],
    ["a two-part value whose first part is not a 32-hex IV", "nothex:deadbeef"],
    ["a three-part value with a short tag", `${"a".repeat(32)}:deadbeef:ab`],
  ])("treats %s as cleartext", (_label, value) => {
    expect(decryptIntegrationCredentials(withKey({ access_token: value })).config.key.access_token).toBe(
      value
    );
  });
});
