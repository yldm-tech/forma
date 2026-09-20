import { TIntegrationNotionConfig, TIntegrationNotionDatabase } from "@forma/types/integration/notion";
import { ENCRYPTION_KEY } from "@/lib/constants";
import { symmetricDecrypt } from "@/lib/crypto";
import { getIntegrationByType } from "../integration/service";

const fetchPages = async (config: TIntegrationNotionConfig) => {
  try {
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
    return (await res.json()).results;
  } catch (error) {
    throw error;
  }
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
  try {
    await fetch(`https://api.notion.com/v1/pages`, {
      headers: getHeaders(config),
      method: "POST",
      body: JSON.stringify({
        parent: {
          database_id: databaseId,
        },
        properties: properties,
      }),
    });
  } catch (error) {
    throw error;
  }
};

const getHeaders = (config: TIntegrationNotionConfig) => {
  // `allowLegacyCbc`: the token is at rest in `Integration.config`, written by whatever version of the app connected the integration, and nothing on a request can influence it. An install that connected Notion before the GCM switch would otherwise lose the integration on the next sync.
  const decryptedToken = symmetricDecrypt(config.key.access_token, ENCRYPTION_KEY!, {
    allowLegacyCbc: true,
  });
  return {
    Accept: "application/json",
    "Content-Type": "application/json",
    Authorization: `Bearer ${decryptedToken}`,
    "Notion-Version": "2022-06-28",
  };
};
