import { Prisma } from "@forma/database/prisma";
import { logger } from "@forma/logger";
import { DatabaseError } from "@forma/types/errors";
import { TIntegrationItem } from "@forma/types/integration";
import {
  TIntegrationAirtableConfig,
  TIntegrationAirtableConfigData,
  TIntegrationAirtableCredential,
  ZIntegrationAirtableBases,
  ZIntegrationAirtableCredential,
  ZIntegrationAirtableTables,
  ZIntegrationAirtableTablesWithFields,
  ZIntegrationAirtableTokenSchema,
} from "@forma/types/integration/airtable";
import { AIRTABLE_CLIENT_ID, AIRTABLE_MESSAGE_LIMIT } from "../constants";
import { createOrUpdateIntegration, getIntegrationByType } from "../integration/service";
import { delay } from "../utils/promises";
import { truncateText } from "../utils/strings";

export const getBases = async (key: string) => {
  const req = await fetch("https://api.airtable.com/v0/meta/bases", {
    headers: {
      Authorization: `Bearer ${key}`,
    },
  });

  if (!req.ok) {
    const body = await req.text().catch(() => "");
    throw new Error(`Airtable API error fetching bases: ${req.status} ${req.statusText} ${body}`);
  }

  const res = await req.json();
  return ZIntegrationAirtableBases.parse(res);
};

const tableFetcher = async (key: TIntegrationAirtableCredential, baseId: string) => {
  const req = await fetch(`https://api.airtable.com/v0/meta/bases/${baseId}/tables`, {
    headers: {
      Authorization: `Bearer ${key.access_token}`,
    },
  });

  if (!req.ok) {
    const body = await req.text().catch(() => "");
    throw new Error(`Airtable API error fetching tables: ${req.status} ${req.statusText} ${body}`);
  }

  const res = await req.json();

  return res;
};

export const getTables = async (key: TIntegrationAirtableCredential, baseId: string) => {
  const res = await tableFetcher(key, baseId);
  return ZIntegrationAirtableTables.parse(res);
};

export const fetchAirtableAuthToken = async (formData: Record<string, any>) => {
  const formBody = Object.keys(formData)
    .map((key) => `${encodeURIComponent(key)}=${encodeURIComponent(formData[key])}`)
    .join("&");

  const tokenReq = await fetch("https://airtable.com/oauth2/v1/token", {
    headers: {
      "Content-Type": "application/x-www-form-urlencoded",
    },
    body: formBody,
    method: "POST",
  });

  const tokenRes: unknown = await tokenReq.json();
  const parsedToken = ZIntegrationAirtableTokenSchema.safeParse(tokenRes);

  if (!parsedToken.success) {
    logger.error(parsedToken.error, "Error parsing airtable token");
    throw new Error(parsedToken.error.message);
  }
  const { access_token, refresh_token, expires_in } = parsedToken.data;
  const expiry_date = new Date();
  expiry_date.setSeconds(expiry_date.getSeconds() + expires_in);

  return {
    access_token,
    expiry_date: expiry_date.toISOString(),
    refresh_token,
  };
};

/** Refresh slightly ahead of the stored expiry, the way `googleSheet/service.ts` does. */
const TOKEN_EXPIRY_BUFFER_MS = 5 * 60 * 1000;

/**
 * Token lifecycle belongs to the credential, not to the caller: the settings UI refreshed through
 * `getAirtableToken` while the response pipeline read `integration.config.key` straight off the record,
 * so an Airtable integration stopped delivering the moment its stored `expiry_date` passed and only
 * resumed when a human happened to open the settings page. Both paths now come through here.
 *
 * Airtable rotates the refresh token on use, so the stored one is dead as soon as this call returns:
 * the rotation is persisted before the new access token is handed back, and a failure to persist is
 * surfaced rather than swallowed. Concurrent deliveries for one workspace would race that single-use
 * token; the response pipeline worker runs at concurrency 1 today, which is what makes this safe.
 */
export const resolveAirtableCredential = async (
  workspaceId: string,
  config: TIntegrationAirtableConfig
): Promise<TIntegrationAirtableCredential> => {
  const credential = ZIntegrationAirtableCredential.parse(config.key);
  const expiresAt = new Date(credential.expiry_date).getTime();

  // An unparseable `expiry_date` is treated as expired: refreshing costs one request, using a dead
  // token costs the response.
  if (Number.isFinite(expiresAt) && expiresAt > Date.now() + TOKEN_EXPIRY_BUFFER_MS) {
    return credential;
  }

  const newToken = await fetchAirtableAuthToken({
    grant_type: "refresh_token",
    refresh_token: credential.refresh_token,
    client_id: AIRTABLE_CLIENT_ID,
  });

  await createOrUpdateIntegration(workspaceId, {
    type: "airtable",
    config: {
      data: config.data,
      email: config.email,
      key: newToken,
    },
  });

  return newToken;
};

export const getAirtableToken = async (workspaceId: string) => {
  try {
    const airtableIntegration = await getIntegrationByType(workspaceId, "airtable");

    if (!airtableIntegration) {
      throw new Error(`No Airtable integration found for workspace ${workspaceId}`);
    }

    const credential = await resolveAirtableCredential(workspaceId, airtableIntegration.config);

    return credential.access_token;
  } catch (error) {
    logger.error(
      {
        workspaceId,
        error,
      },
      "Failed to get Airtable token"
    );
    // Keep the cause: flattening every failure into one string hid a revoked grant behind a generic
    // message, which is the only signal the delivery path has to go on.
    throw new Error("Failed to get Airtable token", { cause: error });
  }
};

export const getAirtableTables = async (workspaceId: string) => {
  let tables: TIntegrationItem[] = [];
  try {
    const token = await getAirtableToken(workspaceId);

    tables = (await getBases(token)).bases;

    return tables;
  } catch (error) {
    if (error instanceof Prisma.PrismaClientKnownRequestError) {
      throw new DatabaseError("Database operation failed");
    }
    throw error;
  }
};

const addRecords = async (
  key: TIntegrationAirtableCredential,
  baseId: string,
  tableId: string,
  data: Record<string, string>
) => {
  const req = await fetch(`https://api.airtable.com/v0/${baseId}/${tableId}`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${key.access_token}`,
      "Content-type": "application/json",
    },
    body: JSON.stringify({
      fields: data,
      typecast: true,
    }),
  });

  if (!req.ok) {
    const body = await req.text().catch(() => "");
    throw new Error(`Airtable API error creating record: ${req.status} ${req.statusText} ${body}`);
  }

  const res = await req.json();

  return res;
};

const addField = async (
  key: TIntegrationAirtableCredential,
  baseId: string,
  tableId: string,
  data: Record<string, string>
) => {
  const req = await fetch(`https://api.airtable.com/v0/meta/bases/${baseId}/tables/${tableId}/fields`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${key.access_token}`,
      "Content-type": "application/json",
    },
    body: JSON.stringify(data),
  });

  return await req.json();
};

const getExistingFields = async (key: TIntegrationAirtableCredential, baseId: string, tableId: string) => {
  const req = await tableFetcher(key, baseId);
  const tables = ZIntegrationAirtableTablesWithFields.parse(req).tables;
  const currentTable = tables.find((t) => t.id === tableId);

  if (!currentTable) {
    throw new Error(`Table with ID ${tableId} not found`);
  }

  return new Set(currentTable.fields.map((f) => f.name));
};

export const writeData = async (
  key: TIntegrationAirtableCredential,
  configData: TIntegrationAirtableConfigData,
  responses: string[],
  elements: string[]
) => {
  if (responses.length !== elements.length) {
    throw new Error(
      `Array length mismatch: responses (${responses.length}) and elements (${elements.length}) must be equal`
    );
  }

  // 1) Build the record payload
  const data: Record<string, string> = {};
  for (let i = 0; i < elements.length; i++) {
    data[elements[i]] =
      responses[i].length > AIRTABLE_MESSAGE_LIMIT
        ? truncateText(responses[i], AIRTABLE_MESSAGE_LIMIT)
        : responses[i];
  }

  // 2) Figure out which fields need creating
  const existingFields = await getExistingFields(key, configData.baseId, configData.tableId);
  const fieldsToCreate = elements.filter((q) => !existingFields.has(q));

  // 3) Create any missing fields with throttling to respect Airtable's 5 req/sec per base limit
  if (fieldsToCreate.length > 0) {
    // Sequential processing with delays
    const DELAY_BETWEEN_REQUESTS = 250; // 250ms = 4 requests per second (staying under 5/sec limit)

    for (let i = 0; i < fieldsToCreate.length; i++) {
      const fieldName = fieldsToCreate[i];

      const createRes = await addField(key, configData.baseId, configData.tableId, {
        name: fieldName,
        type: "singleLineText",
      });

      if (createRes?.error) {
        throw new Error(`Failed to create field "${fieldName}": ${JSON.stringify(createRes)}`);
      }

      // Add delay between requests (except for the last one)
      if (i < fieldsToCreate.length - 1) {
        await delay(DELAY_BETWEEN_REQUESTS);
      }
    }

    // 4) Wait for the new fields to show up
    await waitForFieldsToExist(key, configData, fieldsToCreate);
  }

  // 5) Finally, add the records
  await addRecords(key, configData.baseId, configData.tableId, data);
};

async function waitForFieldsToExist(
  key: TIntegrationAirtableCredential,
  configData: TIntegrationAirtableConfigData,
  fieldNames: string[],
  maxRetries = 5,
  intervalMs = 2000
) {
  let existingFields: Set<string> = new Set(),
    missingFields: string[] = [];

  for (let attempt = 1; attempt <= maxRetries; attempt++) {
    existingFields = await getExistingFields(key, configData.baseId, configData.tableId);
    missingFields = fieldNames.filter((f) => !existingFields.has(f));

    if (missingFields.length === 0) {
      return;
    }

    if (attempt < maxRetries) {
      logger.error(
        `Attempt ${attempt}/${maxRetries}: ${missingFields.length} field(s) still missing [${missingFields.join(
          ", "
        )}], retrying in ${intervalMs / 1000}s…`
      );

      await new Promise((r) => setTimeout(r, intervalMs));
    }
  }

  throw new Error(
    `Timed out waiting for ${missingFields.length} field(s) [${missingFields.join(
      ", "
    )}] to become available. Available fields: [${Array.from(existingFields).join(", ")}]`
  );
}
