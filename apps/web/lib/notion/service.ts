import { TIntegrationNotionConfig, TIntegrationNotionDatabase } from "@forma/types/integration/notion";
import { getIntegrationByType } from "../integration/service";

/**
 * Notion answers a rejected request with a 4xx and a `{ code, message }` body. Both calls below used to
 * discard the response, so a revoked grant or a renamed column read exactly like a success: the delivery
 * pipeline logged nothing and the mapping modal showed an empty database list. The body is read
 * defensively because an error response is not guaranteed to be JSON.
 */
const readNotionError = async (res: Response): Promise<string> => {
  const body: unknown = await res.json().catch(() => null);
  if (!body || typeof body !== "object") return "";
  const { code, message } = body as { code?: unknown; message?: unknown };
  return [code, message].filter((part) => typeof part === "string").join(" ");
};

const fetchPages = async (config: TIntegrationNotionConfig): Promise<TIntegrationNotionDatabase[]> => {
  const res = await fetch("https://api.notion.com/v1/search", {
    headers: getHeaders(config),
    method: "POST",
    body: JSON.stringify({
      page_size: 100,
      filter: {
        value: "database",
        property: "object",
      },
    }),
  });

  if (!res.ok) {
    throw new Error(
      `Notion API error fetching databases: ${res.status} ${res.statusText} ${await readNotionError(res)}`
    );
  }

  const body: { results?: TIntegrationNotionDatabase[] } = await res.json();
  return body.results ?? [];
};

export const getNotionDatabases = async (workspaceId: string): Promise<TIntegrationNotionDatabase[]> => {
  let results: TIntegrationNotionDatabase[] = [];
  try {
    const notionIntegration = await getIntegrationByType(workspaceId, "notion");
    if (notionIntegration && notionIntegration.config?.key.bot_id) {
      results = await fetchPages(notionIntegration.config);
    }
    return results;
  } catch (error) {
    throw error;
  }
};

export const writeData = async (
  databaseId: string,
  properties: Record<string, Object>,
  config: TIntegrationNotionConfig
) => {
  const res = await fetch(`https://api.notion.com/v1/pages`, {
    headers: getHeaders(config),
    method: "POST",
    body: JSON.stringify({
      parent: {
        database_id: databaseId,
      },
      properties: properties,
    }),
  });

  if (!res.ok) {
    throw new Error(
      `Notion API error creating page: ${res.status} ${res.statusText} ${await readNotionError(res)}`
    );
  }
};

const getHeaders = (config: TIntegrationNotionConfig) => {
  // The token arrives in cleartext: `transformIntegration` decrypts every credential field as it reads
  // the row, including the pre-GCM form an install that connected Notion before that switch still holds
  // (lib/integration/credential-encryption.ts). Decrypting again here would fail on the plaintext.
  return {
    Accept: "application/json",
    "Content-Type": "application/json",
    Authorization: `Bearer ${config.key.access_token}`,
    "Notion-Version": "2022-06-28",
  };
};
