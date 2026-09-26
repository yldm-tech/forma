import { createCacheKey } from "@forma/cache";
import { prisma } from "@forma/database";
import { Prisma } from "@forma/database/prisma";
import { normalizeLanguageCode } from "@forma/i18n-utils";
import { TContactAttributesInput } from "@forma/types/contact-attribute";
import { TJsPersonState } from "@forma/types/js";
import { cache } from "@/lib/cache";
import { toLegacyLanguageCodes } from "@/lib/i18n/utils";
import { formatAttributeMessage, updateAttributes } from "@/modules/contacts/lib/attributes";
import { getPersonSegmentIds } from "./segments";

/** Message codes whose presence alone reveals whether an identifier is already a contact here. */
const EXISTENCE_REVEALING_MESSAGE_CODES = new Set(["email_already_exists", "userid_already_exists"]);

/**
 * Shared select for the two contact-state branches (existing contact vs newly created). Kept in one
 * place so both produce the identical shape that feeds buildUserStateFromContact and the in-memory
 * survey-interaction evaluator. `createdAt`/`finished` on responses (and `createdAt` on displays) are
 * consumed by that evaluator (see getPersonSegmentIds), which relies on the load being complete — do
 * NOT add a `take` limit without also bounding it by the widest interaction window, or the in-memory
 * and Prisma evaluators would diverge.
 */
const contactStateSelect = {
  id: true,
  attributes: {
    select: {
      attributeKey: { select: { key: true } },
      value: true,
    },
  },
  responses: {
    select: { surveyId: true, createdAt: true, finished: true },
  },
  displays: {
    select: { surveyId: true, createdAt: true },
    orderBy: { createdAt: "desc" as const },
  },
} satisfies Prisma.ContactSelect;

/**
 * Comprehensive contact data fetcher - gets everything needed in one query
 * Eliminates redundant queries by fetching contact + user state data together
 */
const getContactWithFullData = async (
  workspaceId: string,
  userId: string,
  client: Prisma.TransactionClient = prisma
) => {
  return client.contact.findFirst({
    where: {
      workspaceId,
      attributes: {
        some: {
          attributeKey: { key: "userId", workspaceId },
          value: userId,
        },
      },
    },
    select: contactStateSelect,
  });
};

/**
 * Creates contact with comprehensive data structure
 */
const createContact = async (
  workspaceId: string,
  userId: string,
  client: Prisma.TransactionClient = prisma
) => {
  return client.contact.create({
    data: {
      workspace: {
        connect: { id: workspaceId },
      },
      attributes: {
        create: [
          {
            attributeKey: {
              connect: { key_workspaceId: { key: "userId", workspaceId } },
            },
            value: userId,
          },
        ],
      },
    },
    select: contactStateSelect,
  });
};

/**
 * Returns the workspace's contact for `userId`, creating it on the first identify.
 *
 * The read and the create have to be serialized, because nothing in the schema can stop two of them
 * landing at once. A contact's `userId` is not a Contact column — it is a ContactAttribute row — so
 * the nearest available unique index would be `(attributeKeyId, value)`, which would also forbid two
 * contacts from ever sharing an ordinary attribute value like `plan = "pro"`. Denormalizing `userId`
 * onto Contact to carry a `@@unique([workspaceId, userId])` is the durable fix, but it needs a
 * backfill migration that existing duplicate rows would block, so it is a migration of its own rather
 * than part of this change.
 *
 * Without serialization two concurrent identifies for one brand-new userId both read null and both
 * create, leaving the workspace with two permanent contacts for one person. Later reads are
 * `findFirst`, so they pick between them arbitrarily and the person's displays and responses split
 * across both — the recontact check stops seeing the response it recorded on the sibling row and
 * re-shows an answered survey.
 *
 * So the miss path takes a transaction-scoped Postgres advisory lock on the (workspaceId, userId)
 * pair, then re-reads inside the lock: the loser of the race blocks until the winner commits, finds
 * the contact, and skips its create. The lock is released when the transaction ends, whether it
 * commits or aborts. A hash collision between two unrelated pairs only makes one of them wait.
 *
 * Two deliberate limits. The fast path is unchanged — an already-known contact is served by the same
 * single `findFirst` as before and never opens a transaction, so only the rare first identify pays.
 * And the lock is cooperative: other paths that create contacts (CSV import, the management API) do
 * not take it, so it closes this endpoint racing itself, which is the case the widget actually
 * produces. It is safe on a database that already holds duplicates — there is no constraint to
 * violate, and `findFirst` keeps returning one of them as it does today.
 */
const findOrCreateContact = async (workspaceId: string, userId: string) => {
  const existingContact = await getContactWithFullData(workspaceId, userId);
  if (existingContact) return existingContact;

  return prisma.$transaction(async (tx) => {
    await tx.$executeRaw`SELECT pg_advisory_xact_lock(hashtextextended(${`contact:${workspaceId}:${userId}`}, 0))`;
    const contact = await getContactWithFullData(workspaceId, userId, tx);
    return contact ?? (await createContact(workspaceId, userId, tx));
  });
};

/**
 * Build user state from already-fetched contact data
 * Eliminates the need for separate getUserState query
 */
const buildUserStateFromContact = async (
  contactData: NonNullable<Awaited<ReturnType<typeof getContactWithFullData>>>,
  workspaceId: string,
  userId: string,
  device: "phone" | "desktop"
) => {
  // Get segments (only remaining external call)
  // Ensure segments is always an array to prevent "segments is not iterable" error
  let segments: string[] = [];
  try {
    // The contact's displays/responses are already loaded here, so interaction-only segments can be
    // evaluated in memory without any additional query (see getPersonSegmentIds).
    segments = await getPersonSegmentIds(workspaceId, contactData.id, userId, device, {
      displays: contactData.displays,
      responses: contactData.responses,
    });
    // Double-check that segments is actually an array
    if (!Array.isArray(segments)) {
      segments = [];
    }
  } catch {
    // If segments fetching fails, use empty array as fallback
    segments = [];
  }

  // Process data efficiently from already-fetched contact
  const displays = contactData.displays.map((display) => ({
    surveyId: display.surveyId,
    createdAt: display.createdAt,
  }));

  const responses = contactData.responses.map((response) => response.surveyId);

  const lastDisplayAt = contactData.displays.length > 0 ? contactData.displays[0].createdAt : null;

  return {
    contactId: contactData.id,
    userId,
    segments,
    displays,
    responses,
    lastDisplayAt,
  };
};

/**
 * A contact `language` value that doesn't canonicalize (normalizeLanguageCode -> null) may still be a
 * legitimate, user-configured survey-language ALIAS (the documented `setLanguage("<alias>")`), not junk.
 * Match it (case-insensitively) against the workspace's configured languages and return the CONFIGURED
 * form — the alias exactly as stored (or the code) — so the persisted value stays consistent with the
 * workspace config regardless of the caller's casing, mirroring how codes are canonicalized. Returns null for genuine junk.
 * Only called on the rare non-canonical branch, so the common language-code path pays no extra query.
 */
const resolveWorkspaceLanguageIdentifier = async (
  workspaceId: string,
  value: string
): Promise<string | null> => {
  const target = value.toLowerCase();
  // Cache the workspace's language list (rarely-changing config) so repeated non-canonical writes don't
  // each hit the DB. TTL matches the environment-state cache (createCacheKey.workspace.state) which serves
  // these same languages to SDKs, so this adds no staleness beyond what already exists for this data — a
  // newly-added alias is likewise picked up within the TTL.
  const languages = await cache.withCache(
    () =>
      prisma.language.findMany({
        where: { workspaceId },
        select: { code: true, alias: true },
      }),
    createCacheKey.workspace.languages(workspaceId),
    60 * 1000 // 1 minute in milliseconds
  );
  const match = languages.find((l) => l.code.toLowerCase() === target || l.alias?.toLowerCase() === target);
  if (!match) return null;
  // Return the configured casing: the alias if the value matched the alias, otherwise the code.
  return match.alias && match.alias.toLowerCase() === target ? match.alias : match.code;
};

export const updateUser = async (
  workspaceId: string,
  userId: string,
  device: "phone" | "desktop",
  attributes?: TContactAttributesInput
): Promise<{ state: TJsPersonState; messages?: string[]; errors?: string[] }> => {
  // Single comprehensive query - gets contact + user state data, creating the contact on first sight
  const contactData = await findOrCreateContact(workspaceId, userId);

  // Process contact attributes efficiently (single pass)
  let contactAttributes = contactData.attributes.reduce(
    (acc, ctx) => {
      acc[ctx.attributeKey.key] = ctx.value;
      return acc;
    },
    {} as Record<string, string>
  );

  let messages: string[] = [];
  let errors: string[] = [];
  let language = contactAttributes.language;

  // Standardize the contact's `language` attribute to its canonical BCP-47 tag on write (ENG-1067), so a
  // legacy SDK `setLanguage("de")` lands as `de-DE`. An invalid or blank language is dropped from the
  // payload — the rest of the attributes still persist — rather than stored verbatim. (The SDK never
  // validated `setLanguage`, so callers could send anything; this keeps junk out of the column going
  // forward. Pre-existing invalid values are left as-is; they simply don't match any survey language.)
  let normalizedAttributes = attributes;
  let droppedInvalidLanguage: string | null = null;
  if (attributes && "language" in attributes) {
    const { language: rawLanguage, ...otherAttributes } = attributes;
    // `rawLanguage` is string | number | boolean — stringify once, then decide on the *trimmed* content,
    // so falsy-but-real values (0, false, "0") are treated like any other invalid code while blank or
    // whitespace-only input stays silent (truthiness alone would drop 0/false without a message).
    const languageStr = String(rawLanguage);
    const trimmedLanguage = languageStr.trim();
    const canonicalLanguage = trimmedLanguage ? normalizeLanguageCode(languageStr) : null;
    const configuredLanguage =
      !canonicalLanguage && trimmedLanguage
        ? await resolveWorkspaceLanguageIdentifier(workspaceId, trimmedLanguage)
        : null;
    if (canonicalLanguage) {
      normalizedAttributes = { ...otherAttributes, language: canonicalLanguage };
    } else if (configuredLanguage) {
      // Not a canonicalizable code, but it matches a configured survey-language alias in this workspace
      // (documented setLanguage("<alias>")). Store the configured form (not the caller's casing) so the
      // persisted/echoed value is consistent with the workspace config, and alias matching keeps working.
      normalizedAttributes = { ...otherAttributes, language: configuredLanguage };
    } else {
      normalizedAttributes = otherAttributes;
      // Surface a non-blank, unrecognized value back to the caller; blank/whitespace-only stays silent.
      if (trimmedLanguage) droppedInvalidLanguage = languageStr;
    }
  }

  // Handle attribute updates efficiently
  if (normalizedAttributes && Object.keys(normalizedAttributes).length > 0) {
    // Single pass comparison - check if any attribute has changed
    const hasChanges = Object.entries(normalizedAttributes).some(
      ([key, value]) => value !== contactAttributes[key]
    );

    if (hasChanges) {
      const {
        success,
        messages: updateAttrMessages,
        errors: updateAttrErrors,
      } = await updateAttributes(contactData.id, userId, workspaceId, normalizedAttributes);

      // This runs on the *unauthenticated* public client endpoint, and workspaceId is public — it ships
      // in the widget snippet on the customer's own site. Echoing "the email/userId already exists"
      // turns the endpoint into an oracle for "is this address a contact of this organization?",
      // answerable for any address an attacker cares to try. The SDK only debug-logs these two, so
      // withholding them costs nothing; genuine `errors` are still returned in full.
      messages =
        updateAttrMessages
          ?.filter((message) => !EXISTENCE_REVEALING_MESSAGE_CODES.has(message.code))
          .map(formatAttributeMessage) ?? [];
      errors = updateAttrErrors?.map(formatAttributeMessage) ?? [];

      // Update language if provided (used in response state)
      if (success && normalizedAttributes.language) {
        language = String(normalizedAttributes.language);
      }
    }
  }

  // Tell the caller we ignored an invalid language (mirrors how skipped/invalid attributes are surfaced).
  if (droppedInvalidLanguage) {
    messages.push(
      formatAttributeMessage({
        code: "invalid_language_ignored",
        params: { language: droppedInvalidLanguage },
      })
    );
  }

  // Build user state from already-fetched data (no additional query needed)
  const userStateData = await buildUserStateFromContact(contactData, workspaceId, userId, device);

  // Transitional SDK back-compat (ENG-1067): echo the language to the SDK in a legacy form (the first
  // known alias), matching the legacy codes the client environment serializer exposes, so SDK clients
  // that match the display language by exact code keep working until the canonical-aware versions are
  // adopted (notably older React Native apps). A canonicalizable code is echoed in its legacy form; a
  // non-canonicalizable stored value is a verified survey-language alias (the write path only stores those
  // or canonical codes), so echo it verbatim so the SDK still matches the aliased language. Remove once
  // those clients have drained.
  const storedCanonicalLanguage = language ? normalizeLanguageCode(String(language)) : null;
  const responseLanguage = storedCanonicalLanguage
    ? (toLegacyLanguageCodes(storedCanonicalLanguage)[0] ?? storedCanonicalLanguage)
    : (language ?? undefined);

  return {
    state: {
      data: {
        ...userStateData,
        language: responseLanguage,
      },
      expiresAt: new Date(Date.now() + 1000 * 60 * 30), // 30 minutes
    },
    messages: messages.length > 0 ? messages : undefined,
    errors: errors.length > 0 ? errors : undefined,
  };
};
